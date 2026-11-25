package ingest

import "github.com/microcosm-cc/bluemonday"

var htmlPolicy = func() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.RequireNoFollowOnLinks(true)
	p.AddTargetBlankToFullyQualifiedLinks(true)
	p.AllowAttrs("style").OnElements(
		"table", "thead", "tbody", "tfoot", "tr", "td", "th",
		"p", "div", "span", "br", "hr", "h1", "h2", "h3", "h4", "h5", "h6",
		"ul", "ol", "li", "blockquote", "pre", "code",
	)
	p.AllowAttrs("align", "width", "height", "bgcolor", "cellpadding", "cellspacing", "border", "colspan", "rowspan").
		OnElements("table", "thead", "tbody", "tfoot", "tr", "td", "th")
	return p
}()

// SanitizeHTML removes scripts, event handlers and dangerous URLs from email
// HTML. The frontend still renders it inside a sandboxed iframe with a strict CSP.
func SanitizeHTML(in string) string {
	if in == "" {
		return ""
	}
	return htmlPolicy.Sanitize(in)
}
