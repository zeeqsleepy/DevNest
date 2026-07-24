package scan

import (
	"bufio"
	"bytes"
	"context"
	"sort"
	"time"

	"github.com/devnest/devnest/internal/classify"
	"github.com/devnest/devnest/internal/platform/fs"
)

// LinesRequest describes one line count.
type LinesRequest struct {
	Selection
	// MaxFileBytes skips files larger than this. Zero means the default.
	// A minified bundle or a checked-in dump is not source, and reading it
	// costs more than every real file in the tree put together.
	MaxFileBytes int64
	// Limit caps the language listing. Zero means every language.
	Limit int
}

// LanguageLines is one language's share of the counting.
type LanguageLines struct {
	Language string `json:"language"`
	Files    int    `json:"files"`
	Total    int    `json:"total"`
	Code     int    `json:"code"`
	Comment  int    `json:"comment"`
	Blank    int    `json:"blank"`
	Bytes    int64  `json:"bytes"`
}

// LinesResult is the count.
type LinesResult struct {
	Root      string          `json:"root"`
	Files     int             `json:"files"`
	Total     int             `json:"total"`
	Code      int             `json:"code"`
	Comment   int             `json:"comment"`
	Blank     int             `json:"blank"`
	Bytes     int64           `json:"bytes"`
	Languages []LanguageLines `json:"languages"`
	// Skipped counts files left out: too large, unreadable, or binary.
	Skipped    int       `json:"skipped"`
	Problems   []Problem `json:"problems"`
	DurationMs int64     `json:"durationMs"`
}

const (
	// defaultMaxFileBytes is the size past which a file is assumed not to be
	// something a person wrote.
	defaultMaxFileBytes = 4 * 1024 * 1024
	// readBuffer is the read size for counting. Lines are short; the buffer
	// is sized so that a syscall covers a whole small file.
	readBuffer = 64 * 1024
	// binaryProbe is how much of a file is checked for NUL bytes before
	// deciding it is not text.
	binaryProbe = 8 * 1024
)

// Lines counts lines, split into code, comment, and blank.
//
// Only files in a recognised language are opened. Everything else is counted
// as a file and skipped, because "lines of PNG" is not a number and reading
// every binary in a tree to find that out is the slowest possible way to learn
// nothing.
//
// Each file is streamed through a reused buffer, so memory does not follow the
// size of the tree or of any file in it.
func Lines(ctx context.Context, inspector Inspector, request LinesRequest) (LinesResult, error) {
	started := time.Now()

	walk, err := prepare(ctx, inspector, request.Selection)
	if err != nil {
		return LinesResult{}, err
	}

	limit := request.MaxFileBytes
	if limit <= 0 {
		limit = defaultMaxFileBytes
	}

	counter := newLineCounter()
	perLanguage := make(map[string]*LanguageLines)
	result := LinesResult{Root: walk.root}

	err = inspector.Walk(ctx, walk.options, func(entry fs.Entry) error {
		language, known := classify.LanguageOf(entry.Name)
		if !known {
			return nil
		}

		result.Files++
		if entry.Bytes > limit {
			result.Skipped++
			return nil
		}

		counted, err := counter.count(ctx, inspector, entry.Path, language)
		if err != nil {
			result.Skipped++
			walk.problems = append(walk.problems, Problem{
				Path:   walk.relative(entry.Path),
				Reason: err.Error(),
			})
			return nil
		}
		if counted.binary {
			result.Skipped++
			return nil
		}

		totals, seen := perLanguage[language.Name]
		if !seen {
			totals = &LanguageLines{Language: language.Name}
			perLanguage[language.Name] = totals
		}
		totals.Files++
		totals.Total += counted.total
		totals.Code += counted.code
		totals.Comment += counted.comment
		totals.Blank += counted.blank
		totals.Bytes += entry.Bytes

		result.Total += counted.total
		result.Code += counted.code
		result.Comment += counted.comment
		result.Blank += counted.blank
		result.Bytes += entry.Bytes
		return nil
	})
	if err != nil {
		return LinesResult{}, err
	}

	result.Languages = rankLanguages(perLanguage, request.Limit)
	result.Problems = walk.collected()
	result.DurationMs = time.Since(started).Milliseconds()
	return result, nil
}

