package weather

import (
	"context"
	"sync"
	"time"
)

// Cache fetches and caches a single configured forecast, re-fetching only
// after its refresh interval has elapsed, so that page loads don't hit the
// upstream forecast API on every request.
type Cache struct {
	mu        sync.Mutex
	expiresAt time.Time
	samples   []Sample
	err       error
}

// DefaultRefreshIntervalHours is how often the forecast is refetched when
// Config.RefreshIntervalHours is unset/non-positive.
const DefaultRefreshIntervalHours = 4

// DefaultLookaheadHours is how far ahead the forecast window extends from
// now when Config.LookaheadHours is unset/non-positive.
const DefaultLookaheadHours = 24

// Config is the subset of settings Cache.Get needs to fetch a forecast. It
// mirrors (and is populated from) config.WeatherConfig, kept separate so
// this package does not need to import internal/config.
type Config struct {
	ForecastURLTemplate  string
	Place                string
	LookaheadHours       int
	RefreshIntervalHours int
}

// Get returns the cached forecast samples, fetching (and caching) a fresh
// one if the cache is empty or has expired. now is passed in (rather than
// computed internally) so callers can keep window computation and cache
// expiry consistent with a single "now" per request.
func (c *Cache) Get(ctx context.Context, cfg Config, now time.Time) ([]Sample, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.expiresAt.IsZero() && now.Before(c.expiresAt) {
		return c.samples, c.err
	}

	lookahead := cfg.LookaheadHours
	if lookahead <= 0 {
		lookahead = DefaultLookaheadHours
	}
	refresh := cfg.RefreshIntervalHours
	if refresh <= 0 {
		refresh = DefaultRefreshIntervalHours
	}

	start := now
	end := now.Add(time.Duration(lookahead) * time.Hour)
	c.samples, c.err = Fetch(ctx, cfg.ForecastURLTemplate, cfg.Place, start, end)
	c.expiresAt = now.Add(time.Duration(refresh) * time.Hour)
	return c.samples, c.err
}

// Condition is one configured threshold expression paired with the text to
// show when it evaluates true. It mirrors config.WeatherCondition.
type Condition struct {
	Expression string
	Text       string
}

// ActiveNotices evaluates every condition against samples (in order) and
// returns the Text of each one that evaluates true. A condition with
// invalid syntax, or one that references a variable with no data in
// samples, is reported via onError (if non-nil) and otherwise skipped,
// rather than failing the whole evaluation.
func ActiveNotices(samples []Sample, conditions []Condition, onError func(condition string, err error)) []string {
	lookup := lookupFor(samples)
	var notices []string
	for _, c := range conditions {
		ok, err := Evaluate(c.Expression, lookup)
		if err != nil {
			if onError != nil {
				onError(c.Expression, err)
			}
			continue
		}
		if ok {
			notices = append(notices, c.Text)
		}
	}
	return notices
}
