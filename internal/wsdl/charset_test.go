package wsdl

import (
	"strings"
	"testing"
)

// gbkWSDL 是一份声明 encoding="GBK" 且操作名含中文的 WSDL（GBK 字节）。
// 此前 wsdl.Parse 未设置 CharsetReader，这类文档会直接解析失败：
//
//	xml: encoding "GBK" declared but Decoder.CharsetReader is nil
func gbkWSDL() []byte {
	// "查询" 的 GBK 字节是 0xb2,0xe9,0xd1,0xaf
	const head = `<?xml version="1.0" encoding="GBK"?>
<definitions name="Svc" targetNamespace="http://x.com/svc"
  xmlns="http://schemas.xmlsoap.org/wsdl/"
  xmlns:soap="http://schemas.xmlsoap.org/wsdl/soap/"
  xmlns:tns="http://x.com/svc"
  xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <types>
    <xs:schema targetNamespace="http://x.com/svc" elementFormDefault="qualified">
      <xs:element name="Req"><xs:complexType><xs:sequence>
        <xs:element name="id" type="xs:string"/>
      </xs:sequence></xs:complexType></xs:element>
    </xs:schema>
  </types>
  <message name="In"><part name="p" element="tns:Req"/></message>
  <portType name="PT"><operation name="`
	const tail = `"><input message="tns:In"/></operation></portType>
  <binding name="B" type="tns:PT">
    <soap:binding style="document" transport="http://schemas.xmlsoap.org/soap/http"/>
    <operation name="`
	const tail2 = `"><soap:operation soapAction="a"/><input><soap:body use="literal"/></input></operation>
  </binding>
  <service name="S"><port name="P" binding="tns:B"><soap:address location="http://x/a"/></port></service>
</definitions>`

	cn := []byte{0xb2, 0xe9, 0xd1, 0xaf} // 查询
	var b strings.Builder
	b.WriteString(head)
	b.Write(cn)
	b.WriteString(tail)
	b.Write(cn)
	b.WriteString(tail2)
	return []byte(b.String())
}

// TestParseGBKWSDL 回归：GBK 编码的 WSDL 应能解析，且中文操作名正确。
func TestParseGBKWSDL(t *testing.T) {
	res, err := Parse(gbkWSDL())
	if err != nil {
		t.Fatalf("GBK WSDL 解析失败: %v", err)
	}
	if len(res.Operations) == 0 {
		t.Fatal("未解析出任何 operation")
	}
	found := false
	for _, op := range res.Operations {
		if op.Name == "查询" {
			found = true
		}
	}
	if !found {
		names := make([]string, 0, len(res.Operations))
		for _, op := range res.Operations {
			names = append(names, op.Name)
		}
		t.Fatalf("操作名未被正确解码，实际 = %v", names)
	}
}

// TestParseUTF8WSDLStillWorks 确保 UTF-8 文档不受字符集改动影响。
func TestParseUTF8WSDLStillWorks(t *testing.T) {
	utf8Doc := strings.Replace(
		string(gbkWSDL()),
		`encoding="GBK"`, `encoding="UTF-8"`, 1,
	)
	// 把 GBK 字节换回 UTF-8 文本。
	utf8Doc = strings.ReplaceAll(utf8Doc, string([]byte{0xb2, 0xe9, 0xd1, 0xaf}), "查询")

	res, err := Parse([]byte(utf8Doc))
	if err != nil {
		t.Fatalf("UTF-8 WSDL 解析失败: %v", err)
	}
	if len(res.Operations) != 1 || res.Operations[0].Name != "查询" {
		t.Fatalf("operations = %+v", res.Operations)
	}
}

// TestParseRejectsNonWSDL 保持原有校验行为。
func TestParseRejectsNonWSDL(t *testing.T) {
	if _, err := Parse([]byte(`<?xml version="1.0"?><foo/>`)); err == nil {
		t.Fatal("非 WSDL 文档应返回错误")
	}
}
