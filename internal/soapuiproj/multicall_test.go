package soapuiproj

import "testing"

// multiCall mirrors the real-world SoapUI pattern where one operation keeps
// several named request samples (<con:call>), each with distinct parameters.
// Regression test: the importer must keep ALL calls and their titles, not just
// the first one.
const multiCall = `<?xml version="1.0" encoding="UTF-8"?>
<con:soapui-project name="aq" xmlns:con="http://eviware.com/soapui/config">
  <con:interface name="IHyWebServerbinding" type="wsdl">
    <con:endpoints><con:endpoint>http://localhost:8299/soap/IHyWebServer</con:endpoint></con:endpoints>
    <con:operation action="urn:HyWebServerIntf-IHyWebServer#HyWebServiceIntfMethod" name="HyWebServiceIntfMethod">
      <con:call name="5.6.5 物价查询(药品等)">
        <con:endpoint>http://localhost:8082/py-his/soap/IHyWebServer</con:endpoint>
        <con:request><![CDATA[<env:Envelope><env:Body><aXml><hywebserviceroot><funname>GetItemsPrice</funname><hospitalid>H1504080000001</hospitalid></hywebserviceroot></aXml></env:Body></env:Envelope>]]></con:request>
      </con:call>
      <con:call name="5.6.3 获取医院信息">
        <con:endpoint>http://localhost:8082/py-his/soap/IHyWebServer</con:endpoint>
        <con:request><![CDATA[<env:Envelope><env:Body><aXml><hywebserviceroot><funname>gethospitalinfo</funname></hywebserviceroot></aXml></env:Body></env:Envelope>]]></con:request>
      </con:call>
      <con:call name="5.6.4获取医院通用基础数据">
        <con:endpoint>http://localhost:8082/py-his/soap/IHyWebServer</con:endpoint>
        <con:request><![CDATA[<env:Envelope><env:Body><aXml><hywebserviceroot><funname>GetHosDataList_0</funname></hywebserviceroot></aXml></env:Body></env:Envelope>]]></con:request>
      </con:call>
    </con:operation>
  </con:interface>
</con:soapui-project>`

func TestMultipleCallsPreserved(t *testing.T) {
	p, err := Parse([]byte(multiCall))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(p.Services) != 1 || len(p.Services[0].Operations) != 1 {
		t.Fatalf("services/ops = %d/%d", len(p.Services), len(p.Services[0].Operations))
	}
	op := p.Services[0].Operations[0]
	if op.Name != "HyWebServiceIntfMethod" {
		t.Errorf("op name = %q", op.Name)
	}
	if len(op.Calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(op.Calls))
	}
	wantTitles := []string{"5.6.5 物价查询(药品等)", "5.6.3 获取医院信息", "5.6.4获取医院通用基础数据"}
	for i, want := range wantTitles {
		if op.Calls[i].Title != want {
			t.Errorf("call[%d].Title = %q, want %q", i, op.Calls[i].Title, want)
		}
	}
	// Each call must carry its own distinct body.
	if !contains(op.Calls[0].Request, "GetItemsPrice") {
		t.Errorf("call[0] body missing GetItemsPrice: %q", op.Calls[0].Request)
	}
	if !contains(op.Calls[1].Request, "gethospitalinfo") {
		t.Errorf("call[1] body missing gethospitalinfo: %q", op.Calls[1].Request)
	}
	if !contains(op.Calls[2].Request, "GetHosDataList_0") {
		t.Errorf("call[2] body missing GetHosDataList_0: %q", op.Calls[2].Request)
	}
	// Endpoint and action context preserved.
	if op.Calls[0].Endpoint != "http://localhost:8082/py-his/soap/IHyWebServer" {
		t.Errorf("endpoint = %q", op.Calls[0].Endpoint)
	}
	if op.Calls[0].Action != "urn:HyWebServerIntf-IHyWebServer#HyWebServiceIntfMethod" {
		t.Errorf("action = %q", op.Calls[0].Action)
	}
	// Default request mirrors first call.
	if op.Request != op.Calls[0].Request {
		t.Errorf("op.Request should mirror first call")
	}
}
