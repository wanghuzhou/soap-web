// Package wsdlfetch downloads a WSDL document from a remote URL so it can be
// imported without saving the file locally first.
//
// Fetching a URL supplied by the user is a trust boundary: the request runs on
// the machine hosting soap-web and can therefore reach hosts that the browser
// cannot (that is the point for intranet services). This package keeps the
// operation predictable: only http/https, a bounded response size, a bounded
// timeout, and an early content check so an HTML login/error page produces a
// clear message instead of an obscure XML parse error.
package wsdlfetch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"soapweb/internal/charset"
	"soapweb/internal/model"
	"soapweb/internal/transport"
)

// MaxBytes caps how much of the response is read. WSDL documents with embedded
// schemas are usually a few hundred KB; a response far beyond that is either
// not a WSDL or an attempt to exhaust memory.
const MaxBytes = 8 << 20 // 8 MB

// Timeout bounds the whole download, including redirects.
const Timeout = 30 * time.Second

var (
	// ErrInvalidURL indicates the caller-supplied address is not a usable
	// http/https URL. It is a request-shape problem (HTTP 400).
	ErrInvalidURL = errors.New("WSDL 地址无效")

	// ErrNotWSDL indicates the server answered with something that is not a
	// WSDL document, for example an HTML login page.
	ErrNotWSDL = errors.New("响应内容不是 WSDL 文档")
)

// Result is a fetched WSDL document plus the metadata the importer needs.
type Result struct {
	// Data is the response body normalised to UTF-8. A document that declared a
	// legacy encoding (GBK etc.) has been transcoded and its XML declaration
	// rewritten to UTF-8, so downstream parsers do not decode it a second time.
	Data []byte
	// Name is a fallback project name derived from the final URL, used only
	// when the WSDL itself does not declare a service name.
	Name string
	// FinalURL is the URL after following redirects.
	FinalURL string
}

// Fetch downloads a WSDL from rawURL.
//
// proxyCfg may be nil, in which case the system proxy configuration is used.
// When the caller passes a project's proxy settings, the download travels the
// same network path as that project's SOAP requests — important for WSDLs that
// are only reachable through an intranet proxy.
func Fetch(rawURL string, proxyCfg *model.ProxySettings) (*Result, error) {
	u, err := parseURL(rawURL)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w：%v", ErrInvalidURL, err)
	}
	// Some gateways serve a different document by User-Agent and reject the
	// bare Go client outright; a descriptive UA also makes the traffic easy to
	// identify in server logs.
	req.Header.Set("User-Agent", "soap-web/1.0 (WSDL import)")
	req.Header.Set("Accept", "text/xml, application/xml, application/wsdl+xml, */*")

	client, err := transport.NewClient(proxyCfg, Timeout)
	if err != nil {
		return nil, fmt.Errorf("代理配置错误: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("获取 WSDL 失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("获取 WSDL 失败：服务器返回 %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	if resp.ContentLength > MaxBytes {
		return nil, fmt.Errorf("WSDL 超过 %d MB 上限", MaxBytes>>20)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取 WSDL 失败: %w", err)
	}
	if len(body) > MaxBytes {
		return nil, fmt.Errorf("WSDL 超过 %d MB 上限", MaxBytes>>20)
	}
	if len(body) == 0 {
		return nil, errors.New("响应内容为空")
	}

	data := adaptCharset(body, resp.Header.Get("Content-Type"))
	if !looksLikeWSDL(data) {
		return nil, ErrNotWSDL
	}
	final := resp.Request.URL
	// A redirect must not smuggle a non-http scheme past the initial check.
	if strings.ToLower(final.Scheme) != "http" && strings.ToLower(final.Scheme) != "https" {
		return nil, fmt.Errorf("%w：重定向到了不支持的协议 %q", ErrInvalidURL, final.Scheme)
	}

	return &Result{
		Data:     data,
		Name:     suggestName(final),
		FinalURL: final.String(),
	}, nil
}

// parseURL validates and normalises a user-supplied WSDL URL.
func parseURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("%w：缺少 url", ErrInvalidURL)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%w：%v", ErrInvalidURL, err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("%w：仅支持 http/https 协议，当前为 %q", ErrInvalidURL, u.Scheme)
	}
	if u.Host == "" || u.Hostname() == "" {
		return nil, fmt.Errorf("%w：缺少主机名", ErrInvalidURL)
	}
	u.Scheme = scheme
	return u, nil
}

