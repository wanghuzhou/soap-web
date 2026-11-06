package soapuiproj

import (
	"strings"
	"testing"

	"soapweb/internal/model"
)

func TestMarshalRoundTrip(t *testing.T) {
	orig := model.Project{
		ID:     "p1",
		Name:   "往返工程",
		Source: "soapui",
		Services: []model.Service{{
			ID:   "s1",
			Name: "Svc",
			Wsdl: "http://x/wsdl",
			Operations: []model.Operation{{
				ID:         "o1",
				Name:       "Op1",
				SoapAction: "urn:Op1",
				Endpoint:   "http://x/soap",
				Calls: []model.Call{
					{ID: "c1", Title: "请求一", Endpoint: "http://x/soap", Action: "urn:Op1", Request: "<env:Envelope><a>1</a></env:Envelope>"},
					{ID: "c2", Title: "请求二", Endpoint: "http://x/soap", Action: "urn:Op1", Request: "<env:Envelope><b>2</b></env:Envelope>"},
				},
			}},
		}},
	}

	data, err := Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	xmlStr := string(data)
	for _, want := range []string{"con:soapui-project", "con:interface", "con:call", "<![CDATA[", "请求一", "请求二"} {
		if !strings.Contains(xmlStr, want) {
			t.Errorf("exported xml missing %q\n%s", want, xmlStr)
		}
	}

	// Re-parse and compare structurally.
	got, err := Parse(data)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if got.Name != orig.Name {
		t.Errorf("name = %q, want %q", got.Name, orig.Name)
	}
	if len(got.Services) != 1 || len(got.Services[0].Operations) != 1 {
		t.Fatalf("structure mismatch: %+v", got)
	}
	op := got.Services[0].Operations[0]
	if op.Name != "Op1" || op.SoapAction != "urn:Op1" || op.Endpoint != "http://x/soap" {
		t.Errorf("op = %+v", op)
	}
	if len(op.Calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(op.Calls))
	}
	if op.Calls[0].Title != "请求一" || op.Calls[1].Title != "请求二" {
		t.Errorf("titles = %q, %q", op.Calls[0].Title, op.Calls[1].Title)
	}
	if !strings.Contains(op.Calls[0].Request, "<a>1</a>") {
		t.Errorf("call0 body = %q", op.Calls[0].Request)
	}
	if !strings.Contains(op.Calls[1].Request, "<b>2</b>") {
		t.Errorf("call1 body = %q", op.Calls[1].Request)
	}
}

func TestMarshalEscapedBody(t *testing.T) {
	// A body containing "]]>" must not break the XML; export escapes it and it
	// must still round-trip.
	orig := model.Project{
		Name: "esc",
		Services: []model.Service{{
			Name: "S",
			Operations: []model.Operation{{
				Name:  "O",
				Calls: []model.Call{{Title: "t", Request: "<r><![CDATA[weird ]]> inner</r>"}},
			}},
		}},
	}
	data, err := Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(data)
	if err != nil {
		t.Fatalf("re-parse escaped: %v", err)
	}
	if !strings.Contains(got.Services[0].Operations[0].Calls[0].Request, "weird ]]> inner") {
		t.Errorf("escaped body lost: %q", got.Services[0].Operations[0].Calls[0].Request)
	}
}
