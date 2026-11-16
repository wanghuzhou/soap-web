package wsdl

import (
	"encoding/xml"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"
)

// recursiveWSDL returns a valid WSDL whose request type references itself, the
// shape that used to make template generation recurse forever:
//
//	<complexType name="Node"><sequence><element name="child" type="Node"/></sequence></complexType>
//
// Real WSDLs produce this for trees (folder-in-folder, org units, categories).
func recursiveWSDL(typeBody string) string {
	return `<?xml version="1.0"?>
<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"
  xmlns:xsd="http://www.w3.org/2001/XMLSchema"
  xmlns:soap="http://schemas.xmlsoap.org/wsdl/soap/"
  targetNamespace="urn:t" name="S">
 <types><xsd:schema targetNamespace="urn:t">
   <xsd:element name="Node" type="Node"/>
   <xsd:complexType name="Node">` + typeBody + `</xsd:complexType>
 </xsd:schema></types>
 <message name="M"><part name="p" element="Node"/></message>
 <portType name="PT"><operation name="op"><input message="M"/></operation></portType>
 <binding name="B" type="PT"><soap:binding style="document"/>
   <operation name="op"/></binding>
 <service name="S"><port name="P" binding="B">
   <soap:address location="http://x/"/></port></service>
</definitions>`
}

// TestSelfReferencingTypeTerminates is the regression test for the infinite
// recursion: a type whose child is itself must produce a bounded, well-formed
// template instead of expanding until the process dies.
func TestSelfReferencingTypeTerminates(t *testing.T) {
	// A watchdog: if the guard is ever removed, this test hangs forever, so
	// fail loudly instead of stalling the suite.
	done := make(chan struct{})
	var res *Result
	var parseErr error
	go func() {
		defer close(done)
		res, parseErr = Parse([]byte(recursiveWSDL(
			`<xsd:sequence><xsd:element name="child" type="Node"/></xsd:sequence>`)))
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Parse did not terminate: recursive type expanded without bound")
	}
	if parseErr != nil {
		t.Fatalf("Parse: %v", parseErr)
	}

	body := res.Operations[0].Request
	if len(body) > maxTemplateBytes {
		t.Errorf("template is %d bytes, want <= %d", len(body), maxTemplateBytes)
	}
	if !strings.Contains(body, "递归类型") {
		t.Errorf("expected a cycle marker in the template, got:\n%s", body)
	}
	// Whatever we emit must still be parseable: the user edits and sends it.
	assertWellFormedXML(t, body)
}

// TestMutuallyRecursiveTypesTerminate covers A -> B -> A, which a
// single-type check would miss.
func TestMutuallyRecursiveTypesTerminate(t *testing.T) {
	doc := `<?xml version="1.0"?>
<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"
  xmlns:xsd="http://www.w3.org/2001/XMLSchema"
  xmlns:soap="http://schemas.xmlsoap.org/wsdl/soap/"
  targetNamespace="urn:t" name="S">
 <types><xsd:schema targetNamespace="urn:t">
   <xsd:element name="A" type="A"/>
   <xsd:complexType name="A">
     <xsd:sequence><xsd:element name="toB" type="B"/></xsd:sequence>
   </xsd:complexType>
   <xsd:complexType name="B">
     <xsd:sequence><xsd:element name="toA" type="A"/></xsd:sequence>
   </xsd:complexType>
 </xsd:schema></types>
 <message name="M"><part name="p" element="A"/></message>
 <portType name="PT"><operation name="op"><input message="M"/></operation></portType>
 <binding name="B" type="PT"><soap:binding style="document"/>
   <operation name="op"/></binding>
 <service name="S"><port name="P" binding="B">
   <soap:address location="http://x/"/></port></service>
</definitions>`

	done := make(chan struct{})
	var parseErr error
	go func() {
		defer close(done)
		_, parseErr = Parse([]byte(doc))
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Parse did not terminate for mutually recursive types")
	}
	if parseErr != nil {
		t.Fatalf("Parse: %v", parseErr)
	}
}

