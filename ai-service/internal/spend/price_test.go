package spend

import (
	"testing"
	"time"
)

func TestLookup_MatchesProviderPrefixAndSnapshot(t *testing.T) {
	cases := map[string]string{
		"gpt-6-luna":                    "gpt-6-luna",
		"openai/gpt-6-luna":             "gpt-6-luna",
		"gpt-5.4-mini-2026-03-17":       "gpt-5.4-mini", // longest name wins over gpt-5.4
		"openai/gpt-5.4-mini:free":      "gpt-5.4-mini",
		"gpt-5.4":                       "gpt-5.4",
		"OpenAI/Text-Embedding-3-Large": "text-embedding-3-large",
	}
	for in, want := range cases {
		// Arrange
		on := day("2026-11-16")

		// Act
		p, ok := Lookup(Prices, in, on)

		// Assert
		if !ok || p.Model != want {
			t.Errorf("Lookup(%q) = %q, %v; want %q", in, p.Model, ok, want)
		}
	}
}

func TestLookup_UnknownModelAndLookalikes(t *testing.T) {
	// Arrange
	on := day("2026-11-16")

	// Act
	_, unknown := Lookup(Prices, "anthropic/claude-x", on)
	_, lookalike := Lookup(Prices, "gpt-6-lunatic", on) // not a snapshot of gpt-6-luna
	_, empty := Lookup(Prices, "", on)

	// Assert
	if unknown || lookalike || empty {
		t.Fatalf("unknown=%v lookalike=%v empty=%v", unknown, lookalike, empty)
	}
}

func TestLookup_UsesPriceInForceThatDay(t *testing.T) {
	// Arrange: a price cut on 2026-12-01
	prices := []Price{
		{Model: "m", From: day("2026-01-01"), Input: 2},
		{Model: "m", From: day("2026-12-01"), Input: 1},
	}

	// Act
	nov, _ := Lookup(prices, "m", day("2026-11-30"))
	dec, _ := Lookup(prices, "m", day("2026-12-01"))
	_, before := Lookup(prices, "m", day("2025-12-31"))

	// Assert
	if nov.Input != 2 || dec.Input != 1 || before {
		t.Fatalf("nov=%v dec=%v before=%v", nov.Input, dec.Input, before)
	}
}

func TestCost_TokensCachedAndMinutes(t *testing.T) {
	// Arrange
	luna := Price{Input: 0.10, CachedInput: 0.01, Output: 0.50}
	noCacheRate := Price{Input: 1}
	whisper := Price{PerMinute: 0.006}

	// Act
	got := luna.Cost(Usage{InputTokens: 1_000_000, CachedTokens: 400_000, OutputTokens: 100_000})
	cachedAtInput := noCacheRate.Cost(Usage{InputTokens: 1_000_000, CachedTokens: 500_000})
	minutes := whisper.Cost(Usage{AudioSeconds: 90})
	badCache := luna.Cost(Usage{InputTokens: 10, CachedTokens: 50}) // more cached than sent: capped

	// Assert: 0.6M*0.10 + 0.4M*0.01 + 0.1M*0.50 = 0.06+0.004+0.05
	if got != 114_000 || cachedAtInput != 1_000_000 || minutes != 9_000 || badCache != 0 {
		t.Fatalf("got=%d cachedAtInput=%d minutes=%d badCache=%d", got, cachedAtInput, minutes, badCache)
	}
}

func TestPrices_AreSane(t *testing.T) {
	for _, p := range Prices {
		if p.Model != normalize(p.Model) || p.From.IsZero() || p.From.After(time.Now()) ||
			p.Input < 0 || p.Output < 0 || p.CachedInput < 0 || p.PerMinute < 0 ||
			(p.Input == 0 && p.Output == 0 && p.PerMinute == 0) {
			t.Errorf("bad price row %+v", p)
		}
	}
}
