package parser

import (
	"errors"
	"strings"
	"testing"
)

func TestParseValues_Roundtrip(t *testing.T) {
	yaml := `
api:
  replicas: 3
  resources:
    requests: {cpu: 500m, memory: 256Mi}
    limits:   {cpu: 1, memory: 512Mi}
  image: nginx:1.25
`
	ws, err := ParseValues(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("ParseValues: %v", err)
	}
	if len(ws) != 1 {
		t.Fatalf("want 1 workload, got %d", len(ws))
	}
	w := ws[0]
	if w.Name != "api" {
		t.Errorf("name = %q, want api", w.Name)
	}
	if w.Replicas != 3 {
		t.Errorf("replicas = %d, want 3", w.Replicas)
	}
	if w.Requests.CPU.Value != 500 {
		t.Errorf("cpu request = %d, want 500", w.Requests.CPU.Value)
	}
	if !w.Image.Set || w.Image.Repository != "nginx" || w.Image.Tag != "1.25" {
		t.Errorf("image = %+v", w.Image)
	}
}

func TestParseValues_InvalidInput(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
	}{
		{name: "malformed-yaml", in: ":\n  - not: [valid"},
		// Empty input must surface as 400 via errors.Is(err, ErrParse).
		{name: "empty-stream", in: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseValues(strings.NewReader(tc.in))
			if err == nil {
				t.Fatal("want error")
			}
			if !errors.Is(err, ErrParse) {
				t.Errorf("err should wrap ErrParse; got %v", err)
			}
		})
	}
}