// TestDeeplyNestedSchemaIsCapped checks that a merely deep (not cyclic) schema
// stops at the depth cap rather than recursing to the stack limit.
func TestDeeplyNestedSchemaIsCapped(t *testing.T) {
	// T0 -> T1 -> ... -> T200, far past the cap.
	var types strings.Builder
	types.WriteString(`<xsd:element name="T0" type="T0"/>`)
	for i := 0; i < 200; i++ {
		types.WriteString(`<xsd:complexType name="T`)
		types.WriteString(strconv.Itoa(i))
		types.WriteString(`"><xsd:sequence><xsd:element name="next" type="T`)
		types.WriteString(strconv.Itoa(i + 1))
		types.WriteString(`"/></xsd:sequence></xsd:complexType>`)
	}
	doc := `<?xml version="1.0"?>
<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"
  xmlns:xsd="http://www.w3.org/2001/XMLSchema"
  xmlns:soap="http://schemas.xmlsoap.org/wsdl/soap/"
  targetNamespace="urn:t" name="S">
 <types><xsd:schema targetNamespace="urn:t">` + types.String() + `</xsd:schema></types>
 <message name="M"><part name="p" element="T0"/></message>
 <portType name="PT"><operation name="op"><input message="M"/></operation></portType>
 <binding name="B" type="PT"><soap:binding style="document"/>
   <operation name="op"/></binding>
 <service name="S"><port name="P" binding="B">
   <soap:address location="http://x/"/></port></service>
</definitions>`

	res, err := Parse([]byte(doc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	body := res.Operations[0].Request
	if got := strings.Count(body, "<tns:next"); got > maxElementDepth {
		t.Errorf("nesting = %d, want <= %d", got, maxElementDepth)
	}
	if !strings.Contains(body, "嵌套过深") {
		t.Errorf("expected a depth marker in the template, got:\n%s", body)
	}
	assertWellFormedXML(t, body)
}

// TestWideSchemaIsSizeCapped covers exponential width: each level has 12
// children, so depth alone would still produce a huge document.
func TestWideSchemaIsSizeCapped(t *testing.T) {
	var types strings.Builder
	types.WriteString(`<xsd:element name="W0" type="W0"/>`)
	for i := 0; i < 30; i++ {
		types.WriteString(`<xsd:complexType name="W`)
		types.WriteString(strconv.Itoa(i))
		types.WriteString(`"><xsd:sequence>`)
		for j := 0; j < 12; j++ {
			types.WriteString(`<xsd:element name="c`)
			types.WriteString(strconv.Itoa(j))
			types.WriteString(`" type="W`)
			types.WriteString(strconv.Itoa(i + 1))
			types.WriteString(`"/>`)
		}
		types.WriteString(`</xsd:sequence></xsd:complexType>`)
	}
	doc := `<?xml version="1.0"?>
<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"
  xmlns:xsd="http://www.w3.org/2001/XMLSchema"
  xmlns:soap="http://schemas.xmlsoap.org/wsdl/soap/"
  targetNamespace="urn:t" name="S">
 <types><xsd:schema targetNamespace="urn:t">` + types.String() + `</xsd:schema></types>
 <message name="M"><part name="p" element="W0"/></message>
 <portType name="PT"><operation name="op"><input message="M"/></operation></portType>
 <binding name="B" type="PT"><soap:binding style="document"/>
   <operation name="op"/></binding>
 <service name="S"><port name="P" binding="B">
   <soap:address location="http://x/"/></port></service>
</definitions>`

	res, err := Parse([]byte(doc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	body := res.Operations[0].Request
	// Generous ceiling: the cap applies to the payload, the envelope adds a
	// fixed header, and the "too large" comment can replace the whole thing.
	if len(body) > maxTemplateBytes*2 {
		t.Errorf("template is %d bytes, want the payload capped near %d", len(body), maxTemplateBytes)
	}
}

// TestSiblingReuseIsNotACycle makes sure the cycle guard does not fire on the
// ordinary case where the same type appears twice as siblings.
func TestSiblingReuseIsNotACycle(t *testing.T) {
	doc := `<?xml version="1.0"?>
<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"
  xmlns:xsd="http://www.w3.org/2001/XMLSchema"
  xmlns:soap="http://schemas.xmlsoap.org/wsdl/soap/"
  targetNamespace="urn:t" name="S">
 <types><xsd:schema targetNamespace="urn:t">
   <xsd:element name="Order" type="Order"/>
   <xsd:complexType name="Order">
     <xsd:sequence>
       <xsd:element name="billTo" type="Address"/>
       <xsd:element name="shipTo" type="Address"/>
     </xsd:sequence>
   </xsd:complexType>
   <xsd:complexType name="Address">
     <xsd:sequence><xsd:element name="city" type="xsd:string"/></xsd:sequence>
   </xsd:complexType>
 </xsd:schema></types>
 <message name="M"><part name="p" element="Order"/></message>
 <portType name="PT"><operation name="op"><input message="M"/></operation></portType>
 <binding name="B" type="PT"><soap:binding style="document"/>
   <operation name="op"/></binding>
 <service name="S"><port name="P" binding="B">
   <soap:address location="http://x/"/></port></service>
</definitions>`

	res, err := Parse([]byte(doc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	body := res.Operations[0].Request
	if strings.Contains(body, "递归类型") || strings.Contains(body, "嵌套过深") {
		t.Errorf("sibling reuse was mistaken for recursion:\n%s", body)
	}
	for _, want := range []string{"<billTo>", "<shipTo>", "<city>"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}

// assertWellFormedXML fails when s does not parse as XML. A template is meant to
// be edited and sent, so a truncated tree must still be a complete document.
func assertWellFormedXML(t *testing.T, s string) {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(s))
	dec.Strict = false
	for {
		_, err := dec.Token()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatalf("generated template is not well-formed XML: %v\n%s", err, s)
		}
	}
}
