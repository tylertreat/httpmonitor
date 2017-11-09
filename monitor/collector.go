package monitor

import (
	"sync"

	"github.com/pkg/errors"
	"github.com/tylertreat/BoomFilters"
)

// Collector receives Logs from a Reader and tracks summary statistics.
type Collector struct {
	topk *boom.TopK
	mu   sync.RWMutex
}

// NewCollector creates a Collector used to receive and summarize Log data.
func NewCollector() *Collector {
	return &Collector{
		topk: boom.NewTopK(0.001, 0.99, 5),
	}
}

// Start collecting Logs from the Reader and performing summary statistics.
// This runs until the Reader is closed.
func (c *Collector) Start(reader Reader) error {
	logs, err := reader.Open()
	if err != nil {
		return errors.Wrap(err, "failed to open Reader")
	}

	for l := range logs {
		c.process(l)
	}

	return nil
}

// TopSectionHits returns the top five sections with the most hits. A section
// is defined as being what's before the second '/' in a URL, i.e. the section
// for "http://my.site.com/pages/create' is "http://my.site.com/pages".
func (c *Collector) TopSectionHits() []*boom.Element {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.topk.Elements()
}

// process a single Log.
func (c *Collector) process(l *Log) {
	c.mu.Lock()
	c.topk.Add([]byte(l.Request))
	c.mu.Unlock()
}
