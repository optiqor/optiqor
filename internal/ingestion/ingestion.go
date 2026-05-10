// Package ingestion parses the two upstream telemetry sources Optiqor
// trusts for cost + utilization data:
//
//   - Prometheus `/api/v1/query_range` responses (the in-cluster agent
//     ships these from the customer's Prometheus into the SaaS).
//   - AWS Cost & Usage Report (CUR) rows (read off Athena once the
//     daily CUR drop lands in S3).
//
// The parsers are pure functions over JSON / CSV bytes; the live HTTP
// + S3 clients live elsewhere. Splitting it this way means we can
// unit-test every parse path against fixture bytes — production-grade
// CUR ingestion has historically been a graveyard for off-by-one
// bugs, and the safer pattern is "parsers are deterministic, callers
// are integration-tested."
package ingestion

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---- Prometheus query_range ------------------------------------------

// PromSeries is the normalised representation of a Prometheus
// `query_range` matrix result for one series. The samples are
// chronologically ordered and floats are kept verbatim — the cost +
// confidence engines accept whatever resolution Prometheus ships.
type PromSeries struct {
	Metric  map[string]string
	Samples []PromSample
}

// PromSample is one (timestamp, value) pair.
type PromSample struct {
	At    time.Time
	Value float64
}

// promResponse mirrors the on-the-wire shape Prometheus 2.x returns.
type promResponse struct {
	Status string         `json:"status"`
	Data   promResultData `json:"data"`
}

type promResultData struct {
	ResultType string            `json:"resultType"`
	Result     []json.RawMessage `json:"result"`
}

type promMatrixEntry struct {
	Metric map[string]string `json:"metric"`
	Values [][2]any          `json:"values"`
}

// ErrPromBadStatus is returned when Prometheus' top-level status is
// not "success" — usually means the query syntax was wrong.
var ErrPromBadStatus = errors.New("ingestion: prometheus status != success")

// ErrPromUnsupportedType is returned when the result is not a matrix
// (e.g. an instant `vector`). We deliberately don't auto-promote.
var ErrPromUnsupportedType = errors.New("ingestion: prometheus result is not a matrix")

// ParsePrometheusMatrix decodes one Prometheus matrix response into a
// slice of normalised series. Samples within each series are sorted
// chronologically. Empty matrices return (nil, nil).
func ParsePrometheusMatrix(r io.Reader) ([]PromSeries, error) {
	var raw promResponse
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, fmt.Errorf("ingestion: prometheus decode: %w", err)
	}
	if raw.Status != "success" {
		return nil, fmt.Errorf("%w (got %q)", ErrPromBadStatus, raw.Status)
	}
	if raw.Data.ResultType != "matrix" {
		return nil, fmt.Errorf("%w (got %q)", ErrPromUnsupportedType, raw.Data.ResultType)
	}
	out := make([]PromSeries, 0, len(raw.Data.Result))
	for i, rmsg := range raw.Data.Result {
		var entry promMatrixEntry
		if err := json.Unmarshal(rmsg, &entry); err != nil {
			return nil, fmt.Errorf("ingestion: prometheus matrix[%d]: %w", i, err)
		}
		series := PromSeries{Metric: entry.Metric}
		series.Samples = make([]PromSample, 0, len(entry.Values))
		for j, pair := range entry.Values {
			s, err := promPairToSample(pair)
			if err != nil {
				return nil, fmt.Errorf("ingestion: prometheus matrix[%d][%d]: %w", i, j, err)
			}
			series.Samples = append(series.Samples, s)
		}
		sort.SliceStable(series.Samples, func(a, b int) bool {
			return series.Samples[a].At.Before(series.Samples[b].At)
		})
		out = append(out, series)
	}
	return out, nil
}

func promPairToSample(p [2]any) (PromSample, error) {
	tsRaw, ok := p[0].(float64) // Prometheus emits seconds-since-epoch as a JSON number
	if !ok {
		return PromSample{}, fmt.Errorf("expected float ts, got %T", p[0])
	}
	valStr, ok := p[1].(string) // and stringified values
	if !ok {
		return PromSample{}, fmt.Errorf("expected string value, got %T", p[1])
	}
	v, err := strconv.ParseFloat(valStr, 64)
	if err != nil {
		return PromSample{}, fmt.Errorf("parse value %q: %w", valStr, err)
	}
	sec := int64(tsRaw)
	nsec := int64((tsRaw - float64(sec)) * 1e9)
	return PromSample{
		At:    time.Unix(sec, nsec).UTC(),
		Value: v,
	}, nil
}

