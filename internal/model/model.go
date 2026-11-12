// Package model defines the shared data structures used across the application.
package model

import (
	"errors"
	"fmt"
	"strings"
)

// Project is the root container. It mirrors the structure of a SoapUI project
// closely enough for import, while staying simple for our own needs.
type Project struct {
	ID     string         `json:"id"` // stable identifier for CRUD
	Name   string         `json:"name"`
	Source string         `json:"source"` // e.g. "soapui", "wsdl", "wsdl-url" or "native"
	Proxy  *ProxySettings `json:"proxy,omitempty"`
	// Headers are sent with every request of this project. A call may override
	// an individual value; see Call.Headers.
	Headers    []Header    `json:"headers,omitempty"`
	Services   []Service   `json:"services"`
	TestSuites []TestSuite `json:"testSuites"`
}

// Header is one HTTP header on a project or a call. A slice rather than a map
// keeps the user's order and lets an empty Value mean "send the header with no
// value" instead of being indistinguishable from "not set".
type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// ValidateHeaders rejects names that cannot be sent: a request with a bad
// header name fails at the transport layer with an opaque error, so it is
// caught here where the message can name the culprit.
func ValidateHeaders(headers []Header) error {
	for _, h := range headers {
		name := strings.TrimSpace(h.Name)
		if name == "" {
			return errors.New("请求头名称不能为空")
		}
		for _, r := range name {
			if r <= ' ' || r >= 0x7f || strings.ContainsRune("()<>@,;:\\\"/[]?={}", r) {
				return fmt.Errorf("请求头名称 %q 含非法字符", name)
			}
		}
		if strings.ContainsAny(h.Value, "\r\n") {
			return fmt.Errorf("请求头 %q 的值不能包含换行", name)
		}
	}
	return nil
}

// ProxySettings controls how requests of a project reach the network.
// Mode is one of "system" (use OS/environment proxy), "direct" (no proxy) or
// "custom". For custom, Type is "http" or "socks5"; Username/Password may be
// empty when the proxy needs no authentication.
type ProxySettings struct {
	Mode     string `json:"mode"` // system | direct | custom
	Type     string `json:"type"` // http | socks5
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// Validate reports whether the settings are usable.
func (p *ProxySettings) Validate() error {
	if p == nil {
		return nil
	}
	switch p.Mode {
	case "", "system", "direct":
		return nil
	case "custom":
		if p.Host == "" {
			return errors.New("自定义代理缺少主机地址")
		}
		if p.Port <= 0 || p.Port > 65535 {
			return errors.New("自定义代理端口无效")
		}
		if p.Type != "http" && p.Type != "socks5" {
			return errors.New("自定义代理类型必须是 http 或 socks5")
		}
		return nil
	default:
		return errors.New("未知的代理模式")
	}
}

// Service represents a WSDL-described interface (a SoapUI "interface").
type Service struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Wsdl       string      `json:"wsdl"` // local path or URL
	Operations []Operation `json:"operations"`
}

// Operation is a single callable operation derived from the WSDL.
//
// An operation may carry several saved request samples (SoapUI lets you keep
// multiple named requests per operation, each with its own parameters). These
// live in Calls. Request mirrors the first call for convenience.
type Operation struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	SoapAction string `json:"soapAction"`
	Endpoint   string `json:"endpoint"`
	Style      string `json:"style"`     // document / rpc
	Use        string `json:"use"`       // literal / encoded
	InputName  string `json:"inputName"` // wrapper element name, if known
	Request    string `json:"request"`   // generated or first saved XML template
	Template   string `json:"template"`  // pure structural skeleton (no business data), from WSDL
	Calls      []Call `json:"calls"`     // saved request samples (with titles)

	// Bindings lists every WSDL binding that exposes this operation (for
	// example "SvcSoap11" and "SvcSoap12"). A WSDL often declares the same
	// operation twice, once per SOAP version; the parser keeps one entry per
	// operation and records the alternatives here so the UI can show which
	// protocols are available instead of listing duplicate rows.
	Bindings []string `json:"bindings,omitempty"`
}

// Call is a named saved request sample within an operation (a SoapUI <con:call>).
type Call struct {
	ID       string `json:"id"`
	Title    string `json:"title"` // e.g. "5.6.5 物价查询(药品等)"
	Endpoint string `json:"endpoint"`
	Action   string `json:"action"`
	Request  string `json:"request"` // raw XML body
	// Headers override the project's defaults for this sample only.
	Headers []Header `json:"headers,omitempty"`
	// TimeoutMS bounds this sample's request. 0 means "use the default".
	TimeoutMS int `json:"timeoutMs,omitempty"`
	// Response is the last response the user saved for this sample. It is
	// persisted with the workspace so reopening the sample shows it without
	// sending the request again.
	Response *SavedResponse `json:"response,omitempty"`
}

