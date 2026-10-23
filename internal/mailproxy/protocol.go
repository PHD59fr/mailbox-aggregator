package mailproxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

const maxLine = 8192

func line(reader *bufio.Reader) (string, error) {
	value, err := reader.ReadSlice('\n')
	if err != nil || len(value) > maxLine || len(value) < 2 || value[len(value)-2] != '\r' {
		return "", errors.New("invalid protocol line")
	}
	return string(value[:len(value)-2]), nil
}
func reply(writer io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(writer, format+"\r\n", args...)
	return err
}

func plain(value string) (string, string, error) {
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(decoded) > 4096 {
		return "", "", ErrAuthentication
	}
	defer clear(decoded)
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) != 3 || parts[1] == "" || (parts[0] != "" && parts[0] != parts[1]) {
		return "", "", ErrAuthentication
	}
	return parts[1], parts[2], nil
}

func (s *Service) handleIMAP(raw, conn net.Conn) {
	reader := bufio.NewReaderSize(conn, maxLine)
	if reply(conn, "* OK [CAPABILITY IMAP4rev1 AUTH=PLAIN SASL-IR] Mailbox Aggregator ready") != nil {
		return
	}
	for n := 0; n < 20; n++ {
		command, err := line(reader)
		if err != nil {
			return
		}
		tag, rest, ok := strings.Cut(command, " ")
		if !ok || !validTag(tag) {
			return
		}
		verb, args, _ := strings.Cut(strings.TrimSpace(rest), " ")
		var login, password string
		switch strings.ToUpper(verb) {
		case "CAPABILITY":
			_ = reply(conn, "* CAPABILITY IMAP4rev1 AUTH=PLAIN SASL-IR")
			_ = reply(conn, "%s OK CAPABILITY completed", tag)
			continue
		case "NOOP":
			_ = reply(conn, "%s OK NOOP completed", tag)
			continue
		case "ID":
			_ = reply(conn, "* ID NIL")
			_ = reply(conn, "%s OK ID completed", tag)
			continue
		case "LOGOUT":
			_ = reply(conn, "* BYE Logging out")
			_ = reply(conn, "%s OK LOGOUT completed", tag)
			return
		case "LOGIN":
			login, password, err = loginArgs(args, reader, conn)
		case "AUTHENTICATE":
			fields := strings.Fields(args)
			if len(fields) < 1 || len(fields) > 2 || !strings.EqualFold(fields[0], "PLAIN") {
				_ = reply(conn, "%s NO Unsupported authentication mechanism", tag)
				continue
			}
			var response string
			if len(fields) == 2 {
				response = fields[1]
			} else {
				if reply(conn, "+ ") != nil {
					return
				}
				response, err = line(reader)
			}
			if err == nil {
				login, password, err = plain(response)
			}
		default:
			_ = reply(conn, "%s BAD Authenticate first", tag)
			continue
		}
		if err != nil {
			s.gatewayOperation("imap", "authenticate", raw).End(ErrAuthentication)
			_ = reply(conn, "%s NO [AUTHENTICATIONFAILED] Authentication failed", tag)
			return
		}
		authentication := s.gatewayOperation("imap", "authenticate", raw)
		id, target, revision, err := s.authenticate(raw, login, password, "imap")
		authentication.End(err)
		password = ""
		if err != nil {
			_ = reply(conn, "%s NO [AUTHENTICATIONFAILED] Authentication failed or temporarily limited", tag)
			return
		}
		accepted := false
		err = s.relay(raw, conn, reader, id, revision, "imap", target, func() error { accepted = true; return reply(conn, "%s OK Authentication completed", tag) })
		if err != nil && !accepted {
			_ = reply(conn, "%s NO [UNAVAILABLE] Mailbox unavailable; check proxy settings", tag)
		}
		return
	}
}

func validTag(tag string) bool {
	if tag == "" || len(tag) > 64 {
		return false
	}
	for i := 0; i < len(tag); i++ {
		if tag[i] < 0x21 || tag[i] > 0x7e || strings.ContainsRune("()+{*%\\\"]", rune(tag[i])) {
			return false
		}
	}
	return true
}

