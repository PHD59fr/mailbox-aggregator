package mailproxy

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

type logBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.String()
}

func TestSMTPLibraryErrorsAreLoggedWithoutRawArguments(t *testing.T) {
	var logs logBuffer
	service := &Service{Logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	logger := smtpErrorLogger{service: service}
	logger.Printf("panic serving %v: %v\n%s", "private-user@example.test", "private-password", "private-message-stack")
	logger.Println("private-provider-response")
	if strings.Contains(logs.String(), "private-") || !strings.Contains(logs.String(), `"level":"WARN"`) || !strings.Contains(logs.String(), `"operation":"server_error"`) {
		t.Fatal(logs.String())
	}
}
