package mailhtml

import (
	"strings"
	"testing"
)

func TestOutgoingKeepsPassiveEmailPresentationAfterSanitizing(t *testing.T) {
	value := Outgoing(`<h1>Status</h1><p style="text-align:right"><span style="color:#3b82f6">Hello</span></p><table><tr><th>Item</th><td>Ready</td></tr></table><pre>code</pre><img src="https://tracker.test"><script>alert(1)</script>`)
	for _, safe := range []string{"font-family: Arial", "border-collapse: collapse", "text-align: right", "color: #3b82f6", "white-space: pre-wrap", "Status", "Ready"} {
		if !strings.Contains(value, safe) {
			t.Fatalf("email presentation lost %q: %s", safe, value)
		}
	}
	for _, unsafe := range []string{"<img", "<script", "tracker.test", "alert(1)"} {
		if strings.Contains(value, unsafe) {
			t.Fatalf("unsafe content survived: %s", value)
		}
	}
	if Outgoing("") != "" {
		t.Fatal("empty HTML created an unnecessary MIME alternative")
	}
}
