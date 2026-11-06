package wsdl

import (
	"strings"
	"testing"
)

// multiBindingWSDL 声明两个 portType、两个 binding（SOAP 1.1 / SOAP 1.2），
// 且各 binding 暴露的操作集合不同 —— 用于同时验证顺序稳定与去重。
const multiBindingWSDL = `<?xml version="1.0" encoding="UTF-8"?>
<definitions name="S" targetNamespace="http://x.com/s"
  xmlns="http://schemas.xmlsoap.org/wsdl/"
  xmlns:soap="http://schemas.xmlsoap.org/wsdl/soap/"
  xmlns:soap12="http://schemas.xmlsoap.org/wsdl/soap12/"
  xmlns:tns="http://x.com/s"
  xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <types>
    <xs:schema targetNamespace="http://x.com/s" elementFormDefault="qualified">
      <xs:element name="Req"><xs:complexType><xs:sequence>
        <xs:element name="id" type="xs:string"/>
      </xs:sequence></xs:complexType></xs:element>
    </xs:schema>
  </types>
  <message name="M"><part name="p" element="tns:Req"/></message>
  <portType name="PT_A">
    <operation name="Alpha"><input message="tns:M"/></operation>
    <operation name="Beta"><input message="tns:M"/></operation>
  </portType>
  <portType name="PT_B">
    <operation name="Gamma"><input message="tns:M"/></operation>
  </portType>
  <binding name="BindA11" type="tns:PT_A">
    <soap:binding style="document" transport="http://schemas.xmlsoap.org/soap/http"/>
    <operation name="Alpha"><soap:operation soapAction="Alpha"/><input><soap:body use="literal"/></input></operation>
    <operation name="Beta"><soap:operation soapAction="Beta"/><input><soap:body use="literal"/></input></operation>
  </binding>
  <binding name="BindB12" type="tns:PT_B">
    <soap12:binding style="document" transport="http://schemas.xmlsoap.org/soap/http"/>
    <operation name="Gamma"><soap12:operation soapAction="Gamma"/><input><soap12:body use="literal"/></input></operation>
  </binding>
  <service name="S">
    <port name="PA" binding="tns:BindA11"><soap:address location="http://x/a"/></port>
    <port name="PB" binding="tns:BindB12"><soap12:address location="http://x/b"/></port>
  </service>
</definitions>`

// dualProtocolWSDL 是同一 portType 被两个 binding 同时暴露的常见写法
// （SOAP 1.1 + SOAP 1.2 双栈）。此前每个操作会被重复解析两次。
const dualProtocolWSDL = `<?xml version="1.0" encoding="UTF-8"?>
<definitions name="D" targetNamespace="http://x.com/d"
  xmlns="http://schemas.xmlsoap.org/wsdl/"
  xmlns:soap="http://schemas.xmlsoap.org/wsdl/soap/"
  xmlns:soap12="http://schemas.xmlsoap.org/wsdl/soap12/"
  xmlns:tns="http://x.com/d"
  xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <types>
    <xs:schema targetNamespace="http://x.com/d" elementFormDefault="qualified">
      <xs:element name="Req"><xs:complexType><xs:sequence>
        <xs:element name="n" type="xs:int"/>
      </xs:sequence></xs:complexType></xs:element>
    </xs:schema>
  </types>
  <message name="M"><part name="p" element="tns:Req"/></message>
  <portType name="PT">
    <operation name="Query"><input message="tns:M"/></operation>
  </portType>
  <binding name="Soap11" type="tns:PT">
    <soap:binding style="document" transport="http://schemas.xmlsoap.org/soap/http"/>
    <operation name="Query"><soap:operation soapAction="Query"/><input><soap:body use="literal"/></input></operation>
  </binding>
  <binding name="Soap12" type="tns:PT">
    <soap12:binding style="document" transport="http://schemas.xmlsoap.org/soap/http"/>
    <operation name="Query"><soap12:operation soapAction="Query"/><input><soap12:body use="literal"/></input></operation>
  </binding>
  <service name="S">
    <port name="P11" binding="tns:Soap11"><soap:address location="http://x/11"/></port>
    <port name="P12" binding="tns:Soap12"><soap12:address location="http://x/12"/></port>
  </service>
</definitions>`

