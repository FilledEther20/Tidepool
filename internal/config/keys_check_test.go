package config

import (
	"strings"
	"testing"
)

func tree(t *testing.T, doc string) map[string]interface{} {
	t.Helper()
	_, m, err := parseYAMLTree([]byte(doc), "test")
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestRejectUnknownKeys(t *testing.T) {
	allowed := tree(t, `
mapping:
  "*": {}
section:
  leaf: {}
  update:
    "*": {}
`)
	cases := map[string]struct{ data, wantErr string }{
		"valid":                     {"mapping: {a: b, c: d}\nsection: {leaf: [x], update: {k: v}}\n", ""},
		"wildcard any key":          {"mapping: {anything_goes: x}\n", ""},
		"scalar under leaf":         {"section: {leaf: 1}\n", ""},
		"list under leaf":           {"section: {leaf: [a, b]}\n", ""},
		"unknown top":               {"other: 1\n", `unknown config key "other"`},
		"unknown nested":            {"section: {nope: 1}\n", `unknown config key "section.nope"`},
		"map under wildcard leaf":   {"mapping: {a: {b: c}}\n", `"mapping.a" must be a value`},
		"map under leaf":            {"section: {leaf: {a: b}}\n", `"section.leaf" must be a value`},
		"map under wildcard update": {"section: {update: {k: {x: y}}}\n", `"section.update.k" must be a value`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := rejectUnknownKeys(tree(t, tc.data), allowed, "")
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Fatalf("error %v; want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestRejectUnknownKeysDeterministic(t *testing.T) {
	allowed := tree(t, "a: {}\n")
	data := tree(t, "z: 1\nb: 1\nm: 1\n")
	for i := 0; i < 20; i++ {
		err := rejectUnknownKeys(data, allowed, "")
		if err == nil || !strings.Contains(err.Error(), `"b"`) {
			t.Fatalf("iteration %d: want first sorted unknown key b, got %v", i, err)
		}
	}
}

func TestRejectUnknownKeysBadSchema(t *testing.T) {
	if err := rejectUnknownKeys(tree(t, "a: 1\n"), "not a map", ""); err == nil {
		t.Fatal("expected error for invalid whitelist")
	}
}
