package config

import (
	"path/filepath"
	"sort"
	"strings"
)

const (
	HaltCodeConfig     = "CONFIG"
	HaltCodeDBConn     = "DB_CONN"
	HaltCodeBatchAbort = "BATCH_ABORT"
	RowCodeCast        = "CAST"
	RowCodeValidation  = "VALIDATION"
	RowCodeFK          = "FK"
)

var DeadLetterColumns = []string{
	"failed_at",
	"row_index",
	"record_identifier",
	"error_code",
	"error_message",
	"raw_payload",
}

type Binding struct {
	CSVHeader string
	DBColumn  string
	Column    Column
}

func orderedBindings(mapping map[string]string, schema *TableDefinition) []Binding {
	headers := make([]string, 0, len(mapping))
	for header := range mapping {
		headers = append(headers, header)
	}
	sort.Strings(headers)

	bindings := make([]Binding, 0, len(headers))
	for _, header := range headers {
		column := mapping[header]
		bindings = append(bindings, Binding{
			CSVHeader: header,
			DBColumn:  column,
			Column:    schema.Columns[column],
		})
	}
	return bindings
}

func (cfg *Config) BatchRowLimit() int {
	if len(cfg.Mapping) == 0 {
		return 0
	}
	return cfg.Runtime.MaxParameters / len(cfg.Mapping)
}

func ErrorsPath(inputPath string) string {
	base := filepath.Base(inputPath)
	if dot := strings.LastIndexByte(base, '.'); dot > 0 {
		base = base[:dot]
	}
	return filepath.Join(filepath.Dir(inputPath), base+"_errors.csv")
}
