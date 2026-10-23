// Package directory contains read-only, explicitly requested contact connectors.
package directory

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/example/mail-aggregator/internal/outbound"
	ber "github.com/go-asn1-ber/asn1-ber"
	"github.com/go-ldap/ldap/v3"
)

var ErrDirectory = errors.New("directory request failed")

const maxBytes = 1 << 20

// No other application package consumes BER. Bound declared packet lengths as
// well as the total stream; a hostile LDAP server must not allocate gigabytes.
func init() { ber.MaxPacketLengthBytes = maxBytes }

type Client struct {
	Policy  outbound.Policy
	RootCAs *x509.CertPool
}
type Config struct {
	Kind       string `json:"kind"`
	URL        string `json:"url"`
	Host       string `json:"host"`
	Port       uint16 `json:"port"`
	Security   string `json:"security"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	BaseDN     string `json:"base_dn"`
	PageCookie string `json:"page_cookie"`
	Query      string `json:"query"`
}
type Contact struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}
type Card struct {
	Href string `json:"href"`
	ETag string `json:"etag"`
	Data string `json:"data"`
}
type Result struct {
	Contacts  []Contact `json:"contacts"`
	Cards     []Card    `json:"cards"`
	Truncated bool      `json:"truncated"`
	NextPage  string    `json:"next_page,omitempty"`
}

func (c Client) Read(ctx context.Context, cfg Config) (Result, error) {
	if len(cfg.Username) > 512 || cfg.Password == "" || len(cfg.Password) > 4096 || strings.ContainsAny(cfg.Username, "\r\n\x00") {
		return Result{}, ErrDirectory
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	switch cfg.Kind {
	case "carddav":
		return c.cardDAV(ctx, cfg)
	case "ldap":
		return c.ldap(ctx, cfg)
	default:
		return Result{}, ErrDirectory
	}
}
func (c Client) cardDAV(ctx context.Context, cfg Config) (Result, error) {
	u, err := url.Parse(cfg.URL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || len(cfg.URL) > 2048 {
		return Result{}, ErrDirectory
	}
	port := uint64(443)
	if u.Port() != "" {
		port, err = strconv.ParseUint(u.Port(), 10, 16)
		if err != nil || port == 0 {
			return Result{}, ErrDirectory
		}
	}
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: c.RootCAs}, DisableKeepAlives: true,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return c.Policy.DialContext(ctx, network, u.Hostname(), uint16(port))
		}}
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{Transport: transport, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	body := `<?xml version="1.0" encoding="UTF-8"?><C:addressbook-query xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:carddav"><D:prop><D:getetag/><C:address-data><C:prop name="VERSION"/><C:prop name="FN"/><C:prop name="N"/><C:prop name="EMAIL"/></C:address-data></D:prop><C:filter/><C:limit><C:nresults>50</C:nresults></C:limit></C:addressbook-query>`
	request, err := http.NewRequestWithContext(ctx, "REPORT", u.String(), strings.NewReader(body))
	if err != nil {
		return Result{}, ErrDirectory
	}
	request.Header.Set("Depth", "1")
	request.Header.Set("Content-Type", "application/xml; charset=utf-8")
	request.SetBasicAuth(cfg.Username, cfg.Password)
	response, err := httpClient.Do(request)
	if err != nil {
		return Result{}, ErrDirectory
	}
	defer response.Body.Close()
	if response.StatusCode != 207 {
		return Result{}, ErrDirectory
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil || len(data) > maxBytes {
		return Result{}, ErrDirectory
	}
	return parseCards(data)
}
func parseCards(data []byte) (Result, error) {
	result := Result{Contacts: []Contact{}, Cards: []Card{}}
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	depth := 0
	rootSeen := false
	var current *Card
	var field string
	var value strings.Builder
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Result{}, ErrDirectory
		}
		switch token := token.(type) {
		case xml.Directive:
			return Result{}, ErrDirectory
		case xml.StartElement:
			depth++
			if depth == 1 {
				if rootSeen || token.Name.Space != "DAV:" || token.Name.Local != "multistatus" {
					return Result{}, ErrDirectory
				}
				rootSeen = true
			}
			if depth > 24 {
				return Result{}, ErrDirectory
			}
			if token.Name.Space == "DAV:" && token.Name.Local == "response" {
				if current != nil {
					return Result{}, ErrDirectory
				}
				current = &Card{}
			}
			if current != nil && ((token.Name.Space == "DAV:" && (token.Name.Local == "href" || token.Name.Local == "getetag")) || (token.Name.Space == "urn:ietf:params:xml:ns:carddav" && token.Name.Local == "address-data")) {
				field = token.Name.Local
				value.Reset()
			}
		case xml.CharData:
			if field != "" {
				value.Write(token)
				if value.Len() > 65536 {
					return Result{}, ErrDirectory
				}
			}
		case xml.EndElement:
			if current != nil && field == token.Name.Local {
				switch field {
				case "href":
					current.Href = value.String()
				case "getetag":
					current.ETag = value.String()
				case "address-data":
					current.Data = value.String()
				}
				field = ""
			}
			if token.Name.Space == "DAV:" && token.Name.Local == "response" && current != nil {
				if current.Data != "" {
					if len(result.Cards) < 50 {
						result.Cards = append(result.Cards, *current)
					} else {
						result.Truncated = true
					}
				}
				current = nil
			}
			if token.Name.Local == "number-of-matches-within-limits" {
				result.Truncated = true
			}
			depth--
		}
	}
	if !rootSeen || depth != 0 || current != nil {
		return Result{}, ErrDirectory
	}
	return result, nil
}

