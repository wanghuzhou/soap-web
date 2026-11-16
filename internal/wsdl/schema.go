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
	elements      map[string]xsElement
	types         map[string]xsComplexType
	simples       map[string]xsSimpleType
	rootNS        map[string]string // name -> targetNamespace
	elemQualified map[string]bool   // name -> elementFormDefault=qualified
}

func newSchemaIndex(schemas []schemaElem) *schemaIndex {
	idx := &schemaIndex{
		elements:      map[string]xsElement{},
		types:         map[string]xsComplexType{},
		simples:       map[string]xsSimpleType{},
		rootNS:        map[string]string{},
		elemQualified: map[string]bool{},
	}
	for _, s := range schemas {
		qualified := strings.EqualFold(strings.TrimSpace(s.ElementForm), "qualified")
		for _, e := range s.Elements {
			if e.Name != "" {
				idx.elements[e.Name] = e
				idx.rootNS[e.Name] = s.TargetNS
				idx.elemQualified[e.Name] = qualified
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

// maxElementDepth bounds how deep the generated template may nest. Recursive
// and mutually-recursive schema types are common in real WSDLs (a tree node
// whose child is the same type, folder-in-folder, ...), and element recursion
// has no natural end: unbounded, it recursed forever and exhausted the stack,
// which on Windows kills the process outright instead of failing one request.
const maxElementDepth = 16

// maxTemplateBytes bounds the size of one generated template. Depth alone does
// not bound the output: a wide schema (a type with N children, each with N
// children) grows exponentially with depth. Once the budget is spent the writer
// stops emitting and the caller trims the document to a comment, so a hostile
// or merely huge WSDL cannot turn into a multi-megabyte project.
const maxTemplateBytes = 1 << 20 // 1 MB

// buildRequest generates an XML request template for the given root element.
// It returns the payload and the prefix bound to targetNS ("" when no
// namespace is used). The root element is namespace-qualified; descendants are
// prefixed only when the schema declares elementFormDefault="qualified".
func (idx *schemaIndex) buildRequest(rootElem, targetNS string) (string, string) {
	if rootElem == "" {
		return "<!-- 未能确定请求根元素，请手动编写 -->", ""
	}
	el, ok := idx.elements[rootElem]
	if !ok {
		// root not found in embedded schema; emit a minimal wrapper guess.
		return fmt.Sprintf(`<!-- 未找到根元素 %s 的 schema 定义，请手动补全 -->`, rootElem), ""
	}
	prefix := ""
	rootQName := rootElem
	if targetNS != "" {
		prefix = "tns"
		rootQName = prefix + ":" + rootElem
	}
	qualified := idx.elemQualified[rootElem]
	var b strings.Builder
	w := &templateWriter{b: &b, visiting: map[string]bool{}}
	idx.writeRoot(w, el, rootQName, prefix, targetNS, qualified, 0)
	if b.Len() > maxTemplateBytes {
		// Truncated output is not a usable template; say so rather than save
		// half a document that looks editable but is broken XML.
		return fmt.Sprintf("<!-- 请求结构过大（超过 %d KB），已省略，请手动编写 -->", maxTemplateBytes>>10), prefix
	}
	return b.String(), prefix
}

// templateWriter carries the state shared by one template expansion: the
// output buffer, the types currently being expanded (for cycle detection) and
// the remaining size budget.
type templateWriter struct {
	b        *strings.Builder
	visiting map[string]bool
}

// writeRoot writes the request's root element, declaring xmlns:<prefix> inline
// when a namespace is present.
func (idx *schemaIndex) writeRoot(w *templateWriter, el xsElement, qname, prefix, targetNS string, qualified bool, depth int) {
	nsDecl := ""
	if prefix != "" && targetNS != "" {
		nsDecl = fmt.Sprintf(` xmlns:%s="%s"`, prefix, targetNS)
	}
	idx.writeElement(w, el, qname, nsDecl, prefix, qualified, depth)
}

// writeElement emits one element and its subtree. name is the qualified name to
// write and nsDecl is an optional inline namespace declaration (root only).
//
// Two guards keep a recursive or pathologically wide schema from running away:
// depth is capped, and a type already on the current expansion path is not
// expanded again (that is exactly what a cycle looks like). A truncated branch
// emits a comment, so the result stays well-formed XML the user can edit.
func (idx *schemaIndex) writeElement(w *templateWriter, el xsElement, name, nsDecl, prefix string, qualified bool, depth int) {
	if w.b.Len() >= maxTemplateBytes {
		return
	}
	indent := strings.Repeat("  ", depth)
	ct := el.Complex
	typeName := localName(el.Type)
	if ct == nil && typeName != "" {
		if t, ok := idx.types[typeName]; ok {
			ct = &t
		}
	}
	if ct == nil {
		w.b.WriteString(fmt.Sprintf("%s<%s%s>%s</%s>\n", indent, name, nsDecl, sampleValue(typeName, el.Simple), name))
		return
	}

	// A complex type nested inline has no name of its own, so it is keyed by
	// the element it was declared on; a named type is keyed by its name. Either
	// way the key identifies "which type are we inside right now".
	key := typeName
	if key == "" {
		key = el.Name
	}

	children, attrs := idx.collectMembers(ct, 0)
	attrStr := ""
	for _, a := range attrs {
		attrStr += fmt.Sprintf(` %s=""`, a)
	}
	if len(children) == 0 && len(attrs) == 0 {
		w.b.WriteString(fmt.Sprintf("%s<%s%s/>\n", indent, name, nsDecl))
		return
	}

	budgetExceeded := depth+1 >= maxElementDepth || w.visiting[key]
	w.b.WriteString(fmt.Sprintf("%s<%s%s%s>\n", indent, name, nsDecl, attrStr))
	if budgetExceeded {
		reason := "嵌套过深"
		if w.visiting[key] {
			reason = "递归类型"
		}
		w.b.WriteString(fmt.Sprintf("%s  <!-- %s，子节点已省略，请按需补全 -->\n", indent, reason))
	} else {
		if key != "" {
			w.visiting[key] = true
		}
		for _, ch := range children {
			idx.writeChild(w, ch, prefix, qualified, depth+1)
		}
		if key != "" {
			delete(w.visiting, key)
		}
	}
	w.b.WriteString(fmt.Sprintf("%s</%s>\n", indent, name))
}

// writeChild writes a descendant element. When the schema is qualified the
// element name carries the same prefix as the root; otherwise it is plain.
func (idx *schemaIndex) writeChild(w *templateWriter, el xsElement, prefix string, qualified bool, depth int) {
	local := localName(el.Name)
	if local == "" {
		return
	}
	name := local
	if qualified && prefix != "" {
		name = prefix + ":" + local
	}
	idx.writeElement(w, el, name, "", prefix, qualified, depth)
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
