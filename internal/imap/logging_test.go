package imap

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	protocol "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/example/mail-aggregator/internal/maillog"
)

type rejectedSelectSession struct{ imapserver.Session }

func (s rejectedSelectSession) Select(string, *protocol.SelectOptions) (*protocol.SelectData, error) {
	return nil, &protocol.Error{Type: protocol.StatusResponseTypeNo, Text: "private-provider-response@example.test"}
}

func TestIMAPOperationLogsExcludeHeadersCredentialsAndProviderText(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "rejection"}[reject], func(t *testing.T) {
			receiver, cfg, user, _ := readerFixtureWithSession(t, func(session imapserver.Session) imapserver.Session {
				if reject {
					return rejectedSelectSession{Session: session}
				}
				return session
			})
			if _, err := user.Append("INBOX", literal{strings.NewReader(rawReaderMessage)}, &protocol.AppendOptions{}); err != nil {
				t.Fatal(err)
			}
			var logs bytes.Buffer
			receiver.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
			ctx := maillog.WithRequestID(context.Background(), "imap-request-123")
			_, err := receiver.List(ctx, cfg, 0, 0)
			if (err != nil) != reject {
				t.Fatalf("unexpected list result: %v", err)
			}
			for _, secret := range []string{"private-provider-response", "test-password", "team@example.test", "Équipe", "Un été tranquille"} {
				if strings.Contains(logs.String(), secret) {
					t.Fatalf("IMAP logs leaked private data: %s", logs.String())
				}
			}
			for _, field := range []string{`"protocol":"imap"`, `"operation":"connect"`, `"operation":"list"`, `"request_id":"imap-request-123"`} {
				if !strings.Contains(logs.String(), field) {
					t.Fatalf("IMAP log missing %s: %s", field, logs.String())
				}
			}
			if reject && !strings.Contains(logs.String(), `"status":"failed"`) {
				t.Fatalf("IMAP failure missing: %s", logs.String())
			}
		})
	}
}
