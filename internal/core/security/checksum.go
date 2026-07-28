package security

import (
	"context"
	"crypto/subtle"
	"encoding/hex"
	"path"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/fs"
)

// ChecksumRequest describes one integrity check.
type ChecksumRequest struct {
	Path     string
	Expected string
	// Algorithm may be left empty, in which case it is worked out from the
	// length of the expected digest.
	Algorithm fs.Algorithm
}

// ChecksumResult reports what was expected, what was found, and whether they
// agree.
//
// Both digests are reported even on a match. A verification tool that prints
// only "ok" gives the user nothing to check when they suspect the tool itself.
type ChecksumResult struct {
	Path      string `json:"path"`
	Algorithm string `json:"algorithm"`
	Expected  string `json:"expected"`
	Actual    string `json:"actual"`
	Match     bool   `json:"match"`
	Bytes     int64  `json:"bytes"`
}

// VerifyChecksum checks a file against an expected digest.
//
// A mismatch is a result, not an error: finding out that a download does not
// match its published checksum is the entire purpose of the command, and it
// succeeded in telling you. The caller turns that into a non-zero exit code.
//
// Only a failure to perform the check (a missing file, an unreadable one, a
// digest that is not a digest) comes back as an error.
func VerifyChecksum(ctx context.Context, hasher Hasher, request ChecksumRequest) (ChecksumResult, error) {
	expected, algorithm, err := parseExpected(request.Expected, request.Algorithm)
	if err != nil {
		return ChecksumResult{}, err
	}

	if strings.TrimSpace(request.Path) == "" {
		return ChecksumResult{}, errors.New(errors.CodeInvalidInput, "no file was given").
			WithHint("pass the file to verify and the digest to check it against")
	}

	resolved, err := hasher.Resolve(request.Path)
	if err != nil {
		return ChecksumResult{}, err
	}

	entry, err := hasher.Stat(resolved)
	if err != nil {
		return ChecksumResult{}, err
	}
	if entry.IsDir {
		return ChecksumResult{}, errors.New(errors.CodeInvalidInput,
			"%s is a directory", resolved).
			WithHint("pass the file whose checksum you want to verify")
	}

	checksums, err := hasher.Digest(ctx, resolved, []fs.Algorithm{algorithm})
	if err != nil {
		return ChecksumResult{}, err
	}

	actual := checksums[0].Value

	// Constant-time comparison. The threat it defends against does not really
	// apply to a local file check, but the cost is nothing and getting into
	// the habit of comparing digests this way is worth more than the
	// microseconds.
	match := subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1

	return ChecksumResult{
		Path:      resolved,
		Algorithm: string(algorithm),
		Expected:  expected,
		Actual:    actual,
		Match:     match,
		Bytes:     entry.Bytes,
	}, nil
}

// ChecksumFileRequest describes a check against a published checksum file:
// the SHA256SUMS a release attaches, in the format every *sum tool writes.
type ChecksumFileRequest struct {
	// Path is the checksum file itself. The names inside it are read
	// relative to the directory it sits in, which is where a downloaded
	// release usually lands.
	Path string
	// Only limits the check to these names. Empty checks every entry.
	Only []string
	// Algorithm may be left empty, in which case each line's digest is read
	// for its length. When set it is asserted against every line.
	Algorithm fs.Algorithm
}

// Checksum entry outcomes.
const (
	StatusMatch    = "match"
	StatusMismatch = "mismatch"
	StatusMissing  = "missing"
)

// ChecksumEntry is one line of a checksum file and what became of it.
type ChecksumEntry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Algorithm string `json:"algorithm"`
	Expected  string `json:"expected"`
	// Actual is empty when the file was not there to hash.
	Actual string `json:"actual"`
	Status string `json:"status"`
	Bytes  int64  `json:"bytes"`
}

// ChecksumFileResult reports every entry and the tally.
type ChecksumFileResult struct {
	Source     string          `json:"source"`
	Entries    []ChecksumEntry `json:"entries"`
	Matched    int             `json:"matched"`
	Mismatched int             `json:"mismatched"`
	Missing    int             `json:"missing"`
}

