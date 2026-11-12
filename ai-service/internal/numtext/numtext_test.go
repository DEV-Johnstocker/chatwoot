package numtext

import (
	"strings"
	"testing"
)

func TestLong_FindsDisguisedNumbers(t *testing.T) {
	cases := map[string]string{
		"plain":            "โอน 9952310945",
		"dashes":           "995-2-31094-5",
		"dots":             "995.231.0945",
		"slashes":          "995/231/0945",
		"spaced dashes":    "995 - 231 - 0945",
		"two spaces":       "995  2310945",
		"en dash":          "995–231–0945",
		"non-breaking":     "995\u00a02310945",
		"thin space":       "995\u20092310945",
		"zero-width":       "995\u200b2310945",
		"word joiner":      "995\u20602310945",
		"BOM":              "995\ufeff2310945",
		"thai digits":      "๙๙๕๒๓๑๐๙๔๕",
		"full-width":       "９９５２３１０９４５",
		"math bold":        "𝟗𝟗𝟓𝟐𝟑𝟏𝟎𝟗𝟒𝟓",
		"keycaps":          "9️⃣9️⃣5️⃣2️⃣3️⃣1️⃣0️⃣9️⃣4️⃣5️⃣",
		"line break":       "99523\n10945",
		"O for zero":       "995231O945",
		"long dot run":     "995.......2310945",
		"date then digits": "08:12\n2310945",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			// Act
			got := Long(in)

			// Assert
			if len(got) != 1 || len(got[0]) < 7 {
				t.Fatalf("Long(%q) = %v, want one long number", in, got)
			}
		})
	}
}

func TestLong_LeavesTimesDatesAndPrices(t *testing.T) {
	for _, in := range []string{
		"ราคา 9,900 บาท โปร 3,900", "วันที่ 2026-11-30 10:00", "30/11/2569", "09:00-17:00",
		"1,250,000 บาท", "30 ที่นั่ง 18 บทเรียน", "VAT 7% เป็น 4,173 บาท",
		"วันที่ 2026-12-12 09:00-17:00", "12 ธ.ค. 2569 09:00–17:00", "30 พ.ย. 2026 10:00", "12/12/2569 09.00 - 17.00",
	} {
		t.Run(in, func(t *testing.T) {
			// Act
			got := Long(in)

			// Assert
			if len(got) != 0 {
				t.Fatalf("Long(%q) = %v, want none", in, got)
			}
		})
	}
}

func TestPatternsSurviveNormalization(t *testing.T) {
	// Arrange: text is NFKD-normalised before matching, which splits ำ.
	for _, p := range []string{currency.String(), safeNumber.String()} {
		// Act
		has := strings.Contains(p, "ำ")

		// Assert
		if has {
			t.Errorf("pattern %q contains ำ, which never matches after NFKD", p)
		}
	}
}

func TestPaymentDigits(t *testing.T) {
	cases := map[string]int{
		"โอนยอด 8,900 บาท (VAT 9,523 บาท) วันที่ 12 ธ.ค. 2569 09:00-17:00 รับ 30 ที่นั่ง": 4,
		"ผ่อน 3 หรือ 6 เดือน":         2,
		"995231 ต่อด้วย 0946":         10,
		"1,234,567-890,123 บาท":       13,
		"โทร 081234О678":              10,
		"ราคา ฿3,900 หรือ 500 บาท 7%": 0,
		"0812:345678":  10,
		"9952569-0946": 11,
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			// Act
			got := PaymentDigits(in)

			// Assert
			if got != want {
				t.Fatalf("PaymentDigits(%q) = %d, want %d", in, got, want)
			}
		})
	}
}
