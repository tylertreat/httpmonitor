package monitor

import (
	"sync"
	"time"
)

// windowedAverager is used to compute the average number of hits across a
// configured window of time.
type windowedAverager struct {
	mu      sync.RWMutex
	buckets []*uint64 // use pointers to indicate lack of data points.
	quantum time.Duration
	window  time.Duration
	idx     int
}

// newWindowedAverager creates a new windowAverager which allows computing the
// average number of hits for the given window of time and quantized by the
// given quantum.
func newWindowedAverager(window, quantum time.Duration) *windowedAverager {
	if window < quantum {
		panic("window may not be less than quantum")
	}
	return &windowedAverager{
		// Add 1 since we won't include the current bucket when averaging.
		buckets: make([]*uint64, int(window/quantum)+1),
		quantum: quantum,
		window:  window,
	}
}

// quantize starts a loop that reads the hit timestamps from the given channel
// and places them into buckets until the channel is closed.
func (w *windowedAverager) quantize(hits <-chan time.Time) {
	stop := make(chan struct{})
	go w.tick(stop)
	for hit := range hits {
		if hit.Before(time.Now().Add(-w.window)) {
			continue
		}
		w.mu.Lock()
		if w.buckets[w.idx] == nil {
			x := uint64(0)
			w.buckets[w.idx] = &x
		}
		*w.buckets[w.idx]++
		w.mu.Unlock()
	}
	close(stop)
}

// tick starts a loop that updates the current bucket based on the quantum
// until the given channel is closed.
func (w *windowedAverager) tick(stop <-chan struct{}) {
	t := time.NewTicker(w.quantum)
	defer t.Stop()
	for {
		select {
		case <-t.C:
		case <-stop:
			return
		}
		w.mu.Lock()
		w.idx = (w.idx + 1) % len(w.buckets)
		x := uint64(0)
		w.buckets[w.idx] = &x
		w.mu.Unlock()
	}
}

// average returns the average hit rate for the configured window of time.
func (w *windowedAverager) average() float64 {
	w.mu.RLock()
	sum := uint64(0)
	count := 0
	for i, b := range w.buckets {
		if i == w.idx {
			// Skip the current bucket.
			continue
		}
		if b != nil {
			sum += *b
			count++
		}
	}
	w.mu.RUnlock()
	return float64(sum) / (float64(count) * w.quantum.Seconds())
}

// latest returns the number of hits for the last quantum of time, e.g. if the
// quantum is 1s, this returns the current hits/s.
func (w *windowedAverager) latest() uint64 {
	w.mu.RLock()
	defer w.mu.RUnlock()
	lastIdx := ((w.idx - 1) + len(w.buckets)) % len(w.buckets)
	latest := w.buckets[lastIdx]
	if latest == nil {
		return 0
	}
	return *latest
}
