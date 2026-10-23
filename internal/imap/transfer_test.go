package imap

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"testing"

	protocol "github.com/emersion/go-imap/v2"
	"github.com/example/mail-aggregator/internal/outbound"
)

func TestTransferBetweenProvidersConfirmsCopyBeforeTargetedDeletion(t *testing.T) {
	for _, move := range []bool{false, true} {
		t.Run(fmt.Sprint(move), func(t *testing.T) {
			r, source, user, audit := readerFixture(t, protocol.CapSet{protocol.CapIMAP4rev1: {}, protocol.CapUIDPlus: {}})
			audit.writes = true
			r2, target, _, audit2 := readerFixture(t, protocol.CapSet{protocol.CapIMAP4rev1: {}, protocol.CapUIDPlus: {}})
			audit2.writes = true
			connection, err := tls.Dial("tcp", outbound.HostPort(target.Host, target.Port), &tls.Config{RootCAs: r2.RootCAs, MinVersion: tls.VersionTLS12})
			if err != nil {
				t.Fatal(err)
			}
			r.RootCAs.AddCert(connection.ConnectionState().PeerCertificates[0])
			_ = connection.Close()
			r.Policy.AllowedPorts[target.Port] = struct{}{}
			for i := 0; i < 2; i++ {
				flags := []protocol.Flag{protocol.FlagFlagged}
				if i == 0 {
					flags = append(flags, protocol.FlagDeleted)
				}
				if _, err := user.Append("INBOX", literal{strings.NewReader(rawReaderMessage)}, &protocol.AppendOptions{Flags: flags}); err != nil {
					t.Fatal(err)
				}
			}
			page, err := r.List(context.Background(), source, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			if err := r.TransferBetween(context.Background(), source, target, 2, page.UIDValidity, move); err != nil {
				t.Fatal(err)
			}
			left, err := r.List(context.Background(), source, 0, 0)
			if err != nil || len(left.Messages) != map[bool]int{false: 2, true: 1}[move] {
				t.Fatalf("incorrect source count: %v %v", left, err)
			}
			copied, err := r.List(context.Background(), target, 0, 0)
			if err != nil || len(copied.Messages) != 1 {
				t.Fatalf("missing destination: %v", err)
			}
			data, err := r.Source(context.Background(), target, copied.Messages[0].UID, copied.UIDValidity)
			if err != nil || string(data) != rawReaderMessage {
				t.Fatal("message source changed during transfer")
			}
			if move && audit.expunges != 1 {
				t.Fatal("move did not use targeted expunge")
			}
		})
	}
}
func TestCrossAccountMoveCannotCopyWithoutSafeSourceDeletion(t *testing.T) {
	r, cfg, user, audit := readerFixture(t, protocol.CapSet{protocol.CapIMAP4rev1: {}})
	audit.writes = true
	_, _ = user.Append("INBOX", literal{strings.NewReader(rawReaderMessage)}, &protocol.AppendOptions{})
	page, _ := r.List(context.Background(), cfg, 0, 0)
	if err := r.TransferBetween(context.Background(), cfg, cfg, 1, page.UIDValidity, true); !errors.Is(err, ErrMoveUnsupported) {
		t.Fatalf("unsafe move: %v", err)
	}
	if audit.expunges != 0 || audit.deletedStores != 0 {
		t.Fatal("unsafe move changed the source")
	}
}
