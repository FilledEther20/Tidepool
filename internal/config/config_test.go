package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleIngestion = "../../configs/sample_ingestion.yml"
const sampleKeys = "../../configs/sample_keys.yml"

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const validConfig = `mapping:
  id: txn_id
  amt: amount
on_conflict:
  target:
    - txn_id
  update:
    amt: amount
target:
  schema: public
  table_name: ledger
runtime:
  max_parameters: 1000
  max_failure_rate: 0.05
  max_consecutive_failures: 100
  min_failure_sample: 50
`

func TestLoadSampleConfig(t *testing.T) {
	cfg, err := Load(sampleIngestion, sampleKeys)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Mapping) != 19 || cfg.Mapping["updated_balance"] != "balance_after" {
		t.Errorf("unexpected mapping: %v", cfg.Mapping)
	}
	if got := cfg.ToUpdate.Update["updated_balance"]; got != "balance_after" {
		t.Errorf("update pair = %q", got)
	}
	if len(cfg.ToUpdate.Target) != 1 || cfg.ToUpdate.Target[0] != "txn_id" {
		t.Errorf("target = %v", cfg.ToUpdate.Target)
	}
	if cfg.Target.Schema != "public" || cfg.Target.TableName != "enterprise_banking_ledger" {
		t.Errorf("target = %+v", cfg.Target)
	}
	want := Runtime{MaxParameters: 65535, MaxFailureRate: 0.05, MaxConsecutiveFailures: 100, MinFailureSample: 100}
	if cfg.Runtime != want {
		t.Errorf("runtime = %+v; want %+v", cfg.Runtime, want)
	}
}

func TestLoadMinFailureSample(t *testing.T) {
	keys := sampleKeys
	omitted := strings.Replace(validConfig, "  min_failure_sample: 50\n", "", 1)
	noRuntime := strings.Split(validConfig, "runtime:")[0]

	cases := []struct {
		name, config string
		want         int
		wantErr      bool
	}{
		{"explicit value", validConfig, 50, false},
		{"omitted defaults", omitted, 100, false},
		{"missing runtime section defaults", noRuntime, 100, false},
		{"explicit zero kept", strings.Replace(validConfig, "min_failure_sample: 50", "min_failure_sample: 0", 1), 0, false},
		{"explicit null rejected", strings.Replace(validConfig, "min_failure_sample: 50", "min_failure_sample:", 1), 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Load(writeTemp(t, "c.yml", tc.config), keys)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Runtime.MinFailureSample != tc.want {
				t.Errorf("MinFailureSample = %d; want %d", cfg.Runtime.MinFailureSample, tc.want)
			}
		})
	}
}

func TestLoadFloatAcceptsInteger(t *testing.T) {
	cfg, err := Load(writeTemp(t, "c.yml", strings.Replace(validConfig, "0.05", "1", 1)), sampleKeys)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Runtime.MaxFailureRate != 1 {
		t.Errorf("rate = %v", cfg.Runtime.MaxFailureRate)
	}
}

