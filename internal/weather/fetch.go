// Package weather fetches short-term forecast data (temperature, rainfall,
// snow accumulation) from the Finnish Meteorological Institute's (FMI) open
// data WFS API, and evaluates simple configured boolean conditions over it
// (e.g. "$MIN_TEMPERATURE <= 2") to surface admin-configured hint text on
// the today's-summary view.
package weather

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// httpClient is overridable in tests.
var httpClient = &http.Client{Timeout: 20 * time.Second}

// Sample is one forecast time point, with every requested variable that
// had a numeric (non-missing) value at that point.
type Sample struct {
	Time   time.Time
	Values map[string]float64 // canonical variable name (e.g. "TEMPERATURE") -> value
}

// fieldVariable maps the FMI forecast parameter names this application
// requests to the canonical variable names used in condition expressions
// (see Evaluate/lookupFor). An unrecognized field in the response (e.g. if
// the configured URL's "parameters=" list is customized to request
// something else) is parsed but ignored.
var fieldVariable = map[string]string{
	"Temperature":      "TEMPERATURE",
	"Precipitation1h":  "RAINFALL",
	"SnowAccumulation": "SNOW",
}

// wfsFeatureCollection and the nested types below map just enough of the
// FMI WFS 2.0 "multipointcoverage" stored-query response to extract the
// forecast time series. Field matching in encoding/xml is by local name,
// so the "wfs:"/"gml:"/"gmlcov:"/etc. namespace prefixes in the real
// response don't need to be spelled out here.
type wfsFeatureCollection struct {
	Members []wfsMember `xml:"member"`
}

type wfsMember struct {
	Observation gridSeriesObservation `xml:"GridSeriesObservation"`
}

type gridSeriesObservation struct {
	Result omResult `xml:"result"`
}

type omResult struct {
	Coverage multiPointCoverage `xml:"MultiPointCoverage"`
}

type multiPointCoverage struct {
	// Positions holds whitespace-separated "lat lon unixtime" triples, one
	// triple per forecast time point, in the same order as Values' tuples.
	Positions string `xml:"domainSet>SimpleMultiPoint>positions"`
	// Values holds one whitespace-separated tuple per time point, each
	// tuple holding one value per field in Fields' order.
	Values string  `xml:"rangeSet>DataBlock>doubleOrNilReasonTupleList"`
	Fields []field `xml:"rangeType>DataRecord>field"`
}

type field struct {
	Name string `xml:"name,attr"`
}

// Fetch downloads and parses the forecast for the window [start, end] at
// the given place. urlTemplate is the full request URL with the literal
// placeholders "{place}", "{start}", and "{end}" substituted; start/end
// are formatted as UTC RFC3339 timestamps.
func Fetch(ctx context.Context, urlTemplate, place string, start, end time.Time) ([]Sample, error) {
	url := strings.NewReplacer(
		"{place}", place,
		"{start}", start.UTC().Format(time.RFC3339),
		"{end}", end.UTC().Format(time.RFC3339),
	).Replace(urlTemplate)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("User-Agent", "calendar/1.0 (+weather forecast fetcher)")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: unexpected status %s", url, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response from %s: %w", url, err)
	}
	return Parse(body)
}

// Parse extracts the forecast time series out of an FMI WFS
// "multipointcoverage" XML response body.
func Parse(body []byte) ([]Sample, error) {
	var fc wfsFeatureCollection
	if err := xml.Unmarshal(body, &fc); err != nil {
		return nil, fmt.Errorf("parsing WFS response: %w", err)
	}
	if len(fc.Members) == 0 {
		return nil, fmt.Errorf("no observations in WFS response")
	}
	cov := fc.Members[0].Observation.Result.Coverage

	positions := strings.Fields(cov.Positions)
	if len(positions) == 0 {
		return nil, fmt.Errorf("no forecast positions in WFS response")
	}
	if len(positions)%3 != 0 {
		return nil, fmt.Errorf("unexpected position token count %d (not a multiple of 3)", len(positions))
	}
	numPoints := len(positions) / 3

	if len(cov.Fields) == 0 {
		return nil, fmt.Errorf("no range fields in WFS response")
	}
	fieldNames := make([]string, len(cov.Fields))
	for i, f := range cov.Fields {
		fieldNames[i] = f.Name
	}

	values := strings.Fields(cov.Values)
	if len(values) != numPoints*len(fieldNames) {
		return nil, fmt.Errorf("value token count %d does not match %d points x %d fields", len(values), numPoints, len(fieldNames))
	}

	samples := make([]Sample, 0, numPoints)
	for i := 0; i < numPoints; i++ {
		unixSec, err := strconv.ParseInt(positions[i*3+2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid timestamp %q: %w", positions[i*3+2], err)
		}
		sample := Sample{Time: time.Unix(unixSec, 0).UTC(), Values: map[string]float64{}}
		for j, name := range fieldNames {
			raw := values[i*len(fieldNames)+j]
			v, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				// Missing/"NaN" value for this field at this point: skip it
				// rather than failing the whole fetch.
				continue
			}
			if variable, ok := fieldVariable[name]; ok {
				sample.Values[variable] = v
			}
		}
		samples = append(samples, sample)
	}
	return samples, nil
}

// Aggregate computes one of "MIN", "MAX", "MEAN", or "AVG" ("MEAN" and
// "AVG" are synonyms) over every Sample that has a value for variable
// (e.g. "TEMPERATURE"). ok is false if no sample had that variable.
func Aggregate(samples []Sample, variable, kind string) (value float64, ok bool) {
	var values []float64
	for _, s := range samples {
		if v, has := s.Values[variable]; has {
			values = append(values, v)
		}
	}
	if len(values) == 0 {
		return 0, false
	}
	switch kind {
	case "MIN":
		m := values[0]
		for _, v := range values[1:] {
			if v < m {
				m = v
			}
		}
		return m, true
	case "MAX":
		m := values[0]
		for _, v := range values[1:] {
			if v > m {
				m = v
			}
		}
		return m, true
	case "MEAN", "AVG":
		var sum float64
		for _, v := range values {
			sum += v
		}
		return sum / float64(len(values)), true
	default:
		return 0, false
	}
}

// lookupFor returns a variable lookup function (for Evaluate) backed by
// samples: it recognizes "MIN_", "MAX_", "MEAN_", and "AVG_" prefixes
// (e.g. "MIN_TEMPERATURE") and dispatches to Aggregate for the remainder.
func lookupFor(samples []Sample) func(string) (float64, bool) {
	return func(variable string) (float64, bool) {
		var kind, base string
		switch {
		case strings.HasPrefix(variable, "MIN_"):
			kind, base = "MIN", variable[len("MIN_"):]
		case strings.HasPrefix(variable, "MAX_"):
			kind, base = "MAX", variable[len("MAX_"):]
		case strings.HasPrefix(variable, "MEAN_"):
			kind, base = "MEAN", variable[len("MEAN_"):]
		case strings.HasPrefix(variable, "AVG_"):
			kind, base = "AVG", variable[len("AVG_"):]
		default:
			return 0, false
		}
		return Aggregate(samples, base, kind)
	}
}
