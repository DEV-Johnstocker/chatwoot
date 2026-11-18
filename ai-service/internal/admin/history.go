package admin

// The History page (/admin/history): what changed in the business profile,
// the knowledge base and the bot pictures, before and after, with a Restore
// button for profile versions, edited or deleted answers and removed
// pictures (QA F-C-03). Restores go through the same checks and the same
// change log as normal edits, so a restore can itself be undone. The page
// reads the existing logs (admin_events, kb_audit) and the removed pictures
// (schema v21). Issue: DEV-Johnstocker/chatwoot#1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/audit"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/images"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/knowledge"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/profile"
)

// historyLimit is how many changes the page reads and shows.
const historyLimit = 200

// maxHistoryForm bounds a restore or download post (a few small fields).
const maxHistoryForm = 8 << 10

// HistoryLog reads the change log with the copies kept before each change
// (audit.Postgres). The Events store provides it when a database is set.
type HistoryLog interface {
	History(ctx context.Context, accountID int64, limit int) ([]audit.Entry, error)
	Entry(ctx context.Context, accountID int64, source string, id int64) (audit.Entry, error)
}

type historyData struct {
	AccountID      int64
	Enabled        bool // a change log is available
	Entries        []historyEntry
	Removed        []removedView
	ShowRemoved    bool // pictures are on (a database is set)
	Full           bool // the limit was reached; older changes are not shown
	Limit          int
	ProfileVersion string // the current profile, for the stale-page check on restore
	CanExport      bool
	Notice, Error  string
}

type historyEntry struct {
	When, Who, What, Summary string
	Changes                  []historyChange
	Unchanged                string // fields the change left as they were
	Gone                     bool   // a delete: Changes hold the text as it was
	Note                     string // why there is no Restore, or what it does
	Restore                  *restoreButton
}

// historyChange is one field before and after a change. A side may be
// unknown (nil lines and Known false), e.g. after a later change that was
// not kept.
type historyChange struct {
	Label         string
	Before, After []diffLine
	AfterKnown    bool
}

// diffLine is one line of a field; Changed marks lines the other side lacks.
type diffLine struct {
	Text    string
	Changed bool
}

type restoreButton struct {
	Action, Source string
	ID             int64
	Label          string
	Version        string // the document as the page saw it ("0": none), for the stale-page check
}

type removedView struct {
	ID                  int64
	Name, When, Caption string
	Removed, Size       string
}

// historyLog returns the change log reader, or nil without a database.
func (h *Handler) historyLog() HistoryLog {
	if l, ok := h.Events.(HistoryLog); ok {
		return l
	}
	return nil
}

func (h *Handler) showHistory(w http.ResponseWriter, r *http.Request) {
	acct := accountParam(r.URL.Query().Get("account"))
	data := h.loadHistory(r, acct)
	switch r.URL.Query().Get("done") {
	case "restored":
		data.Notice = tr(langOf(r), "Restored. The restore is now the newest change below, so it can be undone the same way.")
	case "image_restored":
		data.Notice = tr(langOf(r), "Picture put back. The bot can send it again.")
	case "image_forgotten":
		data.Notice = tr(langOf(r), "Picture deleted for good.")
	}
	h.renderPage(w, r, http.StatusOK, "history.html", data)
}

// loadHistory fills the page; errors only show a message.
func (h *Handler) loadHistory(r *http.Request, acct int64) historyData {
	ctx, lang := r.Context(), langOf(r)
	data := historyData{AccountID: acct, Limit: historyLimit, ShowRemoved: h.Images != nil,
		CanExport: h.Profiles != nil || h.Knowledge != nil}
	log := h.historyLog()
	data.Enabled = log != nil
	cur, curOK := profile.Profile{AccountID: acct}, false
	if h.Profiles != nil {
		p, err := h.Profiles.Get(ctx, acct)
		if err == nil {
			cur, curOK = p, true
		} else {
			h.Logger.Warn("history: load profile", "account_id", acct, "error", err)
		}
	}
	data.ProfileVersion = profileVersion(cur)
	docs := map[int64]knowledge.Document{}
	docsOK := h.Knowledge != nil
	if docsOK {
		list, err := h.Knowledge.List(ctx, acct)
		if err != nil {
			h.Logger.Warn("history: load knowledge", "account_id", acct, "error", err)
			docsOK = false
		}
		for _, d := range list {
			docs[d.ID] = d
		}
	}
	if log != nil {
		entries, err := log.History(ctx, acct, historyLimit)
		if err != nil {
			h.Logger.Error("history: load changes", "account_id", acct, "error", err)
			data.Error = tr(lang, "Could not load the change history. Check the database connection.")
		}
		data.Full = len(entries) >= historyLimit
		hb := historyBuilder{h: h, lang: lang, cur: cur, curOK: curOK, docs: docs, docsOK: docsOK, next: map[int64]docState{}}
		for _, e := range entries {
			data.Entries = append(data.Entries, hb.entry(e))
		}
	}
	if h.Images != nil {
		removed, err := h.Images.Removed(ctx, acct)
		if err != nil {
			h.Logger.Warn("history: load removed pictures", "account_id", acct, "error", err)
		}
		for _, m := range removed {
			data.Removed = append(data.Removed, removedView{ID: m.ID, Name: m.Name, When: m.When, Caption: m.Caption,
				Removed: h.whenIn(lang, m.RemovedAt), Size: strconv.Itoa((m.Size+1023)/1024) + " KB"})
		}
	}
	return data
}

