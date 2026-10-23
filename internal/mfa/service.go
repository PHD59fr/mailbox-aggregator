// Package mfa adds an optional online second factor without changing vault keys.
package mfa

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/example/mail-aggregator/internal/filestore"
	"github.com/example/mail-aggregator/internal/session"
	"github.com/google/uuid"
)

var ErrVerification = errors.New("second factor verification failed")
var ErrUnavailable = errors.New("second factor data is unavailable")

type Record struct {
	Enabled     bool     `json:"enabled"`
	Generation  string   `json:"generation"`
	Secret      []byte   `json:"secret,omitempty"`
	LastCounter int64    `json:"last_counter"`
	Recovery    [][]byte `json:"recovery,omitempty"`
}
type envelope struct {
	Version    int    `json:"version"`
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
}
type login struct {
	Token      string
	User       uuid.UUID
	Generation string
	Created    time.Time
	Attempts   int
	Timer      *time.Timer
}
type enrollment struct {
	ID           string
	Secret       []byte
	User         uuid.UUID
	AuthRevision string
	LockRevision uint64
	Timer        *time.Timer
	Created      time.Time
}
type attempts struct {
	Count int
	Since time.Time
}
type Service struct {
	mu          sync.Mutex
	store       *filestore.Store
	sessions    *session.Manager
	box         cipher.AEAD
	logins      map[string]login
	enrollments map[string]enrollment
	limits      map[uuid.UUID]attempts
	now         func() time.Time
}

