package wsdl

import (
	"strings"
	"testing"
)

// A document/literal wrapped WSDL with an embedded schema, modelled on the
// classic DNE online calculator service.
const calcWSDL = `<?xml version="1.0" encoding="utf-8"?>
<wsdl:definitions xmlns:wsdl="http://schemas.xmlsoap.org/wsdl/"
  xmlns:soap="http://schemas.xmlsoap.org/wsdl/soap/"
  xmlns:s="http://www.w3.org/2001/XMLSchema"
  xmlns:tns="http://tempuri.org/" targetNamespace="http://tempuri.org/">
  <wsdl:types>
    <s:schema elementFormDefault="qualified" targetNamespace="http://tempuri.org/">
      <s:element name="Add">
        <s:complexType>
          <s:sequence>
            <s:element minOccurs="1" maxOccurs="1" name="intA" type="s:int"/>
            <s:element minOccurs="1" maxOccurs="1" name="intB" type="s:int"/>
          </s:sequence>
        </s:complexType>
      </s:element>
      <s:element name="AddResponse">
        <s:complexType><s:sequence>
          <s:element minOccurs="0" maxOccurs="1" name="AddResult" type="s:int"/>
        </s:sequence></s:complexType>
      </s:element>
      <s:element name="Person">
        <s:complexType><s:sequence>
          <s:element name="name" type="s:string"/>
          <s:element name="age" type="s:int"/>
          <s:element name="address" type="tns:AddressType"/>
        </s:sequence></s:complexType>
      </s:element>
      <s:complexType name="AddressType">
        <s:sequence>
          <s:element name="street" type="s:string"/>
          <s:element name="city" type="s:string"/>
        </s:sequence>
        <s:attribute name="country" type="s:string"/>
      </s:complexType>
    </s:schema>
  </wsdl:types>
  <wsdl:message name="AddSoapIn"><wsdl:part name="parameters" element="tns:Add"/></wsdl:message>
  <wsdl:message name="AddSoapOut"><wsdl:part name="parameters" element="tns:AddResponse"/></wsdl:message>
  <wsdl:portType name="CalculatorSoap">
    <wsdl:operation name="Add">
      <wsdl:input message="tns:AddSoapIn"/>
      <wsdl:output message="tns:AddSoapOut"/>
    </wsdl:operation>
  </wsdl:portType>
  <wsdl:binding name="CalculatorSoap" type="tns:CalculatorSoap">
    <soap:binding transport="http://schemas.xmlsoap.org/soap/http"/>
    <wsdl:operation name="Add">
      <soap:operation soapAction="http://tempuri.org/Add" style="document"/>
      <wsdl:input><soap:body use="literal"/></wsdl:input>
      <wsdl:output><soap:body use="literal"/></wsdl:output>
    </wsdl:operation>
  </wsdl:binding>
  <wsdl:service name="Calculator">
    <wsdl:port name="CalculatorSoap" binding="tns:CalculatorSoap">
      <soap:address location="http://www.dneonline.com/calculator.asmx"/>
    </wsdl:port>
  </wsdl:service>
</wsdl:definitions>`

func TestParseCalc(t *testing.T) {
	res, err := Parse([]byte(calcWSDL))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(res.Operations) != 1 {
		t.Fatalf("ops = %d", len(res.Operations))
	}
	op := res.Operations[0]
	if op.Name != "Add" {
		t.Errorf("name = %q", op.Name)
	}
	if op.SoapAction != "http://tempuri.org/Add" {
		t.Errorf("action = %q", op.SoapAction)
	}
	if op.Endpoint != "http://www.dneonline.com/calculator.asmx" {
		t.Errorf("endpoint = %q", op.Endpoint)
	}
	if op.InputName != "Add" {
		t.Errorf("inputName = %q", op.InputName)
	}
	req := op.Request
	t.Logf("generated request:\n%s", req)
	// Elements are namespace-qualified with the tns: prefix; body is enveloped.
	for _, want := range []string{"<soapenv:Envelope", "<soapenv:Body>", `<tns:Add xmlns:tns="http://tempuri.org/">`, "<tns:intA>", "<tns:intB>", "</tns:Add>"} {
		if !strings.Contains(req, want) {
			t.Errorf("request missing %q:\n%s", want, req)
		}
	}
}

func TestNestedComplexType(t *testing.T) {
	// Build request for the Person element directly through the index.
	idx := newSchemaIndex(parseSchemasForTest(t, calcWSDL))
	out, _ := idx.buildRequest("Person", "http://tempuri.org/")
	t.Logf("person request:\n%s", out)
	for _, want := range []string{"<tns:name>", "<tns:age>", "<tns:address", "<tns:street>", "<tns:city>", `country=""`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
