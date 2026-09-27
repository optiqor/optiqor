// Package labels parses PR labels into Optiqor policy. The label set
// arrives from the GitHub webhook (Phase 4) and gates dispatch
// behaviour: skip Optiqor entirely, cap a budget, wait for Prometheus
// data to mature. Familiar pattern (CodeQL, Renovate); zero-friction
// for power users.
package labels

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// Policy is the parsed view of a PR's optiqor:* labels.
type Policy struct {
	Skip          bool
	BudgetUSDSet  bool
	BudgetUSD     float64
	WaitForProm   time.Duration
	UnknownLabels []string
}

// Parse reads a slice of PR label strings and returns the policy.
// Unknown optiqor:* labels are returned so the comment renderer can
// warn the user about typos.
func Parse(rawLabels []string) (Policy, error) {
	var p Policy
	for _, raw := range rawLabels {
		l := strings.TrimSpace(raw)
		if !strings.HasPrefix(l, "optiqor:") {
			continue
		}
		key, value, ok := splitKV(l)
		switch {
		case key == "skip" && !ok:
			p.Skip = true
		case key == "budget" && ok:
			n, err := parseBudget(value)
			if err != nil {
				return p, err
			}
			p.BudgetUSD = n
			p.BudgetUSDSet = true
		case key == "wait-for-prom" && ok:
			d, err := parseDuration(value)
			if err != nil {
				return p, err
			}
			p.WaitForProm = d
		default:
			p.UnknownLabels = append(p.UnknownLabels, raw)
		}
	}
	return p, nil
}

// splitKV splits "optiqor:key=value" into ("key", "value", true) and
// "optiqor:key" into ("key", "", false).
func splitKV(label string) (key, value string, hasValue bool) {
	body := strings.TrimPrefix(label, "optiqor:")
	if i := strings.IndexByte(body, '='); i >= 0 {
		return body[:i], body[i+1:], true
	}
	return body, "", false
}

func parseBudget(s string) (float64, error) {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "$"))
	if s == "" {
		return 0, errors.New("labels: budget needs a value: optiqor:budget=$NN")
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, errors.New("labels: budget not a number: " + s)
	}
	if n < 0 {
		return 0, errors.New("labels: budget must be non-negative")
	}
	return n, nil
}

// parseDuration accepts "7d", "12h", "30m". Go's time.ParseDuration
// rejects "d" so we handle days explicitly.
func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("labels: wait-for-prom needs a duration: optiqor:wait-for-prom=7d")
	}
	if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, errors.New("labels: wait-for-prom days not an integer: " + s)
		}
		if days < 0 {
			return 0, errors.New("labels: wait-for-prom must be non-negative")
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, errors.New("labels: wait-for-prom not a duration: " + s)
	}
	return d, nil
}
