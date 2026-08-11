// Package scaffold builds a new project from a committed template.
//
// # Templates are embedded, so they travel with the binary
//
// Running `devnest init` on a machine means the template has to be there. The
// templates live in `templates/` beneath this package and are embedded into
// the binary with go:embed, so an installation downloaded from a release page
// carries them exactly as a build from source does. A template is any
// directory under `templates/` whose files do not start with underscore; those
// are DevNest's own and are not copied.
//
// # A scaffold never overwrites
//
// The whole point of a template is the first commit. Writing over a directory
// that already has files would destroy somebody's work, so a target directory
// that does not exist is created and one that is not empty is refused. There
// is no `--force`: the safe action is to make a new directory and tell the
// person how, and a scaffold that offers a flag to clobber is a scaffold that
// will be used that way once.
package scaffold

import (
	"context"
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed templates
var templateFS embed.FS

// Request describes one scaffold.
type Request struct {
	// Template is the directory under templates/ to copy.
	Template string
	// Target is the directory to create the project in.
	Target string
}

// Result is what was built.
type Result struct {
	Template string   `json:"template"`
	Target   string   `json:"target"`
	Files    []string `json:"files"`
}

// Templates returns the scaffoldable templates, alphabetically, from the
// embedded filesystem.
func Templates() []string {
	entries, err := fs.ReadDir(templateFS, "templates")
	if err != nil {
		return nil
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names
}

// Create builds a project from a template into a target directory.
//
// The target must not already exist, or must be an empty directory. Both are
// the same refusal written as one error: a template is the start of a project,
// and starting over an existing tree is never the right answer.
func Create(ctx context.Context, request Request) (Result, error) {
	if strings.TrimSpace(request.Template) == "" {
		request.Template = "blank"
	}
	if strings.TrimSpace(request.Target) == "" {
		return Result{}, emptyTarget()
	}

	root := filepath.Clean(request.Target)
	if err := refuseOverwrite(root); err != nil {
		return Result{}, err
	}

	templateRoot := "templates/" + request.Template
	if info, err := fs.Stat(templateFS, templateRoot); err != nil || !info.IsDir() {
		return Result{}, unknownTemplate(request.Template, err)
	}

	var files []string
	err := fs.WalkDir(templateFS, templateRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}

		relative := strings.TrimPrefix(path, templateRoot+"/")
		if strings.HasPrefix(filepath.Base(relative), "_") {
			return nil
		}

		if err := ctx.Err(); err != nil {
			return err
		}

		data, err := fs.ReadFile(templateFS, path)
		if err != nil {
			return err
		}

		// A .tpl suffix marks a file whose name must not reach the module as it
		// is: a template carrying a go.mod would otherwise make the scaffolding
		// package look like a nested module to the Go toolchain, which stops an
		// embed of the whole tree. The suffix is dropped so the copied file has
		// the real name.
		if strings.HasSuffix(relative, ".tpl") {
			relative = strings.TrimSuffix(relative, ".tpl")
		}

		destination := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(destination, data, 0o644); err != nil {
			return err
		}
		files = append(files, relative)
		return nil
	})
	if err != nil {
		return Result{}, err
	}

	sort.Strings(files)
	return Result{
		Template: request.Template,
		Target:   request.Target,
		Files:    files,
	}, nil
}

// refuseOverwrite refuses to write into a directory that already holds files.
func refuseOverwrite(target string) error {
	info, err := os.Stat(target)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return notEmpty(target)
	}

	entries, err := os.ReadDir(target)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return notEmpty(target)
	}
	return nil
}
