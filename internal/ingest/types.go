package ingest

import "time"

// RawRow is one CSV record. Line is the physical file line where the record starts (header = 1).
type RawRow struct {
	Seq    uint64
	Line   int
	Fields []string
}

// TypedRow holds values in config.Bindings order, ready to be used as query arguments.
type TypedRow struct {
	Seq    uint64
	Line   int
	Values []any
	Raw    []string
}

// RowError is a row-level fault destined for the dead-letter file; Code is a config.RowCode* value.
type RowError struct {
	Seq     uint64
	Line    int
	Code    string
	Message string
	Raw     []string
}

// Batch is a group of valid rows that fits in one INSERT statement.
type Batch struct {
	Rows []TypedRow
}

// Receipt summarizes a finished or aborted run.
type Receipt struct {
	RowsRead      int64
	RowsValid     int64
	RowsRejected  int64
	RowsCommitted int64
	Batches       int64
	Duration      time.Duration
	ErrorsPath    string
}

func (r Receipt) RowsPerSecond() float64 {
	if r.Duration <= 0 {
		return 0
	}
	return float64(r.RowsRead) / r.Duration.Seconds()
}
