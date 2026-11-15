package spend

import (
	"math"
	"strings"
	"time"
)

// Price is a model's list price from a date on. Costs are worked out when
// the page is read, not stored, so adding a missing price here also prices
// the calls made before it was added.
type Price struct {
	Model       string    // provider-neutral name; also matches dated snapshots (gpt-5.4-mini-2026-03-17)
	From        time.Time // in force from this day (UTC)
	Input       float64   // USD per 1M prompt tokens
	CachedInput float64   // USD per 1M cached prompt tokens; 0 = same as Input
	Output      float64   // USD per 1M completion tokens
	PerMinute   float64   // USD per audio minute (duration-billed transcription)
}

func day(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

// Prices are OpenAI's standard (not batch or priority) list prices, short
// context, read from https://developers.openai.com/api/docs/pricing on
// 2026-11-16. OpenRouter passes OpenAI models through at the same price.
// To change a price, add a row with a later From; keep the old one so
// earlier days stay priced as they were billed.
var Prices = []Price{
	{Model: "gpt-6-luna", From: day("2026-01-01"), Input: 0.10, CachedInput: 0.01, Output: 0.50},
	{Model: "gpt-6-sol", From: day("2026-01-01"), Input: 2.00, CachedInput: 0.20, Output: 10.00},
	{Model: "gpt-6.1-sol", From: day("2026-01-01"), Input: 2.00, CachedInput: 0.10, Output: 10.00},
	{Model: "gpt-6.1-terra", From: day("2026-01-01"), Input: 1.00, CachedInput: 0.05, Output: 5.00},
	{Model: "gpt-5.6-luna", From: day("2026-01-01"), Input: 0.20, CachedInput: 0.02, Output: 1.20},
	{Model: "gpt-5.4", From: day("2026-01-01"), Input: 2.50, CachedInput: 0.25, Output: 15.00},
	{Model: "gpt-5.4-mini", From: day("2026-01-01"), Input: 0.75, CachedInput: 0.075, Output: 4.50},
	{Model: "gpt-5.4-nano", From: day("2026-01-01"), Input: 0.20, CachedInput: 0.02, Output: 1.25},
	{Model: "gpt-5-mini", From: day("2026-01-01"), Input: 0.25, CachedInput: 0.025, Output: 2.00},
	{Model: "gpt-5-nano", From: day("2026-01-01"), Input: 0.05, CachedInput: 0.005, Output: 0.40},
	{Model: "gpt-4.1-mini", From: day("2026-01-01"), Input: 0.40, CachedInput: 0.10, Output: 1.60},
	{Model: "gpt-4o-mini", From: day("2026-01-01"), Input: 0.15, CachedInput: 0.075, Output: 0.60},
	{Model: "text-embedding-3-large", From: day("2026-01-01"), Input: 0.13},
	{Model: "text-embedding-3-small", From: day("2026-01-01"), Input: 0.02},
	{Model: "gpt-4o-mini-transcribe", From: day("2026-01-01"), Input: 1.25, Output: 5.00, PerMinute: 0.003},
	{Model: "gpt-4o-transcribe", From: day("2026-01-01"), Input: 2.50, Output: 10.00, PerMinute: 0.006},
	{Model: "whisper-1", From: day("2026-01-01"), PerMinute: 0.006},
}

// normalize drops the provider prefix (openai/gpt-6-luna) and a variant
// suffix (:free) so OpenAI and OpenRouter names meet.
func normalize(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	if i := strings.LastIndexByte(m, '/'); i >= 0 {
		m = m[i+1:]
	}
	if i := strings.IndexByte(m, ':'); i >= 0 {
		m = m[:i]
	}
	return m
}

// Lookup finds the price of model in force on day: the longest matching
// name (so gpt-5.4-mini wins over gpt-5.4), then the latest From not after day.
func Lookup(prices []Price, model string, on time.Time) (Price, bool) {
	m := normalize(model)
	var best Price
	found := false
	for _, p := range prices {
		if m != p.Model && !strings.HasPrefix(m, p.Model+"-") {
			continue
		}
		if p.From.After(on) {
			continue
		}
		if !found || len(p.Model) > len(best.Model) || (len(p.Model) == len(best.Model) && p.From.After(best.From)) {
			best, found = p, true
		}
	}
	return best, found
}

// Usage is summed token use of one model.
type Usage struct {
	InputTokens, CachedTokens, OutputTokens int64
	AudioSeconds                            float64
}

// Add sums u and o.
func (u Usage) Add(o Usage) Usage {
	return Usage{u.InputTokens + o.InputTokens, u.CachedTokens + o.CachedTokens,
		u.OutputTokens + o.OutputTokens, u.AudioSeconds + o.AudioSeconds}
}

// Micros is an amount in millionths of a US dollar.
type Micros int64

// USD is m in dollars.
func (m Micros) USD() float64 { return float64(m) / 1e6 }

// Cost prices u at p.
func (p Price) Cost(u Usage) Micros {
	cached := min(max(u.CachedTokens, 0), u.InputTokens)
	cachedRate := p.CachedInput
	if cachedRate == 0 {
		cachedRate = p.Input
	}
	usd := (float64(u.InputTokens-cached)*p.Input + float64(cached)*cachedRate + float64(u.OutputTokens)*p.Output) / 1e6
	usd += u.AudioSeconds / 60 * p.PerMinute
	return Micros(math.Round(usd * 1e6))
}
