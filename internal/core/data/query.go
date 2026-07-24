package data

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/devnest/devnest/internal/errors"
)

// QueryRequest selects one value out of a document.
type QueryRequest struct {
	Request
	Expression string
}

// QueryResult is the selected value.
//
// Value is JSON regardless of the format that was read, because a query
// answers a question about structure and JSON is the shape the rest of this
// tool speaks. Object keys inside it come back in sorted order: the value is
// re-encoded from what was parsed, which is the price of selecting a subtree
// rather than reprinting the file. Reprinting the file is what format does,
// and it keeps the original order.
type QueryResult struct {
	Path       string          `json:"path"`
	Expression string          `json:"expression"`
	Kind       string          `json:"kind"`
	Entries    int             `json:"entries"`
	Value      json.RawMessage `json:"value"`
}

// Query selects a value from a JSON document with a path expression.
//
// # The expression
//
// Keys are separated by dots and array elements are selected by index:
//
//	users[0].name
//	.services.api.port
//	["key.with.dots"][2]
//
// A leading dot or $ is optional, so an expression copied out of another tool
// usually works. A key holding a dot, a space, or a bracket goes in quoted
// brackets.
//
// That is the whole syntax. There are no filters, no wildcards, no functions,
// and no arithmetic, and this is a decision rather than a stage of
// implementation: a query language is a product of its own, and the moment
// this one grows a third feature it is a worse jq that nobody has documented.
// Selecting a subtree covers what a person at a terminal actually needs, and
// anything past it is what jq is for.
func Query(reader Reader, request QueryRequest) (QueryResult, error) {
	segments, err := parseExpression(request.Expression)
	if err != nil {
		return QueryResult{}, err
	}

	doc, err := load(reader, request.Request)
	if err != nil {
		return QueryResult{}, err
	}

	value, err := parseJSON(doc)
	if err != nil {
		return QueryResult{}, err
	}

	selected, err := walk(value, segments)
	if err != nil {
		return QueryResult{}, err
	}

	encoded, err := json.Marshal(selected)
	if err != nil {
		return QueryResult{}, errors.Wrap(err, errors.CodeInternal,
			"the selected value cannot be written back as JSON")
	}

	return QueryResult{
		Path:       doc.path,
		Expression: request.Expression,
		Kind:       kindOf(selected),
		Entries:    entriesIn(selected),
		Value:      encoded,
	}, nil
}

// segment is one step of a path: a key, or an index into an array.
type segment struct {
	key     string
	index   int
	isIndex bool
}

func (s segment) String() string {
	if s.isIndex {
		return "[" + strconv.Itoa(s.index) + "]"
	}
	return s.key
}

// parseExpression turns a path expression into steps.
func parseExpression(expression string) ([]segment, error) {
	trimmed := strings.TrimSpace(expression)
	trimmed = strings.TrimPrefix(trimmed, "$")

	if trimmed == "" || trimmed == "." {
		return nil, errors.New(errors.CodeInvalidInput, "no expression was given").
			WithHint("select a value, for example: users[0].name")
	}

	var segments []segment
	for position := 0; position < len(trimmed); {
		switch trimmed[position] {
		case '.':
			// A dot separates two steps, so it has to be followed by one. A
			// trailing or doubled dot is a typo, and quietly ignoring it means
			// the user gets an answer to a question they did not ask.
			if position+1 >= len(trimmed) || trimmed[position+1] == '.' {
				return nil, invalidExpression(expression, "there is an empty key in it")
			}
			position++

		case '[':
			next, parsed, err := parseBracket(trimmed, position)
			if err != nil {
				return nil, err
			}
			segments = append(segments, parsed)
			position = next

		default:
			end := strings.IndexAny(trimmed[position:], ".[")
			if end < 0 {
				end = len(trimmed) - position
			}
			key := trimmed[position : position+end]
			if key == "" {
				return nil, invalidExpression(expression, "there is an empty key in it")
			}
			segments = append(segments, segment{key: key})
			position += end
		}
	}

	if len(segments) == 0 {
		return nil, invalidExpression(expression, "it selects nothing")
	}
	return segments, nil
}

