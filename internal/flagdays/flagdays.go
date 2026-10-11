// Package flagdays fetches and parses Finnish flag-flying days ("liputuspäivät")
// for a given year from an online source, and ensures a configuration has an
// entry for a requested year, fetching on demand if one is missing.
package flagdays

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/Olarin-Aurinkovuori/calendar/internal/config"
)

// httpClient is overridable in tests.
var httpClient = &http.Client{Timeout: 20 * time.Second}

// entryPattern matches lines like "3.2. Alvar ja Aino Aallon ... päivä" or
// "20.6. Juhannus, Suomen lipun päivä | suositeltu vuodesta 2020 lukien".
var entryPattern = regexp.MustCompile(`^(\d{1,2})\.(\d{1,2})\.\s*(.+)$`)

// FetchYear downloads sourceURL (expected to follow the intermin.fi
// liputuspaivat-YYYY markup: a container holding <h2> month headings
// followed by <ul><li> or <p> entries) and parses the flag days it lists for
// the given year.
func FetchYear(ctx context.Context, sourceURL string, year int) ([]config.FlagDay, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("User-Agent", "calendar/1.0 (+flag day fetcher)")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", sourceURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: unexpected status %s", sourceURL, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response from %s: %w", sourceURL, err)
	}
	return Parse(body, year)
}

// Parse extracts flag days for the given year out of HTML content following
// the intermin.fi structure.
func Parse(htmlBody []byte, year int) ([]config.FlagDay, error) {
	doc, err := html.Parse(strings.NewReader(string(htmlBody)))
	if err != nil {
		return nil, fmt.Errorf("parsing HTML: %w", err)
	}
	container := findContentContainer(doc)
	if container == nil {
		return nil, fmt.Errorf("could not locate flag day content in page")
	}
	var entries []string
	for c := container.FirstChild; c != nil; c = c.NextSibling {
		switch {
		case c.Type == html.ElementNode && c.Data == "ul":
			for li := c.FirstChild; li != nil; li = li.NextSibling {
				if li.Type == html.ElementNode && li.Data == "li" {
					if text := normalizeText(textContent(li)); text != "" {
						entries = append(entries, text)
					}
				}
			}
		case c.Type == html.ElementNode && c.Data == "p":
			if text := normalizeText(textContent(c)); text != "" {
				entries = append(entries, text)
			}
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no flag day entries found in page")
	}

	var days []config.FlagDay
	for _, entry := range entries {
		day, ok, err := parseEntry(entry, year)
		if err != nil {
			return nil, err
		}
		if ok {
			days = append(days, day)
		}
	}
	if len(days) == 0 {
		return nil, fmt.Errorf("found entries but none matched the expected date format")
	}
	return days, nil
}

func parseEntry(entry string, year int) (config.FlagDay, bool, error) {
	mandatory := true
	text := entry
	if idx := strings.Index(entry, "|"); idx >= 0 {
		text = strings.TrimSpace(entry[:idx])
		marker := strings.ToLower(entry[idx+1:])
		if strings.Contains(marker, "suositeltu") {
			mandatory = false
		}
	}
	m := entryPattern.FindStringSubmatch(text)
	if m == nil {
		return config.FlagDay{}, false, nil
	}
	day, err := strconv.Atoi(m[1])
	if err != nil {
		return config.FlagDay{}, false, fmt.Errorf("invalid day in %q: %w", entry, err)
	}
	month, err := strconv.Atoi(m[2])
	if err != nil {
		return config.FlagDay{}, false, fmt.Errorf("invalid month in %q: %w", entry, err)
	}
	name := strings.TrimSpace(m[3])
	date := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	return config.FlagDay{
		Date:      date.Format("2006-01-02"),
		Name:      name,
		Mandatory: mandatory,
	}, true, nil
}

// findContentContainer locates the element that wraps the flag day listing.
// On intermin.fi this is a <div data-analytics-asset-type="web-content">;
// as a fallback, any element containing an <h1> whose text mentions
// "liputus" (case-insensitive) is accepted too.
func findContentContainer(n *html.Node) *html.Node {
	var found *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found != nil {
			return
		}
		if n.Type == html.ElementNode {
			for _, a := range n.Attr {
				if a.Key == "data-analytics-asset-type" && a.Val == "web-content" {
					found = n
					return
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
			if found != nil {
				return
			}
		}
	}
	walk(n)
	if found != nil {
		return found
	}
	// Fallback: find an <h1> mentioning "liputus" and use its parent.
	walk = func(n *html.Node) {
		if found != nil {
			return
		}
		if n.Type == html.ElementNode && n.Data == "h1" {
			if strings.Contains(strings.ToLower(textContent(n)), "liputus") && n.Parent != nil {
				found = n.Parent
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
			if found != nil {
				return
			}
		}
	}
	walk(n)
	return found
}

func textContent(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return sb.String()
}

func normalizeText(s string) string {
	s = strings.ReplaceAll(s, "\u00a0", " ")
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

// EnsureYear makes sure the configuration backing store has flag days
// recorded for the given year. If an entry already exists with at least one
// day -- whether stored inline or (if FlaggingDays.YearFileTemplate is
// configured) in that year's external file -- it is left untouched. If it
// is missing or empty, EnsureYear fetches the source URL (built from
// SourceURLTemplate with {year} substituted, or the existing entry's Source
// if set) and stores the result via store.UpdateFlagYear. It returns true
// if new data was fetched and stored.
func EnsureYear(ctx context.Context, store *config.Store, year int) (bool, error) {
	yearKey := strconv.Itoa(year)
	years, err := store.EffectiveFlagYears()
	if err != nil {
		return false, fmt.Errorf("reading flag days for %d: %w", year, err)
	}
	existing := years[yearKey]
	if existing != nil && len(existing.Days) > 0 {
		return false, nil
	}
	var sourceURLTemplate string
	store.View(func(cfg *config.Config) { sourceURLTemplate = cfg.FlaggingDays.SourceURLTemplate })
	sourceURL := ""
	if existing != nil && existing.Source != "" {
		sourceURL = existing.Source
	} else {
		sourceURL = strings.ReplaceAll(sourceURLTemplate, "{year}", yearKey)
	}
	if sourceURL == "" {
		return false, fmt.Errorf("no flag day source configured for year %d", year)
	}
	days, err := FetchYear(ctx, sourceURL, year)
	if err != nil {
		return false, fmt.Errorf("fetching flag days for %d: %w", year, err)
	}
	fetchedAt := time.Now().UTC().Format(time.RFC3339)
	if err := store.UpdateFlagYear(yearKey, func(_ *config.FlagYear) *config.FlagYear {
		return &config.FlagYear{Source: sourceURL, FetchedAt: fetchedAt, Days: days}
	}); err != nil {
		return false, err
	}
	return true, nil
}