// VerifyChecksumFile checks the files listed in a checksum file.
//
// A file named in the list but absent from the directory is reported as
// missing and is not a failure. A release publishes one checksum file for
// every artefact it built, and almost nobody downloads all six; treating the
// five that were never fetched as failures would make the command useless for
// the case it exists to serve. Nothing is lost by it either, because a file
// that is present and wrong is still a mismatch.
//
// Like the single-digest form, a mismatch is a result rather than an error.
// The caller decides the exit code.
func VerifyChecksumFile(
	ctx context.Context,
	hasher Hasher,
	request ChecksumFileRequest,
) (ChecksumFileResult, error) {
	if strings.TrimSpace(request.Path) == "" {
		return ChecksumFileResult{}, errors.New(errors.CodeInvalidInput, "no checksum file was given").
			WithHint("pass the SHA256SUMS published with the download")
	}

	source, err := hasher.Resolve(request.Path)
	if err != nil {
		return ChecksumFileResult{}, err
	}

	contents, err := hasher.ReadFile(source)
	if err != nil {
		return ChecksumFileResult{}, err
	}

	entries, err := parseChecksumFile(string(contents), request.Algorithm)
	if err != nil {
		return ChecksumFileResult{}, err
	}
	if len(entries) == 0 {
		return ChecksumFileResult{}, errors.New(errors.CodeInvalidInput,
			"%s lists no digests", source).
			WithHint("a checksum file has one digest and one file name per line")
	}

	entries, err = selectEntries(entries, request.Only)
	if err != nil {
		return ChecksumFileResult{}, err
	}

	result := ChecksumFileResult{Source: source, Entries: entries}
	directory := filepath.Dir(source)

	for index := range result.Entries {
		if err := ctx.Err(); err != nil {
			return ChecksumFileResult{}, err
		}
		if err := verifyEntry(ctx, hasher, directory, &result.Entries[index]); err != nil {
			return ChecksumFileResult{}, err
		}

		switch result.Entries[index].Status {
		case StatusMatch:
			result.Matched++
		case StatusMismatch:
			result.Mismatched++
		default:
			result.Missing++
		}
	}

	return result, nil
}

// verifyEntry hashes one listed file and records the outcome on the entry.
//
// Only a missing file is folded into the result. Anything else that stops the
// hash (an unreadable file, a name that turned out to be a directory) fails
// the whole run: a check that quietly covered fewer files than the user
// believes is worse than one that stops and says why.
func verifyEntry(ctx context.Context, hasher Hasher, directory string, entry *ChecksumEntry) error {
	resolved, err := hasher.Resolve(filepath.Join(directory, filepath.FromSlash(entry.Name)))
	if err != nil {
		return err
	}
	entry.Path = resolved

	item, err := hasher.Stat(resolved)
	if err != nil {
		if errors.Classify(err).Code == errors.CodeNotFound {
			entry.Status = StatusMissing
			return nil
		}
		return err
	}
	if item.IsDir {
		return errors.New(errors.CodeInvalidInput, "%s is a directory", resolved).
			WithHint("the checksum file names a directory, which cannot be hashed")
	}

	checksums, err := hasher.Digest(ctx, resolved, []fs.Algorithm{fs.Algorithm(entry.Algorithm)})
	if err != nil {
		return err
	}

	entry.Actual = checksums[0].Value
	entry.Bytes = item.Bytes
	entry.Status = StatusMismatch
	if subtle.ConstantTimeCompare([]byte(entry.Actual), []byte(entry.Expected)) == 1 {
		entry.Status = StatusMatch
	}
	return nil
}

// parseChecksumFile reads the format every *sum tool writes: a digest, a
// separator, and a file name, one per line. Blank lines and comments are
// skipped; anything else that is not an entry is an error, because a line
// silently ignored is a file silently unchecked.
func parseChecksumFile(contents string, algorithm fs.Algorithm) ([]ChecksumEntry, error) {
	var entries []ChecksumEntry

	for index, line := range strings.Split(contents, "\n") {
		number := index + 1
		text := strings.TrimSpace(line)
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}

		split := strings.IndexFunc(text, unicode.IsSpace)
		if split < 0 {
			return nil, errors.New(errors.CodeInvalidInput,
				"line %d is not a digest and a file name: %q", number, text).
				WithHint("each line is a digest, whitespace, then the file it belongs to")
		}

		// The separator is two characters as *sum writes it: whitespace and a
		// marker for binary or text mode. Both vary in the wild.
		name := strings.TrimPrefix(strings.TrimLeft(text[split:], " \t"), "*")
		if err := checkEntryName(name, number); err != nil {
			return nil, err
		}

		expected, detected, err := parseExpected(text[:split], algorithm)
		if err != nil {
			return nil, errors.Wrap(err, errors.CodeInvalidInput, "line %d of the checksum file", number)
		}

		entries = append(entries, ChecksumEntry{
			Name:      name,
			Algorithm: string(detected),
			Expected:  expected,
		})
	}

	return entries, nil
}

