package model

import "testing"

// TestMergeHeaders covers the project-default + call-override rule.
func TestMergeHeaders(t *testing.T) {
	project := []Header{
		{Name: "Content-Type", Value: "text/xml"},
		{Name: "Authorization", Value: "Basic project"},
	}
	call := []Header{
		// Case-insensitive override of a project header.
		{Name: "authorization", Value: "Bearer call"},
		{Name: "X-Trace", Value: "1"},
	}

	got := MergeHeaders(project, call)
	want := []Header{
		{Name: "Content-Type", Value: "text/xml"},
		{Name: "Authorization", Value: "Bearer call"},
		{Name: "X-Trace", Value: "1"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d headers, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].Name != want[i].Name || got[i].Value != want[i].Value {
			t.Errorf("header %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestMergeHeadersDropsEmptyNames keeps a blank row in the editor from failing
// every send.
func TestMergeHeadersDropsEmptyNames(t *testing.T) {
	got := MergeHeaders(
		[]Header{{Name: "  ", Value: "x"}},
		[]Header{{Name: "", Value: "y"}, {Name: " Ok ", Value: "1"}},
	)
	if len(got) != 1 || got[0].Name != "Ok" {
		t.Errorf("got %+v, want a single Ok header", got)
	}
}

func TestMergeHeadersAllEmpty(t *testing.T) {
	if got := MergeHeaders(nil, nil); got != nil {
		t.Errorf("got %+v, want nil", got)
	}
}

// TestMergeHeadersKeepsEmptyValue distinguishes "header with no value" from
// "header not set": the value must survive as an empty string.
func TestMergeHeadersKeepsEmptyValue(t *testing.T) {
	got := MergeHeaders(nil, []Header{{Name: "X-Flag", Value: ""}})
	if len(got) != 1 || got[0].Value != "" {
		t.Errorf("got %+v, want one header with an empty value", got)
	}
}

func TestValidateHeaders(t *testing.T) {
	valid := []Header{
		{Name: "Content-Type", Value: "text/xml"},
		{Name: "SOAPAction", Value: `"http://x"`},
		{Name: "X-Custom_1", Value: "v"},
	}
	if err := ValidateHeaders(valid); err != nil {
		t.Errorf("valid headers rejected: %v", err)
	}

	invalid := []struct {
		name   string
		header Header
	}{
		{"empty name", Header{Name: " ", Value: "v"}},
		{"space in name", Header{Name: "Bad Name", Value: "v"}},
		{"colon in name", Header{Name: "Bad:Name", Value: "v"}},
		{"newline in value", Header{Name: "X", Value: "a\nb"}},
		{"carriage return in value", Header{Name: "X", Value: "a\rb"}},
	}
	for _, tc := range invalid {
		if err := ValidateHeaders([]Header{tc.header}); err == nil {
			t.Errorf("%s should be rejected: %+v", tc.name, tc.header)
		}
	}
}

// TestIsSensitiveName pins which headers the UI masks by default.
func TestIsSensitiveName(t *testing.T) {
	sensitive := []string{"Authorization", "authorization", "Proxy-Authorization", "Cookie", "X-Api-Key", "app-token", "client-secret"}
	for _, name := range sensitive {
		if !IsSensitiveName(name) {
			t.Errorf("%q should be sensitive", name)
		}
	}
	plain := []string{"Content-Type", "SOAPAction", "Accept", "X-Trace"}
	for _, name := range plain {
		if IsSensitiveName(name) {
			t.Errorf("%q should not be sensitive", name)
		}
	}
}

// TestCallEffectiveTimeout covers the default and the clamp.
func TestCallEffectiveTimeout(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, DefaultTimeoutMS},
		{-5, DefaultTimeoutMS},
		{500, MinTimeoutMS},
		{30000, 30000},
		{MaxTimeoutMS + 1, MaxTimeoutMS},
	}
	for _, c := range cases {
		if got := (Call{TimeoutMS: c.in}).EffectiveTimeout(); got != c.want {
			t.Errorf("EffectiveTimeout(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}
