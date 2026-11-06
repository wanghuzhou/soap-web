package soapuiproj

import (
	"strings"
	"testing"
)

// gbkWSDL 是 "查询" 的 GBK 字节。
var gbkQuery = []byte{0xb2, 0xe9, 0xd1, 0xaf}

// TestParseGBKProject 回归：GBK 编码的 SoapUI 工程应能解析，中文名正确。
// 此前 charsetReader 只是"原样返回"（注释里自己写着 Best effort），
// 非 UTF-8 文档要么乱码，要么因 CharsetReader 缺失而失败。
func TestParseGBKProject(t *testing.T) {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="GBK"?>
<con:soapui-project id="p1" name="`)
	b.Write(gbkQuery)
	b.WriteString(`" xmlns:con="http://eviware.com/soapui/config">
  <con:interface id="i1" name="`)
	b.Write(gbkQuery)
	b.WriteString(`" type="wsdl">
    <con:endpoints><con:endpoint>http://x/svc</con:endpoint></con:endpoints>
    <con:operation id="op1" action="a" name="`)
	b.Write(gbkQuery)
	b.WriteString(`">
      <con:call id="c1" name="`)
	b.Write(gbkQuery)
	b.WriteString(`">
        <con:request id="r1" mediaType="text/xml">
          <con:request><![CDATA[<soapenv:Envelope><soapenv:Body><q>1</q></soapenv:Body></soapenv:Envelope>]]></con:request>
        </con:request>
      </con:call>
    </con:operation>
  </con:interface>
</con:soapui-project>`)

	p, err := Parse([]byte(b.String()))
	if err != nil {
		t.Fatalf("GBK 工程解析失败: %v", err)
	}
	if p.Name != "查询" {
		t.Errorf("工程名 = %q, want 查询", p.Name)
	}
	if len(p.Services) != 1 {
		t.Fatalf("services = %d", len(p.Services))
	}
	svc := p.Services[0]
	if svc.Name != "查询" {
		t.Errorf("服务名 = %q, want 查询", svc.Name)
	}
	if len(svc.Operations) != 1 {
		t.Fatalf("operations = %d", len(svc.Operations))
	}
	op := svc.Operations[0]
	if op.Name != "查询" {
		t.Errorf("操作名 = %q, want 查询", op.Name)
	}
	if len(op.Calls) != 1 {
		t.Fatalf("calls = %d", len(op.Calls))
	}
	if op.Calls[0].Title != "查询" {
		t.Errorf("请求样例标题 = %q, want 查询", op.Calls[0].Title)
	}
	// 请求体是 ASCII，必须完整保留。
	if !contains(op.Calls[0].Request, "<q>1</q>") {
		t.Errorf("请求体丢失: %q", op.Calls[0].Request)
	}
}

// TestParseUTF8ProjectStillWorks 确保既有 UTF-8 路径无回归。
func TestParseUTF8ProjectStillWorks(t *testing.T) {
	p, err := Parse([]byte(sample))
	if err != nil {
		t.Fatalf("UTF-8 工程解析失败: %v", err)
	}
	if p.Name != "DemoProject" {
		t.Errorf("name = %q", p.Name)
	}
}
