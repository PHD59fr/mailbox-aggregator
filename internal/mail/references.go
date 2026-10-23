package mail

import "regexp"

// Bounds match the send API. Keep the conversation root and newest parents
// when a remote conversation exceeds the supported chain length.
const MaxReferences = 32
const MaxReferenceBytes = 1000
const MaxReferencesBytes = 32768

var messageIDPattern = regexp.MustCompile(`<[^<>\x00-\x20\x7f]{1,998}>`)

func ParseReferences(header string) []string {
	if len(header) > MaxReferencesBytes {
		return nil
	}
	ids := messageIDPattern.FindAllString(header, -1)
	seen := make(map[string]bool)
	result := make([]string, 0, MaxReferences)
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		if len(result) == MaxReferences {
			copy(result[1:], result[2:])
			result = result[:MaxReferences-1]
		}
		result = append(result, id)
	}
	return result
}
