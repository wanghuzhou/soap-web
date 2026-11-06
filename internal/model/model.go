// Package model defines the shared data structures used across the application.
package model

import "errors"

// Project is the root container. It mirrors the structure of a SoapUI project
// closely enough for import, while staying simple for our own needs.
type Project struct {
	ID         string         `json:"id"` // stable identifier for CRUD
	Name       string         `json:"name"`
	Source     string         `json:"source"` // e.g. "soapui" or "native"
	Proxy      *ProxySettings `json:"proxy,omitempty"`
	Services   []Service      `json:"services"`
	TestSuites []TestSuite    `json:"testSuites"`
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
}

// Call is a named saved request sample within an operation (a SoapUI <con:call>).
type Call struct {
	ID       string `json:"id"`
	Title    string `json:"title"` // e.g. "5.6.5 物价查询(药品等)"
	Endpoint string `json:"endpoint"`
	Action   string `json:"action"`
	Request  string `json:"request"` // raw XML body
}

// TestSuite / TestCase / TestStep mirror SoapUI's hierarchy. A TestStep that
// carries a request can be replayed directly without WSDL parsing.
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
	Error      string            `json:"error,omitempty"`
}
