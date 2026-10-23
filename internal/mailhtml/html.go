package mailhtml

import (
	stdhtml "html"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// Sanitize retains passive mail formatting, without scripts, remote images,
// unsafe styling, event handlers or non-web link protocols.
func Sanitize(value string) string { return SanitizeWithCID(value, nil) }
func SanitizeWithCID(value string, ids map[string]bool) string {
	root, err := html.Parse(strings.NewReader(value))
	if err != nil {
		return ""
	}
	var output strings.Builder
	var render func(*html.Node)
	render = func(node *html.Node) {
		if node.Type == html.TextNode {
			output.WriteString(stdhtml.EscapeString(node.Data))
			return
		}
		if node.Type == html.ElementNode && node.Data == "img" {
			var src, alt string
			for _, a := range node.Attr {
				if a.Namespace != "" {
					continue
				}
				if a.Key == "src" {
					src = a.Val
				}
				if a.Key == "alt" {
					alt = a.Val
				}
			}
			if strings.HasPrefix(src, "cid:") && ids[strings.TrimPrefix(src, "cid:")] {
				output.WriteString(`<img src="` + stdhtml.EscapeString(src) + `" alt="` + stdhtml.EscapeString(alt) + `">`)
			}
			return
		}
		tag := node.Data
		allowed := false
		if node.Type == html.ElementNode {
			switch tag {
			case "script", "style", "svg", "math", "iframe", "object", "embed", "template", "head":
				return
			case "p", "br", "strong", "b", "em", "i", "u", "s", "ul", "ol", "li", "blockquote", "a", "code", "span", "h1", "h2", "h3", "h4", "h5", "h6", "div", "pre", "hr", "table", "thead", "tbody", "tfoot", "tr", "td", "th", "caption", "sub", "sup", "dl", "dt", "dd", "del", "ins":
				allowed = true
			}
		}
		if allowed {
			output.WriteString("<" + tag)
			if style := safeFormatting(tag, node.Attr); style != "" {
				output.WriteString(` style="` + stdhtml.EscapeString(style) + `"`)
			}
			if tag == "a" {
				for _, attr := range node.Attr {
					if attr.Key != "href" || attr.Namespace != "" {
						continue
					}
					href := strings.TrimSpace(attr.Val)
					parsed, err := url.Parse(href)
					if err != nil || len(href) > 2048 || strings.ContainsAny(href, "\r\n\x00") {
						continue
					}
					switch strings.ToLower(parsed.Scheme) {
					case "http", "https":
						if parsed.Host == "" {
							continue
						}
					case "mailto":
					default:
						continue
					}
					output.WriteString(` href="` + stdhtml.EscapeString(href) + `" rel="noopener noreferrer" target="_blank"`)
					break
				}
			}
			output.WriteString(">")
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			render(child)
		}
		if allowed && tag != "br" && tag != "hr" {
			output.WriteString("</" + tag + ">")
		}
	}
	render(root)
	return output.String()
}

var hexColor = regexp.MustCompile(`^#[0-9a-f]{3}([0-9a-f]{3})?$`)
var rgbColor = regexp.MustCompile(`^rgb\(\s*([0-9]{1,3})\s*,\s*([0-9]{1,3})\s*,\s*([0-9]{1,3})\s*\)$`)

func safeColor(value string) bool {
	if hexColor.MatchString(value) {
		return true
	}
	match := rgbColor.FindStringSubmatch(value)
	if match == nil {
		return false
	}
	for _, component := range match[1:] {
		number, err := strconv.Atoi(component)
		if err != nil || number > 255 {
			return false
		}
	}
	return true
}

func safeFormatting(tag string, attributes []html.Attribute) string {
	if tag != "span" && tag != "p" && tag != "h1" && tag != "h2" && tag != "h3" && tag != "h4" && tag != "h5" && tag != "h6" {
		return ""
	}
	values := make(map[string]string)
	for _, attribute := range attributes {
		if attribute.Key != "style" || attribute.Namespace != "" {
			continue
		}
		for _, declaration := range strings.Split(attribute.Val, ";") {
			property, value, ok := strings.Cut(declaration, ":")
			if !ok {
				continue
			}
			property = strings.ToLower(strings.TrimSpace(property))
			value = strings.ToLower(strings.TrimSpace(value))
			if tag == "span" {
				switch property {
				case "color", "background-color":
					if safeColor(value) {
						values[property] = value
					}
				case "font-size":
					switch value {
					case "12px", "14px", "16px", "18px", "20px", "24px":
						values[property] = value
					}
				}
			} else if property == "text-align" {
				switch value {
				case "left", "center", "right", "justify":
					values[property] = value
				}
			}
		}
	}
	var declarations []string
	for _, property := range []string{"color", "background-color", "font-size", "text-align"} {
		if value := values[property]; value != "" {
			declarations = append(declarations, property+": "+value)
		}
	}
	return strings.Join(declarations, "; ")
}
