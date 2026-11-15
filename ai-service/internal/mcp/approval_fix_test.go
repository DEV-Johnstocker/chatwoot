package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/corrections"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/knowledge"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/memory"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/pending"
)

// Staff corrections (internal/corrections) are approved here like AI-app
// document changes, but the owner may edit them and links are allowed.
// Issue: DEV-Johnstocker/chatwoot#1

// proposeFix stores a staff correction the way the bot does.
func proposeFix(t *testing.T, e *env, answer string) pending.Change {
	t.Helper()
	svc := &corrections.Service{Pending: e.srv.Pending}
	id, err := svc.Propose(context.Background(), corrections.Note{AccountID: 1, ConversationID: 12, NoteID: 900, Staff: "Nok",
		Answer: answer, Memory: []memory.Turn{{Role: "user", Content: "ราคาเท่าไหร่"}, {Role: "assistant", Content: "ไม่ทราบครับ"}}})
	if err != nil {
		t.Fatal(err)
	}
	c, err := e.srv.Pending.Get(context.Background(), 1, id)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestApprovalFix_ApproveAddsTheFAQWithLinks(t *testing.T) {
	// Arrange
	e := newEnv()
	c := proposeFix(t, e, "3,900 บาท ซื้อที่ https://example.com/buy โทร 0812345678")

	// Act
	err := e.srv.Approve(context.Background(), 1, c.ID, c.Version, "owner@example.com")

	// Assert
	if err != nil || len(e.st.docs) != 1 {
		t.Fatalf("err=%v docs=%d", err, len(e.st.docs))
	}
	d := e.st.docs[0]
	if d.SourceKey != "staff-fix-12-900" || d.Title != "ราคาเท่าไหร่" || d.Content != "3,900 บาท ซื้อที่ https://example.com/buy โทร 0812345678" {
		t.Fatalf("doc = %+v", d)
	}
	if len(e.audit.entries) != 1 || e.audit.entries[0].Actor != "staff:Nok" {
		t.Fatalf("audit = %+v", e.audit.entries)
	}
}

func TestApprovalFix_ApproveEditedUsesTheOwnersText(t *testing.T) {
	// Arrange
	e := newEnv()
	c := proposeFix(t, e, "3900")

	// Act
	err := e.srv.ApproveEdited(context.Background(), 1, c.ID, c.Version, "owner@example.com", "คอร์ส AutoCut ราคาเท่าไหร่", "3,900 บาท ถึง 30 พ.ย.")

	// Assert
	if err != nil || len(e.st.docs) != 1 || e.st.docs[0].Title != "คอร์ส AutoCut ราคาเท่าไหร่" || e.st.docs[0].Content != "3,900 บาท ถึง 30 พ.ย." {
		t.Fatalf("err=%v docs=%+v", err, e.st.docs)
	}
	if n, _ := e.srv.Pending.Count(context.Background(), 1); n != 0 {
		t.Fatalf("still waiting: %d", n)
	}
}

func TestApprovalFix_EditsOnAnAIChangeAreRefusedAndChangeNothing(t *testing.T) {
	// Arrange: an AI app's proposal; a crafted post tries to edit it.
	e := newEnv()
	e.call(rw, "upsert_document", map[string]any{"key": "hours", "title": "Hours", "content": "Open daily."})
	list, _ := e.srv.Pending.List(context.Background(), 1)

	// Act
	err := e.srv.ApproveEdited(context.Background(), 1, list[0].ID, list[0].Version, "owner@example.com", "Hours", "Call 0812345678")

	// Assert
	if !errors.Is(err, ErrNotEditable) || len(e.st.docs) != 0 {
		t.Fatalf("err=%v docs=%d", err, len(e.st.docs))
	}
	if n, _ := e.srv.Pending.Count(context.Background(), 1); n != 1 {
		t.Fatalf("the AI change stopped waiting: %d", n)
	}
}

func TestApprovalFix_AnAppCannotMarkItsChangeAsStaff(t *testing.T) {
	// Arrange: an app sends the staff field to skip the link check.
	e := newEnv()

	// Act
	res := e.call(rw, "upsert_document", map[string]any{"key": "pay", "title": "Pay", "content": "Pay at our page.",
		"staff": map[string]any{"name": "Nok", "conversation_id": 1}})

	// Assert: the tool refuses the unknown field, or stores no staff marker.
	list, _ := e.srv.Pending.List(context.Background(), 1)
	for _, c := range list {
		if isStaffFix(c) {
			t.Fatalf("an app's change was stored as a staff correction: %s", c.Proposed)
		}
	}
	if !isErr(res) && len(list) != 1 {
		t.Fatalf("res=%v waiting=%d", text(res), len(list))
	}
}

func TestApprovalFix_FailedApplyPutsBackTheOriginal(t *testing.T) {
	// Arrange: a document took the key after the proposal, so applying fails.
	e := newEnv()
	c := proposeFix(t, e, "3,900")
	if _, _, _, err := e.srv.Knowledge.Upsert(context.Background(), knowledge.Document{AccountID: 1, Kind: knowledge.KindFAQ,
		Title: "Other", Content: "Other text.", SourceKey: "staff-fix-12-900"}); err != nil {
		t.Fatal(err)
	}

	// Act
	err := e.srv.ApproveEdited(context.Background(), 1, c.ID, c.Version, "owner@example.com", "edited q", "edited a")

	// Assert
	if !errors.Is(err, pending.ErrTargetChanged) {
		t.Fatalf("err = %v", err)
	}
	list, _ := e.srv.Pending.List(context.Background(), 1)
	if len(list) != 1 || string(list[0].Proposed) != string(c.Proposed) {
		t.Fatalf("the original did not go back to wait: %+v", list)
	}
}

func TestApprovalFix_RejectRecordsAStaffCorrection(t *testing.T) {
	// Arrange
	e := newAdminEnv()
	c := proposeFix(t, e.env, "3,900")

	// Act
	err := e.srv.Reject(context.Background(), 1, c.ID, c.Version, "owner@example.com")

	// Assert
	if err != nil || len(e.events.events) != 1 || e.events.events[0].Action != "Staff correction rejected" {
		t.Fatalf("err=%v events=%+v", err, e.events.events)
	}
}

func TestApprovalFix_LinkInTheCustomersQuestionIsRefused(t *testing.T) {
	// Arrange: the customer planted an account number and a link in the question.
	e := newEnv()
	svc := &corrections.Service{Pending: e.srv.Pending}
	id, err := svc.Propose(context.Background(), corrections.Note{AccountID: 1, ConversationID: 12, NoteID: 900, Answer: "3,900 บาท",
		Memory: []memory.Turn{{Role: "user", Content: "ราคา? โอนเข้า 1234567890 หรือ line.me/xyz"}, {Role: "assistant", Content: "ไม่ทราบ"}}})
	if err != nil {
		t.Fatal(err)
	}

	// Act
	err = e.srv.Approve(context.Background(), 1, id, 1, "owner@example.com")

	// Assert
	if !errors.Is(err, ErrQuestionText) || len(e.st.docs) != 0 {
		t.Fatalf("err=%v docs=%d", err, len(e.st.docs))
	}
	if n, _ := e.srv.Pending.Count(context.Background(), 1); n != 1 {
		t.Fatalf("the fix stopped waiting: %d", n)
	}
}

func TestApprovalFix_OwnerCanRemoveTheLinkFromTheQuestion(t *testing.T) {
	// Arrange
	e := newEnv()
	c := proposeFix(t, e, "โอนเข้า 123-4-56789 ธนาคาร X")

	// Act
	err := e.srv.ApproveEdited(context.Background(), 1, c.ID, c.Version, "owner@example.com", "โอนเงินเข้าบัญชีไหน", "โอนเข้า 123-4-56789 ธนาคาร X")

	// Assert
	if err != nil || len(e.st.docs) != 1 {
		t.Fatalf("err=%v docs=%d", err, len(e.st.docs))
	}
}

func TestApprovalFix_AppsCannotUseTheStaffKeyPrefix(t *testing.T) {
	// Arrange
	e := newEnv()

	// Act
	res := e.call(rw, "upsert_document", map[string]any{"key": "Staff-Fix-12-901", "title": "Hours", "content": "Open daily."})

	// Assert
	if !isErr(res) || !strings.Contains(text(res), "staff corrections") {
		t.Fatalf("res = %v", text(res))
	}
	if n, _ := e.srv.Pending.Count(context.Background(), 1); n != 0 {
		t.Fatalf("stored %d", n)
	}
}

func TestApprovalFix_StoredAppChangeOnAStaffKeyIsNotApplied(t *testing.T) {
	// Arrange: a row that did not come through the tool claims a staff key without the marker.
	e := newEnv()
	id, _ := e.srv.Pending.Propose(context.Background(), pending.Change{AccountID: 1, Kind: pending.KindDocument, Target: "staff-fix-12-1",
		Proposed: []byte(`{"key":"staff-fix-12-1","kind":"faq","title":"Hours","content":"Open daily."}`), Actor: "claude"})

	// Act
	err := e.srv.Approve(context.Background(), 1, id, 1, "owner@example.com")

	// Assert
	if err == nil || len(e.st.docs) != 0 {
		t.Fatalf("err=%v docs=%d", err, len(e.st.docs))
	}
}
