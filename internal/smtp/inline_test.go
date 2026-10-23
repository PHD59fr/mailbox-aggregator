package smtp

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"io"
	"mime"
	"mime/multipart"
	stdmail "net/mail"
	"strings"
	"testing"
	"time"

	mailmodel "github.com/example/mail-aggregator/internal/mail"
)

func TestInlineImageUsesRelatedMIMEAndExactAttachmentBytes(t *testing.T) {
	var pngBytes bytes.Buffer
	png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 1, 1)))
	identity := mailmodel.Identity{Email: "sender@example.test"}
	draft := mailmodel.Draft{To: []mailmodel.Address{{Email: "recipient@example.test"}}, HTML: `<p>Hello</p><img src="cid:image@example.test" onerror="evil()"><img src="https://tracker.example/image"><img src="cid:missing">`, Headers: map[string]string{"Organization": "Example", "Importance": "high"}, Attachments: []mailmodel.Attachment{{ContentID: "image@example.test", Filename: "photo.png", ContentType: "image/png", Data: pngBytes.Bytes()}, {Filename: "note.txt", ContentType: "text/plain", Data: []byte("original attachment")}}}
	message, err := BuildMessage(identity, draft, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := stdmail.ReadMessage(bytes.NewReader(message.Data))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Header.Get("Organization") != "Example" || parsed.Header.Get("Importance") != "high" {
		t.Fatal("custom headers lost")
	}
	kind, params, _ := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	if kind != "multipart/mixed" {
		t.Fatal(kind)
	}
	mixed := multipart.NewReader(parsed.Body, params["boundary"])
	body, err := mixed.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	kind, params, _ = mime.ParseMediaType(body.Header.Get("Content-Type"))
	if kind != "multipart/related" {
		t.Fatal(kind)
	}
	related := multipart.NewReader(body, params["boundary"])
	alternative, err := related.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(alternative)
	if !bytes.Contains(data, []byte("cid:image@example.test")) || bytes.Contains(data, []byte("tracker.example")) || bytes.Contains(data, []byte("onerror")) || bytes.Contains(data, []byte("cid:missing")) {
		t.Fatalf("unsafe/lost image: %s", data)
	}
	inline, err := related.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	if inline.Header.Get("Content-ID") != "<image@example.test>" || !strings.HasPrefix(inline.Header.Get("Content-Disposition"), "inline") {
		t.Fatal(inline.Header)
	}
	decoded, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, inline))
	if err != nil || !bytes.Equal(decoded, pngBytes.Bytes()) {
		t.Fatal("inline bytes changed", err)
	}
	normal, err := mixed.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err = io.ReadAll(base64.NewDecoder(base64.StdEncoding, normal))
	if err != nil || string(decoded) != "original attachment" {
		t.Fatal("ordinary attachment changed", err)
	}
}
func TestCustomHeadersAndInlineImagesRejectForgedInput(t *testing.T) {
	identity := mailmodel.Identity{Email: "sender@example.test"}
	draft := mailmodel.Draft{To: []mailmodel.Address{{Email: "recipient@example.test"}}}
	for _, headers := range []map[string]string{{"From": "evil@example.test"}, {"Bcc": "hidden@example.test"}, {"Organization": "safe\r\nBcc: evil@example.test"}, {"Importance": "urgent"}, {"Importance": "high", "importance": "low"}} {
		draft.Headers = headers
		if _, err := BuildMessage(identity, draft, time.Now()); err == nil {
			t.Fatal("forged header accepted", headers)
		}
	}
	draft.Headers = nil
	draft.Attachments = []mailmodel.Attachment{{ContentID: "unsafe", Filename: "not-an-image.png", ContentType: "image/png", Data: []byte("<svg onload='evil()'>")}}
	if _, err := BuildMessage(identity, draft, time.Now()); err == nil {
		t.Fatal("non-image content accepted")
	}
}

func TestReplyToDoesNotChangeEnvelopeIdentityAndRejectsInjection(t *testing.T) {
	identity := mailmodel.Identity{Email: "sender@example.test", ReplyTo: "replies@example.test"}
	draft := mailmodel.Draft{To: []mailmodel.Address{{Email: "recipient@example.test"}}}
	built, err := BuildMessage(identity, draft, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := stdmail.ReadMessage(bytes.NewReader(built.Data))
	if err != nil {
		t.Fatal(err)
	}
	address, err := stdmail.ParseAddress(parsed.Header.Get("Reply-To"))
	if err != nil || address.Address != "replies@example.test" || built.EnvelopeFrom != "sender@example.test" {
		t.Fatal("Reply-To changed the envelope or was omitted", err)
	}
	identity.ReplyTo = "replies@example.test\r\nBcc: evil@example.test"
	if _, err = BuildMessage(identity, draft, time.Now()); err == nil {
		t.Fatal("Reply-To injection accepted")
	}
}