// SavedResponse is a response kept with a request sample. It deliberately
// mirrors the useful parts of SendResponse but omits transient fields.
type SavedResponse struct {
	Status     int               `json:"status"`
	StatusText string            `json:"statusText"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
	DurationMS int64             `json:"durationMs"`
	Size       int               `json:"size"`
	Truncated  bool              `json:"truncated,omitempty"`
}

// MaxSavedResponseBytes caps a persisted response. It matches the transport
// read cap, so anything the UI could display can also be saved.
const MaxSavedResponseBytes = 16 << 20

// ValidateSavedResponse rejects a response body larger than the cap.
func ValidateSavedResponse(r *SavedResponse) error {
	if r == nil {
		return nil
	}
	if len(r.Body) > MaxSavedResponseBytes {
		return fmt.Errorf("响应内容超过 %d MB 上限", MaxSavedResponseBytes>>20)
	}
	return nil
}

// ValidateProjectResponses checks every saved response in a project, so a
// hand-written workspace JSON cannot bypass the size limit.
func ValidateProjectResponses(p Project) error {
	for _, svc := range p.Services {
		for _, op := range svc.Operations {
			for _, c := range op.Calls {
				if err := ValidateSavedResponse(c.Response); err != nil {
					return fmt.Errorf("请求样例「%s」: %w", c.Title, err)
				}
			}
		}
	}
	return nil
}

// DefaultTimeoutMS applies when neither the sample nor the caller sets one.
// Slow legacy services sometimes need more; a runaway request must not hold a
// connection open forever.
const DefaultTimeoutMS = 60000

// MinTimeoutMS / MaxTimeoutMS bound the value a caller may ask for. The lower
// bound keeps a typo ("1") from turning every request into an instant timeout.
const (
	MinTimeoutMS = 1000
	MaxTimeoutMS = 600000
)

// ClampTimeout returns ms bounded to [MinTimeoutMS, MaxTimeoutMS], falling back
// to DefaultTimeoutMS when unset. The API layer and transport share this so the
// bounds cannot drift apart.
func ClampTimeout(ms int) int {
	if ms <= 0 {
		return DefaultTimeoutMS
	}
	if ms < MinTimeoutMS {
		return MinTimeoutMS
	}
	if ms > MaxTimeoutMS {
		return MaxTimeoutMS
	}
	return ms
}

// DefaultRequestBody returns the body a new request sample should start from:
// the operation's structural template, then its generated request, then the
// first saved call. Keeping the order here means seeding a call behaves the
// same everywhere instead of each caller re-implementing the fallback chain.
func (op *Operation) DefaultRequestBody() string {
	if op.Template != "" {
		return op.Template
	}
	if op.Request != "" {
		return op.Request
	}
	if len(op.Calls) > 0 {
		return op.Calls[0].Request
	}
	return ""
}

// SeedCall builds an operation's default request sample from its structural
// template. It reports false when no body can be derived, so callers can skip
// operations that would otherwise save an empty call.
func SeedCall(op *Operation) (Call, bool) {
	body := op.DefaultRequestBody()
	if body == "" {
		return Call{}, false
	}
	title := op.Name
	if title == "" {
		title = "请求"
	}
	return Call{
		Title:    title,
		Endpoint: op.Endpoint,
		Action:   op.SoapAction,
		Request:  body,
	}, true
}

// TestSuite / TestCase / TestStep mirror SoapUI's hierarchy. A TestStep that
// carries a request can be replayed directly without WSDL parsing.
//
// These types are pass-through only: they are read from and written back to
// SoapUI projects so no data is lost, but the UI neither displays nor executes
// them.
type TestSuite struct {
	Name      string     `json:"name"`
	TestCases []TestCase `json:"testCases"`
}

type TestCase struct {
	Name  string     `json:"name"`
	Steps []TestStep `json:"steps"`
}

type TestStep struct {
	Name     string `json:"name"`
	Type     string `json:"type"` // request / groovy / properties / unknown
	Endpoint string `json:"endpoint"`
	Action   string `json:"action"`
	Request  string `json:"request"` // raw XML body saved in the project
}

// SendRequest is the payload the frontend posts to execute an HTTP/SOAP call.
type SendRequest struct {
	ProjectID   string            `json:"projectId"` // used to resolve proxy settings
	URL         string            `json:"url"`
	Method      string            `json:"method"`
	Headers     map[string]string `json:"headers"`
	Body        string            `json:"body"`
	ContentType string            `json:"contentType"`
	TimeoutMS   int               `json:"timeoutMs"`
}

// SendResponse is what we return to the frontend after a call.
type SendResponse struct {
	Status     int               `json:"status"`
	StatusText string            `json:"statusText"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
	DurationMS int64             `json:"durationMs"`
	Size       int               `json:"size"`
	Truncated  bool              `json:"truncated,omitempty"` // body hit the size cap
	Error      string            `json:"error,omitempty"`
}
