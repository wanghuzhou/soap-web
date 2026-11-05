// Package model defines the shared data structures used across the application.
package model

// Project is the root container. It mirrors the structure of a SoapUI project
// closely enough for import, while staying simple for our own needs.
type Project struct {
	Name       string      `json:"name"`
	Source     string      `json:"source"` // e.g. "soapui" or "native"
	Services   []Service   `json:"services"`
	TestSuites []TestSuite `json:"testSuites"`
}

// Service represents a WSDL-described interface (a SoapUI "interface").
type Service struct {
	Name       string      `json:"name"`
	Wsdl       string      `json:"wsdl"` // local path or URL
	Operations []Operation `json:"operations"`
}

// Operation is a single callable operation derived from the WSDL.
type Operation struct {
	Name       string `json:"name"`
	SoapAction string `json:"soapAction"`
	Endpoint   string `json:"endpoint"`
	Style      string `json:"style"`     // document / rpc
	Use        string `json:"use"`       // literal / encoded
	InputName  string `json:"inputName"` // wrapper element name, if known
	Request    string `json:"request"`   // generated XML request template
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
