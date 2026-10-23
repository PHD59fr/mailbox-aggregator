package mail

import (
	"bytes"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"regexp"
)

var cidPattern = regexp.MustCompile(`^[a-zA-Z0-9_.@-]{1,128}$`)

func ValidContentID(id string) bool { return cidPattern.MatchString(id) }
func InlineImages(attachments []Attachment) (map[string]bool, error) {
	ids := map[string]bool{}
	for _, attachment := range attachments {
		if attachment.ContentID == "" {
			continue
		}
		if !ValidContentID(attachment.ContentID) || ids[attachment.ContentID] || len(ids) >= 8 || len(attachment.Data) > 2<<20 {
			return nil, errors.New("invalid inline image ID, count or size")
		}
		config, kind, err := image.DecodeConfig(bytes.NewReader(attachment.Data))
		if err != nil || (kind != "png" && kind != "jpeg") || attachment.ContentType != "image/"+kind || config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 || int64(config.Width)*int64(config.Height) > 16000000 {
			return nil, errors.New("inline images must be valid PNG or JPEG, at most 4096 pixels per side")
		}
		ids[attachment.ContentID] = true
	}
	return ids, nil
}
