package mcp

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/knowledge"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/pending"
)

// approveAll approves every waiting change of account 1, as the owner would.
func approveAll(t *testing.T, s *Server) {
	t.Helper()
	ctx := context.Background()
	list, err := s.Pending.List(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range list {
		if err := s.Approve(ctx, 1, c.ID, c.Version, "owner@example.com"); err != nil {
			t.Fatalf("approve %d: %v", c.ID, err)
		}
	}
}

func TestApproval_ProfileEditWaits(t *testing.T) {
	// Arrange
	e := newAdminEnv()
	before := e.profiles.byAcct[1]

	// Act
	var out map[string]any
	decodeResult(t, e.call(rw, "update_profile", map[string]any{"tone": "Warm and short."}), &out)

	// Assert
	if out["result"] != "waiting for approval" || out["id"] == nil {
		t.Fatalf("out = %v", out)
	}
	if e.profiles.saves != 0 || !reflect.DeepEqual(e.profiles.byAcct[1], before) || len(e.events.events) != 0 {
		t.Fatal("the profile changed before approval")
	}
	if n, _ := e.srv.Pending.Count(context.Background(), 1); n != 1 {
		t.Fatalf("waiting = %d", n)
	}
}

func TestApproval_ApproveProfileAppliesAndRecords(t *testing.T) {
	// Arrange
	e := newAdminEnv()
	e.call(rw, "update_profile", map[string]any{"tone": "Warm and short."})
	e.call(rw, "update_profile", map[string]any{"about": "Auto-cut software"})

	// Act
	approveAll(t, e.srv)

	// Assert
	p := e.profiles.byAcct[1]
	if p.Tone != "Warm and short." || p.About != "Auto-cut software" || p.Rules != "Never promise a price." {
		t.Fatalf("profile = %+v", p)
	}
	if len(e.events.events) != 1 || !strings.Contains(e.events.events[0].Actor, "owner@example.com") ||
		!strings.Contains(e.events.events[0].Actor, "claude") || e.events.events[0].Action != "business profile saved" {
		t.Fatalf("events = %+v", e.events.events)
	}
	if n, _ := e.srv.Pending.Count(context.Background(), 1); n != 0 {
		t.Fatalf("still waiting: %d", n)
	}
}

func TestApproval_ApproveAppliesOnlyProposedFieldsOverNewerOwnerEdits(t *testing.T) {
	// Arrange: the owner edits rules on /admin after the app proposed tone.
	e := newAdminEnv()
	e.call(rw, "update_profile", map[string]any{"tone": "Warm."})
	p := e.profiles.byAcct[1]
	p.Rules = "Owner's newer rule."
	e.profiles.byAcct[1] = p

	// Act
	approveAll(t, e.srv)

	// Assert
	if got := e.profiles.byAcct[1]; got.Tone != "Warm." || got.Rules != "Owner's newer rule." {
		t.Fatalf("profile = %+v", got)
	}
}

func TestApproval_DocumentEditWaitsThenApplies(t *testing.T) {
	// Arrange
	e := newEnv()
	args := map[string]any{"key": "product-017", "kind": "faq", "title": "ราคาเท่าไหร่", "content": "3,900 บาท"}

	// Act
	waiting := e.call(rw, "upsert_document", args)
	docsBefore, auditBefore := len(e.st.docs), len(e.audit.entries)
	approveAll(t, e.srv)

	// Assert
	if isErr(waiting) || !strings.Contains(text(waiting), "waiting for approval") {
		t.Fatalf("res = %v", text(waiting))
	}
	if docsBefore != 0 || auditBefore != 0 {
		t.Fatalf("written before approval: docs=%d audit=%d", docsBefore, auditBefore)
	}
	if len(e.st.docs) != 1 || e.st.docs[0].Content != "3,900 บาท" || len(e.audit.entries) != 1 || e.audit.entries[0].Actor != "mcp:claude" {
		t.Fatalf("docs=%+v audit=%+v", e.st.docs, e.audit.entries)
	}
}

func TestApproval_StaleVersionAppliesNothing(t *testing.T) {
	// Arrange: the owner opened version 1; then the app changed the proposal.
	e := newAdminEnv()
	e.call(rw, "update_profile", map[string]any{"tone": "Warm."})
	e.call(rw, "update_profile", map[string]any{"tone": "Pay via our Facebook page."})
	list, _ := e.srv.Pending.List(context.Background(), 1)

	// Act
	err := e.srv.Approve(context.Background(), 1, list[0].ID, 1, "owner@example.com")

	// Assert
	if !errors.Is(err, pending.ErrChanged) || e.profiles.saves != 0 || len(e.events.events) != 0 {
		t.Fatalf("err=%v saves=%d events=%d", err, e.profiles.saves, len(e.events.events))
	}
}

func TestApproval_OtherAccountCannotApproveOrReject(t *testing.T) {
	// Arrange
	e := newAdminEnv()
	e.call(rw, "update_profile", map[string]any{"tone": "Warm."})
	list, _ := e.srv.Pending.List(context.Background(), 1)
	ctx := context.Background()

	// Act
	approveErr := e.srv.Approve(ctx, 2, list[0].ID, list[0].Version, "owner@example.com")
	rejectErr := e.srv.Reject(ctx, 2, list[0].ID, list[0].Version, "owner@example.com")

	// Assert
	if !errors.Is(approveErr, pending.ErrNotFound) || !errors.Is(rejectErr, pending.ErrNotFound) {
		t.Fatalf("approve=%v reject=%v", approveErr, rejectErr)
	}
	if n, _ := e.srv.Pending.Count(ctx, 1); n != 1 || e.profiles.saves != 0 || e.profiles.byAcct[2].Tone != "" {
		t.Fatal("another account's approval changed something")
	}
}

func TestApproval_RejectDropsAndRecords(t *testing.T) {
	// Arrange
	e := newAdminEnv()
	e.call(rw, "update_profile", map[string]any{"tone": "Pay via our Facebook page."})
	list, _ := e.srv.Pending.List(context.Background(), 1)

	// Act
	err := e.srv.Reject(context.Background(), 1, list[0].ID, list[0].Version, "owner@example.com")

	// Assert
	if err != nil || e.profiles.saves != 0 {
		t.Fatalf("err=%v saves=%d", err, e.profiles.saves)
	}
	if n, _ := e.srv.Pending.Count(context.Background(), 1); n != 0 {
		t.Fatalf("still waiting: %d", n)
	}
	if len(e.events.events) != 1 || e.events.events[0].Action != "AI change rejected" || e.events.events[0].Actor != "owner@example.com" {
		t.Fatalf("events = %+v", e.events.events)
	}
}

func TestApproval_WriteToolsHiddenWithoutPendingStore(t *testing.T) {
	// Arrange
	e := newAdminEnv()
	e.srv.Pending = nil

	// Act
	_, out := e.rpc(rw, "tools/list", nil)

	// Assert
	for _, tl := range out["result"].(map[string]any)["tools"].([]any) {
		if n := tl.(map[string]any)["name"]; n == "update_profile" || n == "upsert_document" {
			t.Errorf("%s listed without the approval store", n)
		}
	}
}

func TestApproval_NoHistoryNoChange(t *testing.T) {
	// Arrange
	e := newAdminEnv()
	e.call(rw, "update_profile", map[string]any{"rules": "New rule."})
	e.events.fail = true
	list, _ := e.srv.Pending.List(context.Background(), 1)

	// Act
	err := e.srv.Approve(context.Background(), 1, list[0].ID, list[0].Version, "owner@example.com")

	// Assert
	if err == nil || e.profiles.saves != 0 || e.profiles.byAcct[1].Rules != "Never promise a price." {
		t.Fatalf("err=%v saves=%d profile=%+v", err, e.profiles.saves, e.profiles.byAcct[1])
	}
	if n, _ := e.srv.Pending.Count(context.Background(), 1); n != 1 {
		t.Fatalf("a failed approval dropped the change: waiting=%d", n)
	}
}

func TestApproval_FullQueueRefusesNewTargets(t *testing.T) {
	// Arrange
	e := newEnv()
	for i := range pending.MaxPerAccount {
		if _, err := e.srv.Pending.Propose(context.Background(), pending.Change{AccountID: 1, Kind: pending.KindDocument,
			Target: "k" + strings.Repeat("x", i), Proposed: []byte(`{}`), Actor: "claude"}); err != nil {
			t.Fatal(err)
		}
	}

	// Act
	res := e.call(rw, "upsert_document", map[string]any{"key": "one-more", "title": "Hours", "content": "Open daily."})

	// Assert
	if !isErr(res) || !strings.Contains(text(res), "wait for the owner") {
		t.Fatalf("res = %v", text(res))
	}
}

func TestApproval_DoubleApproveAppliesOnce(t *testing.T) {
	// Arrange: two tabs approve the same change.
	e := newAdminEnv()
	e.call(rw, "update_profile", map[string]any{"tone": "Warm."})
	list, _ := e.srv.Pending.List(context.Background(), 1)
	ctx := context.Background()

	// Act
	first := e.srv.Approve(ctx, 1, list[0].ID, list[0].Version, "owner@example.com")
	second := e.srv.Approve(ctx, 1, list[0].ID, list[0].Version, "owner@example.com")
	reject := e.srv.Reject(ctx, 1, list[0].ID, list[0].Version, "owner@example.com")

	// Assert
	if first != nil || !errors.Is(second, pending.ErrNotFound) || !errors.Is(reject, pending.ErrNotFound) {
		t.Fatalf("first=%v second=%v reject=%v", first, second, reject)
	}
	if e.profiles.saves != 1 || len(e.events.events) != 1 {
		t.Fatalf("saves=%d events=%d, want one apply", e.profiles.saves, len(e.events.events))
	}
}

func TestApproval_ApplyRechecksText(t *testing.T) {
	// Arrange: a row that did not come through the tool (another code path, SQL).
	e := newEnv()
	id, _ := e.srv.Pending.Propose(context.Background(), pending.Change{AccountID: 1, Kind: pending.KindDocument, Target: "pay",
		Proposed: []byte(`{"key":"pay","title":"Pay","content":"Transfer to 1234567890"}`), Actor: "claude"})

	// Act
	err := e.srv.Approve(context.Background(), 1, id, 1, "owner@example.com")

	// Assert
	if err == nil || len(e.st.docs) != 0 {
		t.Fatalf("err=%v docs=%d", err, len(e.st.docs))
	}
}

func TestApproval_WriteToolCallRefusedWithoutPendingStore(t *testing.T) {
	// Arrange
	e := newEnv()
	e.srv.Pending = nil

	// Act
	res := e.call(rw, "upsert_document", map[string]any{"key": "hours", "title": "Hours", "content": "Open daily."})

	// Assert
	if (res != nil && !isErr(res)) || len(e.st.docs) != 0 {
		t.Fatalf("res=%v docs=%d", res, len(e.st.docs))
	}
}

func TestApproval_ReplyLinksToReviewPage(t *testing.T) {
	// Arrange
	e := newAdminEnv()
	e.srv.AdminURL = "https://support.example.com"

	// Act
	var out map[string]any
	decodeResult(t, e.call(rw, "update_profile", map[string]any{"tone": "Warm and short."}), &out)

	// Assert
	want := "https://support.example.com/admin/approvals?account=1#change-" + jsonNum(out["id"])
	if out["review_url"] != want {
		t.Fatalf("review_url = %v, want %s", out["review_url"], want)
	}
}

func TestApproval_NoLinkWithoutAdminURL(t *testing.T) {
	// Arrange
	e := newAdminEnv()

	// Act
	var out map[string]any
	decodeResult(t, e.call(rw, "update_profile", map[string]any{"tone": "Warm and short."}), &out)

	// Assert
	if _, ok := out["review_url"]; ok {
		t.Fatalf("review_url set without AdminURL: %v", out)
	}
}

func jsonNum(v any) string {
	if f, ok := v.(float64); ok {
		return strconv.FormatInt(int64(f), 10)
	}
	return ""
}

func TestApproval_InstructionsSayEditsWait(t *testing.T) {
	// Arrange
	tok := Token{AccountID: 1, Name: "claude", CanWrite: true}

	// Act
	s := instructions(tok)

	// Assert
	if !strings.Contains(s, "wait for the owner's approval") || strings.Contains(s, "Every change reaches real customers at once") {
		t.Fatalf("instructions do not match the approval flow: %s", s)
	}
}

// --- QA F-C-07: an older proposal must not overwrite a newer owner edit ---

func TestApproval_DocumentEditedSinceProposalIsRefused(t *testing.T) {
	// Arrange: the app proposes a price; then the owner edits the same document on /admin.
	e := newEnv()
	e.st.docs = []knowledge.Document{{ID: 5, AccountID: 1, Kind: knowledge.KindFAQ, Title: "ราคา", Content: "3,900 บาท", SourceKey: "price"}}
	e.call(rw, "upsert_document", map[string]any{"key": "price", "title": "ราคา", "content": "4,900 บาท"})
	e.st.docs[0].Content = "5,900 บาท" // the owner's newer edit
	list, _ := e.srv.Pending.List(context.Background(), 1)

	// Act
	err := e.srv.Approve(context.Background(), 1, list[0].ID, list[0].Version, "owner@example.com")

	// Assert
	if !errors.Is(err, pending.ErrTargetChanged) || !strings.Contains(err.Error(), "changed since this proposal") {
		t.Fatalf("err = %v, want ErrTargetChanged", err)
	}
	if e.st.docs[0].Content != "5,900 บาท" || len(e.audit.entries) != 0 {
		t.Fatalf("the owner's edit was overwritten: docs=%+v audit=%d", e.st.docs, len(e.audit.entries))
	}
	if n, _ := e.srv.Pending.Count(context.Background(), 1); n != 1 {
		t.Fatalf("the refused change should still wait for a reject: waiting=%d", n)
	}
}

func TestApproval_DocumentAddedSinceProposalIsRefused(t *testing.T) {
	// Arrange: the app proposes a new key; the owner adds a document with that key first.
	e := newEnv()
	e.call(rw, "upsert_document", map[string]any{"key": "promo", "title": "โปร", "content": "ลด 10%"})
	e.st.docs = []knowledge.Document{{ID: 9, AccountID: 1, Kind: knowledge.KindText, Title: "โปร", Content: "ลด 20%", SourceKey: "promo"}}
	list, _ := e.srv.Pending.List(context.Background(), 1)

	// Act
	err := e.srv.Approve(context.Background(), 1, list[0].ID, list[0].Version, "owner@example.com")

	// Assert
	if !errors.Is(err, pending.ErrTargetChanged) || e.st.docs[0].Content != "ลด 20%" {
		t.Fatalf("err=%v docs=%+v", err, e.st.docs)
	}
}

func TestApproval_ReproposedAfterOwnerEditApplies(t *testing.T) {
	// Arrange: the app proposes again after the owner's edit, so it saw the newer text.
	e := newEnv()
	e.st.docs = []knowledge.Document{{ID: 5, AccountID: 1, Kind: knowledge.KindFAQ, Title: "ราคา", Content: "3,900 บาท", SourceKey: "price"}}
	e.call(rw, "upsert_document", map[string]any{"key": "price", "title": "ราคา", "content": "4,900 บาท"})
	e.st.docs[0].Content = "5,900 บาท"
	e.call(rw, "upsert_document", map[string]any{"key": "price", "title": "ราคา", "content": "6,900 บาท"})

	// Act
	approveAll(t, e.srv)

	// Assert
	if e.st.docs[0].Content != "6,900 บาท" {
		t.Fatalf("docs = %+v", e.st.docs)
	}
}

func TestApproval_ProposalWithoutBaseStillApplies(t *testing.T) {
	// Arrange: a proposal stored before this check existed has no base.
	e := newEnv()
	e.st.docs = []knowledge.Document{{ID: 5, AccountID: 1, Kind: knowledge.KindFAQ, Title: "ราคา", Content: "3,900 บาท", SourceKey: "price"}}
	_, err := e.srv.Pending.Propose(context.Background(), pending.Change{AccountID: 1, Kind: pending.KindDocument, Target: "price",
		Proposed: []byte(`{"key":"price","title":"ราคา","content":"4,900 บาท"}`), Actor: "claude", TokenID: 1})
	if err != nil {
		t.Fatal(err)
	}

	// Act
	approveAll(t, e.srv)

	// Assert
	if e.st.docs[0].Content != "4,900 บาท" {
		t.Fatalf("docs = %+v", e.st.docs)
	}
}

// --- QA F-D-02: deletes wait for the owner like every other edit ---

func TestApproval_DeleteWaitsThenApplies(t *testing.T) {
	// Arrange
	e := newEnv()
	e.st.docs = []knowledge.Document{{ID: 5, AccountID: 1, Kind: knowledge.KindFAQ, Title: "old promo", Content: "2,900", SourceKey: "promo"}}

	// Act
	waiting := e.call(rw, "delete_document", map[string]any{"key": "promo"})
	docsBefore, auditBefore := len(e.st.docs), len(e.audit.entries)
	approveAll(t, e.srv)

	// Assert
	if isErr(waiting) || !strings.Contains(text(waiting), "waiting for approval") {
		t.Fatalf("res = %v", text(waiting))
	}
	if docsBefore != 1 || auditBefore != 0 {
		t.Fatalf("deleted before approval: docs=%d audit=%d", docsBefore, auditBefore)
	}
	if len(e.st.docs) != 0 || len(e.audit.entries) != 1 || e.audit.entries[0].Action != "delete" ||
		e.audit.entries[0].Before.(map[string]any)["content"] != "2,900" || e.audit.entries[0].Actor != "mcp:claude" {
		t.Fatalf("docs=%+v audit=%+v", e.st.docs, e.audit.entries)
	}
}

func TestApproval_DeleteByIDOfDocumentWithoutKey(t *testing.T) {
	// Arrange: a document added by hand on /admin has no key.
	e := newEnv()
	e.st.docs = []knowledge.Document{{ID: 7, AccountID: 1, Kind: knowledge.KindText, Title: "hand-made", Content: "x"}}

	// Act
	waiting := e.call(rw, "delete_document", map[string]any{"id": 7})
	before := len(e.st.docs)
	approveAll(t, e.srv)

	// Assert
	if isErr(waiting) || before != 1 || len(e.st.docs) != 0 {
		t.Fatalf("res=%v before=%d after=%d", text(waiting), before, len(e.st.docs))
	}
}

func TestApproval_ManyDeletesCannotEmptyTheKnowledgeBase(t *testing.T) {
	// Arrange: a write token tries to delete every document.
	e := newEnv()
	for i := int64(1); i <= 20; i++ {
		e.st.docs = append(e.st.docs, knowledge.Document{ID: i, AccountID: 1, Kind: knowledge.KindText, Title: "t", Content: "c",
			SourceKey: "doc-" + strconv.FormatInt(i, 10)})
	}

	// Act
	for i := 1; i <= 20; i++ {
		e.call(rw, "delete_document", map[string]any{"key": "doc-" + strconv.Itoa(i)})
	}

	// Assert
	if len(e.st.docs) != 20 || len(e.audit.entries) != 0 {
		t.Fatalf("docs=%d audit=%d, want nothing deleted before approval", len(e.st.docs), len(e.audit.entries))
	}
	if n, _ := e.srv.Pending.Count(context.Background(), 1); n != 20 {
		t.Fatalf("waiting = %d", n)
	}
}

func TestApproval_DeleteOfEditedDocumentIsRefused(t *testing.T) {
	// Arrange: the app asks to delete; then the owner rewrites the document.
	e := newEnv()
	e.st.docs = []knowledge.Document{{ID: 5, AccountID: 1, Kind: knowledge.KindFAQ, Title: "promo", Content: "2,900", SourceKey: "promo"}}
	e.call(rw, "delete_document", map[string]any{"key": "promo"})
	e.st.docs[0].Content = "new owner text"
	list, _ := e.srv.Pending.List(context.Background(), 1)

	// Act
	err := e.srv.Approve(context.Background(), 1, list[0].ID, list[0].Version, "owner@example.com")

	// Assert
	if !errors.Is(err, pending.ErrTargetChanged) || len(e.st.docs) != 1 || len(e.audit.entries) != 0 {
		t.Fatalf("err=%v docs=%d audit=%d", err, len(e.st.docs), len(e.audit.entries))
	}
}

func TestApproval_RejectedDeleteKeepsDocument(t *testing.T) {
	// Arrange
	e := newEnv()
	e.st.docs = []knowledge.Document{{ID: 5, AccountID: 1, Kind: knowledge.KindFAQ, Title: "promo", Content: "2,900", SourceKey: "promo"}}
	e.call(rw, "delete_document", map[string]any{"key": "promo"})
	list, _ := e.srv.Pending.List(context.Background(), 1)

	// Act
	err := e.srv.Reject(context.Background(), 1, list[0].ID, list[0].Version, "owner@example.com")

	// Assert
	if err != nil || len(e.st.docs) != 1 || len(e.audit.entries) != 0 {
		t.Fatalf("err=%v docs=%d audit=%d", err, len(e.st.docs), len(e.audit.entries))
	}
}
