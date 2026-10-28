package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"

	"gopkg.in/yaml.v3"
)

const defaultMinFailureSample = 100

type Config struct {
	Mapping  map[string]string `yaml:"mapping"`
	ToUpdate OnConflict        `yaml:"on_conflict"`
	Target   Target            `yaml:"target"`
	Runtime  Runtime           `yaml:"runtime"`
}

// OnConflict describes the upsert related target and columns to update
type OnConflict struct {
	Target []string          `yaml:"target"`
	Update map[string]string `yaml:"update"`
}

// Target holds db related information
type Target struct {
	Schema    string `yaml:"schema"`
	TableName string `yaml:"table_name"`
}

// Runtime holds tunable pipeline limits
type Runtime struct {
	MaxParameters          int     `yaml:"max_parameters"`
	MaxFailureRate         float64 `yaml:"max_failure_rate"`
	MaxConsecutiveFailures int     `yaml:"max_consecutive_failures"`
	// MinFailureSample defaults to 100 only when the key is omitted; an explicit value is kept as written.
	MinFailureSample int `yaml:"min_failure_sample"`
}

type ConfigError struct {
	Code    string
	Message string
}

func (e *ConfigError) Error() string {
	return e.Message
}

func configError(msg string) *ConfigError {
	return &ConfigError{Code: "CONFIG", Message: msg}
}

var (
	strTag = []string{"!!str"}
	intTag = []string{"!!int"}
)

// scalarFields lists each scalar field with the YAML tags it accepts.
var scalarFields = []struct {
	section, field string
	tags           []string
}{
	{"target", "schema", strTag},
	{"target", "table_name", strTag},
	{"runtime", "max_parameters", intTag},
	{"runtime", "max_failure_rate", []string{"!!float", "!!int"}},
	{"runtime", "max_consecutive_failures", intTag},
	{"runtime", "min_failure_sample", intTag},
}

// Load strictly decodes the ingestion config; semantic validation happens later.
func Load(configPath, allowedKeysPath string) (*Config, error) {
	rawConfig, err := os.ReadFile(configPath)
	if err != nil {
		return nil, configError(fmt.Sprintf("read config: %v", err))
	}
	rawKeys, err := os.ReadFile(allowedKeysPath)
	if err != nil {
		return nil, configError(fmt.Sprintf("read allowed keys: %v", err))
	}

	_, allowed, err := parseYAMLTree(rawKeys, "allowed keys")
	if err != nil {
		return nil, err
	}

	root, tree, err := parseYAMLTree(rawConfig, "config")
	if err != nil {
		return nil, err
	}
	if err := rejectUnknownKeys(tree, allowed, ""); err != nil {
		return nil, err
	}
	if err := validateTypes(tree); err != nil {
		return nil, err
	}

	var cfg Config
	if err := root.Decode(&cfg); err != nil {
		return nil, configError(fmt.Sprintf("decode config: %v", err))
	}
	if section, _ := tree["runtime"].(map[string]interface{}); section["min_failure_sample"] == nil {
		cfg.Runtime.MinFailureSample = defaultMinFailureSample
	}
	return &cfg, nil
}

// parseYAMLTree parses exactly one mapping document into nested maps, lists and scalar nodes.
func parseYAMLTree(raw []byte, role string) (*yaml.Node, map[string]interface{}, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	var doc yaml.Node
	if err := decoder.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil, configError(role + " is empty")
		}
		return nil, nil, configError(fmt.Sprintf("parse %s: %v", role, err))
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, nil, configError(role + " must contain exactly one YAML document")
	}

	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, nil, configError(fmt.Sprintf("%s line %d: top level must be a mapping", role, root.Line))
	}
	tree, err := buildTree(root, role, "")
	if err != nil {
		return nil, nil, err
	}
	return root, tree.(map[string]interface{}), nil
}

