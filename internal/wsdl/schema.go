// Package wsdl: schema index + request template generation.
package wsdl

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// schemaElem mirrors a <xs:schema> element.
type schemaElem struct {
	XMLName      xml.Name        `xml:"schema"`
	TargetNS     string          `xml:"targetNamespace,attr"`
	ElementForm  string          `xml:"elementFormDefault,attr"`
	Elements     []xsElement     `xml:"element"`
	ComplexTypes []xsComplexType `xml:"complexType"`
	SimpleTypes  []xsSimpleType  `xml:"simpleType"`
}

type xsElement struct {
	Name      string         `xml:"name,attr"`
	Type      string         `xml:"type,attr"`
	Ref       string         `xml:"ref,attr"`
	MinOccurs string         `xml:"minOccurs,attr"`
	MaxOccurs string         `xml:"maxOccurs,attr"`
	Nillable  string         `xml:"nillable,attr"`
	Complex   *xsComplexType `xml:"complexType"`
	Simple    *xsSimpleType  `xml:"simpleType"`
}

type xsComplexType struct {
	Name           string            `xml:"name,attr"`
	Sequence       *xsGroup          `xml:"sequence"`
	All            *xsGroup          `xml:"all"`
	Choice         *xsGroup          `xml:"choice"`
	Attributes     []xsAttribute     `xml:"attribute"`
	ComplexContent *xsComplexContent `xml:"complexContent"`
	SimpleContent  *xsSimpleContent  `xml:"simpleContent"`
}

type xsComplexContent struct {
	Extension   *xsExtension `xml:"extension"`
	Restriction *xsExtension `xml:"restriction"`
}

type xsSimpleContent struct {
	Extension   *xsExtension `xml:"extension"`
	Restriction *xsExtension `xml:"restriction"`
}

type xsExtension struct {
	Base     string   `xml:"base,attr"`
	Sequence *xsGroup `xml:"sequence"`
	All      *xsGroup `xml:"all"`
	Choice   *xsGroup `xml:"choice"`
}

type xsGroup struct {
	Elements  []xsElement `xml:"element"`
	Sequences []*xsGroup  `xml:"sequence"`
	Choices   []*xsGroup  `xml:"choice"`
	Any       []xsAny     `xml:"any"`
}

type xsAny struct {
	MinOccurs string `xml:"minOccurs,attr"`
}

type xsAttribute struct {
	Name string `xml:"name,attr"`
	Type string `xml:"type,attr"`
}

type xsSimpleType struct {
	Name        string        `xml:"name,attr"`
	Restriction xsRestriction `xml:"restriction"`
}

type xsRestriction struct {
	Base         string   `xml:"base,attr"`
	Enumerations []xsEnum `xml:"enumeration"`
}

type xsEnum struct {
	Value string `xml:"value,attr"`
}

// schemaIndex holds all schemas flattened for lookup by element/type name.
type schemaIndex struct {
	elements map[string]xsElement
	types    map[string]xsComplexType
	simples  map[string]xsSimpleType
	rootNS   map[string]string // name -> targetNamespace (for prefix hints)
}

func newSchemaIndex(schemas []schemaElem) *schemaIndex {
	idx := &schemaIndex{
		elements: map[string]xsElement{},
		types:    map[string]xsComplexType{},
		simples:  map[string]xsSimpleType{},
		rootNS:   map[string]string{},
	}
	for _, s := range schemas {
		for _, e := range s.Elements {
			if e.Name != "" {
				idx.elements[e.Name] = e
				idx.rootNS[e.Name] = s.TargetNS
			}
		}
		for _, t := range s.ComplexTypes {
			if t.Name != "" {
				idx.types[t.Name] = t
			}
		}
		for _, st := range s.SimpleTypes {
			if st.Name != "" {
				idx.simples[st.Name] = st
			}
		}
	}
	return idx
}

// buildRequest generates an XML request template for the given root element.
func (idx *schemaIndex) buildRequest(rootElem, targetNS string) string {
	if rootElem == "" {
		return "<!-- 未能确定请求根元素，请手动编写 -->"
	}
	el, ok := idx.elements[rootElem]
	if !ok {
		// root not found in embedded schema; emit a minimal wrapper guess.
		return fmt.Sprintf(`<!-- 未找到根元素 %s 的 schema 定义，请手动补全 -->`, rootElem)
	}
	var b strings.Builder
	idx.writeElement(&b, el, rootElem, targetNS, 0)
	return b.String()
}

