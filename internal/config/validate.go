package config

import (
	"fmt"
	"math"
	"strings"
)

func validateConfig(cfg *Config) error {
	if len(cfg.Mapping) == 0 {
		return configError("mapping must contain at least one column")
	}
	if cfg.Target.TableName == "" || !sqlIdentifier(cfg.Target.TableName) {
		return configError("target.table_name must be a lowercase SQL identifier")
	}
	if cfg.Target.Schema == "" || !sqlIdentifier(cfg.Target.Schema) {
		return configError("target.schema must be a lowercase SQL identifier")
	}
	if cfg.Schema.Name != cfg.Target.TableName {
		return configError(fmt.Sprintf("target.table_name %q does not match schema table %q", cfg.Target.TableName, cfg.Schema.Name))
	}
	if cfg.Schema.Schema != "" && cfg.Schema.Schema != cfg.Target.Schema {
		return configError(fmt.Sprintf("target.schema %q does not match schema definition %q", cfg.Target.Schema, cfg.Schema.Schema))
	}

	destinations := make(map[string]string, len(cfg.Mapping))
	for header, column := range cfg.Mapping {
		if strings.TrimSpace(header) == "" {
			return configError("mapping contains an empty CSV header")
		}
		if !sqlIdentifier(column) {
			return configError(fmt.Sprintf("mapping.%q must map to a lowercase SQL identifier", header))
		}
		if previous, exists := destinations[column]; exists {
			return configError(fmt.Sprintf("mapping headers %q and %q both target column %q", previous, header, column))
		}
		if _, exists := cfg.Schema.Columns[column]; !exists {
			return configError(fmt.Sprintf("mapping.%q references unknown schema column %q", header, column))
		}
		destinations[column] = header
	}

	if len(cfg.ToUpdate.Target) == 0 {
		return configError("on_conflict.target must contain at least one column")
	}
	if len(cfg.ToUpdate.Update) == 0 {
		return configError("on_conflict.update must contain at least one column")
	}
	seenTarget := make(map[string]bool, len(cfg.ToUpdate.Target))
	for _, column := range cfg.ToUpdate.Target {
		if seenTarget[column] {
			return configError(fmt.Sprintf("on_conflict.target contains duplicate column %q", column))
		}
		seenTarget[column] = true
		if _, exists := destinations[column]; !exists {
			return configError(fmt.Sprintf("on_conflict.target column %q is not mapped", column))
		}
	}
	if !matchesUniqueKey(seenTarget, cfg.Schema.UniqueKeys) {
		return configError("on_conflict.target must match a declared primary or unique key")
	}
	for header, column := range cfg.ToUpdate.Update {
		if cfg.Mapping[header] != column {
			return configError(fmt.Sprintf("on_conflict.update.%q must match an insert mapping pair", header))
		}
	}
	for name, column := range cfg.Schema.Columns {
		if !column.Nullable {
			if _, mapped := destinations[name]; !mapped {
				return configError(fmt.Sprintf("required schema column %q is not mapped", name))
			}
		}
	}

	if cfg.Runtime.MaxParameters < 1 || cfg.Runtime.MaxParameters > defaultMaxParameters {
		return configError(fmt.Sprintf("runtime.max_parameters must be between 1 and %d", defaultMaxParameters))
	}
	if math.IsNaN(cfg.Runtime.MaxFailureRate) || math.IsInf(cfg.Runtime.MaxFailureRate, 0) || cfg.Runtime.MaxFailureRate <= 0 || cfg.Runtime.MaxFailureRate > 1 {
		return configError("runtime.max_failure_rate must be greater than 0 and at most 1")
	}
	if cfg.Runtime.MaxConsecutiveFailures <= 0 {
		return configError("runtime.max_consecutive_failures must be positive")
	}
	if cfg.Runtime.MinFailureSample <= 0 {
		return configError("runtime.min_failure_sample must be positive")
	}
	if cfg.Runtime.MaxParameters/len(cfg.Mapping) == 0 {
		return configError("runtime.max_parameters is too small for one mapped row")
	}
	return nil
}

func sqlIdentifier(value string) bool {
	if value == "" || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for i := 1; i < len(value); i++ {
		c := value[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}

func matchesUniqueKey(target map[string]bool, keys [][]string) bool {
	for _, key := range keys {
		if len(key) != len(target) {
			continue
		}
		match := true
		for _, column := range key {
			if !target[column] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