type limitedConn struct {
	net.Conn
	remaining int64
}

func (c *limitedConn) Read(p []byte) (int, error) {
	if c.remaining <= 0 {
		return 0, ErrDirectory
	}
	if int64(len(p)) > c.remaining {
		p = p[:c.remaining]
	}
	n, err := c.Conn.Read(p)
	c.remaining -= int64(n)
	return n, err
}
func (c Client) ldap(ctx context.Context, cfg Config) (Result, error) {
	if cfg.BaseDN == "" || len(cfg.BaseDN) > 2048 || len(cfg.Query) < 2 || len(cfg.Query) > 256 || (cfg.Security != "tls" && cfg.Security != "starttls") {
		return Result{}, ErrDirectory
	}
	raw, err := c.Policy.DialContext(ctx, "tcp", cfg.Host, cfg.Port)
	if err != nil {
		return Result{}, ErrDirectory
	}
	defer raw.Close()
	stop := context.AfterFunc(ctx, func() { raw.Close() })
	defer stop()
	raw.SetDeadline(time.Now().Add(20 * time.Second))
	secure := &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12, RootCAs: c.RootCAs}
	var conn net.Conn = raw
	if cfg.Security == "tls" {
		tlsConn := tls.Client(raw, secure)
		if tlsConn.HandshakeContext(ctx) != nil {
			return Result{}, ErrDirectory
		}
		conn = tlsConn
	}
	directory := ldap.NewConn(&limitedConn{Conn: conn, remaining: maxBytes}, cfg.Security == "tls")
	directory.Start()
	defer directory.Close()
	directory.SetTimeout(10 * time.Second)
	if cfg.Security == "starttls" {
		if directory.StartTLS(secure) != nil {
			return Result{}, ErrDirectory
		}
	}
	if directory.Bind(cfg.Username, cfg.Password) != nil {
		return Result{}, ErrDirectory
	}
	cookie, err := base64.RawURLEncoding.DecodeString(cfg.PageCookie)
	if err != nil || len(cookie) > 1024 {
		return Result{}, ErrDirectory
	}
	paging := ldap.NewControlPaging(50)
	paging.SetCookie(cookie)
	query := ldap.EscapeFilter(cfg.Query)
	request := ldap.NewSearchRequest(cfg.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 50, 10, false, "(&(mail=*)(|(cn=*"+query+"*)(mail=*"+query+"*)))", []string{"cn", "mail"}, []ldap.Control{paging})
	request.EnforceSizeLimit = true
	found, err := directory.Search(request)
	truncated := ldap.IsErrorWithCode(err, ldap.LDAPResultSizeLimitExceeded)
	if err != nil && !truncated {
		return Result{}, ErrDirectory
	}
	result := Result{Contacts: []Contact{}, Cards: []Card{}, Truncated: truncated}
	if found == nil {
		return result, nil
	}
	if control := ldap.FindControl(found.Controls, ldap.ControlTypePaging); control != nil {
		if page, ok := control.(*ldap.ControlPaging); ok {
			if len(page.Cookie) > 1024 {
				return Result{}, ErrDirectory
			}
			result.NextPage = base64.RawURLEncoding.EncodeToString(page.Cookie)
		}
	}
	seen := map[string]bool{}
	for _, entry := range found.Entries {
		for _, email := range entry.GetAttributeValues("mail") {
			address, err := mail.ParseAddress(email)
			name := entry.GetAttributeValue("cn")
			if err != nil || address.Address != email || len(email) > 254 || len(name) > 128 || strings.ContainsAny(name+email, "\r\n\x00") || seen[strings.ToLower(email)] {
				continue
			}
			if len(result.Contacts) >= 50 {
				result.Truncated = true
				break
			}
			seen[strings.ToLower(email)] = true
			if name == "" {
				name = email
			}
			result.Contacts = append(result.Contacts, Contact{Name: name, Email: email})
		}
	}
	return result, nil
}
