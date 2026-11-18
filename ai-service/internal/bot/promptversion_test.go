package bot

import (
	"strings"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/profile"
)

// Issue: DEV-Johnstocker/chatwoot#1

func TestRulesVersion_StableOverTimeAndChangesWithThePrompt(t *testing.T) {
	// Arrange / Act
	a, b := rulesVersion("Be kind."), rulesVersion("Be kind.")
	c := rulesVersion("Be brief.")

	// Assert
	if a != b || len(a) != 8 {
		t.Errorf("same prompt gave %q and %q, want one 8-character version", a, b)
	}
	if a == c {
		t.Error("a different configured prompt kept the same version")
	}
}

func TestProfileVersion_FollowsTheTextNotTheSaveTime(t *testing.T) {
	// Arrange
	p := profile.Profile{AccountID: 1, BusinessName: "Shop", Rules: "ห้ามให้ส่วนลด", UpdatedAt: time.Now()}
	resaved := p
	resaved.UpdatedAt = p.UpdatedAt.Add(time.Hour)
	edited := p
	edited.Rules = "ให้ส่วนลดได้ 5%"

	// Act / Assert
	if profileVersion(p) != profileVersion(resaved) {
		t.Error("saving the same text again changed the version")
	}
	if profileVersion(p) == profileVersion(edited) {
		t.Error("editing the rules kept the same version")
	}
	if v := promptVersion("abcd1234", p); !strings.HasPrefix(v, "rabcd1234-p") || len(v) != len("rabcd1234-p")+8 {
		t.Errorf("prompt version = %q", v)
	}
}