// docState is what a document looked like after a change, walking the log
// from newest to oldest: the next newer change's "before", or the document
// as it is now.
type docState struct {
	doc    knowledge.Document
	exists bool
}

// historyBuilder turns log entries, newest first, into page entries. It
// remembers the next newer version of the profile and of each document, so
// every change can show its "after".
type historyBuilder struct {
	h      *Handler
	lang   string
	cur    profile.Profile
	curOK  bool
	docs   map[int64]knowledge.Document
	docsOK bool

	nextProfile   *profile.Profile
	profileWalked bool
	next          map[int64]docState
}

func (b *historyBuilder) entry(e audit.Entry) historyEntry {
	lang := b.lang
	v := historyEntry{When: b.h.whenIn(lang, e.At), Who: e.Actor, What: tr(lang, e.Action), Summary: e.Summary}
	switch e.Action {
	case "business profile saved", "business profile restored":
		b.profileEntry(e, &v)
	case "knowledge edited", "knowledge deleted", "knowledge restored", "web page removed":
		b.docEntry(e, &v)
	case "knowledge added":
		if e.Source == audit.SourceAI && e.DocumentID > 0 {
			b.next[e.DocumentID] = docState{} // it did not exist before
		}
	}
	return v
}

// afterProfile is the profile right after the change being walked.
func (b *historyBuilder) afterProfile() (profile.Profile, bool) {
	if b.profileWalked {
		return *b.nextProfile, true
	}
	return b.cur, b.curOK
}

func (b *historyBuilder) profileEntry(e audit.Entry, v *historyEntry) {
	before, ok := profileSnapshot(e.Before)
	if !ok {
		return
	}
	after, afterOK := b.afterProfile()
	b.nextProfile, b.profileWalked = &before, true
	var same []string
	for _, f := range profileFields(b.lang) {
		old, cur := f.get(before), f.get(after)
		if afterOK && old == cur {
			same = append(same, f.label)
			continue
		}
		v.Changes = append(v.Changes, change(f.label, old, cur, afterOK))
	}
	v.Unchanged = strings.Join(same, ", ")
	if before.Empty() {
		// Putting back an empty profile would only wipe the current one.
		v.Note = tr(b.lang, "Before this change the profile was empty.")
		return
	}
	if b.curOK && sameProfile(before, b.cur) {
		v.Note = tr(b.lang, "The profile is like this version now.")
		return
	}
	v.Restore = &restoreButton{Action: "restore_profile", Source: e.Source, ID: e.ID,
		Label: tr(b.lang, "Restore the version before this change")}
}

