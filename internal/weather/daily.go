package weather

import (
	"sort"
	"time"
)

// VariableValue is one variable a condition expression referenced, and the
// value it was evaluated against for one particular day.
type VariableValue struct {
	Variable string
	Value    float64
}

// DayResult is one calendar day's evaluation of a condition expression.
type DayResult struct {
	Date    string // YYYY-MM-DD, in the location EvaluateByDay was called with
	Matched bool
	// Values lists every variable the expression referenced that day
	// (in first-referenced order), together with its resolved value --
	// useful for a human-readable "why did this trigger" tooltip.
	Values []VariableValue
}

// groupByDay buckets samples by their calendar day in loc.
func groupByDay(samples []Sample, loc *time.Location) map[string][]Sample {
	out := map[string][]Sample{}
	for _, s := range samples {
		day := s.Time.In(loc).Format("2006-01-02")
		out[day] = append(out[day], s)
	}
	return out
}

// EvaluateByDay evaluates expression independently for each calendar day
// present in samples (grouped by their local day in loc, e.g. time.Local),
// returning one DayResult per day with any forecast data, in chronological
// order. It returns an error if expression has invalid syntax (checked
// against every day, so a syntax error is reported even if all its
// variables happen to be missing on some days).
func EvaluateByDay(samples []Sample, expression string, loc *time.Location) ([]DayResult, error) {
	byDay := groupByDay(samples, loc)
	dates := make([]string, 0, len(byDay))
	for d := range byDay {
		dates = append(dates, d)
	}
	sort.Strings(dates)

	results := make([]DayResult, 0, len(dates))
	for _, d := range dates {
		base := lookupFor(byDay[d])
		var order []string
		seen := map[string]bool{}
		values := map[string]float64{}
		recording := func(variable string) (float64, bool) {
			v, ok := base(variable)
			if ok && !seen[variable] {
				seen[variable] = true
				order = append(order, variable)
				values[variable] = v
			}
			return v, ok
		}
		matched, err := Evaluate(expression, recording)
		if err != nil {
			return nil, err
		}
		vv := make([]VariableValue, 0, len(order))
		for _, name := range order {
			vv = append(vv, VariableValue{Variable: name, Value: values[name]})
		}
		results = append(results, DayResult{Date: d, Matched: matched, Values: vv})
	}
	return results, nil
}
