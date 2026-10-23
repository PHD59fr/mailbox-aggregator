package mailhtml

import (
	"bytes"
	stdhtml "html"
	"regexp"
	"strings"

	"github.com/aymerick/douceur/css"
	"github.com/aymerick/douceur/parser"
	"github.com/microcosm-cc/bluemonday"
	"golang.org/x/net/html"
)

func incomingPolicy(images bool) *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AllowTables()
	p.AllowLists()
	p.AllowElements("div", "span", "font", "center", "hr")
	p.AllowAttrs("class", "id", "dir", "lang").Globally()
	p.AllowAttrs("align", "valign", "width", "height", "bgcolor", "cellpadding", "cellspacing", "border", "colspan", "rowspan").Matching(regexp.MustCompile(`^[a-zA-Z0-9#% .-]{1,80}$`)).Globally()
	p.AllowAttrs("color", "face", "size").Matching(regexp.MustCompile(`^[a-zA-Z0-9# ,.-]{1,100}$`)).OnElements("font")
	p.AllowAttrs("style").Globally()
	p.AllowStyles("color", "background-color", "font-size", "font-family", "font-weight", "font-style", "text-decoration", "text-align", "vertical-align", "line-height", "letter-spacing", "white-space", "word-break", "word-wrap", "display", "width", "max-width", "min-width", "height", "max-height", "min-height", "margin", "margin-top", "margin-right", "margin-bottom", "margin-left", "padding", "padding-top", "padding-right", "padding-bottom", "padding-left", "border", "border-top", "border-right", "border-bottom", "border-left", "border-color", "border-width", "border-style", "border-radius", "border-collapse", "border-spacing", "table-layout", "list-style-type").Globally()
	// No CSS URLs, positioning, active elements, external fonts or stylesheets.
	p.RequireNoReferrerOnLinks(true)
	p.AddTargetBlankToFullyQualifiedLinks(true)
	if images {
		p.AllowImages()
		p.AllowURLSchemes("cid")
		p.AllowAttrs("referrerpolicy").Matching(regexp.MustCompile(`^no-referrer$`)).OnElements("img")
	}
	return p
}

var richPolicy = incomingPolicy(true)
var privatePolicy = incomingPolicy(false)
var selectorPattern = regexp.MustCompile(`^[a-zA-Z0-9_\s.#>,+*\-]{1,200}$`)
var backgroundURL = regexp.MustCompile(`(?i)background(?:-image)?\s*:[^;]*url\s*\(`)

// Incoming is deliberately separate from the stricter outgoing-editor policy.
// Only passive formatting survives; external content means HTTP(S) images only.
func Incoming(value string, external bool) (string, string) {
	root, err := html.Parse(strings.NewReader(value))
	if err != nil {
		return "", ""
	}
	var styles strings.Builder
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "style" {
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				if child.Type == html.TextNode && styles.Len() < 65536 {
					styles.WriteString(child.Data)
				}
			}
		}
		if node.Type == html.ElementNode && node.Data == "img" {
			if !external && node.Parent != nil {
				label := "[Blocked image]"
				for _, attr := range node.Attr {
					if attr.Key == "alt" {
						alt := []rune(strings.Join(strings.Fields(attr.Val), " "))
						if len(alt) > 200 {
							alt = alt[:200]
						}
						if len(alt) > 0 {
							label += " " + string(alt)
						}
					}
				}
				replaceBlocked(node, label)
				return
			}
			node.Attr = append(node.Attr, html.Attribute{Key: "referrerpolicy", Val: "no-referrer"})
		}
		if node.Type == html.ElementNode && node.Parent != nil {
			switch node.Data {
			case "iframe", "object", "embed", "video", "audio":
				replaceBlocked(node, "[Blocked external content]")
				return
			}
		}
		if node.Type == html.ElementNode {
			for _, attr := range node.Attr {
				if (attr.Key == "background" && strings.TrimSpace(attr.Val) != "") || (attr.Key == "style" && backgroundURL.MatchString(attr.Val)) {
					// CSS/legacy background URLs stay blocked even with image
					// consent. Preserve the original text and show what is missing.
					appendBackgroundPlaceholder(node)
					break
				}
			}
		}
		for child := node.FirstChild; child != nil; {
			next := child.NextSibling
			visit(child)
			child = next
		}
	}
	visit(root)
	var serialized bytes.Buffer
	_ = html.Render(&serialized, root)
	p := privatePolicy
	if external {
		p = richPolicy
	}
	return p.Sanitize(serialized.String()), sanitizeSheet(styles.String())
}

