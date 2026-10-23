package mailhtml

import (
	"strings"
	"testing"
)

func TestSanitizeHTML(t *testing.T) {
	value := `<p onclick="alert(1)">Hello <strong>team</strong> &amp; friends</p><ul><li><em>One</em></li></ul><a href="https://example.test?a=1&amp;b=2">Safe</a><a href="javascript:alert(1)">Unsafe</a><a href="data:text/html,bad">Bad</a><img src="https://tracker.test/x"><script>alert(1)</script><style>body{color:red}</style><svg onload="alert(1)"></svg><iframe src="https://bad.test"></iframe>`
	got := Sanitize(value)
	for _, unwanted := range []string{"onclick", "javascript", "data:text", "<img", "<script", "<style", "<svg", "<iframe", "tracker.test", "alert(1)"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("unsafe content survived: %s", got)
		}
	}
	for _, expected := range []string{"<strong>team</strong>", "<ul><li><em>One</em></li></ul>", `href="https://example.test?a=1&amp;b=2"`, "&amp; friends"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("formatting missing: %s", got)
		}
	}
}

func TestSanitizeHTMLLinks(t *testing.T) {
	for _, href := range []string{"https://example.test", "http://example.test", "mailto:team@example.test"} {
		if got := Sanitize(`<a href="` + href + `">link</a>`); !strings.Contains(got, `href="`+href+`"`) {
			t.Fatalf("safe link removed: %s", got)
		}
	}
	for _, href := range []string{"/relative", "//example.test", "file:///etc/passwd", "javascript:alert(1)", "https:"} {
		if got := Sanitize(`<a href="` + href + `">link</a>`); strings.Contains(got, "href=") {
			t.Fatalf("unsafe link survived: %s", got)
		}
	}
}

func TestSanitizeHTMLRetainsOnlySafeFormatting(t *testing.T) {
	got := Sanitize(`<h2 style="text-align: center; position: fixed">Heading</h2><p style="text-align: right"><span style="color: rgb(59, 130, 246); background-color: #fef08a; font-size: 20px; background-image: url(https://tracker.test)">Styled</span></p><span style="color: expression(alert(1)); font-size: 9999px; background-color: url(https://tracker.test)">Unsafe</span>`)
	for _, expected := range []string{`<h2 style="text-align: center">`, `text-align: right`, `color: rgb(59, 130, 246)`, `background-color: #fef08a`, `font-size: 20px`} {
		if !strings.Contains(got, expected) {
			t.Fatalf("format missing: %s", got)
		}
	}
	for _, bad := range []string{"position", "background-image", "url(", "expression", "9999px", "tracker.test"} {
		if strings.Contains(got, bad) {
			t.Fatalf("unsafe style survived: %s", got)
		}
	}
	if safeColor("rgb(999, 0, 0)") {
		t.Fatal("invalid color accepted")
	}
}
