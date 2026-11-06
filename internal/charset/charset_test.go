package charset

import (
	"strings"
	"testing"
)

// gbk 是 "中文测试" 的 GBK 字节。
var gbk = []byte{0xd6, 0xd0, 0xce, 0xc4, 0xb2, 0xe2, 0xca, 0xd4}

// TestDecodeGBK 覆盖最常见的国内场景：GBK 编码的响应体。
func TestDecodeGBK(t *testing.T) {
	got := string(Decode(gbk, "GBK"))
	if got != "中文测试" {
		t.Fatalf("Decode(GBK) = %q, want 中文测试", got)
	}
	// 大小写与空白都应容忍（HTTP 头里各种写法都有）。
	for _, name := range []string{"gbk", "GBK", " gbk ", "cp936", "windows-936"} {
		if got := string(Decode(gbk, name)); got != "中文测试" {
			t.Errorf("Decode(%q) = %q", name, got)
		}
	}
}

// TestDecodeGB2312 必须走 GBK 解码器（GB2312 是 GBK 子集），
// 而不是 x/text 里无关的 HZ 7-bit 编码。
func TestDecodeGB2312(t *testing.T) {
	if got := string(Decode(gbk, "gb2312")); got != "中文测试" {
		t.Fatalf("Decode(gb2312) = %q, want 中文测试", got)
	}
}

// TestDecodeUTF8Passthrough 确保 UTF-8 与未知字符集不被二次处理。
func TestDecodeUTF8Passthrough(t *testing.T) {
	in := []byte("中文测试")
	for _, name := range []string{"", "utf-8", "UTF-8", "us-ascii", "ascii", "unknown-charset"} {
		if got := Decode(in, name); string(got) != string(in) {
			t.Errorf("Decode(%q) 改变了内容: %q", name, got)
		}
	}
}

// TestDecodeEmpty 空输入不应 panic。
func TestDecodeEmpty(t *testing.T) {
	if got := Decode(nil, "GBK"); len(got) != 0 {
		t.Errorf("Decode(nil) = %q", got)
	}
}

// TestXMLDeclEncoding 解析 XML 声明中的 encoding 属性。
func TestXMLDeclEncoding(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"双引号", `<?xml version="1.0" encoding="GBK"?><r/>`, "GBK"},
		{"单引号", `<?xml version='1.0' encoding='GBK'?><r/>`, "GBK"},
		{"无声明", `<r/>`, ""},
		{"声明无 encoding", `<?xml version="1.0"?><r/>`, ""},
		{"前导空白", "\n  <?xml version=\"1.0\" encoding=\"gb18030\"?><r/>", "gb18030"},
		{"BOM", "\xef\xbb\xbf<?xml version=\"1.0\" encoding=\"GBK\"?><r/>", "GBK"},
		{"未闭合声明", `<?xml version="1.0" encoding="GBK"<r/>`, ""},
	}
	for _, c := range cases {
		if got := XMLDeclEncoding([]byte(c.in)); got != c.want {
			t.Errorf("%s: XMLDeclEncoding = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestDecodeXML 用文档自身的声明转码整个文档。
func TestDecodeXML(t *testing.T) {
	doc := append([]byte(`<?xml version="1.0" encoding="GBK"?><r>`), gbk...)
	doc = append(doc, []byte(`</r>`)...)

	got := string(DecodeXML(doc))
	if !strings.Contains(got, "中文测试") {
		t.Fatalf("DecodeXML 未转码: %q", got)
	}
	if !strings.Contains(got, `encoding="GBK"`) {
		t.Errorf("声明本身不应被破坏: %q", got)
	}

	// UTF-8 文档必须原样返回。
	utf8Doc := []byte(`<?xml version="1.0" encoding="UTF-8"?><r>中文</r>`)
	if got := string(DecodeXML(utf8Doc)); got != string(utf8Doc) {
		t.Errorf("UTF-8 文档被改动: %q", got)
	}
}

// TestReaderIsEncodingXMLHook 确保 Reader 可用作 xml.Decoder 的 CharsetReader。
func TestReaderIsEncodingXMLHook(t *testing.T) {
	r, err := Reader("GBK", strings.NewReader(string(gbk)))
	if err != nil {
		t.Fatalf("Reader 返回错误: %v", err)
	}
	buf := new(strings.Builder)
	if _, err := buf.WriteString(""); err != nil {
		t.Fatal(err)
	}
	out := make([]byte, 0, 16)
	tmp := make([]byte, 4)
	for {
		n, err := r.Read(tmp)
		out = append(out, tmp[:n]...)
		if err != nil {
			break
		}
	}
	if string(out) != "中文测试" {
		t.Fatalf("Reader 输出 = %q", out)
	}
}
