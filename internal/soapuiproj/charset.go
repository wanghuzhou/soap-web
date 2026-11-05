package soapuiproj

import (
	"bytes"
	"io"
	"strings"
)

// charsetReader lets encoding/xml cope with common non-UTF8 declarations by
// ignoring unknown charsets (the bytes are usually already UTF-8 in practice).
func charsetReader(charset string, input io.Reader) (io.Reader, error) {
	switch strings.ToLower(charset) {
	case "utf-8", "utf8", "us-ascii", "ascii", "":
		return input, nil
	default:
		// Best effort: return as-is.
		buf := new(bytes.Buffer)
		_, _ = io.Copy(buf, input)
		return buf, nil
	}
}
