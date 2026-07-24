package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"time"

	"github.com/devnest/devnest/internal/config"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
	"github.com/devnest/devnest/internal/platform/fs"
)

// export describes where a result should be written in addition to the
// terminal, resolved once when the environment is built.
//
// Exporting never replaces terminal output. Somebody who exports a scan
// usually also wants to watch it, and a command that went quiet because a file
// was requested is a command people run twice.
type export struct {
	path     string
	renderer output.Renderer
}

// newExport resolves --export against the configuration, or returns nothing
// when no file was asked for.
func newExport(settings config.Export, path, format string, now time.Time) (*export, error) {
	if path == "" {
		if format != "" {
			return nil, errors.New(errors.CodeInvalidInput,
				"--export-format has nothing to format without --export").
				WithHint("pass --export <path> as well")
		}
		return nil, nil
	}

	resolved := exportPath(settings, path, now)

	if format == "" {
		var err error
		if format, err = formatForExtension(resolved); err != nil {
			return nil, err
		}
	}
	renderer, err := output.NewRenderer(format)
	if err != nil {
		return nil, err
	}

	return &export{path: resolved, renderer: renderer}, nil
}

// exportPath puts a bare filename in the configured directory and, when
// configured, inserts a timestamp so that repeated runs do not overwrite each
// other. A path with a directory in it is used as given: somebody who typed a
// location meant it.
func exportPath(settings config.Export, path string, now time.Time) string {
	if settings.TimestampFiles {
		extension := filepath.Ext(path)
		path = strings.TrimSuffix(path, extension) + "-" + now.Format("20060102-150405") + extension
	}
	if filepath.Dir(path) == "." && !strings.HasPrefix(path, "."+string(filepath.Separator)) {
		return filepath.Join(settings.Directory, path)
	}
	return path
}

func formatForExtension(path string) (string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return "json", nil
	case ".csv":
		return "csv", nil
	case ".md", ".markdown":
		return "markdown", nil
	case ".txt":
		return "table", nil
	}
	return "", errors.New(errors.CodeInvalidInput,
		"cannot tell the export format from %q", filepath.Base(path)).
		WithHint("use a .json, .csv, .md, or .txt extension, or pass --export-format")
}

// write renders the envelope into memory and puts it on disk in one atomic
// step. A result is a bounded summary, so holding a rendered copy costs
// nothing, and it means a rendering failure cannot leave a half-written file.
func (e *export) write(envelope output.Envelope, text output.TextFunc, table output.TableFunc) error {
	var rendered bytes.Buffer

	rows, ok := e.renderer.(output.RowRenderer)
	switch {
	case ok && table != nil:
		if err := rows.RenderRows(&rendered, envelope, table); err != nil {
			return err
		}
	default:
		if err := e.renderer.Render(&rendered, envelope, text); err != nil {
			return err
		}
	}

	return fs.System{}.WriteAtomic(e.path, rendered.Bytes())
}

// exists reports whether the target is already there, so that overwriting one
// can be warned about before anything is written.
func (e *export) exists() bool {
	found, err := fs.System{}.Exists(e.path)
	return err == nil && found
}