// adaptCharset converts the document to UTF-8 when its XML declaration or the
// HTTP header names a legacy charset.
//
// The XML declaration is rewritten to UTF-8 after transcoding. Without that,
// wsdl.Parse would see the original GBK declaration and decode the already
// UTF-8 bytes a second time, producing mojibake.
func adaptCharset(data []byte, contentType string) []byte {
	name := charset.XMLDeclEncoding(data)
	declared := name != ""
	if name == "" {
		_, params, err := mime.ParseMediaType(contentType)
		if err == nil {
			name = strings.TrimSpace(params["charset"])
		}
	}
	if isUTF8Charset(name) {
		return data
	}

	decoded := charset.Decode(data, name)
	if bytes.Equal(decoded, data) && !utf8.Valid(data) {
		// The decoder refused the bytes; leave them untouched so the parser can
		// report the real error instead of silently accepting mangled text.
		return data
	}
	if !utf8.Valid(decoded) {
		return data
	}
	if declared {
		return rewriteXMLDeclEncoding(decoded)
	}
	return decoded
}

// isUTF8Charset reports charsets that need no transcoding.
func isUTF8Charset(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "utf-8", "utf8", "us-ascii", "ascii":
		return true
	default:
		return false
	}
}

// rewriteXMLDeclEncoding replaces the value of encoding="..." in the XML
// declaration with UTF-8, preserving the original quoting style.
func rewriteXMLDeclEncoding(data []byte) []byte {
	prefixLen := 0
	if bytes.HasPrefix(data, []byte("\xef\xbb\xbf")) {
		prefixLen = 3
	}
	rest := data[prefixLen:]
	if !bytes.HasPrefix(rest, []byte("<?xml")) {
		return data
	}
	end := bytes.Index(rest, []byte("?>"))
	if end < 0 {
		return data
	}
	decl := rest[:end]
	idx := bytes.Index(bytes.ToLower(decl), []byte("encoding"))
	if idx < 0 {
		return data
	}
	i := idx + len("encoding")
	for i < len(decl) && (decl[i] == ' ' || decl[i] == '\t') {
		i++
	}
	if i >= len(decl) || decl[i] != '=' {
		return data
	}
	i++
	for i < len(decl) && (decl[i] == ' ' || decl[i] == '\t') {
		i++
	}
	if i >= len(decl) {
		return data
	}
	quote := decl[i]
	if quote != '"' && quote != '\'' {
		return data
	}
	valueStart := i + 1
	rel := bytes.IndexByte(decl[valueStart:], quote)
	if rel < 0 {
		return data
	}
	valueEnd := valueStart + rel

	var out bytes.Buffer
	out.Grow(len(data))
	out.Write(data[:prefixLen])
	out.Write(decl[:valueStart])
	out.WriteString("UTF-8")
	out.Write(decl[valueEnd:])
	out.Write(rest[end:])
	return out.Bytes()
}

// looksLikeWSDL reports whether the document's root element is
// <definitions> (optionally namespace-prefixed, e.g. <wsdl:definitions>).
//
// Inspecting the root element rather than searching for a substring avoids two
// classes of false positives: HTML error pages that merely mention "definitions"
// in their body, and malformed fragments that are not complete documents.
func looksLikeWSDL(data []byte) bool {
	s := bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	for {
		s = bytes.TrimLeft(s, " \t\r\n")
		switch {
		case bytes.HasPrefix(s, []byte("<?")):
			end := bytes.Index(s, []byte("?>"))
			if end < 0 {
				return false
			}
			s = s[end+2:]
		case bytes.HasPrefix(s, []byte("<!--")):
			end := bytes.Index(s, []byte("-->"))
			if end < 0 {
				return false
			}
			s = s[end+3:]
		default:
			if !bytes.HasPrefix(s, []byte("<")) {
				return false
			}
			end := bytes.IndexByte(s, '>')
			if end < 0 {
				return false
			}
			tag := strings.TrimSpace(string(s[1:end]))
			if i := strings.IndexAny(tag, " \t\r\n/"); i >= 0 {
				tag = tag[:i]
			}
			if i := strings.IndexByte(tag, ':'); i >= 0 {
				tag = tag[i+1:]
			}
			return tag == "definitions"
		}
	}
}

// suggestName derives a readable fallback name from the URL: the final path
// segment without a .wsdl/.xml suffix, else the host name.
func suggestName(u *url.URL) string {
	p := strings.TrimRight(u.Path, "/")
	if p != "" {
		base := p[strings.LastIndex(p, "/")+1:]
		lower := strings.ToLower(base)
		for _, suffix := range []string{".wsdl", ".xml"} {
			if strings.HasSuffix(lower, suffix) {
				base = base[:len(base)-len(suffix)]
				break
			}
		}
		if base != "" {
			return base
		}
	}
	return u.Hostname()
}
