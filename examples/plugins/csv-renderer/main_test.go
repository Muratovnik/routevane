package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderCanonicalCSV(t *testing.T) {
	document := []byte(`{"rules":[
		{"kind":"domain_exact","value":"z.example.test","service_id":"example","component_id":"main"},
		{"kind":"domain_exact","value":"a.example.test","service_id":"office, \"lab\"","component_id":"main"},
		{"kind":"domain_exact","value":"z.example.test","service_id":"example","component_id":"main"}
	]}`)
	want := header + "domain_exact,a.example.test,\"office, \"\"lab\"\"\",main\n" +
		"domain_exact,z.example.test,example,main\n"
	h := handler{}
	payload, err := h.Render(document)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != want {
		t.Fatalf("Render() = %q, want %q", payload, want)
	}
	if err := h.Validate(payload); err != nil {
		t.Fatalf("Validate(Render()) = %v", err)
	}
	count, err := h.ProjectedRuleCount(document)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("ProjectedRuleCount() = %d, want 2 unique rows", count)
	}
}

func TestValidateRequiresCanonicalCSVBytes(t *testing.T) {
	row := "domain_exact,example.test,example,main\n"
	canonical := header + row
	if err := (handler{}).Validate([]byte(canonical)); err != nil {
		t.Fatalf("canonical CSV was rejected: %v", err)
	}
	tests := []struct {
		name    string
		payload string
	}{
		{name: "missing final LF", payload: strings.TrimSuffix(canonical, "\n")},
		{name: "CRLF header", payload: strings.Replace(header, "\n", "\r\n", 1) + row},
		{name: "CRLF row", payload: header + strings.Replace(row, "\n", "\r\n", 1)},
		{name: "extra final LF", payload: canonical + "\n"},
		{name: "blank line before row", payload: header + "\n" + row},
		{name: "unnecessary quotes", payload: header + "domain_exact,\"example.test\",example,main\n"},
		{name: "bare quote", payload: header + "domain_exact,example\".test,example,main\n"},
		{name: "unclosed quote", payload: header + "domain_exact,\"example.test,example,main\n"},
		{name: "quoted LF", payload: header + "domain_exact,example.test,\"first\nsecond\",main\n"},
		{name: "quoted CRLF", payload: header + "domain_exact,example.test,\"first\r\nsecond\",main\n"},
		{name: "quoted CR", payload: header + "domain_exact,example.test,\"first\rsecond\",main\n"},
		{name: "duplicate row", payload: canonical + row},
		{name: "unsorted rows", payload: canonical + "domain_exact,a.example.test,example,main\n"},
		{name: "empty field", payload: header + "domain_exact,example.test,,main\n"},
		{name: "padded field", payload: header + "domain_exact,example.test, example,main\n"},
		{name: "extra field", payload: header + "domain_exact,example.test,example,main,extra\n"},
		{name: "no rows", payload: header},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := (handler{}).Validate([]byte(test.payload)); err == nil {
				t.Fatalf("Validate(%q) accepted non-canonical CSV", test.payload)
			}
		})
	}
}

func TestRenderRejectsFieldsThatCannotRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "empty", value: ""},
		{name: "leading space", value: " example"},
		{name: "trailing space", value: "example "},
		{name: "LF", value: "first\nsecond"},
		{name: "CR", value: "first\rsecond"},
		{name: "CRLF", value: "first\r\nsecond"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document, err := json.Marshal(map[string]any{
				"rules": []map[string]string{{
					"kind":         "domain_exact",
					"value":        "example.test",
					"service_id":   test.value,
					"component_id": "main",
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			h := handler{}
			if _, err := h.Render(document); err == nil {
				t.Fatal("Render() accepted a field that cannot round trip")
			}
			if _, err := h.ProjectedRuleCount(document); err == nil {
				t.Fatal("ProjectedRuleCount() accepted a field that cannot round trip")
			}
		})
	}
}