// buildTree rejects non-string and duplicate keys while converting nodes to a generic tree.
func buildTree(node *yaml.Node, role, path string) (interface{}, error) {
	switch node.Kind {
	case yaml.MappingNode:
		out := make(map[string]interface{}, len(node.Content)/2)
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			if key.Kind != yaml.ScalarNode || key.ShortTag() != "!!str" {
				return nil, configError(fmt.Sprintf("%s line %d: key under %q must be a string (quote it)", role, key.Line, pathOrRoot(path)))
			}
			childPath := joinPath(path, key.Value)
			if _, duplicate := out[key.Value]; duplicate {
				return nil, configError(fmt.Sprintf("%s line %d: duplicate key %q", role, key.Line, childPath))
			}
			child, err := buildTree(value, role, childPath)
			if err != nil {
				return nil, err
			}
			out[key.Value] = child
		}
		return out, nil
	case yaml.SequenceNode:
		out := make([]interface{}, 0, len(node.Content))
		for i, item := range node.Content {
			child, err := buildTree(item, role, fmt.Sprintf("%s[%d]", path, i))
			if err != nil {
				return nil, err
			}
			out = append(out, child)
		}
		return out, nil
	default:
		return node, nil
	}
}

// validateTypes checks value kinds without coercion, so 5 or true never pass as strings.
func validateTypes(tree map[string]interface{}) error {
	if value, ok := tree["mapping"]; ok {
		if err := requireStringMap(value, "mapping"); err != nil {
			return err
		}
	}
	if value, ok := tree["on_conflict"]; ok {
		section, err := asMap(value, "on_conflict")
		if err != nil {
			return err
		}
		if target, ok := section["target"]; ok {
			if err := requireStringList(target, "on_conflict.target"); err != nil {
				return err
			}
		}
		if update, ok := section["update"]; ok {
			if err := requireStringMap(update, "on_conflict.update"); err != nil {
				return err
			}
		}
	}
	for _, f := range scalarFields {
		value, ok := tree[f.section]
		if !ok {
			continue
		}
		section, err := asMap(value, f.section)
		if err != nil {
			return err
		}
		if v, ok := section[f.field]; ok {
			if err := requireScalar(v, joinPath(f.section, f.field), f.tags...); err != nil {
				return err
			}
		}
	}
	return nil
}

func asMap(value interface{}, path string) (map[string]interface{}, error) {
	m, ok := value.(map[string]interface{})
	if !ok {
		return nil, configError(fmt.Sprintf("%s must be a mapping, got %s", path, describe(value)))
	}
	return m, nil
}

func requireStringMap(value interface{}, path string) error {
	m, err := asMap(value, path)
	if err != nil {
		return err
	}
	for _, key := range sortedKeys(m) {
		if err := requireScalar(m[key], joinPath(path, key), "!!str"); err != nil {
			return err
		}
	}
	return nil
}

func requireStringList(value interface{}, path string) error {
	list, ok := value.([]interface{})
	if !ok {
		return configError(fmt.Sprintf("%s must be a list, got %s", path, describe(value)))
	}
	for i, item := range list {
		if err := requireScalar(item, fmt.Sprintf("%s[%d]", path, i), "!!str"); err != nil {
			return err
		}
	}
	return nil
}

func requireScalar(value interface{}, path string, tags ...string) error {
	if node, ok := value.(*yaml.Node); ok && slices.Contains(tags, node.ShortTag()) {
		return nil
	}
	return configError(fmt.Sprintf("%s must be %s, got %s", path, tagNames[tags[0]], describe(value)))
}

var tagNames = map[string]string{
	"!!null":  "null",
	"!!str":   "a string",
	"!!int":   "an integer",
	"!!float": "a number",
	"!!bool":  "a boolean",
}

func describe(value interface{}) string {
	switch v := value.(type) {
	case map[string]interface{}:
		return "a mapping"
	case []interface{}:
		return "a list"
	default:
		return tagNames[v.(*yaml.Node).ShortTag()]
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
