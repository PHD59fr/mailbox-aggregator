package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/example/mail-aggregator/internal/filestore"
	"github.com/example/mail-aggregator/internal/vault"
	"github.com/google/uuid"
	"os"
	"time"
)

type PasswordChanger interface {
	PasswordStart(string, string, string, uuid.UUID, bool) (string, string, error)
	PasswordFinish(string, string, RegistrationPayload) error
}

func (s *Service) PasswordStart(login, request, token string, id uuid.UUID, rotate bool) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireFlowsLocked()
	granted, ok := s.grants[token]
	delete(s.grants, token)
	if !ok || time.Since(granted) > 5*time.Minute {
		return "", "", errors.New("fresh explicit reauthentication required")
	}
	view, err := s.sessions.Peek(token)
	if err != nil || view.UserID != id || !s.users.IsActive(id) {
		return "", "", errors.New("invalid session")
	}
	normalized, err := filestore.NormalizeLogin(login)
	if err != nil {
		return "", "", err
	}
	entry, exists := s.index.Lookup(normalized)
	if !exists || entry.UserID != id || entry.Status != filestore.UserActive {
		return "", "", errors.New("invalid account")
	}
	raw, err := decodeWire(request, 1024)
	if err != nil {
		return "", "", err
	}
	registration, err := s.server.Deserialize.RegistrationRequest(raw)
	if err != nil {
		return "", "", err
	}
	response, err := s.server.RegistrationResponse(registration, []byte(normalized), nil)
	if err != nil {
		return "", "", err
	}
	revisions := make(map[string]string)
	var keyVersion uint32
	for _, name := range []string{"auth.json", "vault.json", "config.enc", "accounts.enc", "identities.enc", "smtp.enc", "preferences.enc", "proxy.json"} {
		data, revision, err := s.store.ReadUserFile(id, name)
		if errors.Is(err, os.ErrNotExist) && name == "proxy.json" {
			revision = "*"
			err = nil
		}
		if err != nil {
			return "", "", err
		}
		revisions[name] = revision
		if name == "vault.json" {
			var metadata vault.Metadata
			if json.Unmarshal(data, &metadata) != nil {
				return "", "", errors.New("invalid vault metadata")
			}
			keyVersion = metadata.KeyVersion
		}
	}
	if view.AuthRevision != "" && view.AuthRevision != revisions["auth.json"] {
		return "", "", errors.New("authentication changed")
	}
	if len(s.flows) >= 1024 {
		return "", "", errors.New("too many authentication flows")
	}
	flowID, err := newFlowID()
	if err != nil {
		return "", "", err
	}
	s.flows[flowID] = flow{login: normalized, userID: id, reauthToken: token, passwordChange: true, revisions: revisions, rotate: rotate, keyVersion: keyVersion, created: time.Now()}
	return flowID, wire(response.Serialize()), nil
}
func (s *Service) PasswordFinish(flowID, token string, payload RegistrationPayload) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.flows[flowID]
	if ok {
		delete(s.flows, flowID)
	}
	if !ok || !f.passwordChange || f.reauthToken != token || time.Since(f.created) > 5*time.Minute {
		return errors.New("invalid password change flow")
	}
	view, err := s.sessions.Peek(token)
	if err != nil || view.UserID != f.userID || !s.users.IsActive(f.userID) {
		return errors.New("invalid session")
	}
	raw, err := decodeWire(payload.Record, 8192)
	if err != nil {
		return err
	}
	if _, err := s.server.Deserialize.RegistrationRecord(raw); err != nil {
		return err
	}
	if err := vault.ValidateMetadataJSON(payload.Vault); err != nil {
		return err
	}
	var metadata vault.Metadata
	if json.Unmarshal(payload.Vault, &metadata) != nil {
		return errors.New("invalid vault metadata")
	}
	expectedVersion := f.keyVersion
	if f.rotate {
		expectedVersion++
		if expectedVersion == 0 {
			return errors.New("vault version exhausted")
		}
	}
	if metadata.KeyVersion != expectedVersion {
		return errors.New("unexpected vault key version")
	}
	if (!f.rotate && len(payload.Files) != 0) || (f.rotate && len(payload.Files) != 5) {
		return errors.New("invalid rotation collections")
	}
	authRecord, _ := json.Marshal(map[string]any{"version": 1, "protocol": "OPAQUE-RFC9807", "record": base64.RawURLEncoding.EncodeToString(raw)})
	files := map[string]filestore.ImportFile{"auth.json": {Data: authRecord, Revision: f.revisions["auth.json"]}, "vault.json": {Data: payload.Vault, Revision: f.revisions["vault.json"]}, "proxy.json": {Data: json.RawMessage(`{"version":1}`), Revision: f.revisions["proxy.json"]}}
	for _, name := range []string{"config", "accounts", "identities", "smtp", "preferences"} {
		data, _, err := s.store.ReadUserFile(f.userID, name+".enc")
		if err != nil {
			return err
		}
		if f.rotate {
			var envelope vault.EncryptedFile
			data = payload.Files[name]
			if err := vault.ValidateEncryptedFileJSON(data); err != nil {
				return err
			}
			if json.Unmarshal(data, &envelope) != nil || envelope.KeyVersion != expectedVersion {
				return errors.New("invalid rotated collection")
			}
		}
		files[name+".enc"] = filestore.ImportFile{Data: data, Revision: f.revisions[name+".enc"]}
	}
	if err := s.store.UpdateCredentials(f.userID, files); err != nil {
		return err
	}
	s.sessions.RevokeUser(f.userID)
	for id, flow := range s.flows {
		if flow.userID == f.userID {
			delete(s.flows, id)
		}
	}
	return nil
}
