package bot

import (
	"regexp"
	"strings"
	"unicode"
)

// Chat apps (Messenger, LINE, Instagram) show markdown as raw symbols, so
// replies are sent as plain text whatever the model wrote.
var (
	mdBold    = regexp.MustCompile(`\*\*([^*\n]+)\*\*|__([^_\n]+)__`)
	mdLink    = regexp.MustCompile(`\[([^\]\n]+)\]\((https?://[^)\s]+)\)`)
	mdHeading = regexp.MustCompile(`(?m)^[ \t]{0,3}#{1,6}[ \t]+`)
)

// plainChat removes bold, headings and markdown links (keeping the link's
// text and its address) and swaps the Arabic question mark some models slip
// into Thai text for a plain one.
func plainChat(s string) string {
	s = mdLink.ReplaceAllStringFunc(s, func(m string) string {
		p := mdLink.FindStringSubmatch(m)
		if strings.TrimSpace(p[1]) == p[2] {
			return p[2]
		}
		return p[1] + " " + p[2]
	})
	s = mdBold.ReplaceAllString(s, "$1$2")
	s = mdHeading.ReplaceAllString(s, "")
	return strings.ReplaceAll(s, "؟", "?")
}

// strayScripts are writing systems the bot's customers do not use unless they
// write in them first. Models sometimes slip a word from one of them into a
// Thai or English reply ("คอมبيوتر", "настроение"). Scripts that are written
// together (Chinese characters with Japanese kana) form one group.
var strayScripts = [][]*unicode.RangeTable{
	{unicode.Arabic}, {unicode.Cyrillic}, {unicode.Hebrew}, {unicode.Hangul}, {unicode.Devanagari},
	{unicode.Greek}, {unicode.Armenian}, {unicode.Georgian},
	{unicode.Han, unicode.Hiragana, unicode.Katakana},
}

// dropStrayScripts removes letters of a stray script from reply unless the
// customer's message uses that script, then tidies the spaces left behind.
// Issue: DEV-Johnstocker/chatwoot#1
func dropStrayScripts(reply, customer string) string {
	var drop []*unicode.RangeTable
	for _, group := range strayScripts {
		in := func(r rune) bool { return unicode.In(r, group...) }
		if strings.ContainsFunc(reply, in) && !strings.ContainsFunc(customer, in) {
			drop = append(drop, group...)
		}
	}
	if len(drop) == 0 {
		return reply
	}
	kept := strings.Map(func(r rune) rune {
		if unicode.In(r, drop...) {
			return -1
		}
		return r
	}, reply)
	return tidySpaces(kept)
}

var (
	spaceRun    = regexp.MustCompile(`[ \t]{2,}`)
	spaceBefore = regexp.MustCompile(`[ \t]+([.,!?])`)
)

// tidySpaces collapses runs of spaces, drops spaces before punctuation and
// trims each line's end.
func tidySpaces(s string) string {
	s = spaceRun.ReplaceAllString(s, " ")
	s = spaceBefore.ReplaceAllString(s, "$1")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
