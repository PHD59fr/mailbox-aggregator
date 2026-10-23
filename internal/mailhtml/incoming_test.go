package mailhtml

import (
	"strings"
	"testing"
)

func TestBlockedContentHasSafeVisiblePlaceholders(t *testing.T) {
	raw := `<img src="https://tracker.test/hero" alt="&lt;script&gt;Offer&lt;/script&gt;"><iframe src="https://tracker.test/embed"></iframe><object data="https://tracker.test/object"></object>`
	private, _ := Incoming(raw, false)
	if !strings.Contains(private, "[Blocked image]") || strings.Count(private, "[Blocked external content]") != 2 || !strings.Contains(private, "&lt;script&gt;Offer&lt;/script&gt;") {
		t.Fatalf("missing/unsafe placeholders: %s", private)
	}
	for _, bad := range []string{"tracker.test", "<img", "<iframe", "<object", "<script>"} {
		if strings.Contains(private, bad) {
			t.Fatalf("blocked content survived: %s", private)
		}
	}
	if twice, _ := Incoming(private, false); twice != private {
		t.Fatalf("placeholder is not stable after document sanitizing: %s", twice)
	}
	allowed, _ := Incoming(raw, true)
	if !strings.Contains(allowed, `src="https://tracker.test/hero"`) || strings.Contains(allowed, "[Blocked image]") || strings.Contains(allowed, "tracker.test/embed") {
		t.Fatalf("consent rules changed: %s", allowed)
	}
}

func TestBlockedBackgroundImageKeepsTextAndHasPlaceholder(t *testing.T) {
	for _, external := range []bool{false, true} {
		body, _ := Incoming(`<table background="https://tracker.test/old"><tr><td style="color:blue;background-image:url(https://tracker.test/new)">Hello</td></tr></table>`, external)
		if strings.Count(body, "[Blocked background image]") != 2 || !strings.Contains(body, "Hello") || strings.Contains(body, "tracker.test") {
			t.Fatalf("invisible/unsafe blocked background: %s", body)
		}
		if twice, _ := Incoming(body, external); twice != body {
			t.Fatalf("unstable background placeholders: %s", twice)
		}
	}
}

func TestIncomingFormattingAndExternalConsent(t *testing.T) {
	raw := `<html><head><style>.headline {font-size: 24px; color: #123456; background-image: url(https://tracker.test/css)} @import url(https://tracker.test/import);</style></head><body><table width="600"><tr><td style="padding: 20px; text-align: center; background-color: #eeeeee"><p class="headline">Hello</p><img src="https://tracker.test/pixel" onerror="alert(1)"></td></tr></table><script>alert(1)</script><iframe src="https://tracker.test/frame"></iframe><form action="https://tracker.test/post">Form</form><a href="javascript:alert(1)">Bad</a></body></html>`
	private, styles := Incoming(raw, false)
	for _, expected := range []string{"<table", `padding: 20px`, `text-align: center`, `background-color: #eeeeee`, `class="headline"`} {
		if !strings.Contains(private, expected) {
			t.Fatalf("format lost: %q %s", expected, private)
		}
	}
	if !strings.Contains(styles, ".headline") || !strings.Contains(styles, "font-size: 24px") {
		t.Fatalf("safe stylesheet lost: %s", styles)
	}
	for _, bad := range []string{"tracker.test", "<img", "<script", "<iframe", "onerror", "javascript:", "<form"} {
		if strings.Contains(private, bad) {
			t.Fatalf("private content retained: %q %s", bad, private)
		}
	}
	if strings.Contains(styles, "url(") || strings.Contains(styles, "import") {
		t.Fatalf("unsafe stylesheet: %s", styles)
	}
	public, _ := Incoming(raw, true)
	if !strings.Contains(public, `src="https://tracker.test/pixel"`) || !strings.Contains(public, `referrerpolicy="no-referrer"`) {
		t.Fatalf("explicit image consent failed: %s", public)
	}
	for _, bad := range []string{"<script", "<iframe", "onerror", "javascript:", "<form"} {
		if strings.Contains(public, bad) {
			t.Fatalf("active content survived consent: %s", bad)
		}
	}
}

func TestIncomingRejectsStylesheetEscapeAndExternalCSS(t *testing.T) {
	for _, css := range []string{`</style><script>alert(1)</script>{color:red}`, `@font-face {font-family: evil;src:url(https://tracker.test/font)}`, `p{background-image:url(https://tracker.test);position:fixed;behavior:url(x);color:expression(alert(1))}`} {
		clean := sanitizeSheet(css)
		for _, bad := range []string{"</style", "script", "url(", "position", "expression", "font-face"} {
			if strings.Contains(clean, bad) {
				t.Fatalf("unsafe CSS: %s", clean)
			}
		}
	}
}
