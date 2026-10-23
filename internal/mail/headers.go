package mail

import (
	"errors"
	"net/textproto"
	"strings"
	"unicode"
	"unicode/utf8"
)

// CustomHeaders permits only advisory fields. Identity, transport, MIME and
// authentication headers always remain controlled by the application/provider.
func CustomHeaders(input map[string]string) (map[string]string, error) {
	result := map[string]string{}
	if len(input) > 3 {
		return nil, errors.New("at most three custom headers are allowed")
	}
	for key, value := range input {
		name := textproto.CanonicalMIMEHeaderKey(key)
		if _, exists := result[name]; exists {
			return nil, errors.New("duplicate custom header")
		}
		if !utf8.ValidString(value) || len(value) > 256 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return nil, errors.New("invalid custom header value")
		}
		switch name {
		case "Organization":
			if value == "" {
				return nil, errors.New("empty Organization header")
			}
		case "Importance":
			if value != "low" && value != "normal" && value != "high" {
				return nil, errors.New("invalid Importance header")
			}
		case "X-Priority":
			if value != "1" && value != "3" && value != "5" {
				return nil, errors.New("invalid X-Priority header")
			}
		default:
			return nil, errors.New("custom header is not permitted")
		}
		result[name] = value
	}
	return result, nil
}
