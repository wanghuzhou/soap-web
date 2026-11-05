// Package soapuiproj parses SoapUI project XML files into the internal model.
//
// We only need read-compatibility: extract the service/operation definitions,
// the saved request bodies and endpoints, and the test-suite hierarchy. We do
// not execute scripts or reproduce SoapUI behaviour.
package soapuiproj

import (
	"encoding/xml"
	"errors"
	"html"
	"strings"

	"soapweb/internal/model"
)

// SoapUI project file uses a default namespace plus "con" prefixed names.
// We capture the bits we need via struct tags with wildcard namespaces.

type xmlProject struct {
	XMLName    xml.Name       `xml:"soapui-project"`
	Name       string         `xml:"name,attr"`
	Interfaces []xmlInterface `xml:"interface"`
	TestSuites []xmlTestSuite `xml:"testSuite"`
}

type xmlInterface struct {
	Name       string         `xml:"name,attr"`
	Definition string         `xml:"definition,attr"` // WSDL url/path
	Endpoints  []xmlEP        `xml:"endpoints>endpoint"`
	Operations []xmlOperation `xml:"operation"`
}

type xmlEP struct {
	Value string `xml:",chardata"`
}

type xmlOperation struct {
	Name      string    `xml:"name,attr"`
	Action    string    `xml:"action,attr"`
	Endpoint  string    `xml:"endpoint,attr"`
	Style     string    `xml:"style,attr"`
	Use       string    `xml:"use,attr"`
	InputName string    `xml:"inputName,attr"`
	Calls     []xmlCall `xml:"call"`
}

// xmlCall is <con:call>, which contains a named saved request.
type xmlCall struct {
	Name     string        `xml:"name,attr"`
	XMLName  xml.Name      `xml:"call"`
	Endpoint string        `xml:"endpoint"`
	Requests []xmlNamedReq `xml:"request"`
}

// xmlNamedReq captures <con:request> contents. SoapUI stores the request body
// as character data (often HTML-escaped) inside a nested <con:request> element.
type xmlNamedReq struct {
	XMLName   xml.Name `xml:"request"`
	MediaType string   `xml:"mediaType,attr"`
	Endpoint  string   `xml:"endpoint"`
	Action    string   `xml:"action,attr"`
	Inner     string   `xml:"request"` // the nested <con:request> character data
	Body      string   `xml:",chardata"`
}

// body returns the actual request payload, preferring the nested element.
func (r xmlNamedReq) bodyText() string {
	if strings.TrimSpace(r.Inner) != "" {
		return r.Inner
	}
	return r.Body
}

type xmlTestSuite struct {
	Name      string        `xml:"name,attr"`
	TestCases []xmlTestCase `xml:"testCase"`
}

type xmlTestCase struct {
	Name  string        `xml:"name,attr"`
	Steps []xmlTestStep `xml:"testStep"`
}

type xmlTestStep struct {
	Name   string    `xml:"name,attr"`
	Type   string    `xml:"type,attr"`
	Config xmlConfig `xml:"config"`
}

// xmlConfig holds the request config of a request-type test step.
type xmlConfig struct {
	Request xmlNamedReq `xml:"request"`
}

// Parse decodes SoapUI project bytes into a model.Project.
func Parse(data []byte) (*model.Project, error) {
	clean := sanitizeXML(data)
	var raw xmlProject
	dec := xml.NewDecoder(strings.NewReader(clean))
	dec.Strict = false
	dec.CharsetReader = charsetReader
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	if raw.XMLName.Local != "soapui-project" {
		return nil, errors.New("根元素不是 soapui-project，可能不是 SoapUI 工程文件")
	}

	proj := &model.Project{Name: raw.Name, Source: "soapui"}

	for _, xi := range raw.Interfaces {
		svc := model.Service{Name: xi.Name, Wsdl: xi.Definition}
		var ifaceEndpoint string
		if len(xi.Endpoints) > 0 {
			ifaceEndpoint = strings.TrimSpace(xi.Endpoints[0].Value)
		}
		for _, op := range xi.Operations {
			o := model.Operation{
				Name:       op.Name,
				SoapAction: op.Action,
				Endpoint:   op.Endpoint,
				Style:      op.Style,
				Use:        op.Use,
				InputName:  op.InputName,
			}
			// Each <con:call> is a distinct saved request sample. Preserve its
			// title and its own request body so no parameter set is lost.
			for _, call := range op.Calls {
				if len(call.Requests) == 0 {
					continue
				}
				req := call.Requests[0]
				c := model.Call{
					Title:    strings.TrimSpace(call.Name),
					Endpoint: strings.TrimSpace(firstNonEmpty(req.Endpoint, call.Endpoint)),
					Action:   firstNonEmpty(req.Action, op.Action),
					Request:  unescapeXMLText(req.bodyText()),
				}
				if c.Title == "" {
					c.Title = op.Name
				}
				o.Calls = append(o.Calls, c)
			}
			// Use the first call as the operation's default request/endpoint.
			if len(o.Calls) > 0 {
				o.Request = o.Calls[0].Request
				if o.Endpoint == "" {
					o.Endpoint = o.Calls[0].Endpoint
				}
				if o.SoapAction == "" {
					o.SoapAction = o.Calls[0].Action
				}
			}
			if o.Endpoint == "" {
				o.Endpoint = ifaceEndpoint
			}
			svc.Operations = append(svc.Operations, o)
		}
		proj.Services = append(proj.Services, svc)
	}

	for _, xs := range raw.TestSuites {
		ts := model.TestSuite{Name: xs.Name}
		for _, xc := range xs.TestCases {
			tc := model.TestCase{Name: xc.Name}
			for _, st := range xc.Steps {
				step := model.TestStep{Name: st.Name, Type: st.Type}
				step.Endpoint = strings.TrimSpace(st.Config.Request.Endpoint)
				step.Action = st.Config.Request.Action
				step.Request = unescapeXMLText(st.Config.Request.bodyText())
				tc.Steps = append(tc.Steps, step)
			}
			ts.TestCases = append(ts.TestCases, tc)
		}
		proj.TestSuites = append(proj.TestSuites, ts)
	}

	return proj, nil
}

// unescapeXMLText converts escaped entities (SoapUI stores request bodies with
// &lt; &amp; etc.) back to real XML. It is idempotent for already-unescaped text.
func unescapeXMLText(s string) string {
	return strings.TrimSpace(html.UnescapeString(s))
}

// firstNonEmpty returns the first non-blank string among its arguments.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// sanitizeXML drops a leading UTF-8 BOM and trims whitespace.
func sanitizeXML(data []byte) string {
	s := string(data)
	s = strings.TrimPrefix(s, "\ufeff")
	return strings.TrimSpace(s)
}
