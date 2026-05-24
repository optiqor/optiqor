// Package ingestion parses Prometheus query_range matrices and AWS CUR
// rows into normalised shapes the cost + confidence engines consume.
// Parsers are pure over bytes; the HTTP / S3 / Athena clients live
// elsewhere so every parse path can be unit-tested against fixtures.
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

// PromSeries is the normalised matrix result for one series. Floats
// are kept verbatim; downstream engines accept whatever resolution
// Prometheus ships.
type PromSeries struct {
	Metric  map[string]string
	Samples []PromSample
}

type PromSample struct {
	At    time.Time
	Value float64
}

// promResponse mirrors the Prometheus 2.x query_range wire shape.
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

var ErrPromBadStatus = errors.New("ingestion: prometheus status != success")

// ErrPromUnsupportedType rejects non-matrix results. Auto-promoting an
// instant vector to a matrix would hide query-shape bugs upstream.
var ErrPromUnsupportedType = errors.New("ingestion: prometheus result is not a matrix")

// ParsePrometheusMatrix returns series with samples sorted
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
	// Prometheus emits [seconds-since-epoch-as-number, value-as-string].
	tsRaw, ok := p[0].(float64)
	if !ok {
		return PromSample{}, fmt.Errorf("expected float ts, got %T", p[0])
	}
	valStr, ok := p[1].(string)
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

// CURRow is the narrowed projection of an AWS CUR row that Optiqor
// signs into Receipts. The full CUR has hundreds of columns; widening
// this struct changes the signature surface, so add fields deliberately.
type CURRow struct {
	UsageStartUTC    time.Time
	UsageEndUTC      time.Time
	ServiceCode      string // e.g. AmazonEC2
	UsageType        string // e.g. BoxUsage:m6i.large
	Region           string
	ResourceID       string // optional; typically present for K8s nodes
	UsageQuantity    float64
	UnblendedCostUSD float64
}

// ParseCURRows tolerates unknown columns; missing required ones fail.
func ParseCURRows(r io.Reader) ([]CURRow, error) {
	rd := csv.NewReader(r)
	header, err := rd.Read()
	if errors.Is(err, io.EOF) {
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
		if errors.Is(err, io.EOF) {
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

// requiredCURColumns are the verbatim AWS CUR column names we depend on.
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
	resourceID                             int // -1 when absent
	usage, unblendedCost                   int
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