func (idx *schemaIndex) writeElement(b *strings.Builder, el xsElement, name, ns string, depth int) {
	indent := strings.Repeat("  ", depth)
	// Resolve element's type.
	ct := el.Complex
	typeName := localName(el.Type)
	if ct == nil && typeName != "" {
		if t, ok := idx.types[typeName]; ok {
			ct = &t
		}
	}

	if ct == nil {
		// simple type element: emit with a placeholder value.
		b.WriteString(fmt.Sprintf("%s<%s>%s</%s>\n", indent, name, sampleValue(typeName, el.Simple), name))
		return
	}

	// Complex type: gather child elements.
	children, attrs := idx.collectMembers(ct, 0)
	if len(children) == 0 && len(attrs) == 0 {
		b.WriteString(fmt.Sprintf("%s<%s/>\n", indent, name))
		return
	}
	attrStr := ""
	for _, a := range attrs {
		attrStr += fmt.Sprintf(` %s=""`, a)
	}
	b.WriteString(fmt.Sprintf("%s<%s%s>\n", indent, name, attrStr))
	for _, ch := range children {
		idx.writeElement(b, ch, localName(ch.Name), ns, depth+1)
	}
	b.WriteString(fmt.Sprintf("%s</%s>\n", indent, name))
}

// collectMembers resolves all element members of a complexType, following
// extension bases up to a bounded depth. Returns child xsElements and attr names.
func (idx *schemaIndex) collectMembers(ct *xsComplexType, depth int) ([]xsElement, []string) {
	if ct == nil || depth > 12 {
		return nil, nil
	}
	var elems []xsElement
	var attrs []string
	for _, a := range ct.Attributes {
		if a.Name != "" {
			attrs = append(attrs, a.Name)
		}
	}
	groups := []*xsGroup{ct.Sequence, ct.All, ct.Choice}
	for _, g := range groups {
		if g != nil {
			elems = append(elems, flattenGroup(g)...)
		}
	}
	// Handle extension inheritance.
	var ext *xsExtension
	if ct.ComplexContent != nil {
		if ct.ComplexContent.Extension != nil {
			ext = ct.ComplexContent.Extension
		} else if ct.ComplexContent.Restriction != nil {
			ext = ct.ComplexContent.Restriction
		}
	} else if ct.SimpleContent != nil {
		if ct.SimpleContent.Extension != nil {
			ext = ct.SimpleContent.Extension
		} else if ct.SimpleContent.Restriction != nil {
			ext = ct.SimpleContent.Restriction
		}
	}
	if ext != nil {
		for _, g := range []*xsGroup{ext.Sequence, ext.All, ext.Choice} {
			if g != nil {
				elems = append(elems, flattenGroup(g)...)
			}
		}
		base := localName(ext.Base)
		if bt, ok := idx.types[base]; ok {
			be, ba := idx.collectMembers(&bt, depth+1)
			elems = append(be, elems...) // base members come first
			attrs = append(attrs, ba...)
		}
	}
	return elems, attrs
}

// flattenGroup recursively flattens sequence/choice groups into element list.
func flattenGroup(g *xsGroup) []xsElement {
	if g == nil {
		return nil
	}
	var out []xsElement
	out = append(out, g.Elements...)
	for _, s := range g.Sequences {
		out = append(out, flattenGroup(s)...)
	}
	for _, c := range g.Choices {
		// For a choice, generate the first option only (a valid representative).
		sub := flattenGroup(c)
		if len(sub) > 0 {
			out = append(out, sub[0])
		}
	}
	return out
}

// sampleValue produces a placeholder value for an atomic type.
func sampleValue(typeName string, st *xsSimpleType) string {
	if st != nil && len(st.Restriction.Enumerations) > 0 {
		return st.Restriction.Enumerations[0].Value
	}
	ln := localName(typeName)
	if st != nil && st.Restriction.Base != "" {
		ln = localName(st.Restriction.Base)
	}
	switch ln {
	case "int", "integer", "long", "short", "byte", "decimal", "double", "float",
		"unsignedInt", "unsignedLong", "positiveInteger", "nonNegativeInteger":
		return "0"
	case "boolean":
		return "false"
	case "date":
		return "2024-01-01"
	case "dateTime":
		return "2024-01-01T00:00:00"
	case "time":
		return "00:00:00"
	}
	return ""
}
