// Package encoding is DevNest's encoding module: hex, URL percent-encoding,
// and structural decoding of JSON Web Tokens.
//
// Everything here happens locally and takes its input from the caller. Nothing
// in this package opens a socket, reads a file, or keeps anything after the
// call returns. That is the entire point of the JWT command in particular: a
// token pasted into a web page has been disclosed to whoever runs the page,
// and there is no way to take it back.
//
// # Bytes that are not text
//
// Decoding produces arbitrary bytes, and a terminal given arbitrary bytes can
// move its cursor, change colour, or act on an escape sequence nobody typed. A
// decode result therefore says whether what came out is printable text; when
// it is not, the bytes are carried as Base64 instead and the interface layer
// decides what to show.
//
// # Nothing here verifies anything
//
// JWT decoding reads the header and the payload and reports the claims. It
// does not check the signature, and every result says so in a field rather
// than only in documentation. Verification needs the signing key, key rotation
// handling, and an audience policy; a tool that half-does it teaches people to
// trust output that means nothing.
package encoding

import (
	"encoding/base64"
	"unicode/utf8"
)

// printable reports whether bytes are safe to write to a terminal.
//
// Valid UTF-8 is not enough on its own: an escape character is perfectly valid
// UTF-8 and can repaint the screen. Tab, newline, and carriage return are
// allowed because text legitimately contains them.
//
// The security module carries the same rule for its own decoder. Fifteen lines
// duplicated across two modules is the price of the layering: a module never
// imports another module, and this is too small a thing to justify a package
// of its own.
func printable(data []byte) bool {
	if !utf8.Valid(data) {
		return false
	}

	for _, character := range string(data) {
		switch character {
		case '\t', '\n', '\r':
			continue
		}
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

// carry fills in the decoded form of arbitrary bytes: the text when it is
// text, and Base64 when it is not.
func carry(data []byte) (decoded string, encoded string, isText bool) {
	if printable(data) {
		return string(data), "", true
	}
	return "", base64.StdEncoding.EncodeToString(data), false
}
