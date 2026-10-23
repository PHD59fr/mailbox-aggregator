package mailsasl

import "testing"

func TestXOAuth2FramesTokenAndRejectsControlInjection(t *testing.T) {
	mechanism, response, err := (&XOAuth2{Username: "user@example.test", Token: []byte("access-token")}).Start()
	if err != nil || mechanism != "XOAUTH2" || string(response) != "user=user@example.test\x01auth=Bearer access-token\x01\x01" {
		t.Fatal("invalid XOAUTH2 framing")
	}
	for _, client := range []XOAuth2{{Username: "user\x01auth=admin", Token: []byte("token")}, {Username: "user", Token: []byte("token\x01")}, {Username: "user"}} {
		if _, _, err := client.Start(); err == nil {
			t.Fatal("unsafe credentials accepted")
		}
	}
}
