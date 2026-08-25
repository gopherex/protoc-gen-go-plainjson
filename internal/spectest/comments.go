package spectest

import (
	"bytes"
	"io"
)

// newCommentStripper allows // line comments in case files so each case can
// cite the spec inline. Comment markers inside strings are left alone.
func newCommentStripper(raw []byte) io.Reader {
	var out bytes.Buffer
	out.Grow(len(raw))

	inString, escaped, inComment := false, false, false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch {
		case inComment:
			if c == '\n' {
				inComment = false
				out.WriteByte(c)
			}
		case escaped:
			escaped = false
			out.WriteByte(c)
		case inString:
			if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			out.WriteByte(c)
		case c == '"':
			inString = true
			out.WriteByte(c)
		case c == '/' && i+1 < len(raw) && raw[i+1] == '/':
			inComment = true
			i++
		default:
			out.WriteByte(c)
		}
	}
	return &out
}