func TestLoadRejections(t *testing.T) {
	replace := func(old, repl string) string { return strings.Replace(validConfig, old, repl, 1) }

	cases := map[string]struct{ config, wantSubstr string }{
		"empty file":            {"", "empty"},
		"comment only":          {"# nothing\n", "empty"},
		"list at top level":     {"- a\n- b\n", "top level must be a mapping"},
		"scalar at top level":   {"hello\n", "top level must be a mapping"},
		"multiple documents":    {validConfig + "---\n" + validConfig, "exactly one YAML document"},
		"invalid yaml":          {"mapping: [unclosed\n", "parse config"},
		"unknown top key":       {validConfig + "extra: 1\n", `unknown config key "extra"`},
		"unknown runtime key":   {validConfig + "  bogus: 1\n", `unknown config key "runtime.bogus"`},
		"old on_conflicts key":  {replace("on_conflict:", "on_conflicts:"), `unknown config key "on_conflicts"`},
		"duplicate top key":     {validConfig + "target:\n  schema: x\n", `duplicate key "target"`},
		"duplicate mapping key": {replace("  amt: amount\non_conflict", "  id: other\non_conflict"), `duplicate key "mapping.id"`},
		"duplicate runtime key": {validConfig + "  max_parameters: 5\n", `duplicate key "runtime.max_parameters"`},
		"non-string key":        {replace("  id: txn_id", "  5: txn_id"), "must be a string"},
		"mapping not a map":     {replace("mapping:\n  id: txn_id\n  amt: amount\n", "mapping: x\n"), "mapping must be a mapping"},
		"mapping null":          {replace("mapping:\n  id: txn_id\n  amt: amount\n", "mapping:\n"), "mapping must be a mapping, got null"},
		"number value":          {replace("id: txn_id", "id: 5"), "mapping.id must be a string, got an integer"},
		"bool value":            {replace("id: txn_id", "id: true"), "mapping.id must be a string, got a boolean"},
		"null value":            {replace("id: txn_id", "id:"), "mapping.id must be a string, got null"},
		"nested value":          {replace("id: txn_id", "id: {a: b}"), "must be a value, not a mapping"},
		"target not a list":     {replace("    - txn_id\n", "    txn_id\n"), "on_conflict.target must be a list"},
		"target nested map":     {replace("    - txn_id\n", "    - a: b\n"), "must be a string"},
		"target number item":    {replace("    - txn_id\n", "    - 7\n"), "on_conflict.target[0] must be a string"},
		"update not a map":      {replace("  update:\n    amt: amount\n", "  update: x\n"), "on_conflict.update must be a mapping"},
		"update number":         {replace("    amt: amount\ntarget", "    amt: 3\ntarget"), "on_conflict.update.amt must be a string"},
		"schema number":         {replace("schema: public", "schema: 5"), "target.schema must be a string"},
		"schema null":           {replace("schema: public", "schema:"), "target.schema must be a string, got null"},
		"runtime not a map":     {strings.Split(validConfig, "runtime:")[0] + "runtime: 5\n", "runtime must be a mapping"},
		"params string":         {replace("max_parameters: 1000", "max_parameters: abc"), "runtime.max_parameters must be an integer"},
		"params float":          {replace("max_parameters: 1000", "max_parameters: 1.5"), "runtime.max_parameters must be an integer"},
		"rate string":           {replace("max_failure_rate: 0.05", "max_failure_rate: high"), "runtime.max_failure_rate must be a number"},
		"consecutive bool":      {replace("max_consecutive_failures: 100", "max_consecutive_failures: true"), "runtime.max_consecutive_failures must be an integer"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, err := Load(writeTemp(t, "c.yml", tc.config), sampleKeys)
			if err == nil {
				t.Fatalf("expected error containing %q", tc.wantSubstr)
			}
			if cfg != nil {
				t.Error("config must be nil on error")
			}
			var ce *ConfigError
			if !errors.As(err, &ce) || ce.Code != "CONFIG" {
				t.Errorf("error %v is not a CONFIG ConfigError", err)
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Errorf("error %q does not contain %q", err, tc.wantSubstr)
			}
		})
	}
}

func TestLoadFileErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.yml")
	cfg := writeTemp(t, "c.yml", validConfig)

	for name, paths := range map[string][2]string{
		"missing config": {missing, sampleKeys},
		"missing keys":   {cfg, missing},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Load(paths[0], paths[1])
			var ce *ConfigError
			if got != nil || !errors.As(err, &ce) || ce.Code != "CONFIG" {
				t.Fatalf("got %v, %v", got, err)
			}
		})
	}
}

func TestLoadRejectsBadWhitelist(t *testing.T) {
	cfg := writeTemp(t, "c.yml", validConfig)
	cases := map[string]struct{ keys, wantSubstr string }{
		"empty":              {"", "allowed keys is empty"},
		"leaf not a mapping": {"mapping: true\n", `"mapping"`},
		"list in whitelist":  {"mapping: [a]\n", `"mapping"`},
		"duplicate key":      {"mapping: {}\nmapping: {}\n", "duplicate key"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := Load(cfg, writeTemp(t, "k.yml", tc.keys))
			if got != nil || err == nil || !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Fatalf("got %v, %v; want error containing %q", got, err, tc.wantSubstr)
			}
		})
	}
}
