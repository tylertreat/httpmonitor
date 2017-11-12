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
	close     chan struct{}
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
		close:     make(chan struct{}),
	}, nil
}

// Start collecting data, alerting, and writing summary data to stdout until
// the Monitor is closed.
func (m *Monitor) Start() error {
	go m.report()
	err := m.collector.Start(m.reader)
	m.Stop()
	return errors.Wrap(err, "failed to start collector")
}

// report prints summary data to stdout on the configured interval until the
// Monitor is closed.
func (m *Monitor) report() {
	c := time.Tick(m.opts.ReportingInterval)
	for {
		select {
		case <-c:
		case <-m.close:
			return
		}
		fmt.Println(m.summary())
	}
}

// Stop the Monitor. Once the Monitor has been stopped, it cannot be started
// again.
func (m *Monitor) Stop() error {
	if err := m.reader.Close(); err != nil {
		return errors.Wrap(err, "failed to close log reader")
	}
	close(m.close)
	return nil
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
