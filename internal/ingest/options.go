package ingest

import (
	"fmt"
	"runtime"
)

const (
	MaxWorkers   = 1024
	MaxQueueSize = 1 << 16

	queuePerWorker    = 4
	defaultBatchQueue = 2
)

// Options tunes pipeline concurrency; zero means automatic.
type Options struct {
	ValidationWorkers int
	RawQueueSize      int
	ResultQueueSize   int
	BatchQueueSize    int
}

// Resolve replaces zero values with defaults and rejects out-of-range values.
func (o Options) Resolve() (Options, error) {
	var err error
	if o.ValidationWorkers, err = resolve("validation workers", o.ValidationWorkers, runtime.GOMAXPROCS(0), MaxWorkers); err != nil {
		return Options{}, err
	}
	queue := o.ValidationWorkers * queuePerWorker
	if o.RawQueueSize, err = resolve("raw queue size", o.RawQueueSize, queue, MaxQueueSize); err != nil {
		return Options{}, err
	}
	if o.ResultQueueSize, err = resolve("result queue size", o.ResultQueueSize, queue, MaxQueueSize); err != nil {
		return Options{}, err
	}
	if o.BatchQueueSize, err = resolve("batch queue size", o.BatchQueueSize, defaultBatchQueue, MaxQueueSize); err != nil {
		return Options{}, err
	}
	return o, nil
}

func resolve(name string, value, auto, max int) (int, error) {
	if value < 0 || value > max {
		return 0, fmt.Errorf("%s must be between 0 and %d (0 = automatic), got %d", name, max, value)
	}
	if value == 0 {
		return auto, nil
	}
	return value, nil
}