// parseBracket reads one [...] step, which is either an index or a quoted key.
func parseBracket(expression string, start int) (int, segment, error) {
	end := strings.IndexByte(expression[start:], ']')
	if end < 0 {
		return 0, segment{}, invalidExpression(expression, "a [ is never closed")
	}
	end += start

	inner := strings.TrimSpace(expression[start+1 : end])
	if inner == "" {
		return 0, segment{}, invalidExpression(expression,
			"an empty [] would mean every element, which this syntax does not do")
	}

	if quoted := unquote(inner); quoted != "" {
		return end + 1, segment{key: quoted}, nil
	}

	index, err := strconv.Atoi(inner)
	if err != nil {
		return 0, segment{}, invalidExpression(expression,
			"%q is neither an index nor a quoted key", inner)
	}
	if index < 0 {
		return 0, segment{}, invalidExpression(expression,
			"an index counts from zero and cannot be negative")
	}

	return end + 1, segment{index: index, isIndex: true}, nil
}

// unquote returns the contents of a quoted key, or an empty string when the
// text is not quoted.
func unquote(text string) string {
	if len(text) < 2 {
		return ""
	}
	first, last := text[0], text[len(text)-1]
	if (first == '"' || first == '\'') && last == first {
		return text[1 : len(text)-1]
	}
	return ""
}

func invalidExpression(expression, reason string, args ...any) error {
	return errors.New(errors.CodeInvalidInput,
		"%q is not a valid expression: "+reason, append([]any{expression}, args...)...).
		WithHint("keys are separated by dots, array elements by [n], " +
			"and a key holding a dot goes in [\"quotes\"]")
}

// walk applies the steps of a path to a value.
func walk(value any, segments []segment) (any, error) {
	current := value
	travelled := ""

	for _, step := range segments {
		reached := join(travelled, step)

		switch {
		case step.isIndex:
			array, ok := current.([]any)
			if !ok {
				return nil, wrongShape(travelled, KindArray, kindOf(current))
			}
			if step.index >= len(array) {
				return nil, errors.New(errors.CodeNotFound,
					"%s does not exist: the array has %d element(s)", reached, len(array)).
					WithHint("indexes count from zero")
			}
			current = array[step.index]

		default:
			object, ok := current.(map[string]any)
			if !ok {
				return nil, wrongShape(travelled, KindObject, kindOf(current))
			}
			next, found := object[step.key]
			if !found {
				return nil, missingKey(reached, object)
			}
			current = next
		}

		travelled = reached
	}

	return current, nil
}

func join(travelled string, step segment) string {
	switch {
	case travelled == "":
		return step.String()
	case step.isIndex:
		return travelled + step.String()
	default:
		return travelled + "." + step.String()
	}
}

func wrongShape(travelled, wanted, found string) error {
	where := "the document"
	if travelled != "" {
		where = travelled
	}
	return errors.New(errors.CodeInvalidInput,
		"%s is %s, not %s", where, found, wanted).
		WithHint("check the shape with \"devnest json query\" on the level above")
}

// missingKey names the keys that do exist, because the answer to "no such key"
// is almost always a typo the user can see the moment they are shown the list.
func missingKey(reached string, object map[string]any) error {
	available := make([]string, 0, len(object))
	for name := range object {
		available = append(available, name)
	}
	sort.Strings(available)

	const shown = 8
	listing := strings.Join(available, ", ")
	if len(available) > shown {
		listing = strings.Join(available[:shown], ", ") +
			", and " + strconv.Itoa(len(available)-shown) + " more"
	}

	failure := errors.New(errors.CodeNotFound, "%s does not exist", reached)
	if listing == "" {
		return failure.WithHint("the object is empty")
	}
	return failure.WithHint("the keys here are: %s", listing)
}
