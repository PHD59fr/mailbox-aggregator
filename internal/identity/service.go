package identity

import (
	"errors"
	"net/mail"
	"strings"

	mailmodel "github.com/example/mail-aggregator/internal/mail"
)

var ErrUnauthorizedIdentity = errors.New("sender identity is not authorized")

func Authorize(identities []mailmodel.Identity, id string, smtpConfigIDs map[string]struct{}) (mailmodel.Identity, error) {
	for _, identity := range identities {
		if identity.ID != id {
			continue
		}
		if _, ok := smtpConfigIDs[identity.SMTPConfigID]; !ok {
			return mailmodel.Identity{}, ErrUnauthorizedIdentity
		}
		parsed, err := mail.ParseAddress(identity.Email)
		if err != nil || !strings.EqualFold(parsed.Address, identity.Email) {
			return mailmodel.Identity{}, ErrUnauthorizedIdentity
		}
		return identity, nil
	}
	return mailmodel.Identity{}, ErrUnauthorizedIdentity
}
