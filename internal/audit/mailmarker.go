// Package audit provides scoped, fail-closed checks for synthetic mail markers.
// It does not infer deployment guarantees from an uninspected host or container.
package audit

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Scanner struct {
	RootDirs []string
	TempDirs []string
	LogDirs  []string
}
type ScanResult struct{ Marker, File string }
type ScanResults []ScanResult

// Callers must explicitly supply every directory to inspect. Missing or
// inaccessible paths are errors, never evidence that content was absent.
func NewScanner() *Scanner            { return &Scanner{} }
func (s *Scanner) AddRoot(dir string) { s.RootDirs = append(s.RootDirs, dir) }

func (s *Scanner) Scan(markers []string) (ScanResults, error) {
	roots := append(append(append([]string{}, s.RootDirs...), s.TempDirs...), s.LogDirs...)
	if len(roots) == 0 || len(markers) == 0 {
		return nil, errors.New("audit requires explicit roots and markers")
	}
	patterns := make(map[string][]string)
	maxPattern := 0
	for _, marker := range markers {
		if len(marker) < 16 {
			return nil, errors.New("audit markers must contain at least 16 bytes")
		}
		values := []string{marker}
		// Base64 alignment depends on the bytes preceding the marker. Search its
		// unambiguous interior for each alignment, ignoring MIME folding whitespace.
		for offset := 0; offset < 3; offset++ {
			encoded := base64.StdEncoding.EncodeToString(append(make([]byte, offset), []byte(marker)...))
			values = append(values, encoded[4:len(encoded)-4])
		}
		for _, pattern := range values {
			patterns[marker] = append(patterns[marker], pattern)
			maxPattern = max(maxPattern, len(pattern))
		}
	}
	var results ScanResults
	seen := make(map[string]bool)
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("uninspected audit symlink: %s", path)
			}
			if entry.IsDir() || seen[path] {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("uninspected audit file: %s", path)
			}
			seen[path] = true
			found, err := scanFile(path, patterns, maxPattern)
			results = append(results, found...)
			return err
		})
		if err != nil {
			return results, fmt.Errorf("incomplete audit scan: %w", err)
		}
	}
	return results, nil
}
func scanFile(path string, patterns map[string][]string, overlap int) (ScanResults, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	buffer := make([]byte, 64*1024)
	var rawTail, compactTail []byte
	found := make(map[string]bool)
	for {
		n, readErr := file.Read(buffer)
		raw := append(rawTail, buffer[:n]...)
		compact := append([]byte{}, compactTail...)
		for _, b := range buffer[:n] {
			if b != '\r' && b != '\n' && b != ' ' && b != '\t' {
				compact = append(compact, b)
			}
		}
		for marker, values := range patterns {
			for _, value := range values {
				if bytes.Contains(raw, []byte(value)) || bytes.Contains(compact, []byte(value)) {
					found[marker] = true
				}
			}
		}
		rawTail = append([]byte{}, raw[max(0, len(raw)-overlap):]...)
		compactTail = append([]byte{}, compact[max(0, len(compact)-overlap):]...)
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	var results ScanResults
	for marker := range found {
		results = append(results, ScanResult{Marker: marker, File: path})
	}
	return results, nil
}

// CheckNoMarkers scans only the supplied controlled supports. An error and a
// detected marker both invalidate the check; no mail contents enter its errors.
func CheckNoMarkers(roots, markers []string) error {
	scanner := NewScanner()
	scanner.RootDirs = roots
	results, err := scanner.Scan(markers)
	if err != nil {
		return err
	}
	if len(results) > 0 {
		return fmt.Errorf("mail markers detected in %d inspected files", len(results))
	}
	return nil
}

// FoldedBase64 is used by positive detection controls, not mail persistence.
func FoldedBase64(value string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(value))
	var lines []string
	for len(encoded) > 76 {
		lines = append(lines, encoded[:76])
		encoded = encoded[76:]
	}
	return strings.Join(append(lines, encoded), "\r\n")
}