func NewService(store *filestore.Store, sessions *session.Manager, setup []byte) (*Service, error) {
	if len(setup) == 0 {
		return nil, ErrUnavailable
	}
	mac := hmac.New(sha256.New, setup)
	mac.Write([]byte("mailbox-aggregator/mfa/sealing/v1"))
	key := mac.Sum(nil)
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	box, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Service{store: store, sessions: sessions, box: box, logins: map[string]login{}, enrollments: map[string]enrollment{}, limits: map[uuid.UUID]attempts{}, now: time.Now}, nil
}
func (s *Service) open(user uuid.UUID, data []byte) (Record, error) {
	var sealed envelope
	if len(data) > 16384 || json.Unmarshal(data, &sealed) != nil || sealed.Version != 1 || len(sealed.Nonce) != s.box.NonceSize() {
		return Record{}, ErrUnavailable
	}
	plain, err := s.box.Open(nil, sealed.Nonce, sealed.Ciphertext, []byte("mfa/v1/"+user.String()))
	if err != nil {
		return Record{}, ErrUnavailable
	}
	defer clear(plain)
	var record Record
	if json.Unmarshal(plain, &record) != nil || len(record.Recovery) > 16 || record.Enabled && (len(record.Secret) != 20 || record.Generation == "") {
		return Record{}, ErrUnavailable
	}
	for _, hash := range record.Recovery {
		if len(hash) != sha256.Size {
			return Record{}, ErrUnavailable
		}
	}
	return record, nil
}
func (s *Service) load(user uuid.UUID) (Record, string, error) {
	data, revision, err := s.store.ReadUserFile(user, "mfa.json")
	if errors.Is(err, os.ErrNotExist) {
		return Record{LastCounter: -1}, "*", nil
	}
	if err != nil {
		return Record{}, "", ErrUnavailable
	}
	record, err := s.open(user, data)
	return record, revision, err
}
func (s *Service) save(user uuid.UUID, record Record, revision, authRevision string) error {
	plain, err := json.Marshal(record)
	if err != nil {
		return err
	}
	defer clear(plain)
	nonce := make([]byte, s.box.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	data, err := json.Marshal(envelope{Version: 1, Nonce: nonce, Ciphertext: s.box.Seal(nil, nonce, plain, []byte("mfa/v1/"+user.String()))})
	if err != nil {
		return err
	}
	_, err = s.store.WriteUserFile(user, "mfa.json", data, revision, authRevision)
	return err
}
func token() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
func (s *Service) dropLogin(id string) {
	if flow, ok := s.logins[id]; ok {
		if flow.Timer != nil {
			flow.Timer.Stop()
		}
		delete(s.logins, id)
	}
}
func (s *Service) dropEnrollment(id string) {
	if flow, ok := s.enrollments[id]; ok {
		if flow.Timer != nil {
			flow.Timer.Stop()
		}
		clear(flow.Secret)
		delete(s.enrollments, id)
	}
}
func (s *Service) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, value := range s.logins {
		s.sessions.Revoke(value.Token)
		s.dropLogin(id)
	}
	for id := range s.enrollments {
		s.dropEnrollment(id)
	}
}
func (s *Service) expire() {
	for user, limit := range s.limits {
		if s.now().Sub(limit.Since) >= time.Minute {
			delete(s.limits, user)
		}
	}
	for id, flow := range s.logins {
		if s.now().Sub(flow.Created) > 2*time.Minute {
			s.sessions.Revoke(flow.Token)
			s.dropLogin(id)
		}
	}
	for id, flow := range s.enrollments {
		if s.now().Sub(flow.Created) > 5*time.Minute {
			clear(flow.Secret)
			s.dropEnrollment(id)
		}
	}
}
func (s *Service) allow(user uuid.UUID) bool {
	limit := s.limits[user]
	if limit.Since.IsZero() || s.now().Sub(limit.Since) >= time.Minute {
		limit = attempts{Since: s.now()}
	}
	if limit.Count >= 10 || len(s.limits) >= 10000 && limit.Count == 0 {
		return false
	}
	limit.Count++
	s.limits[user] = limit
	return true
}
func (s *Service) Status(user uuid.UUID) (bool, int, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expire()
	record, _, err := s.load(user)
	defer clear(record.Secret)
	return record.Enabled, len(record.Recovery), record.Generation, err
}
func (s *Service) Binding(user uuid.UUID) (string, error) {
	enabled, _, generation, err := s.Status(user)
	if !enabled {
		generation = ""
	}
	return generation, err
}
func (s *Service) StartEnrollment(user uuid.UUID, sessionToken string, expected ...uint64) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expire()
	view, err := s.sessions.Peek(sessionToken)
	if err != nil || view.UserID != user || len(expected) > 0 && view.LockRevision != expected[0] {
		return "", "", ErrVerification
	}
	record, _, err := s.load(user)
	defer clear(record.Secret)
	if err != nil {
		return "", "", err
	}
	if record.Enabled {
		return "", "", ErrVerification
	}
	if len(s.enrollments) >= 1024 {
		return "", "", ErrUnavailable
	}
	secret := make([]byte, 20)
	if _, err = rand.Read(secret); err != nil {
		return "", "", err
	}
	enrollmentID, err := token()
	if err != nil {
		clear(secret)
		return "", "", err
	}
	// One enrollment per authenticated token, never persisted before confirmation.
	if old, ok := s.enrollments[sessionToken]; ok {
		clear(old.Secret)
		if old.Timer != nil {
			old.Timer.Stop()
		}
	}
	s.enrollments[sessionToken] = enrollment{ID: enrollmentID, Secret: secret, User: user, AuthRevision: view.AuthRevision, LockRevision: view.LockRevision, Created: s.now(), Timer: time.AfterFunc(5*time.Minute, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if current, ok := s.enrollments[sessionToken]; ok && current.ID == enrollmentID {
			s.dropEnrollment(sessionToken)
		}
	})}
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)
	uri := url.URL{Scheme: "otpauth", Host: "totp", Path: "Mailbox Aggregator:" + user.String()}
	query := url.Values{"secret": {encoded}, "issuer": {"Mailbox Aggregator"}, "algorithm": {"SHA1"}, "digits": {"6"}, "period": {"30"}}
	uri.RawQuery = query.Encode()
	return encoded, uri.String(), nil
}
func (s *Service) ConfirmEnrollment(user uuid.UUID, sessionToken, code string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expire()
	flow, ok := s.enrollments[sessionToken]
	if !ok || flow.User != user {
		return nil, ErrVerification
	}
	if !s.allow(user) {
		return nil, ErrVerification
	}
	view, err := s.sessions.Peek(sessionToken)
	if err != nil || view.UserID != user || view.AuthRevision != flow.AuthRevision || view.LockRevision != flow.LockRevision {
		return nil, ErrVerification
	}
	record, revision, err := s.load(user)
	defer clear(record.Secret)
	if err != nil {
		return nil, err
	}
	if record.Enabled {
		return nil, ErrVerification
	}
	record = Record{Secret: flow.Secret, LastCounter: -1}
	if !s.verify(&record, code, false) {
		return nil, ErrVerification
	}
	record.Enabled = true
	record.Generation, err = token()
	if err != nil {
		return nil, err
	}
	codes, hashes, err := recoveryCodes()
	if err != nil {
		return nil, err
	}
	record.Recovery = hashes
	if err = s.save(user, record, revision, view.AuthRevision); err != nil {
		return nil, err
	}
	if err = s.sessions.BindMFA(sessionToken, record.Generation); err != nil {
		return nil, err
	}
	s.sessions.RevokeOthers(user, sessionToken)
	s.dropEnrollment(sessionToken)
	clear(flow.Secret)
	return codes, nil
}
func (s *Service) BeginLogin(user uuid.UUID, sessionToken string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expire()
	view, sessionErr := s.sessions.Peek(sessionToken)
	if sessionErr != nil || view.UserID != user {
		return "", false, ErrVerification
	}
	record, _, err := s.load(user)
	defer clear(record.Secret)
	if err != nil {
		return "", false, err
	}
	if !record.Enabled {
		return "", false, nil
	}
	if len(s.logins) >= 1024 {
		return "", false, ErrUnavailable
	}
	id, err := token()
	if err != nil {
		return "", false, err
	}
	s.logins[id] = login{Token: sessionToken, User: user, Generation: record.Generation, Created: s.now(), Timer: time.AfterFunc(2*time.Minute, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if value, ok := s.logins[id]; ok {
			s.sessions.Revoke(value.Token)
		}
		s.dropLogin(id)
	})}
	return id, true, nil
}
func (s *Service) VerifyLogin(flowID, code string) (string, session.View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expire()
	flow, ok := s.logins[flowID]
	if !ok {
		return "", session.View{}, ErrVerification
	}
	if !s.allow(flow.User) {
		return "", session.View{}, ErrVerification
	}
	flow.Attempts++
	s.logins[flowID] = flow
	if flow.Attempts > 5 {
		s.sessions.Revoke(flow.Token)
		s.dropLogin(flowID)
		return "", session.View{}, ErrVerification
	}
	view, err := s.sessions.Peek(flow.Token)
	if err != nil || view.UserID != flow.User {
		return "", session.View{}, ErrVerification
	}
	record, revision, err := s.load(flow.User)
	defer clear(record.Secret)
	if err != nil {
		return "", session.View{}, err
	}
	if !record.Enabled || record.Generation != flow.Generation || !s.verify(&record, code, true) {
		return "", session.View{}, ErrVerification
	}
	if err = s.save(flow.User, record, revision, view.AuthRevision); err != nil {
		return "", session.View{}, ErrVerification
	}
	if err = s.sessions.BindMFA(flow.Token, record.Generation); err != nil {
		return "", session.View{}, ErrVerification
	}
	s.dropLogin(flowID)
	view, err = s.sessions.Peek(flow.Token)
	return flow.Token, view, err
}
func (s *Service) Change(user uuid.UUID, sessionToken, code string, disable bool, expected ...uint64) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expire()
	if !s.allow(user) {
		return nil, ErrVerification
	}
	view, err := s.sessions.Peek(sessionToken)
	if err != nil || view.UserID != user || len(expected) > 0 && view.LockRevision != expected[0] {
		return nil, ErrVerification
	}
	record, revision, err := s.load(user)
	defer clear(record.Secret)
	if err != nil {
		return nil, err
	}
	if !record.Enabled || view.MFARevision != record.Generation || !s.verify(&record, code, true) {
		return nil, ErrVerification
	}
	var codes []string
	if disable {
		record = Record{LastCounter: -1}
	} else {
		var hashes [][]byte
		codes, hashes, err = recoveryCodes()
		if err != nil {
			return nil, err
		}
		record.Recovery = hashes
	}
	if err = s.save(user, record, revision, view.AuthRevision); err != nil {
		return nil, err
	}
	if err = s.sessions.BindMFA(sessionToken, record.Generation); err != nil {
		return nil, err
	}
	s.sessions.RevokeOthers(user, sessionToken)
	return codes, nil
}
func (s *Service) verify(record *Record, code string, allowRecovery bool) bool {
	code = strings.TrimSpace(code)
	if len(code) == 6 {
		for _, r := range code {
			if r < '0' || r > '9' {
				return false
			}
		}
		counter := s.now().Unix() / 30
		for _, offset := range []int64{0, -1, 1} {
			candidate := counter + offset
			if candidate <= record.LastCounter || candidate < 0 {
				continue
			}
			if subtle.ConstantTimeCompare([]byte(TOTP(record.Secret, candidate, 6)), []byte(code)) == 1 {
				record.LastCounter = candidate
				return true
			}
		}
		return false
	}
	if !allowRecovery || len(code) > 128 {
		return false
	}
	hash := sha256.Sum256([]byte(normalizeRecovery(code)))
	for i, expected := range record.Recovery {
		if subtle.ConstantTimeCompare(hash[:], expected) == 1 {
			record.Recovery = append(record.Recovery[:i], record.Recovery[i+1:]...)
			return true
		}
	}
	return false
}
func normalizeRecovery(code string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
}
func recoveryCodes() ([]string, [][]byte, error) {
	var codes []string
	var hashes [][]byte
	for i := 0; i < 10; i++ {
		raw := make([]byte, 16)
		if _, err := rand.Read(raw); err != nil {
			return nil, nil, err
		}
		value := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
		clear(raw)
		hash := sha256.Sum256([]byte(value))
		hashes = append(hashes, hash[:])
		codes = append(codes, value[:6]+"-"+value[6:12]+"-"+value[12:18]+"-"+value[18:])
	}
	return codes, hashes, nil
}

// TOTP implements RFC 6238's HMAC-SHA1 variant; digits is 6 in the application.
func TOTP(secret []byte, counter int64, digits int) string {
	var value [8]byte
	binary.BigEndian.PutUint64(value[:], uint64(counter))
	mac := hmac.New(sha1.New, secret)
	mac.Write(value[:])
	digest := mac.Sum(nil)
	offset := digest[len(digest)-1] & 15
	number := binary.BigEndian.Uint32(digest[offset:offset+4]) & 0x7fffffff
	modulus := uint32(1000000)
	if digits == 8 {
		modulus = 100000000
	}
	return fmt.Sprintf("%0*d", digits, number%modulus)
}

// ValidateBackup decrypts only the MFA record, never vault or mail credentials.
func ValidateBackup(setup []byte, user uuid.UUID, data []byte) error {
	s, err := NewService(nil, nil, setup)
	if err != nil {
		return err
	}
	record, err := s.open(user, data)
	clear(record.Secret)
	return err
}