func rankLanguages(totals map[string]*LanguageLines, limit int) []LanguageLines {
	languages := make([]LanguageLines, 0, len(totals))
	for _, language := range totals {
		languages = append(languages, *language)
	}

	sort.Slice(languages, func(i, j int) bool {
		if languages[i].Code != languages[j].Code {
			return languages[i].Code > languages[j].Code
		}
		return languages[i].Language < languages[j].Language
	})

	if limit > 0 && len(languages) > limit {
		languages = languages[:limit]
	}
	return languages
}

// counted is one file's numbers.
type counted struct {
	total   int
	code    int
	comment int
	blank   int
	binary  bool
}

// lineCounter reads files through buffers it reuses across the whole walk.
type lineCounter struct {
	reader *bufio.Reader
}

func newLineCounter() *lineCounter {
	return &lineCounter{reader: bufio.NewReaderSize(nil, readBuffer)}
}

// count classifies every line of one file.
//
// The comment detection is deliberately simple: a line whose first non-space
// characters open a comment is a comment, and a block comment runs until its
// terminator. It does not parse the language, so a comment marker inside a
// string literal is counted as a comment.
//
// That is the right trade for this command. Parsing forty languages to get
// the last percent right means forty parsers to maintain, and the number this
// produces is used to compare files and directories with each other, where a
// consistent small error cancels out.
func (l *lineCounter) count(ctx context.Context, inspector Inspector, path string, language classify.Language) (counted, error) {
	file, err := inspector.Open(path)
	if err != nil {
		return counted{}, err
	}
	defer func() {
		// Opened for reading only, so a failed close cannot lose anything.
		_ = file.Close()
	}()

	l.reader.Reset(file)

	if head, err := l.reader.Peek(binaryProbe); err == nil || len(head) > 0 {
		if bytes.IndexByte(head, 0) >= 0 {
			return counted{binary: true}, nil
		}
	}

	var result counted
	inBlock := false
	var closing string

	for {
		if result.total%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return counted{}, err
			}
		}

		line, err := l.reader.ReadSlice('\n')
		if len(line) == 0 && err != nil {
			break
		}
		result.total++

		trimmed := bytes.TrimSpace(line)
		switch {
		case inBlock:
			result.comment++
			if index := bytes.Index(trimmed, []byte(closing)); index >= 0 {
				inBlock = false
				// Code after a block comment ends on the same line still
				// counts as code, which is what makes a closing brace on a
				// documentation line count correctly.
				if rest := bytes.TrimSpace(trimmed[index+len(closing):]); len(rest) > 0 {
					result.comment--
					result.code++
				}
			}

		case len(trimmed) == 0:
			result.blank++

		case startsComment(trimmed, language.Line):
			result.comment++

		default:
			opened, terminator := opensBlock(trimmed, language.Block)
			if !opened {
				result.code++
				break
			}
			result.comment++
			if !bytes.Contains(trimmed, []byte(terminator)) {
				inBlock = true
				closing = terminator
			}
		}

		if err != nil {
			break
		}
	}

	return result, nil
}

// startsComment reports whether a line begins with a line-comment token.
func startsComment(line []byte, tokens []string) bool {
	for _, token := range tokens {
		if bytes.HasPrefix(line, []byte(token)) {
			return true
		}
	}
	return false
}

// opensBlock reports whether a line begins a block comment, and what would
// close it.
func opensBlock(line []byte, pairs [][2]string) (bool, string) {
	for _, pair := range pairs {
		if bytes.HasPrefix(line, []byte(pair[0])) {
			return true, pair[1]
		}
	}
	return false, ""
}
