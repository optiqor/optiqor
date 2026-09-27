package prom

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewHTTPClient_ValidationGuards(t *testing.T) {
	for _, tc := range []struct {
		name string
		url  string
		want bool // true means New should error
	}{
		{"empty", "", true},
		{"whitespace", "   ", true},
		{"non-http scheme", "ftp://foo:9090", true},
		{"good http", "http://prom:9090", false},
		{"good https", "https://prom:9090/path", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewHTTPClient(tc.url)
			gotErr := err != nil
			if gotErr != tc.want {
				t.Fatalf("err = %v, want err? %v", err, tc.want)
			}
		})
	}
}

func TestQuery_ParsesInstantVector(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("query"); got != "up" {
			t.Errorf("query = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status":"success",
			"data":{"resultType":"vector","result":[
				{"metric":{"namespace":"prod","pod":"api-7b"},"value":[1700000000,"42"]},
				{"metric":{"namespace":"prod","pod":"worker-1"},"value":[1700000000,"7.5"]}
			]}
		}`))
	}))
	t.Cleanup(srv.Close)

	c, err := NewHTTPClient(srv.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, err := c.Query(context.Background(), "up", time.Unix(1700000000, 0))
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("samples = %d, want 2", len(got))
	}
	if got[0].Value != 42 || got[1].Value != 7.5 {
		t.Errorf("values: %v %v", got[0].Value, got[1].Value)
	}
}

func TestQuery_RetriesOn5xx(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n++
		if n == 1 {
			http.Error(w, "boom", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	t.Cleanup(srv.Close)

	c, _ := NewHTTPClient(srv.URL)
	if _, err := c.Query(context.Background(), "vector(0)", time.Now()); err != nil {
		t.Fatalf("Query after retry: %v", err)
	}
	if n != 2 {
		t.Errorf("attempts = %d, want 2 (retry on first 5xx)", n)
	}
}

func TestQuery_DoesNotRetry4xx(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n++
		http.Error(w, "bad", http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)

	c, _ := NewHTTPClient(srv.URL)
	if _, err := c.Query(context.Background(), "bad", time.Now()); err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("want 400 err, got %v", err)
	}
	if n != 1 {
		t.Errorf("attempts = %d, want 1 (no retry on 4xx)", n)
	}
}

func TestScraper_MergesByWorkload(t *testing.T) {
	c := NewInMemoryClient()
	c.Add(DefaultProfile.CPURate, []Sample{
		{Labels: map[string]string{"namespace": "prod", "pod": "api-7b"}, Value: 0.5, At: time.Unix(1700, 0)},
		{Labels: map[string]string{"namespace": "prod", "pod": "api-7c"}, Value: 1.5, At: time.Unix(1700, 0)},
	})
	c.Add(DefaultProfile.MemoryWorking, []Sample{
		{Labels: map[string]string{"namespace": "prod", "pod": "api-7b"}, Value: 100, At: time.Unix(1700, 0)},
	})
	c.Add(DefaultProfile.OOMKilledIncrease, []Sample{
		{Labels: map[string]string{"namespace": "prod", "pod": "api-7c"}, Value: 3, At: time.Unix(1700, 0)},
	})

	owners := map[PodKey]WorkloadKey{
		{Namespace: "prod", Pod: "api-7b"}: {Namespace: "prod", Kind: "Deployment", Name: "api"},
		{Namespace: "prod", Pod: "api-7c"}: {Namespace: "prod", Kind: "Deployment", Name: "api"},
	}
	s := NewScraper(c, owners)
	s.NowFunc = func() time.Time { return time.Unix(1700, 0) }

	got, err := s.Scrape(context.Background())
	if err != nil {
		t.Fatalf("Scrape: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("rows = %d, want 1 (both pods roll up into api)", len(got))
	}
	if got[0].CPUUsageCores != 2 || got[0].MemoryWorking != 100 || got[0].OOMKilledCount != 3 {
		t.Errorf("aggregation wrong: %+v", got[0])
	}
}

func TestScraper_PartialFailureKeepsGoing(t *testing.T) {
	c := NewInMemoryClient()
	c.Err = errors.New("prom down")
	owners := map[PodKey]WorkloadKey{}
	s := NewScraper(c, owners)
	if _, err := s.Scrape(context.Background()); err == nil {
		t.Error("scrape with all-failing client should surface the last error")
	}
}

func TestScraper_ConcurrentSetPodOwnersAndScrape(t *testing.T) {
	// Race detector pins the Set/Read invariant. Without the mutex
	// guard this test crashes "concurrent map iteration and write"
	// under `go test -race` within ~10 iterations.
	c := NewInMemoryClient()
	c.Add(DefaultProfile.CPURate, []Sample{
		{Labels: map[string]string{"namespace": "prod", "pod": "api-1"}, Value: 1, At: time.Unix(1700, 0)},
	})
	s := NewScraper(c, map[PodKey]WorkloadKey{
		{Namespace: "prod", Pod: "api-1"}: {Namespace: "prod", Kind: "Deployment", Name: "api"},
	})

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = s.Scrape(context.Background())
			}
		}
	}()
	for i := 0; i < 200; i++ {
		s.SetPodOwners(map[PodKey]WorkloadKey{
			{Namespace: "prod", Pod: "api-1"}: {Namespace: "prod", Kind: "Deployment", Name: "api"},
		})
	}
	close(stop)
	<-done
}
