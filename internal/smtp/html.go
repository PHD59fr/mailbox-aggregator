package smtp

import "github.com/example/mail-aggregator/internal/mailhtml"

func sanitizeHTML(value string) string { return mailhtml.Outgoing(value) }
