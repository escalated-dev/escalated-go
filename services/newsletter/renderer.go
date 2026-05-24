// Package newsletter implements the renderer, planner, dispatcher, and
// tracker services for the optional Escalated newsletter feature.
//
// The renderer is the heart of the package: Markdown -> theme wrap ->
// click rewrite + pixel injection. Markdown is host-pluggable via the
// Config.MarkdownToHTML field; default fallback is escape+paragraph.
package newsletter

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html/template"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/escalated-dev/escalated-go/models"
)

// Config controls the renderer + dispatcher behavior.
type Config struct {
	BaseURL             string
	DefaultTheme        string
	TrackingEnabled     bool
	ThemesDir           string // absolute path to <slug>.html template files
	MarkdownToHTML      func(md string) string
	Brand               Brand
	BatchSize           int
	ClaimTimeoutMinutes int
	AutoPauseBounceRate float64
	AutoPauseThreshold  int
	EnableNewsletters   bool
}

// Brand fields are rendered into themes via template variables.
type Brand struct {
	Name            string
	Accent          string
	LogoURL         string
	PhysicalAddress string
}

var allowedSchemes = map[string]bool{"http": true, "https": true, "mailto": true, "tel": true}

// Go's RE2-based regexp package does not support backreferences, so we match
// double-quoted and single-quoted href attributes with two separate alternations.
var anchorRE = regexp.MustCompile(`(?i)(<a\s[^>]*\bhref=)(?:"([^"]*)"|'([^']*)')`)
var mergeFieldRE = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.]+)\s*\}\}`)

// Renderer turns a delivery + newsletter + contact into themed HTML.
type Renderer struct {
	cfg Config
}

// NewRenderer constructs a Renderer with the given config. The config is
// captured by value; mutate the returned struct's cfg only via fresh
// constructions.
func NewRenderer(cfg Config) *Renderer {
	if cfg.DefaultTheme == "" {
		cfg.DefaultTheme = "default"
	}
	return &Renderer{cfg: cfg}
}

// Render builds the final HTML for one delivery row.
func (r *Renderer) Render(d *models.NewsletterDelivery, n *models.Newsletter, c *models.Contact, tpl *models.NewsletterTemplate) (string, error) {
	bodyMD := strDeref(n.BodyMarkdown)
	if bodyMD == "" && tpl != nil {
		bodyMD = tpl.BodyMarkdown
	}
	themeSlug := strDeref(n.Theme)
	if themeSlug == "" && tpl != nil {
		themeSlug = tpl.Theme
	}
	if themeSlug == "" {
		themeSlug = r.cfg.DefaultTheme
	}

	body := r.markdownToHTML(bodyMD)
	body = r.resolveMergeFields(body, c, d)

	themed, err := r.renderTheme(themeSlug, map[string]any{
		"Subject":          n.Subject,
		"Body":             template.HTML(body),
		"UnsubscribeURL":   r.UnsubscribeURL(d),
		"ViewInBrowserURL": r.ViewInBrowserURL(d),
		"Brand":            r.cfg.Brand,
	})
	if err != nil {
		return "", err
	}

	if !r.cfg.TrackingEnabled {
		return themed, nil
	}
	return r.injectPixel(r.rewriteLinks(themed, d), d), nil
}

// UnsubscribeURL returns the absolute one-click unsubscribe URL.
func (r *Renderer) UnsubscribeURL(d *models.NewsletterDelivery) string {
	return strings.TrimRight(r.cfg.BaseURL, "/") + "/escalated/n/u/" + d.TrackingToken
}

// ViewInBrowserURL returns the absolute view-in-browser URL.
func (r *Renderer) ViewInBrowserURL(d *models.NewsletterDelivery) string {
	return strings.TrimRight(r.cfg.BaseURL, "/") + "/escalated/n/v/" + d.TrackingToken
}

func (r *Renderer) markdownToHTML(md string) string {
	if r.cfg.MarkdownToHTML != nil {
		return r.cfg.MarkdownToHTML(md)
	}
	// Minimal fallback. Production hosts should plug in goldmark/blackfriday/etc.
	escaped := template.HTMLEscapeString(md)
	parts := strings.Split(escaped, "\n\n")
	return "<p>" + strings.Join(parts, "</p><p>") + "</p>"
}

func (r *Renderer) resolveMergeFields(html string, c *models.Contact, d *models.NewsletterDelivery) string {
	return mergeFieldRE.ReplaceAllStringFunc(html, func(match string) string {
		m := mergeFieldRE.FindStringSubmatch(match)
		if len(m) < 2 {
			return ""
		}
		return template.HTMLEscapeString(r.resolvePath(strings.TrimSpace(m[1]), c, d))
	})
}

func (r *Renderer) resolvePath(path string, c *models.Contact, d *models.NewsletterDelivery) string {
	name := strDeref(c.Name)
	switch {
	case path == "contact.name":
		return name
	case path == "contact.first_name":
		if i := strings.Index(name, " "); i >= 0 {
			return name[:i]
		}
		return name
	case path == "contact.email":
		return c.Email
	case path == "unsubscribe_url":
		return r.UnsubscribeURL(d)
	case path == "view_in_browser_url":
		return r.ViewInBrowserURL(d)
	case strings.HasPrefix(path, "contact.metadata."):
		key := strings.TrimPrefix(path, "contact.metadata.")
		if c.Metadata != nil {
			if v, ok := c.Metadata[key]; ok {
				return fmt.Sprintf("%v", v)
			}
		}
		return ""
	}
	return ""
}

func (r *Renderer) renderTheme(slug string, data map[string]any) (string, error) {
	candidate := filepath.Join(r.cfg.ThemesDir, slug+".html")
	if _, err := os.Stat(candidate); err != nil {
		candidate = filepath.Join(r.cfg.ThemesDir, "default.html")
	}
	t, err := template.ParseFiles(candidate)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (r *Renderer) rewriteLinks(html string, d *models.NewsletterDelivery) string {
	unsub := r.UnsubscribeURL(d)
	view := r.ViewInBrowserURL(d)
	return anchorRE.ReplaceAllStringFunc(html, func(match string) string {
		m := anchorRE.FindStringSubmatch(match)
		if len(m) < 4 {
			return match
		}
		prefix := m[1]
		var quote, href string
		if m[2] != "" {
			quote, href = `"`, m[2]
		} else {
			quote, href = `'`, m[3]
		}
		if href == "" || strings.HasPrefix(href, "#") {
			return match
		}
		u, err := url.Parse(href)
		if err != nil {
			return fmt.Sprintf("%s%s#%s", prefix, quote, quote)
		}
		scheme := strings.ToLower(u.Scheme)
		if !allowedSchemes[scheme] {
			return fmt.Sprintf("%s%s#%s", prefix, quote, quote)
		}
		if scheme == "mailto" || scheme == "tel" {
			return match
		}
		if strings.HasPrefix(href, unsub) || strings.HasPrefix(href, view) {
			return match
		}
		encoded := base64.RawURLEncoding.EncodeToString([]byte(href))
		tracked := strings.TrimRight(r.cfg.BaseURL, "/") + "/escalated/n/c/" + d.TrackingToken + "?u=" + encoded
		return fmt.Sprintf("%s%s%s%s", prefix, quote, tracked, quote)
	})
}

func (r *Renderer) injectPixel(html string, d *models.NewsletterDelivery) string {
	pixelURL := strings.TrimRight(r.cfg.BaseURL, "/") + "/escalated/n/o/" + d.TrackingToken + ".gif"
	pixel := fmt.Sprintf(`<img src="%s" width="1" height="1" alt="" />`, template.HTMLEscapeString(pixelURL))
	if strings.Contains(html, "</body>") {
		return strings.Replace(html, "</body>", pixel+"</body>", 1)
	}
	return html + pixel
}

func strDeref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
