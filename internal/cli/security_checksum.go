package cli

import (
	"context"
	"flag"
	"io"
	"strings"

	"github.com/devnest/devnest/internal/core/security"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
	"github.com/devnest/devnest/internal/platform/fs"
)

func newSecurityChecksumCommand() *Command {
	var algorithm string

	return &Command{
		Name:    "checksum",
		Summary: "Verify a file against a published digest",
		Usage:   "devnest security checksum <file> <hash> [flags]",
		Description: "Check that a file matches the digest published alongside it.\n\n" +
			"The algorithm is worked out from the length of the digest you paste: 32 " +
			"characters is MD5, 64 is SHA-256, 128 is SHA-512, so there is nothing to " +
			"remember. Pass --algorithm to state it explicitly; a disagreement with the " +
			"length is reported rather than quietly resolved.\n\n" +
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
		},
		SetFlags: func(set *flag.FlagSet) {
			set.StringVar(&algorithm, "algorithm", "",
				"state the algorithm instead of inferring it: "+strings.Join(algorithmNames(), ", "))
			set.StringVar(&algorithm, "a", "", "state the algorithm (shorthand)")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			if len(args) != 2 {
				return errors.New(errors.CodeInvalidInput,
					"expected a file and a digest, found %d argument(s)", len(args)).
					WithHint("run \"devnest security checksum --help\" for usage")
			}

			var chosen fs.Algorithm
			if algorithm != "" {
				parsed, err := fs.ParseAlgorithm(algorithm)
				if err != nil {
					return err
				}
				chosen = parsed
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
