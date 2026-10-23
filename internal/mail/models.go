package mail

import "time"

type Account struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	IMAPHost     string `json:"imap_host"`
	IMAPPort     uint16 `json:"imap_port"`
	IMAPSecurity string `json:"imap_security"`
	IMAPUsername string `json:"imap_username"`
	SMTPConfigID string `json:"smtp_config_id,omitempty"`
	IsDefault    bool   `json:"is_default"`
}

type SMTPConfig struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Host      string `json:"host"`
	Port      uint16 `json:"port"`
	Security  string `json:"security"`
	Username  string `json:"username"`
	IsDefault bool   `json:"is_default"`
}

type MessageRef struct {
	AccountID   string `json:"account_id"`
	Mailbox     string `json:"mailbox"`
	UIDValidity uint32 `json:"uid_validity"`
	UID         uint32 `json:"uid"`
}
type Address struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email"`
}
type Attachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Data        []byte `json:"-"`
}
type Identity struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Email        string `json:"email"`
	ReplyTo      string `json:"reply_to,omitempty"`
	Signature    string `json:"signature,omitempty"`
	SMTPConfigID string `json:"smtp_config_id"`
	IsDefault    bool   `json:"is_default"`
}
type Draft struct {
	IdentityID  string       `json:"identity_id"`
	To          []Address    `json:"to"`
	CC          []Address    `json:"cc,omitempty"`
	BCC         []Address    `json:"bcc,omitempty"`
	Subject     string       `json:"subject"`
	Text        string       `json:"text,omitempty"`
	HTML        string       `json:"html,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
	InReplyTo   string       `json:"in_reply_to,omitempty"`
	References  []string     `json:"references,omitempty"`
}
type Summary struct {
	Ref     MessageRef `json:"ref"`
	Subject string     `json:"subject"`
	From    Address    `json:"from"`
	Date    time.Time  `json:"date"`
	Seen    bool       `json:"seen"`
	Flagged bool       `json:"flagged"`
	Preview string     `json:"preview,omitempty"`
}
