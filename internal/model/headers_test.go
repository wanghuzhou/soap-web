package model

import "testing"

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

// TestClampTimeout covers the default and the bounds shared by the API and
// transport.
func TestClampTimeout(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, DefaultTimeoutMS},
		{-5, DefaultTimeoutMS},
		{500, MinTimeoutMS},
		{30000, 30000},
		{MaxTimeoutMS + 1, MaxTimeoutMS},
	}
	for _, c := range cases {
		if got := ClampTimeout(c.in); got != c.want {
			t.Errorf("ClampTimeout(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}
