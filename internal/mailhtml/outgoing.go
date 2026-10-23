package mailhtml

import (
	"bytes"
	"strings"

	"golang.org/x/net/html"
)

// Outgoing adds fixed, passive email formatting after sanitization. These
// inline styles travel with the MIME part, including in clients without CSS
// stylesheet support. App palettes never change a recipient's email colors.
func Outgoing(value string) string {
	value = Sanitize(value)
	if strings.TrimSpace(value) == "" {
		return ""
	}
	root, err := html.Parse(strings.NewReader(value))
	if err != nil {
		return value
	}
	styles := map[string]string{
		"p":  "margin: 0 0 1em",
		"h1": "font-size: 28px; line-height: 1.25; margin: 1em 0 .6em",
		"h2": "font-size: 24px; line-height: 1.25; margin: 1em 0 .6em",
		"h3": "font-size: 20px; line-height: 1.25; margin: 1em 0 .6em",
		"a":  "color: #3b82f6; text-decoration: underline",
		"ul": "padding-left: 24px", "ol": "padding-left: 24px",
		"blockquote": "margin: 1em 0; padding-left: 16px; border-left: 3px solid #cbd5e1; color: #596776",
		"pre":        "white-space: pre-wrap; padding: 14px; background-color: #f1f4f7; border-radius: 6px",
		"code":       "font-family: monospace; font-size: .9em; line-height: 1.6",
		"table":      "border-collapse: collapse; max-width: 100%; width: 100%; margin: 1em 0",
		"td":         "border: 1px solid #d6dde5; padding: 8px 10px; text-align: left",
		"th":         "border: 1px solid #d6dde5; padding: 8px 10px; text-align: left; background-color: #f1f4f7",
		"hr":         "border: 0; border-top: 1px solid #d6dde5; margin: 1.5em 0",
	}
	var body *html.Node
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if node.Type == html.ElementNode {
			if node.Data == "body" {
				body = node
			}
			style := styles[node.Data]
			if node.Data == "p" && node.Parent != nil {
				switch node.Parent.Data {
				case "td", "th":
					style = "margin: 0"
				case "li":
					style = "margin: 0 0 .35em"
				}
			}
			if style != "" {
				found := false
				for i := range node.Attr {
					if node.Attr[i].Key == "style" {
						node.Attr[i].Val = style + "; " + node.Attr[i].Val
						found = true
					}
				}
				if !found {
					node.Attr = append(node.Attr, html.Attribute{Key: "style", Val: style})
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(root)
	if body == nil {
		return value
	}
	var output bytes.Buffer
	output.WriteString(`<div style="font-family: Arial, sans-serif; font-size: 15px; line-height: 1.65; color: #202a35; background-color: #ffffff; overflow-wrap: anywhere">`)
	for child := body.FirstChild; child != nil; child = child.NextSibling {
		if err := html.Render(&output, child); err != nil {
			return value
		}
	}
	output.WriteString("</div>")
	return output.String()
}
