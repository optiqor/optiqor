package ingestion

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const promMatrixOK = `{
  "status":"success",
  "data":{
    "resultType":"matrix",
    "result":[
      {"metric":{"__name__":"up","job":"api"},
       "values":[[1736208000,"1"],[1736208060,"0.5"]]}
    ]
  }
}`

const promMatrixOutOfOrder = `{
  "status":"success",
  "data":{
    "resultType":"matrix",
    "result":[
      {"metric":{"j":"a"},
       "values":[[2000,"1"],[1000,"2"]]}
    ]
  }
}`

func TestParsePrometheusMatrix(t *testing.T) {
	for _, tc := range []struct {
		name    string
		in      string
		wantErr error // errors.Is target; nil + errAny=false means "no error"
		errAny  bool  // true means any non-nil err is acceptable
		check   func(t *testing.T, got []PromSeries)
	}{
		{
			name: "happy matrix preserves labels values and chronology",
			in:   promMatrixOK,
			check: func(t *testing.T, got []PromSeries) {
				t.Helper()
				if len(got) != 1 {
					t.Fatalf("series count = %d", len(got))
				}
				s := got[0]
				if s.Metric["job"] != "api" {
					t.Errorf("metric label lost: %+v", s.Metric)
				}
				if len(s.Samples) != 2 {
					t.Fatalf("samples = %d", len(s.Samples))
				}
				if s.Samples[0].Value != 1 || s.Samples[1].Value != 0.5 {
					t.Errorf("values lost: %+v", s.Samples)
				}
				if !s.Samples[0].At.Before(s.Samples[1].At) {
					t.Errorf("samples not chronological")
				}
			},
		},
		{
			name: "out-of-order samples are re-sorted ascending",
			in:   promMatrixOutOfOrder,
			check: func(t *testing.T, got []PromSeries) {
				t.Helper()
				if got[0].Samples[0].At.After(got[0].Samples[1].At) {
					t.Errorf("samples not resorted")
				}
			},
		},
		{
			name:    "non-success status returns ErrPromBadStatus",
			in:      `{"status":"error","data":{"resultType":"matrix","result":[]}}`,
			wantErr: ErrPromBadStatus,
		},
		{
			name:    "vector result type is rejected",
			in:      `{"status":"success","data":{"resultType":"vector","result":[]}}`,
			wantErr: ErrPromUnsupportedType,
		},
		{
			name:   "malformed JSON returns parse error",
			in:     `{not json`,
			errAny: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParsePrometheusMatrix(strings.NewReader(tc.in))
			switch {
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("want %v, got %v", tc.wantErr, err)
				}
			case tc.errAny:
				if err == nil {
					t.Error("want error")
				}
			default:
				if err != nil {
					t.Fatalf("parse: %v", err)
				}
				if tc.check != nil {
					tc.check(t, got)
				}
			}
		})
	}
}

const curOK = `lineItem/UsageStartDate,lineItem/UsageEndDate,lineItem/ProductCode,lineItem/UsageType,lineItem/UsageAmount,lineItem/UnblendedCost,product/region,lineItem/ResourceId
2026-05-01T00:00:00Z,2026-05-01T01:00:00Z,AmazonEC2,BoxUsage:m6i.large,1.0,0.096,us-east-1,i-0123
2026-05-01T01:00:00Z,2026-05-01T02:00:00Z,AmazonEC2,BoxUsage:m6i.large,1.0,0.096,us-east-1,i-0123
`

func TestParseCURRows(t *testing.T) {
	missingHeader := "lineItem/UsageStartDate,lineItem/UsageEndDate\n2026-05-01T00:00:00Z,2026-05-01T01:00:00Z\n"

	badTimestamp := `lineItem/UsageStartDate,lineItem/UsageEndDate,lineItem/ProductCode,lineItem/UsageType,lineItem/UsageAmount,lineItem/UnblendedCost,product/region
not-a-date,2026-05-01T01:00:00Z,EC2,Box,1.0,0.1,us-east-1
`

	noResource := `lineItem/UsageStartDate,lineItem/UsageEndDate,lineItem/ProductCode,lineItem/UsageType,lineItem/UsageAmount,lineItem/UnblendedCost,product/region
2026-05-01T00:00:00Z,2026-05-01T01:00:00Z,EC2,Box,1.0,0.1,us-east-1
`

	for _, tc := range []struct {
		name    string
		in      string
		wantErr bool
		check   func(t *testing.T, got []CURRow)
	}{
		{
			name: "happy row decodes cost timestamp and resource id",
			in:   curOK,
			check: func(t *testing.T, got []CURRow) {
				t.Helper()
				if len(got) != 2 {
					t.Fatalf("rows = %d, want 2", len(got))
				}
				if got[0].UnblendedCostUSD != 0.096 {
					t.Errorf("cost = %v", got[0].UnblendedCostUSD)
				}
				if got[0].UsageStartUTC != time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC) {
					t.Errorf("start = %v", got[0].UsageStartUTC)
				}
				if got[0].ResourceID != "i-0123" {
					t.Errorf("resource id = %q", got[0].ResourceID)
				}
			},
		},
		{
			name:    "missing required column fails",
			in:      missingHeader,
			wantErr: true,
		},
		{
			name: "empty stream returns no rows without error",
			in:   "",
			check: func(t *testing.T, got []CURRow) {
				t.Helper()
				if len(got) != 0 {
					t.Errorf("got %d rows, want 0", len(got))
				}
			},
		},
		{
			name:    "bad timestamp surfaces wrapped error",
			in:      badTimestamp,
			wantErr: true,
		},
		{
			name: "optional resource id column may be absent",
			in:   noResource,
			check: func(t *testing.T, got []CURRow) {
				t.Helper()
				if got[0].ResourceID != "" {
					t.Errorf("resource id should be empty when column missing: %q", got[0].ResourceID)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseCURRows(strings.NewReader(tc.in))
			if tc.wantErr {
				if err == nil {
					t.Error("want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if tc.check != nil {
				tc.check(t, got)
			}
		})
	}
}
