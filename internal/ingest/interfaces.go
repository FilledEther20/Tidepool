package ingest

import "context"

// RowSource yields records in file order and returns io.EOF after the last one.
type RowSource interface {
	Next() (RawRow, error)
}

// Validator must be safe for concurrent use; a non-nil RowError rejects the row.
type Validator interface {
	Validate(RawRow) (TypedRow, *RowError)
}

// BatchExecutor commits one batch atomically; dry-run implementations only build SQL.
type BatchExecutor interface {
	Execute(ctx context.Context, batch Batch) error
}

// DeadLetterSink receives rejected rows; implementations own their file and are called from one goroutine.
type DeadLetterSink interface {
	Write(RowError) error
	Close() error
}
