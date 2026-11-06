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
	"fmt"
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
	Name        string        `xml:"name,attr"`
	Type        string        `xml:"type,attr"`
	PortType    string        `xml:"portType,attr"`
	SoapBinding []soapBinding `xml:"binding"`
	Ops         []wBindOp     `xml:"operation"`
}

// soapBinding is <soap:binding style="rpc|document" transport="..."/>.
type soapBinding struct {
	Style     string `xml:"style,attr"`
	Transport string `xml:"transport,attr"`
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
	Use           string  `xml:"use,attr"`
	Namespace     string  `xml:"namespace,attr"`
	EncodingStyle string  `xml:"encodingStyle,attr"`
	Parts         []wPart `xml:"part"`
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
		bindingStyle := ""
		if len(b.SoapBinding) > 0 {
			bindingStyle = b.SoapBinding[0].Style
		}
		for _, bo := range b.Ops {
			op := model.Operation{Name: bo.Name, Endpoint: endpointByBinding[bName]}
			if len(bo.SoapOp) > 0 {
				op.SoapAction = bo.SoapOp[0].Action
				op.Style = bo.SoapOp[0].Style
			}
			if op.Style == "" {
				op.Style = bindingStyle
			}
			// Capture the SOAP body attributes: use / namespace / encodingStyle.
			var bodyNS, encodingStyle string
			if len(bo.Input) > 0 {
				op.Use = bo.Input[0].Body.Use
				bodyNS = strings.TrimSpace(bo.Input[0].Body.Namespace)
				encodingStyle = strings.TrimSpace(bo.Input[0].Body.EncodingStyle)
			}
			// Find the input message to know the request structure.
			var rootElem string
			var parts []wPart
			if pto, ok := ptOps[bo.Name]; ok && len(pto.Input) > 0 {
				msg := msgByQName[localName(pto.Input[0].Message)]
				parts = msg.Parts
				if len(msg.Parts) > 0 {
					rootElem = localName(msg.Parts[0].Element)
				}
			}
			op.InputName = rootElem

			// Build the payload. document/literal uses the element structure;
			// rpc/encoded uses an operation wrapper over the message parts.
			// The chosen namespace comes from soap:body/@namespace for RPC
			// (which often differs from targetNamespace), else targetNamespace.
			built := ""
			decls := []nsDecl{}
			isRPC := strings.EqualFold(op.Style, "rpc")

			if !isRPC && rootElem != "" {
				payload, _ := idx.buildRequest(rootElem, d.TargetNS)
				built = payload
				// The root element declares its own xmlns:tns binding, so no
				// Envelope-level declaration is needed here.
			}
			if isRPC || built == "" || isPlaceholder(built) {
				ns := bodyNS
				if ns == "" {
					ns = d.TargetNS
				}
				prefix := prefixForNS(ns)
				built = buildRPCRequest(bo.Name, prefix, encodingStyle, parts)
				if prefix != "" && ns != "" {
					decls = append(decls, nsDecl{prefix: prefix, uri: ns})
				}
			}
			if !isPlaceholder(built) {
				built = wrapSOAPEnvelope(built, decls)
			}
			op.Request = built
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

// nsDecl is a namespace prefix binding to declare on the Envelope.
type nsDecl struct{ prefix, uri string }

// prefixForNS picks a readable prefix for a namespace URI.
func prefixForNS(uri string) string {
	if uri == "" {
		return ""
	}
	if strings.HasPrefix(uri, "urn:") {
		return "urn"
	}
	return "tns"
}

// wrapSOAPEnvelope wraps a request payload element in a SOAP 1.1 Envelope so
// the template is directly sendable. Namespace bindings used by the payload are
// declared on the Envelope (matching how SoapUI-generated requests look).
func wrapSOAPEnvelope(payload string, decls []nsDecl) string {
	// Indent every payload line by 6 spaces (inside Envelope>Body).
	lines := strings.Split(strings.TrimRight(payload, "\n"), "\n")
	var indented strings.Builder
	for _, ln := range lines {
		indented.WriteString("      ")
		indented.WriteString(ln)
		indented.WriteString("\n")
	}
	var b strings.Builder
	b.WriteString(`<soapenv:Envelope xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xmlns:xsd="http://www.w3.org/2001/XMLSchema" xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/"`)
	seen := map[string]bool{"xsi": true, "xsd": true, "soapenv": true}
	for _, d := range decls {
		if d.prefix == "" || d.uri == "" || seen[d.prefix] {
			continue
		}
		seen[d.prefix] = true
		b.WriteString(fmt.Sprintf(` xmlns:%s="%s"`, d.prefix, d.uri))
	}
	b.WriteString(">\n")
	b.WriteString("   <soapenv:Header/>\n")
	b.WriteString("   <soapenv:Body>\n")
	b.WriteString(indented.String())
	b.WriteString("   </soapenv:Body>\n")
	b.WriteString("</soapenv:Envelope>")
	return b.String()
}

// isPlaceholder reports whether a generated body is only an explanatory
// comment/placeholder (i.e. nothing usable was produced).
func isPlaceholder(s string) bool {
	t := strings.TrimSpace(s)
	return t == "" || (strings.HasPrefix(t, "<!--") && strings.HasSuffix(t, "-->"))
}

// buildRPCRequest builds an RPC/encoded style request body: an operation
// wrapper element (namespace-qualified via prefix) carrying the SOAP encoding
// style, with one child per message part. Handles the legacy pattern where
// parts are declared as <part name="x" type="xs:string"/> instead of elements.
// Only the structural skeleton is produced (empty values, no business data).
func buildRPCRequest(opName, prefix, encodingStyle string, parts []wPart) string {
	if opName == "" {
		return "<!-- 无法确定操作名，请手动补全 -->"
	}
	qname := opName
	if prefix != "" {
		qname = prefix + ":" + opName
	}
	attr := ""
	if encodingStyle != "" {
		attr = fmt.Sprintf(` soapenv:encodingStyle="%s"`, encodingStyle)
	}
	var b strings.Builder
	b.WriteString("<" + qname + attr + ">\n")
	for _, p := range parts {
		name := p.Name
		if name == "" {
			name = "param"
		}
		if p.Type != "" {
			b.WriteString("   <" + name + ` xsi:type="xsd:` + localName(p.Type) + `">` + "</" + name + ">\n")
		} else {
			b.WriteString("   <" + name + "></" + name + ">\n")
		}
	}
	b.WriteString("</" + qname + ">")
	return b.String()
}
