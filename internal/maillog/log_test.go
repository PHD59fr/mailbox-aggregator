package maillog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"strings"
	"testing"

	"github.com/example/mail-aggregator/internal/outbound"
)

func TestLogsClassifyErrorsWithoutProviderText(t *testing.T) {
	const secret = "private-password-subject-recipient@example.test"
	for _, test := range []struct {
		name, kind, status, level string
		err                       error
	}{
		{name: "success", status: "success", level: "INFO"},
		{name: "provider rejection", err: errors.New(secret), kind: "operation_failed", status: "failed", level: "WARN"},
		{name: "DNS", err: &net.DNSError{Name: secret, Err: secret}, kind: "dns", status: "failed", level: "WARN"},
		{name: "timeout", err: context.DeadlineExceeded, kind: "timeout", status: "failed", level: "WARN"},
		{name: "blocked destination", err: outbound.ErrUnsafeDestination, kind: "destination_denied", status: "failed", level: "WARN"},
		{name: "canceled", err: context.Canceled, kind: "canceled", status: "canceled", level: "DEBUG"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug}))
			ctx := WithRequestID(context.Background(), "request-123")
			op := Start(ctx, logger, "smtp", "provider", "connect", "host", "smtp.example.test", "port", 465)
			op.Stage = "authenticate"
			op.End(test.err)
			if strings.Contains(output.String(), secret) {
				t.Fatalf("provider details leaked: %s", output.String())
			}
			var record map[string]any
			if err := json.Unmarshal(output.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			for field, want := range map[string]string{"status": test.status, "level": test.level, "protocol": "smtp", "component": "provider", "operation": "connect", "stage": "authenticate", "request_id": "request-123"} {
				if record[field] != want {
					t.Errorf("%s = %v, want %s", field, record[field], want)
				}
			}
			if test.kind != "" && record["error_kind"] != test.kind {
				t.Errorf("error_kind = %v, want %s", record["error_kind"], test.kind)
			}
			if duration, ok := record["duration_ms"].(float64); !ok || duration < 0 {
				t.Errorf("invalid duration: %v", record["duration_ms"])
			}
		})
	}
}

func TestCancellationDoesNotHideConfirmedSMTPAcceptance(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	ctx, cancel := context.WithCancel(context.Background())
	op := Start(ctx, logger, "smtp", "provider", "send")
	cancel()
	op.End(nil)
	if !strings.Contains(output.String(), `"status":"success"`) {
		t.Fatal(output.String())
	}
}

func TestCancellationDoesNotHideAmbiguousSMTPSubmission(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	ctx, cancel := context.WithCancel(context.Background())
	op := Start(ctx, logger, "smtp", "provider", "send")
	cancel()
	op.Outcome = "unknown"
	op.End(errors.New("unconfirmed SMTP result"))
	if !strings.Contains(output.String(), `"status":"unknown"`) || !strings.Contains(output.String(), `"level":"WARN"`) {
		t.Fatal(output.String())
	}
}
