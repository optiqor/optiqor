package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// -update regenerates the golden files after a deliberate UX change.
var update = flag.Bool("update", false, "update golden files")

const goldenDir = "../../testdata/golden"

func TestCmd_Golden_Stable(t *testing.T) {
	if err := os.MkdirAll(goldenDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Pin width or the dev's $COLUMNS leaks into goldens and diverges
	// from CI (no TTY -> fallback 80).
	t.Setenv("COLUMNS", "80")

	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "demo_plain", args: []string{"--no-color", "demo"}},
		{name: "demo_json", args: []string{"demo", "--json"}},
		{name: "analyze_fixture_plain", args: []string{"--no-color", "analyze", "../../testdata/fixtures/basic-chart/values.yaml"}},
		{name: "analyze_fixture_severity_high", args: []string{"--no-color", "analyze", "../../testdata/fixtures/basic-chart/values.yaml", "--severity", "high"}},
		{name: "analyze_fixture_detector_filter", args: []string{"--no-color", "analyze", "../../testdata/fixtures/basic-chart/values.yaml", "--detector", "image-pinned-latest"}},
		{name: "score_fixture_plain", args: []string{"--no-color", "score", "../../testdata/fixtures/basic-chart/values.yaml"}},
		{name: "score_fixture_json", args: []string{"score", "../../testdata/fixtures/basic-chart/values.yaml", "--json"}},
		{name: "audit_fixture_plain", args: []string{"--no-color", "audit", "../../testdata/fixtures/basic-chart/values.yaml", "--fail-on", ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newRootCmd()
			var buf bytes.Buffer
			cmd.SetOut(&buf)
			cmd.SetErr(&buf)
			cmd.SetArgs(tc.args)
			// Must run in cmd/optiqor so ../../testdata/... resolves.
			_ = cmd.Execute()
			got := normalize(buf.String())

			path := filepath.Join(goldenDir, tc.name+".txt")
			if *update {
				if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden %s (run with -update to create): %v", path, err)
			}
			if got != string(want) {
				t.Errorf("golden mismatch for %s\n--- want\n%s\n--- got\n%s", tc.name, want, got)
			}
		})
	}
}

// normalize strips the test's cwd and the repo root from filepath.Abs
// output so goldens stay bit-identical across laptops and CI runners.
func normalize(s string) string {
	s = strings.ReplaceAll(s, `\`, `/`)
	s = strings.ReplaceAll(s, `//`, `/`)
	s = strings.ReplaceAll(s, `https:/`, `https://`)
	s = strings.ReplaceAll(s, `http:/`, `http://`)

	if cwd, err := os.Getwd(); err == nil {
		cwdNorm := strings.ReplaceAll(cwd, `\`, `/`)

		if repo, err := filepath.Abs(filepath.Join(cwd, "..", "..")); err == nil {
			repoNorm := strings.ReplaceAll(repo, `\`, `/`)
			s = strings.ReplaceAll(s, repoNorm, "<REPO>")
		}
		s = strings.ReplaceAll(s, cwdNorm, "<CWD>")
	}
	return s
}
