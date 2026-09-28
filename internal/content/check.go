package content

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Severity of a validation issue.
type Severity string

const (
	SevError   Severity = "error"
	SevWarning Severity = "warning"
)

// Issue is one validation finding for a field.
type Issue struct {
	Field    string   `json:"field"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
}

// Length guidance for search results and social cards.
const (
	maxMetaTitle       = 60
	maxMetaDescription = 160
	maxOGTitle         = 70
	maxOGDescription   = 200
)

var (
	slugRe      = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	twitterCard = map[string]bool{"summary": true, "summary_large_image": true, "app": true, "player": true}
)

// Check validates the fields needed to publish with complete SEO and social
// metadata. Errors block push; warnings only fail `content check --strict`.
func Check(d *Doc) []Issue {
	var out []Issue
	add := func(sev Severity, field, format string, a ...any) {
		out = append(out, Issue{field, sev, fmt.Sprintf(format, a...)})
	}
	req := func(field, v string) bool {
		if strings.TrimSpace(v) == "" {
			add(SevError, field, "required")
			return false
		}
		return true
	}
	maxLen := func(field string, v *string, n int) {
		if v != nil && utf8.RuneCountInString(*v) > n {
			add(SevWarning, field, "%d characters, recommended max %d", utf8.RuneCountInString(*v), n)
		}
	}
	isURL := func(field string, v *string) {
		if v == nil {
			return
		}
		u, err := url.Parse(*v)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			add(SevError, field, "must be an absolute http(s) URL")
		}
	}

	if req("slug", d.Slug) && !slugRe.MatchString(d.Slug) {
		add(SevError, "slug", "must be lowercase letters, digits, and single hyphens")
	}
	req("title", d.Title)
	req("contentType", d.ContentType)
	req("body", d.Body)
	req("meta.description", deref(d.Meta.Description))
	req("og.description", deref(d.OG.Description))

	// Titles fall back to the content title, so length applies to the
	// effective value.
	maxLen("meta.title", orTitle(d.Meta.Title, d.Title), maxMetaTitle)
	maxLen("meta.description", d.Meta.Description, maxMetaDescription)
	maxLen("og.title", orTitle(d.OG.Title, d.Title), maxOGTitle)
	maxLen("og.description", d.OG.Description, maxOGDescription)

	isURL("meta.canonicalUrl", d.Meta.CanonicalURL)
	isURL("og.imageUrl", d.OG.ImageURL)
	if d.OG.ImageURL == nil {
		add(SevWarning, "og.imageUrl", "missing; social cards will have no image")
	}
	if c := d.OG.TwitterCardType; c != nil && !twitterCard[*c] {
		add(SevError, "og.twitterCardType", "must be one of summary, summary_large_image, app, player")
	}
	if d.Excerpt == nil {
		add(SevWarning, "excerpt", "missing")
	}
	return out
}

// HasErrors reports whether any issue is an error.
func HasErrors(issues []Issue) bool {
	for _, i := range issues {
		if i.Severity == SevError {
			return true
		}
	}
	return false
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func orTitle(p *string, title string) *string {
	if p != nil {
		return p
	}
	return &title
}
