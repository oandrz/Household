package domain

import (
	"html"
	"regexp"
	"strings"
)

// linkPattern matches an http or https URL up to the first character that
// cannot appear inside one unescaped: whitespace, or one of the delimiters
// that surround a URL in HTML (<, >, ", '). Trailing sentence punctuation is
// removed afterwards rather than excluded here, because a "." or a "?" is
// legitimately *inside* most of Hearth's URLs and only ever junk at the end.
var linkPattern = regexp.MustCompile(`https?://[^\s<>"']+`)

// trailingPunctuation is stripped from the end of a match. It is written out
// character by character on purpose, and "-" and "_" are deliberately absent:
// every token in this product is base64.RawURLEncoding (adapter/crypto/
// tokens.go), whose alphabet includes both, and both link shapes put the
// token last -- "<base>/invite/<token>" and
// "<base>/sign-in/magic?token=<token>". Adding either character here would
// silently truncate roughly one token in thirty-two into a link that looks
// right, copies right, and fails on use. Do not "simplify" this into a
// general non-alphanumeric strip.
const trailingPunctuation = `.,;:!?)]>"'`

// ExtractLinks returns every http and https URL in a message body, in the
// order they appear, with duplicates removed. It reads text when text has
// content and falls back to htmlBody otherwise.
//
// Hearth sends text/plain only (adapter/mail/smtp.go builds every message
// with gomail.TypeTextPlain), so the HTML path is for messages this product
// did not send -- anything that speaks SMTP to Mailpit lands in the same
// store, and an empty link list on such a message would read as a broken
// screen rather than as a message with no links.
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
