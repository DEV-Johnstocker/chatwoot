package admin

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Leaving a page with unsaved changes asks first, and save messages show once.
// The behaviour lives in static/admin.js; these tests pin the markers it reads.
// Issue: DEV-Johnstocker/chatwoot#1

func TestUnsaved_EverySaveMessageIsMarked(t *testing.T) {
	// Arrange
	saveMsg := regexp.MustCompile(`\{\{if (\.Saved|\.Notice|eq \.Done "[a-z-]+")\}\}<div class="alert [a-z]+"[^>]*>`)

	// Act
	for name, src := range templateSources(t) {
		for _, tag := range saveMsg.FindAllString(src, -1) {
			// Assert
			if !strings.Contains(tag, "data-flash") {
				t.Errorf("%s: save message %q lacks data-flash", name, tag)
			}
		}
	}
}

func TestUnsaved_NotSavedErrorsAreMarked(t *testing.T) {
	// Arrange
	notSaved := regexp.MustCompile(`<div class="alert err"[^>]*><strong>\{\{T "Not (saved|added)\."\}\}`)

	// Act
	found := 0
	for name, src := range templateSources(t) {
		for _, tag := range notSaved.FindAllString(src, -1) {
			found++
			// Assert
			if !strings.Contains(tag, "data-unsaved") {
				t.Errorf("%s: %q lacks data-unsaved, so the rejected form would not count as changed", name, tag)
			}
		}
	}
	if found < 5 {
		t.Errorf("found %d Not saved/added messages, want the business, hours, nudge, alerts and knowledge ones", found)
	}
}

func TestUnsaved_TestChatOptsOutAndScriptWarns(t *testing.T) {
	// Arrange
	sources := templateSources(t)
	js, err := os.ReadFile("static/admin.js")
	if err != nil {
		t.Fatal(err)
	}

	// Act / Assert
	if !strings.Contains(sources["test.html"], `action="/admin/test" autocomplete="off" enctype="multipart/form-data" data-no-warn`) {
		t.Error("the test chat must not ask before leaving")
	}
	for _, want := range []string{"beforeunload", "data-unsaved", "[data-flash]", "history.replaceState", "'Unsaved changes': 'ยังไม่ได้บันทึก'"} {
		if !strings.Contains(string(js), want) {
			t.Errorf("admin.js lacks %q", want)
		}
	}
	if !strings.Contains(sources["layout.html"], ".unsaved-note") || !strings.Contains(sources["layout.html"], ".alert.toast") {
		t.Error("layout.html lacks the unsaved note or toast style")
	}
}
