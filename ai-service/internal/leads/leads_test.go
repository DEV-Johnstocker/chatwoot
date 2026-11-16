package leads

import (
	"strings"
	"testing"
)

func TestAsksPrice(t *testing.T) {
	// Arrange
	cases := map[string]bool{
		"คอร์สนี้ราคาเท่าไหร่คะ":         true,
		"กี่บาทครับ":                     true,
		"มีโปรไหมคะ":                     true,
		"อยากสมัครค่ะ":                   true,
		"ขอเลขบัญชีหน่อย":                true,
		"How much is the course?":        true,
		"Can I pay by card":              true,
		"what are your prices":           true,
		"สวัสดีค่ะ":                      false,
		"เรียนออนไลน์ได้ไหม":             false,
		"Hello, is anyone there?":        false,
		"I ordered nothing, just asking": false,
		"":                               false,
	}
	for text, want := range cases {
		// Act
		got := AsksPrice(text)

		// Assert
		if got != want {
			t.Errorf("AsksPrice(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestGivesPrice(t *testing.T) {
	// Arrange
	cases := map[string]bool{
		"ราคา 3,900 บาทค่ะ":                   true,
		"ราคาพิเศษ ฿2,490":                    true,
		"Only 990 THB until Friday":           true,
		"สมัครได้ที่ https://example.com/buy": true,
		"ติดต่อแอดมินได้เลยค่ะ":               false,
		"เรียน 12 ชั่วโมงค่ะ":                 false,
	}
	for text, want := range cases {
		// Act
		got := GivesPrice(text)

		// Assert
		if got != want {
			t.Errorf("GivesPrice(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestClip(t *testing.T) {
	// Arrange
	long := strings.Repeat("ก", 300)

	// Act
	got, short, bad, nul := Clip(long, 200), Clip("  สมชาย  ", 200), Clip("a\xffb", 10), Clip("Som\x00chai", 10)

	// Assert
	if n := len([]rune(got)); n != 200 {
		t.Errorf("clipped to %d runes, want 200", n)
	}
	if short != "สมชาย" || bad != "ab" || nul != "Somchai" {
		t.Errorf("short=%q bad=%q nul=%q (Postgres text refuses NUL)", short, bad, nul)
	}
}

func TestStageValid(t *testing.T) {
	// Arrange, Act, Assert
	for _, s := range Stages {
		if !s.Valid() {
			t.Errorf("%q not valid", s)
		}
	}
	if Stage("won").Valid() || Stage("").Valid() {
		t.Error("unknown stage reported valid")
	}
}
