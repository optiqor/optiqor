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

func TestParsePrometheusMatrix_OK(t *testing.T) {
	got, err := ParsePrometheusMatrix(strings.NewReader(promMatrixOK))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
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
}

func TestParsePrometheusMatrix_OutOfOrderResorted(t *testing.T) {
	got, err := ParsePrometheusMatrix(strings.NewReader(promMatrixOutOfOrder))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got[0].Samples[0].At.After(got[0].Samples[1].At) {
		t.Errorf("samples not resorted")
	}
}

func TestParsePrometheusMatrix_NonSuccessStatusFails(t *testing.T) {
	_, err := ParsePrometheusMatrix(strings.NewReader(`{"status":"error","data":{"resultType":"matrix","result":[]}}`))
	if !errors.Is(err, ErrPromBadStatus) {
		t.Errorf("want ErrPromBadStatus, got %v", err)
	}
}

func TestParsePrometheusMatrix_VectorRejected(t *testing.T) {
	_, err := ParsePrometheusMatrix(strings.NewReader(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	if !errors.Is(err, ErrPromUnsupportedType) {
		t.Errorf("want ErrPromUnsupportedType, got %v", err)
	}
}

func TestParsePrometheusMatrix_MalformedJSONFails(t *testing.T) {
	_, err := ParsePrometheusMatrix(strings.NewReader(`{not json`))
	if err == nil {
		t.Error("want error on malformed JSON")
	}
}

const curOK = `lineItem/UsageStartDate,lineItem/UsageEndDate,lineItem/ProductCode,lineItem/UsageType,lineItem/UsageAmount,lineItem/UnblendedCost,product/region,lineItem/ResourceId
2026-05-01T00:00:00Z,2026-05-01T01:00:00Z,AmazonEC2,BoxUsage:m6i.large,1.0,0.096,us-east-1,i-0123
2026-05-01T01:00:00Z,2026-05-01T02:00:00Z,AmazonEC2,BoxUsage:m6i.large,1.0,0.096,us-east-1,i-0123
`

func TestParseCURRows_OK(t *testing.T) {
	got, err := ParseCURRows(strings.NewReader(curOK))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
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
}

func TestParseCURRows_MissingHeaderFails(t *testing.T) {
	missing := "lineItem/UsageStartDate,lineItem/UsageEndDate\n2026-05-01T00:00:00Z,2026-05-01T01:00:00Z\n"
	_, err := ParseCURRows(strings.NewReader(missing))
	if err == nil {
		t.Error("want error when required column absent")
	}
}

func TestParseCURRows_EmptyStream(t *testing.T) {
	got, err := ParseCURRows(strings.NewReader(""))
	if err != nil {
		t.Errorf("empty stream should not error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d rows, want 0", len(got))
	}
}

func TestParseCURRows_BadTimestampWrapped(t *testing.T) {
	bad := `lineItem/UsageStartDate,lineItem/UsageEndDate,lineItem/ProductCode,lineItem/UsageType,lineItem/UsageAmount,lineItem/UnblendedCost,product/region
not-a-date,2026-05-01T01:00:00Z,EC2,Box,1.0,0.1,us-east-1
`
	_, err := ParseCURRows(strings.NewReader(bad))
	if err == nil {
		t.Error("want timestamp parse error")
	}
}

func TestParseCURRows_OptionalResourceID(t *testing.T) {
	noRes := `lineItem/UsageStartDate,lineItem/UsageEndDate,lineItem/ProductCode,lineItem/UsageType,lineItem/UsageAmount,lineItem/UnblendedCost,product/region
2026-05-01T00:00:00Z,2026-05-01T01:00:00Z,EC2,Box,1.0,0.1,us-east-1
`
	got, err := ParseCURRows(strings.NewReader(noRes))
	if err != nil {
		t.Fatal(err)
	}
	if got[0].ResourceID != "" {
		t.Errorf("resource id should be empty when column missing: %q", got[0].ResourceID)
	}
}
