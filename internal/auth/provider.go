package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/bytemare/opaque"
	"github.com/example/mail-aggregator/internal/filestore"
	"github.com/example/mail-aggregator/internal/mfa"
	"github.com/example/mail-aggregator/internal/session"
	"github.com/example/mail-aggregator/internal/user"
	"github.com/example/mail-aggregator/internal/vault"
	"github.com/google/uuid"
)

var ErrUnavailable = errors.New("OPAQUE authentication provider is unavailable")

type Availability interface{ Available() bool }

// FlowProvider exposes app-level RFC 9807 exchanges. Wire messages are opaque
// protocol-library values serialized as base64url, not application-built PAKEs.
type FlowProvider interface {
	Available() bool
	RegistrationStart(login string, request string) (flowID string, response string, userID uuid.UUID, err error)
	RegistrationFinish(flowID string, payload RegistrationPayload) (Result, error)
	LoginStart(login string, request string, reauthToken string, expectedUser uuid.UUID) (flowID string, response string, err error)
	LoginFinish(flowID string, request string) (Result, error)
}

type UnavailableProvider struct{ Reason string }

func (UnavailableProvider) Available() bool { return false }

// Service implements RFC 9807 authentication with the maintained bytemare
// implementation. OPAQUE protocol values are transported as unpadded base64url.
type Service struct {
	server       *opaque.Server
	conf         *opaque.Configuration
	index        *filestore.UserIndex
	store        *filestore.Store
	users        *user.Service
	sessions     *session.Manager
	mu           sync.Mutex
	flows        map[string]flow
	grants       map[string]time.Time
	secondFactor *mfa.Service
}

type flow struct {
	login          string
	userID         uuid.UUID
	registration   bool
	passwordChange bool
	authRevision   string
	revisions      map[string]string
	rotate         bool
	keyVersion     uint32
	mac            []byte
	created        time.Time
	reauthToken    string
}

type RegistrationPayload struct {
	Record string                     `json:"registration_record"`
	Vault  json.RawMessage            `json:"vault_metadata"`
	Files  map[string]json.RawMessage `json:"files"`
}

type Result struct {
	Token  string
	View   session.View
	UserID uuid.UUID
}

func NewService(setup string, index *filestore.UserIndex, store *filestore.Store, users *user.Service, sessions *session.Manager) (*Service, error) {
	if setup == "" {
		return nil, ErrUnavailable
	}
	conf := opaque.DefaultConfiguration()
	keyMaterial, err := conf.DecodeServerKeyMaterialHex(setup)
	if err != nil {
		return nil, errors.New("invalid OPAQUE server setup")
	}
	server, err := conf.Server()
	if err != nil {
		return nil, err
	}
	if err = server.SetKeyMaterial(keyMaterial); err != nil {
		return nil, errors.New("invalid OPAQUE server key material")
	}
	secondFactor, err := mfa.NewService(store, sessions, []byte(keyMaterial.Hex()))
	if err != nil {
		return nil, err
	}
	return &Service{server: server, conf: conf, index: index, store: store, users: users, sessions: sessions, flows: make(map[string]flow), grants: make(map[string]time.Time), secondFactor: secondFactor}, nil
}

func (s *Service) SecondFactor() *mfa.Service { return s.secondFactor }
func (s *Service) ConsumeReauthentication(token string, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireFlowsLocked()
	granted, ok := s.grants[token]
	delete(s.grants, token)
	view, err := s.sessions.Peek(token)
	if !ok || time.Since(granted) > 5*time.Minute || err != nil || view.UserID != id || !s.users.IsActive(id) {
		return errors.New("fresh explicit reauthentication required")
	}
	return nil
}

func (s *Service) Available() bool { return s != nil && s.server != nil }

