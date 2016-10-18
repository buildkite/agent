package agent

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

type EnvironmentVariableInterpolator struct {
	Data []byte
}

var variablesWithBracketsRegex = regexp.MustCompile(`([\\\$]?\$\{([^}]+?)})`)
var variablesWithNoBracketsRegex = regexp.MustCompile(`([\\\$]?\$[a-zA-Z0-9_]+)`)

func (p EnvironmentVariableInterpolator) Interpolate() (parsed []byte, err error) {
	// Do a parse and handle ENV variables with the ${} syntax, i.e. ${FOO}
	parsed = variablesWithBracketsRegex.ReplaceAllFunc(p.Data, func(part []byte) []byte {
		v := string(part[:])

		if err == nil {
			key, option := p.extractKeyAndOptionFromVariable(v)

			// Just return the key by itself if it was escaped
			if p.isPrefixedWithEscapeSequence(v) {
				v = key
			} else {
				err = p.isValidPosixEnvironmentVariable(v)
				if err != nil {
					return []byte(v)
				}

				vv, isEnvironmentVariableSet := os.LookupEnv(key)

				switch {
				case strings.HasPrefix(option, "?"):
					if vv == "" {
						errorMessage := option[1:]
						if errorMessage == "" {
							errorMessage = "not set"
						}
						err = fmt.Errorf("$%s: %s", key, errorMessage)
					}

				case strings.HasPrefix(option, ":-"):
					if vv == "" {
						vv = option[2:]
					}

				case strings.HasPrefix(option, "-"):
					if !isEnvironmentVariableSet {
						vv = option[1:]
					}

				case option != "":
					err = fmt.Errorf("Invalid option `%s` for environment variable `%s`", option, key)
				}

				v = vv
			}
		}

		return []byte(v)
	})

	// Another parse but this time target ENV variables without the {}
	// surrounding it, i.e. $FOO. These ones are super simple to replace.
	parsed = variablesWithNoBracketsRegex.ReplaceAllFunc(parsed, func(part []byte) []byte {
		v := string(part[:])

		if err == nil {
			key, _ := p.extractKeyAndOptionFromVariable(v)

			// Just return the key by itself if it was escaped
			if p.isPrefixedWithEscapeSequence(v) {
				v = key
			} else {
				err = p.isValidPosixEnvironmentVariable(v)
				if err != nil {
					return []byte(v)
				}

				v = os.Getenv(key)
			}
		}

		return []byte(v)
	})

	return
}

func (p EnvironmentVariableInterpolator) isPrefixedWithEscapeSequence(variable string) bool {
	return strings.HasPrefix(variable, "$$") || strings.HasPrefix(variable, "\\$")
}

var validPosixEnvironmentVariablePrefixRegex = regexp.MustCompile(`\A\${1}\{?[a-zA-Z]`)

// Returns true if the variable is a valid POSIX environment variale. It will
// return false if the variable begins with a number, or it starts with two $$
// characters.
func (p EnvironmentVariableInterpolator) isValidPosixEnvironmentVariable(variable string) error {
	if validPosixEnvironmentVariablePrefixRegex.MatchString(variable) {
		return nil
	} else {
		return fmt.Errorf("Invalid environment variable `%s` - they can only start with a letter", variable)
	}
}

var firstNonEnvironmentVariableCharacterRegex = regexp.MustCompile(`[^a-zA-Z0-9_]`)

// Takes an environment variable, and extracts the variable name and a suffixed
// option.  For example, ${BEST_COMMAND:-lol} will be turned split into
// "BEST_COMMAND" and ":-lol". Regualr environment variables like $FOO will
// return "FOO" as the `key`, and a blank string as the `option`.
func (p EnvironmentVariableInterpolator) extractKeyAndOptionFromVariable(variable string) (key string, option string) {
	if strings.HasPrefix(variable, "${") {
		// Trim the first 2 characters `${` and the last character `}`
		trimmed := variable[2 : len(variable)-1]

		optionsIndicies := firstNonEnvironmentVariableCharacterRegex.FindStringIndex(trimmed)
		if len(optionsIndicies) > 0 {
			key = trimmed[0:optionsIndicies[0]]
			option = trimmed[optionsIndicies[0]:len(trimmed)]
		} else {
			key = trimmed
		}
	} else {
		// Trim the first character `$`
		key = variable[1:]
	}

	return
}
