package config

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestParseSchemaFixture(t *testing.T) {
	raw, err := os.ReadFile("../../schema_db.sql")
	if err != nil {
		t.Fatal(err)
	}
	table, err := parseSchema(raw)
	if err != nil {
		t.Fatal(err)
	}
	if table.Schema != "" || table.Name != "enterprise_banking_ledger" || len(table.Columns) != 19 {
		t.Fatalf("unexpected fixture metadata: %+v", table)
	}
	if got := table.Columns["geo_long"]; got.Type != "numeric" || got.Precision != 9 || got.Scale != 6 {
		t.Errorf("unexpected geo_long metadata: %+v", got)
	}
	if got := table.Columns["amount"]; got.Type != "numeric" || got.Precision != 18 || got.Scale != 4 {
		t.Errorf("unexpected amount metadata: %+v", got)
	}
	if table.Columns["txn_id"].Nullable || !reflect.DeepEqual(table.UniqueKeys, [][]string{{"txn_id"}}) {
		t.Fatalf("unexpected primary key metadata: %+v", table)
	}
}

func TestParseSchemaTypesAndConstraints(t *testing.T) {
	raw := `-- leading comment
	 cReAtE TaBlE IF NOT EXISTS public.ledger (
	 id UUID NOT NULL, account VARCHAR(/* size */50) UNIQUE,
	 amount DECIMAL(18, /* comma is part of the type */4),
	 ratio NUMERIC(9,0), count INT, other INTEGER NULL,
	 active BOOLEAN, created TIMESTAMP WITHOUT TIME ZONE,
	 cleared TIMESTAMP, metadata JSONB,
	 CONSTRAINT ledger_pk PRIMARY KEY(id, count),
	 CONSTRAINT ledger_uq UNIQUE (account, amount),
	 UNIQUE (ratio, other)
	 ); /* trailing /* nested */ comment */ -- done`
	table, err := parseSchema([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if table.Schema != "public" || table.Name != "ledger" {
		t.Fatalf("unexpected table: %+v", table)
	}
	want := map[string]Column{
		"id":       {Name: "id", Type: "uuid"},
		"account":  {Name: "account", Type: "varchar", Length: 50, Nullable: true},
		"amount":   {Name: "amount", Type: "numeric", Precision: 18, Scale: 4, Nullable: true},
		"ratio":    {Name: "ratio", Type: "numeric", Precision: 9, Nullable: true},
		"count":    {Name: "count", Type: "integer"},
		"other":    {Name: "other", Type: "integer", Nullable: true},
		"active":   {Name: "active", Type: "boolean", Nullable: true},
		"created":  {Name: "created", Type: "timestamp", Nullable: true},
		"cleared":  {Name: "cleared", Type: "timestamp", Nullable: true},
		"metadata": {Name: "metadata", Type: "jsonb", Nullable: true},
	}
	if !reflect.DeepEqual(table.Columns, want) {
		t.Errorf("columns = %#v; want %#v", table.Columns, want)
	}
	keys := [][]string{{"account"}, {"id", "count"}, {"account", "amount"}, {"ratio", "other"}}
	if !reflect.DeepEqual(table.UniqueKeys, keys) {
		t.Errorf("keys = %v; want %v", table.UniqueKeys, keys)
	}
}

func TestParseSchemaAcceptedBoundaries(t *testing.T) {
	for _, raw := range []string{
		"create table t (id uuid primary key not null unique)",
		"CREATE TABLE t (id uuid NOT NULL PRIMARY KEY);",
		"CREATE TABLE t (a varchar(1), b varchar(10485760), c numeric(1,1), d decimal(1000,1000))",
		"CREATE TABLE t (CONSTRAINT pk PRIMARY KEY (a), a integer)",
		"CREATE TABLE _table2 (_id2 integer NULL UNIQUE)",
		"CREATE/* comment */TABLE t (a integer)-- trailing",
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := parseSchema([]byte(raw)); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestParseSchemaRejectsUnsupportedAndInvalidSQL(t *testing.T) {

	cases := map[string]string{
		"empty":                    "",
		"comment only":             "-- no table",
		"braces":                   "CREATE TABLE t {a INT}",
		"empty table":              "CREATE TABLE t ()",
		"constraint only":          "CREATE TABLE t (UNIQUE(a))",
		"uppercase table":          "CREATE TABLE T (a INT)",
		"uppercase schema":         "CREATE TABLE Public.t (a INT)",
		"uppercase column":         "CREATE TABLE t (A INT)",
		"uppercase constraint":     "CREATE TABLE t (a INT, CONSTRAINT UQ UNIQUE(a))",
		"uppercase key column":     "CREATE TABLE t (a INT, UNIQUE(A))",
		"quoted table":             `CREATE TABLE "t" (a INT)`,
		"quoted column":            "CREATE TABLE t (`a` INT)",
		"qualified column":         "CREATE TABLE t (x.a INT)",
		"three part table":         "CREATE TABLE db.public.t (a INT)",
		"default":                  "CREATE TABLE t (a INT DEFAULT 1)",
		"inline check":             "CREATE TABLE t (a INT CHECK(a > 0))",
		"table check":              "CREATE TABLE t (a INT, CHECK(a))",
		"references":               "CREATE TABLE t (a INT REFERENCES other(a))",
		"foreign key":              "CREATE TABLE t (a INT, FOREIGN KEY(a) REFERENCES other(a))",
		"named check":              "CREATE TABLE t (a INT, CONSTRAINT c CHECK(a))",
		"inline named constraint":  "CREATE TABLE t (a INT CONSTRAINT pk PRIMARY KEY)",
		"unknown type":             "CREATE TABLE t (a TEXT)",
		"timestamp time zone":      "CREATE TABLE t (a TIMESTAMP WITH TIME ZONE)",
		"timestamp precision":      "CREATE TABLE t (a TIMESTAMP(6))",
		"incomplete timestamp":     "CREATE TABLE t (a TIMESTAMP WITHOUT TIME)",
		"array":                    "CREATE TABLE t (a INT[])",
		"varchar missing length":   "CREATE TABLE t (a VARCHAR)",
		"varchar zero":             "CREATE TABLE t (a VARCHAR(0))",
		"varchar too large":        "CREATE TABLE t (a VARCHAR(10485761))",
		"varchar negative":         "CREATE TABLE t (a VARCHAR(-1))",
		"varchar fractional":       "CREATE TABLE t (a VARCHAR(1.5))",
		"varchar overflow":         "CREATE TABLE t (a VARCHAR(999999999999999999999999))",
		"numeric missing scale":    "CREATE TABLE t (a NUMERIC(10))",
		"numeric zero precision":   "CREATE TABLE t (a NUMERIC(0,0))",
		"numeric large precision":  "CREATE TABLE t (a NUMERIC(1001,0))",
		"numeric negative scale":   "CREATE TABLE t (a NUMERIC(10,-1))",
		"numeric excessive scale":  "CREATE TABLE t (a NUMERIC(10,11))",
		"numeric missing comma":    "CREATE TABLE t (a NUMERIC(10 2))",
		"duplicate column":         "CREATE TABLE t (a INT, a UUID)",
		"duplicate inline pk":      "CREATE TABLE t (a INT PRIMARY KEY PRIMARY KEY)",
		"two inline pk":            "CREATE TABLE t (a INT PRIMARY KEY, b INT PRIMARY KEY)",
		"inline and table pk":      "CREATE TABLE t (a INT PRIMARY KEY, PRIMARY KEY(a))",
		"table and inline pk":      "CREATE TABLE t (PRIMARY KEY(a), a INT PRIMARY KEY)",
		"two table pk":             "CREATE TABLE t (a INT, PRIMARY KEY(a), PRIMARY KEY(a))",
		"duplicate inline unique":  "CREATE TABLE t (a INT UNIQUE UNIQUE)",
		"repeated null":            "CREATE TABLE t (a INT NULL NULL)",
		"repeated not null":        "CREATE TABLE t (a INT NOT NULL NOT NULL)",
		"contradictory null":       "CREATE TABLE t (a INT NULL NOT NULL)",
		"contradictory not null":   "CREATE TABLE t (a INT NOT NULL NULL)",
		"null before pk":           "CREATE TABLE t (a INT NULL PRIMARY KEY)",
		"null after pk":            "CREATE TABLE t (a INT PRIMARY KEY NULL)",
		"table pk nullable":        "CREATE TABLE t (a INT NULL, PRIMARY KEY(a))",
		"forward pk nullable":      "CREATE TABLE t (PRIMARY KEY(a), a INT NULL)",
		"duplicate key column":     "CREATE TABLE t (a INT, UNIQUE(a,a))",
		"duplicate pk column":      "CREATE TABLE t (a INT, PRIMARY KEY(a,a))",
		"missing key column":       "CREATE TABLE t (a INT, UNIQUE(b))",
		"missing primary column":   "CREATE TABLE t (a INT, PRIMARY KEY(b))",
		"duplicate constraint":     "CREATE TABLE t (a INT, CONSTRAINT uq UNIQUE(a), CONSTRAINT uq UNIQUE(a))",
		"empty key":                "CREATE TABLE t (a INT, UNIQUE())",
		"key expression":           "CREATE TABLE t (a INT, UNIQUE(lower(a)))",
		"key sort direction":       "CREATE TABLE t (a INT, UNIQUE(a DESC))",
		"deferrable":               "CREATE TABLE t (a INT, UNIQUE(a) DEFERRABLE)",
		"trailing comma":           "CREATE TABLE t (a INT,)",
		"missing comma":            "CREATE TABLE t (a INT b INT)",
		"missing close":            "CREATE TABLE t (a INT",
		"missing type":             "CREATE TABLE t (a)",
		"bad if exists":            "CREATE TABLE IF EXISTS t (a INT)",
		"unterminated comment":     "CREATE TABLE t (a INT) /*",
		"comment splits keyword":   "CRE/* x */ATE TABLE t (a INT)",
		"extra statement":          "CREATE TABLE t (a INT); CREATE TABLE u (b INT)",
		"extra semicolon":          "CREATE TABLE t (a INT);;",
		"trailing storage options": "CREATE TABLE t (a INT) WITH (fillfactor=70)",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			table, err := parseSchema([]byte(raw))
			if err == nil || table != nil {
				t.Fatalf("expected nil table and error; got %+v, %v", table, err)
			}
			var configErr *ConfigError
			if !errors.As(err, &configErr) || configErr.Code != "CONFIG" {
				t.Fatalf("expected CONFIG error, got %T: %v", err, err)
			}
			if !strings.Contains(err.Error(), "schema at byte") {
				t.Errorf("error lacks schema context: %v", err)
			}
		})
	}
}
