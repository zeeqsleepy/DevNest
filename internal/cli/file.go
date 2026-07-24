package cli

import (
	"flag"
	"strings"

	"github.com/devnest/devnest/internal/core/file"
	"github.com/devnest/devnest/internal/platform/fs"
)

// newFileCommand builds the "file" group.
func newFileCommand() *Command {
	return &Command{
		Name:    "file",
		Summary: "Organise, inspect, and analyse files",
		Usage:   "devnest file <command> [path] [flags]",
		Description: "Work with files and directories: group them into folders, " +
			"find duplicates by content, rename in batches, search by extension, " +
			"see where the space went, and compute checksums.\n\n" +
			"Nothing here ever deletes a file, and nothing that changes the disk " +
			"does so without --apply.",
		Commands: []*Command{
			newFileOrganizeCommand(),
			newFileDuplicateCommand(),
			newFileRenameCommand(),
			newFileFilterCommand(),
			newFileSizeCommand(),
			newFileHashCommand(),
		},
	}
}

// selectionFlags registers the walk options shared by every file command, so
// that --recursive means the same thing in all of them.
type selectionFlags struct {
	recursive      bool
	depth          int
	includeHidden  bool
	followSymlinks bool
	exclude        repeatable
}

func (s *selectionFlags) register(set *flag.FlagSet, recursiveByDefault bool) {
	s.registerCommon(set, recursiveByDefault)
	set.IntVar(&s.depth, "depth", 0, "limit how deep to descend (0 means unlimited)")
}

// registerWithoutDepth is for "size", which always measures the whole tree and
// uses --depth for how much of the result to report. Registering both would be
// two flags with one name, so the command that owns the name registers it.
func (s *selectionFlags) registerWithoutDepth(set *flag.FlagSet, recursiveByDefault bool) {
	s.registerCommon(set, recursiveByDefault)
}

func (s *selectionFlags) registerCommon(set *flag.FlagSet, recursiveByDefault bool) {
	set.BoolVar(&s.recursive, "recursive", recursiveByDefault, "descend into subdirectories")
	set.BoolVar(&s.recursive, "r", recursiveByDefault, "descend into subdirectories (shorthand)")
	set.BoolVar(&s.includeHidden, "include-hidden", false, "include hidden files")
	set.BoolVar(&s.followSymlinks, "follow-symlinks", false, "descend into symlinked directories")
	set.Var(&s.exclude, "exclude", "skip entries matching a glob (repeatable)")
}

// selection turns the flags into a request, folding in the excludes the user
// configured. Configuration is read here rather than inside the module,
// because a module never reads configuration.
func (s *selectionFlags) selection(env *Env, root string) file.Selection {
	exclude := append([]string(nil), env.Config.Scan.Exclude...)
	exclude = append(exclude, s.exclude...)

	return file.Selection{
		Root:           root,
		Recursive:      s.recursive,
		MaxDepth:       s.depth,
		IncludeHidden:  s.includeHidden,
		FollowSymlinks: s.followSymlinks || env.Config.Scan.FollowSymlinks,
		Exclude:        exclude,
	}
}

// repeatable collects a flag that may be given more than once.
type repeatable []string

func (r *repeatable) String() string { return strings.Join(*r, ",") }

func (r *repeatable) Set(value string) error {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		*r = append(*r, trimmed)
	}
	return nil
}

// firstPath returns the positional path argument, defaulting to the current
// directory. Commands that operate on a project mean "here" nine times out of
// ten.
func firstPath(args []string) string {
	if len(args) == 0 {
		return "."
	}
	return args[0]
}

// filesystem is the real filesystem, which is what every command gets in
// production. Tests call the module directly with a fake.
func filesystem() fs.System { return fs.System{} }