func (b *historyBuilder) docEntry(e audit.Entry, v *historyEntry) {
	before, ok := docSnapshot(e.Before)
	if !ok {
		return
	}
	if before.ID == 0 {
		before.ID = e.DocumentID
	}
	after, afterOK := b.next[before.ID], true
	if _, walked := b.next[before.ID]; !walked {
		d, exists := b.docs[before.ID]
		after, afterOK = docState{doc: d, exists: exists}, b.docsOK
	}
	b.next[before.ID] = docState{doc: before, exists: true}
	lang := b.lang
	if e.Action == "knowledge deleted" || e.Action == "web page removed" {
		v.Gone = true
		after, afterOK = docState{}, true
	}
	var same []string
	for _, f := range docFields(lang, b.h) {
		old := f.get(before)
		cur := ""
		if after.exists {
			cur = f.get(after.doc)
		}
		if !v.Gone && afterOK && after.exists && old == cur {
			same = append(same, f.label)
			continue
		}
		v.Changes = append(v.Changes, change(f.label, old, cur, afterOK && after.exists))
	}
	v.Unchanged = strings.Join(same, ", ")
	now, nowExists := b.docs[before.ID]
	version := "0"
	if nowExists {
		version = docVersion(now)
	}
	switch {
	case knowledge.SyncedKey(before.SourceKey):
		v.Note = tr(lang, "This came from a website or data file. Add the page or file again on the Knowledge page to bring it back.")
	case nowExists && sameDoc(before, now):
		v.Note = tr(lang, "The knowledge base has this text now.")
	case v.Gone:
		v.Restore = &restoreButton{Action: "restore_doc", Source: e.Source, ID: e.ID, Version: version, Label: tr(lang, "Restore this answer")}
	default:
		v.Restore = &restoreButton{Action: "restore_doc", Source: e.Source, ID: e.ID, Version: version, Label: tr(lang, "Restore the text before this change")}
	}
}

// change builds one field's before/after with changed lines marked.
func change(label, before, after string, afterKnown bool) historyChange {
	c := historyChange{Label: label, AfterKnown: afterKnown}
	if !afterKnown {
		c.Before = plainLines(before)
		return c
	}
	c.Before, c.After = lineDiff(before, after)
	return c
}

// maxDiffCells bounds the line comparison (lines before × lines after).
const maxDiffCells = 1 << 20

