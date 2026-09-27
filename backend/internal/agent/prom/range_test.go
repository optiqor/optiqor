package prom

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestQueryRange_ParsesMatrix(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query_range" {
			t.Errorf("path = %q", r.URL.Path)
		}
		q := r.URL.Query()
		if got := q.Get("query"); got != "up" {
			t.Errorf("query = %q", got)
		}
		if got := q.Get("step"); got != "3600" {
			t.Errorf("step = %q, want 3600", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status":"success",
			"data":{"resultType":"matrix","result":[
				{"metric":{"namespace":"prod","pod":"api-7b"},"values":[
					[1700000000, "1.5"],
					[1700003600, "1.7"],
					[1700007200, "2.1"]
				]}
			]}
		}`))
	}))
	t.Cleanup(srv.Close)

	c, err := NewHTTPClient(srv.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, err := c.QueryRange(context.Background(), "up", Range{
		Start: time.Unix(1700000000, 0),
		End:   time.Unix(1700007200, 0),
		Step:  time.Hour,
	})
	if err != nil {
		t.Fatalf("QueryRange: %v", err)
	}
	if len(got) != 1 || len(got[0].Points) != 3 {
		t.Fatalf("matrix shape: %+v", got)
	}
	if got[0].Points[0].Value != 1.5 || got[0].Points[2].Value != 2.1 {
		t.Errorf("values = %+v", got[0].Points)
	}
}

func TestQueryRange_RejectsBadRange(t *testing.T) {
	c, _ := NewHTTPClient("http://prom:9090")
	for _, tc := range []struct {
		name string
		r    Range
		want string
	}{
		{"zero step", Range{Start: time.Unix(1, 0), End: time.Unix(2, 0)}, "positive Step"},
		{"end before start", Range{Start: time.Unix(2, 0), End: time.Unix(1, 0), Step: time.Hour}, "End > Start"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.QueryRange(context.Background(), "x", tc.r)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("want %q, got %v", tc.want, err)
			}
		})
	}
}

func TestQueryRange_RejectsNonMatrixResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	t.Cleanup(srv.Close)
	c, _ := NewHTTPClient(srv.URL)
	_, err := c.QueryRange(context.Background(), "x", Range{Start: time.Unix(1, 0), End: time.Unix(2, 0), Step: time.Second})
	if err == nil || !strings.Contains(err.Error(), "matrix") {
		t.Errorf("want matrix-type error, got %v", err)
	}
}

func TestQuery_ParsesNegativeAndFractionalTimestamps(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[
			{"metric":{},"value":[1700000000.123,"42"]}
		]}}`))
	}))
	t.Cleanup(srv.Close)
	c, _ := NewHTTPClient(srv.URL)
	got, err := c.Query(context.Background(), "x", time.Unix(1700000000, 0))
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 || got[0].At.Nanosecond() == 0 {
		t.Errorf("fractional timestamp dropped: %+v", got)
	}
}

type recObserver struct {
	calls []obsCall
}

type obsCall struct {
	kind, status string
	latency      time.Duration
}

func (r *recObserver) ObserveQuery(k, s string, l time.Duration) {
	r.calls = append(r.calls, obsCall{kind: k, status: s, latency: l})
}

func TestQuery_RecordsObserverOnSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	t.Cleanup(srv.Close)

	obs := &recObserver{}
	c, err := NewHTTPClient(srv.URL, WithObserver(obs))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.Query(context.Background(), "up", time.Now()); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(obs.calls) != 1 {
		t.Fatalf("observer calls = %d, want 1", len(obs.calls))
	}
	if obs.calls[0].kind != "query" || obs.calls[0].status != "ok" {
		t.Errorf("call = %+v", obs.calls[0])
	}
}

func TestQuery_RecordsObserverOn4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	obs := &recObserver{}
	c, _ := NewHTTPClient(srv.URL, WithObserver(obs))
	if _, err := c.Query(context.Background(), "x", time.Now()); err == nil {
		t.Fatal("want 401 error")
	}
	if len(obs.calls) != 1 || obs.calls[0].status != "401" {
		t.Errorf("observer should bucket 401; got %+v", obs.calls)
	}
}

func TestQuery_AuthHeaderApplied(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	t.Cleanup(srv.Close)

	auth, _ := NewAuthenticator(AuthConfig{Mode: AuthBearer, BearerToken: "abc"})
	c, err := NewHTTPClient(srv.URL, WithAuth(auth))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.Query(context.Background(), "x", time.Now()); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if seen != "Bearer abc" {
		t.Errorf("Authorization header = %q, want %q", seen, "Bearer abc")
	}
}

func TestStatusBucket_Coarsens(t *testing.T) {
	for _, tc := range []struct {
		code int
		want string
	}{
		{401, "401"},
		{403, "403"},
		{404, "404"},
		{429, "429"},
		{412, "4xx"},
		{500, "5xx"},
		{503, "5xx"},
		{200, "200"},
	} {
		t.Run(strconv.Itoa(tc.code), func(t *testing.T) {
			if got := statusBucket(tc.code); got != tc.want {
				t.Errorf("statusBucket(%d) = %q, want %q", tc.code, got, tc.want)
			}
		})
	}
}

func TestFormatTime_PreservesSubsecond(t *testing.T) {
	at := time.Unix(1700000000, 500_000_000) // 1700000000.5
	got := formatTime(at)
	v, err := strconv.ParseFloat(got, 64)
	if err != nil {
		t.Fatalf("not a float: %v", err)
	}
	if v < 1700000000.4 || v > 1700000000.6 {
		t.Errorf("formatTime lost subsecond: %s", got)
	}
}

func TestNewHTTPClient_OptionRejectsBadValues(t *testing.T) {
	for _, tc := range []struct {
		name string
		opt  HTTPOption
		want string
	}{
		{"zero timeout", WithTimeout(0), "timeout"},
		{"negative max body", WithMaxBodyBytes(-1), "MaxBodyBytes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewHTTPClient("http://prom:9090", tc.opt)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestQuery_NoAuthDoesNotSetHeader(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	t.Cleanup(srv.Close)

	c, _ := NewHTTPClient(srv.URL)
	if _, err := c.Query(context.Background(), "x", time.Now()); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if seen != "" {
		t.Errorf("unauthenticated path set Authorization: %q", seen)
	}
}

// Cross-check: the unauthenticated path puts no extra query params.
func TestQuery_EncodesTimeAsFloat(t *testing.T) {
	var seenQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenQuery = r.URL.Query()
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	t.Cleanup(srv.Close)
	c, _ := NewHTTPClient(srv.URL)
	at := time.Unix(1700000000, 0)
	if _, err := c.Query(context.Background(), "up", at); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if got := seenQuery.Get("time"); got != "1700000000" {
		t.Errorf("time = %q, want 1700000000", got)
	}
}
