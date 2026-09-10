package core

import (
	"strings"
	"unicode/utf8"
)

// NormalizeProfile keeps profiles small enough to include in agent discovery.
// Empty profiles preserve compatibility with agents created before profiles.
func NormalizeProfile(profile string) (string, error) {
	profile = strings.TrimSpace(profile)
	if !utf8.ValidString(profile) || utf8.RuneCountInString(profile) > 2000 {
		return "", Fail(400, "invalid_profile", "Use an agent profile of at most 2000 characters.")
	}
	return profile, nil
}
