package mailsasl

import (
	"errors"
	"strconv"
	"strings"

	"github.com/xdg-go/scram"
)

// SCRAM256 is explicitly selected by an account. No downgrade to LOGIN occurs.
// Bound the server's work factor before the library performs PBKDF2.
type SCRAM256 struct {
	Username     string
	Password     []byte
	conversation *scram.ClientConversation
	step         int
}

var errSCRAM = errors.New("SCRAM-SHA-256 authentication failed")

func (c *SCRAM256) Start() (string, []byte, error) {
	if c.conversation != nil || c.Username == "" || len(c.Username) > 512 || len(c.Password) == 0 || len(c.Password) > 4096 {
		return "", nil, errSCRAM
	}
	client, err := scram.SHA256.NewClient(c.Username, string(c.Password), "")
	if err != nil {
		return "", nil, errSCRAM
	}
	c.conversation = client.NewConversation()
	first, err := c.conversation.Step("")
	if err != nil {
		return "", nil, errSCRAM
	}
	return "SCRAM-SHA-256", []byte(first), nil
}
func (c *SCRAM256) Next(challenge []byte) ([]byte, error) {
	if c.conversation == nil || c.step >= 2 || len(challenge) > 16384 {
		return nil, errSCRAM
	}
	if c.step == 0 {
		iterations := 0
		for _, field := range strings.Split(string(challenge), ",") {
			if strings.HasPrefix(field, "i=") {
				if iterations != 0 {
					return nil, errSCRAM
				}
				n, err := strconv.Atoi(field[2:])
				if err != nil || n < 4096 || n > 1000000 {
					return nil, errSCRAM
				}
				iterations = n
			}
		}
		if iterations == 0 {
			return nil, errSCRAM
		}
	}
	c.step++
	response, err := c.conversation.Step(string(challenge))
	if err != nil {
		return nil, errSCRAM
	}
	return []byte(response), nil
}
func (c *SCRAM256) Valid() bool { return c.conversation != nil && c.conversation.Valid() }
