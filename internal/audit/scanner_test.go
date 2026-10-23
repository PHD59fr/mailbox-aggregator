package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScannerDetectsActualWritesIncludingBinaryLongLinesAndMIME(t *testing.T) {
	marker := "MAIL-AUDIT-POSITIVE-42-unique-content-0123456789"
	for _, content := range []string{"\x00" + marker, strings.Repeat("a", 70000) + marker, FoldedBase64("x" + marker), FoldedBase64("xx" + marker), FoldedBase64(marker)} {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "witness.bin"), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		scanner := NewScanner()
		scanner.AddRoot(root)
		found, err := scanner.Scan([]string{marker})
		if err != nil || len(found) != 1 {
			t.Fatalf("positive control was not detected: count=%d err=%v", len(found), err)
		}
	}
}
func TestScannerFailsOnMissingRootsAndSymlinks(t *testing.T) {
	scanner := NewScanner()
	if _, err := scanner.Scan([]string{"unique-marker-0123456789"}); err == nil {
		t.Fatal("empty scan passed")
	}
	scanner.AddRoot(filepath.Join(t.TempDir(), "missing"))
	if _, err := scanner.Scan([]string{"unique-marker-0123456789"}); err == nil {
		t.Fatal("missing support passed")
	}
	root := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "outside")); err != nil {
		t.Fatal(err)
	}
	scanner.RootDirs = []string{root}
	if _, err := scanner.Scan([]string{"unique-marker-0123456789"}); err == nil {
		t.Fatal("uninspected symlink passed")
	}
}