// ---- AWS CUR row ----------------------------------------------------

// CURRow is the subset of the AWS Cost & Usage Report that Optiqor
// trusts. The full CUR has hundreds of columns; we deliberately
// narrow to the few we sign into Receipts so future widening doesn't
// quietly change the signature surface.
type CURRow struct {
	UsageStartUTC   time.Time
	UsageEndUTC     time.Time
	ServiceCode     string  // e.g. AmazonEC2
	UsageType       string  // e.g. BoxUsage:m6i.large
	Region          string
	ResourceID      string  // optional but typical for K8s nodes
	UsageQuantity   float64
	UnblendedCostUSD float64
}

// ParseCURRows reads a CUR CSV stream and returns rows projected onto
// CURRow. Unknown headers are tolerated; missing required ones return
// an error.
func ParseCURRows(r io.Reader) ([]CURRow, error) {
	rd := csv.NewReader(r)
	header, err := rd.Read()
	if err == io.EOF {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ingestion: cur header: %w", err)
	}
	idx, err := indexCURHeader(header)
	if err != nil {
		return nil, err
	}
	out := make([]CURRow, 0, 64)
	row := 0
	for {
		rec, err := rd.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("ingestion: cur row %d: %w", row, err)
		}
		r, err := decodeCURRow(rec, idx, row)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
		row++
	}
	return out, nil
}

// Required CUR columns. The strings are the verbatim AWS column names.
var requiredCURColumns = []string{
	"lineItem/UsageStartDate",
	"lineItem/UsageEndDate",
	"lineItem/ProductCode",
	"lineItem/UsageType",
	"lineItem/UsageAmount",
	"lineItem/UnblendedCost",
	"product/region",
}

type curIndex struct {
	start, end, service, usageType, region int
	resourceID                              int // -1 when absent
	usage, unblendedCost                    int
}

func indexCURHeader(header []string) (curIndex, error) {
	idx := curIndex{resourceID: -1}
	have := map[string]int{}
	for i, h := range header {
		have[strings.TrimSpace(h)] = i
	}
	for _, c := range requiredCURColumns {
		if _, ok := have[c]; !ok {
			return idx, fmt.Errorf("ingestion: cur header missing %q", c)
		}
	}
	idx.start = have["lineItem/UsageStartDate"]
	idx.end = have["lineItem/UsageEndDate"]
	idx.service = have["lineItem/ProductCode"]
	idx.usageType = have["lineItem/UsageType"]
	idx.usage = have["lineItem/UsageAmount"]
	idx.unblendedCost = have["lineItem/UnblendedCost"]
	idx.region = have["product/region"]
	if i, ok := have["lineItem/ResourceId"]; ok {
		idx.resourceID = i
	}
	return idx, nil
}

func decodeCURRow(rec []string, idx curIndex, row int) (CURRow, error) {
	start, err := time.Parse(time.RFC3339, rec[idx.start])
	if err != nil {
		return CURRow{}, fmt.Errorf("ingestion: cur row %d: start time %q: %w", row, rec[idx.start], err)
	}
	end, err := time.Parse(time.RFC3339, rec[idx.end])
	if err != nil {
		return CURRow{}, fmt.Errorf("ingestion: cur row %d: end time %q: %w", row, rec[idx.end], err)
	}
	usage, err := strconv.ParseFloat(rec[idx.usage], 64)
	if err != nil {
		return CURRow{}, fmt.Errorf("ingestion: cur row %d: usage %q: %w", row, rec[idx.usage], err)
	}
	cost, err := strconv.ParseFloat(rec[idx.unblendedCost], 64)
	if err != nil {
		return CURRow{}, fmt.Errorf("ingestion: cur row %d: cost %q: %w", row, rec[idx.unblendedCost], err)
	}
	out := CURRow{
		UsageStartUTC:    start.UTC(),
		UsageEndUTC:      end.UTC(),
		ServiceCode:      rec[idx.service],
		UsageType:        rec[idx.usageType],
		Region:           rec[idx.region],
		UsageQuantity:    usage,
		UnblendedCostUSD: cost,
	}
	if idx.resourceID >= 0 {
		out.ResourceID = rec[idx.resourceID]
	}
	return out, nil
}
