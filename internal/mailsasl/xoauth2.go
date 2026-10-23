package mailsasl

import (
	"errors"
	"strings"
)

// XOAuth2 implements the provider SASL mechanism; it never falls back to using
// an access token as a normal account password.
type XOAuth2 struct {
	Username string
	Token    []byte
}

func (c *XOAuth2) Start() (string, []byte, error) {
	if c.Username == "" || len(c.Token) == 0 || len(c.Token) > 16384 || strings.ContainsAny(c.Username, "\x00\x01\r\n") {
		return "", nil, errors.New("invalid OAuth authentication material")
	}
	for _, b := range c.Token {
		if b < 33 || b > 126 {
			return "", nil, errors.New("invalid OAuth access token")
		}
	}
	return "XOAUTH2", []byte("user=" + c.Username + "\x01auth=Bearer " + string(c.Token) + "\x01\x01"), nil
}
func (*XOAuth2) Next([]byte) ([]byte, error) { return []byte{}, nil }
