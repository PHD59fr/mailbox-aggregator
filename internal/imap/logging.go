package imap

import (
	"context"

	"github.com/example/mail-aggregator/internal/maillog"
)

func (r Receiver) operation(ctx context.Context, cfg Configuration, name string) *maillog.Operation {
	return maillog.Start(ctx, r.Logger, "imap", "provider", name, "host", cfg.Host, "port", cfg.Port, "security", cfg.Security)
}
