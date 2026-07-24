package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/devnest/devnest/internal/core/file"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
	"github.com/devnest/devnest/internal/platform/fs"
)

func newFileHashCommand() *Command {
	var (
		algorithms repeatable
		all        bool
	)

	return &Command{
		Name:    "hash",
		Summary: "Compute checksums for one or more files",
		Usage:   "devnest file hash <file...> [flags]",
		Description: "Compute a checksum for one or more files. SHA-256 by default; " +
			"SHA-512 and MD5 are also available.\n\n" +
			"Asking for several algorithms reads each file once and feeds every digest " +
			"from that single pass, so three checksums of a large file cost one read " +
			"rather than three. Files are streamed through a fixed buffer, so memory " +
			"use does not depend on file size.",
		Examples: []Example{
			{
				Command:     "devnest file hash installer.exe",
				Description: "Compute the SHA-256 of a downloaded file.",
			},
			{
				Command:     "devnest file hash installer.exe --algorithm md5 --algorithm sha256",
				Description: "Compute two digests in a single read of the file.",
			},
			{
				Command:     "devnest file hash *.iso --all --output json",
				Description: "Compute every supported digest for several files, as JSON.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.Var(&algorithms, "algorithm", "digest to compute: sha256, sha512, or md5 (repeatable)")
			set.Var(&algorithms, "a", "digest to compute (shorthand, repeatable)")
			set.BoolVar(&all, "all", false, "compute every supported digest in one pass")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			if len(args) == 0 {
				return errors.New(errors.CodeInvalidInput, "no file was given").
					WithHint("run \"devnest file hash --help\" for usage")
			}

			chosen, err := chooseAlgorithms(algorithms, all)
			if err != nil {
				return err
			}

			result, err := file.Hash(ctx, filesystem(), file.HashRequest{
				Paths:      args,
				Algorithms: chosen,
			})
			if err != nil {
				return err
			}

			reportProblems(env, result.Problems)
			return env.Emit(result, hashText(result))
		},
	}
}

func chooseAlgorithms(names []string, all bool) ([]fs.Algorithm, error) {
	if all {
		return fs.Algorithms(), nil
	}
	if len(names) == 0 {
		return []fs.Algorithm{fs.SHA256}, nil
	}

	// Duplicates are dropped rather than rejected: asking for sha256 twice is
	// harmless, and failing on it would be pedantry.
	seen := make(map[fs.Algorithm]bool, len(names))
	chosen := make([]fs.Algorithm, 0, len(names))

	for _, name := range names {
		algorithm, err := fs.ParseAlgorithm(name)
		if err != nil {
			return nil, err
		}
		if !seen[algorithm] {
			seen[algorithm] = true
			chosen = append(chosen, algorithm)
		}
	}
	return chosen, nil
}

func hashText(result file.HashResult) output.TextFunc {
	return func(w io.Writer) error {
		// One file with one digest is the common case, and a table for a
		// single value is noise. Print the digest on its own so it can be
		// copied or piped straight into a comparison.
		if len(result.Files) == 1 && len(result.Files[0].Checksums) == 1 {
			fmt.Fprintf(w, "%s  %s\n",
				result.Files[0].Checksums[0].Value, result.Files[0].Name)
			return writeProblems(w, result.Problems)
		}

		rows := make([][]string, 0, len(result.Files))
		for _, item := range result.Files {
			for _, checksum := range item.Checksums {
				rows = append(rows, []string{
					item.Name,
					checksum.Algorithm,
					checksum.Value,
					output.Bytes(item.Bytes),
				})
			}
		}

		err := output.WriteTable(w, []output.Column{
			{Title: "file"},
			{Title: "algorithm"},
			{Title: "hash"},
			{Title: "size", Right: true},
		}, rows)
		if err != nil {
			return err
		}

		return writeProblems(w, result.Problems)
	}
}
