package config

import (
	"os"
	"regexp"
)

// envVarRef matches ${VAR_NAME}; name must be a shell-style identifier (ASCII letters, digits, underscore).
var envVarRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// expandEnvVars replaces each ${VAR} with os.Getenv("VAR"). If the variable is unset, the result is empty.
func expandEnvVars(s string) string {
	return envVarRef.ReplaceAllStringFunc(s, func(match string) string {
		sub := envVarRef.FindStringSubmatch(match)
		if len(sub) != 2 {
			return match
		}
		return os.Getenv(sub[1])
	})
}
