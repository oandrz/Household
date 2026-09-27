package domain

import (
	"html"
	"regexp"
	"strings"
)

// linkPattern matches an http or https URL up to the first character that
// can't appear inside one unescaped: whitespace or an HTML delimiter (<, >,
// ", '). Trailing punctuation is stripped afterward, not excluded here --
// "." and "?" are legitimately inside most URLs and only junk at the end.
var linkPattern = regexp.MustCompile(`https?://[^\s<>"']+`)

// trailingPunctuation is stripped from a match's end, char by char on
// purpose: "-" and "_" are absent, since every token is base64.RawURLEncoding
// (whose alphabet includes both) and every link shape puts the token last.
// Adding either character would truncate about one token in thirty-two into
// a link that looks and copies right but fails on use -- don't "simplify"
// this into a general non-alphanumeric strip.
const trailingPunctuation = `.,;:!?)]>"'`

// ExtractLinks returns every http and https URL in a message body, in the
// order they appear, with duplicates removed. It reads text when text has
// content and falls back to htmlBody otherwise.
//
// Hearth sends text/plain only, so the HTML path is for messages this
// product didn't send (smtp.go always uses gomail.TypeTextPlain) --
// anything that speaks SMTP to Mailpit lands in the same store, where an
// empty link list would read as broken, not as a message with no links.
//
// The result is always non-nil, so a caller can range over it and a JSON
// encoder writes [] rather than null.
func ExtractLinks(text, htmlBody string) []string {
	source := text
	if strings.TrimSpace(source) == "" {
		// Only on this path: an href writes "&amp;" where the URL has "&".
		source = html.UnescapeString(htmlBody)
	}

	links := make([]string, 0)
	seen := make(map[string]bool)
	for _, match := range linkPattern.FindAllString(source, -1) {
		link := strings.TrimRight(match, trailingPunctuation)
		if link == "" || seen[link] {
			continue
		}
		seen[link] = true
		links = append(links, link)
	}
	return links
}
