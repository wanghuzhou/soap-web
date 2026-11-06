// Package charset decodes legacy single-byte / multi-byte encodings that are
// still common in domestic (especially SOAP) services: GBK, GB18030, GB2312 and
// Big5. Without this, a GBK response body is handed to the frontend as raw
// bytes interpreted as UTF-8 and every Chinese character renders as mojibake.
//
// The package is deliberately dependency-light: only golang.org/x/text is used.
package charset

import (
	"bytes"
	"io"
	"strings"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

// lookup returns the encoding registered for a charset name. It reports false
// for UTF-8 / ASCII (already handled) and for anything unknown, so callers can
// fall back to treating the bytes as UTF-8.
func lookup(name string) (encoding.Encoding, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "utf-8", "utf8", "us-ascii", "ascii", "unicode-1-1-utf-8":
		return nil, false
	case "gbk", "cp936", "ms936", "windows-936", "x-gbk":
		return simplifiedchinese.GBK, true
	case "gb2312", "gb-2312", "euc-cn", "x-euc-cn", "csgb2312":
		// GB2312 is a subset of GBK, so the GBK decoder handles it correctly.
		// (HZGB2312 in x/text is the unrelated HZ 7-bit encoding.)
		return simplifiedchinese.GBK, true
	case "gb18030", "gb-18030":
		return simplifiedchinese.GB18030, true
	case "big5", "big-5", "cp950", "ms950", "windows-950", "x-x-big5":
		return traditionalchinese.Big5, true
	default:
		return nil, false
	}
}

// Decode converts bytes in the named charset to UTF-8. Unknown or UTF-8 names
// return the input unchanged; a decoding failure also returns the input
// unchanged, because mangled-but-visible bytes beat an empty response.
func Decode(data []byte, name string) []byte {
	enc, ok := lookup(name)
	if !ok || len(data) == 0 {
		return data
	}
	out, err := enc.NewDecoder().Bytes(data)
	if err != nil {
		return data
	}
	return out
}

// Reader is the encoding/xml CharsetReader hook: it lets xml.Decoder handle
// documents whose XML declaration names a legacy charset. Returning the reader
// unchanged for unknown names keeps decoding best-effort rather than fatal.
func Reader(name string, input io.Reader) (io.Reader, error) {
	enc, ok := lookup(name)
	if !ok {
		return input, nil
	}
	return enc.NewDecoder().Reader(input), nil
}

// XMLDeclEncoding extracts the value of encoding="..." from the XML declaration
// at the start of the document. It returns "" when the document has no
// declaration or declares no encoding.
func XMLDeclEncoding(data []byte) string {
	// The declaration must appear at the very start (leading BOM allowed).
	s := bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	s = bytes.TrimLeft(s, " \t\r\n")
	if !bytes.HasPrefix(s, []byte("<?xml")) {
		return ""
	}
	end := bytes.Index(s, []byte("?>"))
	if end < 0 {
		return ""
	}
	decl := string(s[:end])
	for _, field := range strings.Fields(decl) {
		const prefix = "encoding="
		if !strings.HasPrefix(field, prefix) {
			continue
		}
		v := strings.Trim(field[len(prefix):], `"'`)
		return v
	}
	return ""
}

// DecodeXML converts a document to UTF-8 using its own XML declaration.
// This is the single entry point used before handing bytes to xml.Decoder.
func DecodeXML(data []byte) []byte {
	if enc := XMLDeclEncoding(data); enc != "" {
		return Decode(data, enc)
	}
	return data
}
