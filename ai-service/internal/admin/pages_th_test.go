package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPagesInThai_TestBusinessProviders(t *testing.T) {
	cases := map[string]struct{ want, english []string }{
		"/admin/test":      {[]string{`<html lang="th"`, "ทดสอบบอต · Pronto", "ปิดแชททดสอบอยู่"}, []string{"Test chat is off", "Test bot · Pronto"}},
		"/admin/business":  {[]string{`<html lang="th"`, "โปรไฟล์ธุรกิจ · Pronto"}, []string{"Business profile · Pronto"}},
		"/admin/providers": {[]string{`<html lang="th"`, "ผู้ให้บริการหลัก", "ทดสอบการเชื่อมต่อ", "บันทึกการตั้งค่า"}, []string{"Primary provider", "Test connection", "Save settings"}},
	}
	for path, c := range cases {
		// Arrange
		e := setup(t, nil)
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.SetBasicAuth(Username, pw)
		r.Header.Set("Accept-Language", "th")

		// Act
		body := e.do(r).Body.String()

		// Assert
		for _, w := range c.want {
			if !strings.Contains(body, w) {
				t.Errorf("%s lacks %q", path, w)
			}
		}
		for _, en := range c.english {
			if strings.Contains(body, en) {
				t.Errorf("%s still says %q", path, en)
			}
		}
	}
}

func TestTestBot_FormErrorInThai(t *testing.T) {
	// Arrange
	msg := formError{"Attach up to %d files per message.", []any{4}}

	// Act
	en, th := msg.Error(), ownerMsg(msg).in(langTH)

	// Assert
	if en != "Attach up to 4 files per message." || th != "แนบได้สูงสุด 4 ไฟล์ต่อข้อความ" {
		t.Fatalf("en %q th %q", en, th)
	}
}
