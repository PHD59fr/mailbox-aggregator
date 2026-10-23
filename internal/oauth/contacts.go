package oauth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
)

const ContactsScope = "https://www.googleapis.com/auth/contacts.readonly"

type Contact struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}
type ContactPage struct {
	Contacts []Contact `json:"contacts"`
	NextPage string    `json:"next_page"`
}

func (s *Service) Contacts(ctx context.Context, owner, access, page string, revision uint64) (ContactPage, error) {
	result := ContactPage{Contacts: []Contact{}}
	if !s.config.GoogleContacts || access == "" || len(access) > 16384 || strings.ContainsAny(access, "\r\n\x00") || len(page) > 2048 {
		return result, ErrProvider
	}
	ctx, done, err := s.operation(ctx, owner, revision)
	if err != nil {
		return result, err
	}
	defer done()
	release, err := s.beginHTTP()
	if err != nil {
		return result, err
	}
	defer release()
	params := url.Values{"personFields": {"names,emailAddresses"}, "pageSize": {"50"}, "sources": {"READ_SOURCE_TYPE_CONTACT"}}
	if page != "" {
		params.Set("pageToken", page)
	}
	request, err := http.NewRequestWithContext(ctx, "GET", "https://people.googleapis.com/v1/people/me/connections?"+params.Encode(), nil)
	if err != nil {
		return result, ErrProvider
	}
	request.Header.Set("Authorization", "Bearer "+access)
	response, err := s.client.Do(request)
	if err != nil {
		return result, ErrProvider
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1048577))
	defer clear(data)
	if err != nil || len(data) > 1048576 || response.StatusCode != 200 {
		return result, ErrProvider
	}
	var decoded struct {
		Connections []struct {
			Names []struct {
				DisplayName string `json:"displayName"`
			} `json:"names"`
			Emails []struct {
				Value string `json:"value"`
			} `json:"emailAddresses"`
		} `json:"connections"`
		Next string `json:"nextPageToken"`
	}
	if json.Unmarshal(data, &decoded) != nil || len(decoded.Connections) > 50 || len(decoded.Next) > 2048 {
		return result, ErrProvider
	}
	seen := map[string]bool{}
	for _, person := range decoded.Connections {
		for _, value := range person.Emails {
			address, err := mail.ParseAddress(value.Value)
			name := ""
			if len(person.Names) > 0 {
				name = person.Names[0].DisplayName
			}
			if name == "" {
				name = value.Value
			}
			if err != nil || address.Address != value.Value || len(value.Value) > 254 || len(name) > 128 || strings.ContainsAny(name+value.Value, "\r\n\x00") || seen[strings.ToLower(value.Value)] {
				continue
			}
			if len(result.Contacts) >= 100 {
				break
			}
			seen[strings.ToLower(value.Value)] = true
			result.Contacts = append(result.Contacts, Contact{Name: name, Email: value.Value})
		}
	}
	if !s.active(owner, revision) {
		return ContactPage{}, ErrFlow
	}
	result.NextPage = decoded.Next
	return result, nil
}
