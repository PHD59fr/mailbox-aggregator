package sieve

import (
	"bufio"
	"net"
	"strings"
	"testing"
)

func TestResponseLiteralAndStatusNames(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	go func() { defer server.Close(); server.Write([]byte("\"OK\" ACTIVE\r\n\"NO\"\r\nOK\r\n")) }()
	w := newWire(client)
	rows, err := w.response()
	if err != nil || len(rows) != 2 || rows[0][0] != "OK" {
		t.Fatal(rows, err)
	}
	if list, err := scripts(rows); err != nil || !list[0].Active {
		t.Fatal(list, err)
	}
	client2, server2 := net.Pipe()
	defer client2.Close()
	go func() { defer server2.Close(); server2.Write([]byte("{8}\r\nNO\r\nOK\r\n\r\nOK\r\n")) }()
	rows, err = newWire(client2).response()
	if err != nil || len(rows) != 1 || rows[0][0] != "NO\r\nOK\r\n" {
		t.Fatal(rows, err)
	}
}
func TestResponseRejectsOversizedAndMalformedLiterals(t *testing.T) {
	for _, input := range []string{"{65537}\r\n", "{999999999999999999999}\r\n", "\"unfinished\r\n", "{1}\r\nx", strings.Repeat("x", 65537) + "\r\n"} {
		w := &wire{reader: bufio.NewReaderSize(strings.NewReader(input), 65536), remaining: 262144}
		if _, err := w.response(); err == nil {
			t.Fatal("accepted invalid response")
		}
	}
}
func TestQuotedNamesCannotInjectCommands(t *testing.T) {
	if validName("safe\r\nSETACTIVE evil") {
		t.Fatal("accepted newline")
	}
	if quote(`x"\y`) != `"x\"\\y"` {
		t.Fatal(quote(`x"\y`))
	}
}
