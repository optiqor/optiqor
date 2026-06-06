package watch

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// minimalValues is a valid Helm values YAML with one workload.
const minimalValues = `
api:
  replicaCount: 1
  image:
    repository: nginx
    tag: latest
  resources:
    requests:
      cpu: 10m
      memory: 32Mi
    limits:
      cpu: 500m
      memory: 256Mi
`

// TestRun_StartsAndExits verifies Run returns nil when context is
// cancelled — proves clean shutdown without needing OS signals.
func TestRun_StartsAndExits(t *testing.T) {
	dir := t.TempDir()
	values := filepath.Join(dir, "values.yaml")
	if err := os.WriteFile(values, []byte(minimalValues), 0o600); err != nil {
		t.Fatal(err)
	}

	var buf safeBuffer
	ctx, cancel := context.WithCancel(context.Background())

	opts := WithContext(ctx, Options{
		JSON:  true,
		Out:   &buf,
		IsTTY: false,
	})

	done := make(chan error, 1)
	go func() {
		done <- Run(values, opts)
	}()

	// Wait for the "start" event.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), `"event":"start"`) {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned error on clean shutdown: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not exit within 3 s after context cancel")
	}

	if !strings.Contains(buf.String(), `"event":"start"`) {
		t.Errorf("expected start event in output, got: %s", buf.String())
	}
}

// TestRun_EmitsUpdateOnFileWrite verifies that editing the watched
// YAML file causes a new JSON event to appear.
func TestRun_EmitsUpdateOnFileWrite(t *testing.T) {
	dir := t.TempDir()
	values := filepath.Join(dir, "values.yaml")
	if err := os.WriteFile(values, []byte(minimalValues), 0o600); err != nil {
		t.Fatal(err)
	}

	var buf safeBuffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	opts := WithContext(ctx, Options{
		JSON:  true,
		Out:   &buf,
		IsTTY: false,
	})

	done := make(chan error, 1)
	go func() {
		done <- Run(values, opts)
	}()

	// Wait for the initial "start" event.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), `"event":"start"`) {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if !strings.Contains(buf.String(), `"event":"start"`) {
		t.Fatal("start event not received within 2 s")
	}

	// Modify the file to trigger an update event.
	updated := minimalValues + "\n# touched\n"
	if err := os.WriteFile(values, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}

	// Wait for the "start" event from the re-run (debounce=200ms; 2s headroom).
	deadline = time.Now().Add(2 * time.Second)
	var got2 bool
	for time.Now().Before(deadline) {
		// After the file change a second "start" event is emitted
		// (runOnce always uses "start" as the eventKind).
		count := strings.Count(buf.String(), `"event":"start"`)
		if count >= 2 {
			got2 = true
			break
		}
		time.Sleep(30 * time.Millisecond)
	}

	cancel()
	<-done

	if !got2 {
		t.Errorf("second analysis event not received after file write; output: %s", buf.String())
	}
}

// TestRun_JSONEventsAreValidJSON verifies every emitted line is valid
// JSON with required fields.
func TestRun_JSONEventsAreValidJSON(t *testing.T) {
	dir := t.TempDir()
	values := filepath.Join(dir, "values.yaml")
	if err := os.WriteFile(values, []byte(minimalValues), 0o600); err != nil {
		t.Fatal(err)
	}

	var buf safeBuffer
	ctx, cancel := context.WithCancel(context.Background())

	opts := WithContext(ctx, Options{
		JSON:  true,
		Out:   &buf,
		IsTTY: false,
	})

	done := make(chan error, 1)
	go func() {
		done <- Run(values, opts)
	}()

	// Wait for start event then cancel.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), `"event":"start"`) {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	cancel()
	<-done

	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var evt map[string]any
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			t.Errorf("line is not valid JSON: %s — %v", line, err)
			continue
		}
		if _, ok := evt["event"]; !ok {
			t.Errorf("JSON event missing 'event' field: %s", line)
		}
		if _, ok := evt["timestamp"]; !ok {
			t.Errorf("JSON event missing 'timestamp' field: %s", line)
		}
	}
}

// TestRun_BadPath verifies that Run returns an error immediately when
// the path does not exist.
func TestRun_BadPath(t *testing.T) {
	err := Run("/nonexistent/path/values.yaml", Options{JSON: true})
	if err == nil {
		t.Error("expected error for nonexistent path, got nil")
	}
}

// TestIsRelevant checks that only YAML write/create/remove events pass
// the filter.
func TestIsRelevant(t *testing.T) {
	cases := []struct {
		name string
		ev   fsnotify.Event
		want bool
	}{
		{
			name: "yaml write",
			ev:   fsnotify.Event{Name: "values.yaml", Op: fsnotify.Write},
			want: true,
		},
		{
			name: "yml create",
			ev:   fsnotify.Event{Name: "chart.yml", Op: fsnotify.Create},
			want: true,
		},
		{
			name: "yaml remove",
			ev:   fsnotify.Event{Name: "values.yaml", Op: fsnotify.Remove},
			want: true,
		},
		{
			name: "yaml chmod — ignored",
			ev:   fsnotify.Event{Name: "values.yaml", Op: fsnotify.Chmod},
			want: false,
		},
		{
			name: "non-yaml write — ignored",
			ev:   fsnotify.Event{Name: "README.md", Op: fsnotify.Write},
			want: false,
		},
		{
			name: "go file write — ignored",
			ev:   fsnotify.Event{Name: "main.go", Op: fsnotify.Write},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRelevant(tc.ev); got != tc.want {
				t.Errorf("isRelevant(%v) = %v, want %v", tc.ev, got, tc.want)
			}
		})
	}
}
