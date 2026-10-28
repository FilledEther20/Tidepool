package config

import "fmt"

// rejectUnknownKeys checks key names against the whitelist, where {} marks a leaf and "*" any key; value types are validated separately.
func rejectUnknownKeys(data, allowed interface{}, path string) error {
	allowedMap, ok := allowed.(map[string]interface{})
	if !ok {
		return configError(fmt.Sprintf("invalid allowed_keys schema at %q", pathOrRoot(path)))
	}

	v, ok := data.(map[string]interface{})
	if !ok {
		return nil
	}
	if len(allowedMap) == 0 {
		return configError(fmt.Sprintf("config key %q must be a value, not a mapping", pathOrRoot(path)))
	}

	wildcardSpec, hasWildcard := allowedMap["*"]
	for _, key := range sortedKeys(v) {
		childPath := joinPath(path, key)
		spec := wildcardSpec
		if !hasWildcard {
			var known bool
			if spec, known = allowedMap[key]; !known {
				return configError(fmt.Sprintf("unknown config key %q", childPath))
			}
		}
		if err := rejectUnknownKeys(v[key], spec, childPath); err != nil {
			return err
		}
	}
	return nil
}

func pathOrRoot(path string) string {
	if path == "" {
		return "(root)"
	}

	return path
}

func joinPath(path, key string) string {
	if path == "" {
		return key
	}

	return path + "." + key
}
