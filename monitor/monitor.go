package monitor

import (
	"fmt"
	"time"

	"github.com/codahale/hdrhistogram"
	"github.com/pkg/errors"
)

// MonitorOpts contains options for configuring a Monitor.
type MonitorOpts struct {
	NumTopSections    uint
	AlertWindow       time.Duration
	AlertThreshold    float64
	ReportingInterval time.Duration
}

// Monitor reads, parses, and collects HTTP traffic data from a configured log
// file. It also provides alerting functionality.
type Monitor struct {
	reader    reader
	collector *collector
	opts      MonitorOpts
}

// New creates a new Monitor that collects data from the given HTTP log file in
// Common Log Format.
func New(file string, opts MonitorOpts) (*Monitor, error) {
	reader, err := NewCommonLogFormatReader(file)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create log file reader")
	}
	collector := newCollector(opts.NumTopSections, opts.AlertWindow)
	return &Monitor{
		reader:    reader,
		collector: collector,
		opts:      opts,
	}, nil
}

// Start collecting data and alerting. Summary data is written to stdout.
func (m *Monitor) Start() error {
	go func() {
		c := time.Tick(m.opts.ReportingInterval)
		for _ = range c {
			fmt.Println(m.summary())
		}
	}()

	err := m.collector.Start(m.reader)
	return errors.Wrap(err, "failed to start collector")
}

// Stop the Monitor.
func (m *Monitor) Stop() error {
	return m.reader.Close()
}

// summary returns a point-in-time snapshot of the data.
func (m *Monitor) summary() *Summary {
	s := &Summary{Timestamp: time.Now()}
	m.collector.RLock()
	defer m.collector.RUnlock()

	s.TopSections = m.collector.topSections.Elements()
	s.DistinctIPs = m.collector.ipHll.Count()
	s.SizeHist = hdrhistogram.Import(m.collector.sizeHist.Merge().Export())
	s.StatusFreq = m.collector.statusFreq
	s.HitsPerSecond = m.collector.averager.latest()
	s.AvgHits = m.collector.averager.average()
	s.Window = m.opts.AlertWindow
	return s
}
