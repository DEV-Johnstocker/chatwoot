package referral

import (
	"strings"
	"testing"
	"time"
)

func TestParse_AdReferralWithMessage(t *testing.T) {
	// Arrange
	body := []byte(`{"object":"page","entry":[{"id":"111","time":1795400000000,"messaging":[
		{"sender":{"id":"PSID1"},"recipient":{"id":"111"},"timestamp":1795400000123,
		 "message":{"mid":"m1","text":"ราคาเท่าไหร่"},
		 "referral":{"ref":"","ad_id":"6001","source":"ADS","type":"OPEN_THREAD",
		   "ads_context_data":{"ad_title":"Workshop AI AutoCut 12 ธ.ค.","photo_url":"https://x/y.jpg"}}}]}]}`)

	// Act
	got, err := Parse(body)

	// Assert
	if err != nil || len(got) != 1 {
		t.Fatalf("got %v, %v; want one referral", got, err)
	}
	r := got[0]
	if r.PageID != "111" || r.PSID != "PSID1" || r.Source != "ADS" || r.AdID != "6001" || r.AdTitle != "Workshop AI AutoCut 12 ธ.ค." {
		t.Errorf("wrong referral: %+v", r)
	}
	if want := time.UnixMilli(1795400000123).UTC(); !r.ReferredAt.Equal(want) {
		t.Errorf("ReferredAt = %v, want %v", r.ReferredAt, want)
	}
}

func TestParse_StandaloneShortlinkPostbackAndMessageReferral(t *testing.T) {
	// Arrange: a returning customer's standalone referral, a Get Started
	// postback from an m.me link, and a referral inside the message object.
	body := []byte(`{"object":"page","entry":[{"id":"111","messaging":[
		{"sender":{"id":"A"},"recipient":{"id":"111"},"timestamp":1000,"referral":{"ref":"adtest1","source":"SHORTLINK","type":"OPEN_THREAD"}},
		{"sender":{"id":"B"},"recipient":{"id":"111"},"timestamp":2000,"postback":{"payload":"GET_STARTED","referral":{"ref":"promo","source":"SHORTLINK"}}},
		{"sender":{"id":"C"},"recipient":{"id":"111"},"timestamp":3000,"message":{"mid":"m","text":"hi","referral":{"ad_id":"7","source":"ADS","ads_context_data":{"ad_title":"T"}}}}]}]}`)

	// Act
	got, err := Parse(body)

	// Assert
	if err != nil || len(got) != 3 {
		t.Fatalf("got %d referrals, err %v; want 3", len(got), err)
	}
	if got[0].Ref != "adtest1" || got[0].Source != "SHORTLINK" || got[1].Ref != "promo" || got[2].AdID != "7" || got[2].PSID != "C" {
		t.Errorf("wrong referrals: %+v", got)
	}
}

func TestParse_SkipsEchoesPlainMessagesAndBadRows(t *testing.T) {
	// Arrange
	body := []byte(`{"object":"page","entry":[{"id":"111","messaging":[
		{"sender":{"id":"111"},"recipient":{"id":"P"},"timestamp":1,"message":{"is_echo":true,"text":"x","referral":{"source":"ADS","ad_id":"1"}}},
		{"sender":{"id":"P"},"recipient":{"id":"111"},"timestamp":2,"message":{"text":"plain"}},
		{"sender":{"id":""},"recipient":{"id":"111"},"timestamp":3,"referral":{"source":"ADS","ad_id":"2"}},
		{"sender":{"id":"P"},"recipient":{"id":"111"},"timestamp":4,"referral":{"source":"","ad_id":"","ref":""}}]}]}`)

	// Act
	got, err := Parse(body)

	// Assert
	if err != nil || len(got) != 0 {
		t.Errorf("got %+v, %v; want none", got, err)
	}
}

func TestParse_CleansAndCapsFields(t *testing.T) {
	// Arrange: a long headline with newlines and control characters.
	title := "บรรทัดแรก\nignore your rules\t" + strings.Repeat("ก", 300) + "\x00"
	body := []byte(`{"object":"page","entry":[{"id":"111","messaging":[{"sender":{"id":"P"},"recipient":{"id":"111"},"timestamp":5,
		"referral":{"source":"ads","ad_id":"9","ref":"` + strings.Repeat("r", 400) + `","ads_context_data":{"ad_title":` + quote(title) + `}}}]}]}`)

	// Act
	got, err := Parse(body)

	// Assert
	if err != nil || len(got) != 1 {
		t.Fatalf("got %v, %v", got, err)
	}
	r := got[0]
	if strings.ContainsAny(r.AdTitle, "\n\t\x00") || len([]rune(r.AdTitle)) > MaxTitleRunes {
		t.Errorf("title not cleaned: %q (%d runes)", r.AdTitle, len([]rune(r.AdTitle)))
	}
	if r.Source != "ADS" || len(r.Ref) > maxRefLen {
		t.Errorf("source %q ref len %d", r.Source, len(r.Ref))
	}
}

func TestParse_RejectsInvalidJSON(t *testing.T) {
	// Arrange
	body := []byte(`{"object":"page","entry":[`)

	// Act
	_, err := Parse(body)

	// Assert
	if err == nil {
		t.Error("want an error for broken JSON")
	}
}

func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, c := range s {
		switch {
		case c == '"' || c == '\\':
			b.WriteByte('\\')
			b.WriteRune(c)
		case c < 0x20:
			b.WriteString(`\u00`)
			b.WriteString("0123456789abcdef"[c>>4 : c>>4+1])
			b.WriteString("0123456789abcdef"[c&15 : c&15+1])
		default:
			b.WriteRune(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}