// Parse LOGIN astrings, including synchronizing literals used for credentials
// with spaces, quotes, Unicode or other characters outside the atom grammar.
func loginArgs(input string, reader *bufio.Reader, writer io.Writer) (string, string, error) {
	var values []string
	for len(values) < 2 {
		input = strings.TrimLeft(input, " ")
		if input == "" {
			return "", "", ErrAuthentication
		}
		var value string
		if input[0] == '"' {
			end := false
			for i := 1; i < len(input); i++ {
				if input[i] == '"' {
					input = input[i+1:]
					end = true
					break
				}
				if input[i] == '\\' {
					i++
					if i >= len(input) || (input[i] != '"' && input[i] != '\\') {
						return "", "", ErrAuthentication
					}
				}
				value += input[i : i+1]
			}
			if !end {
				return "", "", ErrAuthentication
			}
		} else if input[0] == '{' {
			if !strings.HasSuffix(input, "}") {
				return "", "", ErrAuthentication
			}
			lengthText := strings.TrimSuffix(strings.TrimSuffix(input[1:], "}"), "+")
			length, err := strconv.Atoi(lengthText)
			if err != nil || length < 0 || length > 4096 {
				return "", "", ErrAuthentication
			}
			if !strings.HasSuffix(input, "+}") {
				if reply(writer, "+ Ready for literal") != nil {
					return "", "", ErrAuthentication
				}
			}
			data := make([]byte, length)
			if _, err = io.ReadFull(reader, data); err != nil {
				return "", "", err
			}
			value = string(data)
			clear(data)
			input, err = line(reader)
			if err != nil {
				return "", "", err
			}
		} else {
			if end := strings.IndexByte(input, ' '); end >= 0 {
				value, input = input[:end], input[end:]
			} else {
				value, input = input, ""
			}
			if strings.ContainsAny(value, "(){%*\\\"\x00\r\n\t") {
				return "", "", ErrAuthentication
			}
		}
		if strings.ContainsAny(value, "\x00\r\n") {
			return "", "", ErrAuthentication
		}
		values = append(values, value)
		if len(values) == 1 && input != "" && input[0] != ' ' {
			return "", "", ErrAuthentication
		}
	}
	if strings.TrimSpace(input) != "" {
		return "", "", ErrAuthentication
	}
	return values[0], values[1], nil
}

func (s *Service) connectIMAP(ctx context.Context, target Target) (connection upstream, resultErr error) {
	op := s.providerOperation(ctx, "imap", "connect", target)
	op.Stage = "dial"
	defer op.Result(&resultErr)
	raw, err := s.Policy.DialContext(ctx, "tcp", target.Host, target.Port)
	if err != nil {
		return upstream{}, err
	}
	stopDial := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer stopDial()
	success := false
	defer func() {
		if !success {
			_ = raw.Close()
		}
	}()
	deadline, _ := ctx.Deadline()
	_ = raw.SetDeadline(deadline)
	var conn net.Conn = raw
	op.Stage = "tls"
	if target.Security == "tls" {
		secure := tls.Client(raw, s.tlsConfig(target.Host))
		if err = secure.HandshakeContext(ctx); err != nil {
			return upstream{}, err
		}
		conn = secure
	}
	reader := bufio.NewReaderSize(conn, maxLine)
	op.Stage = "greeting"
	greeting, err := line(reader)
	if err != nil || !strings.HasPrefix(strings.ToUpper(greeting), "* OK") {
		return upstream{}, errors.New("invalid IMAP greeting")
	}
	if target.Security == "starttls" {
		op.Stage = "tls"
		if reply(conn, "px1 STARTTLS") != nil {
			return upstream{}, io.ErrClosedPipe
		}
		if err = imapResult(reader, "px1"); err != nil {
			return upstream{}, err
		}
		if reader.Buffered() != 0 {
			return upstream{}, errors.New("unexpected data before STARTTLS")
		}
		secure := tls.Client(raw, s.tlsConfig(target.Host))
		if err = secure.HandshakeContext(ctx); err != nil {
			return upstream{}, err
		}
		conn = secure
		reader = bufio.NewReaderSize(conn, maxLine)
	}
	// Synchronizing literals avoid quoting ambiguities and command injection.
	op.Stage = "authenticate"
	if reply(conn, "px2 LOGIN {%d}", len(target.Username)) != nil {
		return upstream{}, io.ErrClosedPipe
	}
	if err = imapContinuation(reader); err != nil {
		return upstream{}, err
	}
	if reply(conn, "%s {%d}", target.Username, len(target.Password)) != nil {
		return upstream{}, io.ErrClosedPipe
	}
	if err = imapContinuation(reader); err != nil {
		return upstream{}, err
	}
	if reply(conn, "%s", target.Password) != nil {
		return upstream{}, io.ErrClosedPipe
	}
	if err = imapResult(reader, "px2"); err != nil {
		return upstream{}, err
	}
	_ = conn.SetDeadline(time.Time{})
	success = true
	op.Stage = "ready"
	return upstream{reader: reader, writer: conn, close: conn.Close, conn: raw}, nil
}

func imapResult(reader *bufio.Reader, tag string) error {
	for n := 0; n < 100; n++ {
		value, err := line(reader)
		if err != nil {
			return err
		}
		fields := strings.Fields(value)
		if len(fields) >= 2 && fields[0] == tag {
			if strings.EqualFold(fields[1], "OK") {
				return nil
			}
			return errors.New("IMAP command rejected")
		}
		if strings.HasPrefix(strings.ToUpper(value), "* BYE") {
			break
		}
	}
	return errors.New("invalid IMAP response")
}
func imapContinuation(reader *bufio.Reader) error {
	for n := 0; n < 100; n++ {
		value, err := line(reader)
		if err != nil {
			return err
		}
		if strings.HasPrefix(value, "+") {
			return nil
		}
		if !strings.HasPrefix(value, "* ") || strings.HasPrefix(strings.ToUpper(value), "* BYE") {
			break
		}
	}
	return errors.New("IMAP login rejected")
}
