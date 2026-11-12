package stats

import "strings"

// ---- estimated cost, at Anthropic's API prices ----

// Price is what a model's tokens cost on the Claude API, in dollars per
// million tokens.
type Price struct {
	Input, Write5m, Write1h, Read, Output float64
}

// prices are the Claude API's list prices
// (platform.claude.com/docs/en/about-claude/pricing, November 2026), by the
// start of a model id without "claude-". Longer prefixes come first, so
// "opus-4-1" is matched before "opus-4".
var prices = []struct {
	prefix string
	price  Price
}{
	{"fable-5-2", Price{8, 10, 16, 0.20, 40}},
	{"mythos-5-2", Price{8, 10, 16, 0.20, 40}},
	{"fable-5-1", Price{10, 12.5, 20, 0.25, 50}},
	{"mythos-5-1", Price{10, 12.5, 20, 0.25, 50}},
	{"fable-5", Price{10, 12.5, 20, 1, 50}},
	{"mythos-5", Price{10, 12.5, 20, 1, 50}},
	{"opus-5-5", Price{4, 5, 8, 0.20, 20}},
	{"opus-5", Price{5, 6.25, 10, 0.50, 25}},
	{"opus-4-8", Price{5, 6.25, 10, 0.50, 25}},
	{"opus-4-7", Price{5, 6.25, 10, 0.50, 25}},
	{"opus-4-6", Price{5, 6.25, 10, 0.50, 25}},
	{"opus-4-5", Price{5, 6.25, 10, 0.50, 25}},
	{"opus-4-1", Price{15, 18.75, 30, 1.50, 75}},
	{"opus-4", Price{15, 18.75, 30, 1.50, 75}},
	{"sonnet-5-5", Price{2, 2.5, 4, 0.10, 10}},
	{"sonnet-5", Price{2, 2.5, 4, 0.20, 10}},
	{"sonnet-4", Price{3, 3.75, 6, 0.30, 15}}, // 4, 4.5 and 4.6 cost the same
	{"haiku-5-5", Price{0.10, 0.125, 0.20, 0.01, 0.50}},
	{"haiku-4-5", Price{1, 1.25, 2, 0.10, 5}},
}

// haiku55Long is Claude Haiku 5.5's price for a request whose prompt is over
// 100,000 tokens (cache reads and writes count towards it).
var haiku55Long = Price{0.50, 0.625, 1, 0.05, 2.50}

// latest is the price of each family's newest model: a model id Quarters
// doesn't know yet is far more likely to be new than old.
var latest = map[string]string{"fable": "fable-5-2", "mythos": "mythos-5-2", "opus": "opus-5-5", "sonnet": "sonnet-5-5", "haiku": "haiku-5-5"}

// modelKey is a model id without its provider prefix ("us.anthropic."),
// "claude-" and a "[1m]" suffix, lower case: "opus-5-5".
func modelKey(model string) string {
	m := strings.ToLower(model)
	if i := strings.Index(m, "claude-"); i >= 0 {
		m = m[i+len("claude-"):]
	}
	m, _, _ = strings.Cut(m, "[")
	return m
}

// PriceOf is a model's API price; ok is false for a model that isn't
// Claude's (or Claude Code's "<synthetic>" replies, which cost nothing).
func PriceOf(model string) (p Price, ok bool) {
	key := modelKey(model)
	for _, e := range prices {
		if strings.HasPrefix(key, e.prefix) {
			return e.price, true
		}
	}
	for family, newest := range latest {
		if strings.Contains(key, family) {
			return PriceOf("claude-" + newest)
		}
	}
	return Price{}, false
}

// Response is one API response's usage as a session log records it.
type Response struct {
	Model   string
	Usage   Usage
	Write1h int    // the part of Usage.CacheWrite kept for an hour (the rest: 5 minutes)
	Speed   string // "fast" for fast mode
	Geo     string // inference_geo: "us" costs 10% more
}

// Cost estimates what a response would cost at the Claude API's list
// prices, in dollars. On a Pro or Max plan nothing is paid per token: it
// shows what the plan is worth.
func (r Response) Cost() float64 {
	p, ok := PriceOf(r.Model)
	if !ok {
		return 0
	}
	u := r.Usage
	if strings.HasPrefix(modelKey(r.Model), "haiku-5-5") && u.Input+u.CacheRead+u.CacheWrite > 100_000 {
		p = haiku55Long
	}
	w1h := min(max(r.Write1h, 0), u.CacheWrite)
	cost := (float64(u.Input)*p.Input + float64(u.CacheWrite-w1h)*p.Write5m + float64(w1h)*p.Write1h +
		float64(u.CacheRead)*p.Read + float64(u.Output)*p.Output) / 1e6
	if r.Speed == "fast" {
		cost *= 2 // fast mode is twice the price, caching multipliers on top
	}
	if r.Geo == "us" {
		cost *= 1.1
	}
	return cost
}
