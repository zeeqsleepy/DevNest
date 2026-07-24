package scan

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/devnest/devnest/internal/platform/fs"
)

// TreeRequest describes one directory tree listing.
type TreeRequest struct {
	Selection
	// Depth limits how many levels are shown. Zero means the default, not
	// unlimited: printing every level of a large repository produces
	// thousands of lines, and nobody who asked for a tree wanted that.
	Depth int
	// Files includes files as well as directories.
	Files bool
	// MaxEntries caps how many children one directory lists. Zero means the
	// default.
	MaxEntries int
}

// Node is one directory or file in the tree.
//
// A recursive plain struct, which serialises to nested JSON exactly as it
// reads on screen.
type Node struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"isDirectory"`
	// Files and Bytes are the totals for this subtree, so a directory that
	// is not expanded still says how much is under it.
	Files int    `json:"files"`
	Bytes int64  `json:"bytes"`
	Depth int    `json:"depth"`
	Nodes []Node `json:"nodes,omitempty"`
	// Truncated says this directory holds more children than are listed
	// here, so a short listing is never mistaken for a small directory.
	Truncated bool `json:"truncated,omitempty"`
}

// TreeResult is the listing.
type TreeResult struct {
	Root        string `json:"root"`
	Depth       int    `json:"depth"`
	Files       int    `json:"files"`
	Directories int    `json:"directories"`
	Bytes       int64  `json:"bytes"`
	Nodes       []Node `json:"nodes"`
	// Truncated says the root itself holds more entries than are listed.
	Truncated  bool      `json:"truncated,omitempty"`
	Problems   []Problem `json:"problems"`
	DurationMs int64     `json:"durationMs"`
}

const (
	// defaultTreeDepth is how deep a tree goes when nobody says.
	defaultTreeDepth = 3
	// defaultMaxEntries caps one directory's children. A node_modules that
	// slipped through has eight hundred; three screens of them is not a
	// tree, it is a listing.
	defaultMaxEntries = 100
)

// Tree lists the shape of a directory.
//
// The whole tree is walked whatever the depth: a directory reports the file
// count and size of everything under it, including the levels not shown, so
// the numbers beside a collapsed directory are the real ones.
func Tree(ctx context.Context, inspector Inspector, request TreeRequest) (TreeResult, error) {
	started := time.Now()

	depth := request.Depth
	if depth < 1 {
		depth = defaultTreeDepth
	}
	maxEntries := request.MaxEntries
	if maxEntries < 1 {
		maxEntries = defaultMaxEntries
	}

	// The walk is not depth-limited even when the display is. Totals for a
	// directory have to cover what is inside it, and a walk that stopped at
	// the display depth would report a directory holding nothing.
	selection := request.Selection
	selection.MaxDepth = 0

	walk, err := prepare(ctx, inspector, selection)
	if err != nil {
		return TreeResult{}, err
	}

	builder := &treeBuilder{
		root:       walk.root,
		depth:      depth,
		maxEntries: maxEntries,
		files:      request.Files,
		nodes:      map[string]*Node{"": {Name: filepath.Base(walk.root), Path: "", IsDir: true}},
	}

	options := walk.options
	options.IncludeDirs = true

	result := TreeResult{Root: walk.root, Depth: depth}
	err = inspector.Walk(ctx, options, func(entry fs.Entry) error {
		relative := walk.relative(entry.Path)
		if entry.IsDir {
			result.Directories++
			builder.addDirectory(relative)
			return nil
		}

		result.Files++
		result.Bytes += entry.Bytes
		builder.addFile(relative, entry.Bytes)
		return nil
	})
	if err != nil {
		return TreeResult{}, err
	}

	result.Nodes, result.Truncated = builder.assemble()
	result.Problems = walk.collected()
	result.DurationMs = time.Since(started).Milliseconds()
	return result, nil
}

// treeBuilder assembles nodes as the walk reports them.
//
// The walk arrives in sorted order, parents before children, so a directory's
// node always exists by the time anything inside it does. Totals are pushed up
// to every ancestor as each file arrives, which is what makes a collapsed
// directory report the truth.
type treeBuilder struct {
	root       string
	depth      int
	maxEntries int
	files      bool
	nodes      map[string]*Node
	children   map[string][]string
}

func (t *treeBuilder) addDirectory(relative string) {
	if relative == "." || relative == "" {
		return
	}
	t.ensure(relative, true, 0)
}

func (t *treeBuilder) addFile(relative string, size int64) {
	if t.files {
		t.ensure(relative, false, size)
	}
	for _, ancestor := range ancestors(relative) {
		if node, seen := t.nodes[ancestor]; seen {
			node.Files++
			node.Bytes += size
		}
	}
}

// ensure creates a node and links it to its parent.
func (t *treeBuilder) ensure(relative string, isDir bool, size int64) {
	if _, seen := t.nodes[relative]; seen {
		return
	}
	if t.children == nil {
		t.children = make(map[string][]string)
	}

	parent := parentOf(relative)
	node := &Node{
		Name:  filepath.Base(relative),
		Path:  relative,
		IsDir: isDir,
		Bytes: size,
		Depth: strings.Count(relative, "/") + 1,
	}
	if !isDir {
		node.Files = 1
	}

	t.nodes[relative] = node
	t.children[parent] = append(t.children[parent], relative)
}

// assemble turns the flat node map into the nested listing, cutting it at the
// display depth and at the per-directory cap.
func (t *treeBuilder) assemble() ([]Node, bool) {
	return t.build("", 1)
}

// build returns one directory's children, and whether the listing was cut
// short. The flag belongs to the directory whose children were cut, not to the
// children, so it is returned rather than stamped on each of them.
func (t *treeBuilder) build(parent string, level int) ([]Node, bool) {
	paths := t.children[parent]
	if len(paths) == 0 || level > t.depth {
		return nil, false
	}

	sort.Slice(paths, func(i, j int) bool {
		left, right := t.nodes[paths[i]], t.nodes[paths[j]]
		if left.IsDir != right.IsDir {
			return left.IsDir
		}
		return left.Path < right.Path
	})

	truncated := false
	if len(paths) > t.maxEntries {
		paths = paths[:t.maxEntries]
		truncated = true
	}

	nodes := make([]Node, 0, len(paths))
	for _, path := range paths {
		node := *t.nodes[path]
		node.Nodes, node.Truncated = t.build(path, level+1)
		nodes = append(nodes, node)
	}
	return nodes, truncated
}

// ancestors lists every directory between a path and the root, closest first.
func ancestors(relative string) []string {
	var found []string
	current := parentOf(relative)

	for current != "" {
		found = append(found, current)
		current = parentOf(current)
	}
	return append(found, "")
}

// parentOf returns the containing directory of a slash-separated path, or the
// empty string for something directly under the root.
func parentOf(relative string) string {
	if index := strings.LastIndex(relative, "/"); index > 0 {
		return relative[:index]
	}
	return ""
}
