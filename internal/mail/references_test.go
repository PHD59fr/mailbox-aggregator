package mail

import (
	"fmt"
	"strings"
	"testing"
)

func TestReferencesBoundedDeduplicatedAndPreserveRoot(t *testing.T) {
	header := "(comment) <root@example.test>\r\n <parent@example.test> <root@example.test> malformed"
	ids := ParseReferences(header)
	if len(ids) != 2 || ids[0] != "<root@example.test>" || ids[1] != "<parent@example.test>" {
		t.Fatalf("unexpected chain: %v", ids)
	}
	for i := 0; i < 50; i++ {
		header += fmt.Sprintf(" <parent%d@example.test>", i)
	}
	ids = ParseReferences(header)
	if len(ids) != 32 || ids[0] != "<root@example.test>" || ids[31] != "<parent49@example.test>" {
		t.Fatalf("lost root or recent parents: %v", ids)
	}
	if len(ParseReferences(strings.Repeat("a", MaxReferencesBytes+1))) != 0 {
		t.Fatal("unbounded header accepted")
	}
}
