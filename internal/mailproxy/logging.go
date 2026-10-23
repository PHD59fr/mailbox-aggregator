package mailproxy

import (
	"context"
	"errors"
	"net"

	"github.com/example/mail-aggregator/internal/maillog"
)

func (s *Service) gatewayOperation(protocol, operation string, conn net.Conn) *maillog.Operation {
	return maillog.Start(context.Background(), s.Logger, protocol, "gateway", operation, "client_addr", conn.RemoteAddr().String())
}

func (s *Service) providerOperation(ctx context.Context, protocol, operation string, target Target) *maillog.Operation {
	return maillog.Start(ctx, s.Logger, protocol, "gateway_upstream", operation, "host", target.Host, "port", target.Port, "security", target.Security)
}

// The SMTP library's formatted errors may include provider text or panic values.
// Keep its operational failures visible without forwarding those arguments.
type smtpErrorLogger struct{ service *Service }

func (l smtpErrorLogger) Printf(format string, args ...interface{}) {
	operation := "server_error"
	if format == "accept error: %s; retrying in %s" {
		operation = "accept"
	}
	op := maillog.Start(context.Background(), l.service.Logger, "smtp", "gateway", operation)
	for _, arg := range args {
		if err, ok := arg.(error); ok {
			op.End(err)
			return
		}
	}
	op.End(errors.New("SMTP server failure"))
}

func (l smtpErrorLogger) Println(args ...interface{}) { l.Printf("") }
