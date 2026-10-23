package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type environment map[string]string

// readEnvironment reads the first existing file, then applies the process
// environment. Values are data: shell commands and substitutions are never run.
func readEnvironment(process []string, filenames ...string) (environment, string, error) {
	values := make(environment)
	directory := ""
	for _, filename := range filenames {
		file, err := os.Open(filename)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, "", fmt.Errorf("open configuration file %s: %w", filename, err)
		}
		values, err = parseEnvFile(bufio.NewScanner(file))
		closeErr := file.Close()
		if err != nil {
			return nil, "", fmt.Errorf("read configuration file %s: %w", filename, err)
		}
		if closeErr != nil {
			return nil, "", fmt.Errorf("close configuration file %s: %w", filename, closeErr)
		}
		absolute, err := filepath.Abs(filename)
		if err != nil {
			return nil, "", err
		}
		directory = filepath.Dir(absolute)
		break
	}
	for _, assignment := range process {
		if key, value, ok := strings.Cut(assignment, "="); ok {
			values[key] = value
		}
	}
	return values, directory, nil
}

func parseEnvFile(scanner *bufio.Scanner) (environment, error) {
	values := make(environment)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		if strings.HasPrefix(text, "export ") || strings.HasPrefix(text, "export\t") {
			text = strings.TrimSpace(text[6:])
		}
		key, raw, ok := strings.Cut(text, "=")
		key = strings.TrimSpace(key)
		if !ok || !validEnvKey(key) {
			return nil, fmt.Errorf("invalid assignment at line %d", line)
		}
		value, ok := parseEnvValue(strings.TrimSpace(raw))
		if !ok {
			// Never include the line or value: it may contain key material.
			return nil, fmt.Errorf("invalid value at line %d", line)
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, errors.New("unable to read configuration or configuration line exceeds size limit")
	}
	return values, nil
}

func validEnvKey(key string) bool {
	if key == "" {
		return false
	}
	for index, char := range key {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || char == '_' || (index > 0 && char >= '0' && char <= '9') {
			continue
		}
		return false
	}
	return true
}

func parseEnvValue(raw string) (string, bool) {
	if raw == "" {
		return "", true
	}
	if raw[0] == '\'' || raw[0] == '"' {
		quote := raw[0]
		for index := 1; index < len(raw); index++ {
			if quote == '"' && raw[index] == '\\' {
				index++
				continue
			}
			if raw[index] != quote {
				continue
			}
			rest := strings.TrimSpace(raw[index+1:])
			if rest != "" && !strings.HasPrefix(rest, "#") {
				return "", false
			}
			if quote == '\'' {
				return raw[1:index], true
			}
			value, err := strconv.Unquote(raw[:index+1])
			return value, err == nil
		}
		return "", false
	}
	for index, char := range raw {
		if char == '#' && (index == 0 || raw[index-1] == ' ' || raw[index-1] == '\t') {
			return strings.TrimSpace(raw[:index]), true
		}
	}
	return raw, true
}
