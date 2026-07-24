package output

import (
	"io"
	"os"
)

// IsTerminal reports whether a stream is an interactive terminal.
//
// The check is a character-device test, which is true for a console on every
// supported platform and false for a file or a pipe. It is also true for the
// null device, which costs nothing: colour written to a discarded stream is
// discarded with it.
//
// The parameter is untyped because DevNest passes its streams around as
// io.Reader and io.Writer, and both stdin and stderr need this question
// answered. Only an *os.File can be a terminal; everything else is not.
func IsTerminal(stream any) bool {
	file, ok := stream.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// UseColor decides whether to emit colour, given the configured mode and the
// stream being written to.
//
// The NO_COLOR convention is honoured for any value, including an empty one,
// and overrides "auto" but not an explicit "always".
func UseColor(mode string, w io.Writer, lookupEnv func(string) (string, bool)) bool {
	switch mode {
	case "always":
		return true
	case "never":
		return false
	}
	if lookupEnv != nil {
		if _, set := lookupEnv("NO_COLOR"); set {
			return false
		}
	}
	return IsTerminal(w)
}
