package admin

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/mcp"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/pending"
)

// Staff corrections on the Approvals page: shown with the bot's reply and
// editable question and answer. Issue: DEV-Johnstocker/chatwoot#1

func staffFix(question, answer string) map[string]any {
	return map[string]any{"key": "staff-fix-12-900", "kind": "faq", "title": question, "content": answer, "base": "",
		"staff": pending.StaffFix{Name: "Nok", ConversationID: 12, NoteID: 900, BotReply: "ไม่ทราบครับ <script>"}}
}

func TestApprovals_StaffFixShowsEditableQuestionAndAnswer(t *testing.T) {
	// Arrange
	e := newApprovalsEnv(t, true)
	e.propose(t, 2, pending.KindDocument, "staff-fix-12-900", staffFix("ราคาคอร์สเท่าไหร่", "3,900 บาท ถึง 30 พ.ย."))

	// Act
	body := serve(e.mux, approvalsGet("account=2")).Body.String()

	// Assert
	for _, want := range []string{"Staff correction from chat #12", "<strong>Nok</strong> (staff)", "New FAQ", "(staff)", "The bot replied",
		"ไม่ทราบครับ &lt;script&gt;", `name="question"`, ">ราคาคอร์สเท่าไหร่</textarea>", `name="answer"`, ">3,900 บาท ถึง 30 พ.ย.</textarea>"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(body, "<script>") {
		t.Error("bot reply not escaped")
	}
	if strings.Contains(body, "(AI app)") {
		t.Error("staff correction labelled as an AI app")
	}
}

func TestApprovals_StaffFixApprovesTheOwnersEdits(t *testing.T) {
	// Arrange
	e := newApprovalsEnv(t, true)
	c := e.propose(t, 2, pending.KindDocument, "staff-fix-12-900", staffFix("ราคา", "3,900"))

	// Act
	res := serve(e.mux, approvalsPost(url.Values{"account": {"2"}, "id": {"1"}, "version": {"1"}, "decision": {"approve"},
		"question": {"  ราคาคอร์ส AutoCut เท่าไหร่ "}, "answer": {" 3,900 บาท https://example.com/buy "}}))

	// Assert
	want := approveCall{op: "approve-edited", acct: 2, id: c.ID, version: c.Version, approver: "admin password",
		title: "ราคาคอร์ส AutoCut เท่าไหร่", content: "3,900 บาท https://example.com/buy"}
	if res.Code != http.StatusSeeOther || len(e.approver.calls) != 1 || e.approver.calls[0] != want {
		t.Fatalf("status %d calls %+v", res.Code, e.approver.calls)
	}
}

func TestApprovals_StaffFixRejectIgnoresTheFields(t *testing.T) {
	// Arrange
	e := newApprovalsEnv(t, true)
	e.propose(t, 2, pending.KindDocument, "staff-fix-12-900", staffFix("ราคา", "3,900"))

	// Act: the reject button skips the browser's required check.
	res := serve(e.mux, approvalsPost(url.Values{"account": {"2"}, "id": {"1"}, "version": {"1"}, "decision": {"reject"},
		"question": {""}, "answer": {""}}))

	// Assert
	if res.Code != http.StatusSeeOther || len(e.approver.calls) != 1 || e.approver.calls[0].op != "reject" {
		t.Fatalf("status %d calls %+v", res.Code, e.approver.calls)
	}
}

func TestApprovals_StaffFixBadEditsChangeNothing(t *testing.T) {
	cases := map[string][2]string{
		"empty question":    {" ", "3,900"},
		"empty answer":      {"ราคา", ""},
		"question too long": {strings.Repeat("ก", 301), "3,900"},
		"answer too long":   {"ราคา", strings.Repeat("ก", 4001)},
	}
	for name, qa := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			e := newApprovalsEnv(t, true)
			e.propose(t, 2, pending.KindDocument, "staff-fix-12-900", staffFix("ราคา", "3,900"))

			// Act
			res := serve(e.mux, approvalsPost(url.Values{"account": {"2"}, "id": {"1"}, "version": {"1"}, "decision": {"approve"},
				"question": {qa[0]}, "answer": {qa[1]}}))

			// Assert
			if res.Code != http.StatusUnprocessableEntity || len(e.approver.calls) != 0 || !strings.Contains(res.Body.String(), "Not applied") {
				t.Fatalf("status %d calls %+v", res.Code, e.approver.calls)
			}
		})
	}
}

func TestApprovals_EditsOnAnAIChangeAreRefused(t *testing.T) {
	// Arrange: a crafted post sends edits for an AI app's change.
	e := newApprovalsEnv(t, true)
	e.propose(t, 2, pending.KindDocument, "hours", map[string]any{"key": "hours", "title": "Hours", "content": "9 to 5"})
	e.approver.err = mcp.ErrNotEditable

	// Act
	res := serve(e.mux, approvalsPost(url.Values{"account": {"2"}, "id": {"1"}, "version": {"1"}, "decision": {"approve"},
		"question": {"Hours"}, "answer": {"call 0812345678"}}))

	// Assert
	if res.Code != http.StatusUnprocessableEntity || !strings.Contains(res.Body.String(), "only a staff correction can be edited") {
		t.Fatalf("status %d body %s", res.Code, res.Body.String())
	}
}

func TestApprovals_StaffFixQuestionWithLinkExplainsWhy(t *testing.T) {
	// Arrange
	e := newApprovalsEnv(t, true)
	e.propose(t, 2, pending.KindDocument, "staff-fix-12-900", staffFix("โอน line.me/x", "3,900"))
	e.approver.err = mcp.ErrQuestionText

	// Act
	res := serve(e.mux, approvalsPost(url.Values{"account": {"2"}, "id": {"1"}, "version": {"1"}, "decision": {"approve"},
		"question": {"โอน line.me/x"}, "answer": {"3,900"}}))

	// Assert
	if res.Code != http.StatusUnprocessableEntity || !strings.Contains(res.Body.String(), "Remove it from the question") {
		t.Fatalf("status %d", res.Code)
	}
}

func TestApprovals_StaffFixEditsAreCleaned(t *testing.T) {
	// Arrange
	e := newApprovalsEnv(t, true)
	e.propose(t, 2, pending.KindDocument, "staff-fix-12-900", staffFix("ราคา", "3,900"))

	// Act
	serve(e.mux, approvalsPost(url.Values{"account": {"2"}, "id": {"1"}, "version": {"1"}, "decision": {"approve"},
		"question": {"ราคา\x00"}, "answer": {"3,900\xff"}}))

	// Assert
	if len(e.approver.calls) != 1 || e.approver.calls[0].title != "ราคา" || e.approver.calls[0].content != "3,900�" {
		t.Fatalf("calls = %+v", e.approver.calls)
	}
}
