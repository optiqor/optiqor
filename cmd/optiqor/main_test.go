package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRoot_Help_ContainsCommandsAndDisclosure(t *testing.T) {
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute --help: %v", err)
	}
	for _, want := range []string{
		"analyze",
		"demo",
		accuracyDisclosure,
		"Examples:",
		"optiqor analyze",
		"--no-color",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("help missing %q:\n%s", want, buf.String())
		}
	}
}

func TestRoot_Version_NamesBinary(t *testing.T) {
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--version"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute --version: %v", err)
	}
	for _, want := range []string{"optiqor", "Helm chart cost analysis"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("version missing %q:\n%s", want, buf.String())
		}
	}
}

func TestDemo_FullPath_EmitsDisclosureAndWorkloads(t *testing.T) {
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--no-color", "demo"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute demo: %v\n%s", err, buf.String())
	}
	out := buf.String()
	for _, want := range []string{
		"optiqor",
		"Helm chart cost",
		"api",
		"worker",
		// ±40% accuracy disclosure is mandatory in every renderer output (CLAUDE.md hard rule).
		"±40%",
		"optiqor.dev/get",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in demo output:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b") {
		t.Errorf("--no-color output should be ANSI-free; got:\n%s", out)
	}
}

func TestAnalyze_FixtureFile_FiresWellKnownDetectors(t *testing.T) {
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--no-color", "analyze", "--fail-on", "", "../../testdata/fixtures/basic-chart/values.yaml"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute analyze: %v\n%s", err, buf.String())
	}
	out := buf.String()
	for _, want := range []string{
		"15 workloads",
		"HIGH",
		"MED",
		"LOW",
		"api",
		"worker",
		"cache",
		"logger",
		"CPU request appears overprovisioned",
		"Memory limit not set",
		"Image not pinned to a stable tag",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in analyze output:\n%s", want, out)
		}
	}
}

func TestAnalyze_JSON_ShapeContainsDisclosureAndDetectors(t *testing.T) {
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"analyze", "--json", "--fail-on", "", "../../testdata/fixtures/basic-chart/values.yaml"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute --json: %v\n%s", err, buf.String())
	}
	out := buf.String()
	for _, want := range []string{
		`"accuracy_disclosure"`,
		`"workloads_analyzed": 15`,
		`"DetectorID": "cpu-overprovisioned"`,
		`"DetectorID": "memory-overprovisioned"`,
		`"DetectorID": "missing-memory-limit"`,
		`"DetectorID": "missing-cpu-limit"`,
		`"DetectorID": "image-pinned-latest"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in JSON output:\n%s", want, out)
		}
	}
}

func TestResolveColor(t *testing.T) {
	for _, tc := range []struct {
		name          string
		noColorFlag   bool
		noColorEnv    string
		clicolorForce string
		setNoColorEnv bool
		setForceEnv   bool
		nonTTYOut     bool
		want          bool
	}{
		{
			name:        "no-color-flag-wins",
			noColorFlag: true,
			want:        false,
		},
		{
			name:          "no-color-env-disables",
			setNoColorEnv: true,
			noColorEnv:    "1",
			want:          false,
		},
		{
			name:          "clicolor-force-overrides-non-tty",
			setForceEnv:   true,
			clicolorForce: "1",
			setNoColorEnv: true,
			noColorEnv:    "",
			nonTTYOut:     true,
			want:          true,
		},
		{
			name:          "non-tty-without-forces-disables",
			setForceEnv:   true,
			clicolorForce: "",
			setNoColorEnv: true,
			noColorEnv:    "",
			nonTTYOut:     true,
			want:          false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setNoColorEnv {
				t.Setenv("NO_COLOR", tc.noColorEnv)
			}
			if tc.setForceEnv {
				t.Setenv("CLICOLOR_FORCE", tc.clicolorForce)
			}
			cmd := newRootCmd()
			if tc.nonTTYOut {
				cmd.SetOut(&bytes.Buffer{})
			}
			if got := resolveColor(cmd, tc.noColorFlag); got != tc.want {
				t.Errorf("resolveColor = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAtoi(t *testing.T) {
	for _, tc := range []struct {
		name    string
		in      string
		want    int
		wantErr bool
	}{
		{name: "empty-string-is-zero", in: "", want: 0},
		{name: "zero", in: "0", want: 0},
		{name: "three-digit", in: "123", want: 123},
		{name: "two-digit", in: "99", want: 99},
		{name: "all-letters-errors", in: "abc", wantErr: true},
		{name: "trailing-letter-errors", in: "12x", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := atoi(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("atoi(%q): expected error", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("atoi(%q): unexpected error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("atoi(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}
