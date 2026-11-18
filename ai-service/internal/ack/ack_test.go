package ack

import "testing"

// Issue: DEV-Johnstocker/chatwoot#1
func TestIsAck_NumbersAndQuestionMarksSaySomething(t *testing.T) {
	for s, want := range map[string]bool{
		"555": true, "55555+": true, "ok thanks": true, "โอเค ขอบคุณครับ": true, StickerNote: true,
		"??": false, "2": false, "3900?": false, "ขอบคุณ ราคา": false, "ok how much": false,
	} {
		if got := IsAck(s); got != want {
			t.Errorf("IsAck(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestTrimPoliteEnding(t *testing.T) {
	if got := TrimPoliteEnding("แนวไหนอยู่ครับ"); got != "แนวไหนอยู่" {
		t.Errorf("got %q", got)
	}
}
