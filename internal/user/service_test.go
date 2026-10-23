package user

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/example/mail-aggregator/internal/filestore"
)

type revoker struct{ ids []uuid.UUID }

func (r *revoker) RevokeUser(id uuid.UUID) { r.ids = append(r.ids, id) }

func TestDeleteOwnAndRecovery(t *testing.T) {
	root := t.TempDir()
	store, err := filestore.New(root, 1024)
	if err != nil {
		t.Fatal(err)
	}
	index, err := filestore.OpenUserIndex(store.IndexDirectory())
	if err != nil {
		t.Fatal(err)
	}
	r := &revoker{}
	service := NewService(store, index, r, nil)
	id, err := service.Register("person@example.test", RegistrationFiles{AuthJSON: []byte("{}")})
	if err != nil {
		t.Fatal(err)
	}
	if err = service.DeleteOwn(id); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(root, "users", id.String())); !os.IsNotExist(err) {
		t.Fatalf("user directory still exists: %v", err)
	}
	if _, ok := index.Lookup("person@example.test"); ok {
		t.Fatal("index entry still exists")
	}
	if len(r.ids) != 1 || r.ids[0] != id {
		t.Fatal("sessions not revoked")
	}

	crashID, _ := uuid.NewRandom()
	if err = store.CreateUser(crashID, map[string][]byte{"auth.json": []byte("{}")}); err != nil {
		t.Fatal(err)
	}
	if err = index.Add("crash@example.test", crashID); err != nil {
		t.Fatal(err)
	}
	if err = service.writeTombstone(crashID, "requested"); err != nil {
		t.Fatal(err)
	}
	if err = service.RecoverDeletions(); err != nil {
		t.Fatal(err)
	}
	if _, ok := index.Lookup("crash@example.test"); ok {
		t.Fatal("crashed deletion resurrected")
	}
}
