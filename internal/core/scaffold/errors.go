package scaffold

import (
	"github.com/devnest/devnest/internal/errors"
)

func emptyTarget() error {
	return errors.New(errors.CodeInvalidInput, "no target directory was given").
		WithHint("pass the directory to create, for example: devnest init my-project")
}

func notEmpty(target string) error {
	return errors.New(errors.CodeConflict,
		"%s already contains files", target).
		WithHint("a template is the start of a project; make a new, empty directory")
}

func unknownTemplate(name string, err error) error {
	return errors.New(errors.CodeNotFound,
		"unknown template %q", name).
		WithHint("run \"devnest init --list\" to see the available templates")
}