// checkEntryName refuses a name that would take the check outside the
// directory the checksum file lives in.
//
// A checksum file is downloaded from the same page as the thing it describes,
// so it is exactly as trustworthy as the download it is meant to vouch for.
// This command only reads and hashes, but a list that can name any path on the
// machine is a list that can be used to ask whether a particular file exists,
// and there is no reason to allow it.
func checkEntryName(name string, number int) error {
	if name == "" {
		return errors.New(errors.CodeInvalidInput, "line %d has a digest but no file name", number)
	}

	slashed := filepath.ToSlash(name)
	if filepath.IsAbs(name) || strings.HasPrefix(slashed, "/") || strings.Contains(name, `\`) {
		return errors.New(errors.CodeInvalidInput,
			"line %d names a path that is not relative to the checksum file: %q", number, name).
			WithHint("entries name a file beside the checksum file, with forward slashes")
	}
	for _, segment := range strings.Split(slashed, "/") {
		if segment == ".." {
			return errors.New(errors.CodeInvalidInput,
				"line %d points outside the checksum file's directory: %q", number, name).
				WithHint("entries are read relative to the checksum file's own directory")
		}
	}
	return nil
}

// selectEntries narrows the list to the names the user asked about.
//
// A name that is not in the checksum file is an error rather than an empty
// result. Someone naming a file explicitly is asking whether that file is
// good, and answering with silence would read as a pass.
func selectEntries(entries []ChecksumEntry, only []string) ([]ChecksumEntry, error) {
	if len(only) == 0 {
		return entries, nil
	}

	wanted := make(map[string]bool, len(only))
	for _, name := range only {
		wanted[entryKey(name)] = true
	}

	selected := make([]ChecksumEntry, 0, len(only))
	found := make(map[string]bool, len(only))
	for _, entry := range entries {
		if wanted[entryKey(entry.Name)] {
			found[entryKey(entry.Name)] = true
			selected = append(selected, entry)
		}
	}

	for _, name := range only {
		if !found[entryKey(name)] {
			return nil, errors.New(errors.CodeNotFound,
				"%s is not listed in the checksum file", name).
				WithHint("run without naming a file to see everything the checksum file covers")
		}
	}
	return selected, nil
}

// entryKey compares by file name alone, so "dist/devnest.zip" on the command
// line still finds "devnest.zip" in the checksum file.
func entryKey(name string) string {
	return path.Base(filepath.ToSlash(strings.TrimSpace(name)))
}

// parseExpected validates the digest and works out which algorithm produced
// it.
//
// Every supported algorithm has a distinct output length, so a user who pasted
// a checksum from a release page does not have to know or say which kind it
// is. When they do say, and it disagrees with the length, that is a mistake
// worth reporting rather than quietly overriding.
func parseExpected(expected string, algorithm fs.Algorithm) (string, fs.Algorithm, error) {
	normalised := strings.ToLower(strings.TrimSpace(expected))
	if normalised == "" {
		return "", "", errors.New(errors.CodeInvalidInput, "no expected digest was given").
			WithHint("pass the digest published alongside the file")
	}

	// Checksum files often carry a leading marker for binary or text mode.
	normalised = strings.TrimPrefix(normalised, "*")

	if _, err := hex.DecodeString(normalised); err != nil {
		return "", "", errors.New(errors.CodeInvalidInput,
			"%q is not a hexadecimal digest", expected).
			WithHint("a digest is hex characters only, with no spaces or prefix")
	}

	detected, known := fs.AlgorithmForLength(len(normalised))
	if !known {
		return "", "", errors.New(errors.CodeInvalidInput,
			"a digest of %d characters does not match any supported algorithm",
			len(normalised)).
			WithHint("expected %d characters for md5, %d for sha256, or %d for sha512",
				fs.DigestLength(fs.MD5), fs.DigestLength(fs.SHA256), fs.DigestLength(fs.SHA512))
	}

	if algorithm != "" && algorithm != detected {
		return "", "", errors.New(errors.CodeInvalidInput,
			"the digest is %d characters, which is %s, but --algorithm says %s",
			len(normalised), detected, algorithm).
			WithHint("leave --algorithm off and it will be worked out from the digest")
	}

	return normalised, detected, nil
}