func replaceBlocked(node *html.Node, label string) {
	node.Parent.InsertBefore(blockedPlaceholder(label), node)
	node.Parent.RemoveChild(node)
}

func blockedPlaceholder(label string) *html.Node {
	placeholder := &html.Node{Type: html.ElementNode, Data: "span", Attr: []html.Attribute{{Key: "class", Val: "blocked-element"}}}
	placeholder.AppendChild(&html.Node{Type: html.TextNode, Data: label})
	return placeholder
}

func appendBackgroundPlaceholder(node *html.Node) {
	placeholder := blockedPlaceholder("[Blocked background image]")
	switch node.Data {
	case "table":
		caption := &html.Node{Type: html.ElementNode, Data: "caption"}
		caption.AppendChild(placeholder)
		node.InsertBefore(caption, node.FirstChild)
	case "thead", "tbody", "tfoot":
		row := &html.Node{Type: html.ElementNode, Data: "tr"}
		cell := &html.Node{Type: html.ElementNode, Data: "td"}
		cell.AppendChild(placeholder)
		row.AppendChild(cell)
		node.AppendChild(row)
	case "tr":
		cell := &html.Node{Type: html.ElementNode, Data: "td"}
		cell.AppendChild(placeholder)
		node.AppendChild(cell)
	case "img", "input", "br", "hr", "source", "link", "meta", "col", "colgroup":
		// Void/column elements cannot contain a visible placeholder.
	default:
		node.AppendChild(placeholder)
	}
}

func sanitizeSheet(value string) string {
	if len(value) > 65536 {
		value = value[:65536]
	}
	sheet, err := parser.Parse(value)
	if err != nil {
		return ""
	}
	var out strings.Builder
	for i, rule := range sheet.Rules {
		if i >= 250 {
			break
		}
		if rule.Kind != css.QualifiedRule || !selectorPattern.MatchString(rule.Prelude) {
			continue
		}
		var declarations strings.Builder
		for j, declaration := range rule.Declarations {
			if j >= 60 {
				break
			}
			declarations.WriteString(declaration.String())
		}
		filtered := richPolicy.Sanitize(`<span style="` + stdhtml.EscapeString(declarations.String()) + `">x</span>`)
		root, _ := html.Parse(strings.NewReader(filtered))
		var style string
		var find func(*html.Node)
		find = func(n *html.Node) {
			if n.Type == html.ElementNode && n.Data == "span" {
				for _, attr := range n.Attr {
					if attr.Key == "style" {
						style = attr.Val
					}
				}
			}
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				find(child)
			}
		}
		find(root)
		if style != "" {
			style = strings.ReplaceAll(style, "<", `\3c `)
			style = strings.ReplaceAll(style, ">", `\3e `)
			out.WriteString(rule.Prelude + "{" + style + "}\n")
		}
	}
	return out.String()
}

// Document is served with its own strict CSP instead of inheriting the app's
// inline-style prohibition through srcdoc. It still has an opaque-origin sandbox.
func Document(body, styles string, external bool) string {
	body, _ = Incoming(body, external)
	styles = sanitizeSheet(styles)
	return `<!doctype html><html><head><meta charset="utf-8"><meta name="referrer" content="no-referrer"><meta name="viewport" content="width=device-width,initial-scale=1"><style>:root{color-scheme:light}body{margin:0;padding:22px;font:15px/1.65 system-ui,sans-serif;color:#202a3b;background:white;overflow-wrap:anywhere}table{max-width:100%}img{max-width:100%;height:auto}pre{white-space:pre-wrap}blockquote{margin-left:0;padding-left:16px;border-left:3px solid #d4daec}a{color:#3454cf}</style><style>` + styles + `</style><style>span.blocked-element{display:inline-block!important;min-width:100px!important;min-height:24px!important;max-width:100%!important;padding:8px 12px!important;margin:4px 0!important;border:1px dashed #bac4d7!important;border-radius:8px!important;background:#f1f4f9!important;color:#53627b!important;font:13px/1.5 system-ui,sans-serif!important;white-space:normal!important}</style></head><body>` + body + `</body></html>`
}
