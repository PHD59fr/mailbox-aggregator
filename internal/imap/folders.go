package imap

import (
	"context"
	"sort"
	"strings"
	"time"

	protocol "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

const MaxFolders = 200

type Folder struct {
	Name        string `json:"name"`
	Role        string `json:"role,omitempty"`
	SpecialUse  bool   `json:"special_use,omitempty"`
	Delimiter   string `json:"delimiter"`
	Selectable  bool   `json:"selectable"`
	Messages    uint32 `json:"messages"`
	Unseen      uint32 `json:"unseen"`
	UIDNext     uint32 `json:"uid_next"`
	UIDValidity uint32 `json:"uid_validity"`
	Error       string `json:"error,omitempty"`
}
type FolderPage struct {
	Folders   []Folder `json:"folders"`
	Truncated bool     `json:"truncated"`
}

// Folders synchronizes names and counts only, on one bounded read-only
// connection. It does not copy message bodies or create local/provider folders.
func (r Receiver) Folders(ctx context.Context, cfg Configuration) (FolderPage, error) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	client, closeConn, err := r.connect(ctx, cfg, nil)
	if err != nil {
		return FolderPage{}, err
	}
	defer closeConn()
	page := FolderPage{Folders: []Folder{}}
	command := client.List("", "*", folderListOptions(client))
	for data := command.Next(); data != nil; data = command.Next() {
		if len(page.Folders) == MaxFolders {
			page.Truncated = true
			// Closing the connection prevents an unbounded LIST drain.
			drained := make(chan struct{})
			go func() { _ = command.Close(); close(drained) }()
			closeConn()
			<-drained
			break
		}
		role, special := folderRole(data)
		folder := Folder{Name: data.Mailbox, Role: role, SpecialUse: special, Selectable: true}
		if data.Delim != 0 {
			folder.Delimiter = string(data.Delim)
		}
		for _, attr := range data.Attrs {
			if attr == protocol.MailboxAttrNoSelect || attr == protocol.MailboxAttrNonExistent {
				folder.Selectable = false
			}
		}
		page.Folders = append(page.Folders, folder)
	}
	if !page.Truncated {
		if err = command.Close(); err != nil {
			return FolderPage{}, err
		}
	} else {
		// The capped LIST connection was closed; obtain counts on a fresh
		// connection without ever holding two provider connections at once.
		client, closeConn, err = r.connect(ctx, cfg, nil)
		if err != nil {
			return FolderPage{}, err
		}
		defer closeConn()
	}
	if ctx.Err() != nil {
		return FolderPage{}, ctx.Err()
	}
	for i := range page.Folders {
		folder := &page.Folders[i]
		if !folder.Selectable {
			continue
		}
		data, err := client.Status(folder.Name, &protocol.StatusOptions{NumMessages: true, NumUnseen: true, UIDNext: true, UIDValidity: true}).Wait()
		if ctx.Err() != nil {
			return FolderPage{}, ctx.Err()
		}
		if err != nil || data.NumMessages == nil || data.NumUnseen == nil {
			folder.Error = "Could not check this folder."
			continue
		}
		folder.Messages = *data.NumMessages
		folder.Unseen = *data.NumUnseen
		folder.UIDNext = uint32(data.UIDNext)
		folder.UIDValidity = data.UIDValidity
	}
	sort.Slice(page.Folders, func(i, j int) bool {
		return strings.ToLower(page.Folders[i].Name) < strings.ToLower(page.Folders[j].Name)
	})
	return page, nil
}

func folderListOptions(client *imapclient.Client) *protocol.ListOptions {
	if client.Caps().Has(protocol.CapSpecialUse) {
		return &protocol.ListOptions{ReturnSpecialUse: true}
	}
	return nil
}

func folderRole(data *protocol.ListData) (string, bool) {
	for _, attr := range data.Attrs {
		role := map[protocol.MailboxAttr]string{protocol.MailboxAttrSent: "sent", protocol.MailboxAttrDrafts: "drafts", protocol.MailboxAttrTrash: "trash", protocol.MailboxAttrArchive: "archive", protocol.MailboxAttrJunk: "spam", protocol.MailboxAttrAll: "all"}[attr]
		if role != "" {
			return role, true
		}
	}
	name := strings.ToLower(data.Mailbox)
	if name == "inbox" {
		return "inbox", false
	}
	if data.Delim != 0 {
		parts := strings.Split(name, string(data.Delim))
		if len(parts) > 1 {
			parent := strings.Join(parts[:len(parts)-1], string(data.Delim))
			if parent != "inbox" && parent != "[gmail]" && parent != "[google mail]" {
				return "", false
			}
			name = parts[len(parts)-1]
		}
	}
	name = strings.NewReplacer("é", "e", "è", "e", "ê", "e", "ë", "e", "ä", "a", "ö", "o", "ü", "u", "ô", "o", "î", "i", " ", "", "-", "", "_", "", "'", "", "’", "").Replace(name)
	for role, names := range map[string][]string{
		"sent":    {"sent", "sentmail", "sentitems", "sentmessages", "envoyes", "elementsenvoyes", "messagesenvoyes", "gesendet", "gesendete"},
		"drafts":  {"draft", "drafts", "brouillons", "entwurfe", "entwurf"},
		"trash":   {"trash", "bin", "deleted", "deleteditems", "deletedmessages", "corbeille", "papierkorb", "geloscht"},
		"archive": {"archive", "archives"},
		"spam":    {"spam", "junk", "junkemail", "indesirables", "courrierindesirable"},
		"outbox":  {"outbox", "boitedenvoi", "postausgang"},
		"all":     {"allmail", "touslesmessages"},
	} {
		for _, candidate := range names {
			if name == candidate {
				return role, false
			}
		}
	}
	return "", false
}
