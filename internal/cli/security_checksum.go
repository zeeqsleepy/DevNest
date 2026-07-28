package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/devnest/devnest/internal/core/security"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
	"github.com/devnest/devnest/internal/platform/fs"
)

func newSecurityChecksumCommand() *Command {
	var (
		algorithm string
		check     string
	)

	return &Command{
		Name:    "checksum",
		Summary: "Verify a file against a published digest",
		Usage:   "devnest security checksum <file> <hash> [flags]",
		Description: "Check that a file matches the digest published alongside it.\n\n" +
			"The algorithm is worked out from the length of the digest you paste: 32 " +
			"characters is MD5, 64 is SHA-256, 128 is SHA-512, so there is nothing to " +
			"remember. Pass --algorithm to state it explicitly; a disagreement with the " +
			"length is reported rather than quietly resolved.\n\n" +
			"--check takes a whole checksum file instead of one pasted digest: the " +
			"SHA256SUMS a release publishes, in the format every *sum tool writes. Names " +
			"in it are read relative to the file itself, so verifying a directory of " +
			"downloads is one command. Name files after the flag to check only those.\n\n" +
			"A file listed in the checksum file but not present is reported as missing " +
			"rather than failed: a release publishes digests for every platform it built, " +
			"and you downloaded one of them. A file that is present and wrong is still a " +
			"mismatch.\n\n" +
			"A mismatch is a result, not a malfunction: finding out that a download does " +
			"not match its checksum is the whole point. The command exits non-zero so a " +
			"script can act on it, and prints both digests either way, because a " +
			"verification tool that prints only \"ok\" gives you nothing to check when " +
			"you start doubting the tool.",
		Examples: []Example{
			{
				Command:     "devnest security checksum devnest.zip 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
				Description: "Verify a download against the SHA-256 from the release page.",
			},
			{
				Command:     "devnest security checksum installer.msi d41d8cd98f00b204e9800998ecf8427e",
				Description: "Verify against an MD5, recognised from its length.",
			},
			{
				Command:     "devnest security checksum --check SHA256SUMS",
				Description: "Verify every downloaded file a checksum file covers.",
			},
			{
				Command:     "devnest security checksum --check checksums.txt devnest_windows_amd64.zip",
				Description: "Verify only the one artefact you actually downloaded.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.StringVar(&algorithm, "algorithm", "",
				"state the algorithm instead of inferring it: "+strings.Join(algorithmNames(), ", "))
			set.StringVar(&algorithm, "a", "", "state the algorithm (shorthand)")
			set.StringVar(&check, "check", "",
				"verify against a checksum file such as SHA256SUMS, rather than one digest")
			set.StringVar(&check, "c", "", "verify against a checksum file (shorthand)")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			if check != "" {
				chosen, err := chosenAlgorithm(algorithm)
				if err != nil {
					return err
				}
				return runChecksumFile(ctx, env, check, args, chosen)
			}

			if len(args) != 2 {
				return errors.New(errors.CodeInvalidInput,
					"expected a file and a digest, found %d argument(s)", len(args)).
					WithHint("run \"devnest security checksum --help\" for usage")
			}

			chosen, err := chosenAlgorithm(algorithm)
			if err != nil {
				return err
			}

			result, err := security.VerifyChecksum(ctx, filesystem(), security.ChecksumRequest{
				Path:      args[0],
				Expected:  args[1],
				Algorithm: chosen,
			})
			if err != nil {
				return err
			}

			if err := env.Emit(result, checksumText(result)); err != nil {
				return err
			}

			if !result.Match {
				return errors.New(errors.CodeCheckFailed,
					"%s does not match the expected digest", result.Path).
					WithHint("the file may be incomplete, corrupted, or not the one published")
			}
			return nil
		},
	}
}

func chosenAlgorithm(name string) (fs.Algorithm, error) {
	if name == "" {
		return "", nil
	}
	return fs.ParseAlgorithm(name)
}

// runChecksumFile verifies everything a checksum file covers, or the files
// named after the flag.
func runChecksumFile(
	ctx context.Context,
	env *Env,
	path string,
	only []string,
	algorithm fs.Algorithm,
) error {
	result, err := security.VerifyChecksumFile(ctx, filesystem(), security.ChecksumFileRequest{
		Path:      path,
		Only:      only,
		Algorithm: algorithm,
	})
	if err != nil {
		return err
	}

	err = env.EmitTable(result, checksumFileText(result), checksumFileTable(result))
	if err != nil {
		return err
	}

	switch {
	case result.Mismatched > 0:
		return errors.New(errors.CodeCheckFailed,
			"%d of the %d files checked do not match", result.Mismatched,
			result.Matched+result.Mismatched).
			WithHint("a file may be incomplete, corrupted, or not the one published")
	case result.Matched == 0:
		// Every entry was missing, so nothing was verified. Reporting that as
		// a pass would be the one answer this command must never give.
		return errors.New(errors.CodeNotFound,
			"none of the %d files listed in %s were found", len(result.Entries), result.Source).
			WithHint("run this where the downloads are, or beside the checksum file")
	}
	return nil
}

func checksumFileText(result security.ChecksumFileResult) output.TextFunc {
	return func(w io.Writer) error {
		rows := checksumFileRows(result)
		if err := output.WriteTable(w, checksumFileColumns(), rows); err != nil {
			return err
		}

		fmt.Fprintf(w, "\n%d matched, %d did not, %d not found\n",
			result.Matched, result.Mismatched, result.Missing)
		return nil
	}
}

func checksumFileTable(result security.ChecksumFileResult) output.TableFunc {
	return func() output.Table {
		return output.Table{Columns: checksumFileColumns(), Rows: checksumFileRows(result)}
	}
}

func checksumFileColumns() []output.Column {
	return []output.Column{
		{Title: "file"},
		{Title: "algorithm"},
		{Title: "result"},
		{Title: "size", Right: true},
	}
}

func checksumFileRows(result security.ChecksumFileResult) [][]string {
	rows := make([][]string, 0, len(result.Entries))
	for _, entry := range result.Entries {
		size := ""
		if entry.Status != security.StatusMissing {
			size = output.Bytes(entry.Bytes)
		}
		rows = append(rows, []string{entry.Name, entry.Algorithm, entry.Status, size})
	}
	return rows
}

func checksumText(result security.ChecksumResult) output.TextFunc {
	return func(w io.Writer) error {
		verdict := "no match"
		if result.Match {
			verdict = "match"
		}

		return output.WriteFields(w, []output.Field{
			{Label: "file", Value: result.Path},
			{Label: "size", Value: output.Bytes(result.Bytes)},
			{Label: "algorithm", Value: result.Algorithm},
			{Label: "expected", Value: result.Expected},
			{Label: "actual", Value: result.Actual},
			{Label: "result", Value: verdict},
		})
	}
}
