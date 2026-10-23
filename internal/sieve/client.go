// Package sieve implements bounded ManageSieve requests over verified TLS.
package sieve

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/example/mail-aggregator/internal/outbound"
)

const MaxScript = 65536

var ErrProtocol = errors.New("ManageSieve request failed")
var ErrConflict = errors.New("remote script changed; reload before saving")
var ErrSyntax = errors.New("server rejected Sieve syntax or extensions")

type Config struct {
	Host     string `json:"host"`
	Port     uint16 `json:"port"`
	Security string `json:"security"`
	Username string `json:"username"`
	Password string `json:"password"`
}
type Request struct {
	Config
	Action         string `json:"action"`
	Name           string `json:"name"`
	Script         string `json:"script"`
	ExpectedHash   string `json:"expected_hash"`
	ExpectedActive string `json:"expected_active"`
}
type Script struct {
	Name   string `json:"name"`
	Active bool   `json:"active"`
}
type Result struct {
	Scripts    []Script `json:"scripts"`
	Extensions []string `json:"extensions"`
	Script     string   `json:"script,omitempty"`
	Hash       string   `json:"hash,omitempty"`
}
type Client struct {
	Policy  outbound.Policy
	RootCAs *x509.CertPool
}
type wire struct {
	conn      net.Conn
	reader    *bufio.Reader
	remaining int
	status    bool
}

func newWire(c net.Conn) *wire {
	return &wire{conn: c, reader: bufio.NewReaderSize(c, 65536), remaining: 262144}
}
func (w *wire) line() (string, error) {
	b, err := w.reader.ReadSlice('\n')
	w.remaining -= len(b)
	if err != nil || w.remaining < 0 || len(b) < 2 || b[len(b)-2] != '\r' {
		return "", ErrProtocol
	}
	return string(b[:len(b)-2]), nil
}