// TestParseOrderIsDeterministic 回归：解析曾遍历 bindByQName 这个 map，
// 导致同一份 WSDL 每次解析出的 operation 顺序可能不同（实测 60 次里
// 有 7 次顺序翻转），进而使工程树顺序不稳定、导出结果不可复现。
func TestParseOrderIsDeterministic(t *testing.T) {
	var first []string
	seen := map[string]int{}
	for i := 0; i < 60; i++ {
		res, err := Parse([]byte(multiBindingWSDL))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		names := make([]string, 0, len(res.Operations))
		for _, op := range res.Operations {
			names = append(names, op.Name)
		}
		key := strings.Join(names, ",")
		seen[key]++
		if i == 0 {
			first = names
		}
	}
	if len(seen) != 1 {
		t.Fatalf("顺序不稳定，出现 %d 种: %v", len(seen), seen)
	}
	// 且必须保持 WSDL 中的声明顺序。
	want := []string{"Alpha", "Beta", "Gamma"}
	if strings.Join(first, ",") != strings.Join(want, ",") {
		t.Fatalf("顺序 = %v, want %v（应保持声明顺序）", first, want)
	}
}

// TestParseDeduplicatesAcrossBindings 回归：同一 portType 被 SOAP 1.1 与
// SOAP 1.2 两个 binding 暴露时，操作曾被重复解析（3 个操作变 6 条，
// 名字完全相同，用户无法分辨）。
func TestParseDeduplicatesAcrossBindings(t *testing.T) {
	res, err := Parse([]byte(dualProtocolWSDL))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(res.Operations) != 1 {
		names := make([]string, 0, len(res.Operations))
		for _, op := range res.Operations {
			names = append(names, op.Name)
		}
		t.Fatalf("解析出 %d 个 operation %v，应去重为 1 个", len(res.Operations), names)
	}
	op := res.Operations[0]
	if op.Name != "Query" {
		t.Errorf("name = %q", op.Name)
	}
	// 备选 binding 必须被记录，否则信息丢失。
	if len(op.Bindings) != 2 {
		t.Fatalf("bindings = %v，应记录两个备选 binding", op.Bindings)
	}
	if op.Bindings[0] != "Soap11" || op.Bindings[1] != "Soap12" {
		t.Errorf("bindings = %v，应按声明顺序为 [Soap11 Soap12]", op.Bindings)
	}
	// 首个 binding 的 endpoint 生效。
	if op.Endpoint != "http://x/11" {
		t.Errorf("endpoint = %q, want http://x/11", op.Endpoint)
	}
}

// TestParseRecordsSingleBinding 单 binding 时也记录自身。
func TestParseRecordsSingleBinding(t *testing.T) {
	res, err := Parse([]byte(multiBindingWSDL))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, op := range res.Operations {
		if len(op.Bindings) != 1 {
			t.Errorf("%s: bindings = %v, want 1 项", op.Name, op.Bindings)
		}
	}
}

// TestParseDifferentPortTypesKeepAllOps 确认去重只针对同名操作，
// 不同 portType 的操作都必须保留。
func TestParseDifferentPortTypesKeepAllOps(t *testing.T) {
	res, err := Parse([]byte(multiBindingWSDL))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(res.Operations) != 3 {
		t.Fatalf("operations = %d, want 3", len(res.Operations))
	}
	got := map[string]bool{}
	for _, op := range res.Operations {
		got[op.Name] = true
	}
	for _, want := range []string{"Alpha", "Beta", "Gamma"} {
		if !got[want] {
			t.Errorf("缺少操作 %q", want)
		}
	}
}
