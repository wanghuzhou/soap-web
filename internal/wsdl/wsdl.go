// Package wsdl parses WSDL 1.1 documents (with embedded or referenced XML
// Schema) into operations and generates editable XML request templates by
// walking the schema type tree.
//
// Scope is deliberately pragmatic: we generate structurally-correct request
// bodies, degrading gracefully when a construct is too complex to resolve
// (placeholder comment instead of failing).
package wsdl

import (
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"soapweb/internal/model"
)

// ---- raw WSDL structures (document/literal oriented) ----

type defs struct {
	XMLName   xml.Name     `xml:"definitions"`
	TargetNS  string       `xml:"targetNamespace,attr"`
	Name      string       `xml:"name,attr"`
	Services  []wService   `xml:"service"`
	Bindings  []wBinding   `xml:"binding"`
	PortTypes []wPortType  `xml:"portType"`
	Messages  []wMessage   `xml:"message"`
	Schemas   []schemaElem `xml:"types>schema"`
}

type wService struct {
	Name  string  `xml:"name,attr"`
	Ports []wPort `xml:"port"`
}

type wPort struct {
	Name    string  `xml:"name,attr"`
	Binding string  `xml:"binding,attr"`
	Address []wAddr `xml:"address"`
}

type wAddr struct {
	Location string `xml:"location,attr"`
}

type wBinding struct {
	Name     string    `xml:"name,attr"`
	Type     string    `xml:"type,attr"`
	PortType string    `xml:"portType,attr"`
	Ops      []wBindOp `xml:"operation"`
}

type wBindOp struct {
	Name   string    `xml:"name,attr"`
	SoapOp []soapOp  `xml:"operation"`
	Input  []wBindIO `xml:"input"`
	Output []wBindIO `xml:"output"`
}

type soapOp struct {
	Action string `xml:"soapAction,attr"`
	Style  string `xml:"style,attr"`
}

type wBindIO struct {
	Body wSOAPBody `xml:"body"`
}

type wSOAPBody struct {
	Use   string  `xml:"use,attr"`
	Parts []wPart `xml:"part"`
}

type wPortType struct {
	Name string  `xml:"name,attr"`
	Ops  []wPTOp `xml:"operation"`
}

type wPTOp struct {
	Name   string    `xml:"name,attr"`
	Input  []wPTOpIO `xml:"input"`
	Output []wPTOpIO `xml:"output"`
}

type wPTOpIO struct {
	Message string `xml:"message,attr"`
}

type wMessage struct {
	Name  string  `xml:"name,attr"`
	Parts []wPart `xml:"part"`
}

type wPart struct {
	Name    string `xml:"name,attr"`
	Element string `xml:"element,attr"`
	Type    string `xml:"type,attr"`
}

// Parse parses a WSDL byte slice into a model-style Service (defined in model).
func Parse(data []byte) (*Result, error) {
	clean := strings.TrimPrefix(string(data), "\ufeff")
	var d defs
	dec := xml.NewDecoder(strings.NewReader(clean))
	dec.Strict = false
	if err := dec.Decode(&d); err != nil {
		return nil, err
	}
	if d.XMLName.Local != "definitions" {
		return nil, errors.New("根元素不是 definitions，可能不是 WSDL 文件")
	}

	// Build lookup tables.
	msgByQName := map[string]wMessage{}
	for _, m := range d.Messages {
		msgByQName[localName(m.Name)] = m
	}
	ptByQName := map[string]wPortType{}
	for _, pt := range d.PortTypes {
		ptByQName[localName(pt.Name)] = pt
	}
	bindByQName := map[string]wBinding{}
	for _, b := range d.Bindings {
		bindByQName[localName(b.Name)] = b
	}

	// schema index for element/type resolution
	idx := newSchemaIndex(d.Schemas)

	// Map binding -> endpoint via service ports.
	endpointByBinding := map[string]string{}
	for _, s := range d.Services {
		for _, p := range s.Ports {
			if len(p.Address) > 0 {
				endpointByBinding[localName(p.Binding)] = p.Address[0].Location
			}
		}
	}

	res := &Result{Name: d.Name, TargetNS: d.TargetNS}

	for bName, b := range bindByQName {
		pt := ptByQName[localName(b.Type)]
		// index portType ops by name
		ptOps := map[string]wPTOp{}
		for _, o := range pt.Ops {
			ptOps[o.Name] = o
		}
		for _, bo := range b.Ops {
			op := model.Operation{Name: bo.Name, Endpoint: endpointByBinding[bName]}
			if len(bo.SoapOp) > 0 {
				op.SoapAction = bo.SoapOp[0].Action
				op.Style = bo.SoapOp[0].Style
			}
			if len(bo.Input) > 0 {
				op.Use = bo.Input[0].Body.Use
			}
			// Find the input message to know the root element.
			var rootElem string
			if pto, ok := ptOps[bo.Name]; ok && len(pto.Input) > 0 {
				msg := msgByQName[localName(pto.Input[0].Message)]
				if len(msg.Parts) > 0 {
					rootElem = localName(msg.Parts[0].Element)
					if rootElem == "" {
						rootElem = msg.Parts[0].Name
					}
				}
			}
			op.InputName = rootElem
			op.Request = idx.buildRequest(rootElem, d.TargetNS)
			res.Operations = append(res.Operations, op)
		}
	}

	if len(res.Operations) == 0 {
		return nil, errors.New("未从 WSDL 中解析出任何操作（可能缺少 binding/service 定义）")
	}
	return res, nil
}

// Result is the parsed WSDL output returned to the API layer.
type Result struct {
	Name       string            `json:"name"`
	TargetNS   string            `json:"targetNamespace"`
	Operations []model.Operation `json:"operations"`
}

// Fetch loads a WSDL from an http(s) URL (helper for future remote import).
func Fetch(url string) ([]byte, error) {
	c := &http.Client{Timeout: 30 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func localName(q string) string {
	if i := strings.Index(q, ":"); i >= 0 {
		return q[i+1:]
	}
	// strip clark-ish namespace if any
	if i := strings.LastIndex(q, "}"); i >= 0 {
		return q[i+1:]
	}
	return q
}