// A response may use quoted strings or bounded literals. Never interpret a
// literal's contents as a new command or status response.
func (w *wire) row() ([]string, error) {
	line, err := w.line()
	if err != nil {
		return nil, err
	}
	line = strings.TrimLeft(line, " \t")
	w.status = len(line) > 0 && line[0] != '"' && line[0] != '{'
	var tokens []string
	for len(line) > 0 {
		line = strings.TrimLeft(line, " \t")
		if line == "" {
			break
		}
		if len(tokens) >= 64 {
			return nil, ErrProtocol
		}
		switch line[0] {
		case '"':
			var value strings.Builder
			closed := false
			i := 1
			for i < len(line) {
				ch := line[i]
				i++
				if ch == '"' {
					closed = true
					break
				}
				if ch == '\\' {
					if i >= len(line) || (line[i] != '\\' && line[i] != '"') {
						return nil, ErrProtocol
					}
					ch = line[i]
					i++
				}
				value.WriteByte(ch)
			}
			if !closed {
				return nil, ErrProtocol
			}
			tokens = append(tokens, value.String())
			line = line[i:]
		case '{':
			if !strings.HasSuffix(line, "}") {
				return nil, ErrProtocol
			}
			n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(line, "{"), "}"))
			if err != nil || n < 0 || n > MaxScript || n > w.remaining {
				return nil, ErrProtocol
			}
			data := make([]byte, n)
			if _, err = io.ReadFull(w.reader, data); err != nil {
				return nil, ErrProtocol
			}
			w.remaining -= n
			tokens = append(tokens, string(data))
			line, err = w.line()
			if err != nil {
				return nil, err
			}
		default:
			i := strings.IndexAny(line, " \t")
			if i < 0 {
				i = len(line)
			}
			tokens = append(tokens, line[:i])
			line = line[i:]
		}
	}
	return tokens, nil
}
func (w *wire) response() ([][]string, error) {
	var rows [][]string
	for {
		row, err := w.row()
		if err != nil {
			return nil, err
		}
		if len(row) == 0 {
			continue
		}
		switch {
		case w.status && strings.EqualFold(row[0], "OK"):
			return rows, nil
		case w.status && (strings.EqualFold(row[0], "NO") || strings.EqualFold(row[0], "BYE")):
			return nil, ErrProtocol
		}

		rows = append(rows, row)
		if len(rows) > 128 {
			return nil, ErrProtocol
		}
	}
}
func (w *wire) command(command string) ([][]string, error) {
	if _, err := io.WriteString(w.conn, command+"\r\n"); err != nil {
		return nil, ErrProtocol
	}
	return w.response()
}
func quote(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
}
func validName(name string) bool {
	return name != "" && len(name) <= 128 && !strings.ContainsAny(name, "\r\n\x00")
}
func hash(script string) string {
	sum := sha256.Sum256([]byte(script))
	return hex.EncodeToString(sum[:])
}
func scripts(rows [][]string) ([]Script, error) {
	result := []Script{}
	seen := map[string]bool{}
	for _, row := range rows {
		if len(result) >= 64 || len(row) < 1 || len(row) > 2 || !validName(row[0]) || seen[row[0]] || (len(row) == 2 && strings.ToUpper(row[1]) != "ACTIVE") {
			return nil, ErrProtocol
		}
		seen[row[0]] = true
		result = append(result, Script{Name: row[0], Active: len(row) == 2})
	}
	return result, nil
}
func (w *wire) get(name string) (string, error) {
	rows, err := w.command("GETSCRIPT " + quote(name))
	if err != nil || len(rows) != 1 || len(rows[0]) != 1 {
		return "", ErrProtocol
	}
	return rows[0][0], nil
}
func (w *wire) literal(command, script string) error {
	_, err := w.command(command + " {" + strconv.Itoa(len(script)) + "+}\r\n" + script)
	return err
}
func (c Client) Do(ctx context.Context, request Request) (Result, error) {
	result := Result{Scripts: []Script{}, Extensions: []string{}}
	if request.Username == "" || len(request.Username) > 512 || request.Password == "" || len(request.Password) > 4096 || strings.ContainsAny(request.Username+request.Password, "\x00") || len(request.Script) > MaxScript {
		return result, ErrProtocol
	}
	if request.Action != "list" && request.Action != "check" && request.Action != "deactivate" && !validName(request.Name) {
		return result, ErrProtocol
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	raw, err := c.Policy.DialContext(ctx, "tcp", request.Host, request.Port)
	if err != nil {
		return result, ErrProtocol
	}
	defer raw.Close()
	raw.SetDeadline(time.Now().Add(20 * time.Second))
	stop := context.AfterFunc(ctx, func() { raw.Close() })
	defer stop()
	secure := &tls.Config{ServerName: request.Host, MinVersion: tls.VersionTLS12, RootCAs: c.RootCAs}
	var conn net.Conn = raw
	if request.Security == "tls" {
		tlsConn := tls.Client(raw, secure)
		if tlsConn.HandshakeContext(ctx) != nil {
			return result, ErrProtocol
		}
		conn = tlsConn
	} else if request.Security != "starttls" {
		return result, ErrProtocol
	}
	w := newWire(conn)
	capabilities, err := w.response()
	if err != nil {
		return result, ErrProtocol
	}
	has := func(rows [][]string, key, value string) bool {
		for _, row := range rows {
			if strings.EqualFold(row[0], key) {
				if value == "" {
					return true
				}
				if len(row) > 1 {
					for _, item := range strings.Fields(row[1]) {
						if strings.EqualFold(item, value) {
							return true
						}
					}
				}
			}
		}
		return false
	}
	if request.Security == "starttls" {
		if !has(capabilities, "STARTTLS", "") {
			return result, ErrProtocol
		}
		if _, err = w.command("STARTTLS"); err != nil {
			return result, ErrProtocol
		}
		tlsConn := tls.Client(raw, secure)
		if tlsConn.HandshakeContext(ctx) != nil {
			return result, ErrProtocol
		}
		w = newWire(tlsConn)
		// RFC 5804 requires a fresh greeting after the TLS handshake.
		capabilities, err = w.response()
		if err != nil {
			return result, ErrProtocol
		}
	}
	if !has(capabilities, "SASL", "PLAIN") {
		return result, ErrProtocol
	}
	for _, row := range capabilities {
		if strings.EqualFold(row[0], "SIEVE") && len(row) > 1 {
			result.Extensions = strings.Fields(row[1])
		}
	}
	credentials := []byte("\x00" + request.Username + "\x00" + request.Password)
	encoded := base64.StdEncoding.EncodeToString(credentials)
	clear(credentials)
	if _, err = w.command(`AUTHENTICATE "PLAIN" ` + quote(encoded)); err != nil {
		return result, ErrProtocol
	}
	rows, err := w.command("LISTSCRIPTS")
	if err != nil {
		return result, ErrProtocol
	}
	result.Scripts, err = scripts(rows)
	if err != nil {
		return result, err
	}
	var exists bool
	var active string
	for _, script := range result.Scripts {
		if script.Name == request.Name {
			exists = true
		}
		if script.Active {
			active = script.Name
		}
	}
	switch request.Action {
	case "list":
		return result, nil
	case "get":
		if !exists {
			return result, ErrConflict
		}
		result.Script, err = w.get(request.Name)
		result.Hash = hash(result.Script)
		return result, err
	case "check":
		if w.literal("CHECKSCRIPT", request.Script) != nil {
			return result, ErrSyntax
		}
		return result, nil
	case "put", "activate", "delete":
		if request.ExpectedActive != active {
			return result, ErrConflict
		}
		if exists {
			previous, e := w.get(request.Name)
			if e != nil {
				return result, e
			}
			if request.ExpectedHash == "" || hash(previous) != request.ExpectedHash {
				return result, ErrConflict
			}
		} else if request.ExpectedHash != "" || request.Action != "put" {
			return result, ErrConflict
		}
		if request.Action == "put" {
			if w.literal("CHECKSCRIPT", request.Script) != nil {
				return result, ErrSyntax
			}
			if w.literal("PUTSCRIPT "+quote(request.Name), request.Script) != nil {
				return result, ErrProtocol
			}
			result.Hash = hash(request.Script)
		} else if request.Action == "activate" {
			_, err = w.command("SETACTIVE " + quote(request.Name))
		} else {
			if active == request.Name {
				return result, ErrConflict
			}
			_, err = w.command("DELETESCRIPT " + quote(request.Name))
		}
	case "deactivate":
		if active == "" || request.ExpectedActive != active {
			return result, ErrConflict
		}
		_, err = w.command(`SETACTIVE ""`)
	default:
		return result, ErrProtocol
	}
	return result, err
}
