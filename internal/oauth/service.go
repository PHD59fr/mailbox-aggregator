// Package oauth brokers fixed-provider OAuth flows while the vault is unlocked.
// Refresh tokens are returned to the browser for vault encryption, never saved
// by this package. It provides no autonomous refresh or mail worker.
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/example/mail-aggregator/internal/operations"
	"github.com/example/mail-aggregator/internal/session"
)

var ErrFlow = errors.New("OAuth authorization expired, was cancelled or no longer belongs to an unlocked session")
var ErrProvider = errors.New("OAuth provider rejected authorization; reconnect the account if its grant expired")

type ProviderConfig struct{ ClientID, ClientSecret string }
type Config struct {
	GoogleContacts    bool
	PublicURL         string
	Google, Microsoft ProviderConfig
}
type Tokens struct {
	Provider     string `json:"provider"`
	Email        string `json:"email,omitempty"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int64  `json:"expires_in"`
	ExpiresAt    int64  `json:"expires_at"`
	Scope        string `json:"scope,omitempty"`
}
type provider struct {
	ProviderConfig
	Authorize, Token, Scope string
}
type flow struct {
	Owner                     string
	LockRevision              uint64
	Provider, Email, Verifier string
	Scope                     string
	Created                   time.Time
	Processing                bool
	Result                    *Tokens
	Failed                    bool
	Timer                     *time.Timer
}
type Service struct {
	mu         sync.Mutex
	config     Config
	sessions   *session.Manager
	operations *operations.Registry
	flows      map[string]*flow
	httpSlots  chan struct{}
	client     *http.Client
	now        func() time.Time
}

func New(config Config, sessions *session.Manager, registry *operations.Registry) *Service {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &Service{httpSlots: make(chan struct{}, 16), config: config, sessions: sessions, operations: registry, flows: map[string]*flow{}, client: &http.Client{Transport: transport, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, now: time.Now}
}
func (s *Service) provider(name string) (provider, bool) {
	var p provider
	switch name {
	case "google":
		p = provider{s.config.Google, "https://accounts.google.com/o/oauth2/v2/auth", "https://oauth2.googleapis.com/token", "https://mail.google.com/"}
	case "microsoft":
		p = provider{s.config.Microsoft, "https://login.microsoftonline.com/common/oauth2/v2.0/authorize", "https://login.microsoftonline.com/common/oauth2/v2.0/token", "offline_access https://outlook.office.com/IMAP.AccessAsUser.All https://outlook.office.com/SMTP.Send"}
	default:
		return p, false
	}
	return p, p.ClientID != "" && s.config.PublicURL != ""
}
func (s *Service) Callback(name string) string {
	return strings.TrimRight(s.config.PublicURL, "/") + "/api/v1/oauth/" + name + "/callback"
}
func (s *Service) Options() map[string]any {
	result := map[string]any{}
	for _, name := range []string{"google", "microsoft"} {
		_, available := s.provider(name)
		result[name] = map[string]any{"available": available, "callback_url": s.Callback(name)}
	}
	return result
}
func (s *Service) active(owner string, revision uint64) bool {
	view, err := s.sessions.Peek(owner)
	return err == nil && view.LockRevision == revision
}
func (s *Service) expire() {
	for id, value := range s.flows {
		if s.now().Sub(value.Created) > 5*time.Minute || !s.active(value.Owner, value.LockRevision) {
			s.drop(id)
		}
	}
}
func (s *Service) drop(id string) {
	if value := s.flows[id]; value != nil {
		if value.Timer != nil {
			value.Timer.Stop()
		}
		value.Owner = ""
		value.Verifier = ""
		value.Result = nil
		delete(s.flows, id)
	}
}
func (s *Service) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.flows {
		s.drop(id)
	}
	s.client.CloseIdleConnections()
}
func random() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
func (s *Service) Begin(owner, name, email string, revision uint64) (string, error) {
	return s.BeginWithContacts(owner, name, email, revision, false)
}
func (s *Service) BeginWithContacts(owner, name, email string, revision uint64, contacts bool) (string, error) {
	p, ok := s.provider(name)
	if !ok {
		return "", ErrProvider
	}
	if contacts {
		if name != "google" || !s.config.GoogleContacts {
			return "", ErrProvider
		}
		p.Scope += " " + ContactsScope
	}
	view, err := s.sessions.Peek(owner)
	if err != nil || view.LockRevision != revision {
		return "", ErrFlow
	}
	state, err := random()
	if err != nil {
		return "", err
	}
	verifier, err := random()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.expire()
	if len(s.flows) >= 1024 {
		s.mu.Unlock()
		return "", ErrFlow
	}
	for id, value := range s.flows {
		if value.Owner == owner {
			s.drop(id)
		}
	}
	s.flows[state] = &flow{Owner: owner, LockRevision: view.LockRevision, Provider: name, Email: email, Scope: p.Scope, Verifier: verifier, Created: s.now()}
	s.flows[state].Timer = time.AfterFunc(5*time.Minute, func() { s.mu.Lock(); defer s.mu.Unlock(); s.drop(state) })
	s.mu.Unlock()
	digest := sha256.Sum256([]byte(verifier))
	params := url.Values{"client_id": {p.ClientID}, "redirect_uri": {s.Callback(name)}, "response_type": {"code"}, "scope": {p.Scope}, "state": {state}, "code_challenge": {base64.RawURLEncoding.EncodeToString(digest[:])}, "code_challenge_method": {"S256"}, "login_hint": {email}}
	if name == "google" {
		params.Set("access_type", "offline")
		params.Set("prompt", "consent")
	} else {
		params.Set("prompt", "select_account")
	}
	return p.Authorize + "?" + params.Encode(), nil
}
func (s *Service) beginHTTP() (func(), error) {
	select {
	case s.httpSlots <- struct{}{}:
		return func() { <-s.httpSlots }, nil
	default:
		return nil, ErrProvider
	}
}
func (s *Service) request(ctx context.Context, endpoint string, params url.Values) (Tokens, error) {
	release, err := s.beginHTTP()
	if err != nil {
		return Tokens{}, err
	}
	defer release()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(params.Encode()))
	if err != nil {
		return Tokens{}, ErrProvider
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		return Tokens{}, ErrProvider
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	defer clear(data)
	if err != nil || len(data) > 65536 || response.StatusCode != 200 {
		return Tokens{}, ErrProvider
	}
	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		TokenType    string `json:"token_type"`
		Scope        string `json:"scope"`
	}
	if json.Unmarshal(data, &result) != nil || result.AccessToken == "" || len(result.AccessToken) > 16384 || len(result.RefreshToken) > 16384 || !strings.EqualFold(result.TokenType, "Bearer") || result.ExpiresIn < 30 || result.ExpiresIn > 86400 {
		return Tokens{}, ErrProvider
	}
	return Tokens{AccessToken: result.AccessToken, RefreshToken: result.RefreshToken, ExpiresIn: result.ExpiresIn, ExpiresAt: s.now().Unix() + result.ExpiresIn, Scope: result.Scope}, nil
}
func (s *Service) operation(ctx context.Context, owner string, revision uint64) (context.Context, func(), error) {
	view, err := s.sessions.Peek(owner)
	if err != nil || view.LockRevision != revision {
		return nil, nil, ErrFlow
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	untrack := func() {}
	if s.operations != nil {
		untrack = s.operations.Track(view.UserID, cancel)
	}
	return ctx, func() { cancel(); untrack() }, nil
}
func (s *Service) Complete(ctx context.Context, name, state, code string, cancelled bool) error {
	s.mu.Lock()
	s.expire()
	value, ok := s.flows[state]
	if !ok || value.Provider != name || value.Processing || value.Result != nil {
		s.mu.Unlock()
		return ErrFlow
	}
	value.Processing = true
	current := *value
	s.mu.Unlock()
	var result Tokens
	var err error
	if cancelled || code == "" || len(code) > 16384 {
		err = ErrProvider
	} else {
		p, _ := s.provider(name)
		var operation context.Context
		var done func()
		operation, done, err = s.operation(ctx, current.Owner, current.LockRevision)
		if err == nil {
			defer done()
			params := url.Values{"grant_type": {"authorization_code"}, "client_id": {p.ClientID}, "redirect_uri": {s.Callback(name)}, "code_verifier": {current.Verifier}, "code": {code}}
			if p.ClientSecret != "" {
				params.Set("client_secret", p.ClientSecret)
			}
			result, err = s.request(operation, p.Token, params)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.flows[state] != value || !s.active(current.Owner, current.LockRevision) {
		s.drop(state)
		return ErrFlow
	}
	value.Failed = err != nil
	value.Verifier = ""
	result.Provider = name
	if result.Scope == "" {
		result.Scope = current.Scope
	}
	result.Email = current.Email
	if err == nil {
		value.Result = &result
	}
	return nil
}
func (s *Service) Take(owner, state string) (Tokens, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expire()
	value, ok := s.flows[state]
	if !ok || value.Owner != owner || !s.active(owner, value.LockRevision) {
		return Tokens{}, ErrFlow
	}
	if value.Failed {
		s.drop(state)
		return Tokens{}, ErrProvider
	}
	if value.Result == nil {
		return Tokens{}, ErrFlow
	}
	result := *value.Result
	s.drop(state)
	return result, nil
}
func (s *Service) Refresh(ctx context.Context, owner, name, refresh string, revision uint64) (Tokens, error) {
	p, ok := s.provider(name)
	if !ok || refresh == "" || len(refresh) > 16384 {
		return Tokens{}, ErrProvider
	}
	view, err := s.sessions.Peek(owner)
	if err != nil {
		return Tokens{}, ErrFlow
	}
	ctx, done, err := s.operation(ctx, owner, revision)
	if err != nil {
		return Tokens{}, err
	}
	defer done()
	params := url.Values{"grant_type": {"refresh_token"}, "client_id": {p.ClientID}, "refresh_token": {refresh}}
	if p.ClientSecret != "" {
		params.Set("client_secret", p.ClientSecret)
	}
	result, err := s.request(ctx, p.Token, params)
	if err != nil {
		return Tokens{}, err
	}
	if !s.active(owner, view.LockRevision) {
		return Tokens{}, ErrFlow
	}
	result.Provider = name
	return result, nil
}
func (s *Service) Revoke(ctx context.Context, owner, name, refresh string, revision uint64) error {
	if name != "google" || refresh == "" || len(refresh) > 16384 {
		return ErrProvider
	}
	ctx, done, err := s.operation(ctx, owner, revision)
	if err != nil {
		return err
	}
	defer done()
	release, err := s.beginHTTP()
	if err != nil {
		return err
	}
	defer release()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/revoke", strings.NewReader(url.Values{"token": {refresh}}.Encode()))
	if err != nil {
		return ErrProvider
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := s.client.Do(request)
	if err != nil {
		return ErrProvider
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return ErrProvider
	}
	return nil
}
