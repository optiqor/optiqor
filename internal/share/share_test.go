package share

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type sample struct {
	Source    string `json:"source"`
	Workloads int    `json:"workloads_analyzed"`
	Findings  []any  `json:"findings"`
}

func TestHash(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b sample
		// equal=true asserts Hash(a) == Hash(b); false asserts they differ.
		equal bool
	}{
		{
			name:  "stable across calls",
			a:     sample{Source: "/tmp/x", Workloads: 3, Findings: []any{map[string]any{"DetectorID": "cpu", "Severity": "MED"}}},
			b:     sample{Source: "/tmp/x", Workloads: 3, Findings: []any{map[string]any{"DetectorID": "cpu", "Severity": "MED"}}},
			equal: true,
		},
		{
			// Source-path differences must not change the hash — that's
			// the point of sanitisation.
			name:  "ignores source path",
			a:     sample{Source: "/home/alice/chart", Workloads: 1},
			b:     sample{Source: "/home/bob/chart", Workloads: 1},
			equal: true,
		},
		{
			name:  "workload count affects hash",
			a:     sample{Workloads: 3},
			b:     sample{Workloads: 5},
			equal: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ha, err := Hash(tc.a)
			if err != nil {
				t.Fatal(err)
			}
			hb, err := Hash(tc.b)
			if err != nil {
				t.Fatal(err)
			}
			if len(ha) != HashLen {
				t.Errorf("hash len = %d, want %d", len(ha), HashLen)
			}
			if (ha == hb) != tc.equal {
				t.Errorf("equal=%v: ha=%q hb=%q", tc.equal, ha, hb)
			}
		})
	}
}

func TestURL_Format(t *testing.T) {
	u, err := URL(sample{Workloads: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(u, BaseURL) {
		t.Errorf("URL missing base: %q", u)
	}
	if !IsHash(strings.TrimPrefix(u, BaseURL)) {
		t.Errorf("URL trailer is not a valid hash: %q", u)
	}
}

func TestSanitise_TruncatesLongDetail(t *testing.T) {
	long := strings.Repeat("a", 400)
	in := map[string]any{
		"source":   "/path",
		"findings": []any{map[string]any{"Detail": long}},
	}
	out, err := Sanitise(in)
	if err != nil {
		t.Fatal(err)
	}
	doc := out.(map[string]any)
	if doc["source"] != "(redacted)" {
		t.Errorf("source not redacted: %v", doc["source"])
	}
	finds := doc["findings"].([]any)
	d := finds[0].(map[string]any)["Detail"].(string)
	if len(d) > 260 {
		t.Errorf("detail not truncated: len=%d", len(d))
	}
	if !strings.HasSuffix(d, "…") {
		t.Errorf("truncation marker missing: %q", d)
	}
}

func TestUpload(t *testing.T) {
	for _, tc := range []struct {
		name      string
		handler   http.HandlerFunc
		closeSrv  bool // close the server before Upload — simulates network error
		input     sample
		wantPost  bool
		wantErrIn string // substring expected in res.Error when wantPost=false
		check     func(t *testing.T, res UploadResult, captured map[string]string, body []byte)
	}{
		{
			name: "posts sanitised json",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusAccepted)
			},
			input:    sample{Source: "/tmp/x", Workloads: 2},
			wantPost: true,
			check: func(t *testing.T, res UploadResult, captured map[string]string, body []byte) {
				t.Helper()
				if !IsHash(res.Hash) {
					t.Errorf("Hash invalid: %q", res.Hash)
				}
				if !strings.HasPrefix(res.URL, BaseURL) {
					t.Errorf("URL = %q", res.URL)
				}
				if captured["method"] != http.MethodPost {
					t.Errorf("method = %q", captured["method"])
				}
				if captured["content-type"] != "application/json" {
					t.Errorf("content-type = %q", captured["content-type"])
				}
				if captured["x-optiqor-hash"] != res.Hash {
					t.Errorf("X-Optiqor-Hash = %q, want %q", captured["x-optiqor-hash"], res.Hash)
				}
				// Sanitisation contract: source path must not appear in
				// the wire body.
				if strings.Contains(string(body), "/tmp/x") {
					t.Errorf("source path leaked into upload body: %s", body)
				}
			},
		},
		{
			name: "non-2xx becomes error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			input:     sample{Workloads: 1},
			wantPost:  false,
			wantErrIn: "500",
		},
		{
			// Closed server → connection-refused style error. Hash/URL
			// must still be populated so callers can show the local hash.
			name:      "network error graceful",
			handler:   func(http.ResponseWriter, *http.Request) {},
			closeSrv:  true,
			input:     sample{Workloads: 1},
			wantPost:  false,
			wantErrIn: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			captured := map[string]string{}
			var body []byte
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured["method"] = r.Method
				captured["content-type"] = r.Header.Get("Content-Type")
				captured["x-optiqor-hash"] = r.Header.Get("X-Optiqor-Hash")
				body, _ = io.ReadAll(r.Body)
				tc.handler(w, r)
			}))
			if tc.closeSrv {
				srv.Close()
			} else {
				defer srv.Close()
			}

			res := Upload(tc.input, srv.URL, false)

			if res.Posted != tc.wantPost {
				t.Fatalf("Posted = %v want %v; Error=%q", res.Posted, tc.wantPost, res.Error)
			}
			if !IsHash(res.Hash) {
				t.Errorf("Hash should be populated even on failure: %q", res.Hash)
			}
			if !tc.wantPost && tc.wantErrIn != "" && !strings.Contains(res.Error, tc.wantErrIn) {
				t.Errorf("Error %q does not contain %q", res.Error, tc.wantErrIn)
			}
			if !tc.wantPost && tc.wantErrIn == "" && res.Error == "" {
				t.Error("Error should describe the failure")
			}
			if tc.check != nil {
				tc.check(t, res, captured, body)
			}
		})
	}
}

func TestIsHash(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want bool
	}{
		{"empty", "", false},
		{"too short", "abc", false},
		{"lowercase hex", "abcdef012345", true},
		{"uppercase hex", "ABCDEF012345", true},
		{"non-hex char", "abcdef01234g", false},
		{"too long", "abcdef0123456", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsHash(tc.in); got != tc.want {
				t.Errorf("IsHash(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestUpload_PrivateHeaderSent(t *testing.T) {
	var gotPrivate string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPrivate = r.Header.Get("X-Optiqor-Private")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	res := Upload(sample{Workloads: 1}, srv.URL, true)
	if !res.Posted {
		t.Fatalf("Posted = false, error = %q", res.Error)
	}
	if gotPrivate != "1" {
		t.Errorf("X-Optiqor-Private = %q, want %q", gotPrivate, "1")
	}
}

func TestUpload_NoPrivateHeaderByDefault(t *testing.T) {
	var gotPrivate string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPrivate = r.Header.Get("X-Optiqor-Private")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	Upload(sample{Workloads: 1}, srv.URL, false)
	if gotPrivate != "" {
		t.Errorf("X-Optiqor-Private should be absent, got %q", gotPrivate)
	}
}