func (s *Service) RegistrationStart(login string, request string) (string, string, uuid.UUID, error) {
	normalized, err := filestore.NormalizeLogin(login)
	if err != nil {
		return "", "", uuid.Nil, err
	}
	if _, ok := s.index.Lookup(normalized); ok {
		return "", "", uuid.Nil, filestore.ErrConflict
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireFlowsLocked()
	if len(s.flows) >= 1024 {
		return "", "", uuid.Nil, errors.New("too many authentication flows")
	}
	id, err := uuid.NewRandom()
	if err != nil {
		return "", "", uuid.Nil, err
	}
	raw, err := decodeWire(request, 1024)
	if err != nil {
		return "", "", uuid.Nil, err
	}
	regReq, err := s.server.Deserialize.RegistrationRequest(raw)
	if err != nil {
		return "", "", uuid.Nil, err
	}
	resp, err := s.server.RegistrationResponse(regReq, []byte(normalized), nil)
	if err != nil {
		return "", "", uuid.Nil, err
	}
	flowID, err := newFlowID()
	if err != nil {
		return "", "", uuid.Nil, err
	}
	// Only a valid replacement request may discard an abandoned flow.
	for id, f := range s.flows {
		if f.registration && f.login == normalized {
			delete(s.flows, id)
			break
		}
	}
	s.flows[flowID] = flow{login: normalized, userID: id, registration: true, created: time.Now()}
	return flowID, wire(resp.Serialize()), id, nil
}

func (s *Service) RegistrationFinish(flowID string, payload RegistrationPayload) (Result, error) {
	s.mu.Lock()
	f, ok := s.flows[flowID]
	if ok {
		delete(s.flows, flowID)
	}
	s.mu.Unlock()
	if !ok || !f.registration || time.Since(f.created) > 5*time.Minute {
		return Result{}, errors.New("invalid authentication flow")
	}
	recordBytes, err := decodeWire(payload.Record, 8192)
	if err != nil {
		return Result{}, err
	}
	if _, err = s.server.Deserialize.RegistrationRecord(recordBytes); err != nil {
		return Result{}, err
	}
	if len(payload.Vault) == 0 || len(payload.Vault) > 16<<10 || len(payload.Files) != 5 {
		return Result{}, errors.New("invalid initial vault")
	}
	for _, name := range []string{"config", "accounts", "identities", "smtp", "preferences"} {
		data, exists := payload.Files[name]
		if !exists {
			return Result{}, errors.New("missing encrypted vault file")
		}
		if err = vaultValidate(data); err != nil {
			return Result{}, err
		}
	}
	if err = vaultMetadataValidate(payload.Vault); err != nil {
		return Result{}, err
	}
	authJSON, _ := json.Marshal(map[string]any{"version": 1, "protocol": "OPAQUE-RFC9807", "record": base64.RawURLEncoding.EncodeToString(recordBytes)})
	files := user.RegistrationFiles{AuthJSON: authJSON, VaultJSON: payload.Vault, ConfigEncrypted: payload.Files["config"], Accounts: payload.Files["accounts"], Identities: payload.Files["identities"], SMTP: payload.Files["smtp"], Preferences: payload.Files["preferences"]}
	if _, err = s.users.RegisterWithID(f.login, f.userID, files); err != nil {
		return Result{}, err
	}
	_, authRevision, err := s.store.ReadUserFile(f.userID, "auth.json")
	if err != nil {
		return Result{}, err
	}
	token, view, err := s.createBoundSession(f.userID, authRevision)
	if err != nil {
		return Result{}, err
	}
	return Result{Token: token, View: view, UserID: f.userID}, nil
}

func (s *Service) LoginStart(login, request string, reauthToken string, expectedUser uuid.UUID) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireFlowsLocked()
	if len(s.flows) >= 1024 {
		return "", "", errors.New("too many authentication flows")
	}
	normalized, err := filestore.NormalizeLogin(login)
	if err != nil {
		return "", "", err
	}
	requestBytes, err := decodeWire(request, 4096)
	if err != nil {
		return "", "", err
	}
	ke1, err := s.server.Deserialize.KE1(requestBytes)
	if err != nil {
		return "", "", err
	}
	entry, exists := s.index.Lookup(normalized)
	var id uuid.UUID
	var rec *opaque.ClientRecord
	var authRevision string
	if exists && entry.Status == filestore.UserActive {
		id = entry.UserID
		if expectedUser != uuid.Nil && expectedUser != id {
			exists = false
		}
		if exists {
			data, revision, readErr := s.store.ReadUserFile(id, "auth.json")
			authRevision = revision
			if readErr != nil {
				return "", "", readErr
			}
			var authRecord struct {
				Version  int    `json:"version"`
				Protocol string `json:"protocol"`
				Record   string `json:"record"`
			}
			if json.Unmarshal(data, &authRecord) != nil || authRecord.Version != 1 || authRecord.Protocol != "OPAQUE-RFC9807" {
				return "", "", errors.New("invalid authentication record")
			}
			raw, decodeErr := base64.RawURLEncoding.DecodeString(authRecord.Record)
			if decodeErr != nil {
				return "", "", errors.New("invalid authentication record")
			}
			registrationRecord, decodeErr := s.server.Deserialize.RegistrationRecord(raw)
			if decodeErr != nil {
				return "", "", errors.New("invalid authentication record")
			}
			rec = &opaque.ClientRecord{CredentialIdentifier: []byte(normalized), ClientIdentity: []byte(normalized), RegistrationRecord: registrationRecord}
		}
	}
	if rec == nil {
		id = uuid.Nil
		rec, err = s.conf.GetFakeRecord([]byte(normalized))
		if err != nil {
			return "", "", err
		}
	}
	ke2, out, err := s.server.GenerateKE2(ke1, rec)
	if err != nil {
		return "", "", err
	}
	flowID, err := newFlowID()
	if err != nil {
		return "", "", err
	}
	s.flows[flowID] = flow{login: normalized, userID: id, authRevision: authRevision, mac: append([]byte(nil), out.ClientMAC...), created: time.Now(), reauthToken: reauthToken}
	return flowID, wire(ke2.Serialize()), nil
}

