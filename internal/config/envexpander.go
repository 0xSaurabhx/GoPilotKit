package config

import (
	"os"
	"regexp"
)

// envVarPattern matches ${VAR_NAME} or ${VAR_NAME|default_value} patterns.
var envVarPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*?)(?:\|([^}]*))?\}`)

// ExpandEnvVars replaces ${VAR} and ${VAR|default} patterns in the input
// with their corresponding environment variable values.
//
// Supported patterns:
//   - ${VAR}         — replaced with the value of VAR, or empty string if unset
//   - ${VAR|default} — replaced with the value of VAR, or "default" if unset
//
// Example:
//
//	ExpandEnvVars([]byte(`api_key: ${OPENAI_API_KEY}`))
//	// → []byte(`api_key: sk-...`)  (if OPENAI_API_KEY is set)
func ExpandEnvVars(data []byte) []byte {
	return envVarPattern.ReplaceAllFunc(data, func(match []byte) []byte {
		submatches := envVarPattern.FindSubmatch(match)
		if len(submatches) < 2 {
			return match
		}
		varName := string(submatches[1])
		defaultVal := ""
		if len(submatches) >= 3 {
			defaultVal = string(submatches[2])
		}

		if val, ok := os.LookupEnv(varName); ok {
			return []byte(val)
		}
		return []byte(defaultVal)
	})
}

// ExpandEnvVarsString is a string-returning convenience wrapper around ExpandEnvVars.
func ExpandEnvVarsString(data string) string {
	return string(ExpandEnvVars([]byte(data)))
}

// HasUnexpandedVars checks if the input contains any unexpanded ${VAR} patterns.
func HasUnexpandedVars(data []byte) bool {
	return envVarPattern.Match(data)
}

// ListEnvVars returns the names of all environment variables referenced in the input.
func ListEnvVars(data []byte) []string {
	matches := envVarPattern.FindAllSubmatch(data, -1)
	seen := make(map[string]bool)
	var vars []string
	for _, m := range matches {
		if len(m) >= 2 {
			name := string(m[1])
			if !seen[name] {
				seen[name] = true
				vars = append(vars, name)
			}
		}
	}
	return vars
}