// lineDiff splits both texts into lines and marks the lines not shared by
// the other side (a longest common subsequence of lines). Very long texts
// are only split, not compared.
func lineDiff(a, b string) ([]diffLine, []diffLine) {
	al, bl := splitLines(a), splitLines(b)
	if len(al)*len(bl) > maxDiffCells {
		return plainLines(a), plainLines(b)
	}
	// lcs[i][j] = length of the common subsequence of al[i:] and bl[j:].
	lcs := make([][]int, len(al)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(bl)+1)
	}
	for i := len(al) - 1; i >= 0; i-- {
		for j := len(bl) - 1; j >= 0; j-- {
			if al[i] == bl[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	out1, out2 := make([]diffLine, 0, len(al)), make([]diffLine, 0, len(bl))
	i, j := 0, 0
	for i < len(al) || j < len(bl) {
		switch {
		case i < len(al) && j < len(bl) && al[i] == bl[j]:
			out1, out2 = append(out1, diffLine{Text: al[i]}), append(out2, diffLine{Text: bl[j]})
			i, j = i+1, j+1
		case j >= len(bl) || (i < len(al) && lcs[i+1][j] >= lcs[i][j+1]):
			out1 = append(out1, diffLine{Text: al[i], Changed: true})
			i++
		default:
			out2 = append(out2, diffLine{Text: bl[j], Changed: true})
			j++
		}
	}
	return out1, out2
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func plainLines(s string) []diffLine {
	var out []diffLine
	for _, l := range splitLines(s) {
		out = append(out, diffLine{Text: l})
	}
	return out
}

type profileField struct {
	label string
	get   func(profile.Profile) string
}

// profileFields are the profile fields a restore puts back. The bank account
// is left out: the history keeps it only masked, and a restore keeps the
// current one.
func profileFields(lang string) []profileField {
	onOff := func(on bool) string {
		if on {
			return tr(lang, "On")
		}
		return tr(lang, "Off")
	}
	return []profileField{
		{tr(lang, "Business name"), func(p profile.Profile) string { return p.BusinessName }},
		{tr(lang, "What the business does"), func(p profile.Profile) string { return p.About }},
		{tr(lang, "Tone and style"), func(p profile.Profile) string { return p.Tone }},
		{tr(lang, "How customers buy or reach staff"), func(p profile.Profile) string { return p.Contact }},
		{tr(lang, "Rules"), func(p profile.Profile) string { return p.Rules }},
		{tr(lang, "Voice notes"), func(p profile.Profile) string { return onOff(p.Features.Voice) }},
		{tr(lang, "Images"), func(p profile.Profile) string { return onOff(p.Features.Images) }},
		{tr(lang, "Stickers and likes"), func(p profile.Profile) string { return onOff(p.Features.Stickers) }},
		{tr(lang, "Human-like typing time"), func(p profile.Profile) string { return onOff(p.Features.Typing) }},
	}
}

func sameProfile(a, b profile.Profile) bool {
	for _, f := range profileFields(langEN) {
		if f.get(a) != f.get(b) {
			return false
		}
	}
	return true
}

type docField struct {
	label string
	get   func(knowledge.Document) string
}

func docFields(lang string, h *Handler) []docField {
	return []docField{
		{tr(lang, "Title or question"), func(d knowledge.Document) string { return d.Title }},
		{tr(lang, "Text or answer"), func(d knowledge.Document) string { return d.Content }},
		{tr(lang, "Used in"), func(d knowledge.Document) string { return inboxLabel(lang, d.InboxID) }},
		{tr(lang, "Dates"), func(d knowledge.Document) string { return h.docDates(lang, d) }},
	}
}

// docDates is a document's validity window as start date to last day.
func (h *Handler) docDates(lang string, d knowledge.Document) string {
	if d.ValidFrom.IsZero() && d.ValidUntil.IsZero() {
		return tr(lang, "Always")
	}
	from, last := startDate(d, h.loc()), lastDay(d, h.loc())
	return tr(lang, "from %s to %s", orNow(lang, from), orNoEnd(lang, last))
}

func sameDoc(a, b knowledge.Document) bool {
	return a.Title == b.Title && a.Content == b.Content && a.InboxID == b.InboxID &&
		a.ValidFrom.Equal(b.ValidFrom) && a.ValidUntil.Equal(b.ValidUntil)
}

// profileSnapshot reads a profile copy from the log. The admin page keeps
// profile.Profile as JSON (Go field names; the bank account masked); AI-tool
// approvals keep the MCP view (snake_case names, feature switches as bools).
func profileSnapshot(raw json.RawMessage) (profile.Profile, bool) {
	var m map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &m) != nil {
		return profile.Profile{}, false
	}
	found := false
	str := func(keys ...string) string {
		for _, k := range keys {
			var s string
			if v, ok := m[k]; ok && json.Unmarshal(v, &s) == nil {
				found = true
				return s
			}
		}
		return ""
	}
	flag := func(k string) bool {
		var b bool
		_ = json.Unmarshal(m[k], &b)
		return b
	}
	p := profile.Profile{
		BusinessName: str("BusinessName", "business_name"), About: str("About", "about"), Tone: str("Tone", "tone"),
		Contact: str("Contact", "contact"), Rules: str("Rules", "rules"),
	}
	if f, ok := m["Features"]; ok {
		_ = json.Unmarshal(f, &p.Features)
	} else {
		p.Features = profile.Features{Voice: flag("voice_notes"), Images: flag("images"), Stickers: flag("stickers"), Typing: flag("typing")}
	}
	return p, found
}

// docSnapshot reads a document copy from the log (docCopy or the MCP
// auditCopy: the same keys).
func docSnapshot(raw json.RawMessage) (knowledge.Document, bool) {
	var c struct {
		ID         int64      `json:"id"`
		Key        string     `json:"key"`
		Kind       string     `json:"kind"`
		Title      *string    `json:"title"`
		Content    *string    `json:"content"`
		InboxID    int64      `json:"inbox_id"`
		ValidFrom  *time.Time `json:"valid_from"`
		ValidUntil *time.Time `json:"valid_until"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &c) != nil || c.Title == nil || c.Content == nil {
		return knowledge.Document{}, false
	}
	d := knowledge.Document{ID: c.ID, SourceKey: c.Key, Kind: knowledge.Kind(c.Kind), Title: *c.Title, Content: *c.Content, InboxID: c.InboxID}
	if d.Kind == "" {
		d.Kind = knowledge.KindText
	}
	if c.ValidFrom != nil {
		d.ValidFrom = *c.ValidFrom
	}
	if c.ValidUntil != nil {
		d.ValidUntil = *c.ValidUntil
	}
	return d, true
}

// Restore and download failures the page explains.
var (
	errNotRestorable = errors.New("this change cannot be restored")
	errStaleProfile  = errors.New("the profile changed after the page opened")
	errStaleDoc      = errors.New("the document changed after the page opened")
	errSyncedDoc     = errors.New("synced from a website or data file")
)

func (h *Handler) submitHistory(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxHistoryForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "form too large or invalid", http.StatusBadRequest)
		return
	}
	f := r.PostForm
	// Parse strictly: falling back to account 1 would change another business.
	acct, err := strconv.ParseInt(strings.TrimSpace(f.Get("account")), 10, 64)
	if err != nil || acct < 1 {
		http.Error(w, "account must be a positive number", http.StatusBadRequest)
		return
	}
	id, _ := strconv.ParseInt(f.Get("id"), 10, 64)
	done := "restored"
	switch f.Get("action") {
	case "export_json", "export_csv":
		h.exportKnowledge(w, r, acct, strings.TrimPrefix(f.Get("action"), "export_"))
		return
	case "restore_profile":
		err = h.restoreProfile(r, acct, f.Get("source"), id, f.Get("version"))
	case "restore_doc":
		err = h.restoreDoc(r, acct, f.Get("source"), id, f.Get("version"))
	case "restore_image":
		err, done = h.restoreImage(r, acct, id), "image_restored"
	case "forget_image":
		if f.Get("confirm") != "yes" {
			// Only the separate, confirmed form erases (as on Bot images).
			http.Error(w, "confirm first", http.StatusBadRequest)
			return
		}
		err, done = h.forgetImage(r, acct, id), "image_forgotten"
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
		return
	}
	if err != nil {
		h.historyFailed(w, r, acct, err)
		return
	}
	http.Redirect(w, r, "/admin/history?account="+strconv.FormatInt(acct, 10)+"&done="+done, http.StatusSeeOther)
}

// historyFailed shows the page again with what went wrong.
func (h *Handler) historyFailed(w http.ResponseWriter, r *http.Request, acct int64, err error) {
	lang := langOf(r)
	status, msg := http.StatusUnprocessableEntity, ""
	switch {
	case errors.Is(err, audit.ErrNotFound), errors.Is(err, errNotRestorable):
		status, msg = http.StatusNotFound, tr(lang, "That change is no longer in the history, or it cannot be restored. Reload the page.")
	case errors.Is(err, errStaleProfile):
		status, msg = http.StatusConflict, tr(lang, "The profile was changed after you opened this page. Reload the page and check before restoring.")
	case errors.Is(err, errStaleDoc):
		status, msg = http.StatusConflict, tr(lang, "This answer was changed after you opened this page. Reload the page and check before restoring.")
	case errors.Is(err, errSyncedDoc):
		msg = tr(lang, "This came from a website or data file. Add the page or file again on the Knowledge page to bring it back.")
	case errors.Is(err, images.ErrNotFound):
		status, msg = http.StatusNotFound, tr(lang, "That picture is no longer kept. Reload the page.")
	case errors.Is(err, images.ErrName), errors.Is(err, images.ErrFull), errors.Is(err, images.ErrText), errors.Is(err, images.ErrType):
		msg = imageError(lang, err)
	default:
		h.Logger.Warn("restore failed", "account_id", acct, "error", err)
		msg = tr(lang, "Could not restore: %s", err.Error())
	}
	data := h.loadHistory(r, acct)
	data.Error = msg
	h.renderPage(w, r, status, "history.html", data)
}

// entryFor loads one change of the account, or an error when there is none
// or no log.
func (h *Handler) entryFor(r *http.Request, acct int64, source string, id int64) (audit.Entry, error) {
	log := h.historyLog()
	if log == nil {
		return audit.Entry{}, errNotRestorable
	}
	return log.Entry(r.Context(), acct, source, id)
}

// restoreProfile saves the profile as it was before a logged change: the
// text fields and feature switches; the bank account stays as it is now.
func (h *Handler) restoreProfile(r *http.Request, acct int64, source string, id int64, version string) error {
	if h.Profiles == nil {
		return errNotRestorable
	}
	e, err := h.entryFor(r, acct, source, id)
	if err != nil {
		return err
	}
	snap, ok := profileSnapshot(e.Before)
	if !ok || snap.Empty() || (e.Action != "business profile saved" && e.Action != "business profile restored") {
		return errNotRestorable
	}
	cur, err := h.Profiles.Get(r.Context(), acct)
	if err != nil {
		return err
	}
	if version != "" && version != profileVersion(cur) {
		return errStaleProfile
	}
	next := cur
	next.AccountID = acct
	next.BusinessName, next.About, next.Tone, next.Contact, next.Rules = snap.BusinessName, snap.About, snap.Tone, snap.Contact, snap.Rules
	// Reply timing is a delivery setting, not profile text: a restore keeps
	// the current one (older copies do not record it).
	typing := cur.Features.Typing
	next.Features = snap.Features
	next.Features.Typing = typing
	next = next.Normalize()
	if err := h.Profiles.Save(r.Context(), next); err != nil {
		return err
	}
	kept := cur
	kept.BankTransfer = maskBank(cur.BankTransfer)
	h.record(r, acct, "business profile restored", tr(langEN, "version from %s", h.whenIn(langEN, e.At)), kept)
	h.Logger.Info("business profile restored", "account_id", acct, "from_change", e.ID, "remote_addr", r.RemoteAddr)
	return nil
}

// restoreDoc puts a document back as it was before a logged edit or delete:
// over the same document when it still exists (by ID, else by key),
// otherwise as a new one, through the normal checks.
func (h *Handler) restoreDoc(r *http.Request, acct int64, source string, id int64, version string) error {
	if h.Knowledge == nil {
		return errNotRestorable
	}
	e, err := h.entryFor(r, acct, source, id)
	if err != nil {
		return err
	}
	snap, ok := docSnapshot(e.Before)
	switch e.Action {
	case "knowledge edited", "knowledge deleted", "knowledge restored":
	default:
		ok = false
	}
	if !ok {
		return errNotRestorable
	}
	if knowledge.SyncedKey(snap.SourceKey) {
		return errSyncedDoc
	}
	if snap.ID == 0 {
		snap.ID = e.DocumentID
	}
	ctx := r.Context()
	snap.AccountID = acct
	cur, getErr := h.Knowledge.Get(ctx, acct, snap.ID)
	if errors.Is(getErr, knowledge.ErrNotFound) && snap.SourceKey != "" {
		cur, getErr = h.Knowledge.GetByKey(ctx, acct, snap.SourceKey)
	}
	// A form without a version (a script) is not checked, as on the knowledge page.
	if version != "" {
		now := "0"
		if getErr == nil {
			now = docVersion(cur)
		}
		if getErr == nil || errors.Is(getErr, knowledge.ErrNotFound) {
			if now != version {
				return errStaleDoc
			}
		}
	}
	switch {
	case getErr == nil:
		d := snap
		d.ID, d.SourceKey = cur.ID, cur.SourceKey
		if _, err := h.Knowledge.Update(ctx, d); err != nil {
			return err
		}
		h.record(r, acct, "knowledge restored", d.Title, docCopy(cur))
		h.Logger.Info("knowledge restored", "account_id", acct, "document_id", d.ID, "from_change", e.ID, "remote_addr", r.RemoteAddr)
	case errors.Is(getErr, knowledge.ErrNotFound):
		d := snap
		d.ID = 0
		newID, _, err := h.Knowledge.Add(ctx, d)
		if err != nil {
			return err
		}
		h.record(r, acct, "knowledge restored", d.Title, nil)
		h.Logger.Info("knowledge restored", "account_id", acct, "document_id", newID, "from_change", e.ID, "remote_addr", r.RemoteAddr)
	default:
		return getErr
	}
	return nil
}

// restoreImage puts a removed picture back with the upload checks.
func (h *Handler) restoreImage(r *http.Request, acct, removedID int64) error {
	if h.Images == nil {
		return errNotRestorable
	}
	var name string
	if list, err := h.Images.Removed(r.Context(), acct); err == nil {
		for _, m := range list {
			if m.ID == removedID {
				name = m.Name
			}
		}
	}
	newID, err := h.Images.Restore(r.Context(), acct, removedID)
	if err != nil {
		return err
	}
	h.record(r, acct, "bot image restored", name, nil)
	h.Logger.Info("bot image restored", "account_id", acct, "image_id", newID, "remote_addr", r.RemoteAddr)
	return nil
}

// startDate is a document's first day in the business time zone, "" if open.
func startDate(d knowledge.Document, loc *time.Location) string {
	if d.ValidFrom.IsZero() {
		return ""
	}
	return d.ValidFrom.In(loc).Format(time.DateOnly)
}

// lastDay is the last day a document is used ("until" is the start of the
// next day, as the data-file importer sets it), "" if open-ended.
func lastDay(d knowledge.Document, loc *time.Location) string {
	if d.ValidUntil.IsZero() {
		return ""
	}
	return d.ValidUntil.Add(-time.Nanosecond).In(loc).Format(time.DateOnly)
}

// forgetImage erases a removed picture for good (it may show a customer).
func (h *Handler) forgetImage(r *http.Request, acct, removedID int64) error {
	if h.Images == nil {
		return errNotRestorable
	}
	if err := h.Images.Forget(r.Context(), acct, removedID); err != nil {
		return err
	}
	h.record(r, acct, "removed bot image deleted for good", "removed picture "+strconv.FormatInt(removedID, 10), nil)
	h.Logger.Info("removed bot image deleted for good", "account_id", acct, "removed_id", removedID, "remote_addr", r.RemoteAddr)
	return nil
}
