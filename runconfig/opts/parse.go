package opts

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/container"
)

// VolumeSplitN splits raw into a maximum of n parts, separated by a separator colon.
// A separator colon is the last `:` character in the regex `[:\\]?[a-zA-Z]:` (note `\\` is `\` escaped).
// In Windows driver letter appears in two situations:
// a. `^[a-zA-Z]:` (A colon followed  by `^[a-zA-Z]:` is OK as colon is the separator in volume option)
// b. A string in the format like `\\?\C:\Windows\...` (UNC).
// Therefore, a driver letter can only follow either a `:` or `\\`
// This allows to correctly split strings such as `C:\foo:D:\:rw` or `/tmp/q:/foo`.
func VolumeSplitN(raw string, n int) []string {
	var array []string
	if len(raw) == 0 || raw[0] == ':' {
		// invalid
		return nil
	}
	// numberOfParts counts the number of parts separated by a separator colon
	numberOfParts := 0
	// left represents the left-most cursor in raw, updated at every `:` character considered as a separator.
	left := 0
	// right represents the right-most cursor in raw incremented with the loop. Note this
	// starts at index 1 as index 0 is already handle above as a special case.
	for right := 1; right < len(raw); right++ {
		// stop parsing if reached maximum number of parts
		if n >= 0 && numberOfParts >= n {
			break
		}
		if raw[right] != ':' {
			continue
		}
		potentialDriveLetter := raw[right-1]
		if (potentialDriveLetter >= 'A' && potentialDriveLetter <= 'Z') || (potentialDriveLetter >= 'a' && potentialDriveLetter <= 'z') {
			if right > 1 {
				beforePotentialDriveLetter := raw[right-2]
				// Only `:` or `\\` are checked (`/` could fall into the case of `/tmp/q:/foo`)
				if beforePotentialDriveLetter != ':' && beforePotentialDriveLetter != '\\' {
					// e.g. `C:` is not preceded by any delimiter, therefore it was not a drive letter but a path ending with `C:`.
					array = append(array, raw[left:right])
					left = right + 1
					numberOfParts++
				}
				// else, `C:` is considered as a drive letter and not as a delimiter, so we continue parsing.
			}
			// if right == 1, then `C:` is the beginning of the raw string, therefore `:` is again not considered a delimiter and we continue parsing.
		} else {
			// if `:` is not preceded by a potential drive letter, then consider it as a delimiter.
			array = append(array, raw[left:right])
			left = right + 1
			numberOfParts++
		}
	}
	// need to take care of the last part
	if left < len(raw) {
		if n >= 0 && numberOfParts >= n {
			// if the maximum number of parts is reached, just append the rest to the last part
			// left-1 is at the last `:` that needs to be included since not considered a separator.
			array[n-1] += raw[left-1:]
		} else {
			array = append(array, raw[left:])
		}
	}
	return array
}

// ReadKVStrings reads a file of line terminated key=value pairs, and overrides any keys
// present in the file with additional pairs specified in the override parameter
func ReadKVStrings(files []string, override []string) ([]string, error) {
	envVariables := []string{}
	for _, ef := range files {
		parsedVars, err := ParseEnvFile(ef)
		if err != nil {
			return nil, err
		}
		envVariables = append(envVariables, parsedVars...)
	}
	// parse the '-e' and '--env' after, to allow override
	envVariables = append(envVariables, override...)

	return envVariables, nil
}

// ConvertKVStringsToMap converts ["key=value"] to {"key":"value"}
func ConvertKVStringsToMap(values []string) map[string]string {
	result := make(map[string]string, len(values))
	for _, value := range values {
		kv := strings.SplitN(value, "=", 2)
		if len(kv) == 1 {
			result[kv[0]] = ""
		} else {
			result[kv[0]] = kv[1]
		}
	}

	return result
}

// ConvertKVStringsToMapWithNil converts ["key=value"] to {"key":"value"}
// but set unset keys to nil - meaning the ones with no "=" in them.
// We use this in cases where we need to distinguish between
//   FOO=  and FOO
// where the latter case just means FOO was mentioned but not given a value
func ConvertKVStringsToMapWithNil(values []string) map[string]*string {
	result := make(map[string]*string, len(values))
	for _, value := range values {
		kv := strings.SplitN(value, "=", 2)
		if len(kv) == 1 {
			result[kv[0]] = nil
		} else {
			result[kv[0]] = &kv[1]
		}
	}

	return result
}

// ParseRestartPolicy returns the parsed policy or an error indicating what is incorrect
func ParseRestartPolicy(policy string) (container.RestartPolicy, error) {
	p := container.RestartPolicy{}

	if policy == "" {
		return p, nil
	}

	parts := strings.Split(policy, ":")

	if len(parts) > 2 {
		return p, fmt.Errorf("invalid restart policy format")
	}
	if len(parts) == 2 {
		count, err := strconv.Atoi(parts[1])
		if err != nil {
			return p, fmt.Errorf("maximum retry count must be an integer")
		}

		p.MaximumRetryCount = count
	}

	p.Name = parts[0]

	return p, nil
}