func (s *Service) LoginFinish(flowID, request string) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.flows[flowID]
	if ok {
		delete(s.flows, flowID)
	}
	if !ok || f.registration || f.passwordChange || time.Since(f.created) > 5*time.Minute {
		return Result{}, errors.New("invalid authentication flow")
	}
	bytes, err := decodeWire(request, 1024)
	if err != nil {
		return Result{}, err
	}
	ke3, err := s.server.Deserialize.KE3(bytes)
	if err != nil {
		return Result{}, err
	}
	if err = s.server.LoginFinish(ke3, f.mac); err != nil || f.userID == uuid.Nil || !s.users.IsActive(f.userID) {
		return Result{}, errors.New("authentication failed")
	}
	_, currentRevision, err := s.store.ReadUserFile(f.userID, "auth.json")
	if err != nil || currentRevision != f.authRevision {
		return Result{}, errors.New("authentication changed during login")
	}
	if f.reauthToken != "" {
		if err = s.sessions.Reauthenticate(f.reauthToken); err != nil {
			return Result{}, errors.New("authentication failed")
		}
		view, err := s.sessions.Get(f.reauthToken)
		if err != nil || view.UserID != f.userID || view.AuthRevision != "" && view.AuthRevision != currentRevision {
			return Result{}, errors.New("authentication failed")
		}
		if s.grants == nil {
			s.grants = make(map[string]time.Time)
		}
		s.grants[f.reauthToken] = time.Now()
		return Result{View: view, UserID: f.userID}, nil
	}
	token, view, err := s.createBoundSession(f.userID, currentRevision)
	if err != nil {
		return Result{}, err
	}
	return Result{Token: token, View: view, UserID: f.userID}, nil
}

func (s *Service) expireFlowsLocked() {
	for token, created := range s.grants {
		if time.Since(created) > 5*time.Minute {
			delete(s.grants, token)
		}
	}
	for id, f := range s.flows {
		if time.Since(f.created) > 5*time.Minute {
			delete(s.flows, id)
		}
	}
}
func newFlowID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func decodeWire(value string, max int) ([]byte, error) {
	if len(value) > max*2 {
		return nil, errors.New("protocol message too large")
	}
	b, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(b) == 0 || len(b) > max {
		return nil, errors.New("invalid protocol message")
	}
	return b, nil
}
func wire(value []byte) string { return base64.RawURLEncoding.EncodeToString(value) }
func SetupHex() (string, error) {
	c := opaque.DefaultConfiguration()
	sk, pk := c.KeyGen()
	if sk == nil || pk == nil {
		return "", errors.New("could not generate OPAQUE key")
	}
	return (&opaque.ServerKeyMaterial{PrivateKey: sk, PublicKeyBytes: pk.Encode(), OPRFGlobalSeed: c.GenerateOPRFSeed(), Identity: []byte("mail-aggregator")}).Hex(), nil
}

// Imported by the API to apply the vault's strict envelope validators without
// exposing any cryptographic parsing to the HTTP package.
func ValidateVaultInputs(metadata json.RawMessage, files map[string]json.RawMessage) error {
	if len(files) != 5 {
		return errors.New("invalid encrypted files")
	}
	for _, name := range []string{"config", "accounts", "identities", "smtp", "preferences"} {
		if data, ok := files[name]; !ok || vaultValidate(data) != nil {
			return errors.New("invalid encrypted file")
		}
	}
	return vaultMetadataValidate(metadata)
}
func vaultValidate(data []byte) error         { return vault.ValidateEncryptedFileJSON(data) }
func vaultMetadataValidate(data []byte) error { return vault.ValidateMetadataJSON(data) }

func (s *Service) createBoundSession(id uuid.UUID, revision string) (string, session.View, error) {
	token, view, err := s.sessions.Create(id)
	if err != nil {
		return "", view, err
	}
	if err := s.sessions.BindAuthentication(token, revision); err != nil {
		s.sessions.Revoke(token)
		return "", session.View{}, err
	}
	view.AuthRevision = revision
	return token, view, nil
}
