package wsdl

import (
	"strings"
	"testing"
)

// A minimal RPC/encoded WSDL: the input message declares a typed part
// (<part name="aXml" type="xs:string"/>) rather than an element, which the
// document/literal generator cannot resolve. The generator must fall back to
// an RPC-style wrapper containing that part.
const rpcWSDL = `<?xml version="1.0" encoding="utf-8"?>
<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"
  xmlns:soap="http://schemas.xmlsoap.org/wsdl/soap/"
  xmlns:xs="http://www.w3.org/2001/XMLSchema"
  xmlns:tns="urn:Demo" targetNamespace="urn:Demo">
  <message name="Op0Request"><part name="aXml" type="xs:string"/></message>
  <message name="Op0Response"><part name="return" type="xs:string"/></message>
  <portType name="DemoPort">
    <operation name="DoThing">
      <input message="tns:Op0Request"/>
      <output message="tns:Op0Response"/>
    </operation>
  </portType>
  <binding name="DemoBinding" type="tns:DemoPort">
    <soap:binding transport="http://schemas.xmlsoap.org/soap/http" style="rpc"/>
    <operation name="DoThing">
      <soap:operation soapAction="urn:DoThing" style="rpc"/>
      <input><soap:body use="encoded" namespace="urn:Demo" encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"/></input>
    </operation>
  </binding>
  <service name="Demo">
    <port name="DemoPort" binding="tns:DemoBinding">
      <soap:address location="http://localhost/x"/>
    </port>
  </service>
</definitions>`

func TestParseRPCEncoded(t *testing.T) {
	res, err := Parse([]byte(rpcWSDL))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(res.Operations) != 1 {
		t.Fatalf("ops = %d", len(res.Operations))
	}
	req := res.Operations[0].Request
	t.Logf("rpc request:\n%s", req)
	if strings.Contains(req, "未找到根元素") {
		t.Errorf("should not emit placeholder: %s", req)
	}
	for _, want := range []string{
		`<soapenv:Envelope`,
		`xmlns:urn="urn:Demo"`,
		`<urn:DoThing`,
		`soapenv:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"`,
		`<aXml xsi:type="xsd:string">`,
		`</urn:DoThing>`,
	} {
		if !strings.Contains(req, want) {
			t.Errorf("rpc request missing %q:\n%s", want, req)
		}
	}
}

func TestIsPlaceholder(t *testing.T) {
	if !isPlaceholder("<!-- 未找到根元素 x -->") {
		t.Error("comment should be placeholder")
	}
	if !isPlaceholder("   ") {
		t.Error("blank should be placeholder")
	}
	if isPlaceholder("<Add/>") {
		t.Error("real element is not placeholder")
	}
}
