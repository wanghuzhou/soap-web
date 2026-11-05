package soapuiproj

import "testing"

const sample = `<?xml version="1.0" encoding="UTF-8"?>
<con:soapui-project id="abc" activeEnvironment="Default" name="DemoProject" soapui-version="5.7.0" xmlns:con="http://eviware.com/soapui/config">
  <con:interface xsi:type="con:WsdlInterface" id="i1" name="CalculatorSoap" type="wsdl" bindingName="{http://tempuri.org/}CalculatorSoap" soapVersion="1_1">
    <con:definitionCache type="org.apache.xmlbeans.impl.schema.DocumentFactoryImpl"/>
    <con:endpoints>
      <con:endpoint>http://www.dneonline.com/calculator.asmx</con:endpoint>
    </con:endpoints>
    <con:operation id="op1" isOneWay="false" action="http://tempuri.org/Add" name="Add" bindingOperationName="Add" type="Request-Response" outputName="AddResponse">
      <con:call id="c1" name="Add">
        <con:request id="r1" name="Add" mediaType="text/xml" postQueryString="false">
          <con:endpoint>http://www.dneonline.com/calculator.asmx</con:endpoint>
          <con:request><![CDATA[<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/" xmlns:tem="http://tempuri.org/">
   <soapenv:Header/>
   <soapenv:Body>
      <tem:Add>
         <tem:intA>1</tem:intA>
         <tem:intB>2</tem:intB>
      </tem:Add>
   </soapenv:Body>
</soapenv:Envelope>]]></con:request>
        </con:request>
      </con:call>
    </con:operation>
  </con:interface>
  <con:testSuite id="ts1" name="TestSuite 1">
    <con:testCase id="tc1" name="TestCase 1">
      <con:testStep type="request" name="Add Request">
        <con:config>
          <con:request xmlns:con="http://eviware.com/soapui/config">
            <con:endpoint>http://www.dneonline.com/calculator.asmx</con:endpoint>
            <con:request><![CDATA[<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/"><soapenv:Body><tem:Add xmlns:tem="http://tempuri.org/"><tem:intA>3</tem:intA></tem:Add></soapenv:Body></soapenv:Envelope>]]></con:request>
          </con:request>
        </con:config>
      </con:testStep>
    </con:testCase>
  </con:testSuite>
</con:soapui-project>`

func TestParseSample(t *testing.T) {
	p, err := Parse([]byte(sample))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.Name != "DemoProject" {
		t.Errorf("name = %q", p.Name)
	}
	if len(p.Services) != 1 || len(p.Services[0].Operations) != 1 {
		t.Fatalf("services/ops = %d/%d", len(p.Services), len(p.Services[0].Operations))
	}
	op := p.Services[0].Operations[0]
	if op.Name != "Add" || op.SoapAction != "http://tempuri.org/Add" {
		t.Errorf("op = %+v", op)
	}
	if op.Endpoint != "http://www.dneonline.com/calculator.asmx" {
		t.Errorf("endpoint = %q", op.Endpoint)
	}
	if !contains(op.Request, "<tem:intA>1</tem:intA>") {
		t.Errorf("request body not extracted: %q", op.Request)
	}
	if len(p.TestSuites) != 1 || len(p.TestSuites[0].TestCases) != 1 {
		t.Fatalf("testsuites not parsed")
	}
	step := p.TestSuites[0].TestCases[0].Steps[0]
	if !contains(step.Request, "<tem:intA>3</tem:intA>") {
		t.Errorf("teststep request not extracted: %q", step.Request)
	}
}

func TestRejectsNonSoapUI(t *testing.T) {
	if _, err := Parse([]byte(`<foo/>`)); err == nil {
		t.Fatal("expected error for non-soapui root")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
