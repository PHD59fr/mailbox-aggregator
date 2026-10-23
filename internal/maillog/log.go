// Package maillog records mail activity without protocol transcripts or message data.
package maillog

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log/slog"
	"net"
	"time"

	"github.com/example/mail-aggregator/internal/outbound"
)

type requestIDKey struct{}

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// Operation holds only operational metadata. Stage and Outcome must be fixed
// application labels, never provider responses, commands or user input.
type Operation struct {
	Stage, Outcome string
	ctx            context.Context
	logger         *slog.Logger
	started        time.Time
	attrs          []any
}

func Start(ctx context.Context, logger *slog.Logger, protocol, component, operation string, attrs ...any) *Operation {
	fields := []any{"protocol", protocol, "component", component, "operation", operation}
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		fields = append(fields, "request_id", id)
	}
	fields = append(fields, attrs...)
	return &Operation{ctx: ctx, logger: logger, started: time.Now(), attrs: fields}
}

func (o *Operation) Result(err *error) { o.End(*err) }

func (o *Operation) End(err error) {
	if o.logger == nil {
		return
	}
	status, level := "success", slog.LevelInfo
	attrs := append([]any(nil), o.attrs...)
	if err != nil {
		status, level = "failed", slog.LevelWarn
		kind := errorKind(err)
		if errors.Is(err, context.Canceled) || errors.Is(o.ctx.Err(), context.Canceled) {
			status, kind, level = "canceled", "canceled", slog.LevelDebug
		} else if errors.Is(o.ctx.Err(), context.DeadlineExceeded) {
			kind = "timeout"
		}
		attrs = append(attrs, "error_kind", kind)
	}
	if o.Outcome != "" {
		status = o.Outcome
		if o.Outcome == "unknown" {
			// Cancellation must not hide an ambiguous SMTP submission.
			level = slog.LevelWarn
		}
	}
	attrs = append(attrs, "status", status, "duration_ms", time.Since(o.started).Milliseconds())
	if o.Stage != "" {
		attrs = append(attrs, "stage", o.Stage)
	}
	o.logger.Log(o.ctx, level, "mail operation", attrs...)
}

// Error text can echo credentials, addresses, folder names or message content.
// Only classify known error types; never serialize the original error.
func errorKind(err error) string {
	var dns *net.DNSError
	var certificate *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var network net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, outbound.ErrUnsafeDestination), errors.Is(err, outbound.ErrPortNotAllowed):
		return "destination_denied"
	case errors.As(err, &dns):
		return "dns"
	case errors.As(err, &certificate), errors.As(err, &unknownAuthority), errors.As(err, &hostname):
		return "tls_certificate"
	case errors.As(err, &network):
		if network.Timeout() {
			return "timeout"
		}
		return "network"
	default:
		return "operation_failed"
	}
}
