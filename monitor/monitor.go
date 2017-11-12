package monitor

import (
	"fmt"
	"time"

	"github.com/codahale/hdrhistogram"
	"github.com/pkg/errors"
)

// quantum is the granularity of time-series measurements.
const quantum = time.Second

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
	*collector
	reader reader
	opts   MonitorOpts
	close  chan struct{}
}

// New creates a new Monitor that collects data from the given HTTP log file in
// Common Log Format.
func New(file string, opts MonitorOpts) (*Monitor, error) {
	reader, err := NewCommonLogFormatReader(file)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create log file reader")
	}
	collector := newCollector(opts.NumTopSections, opts.AlertWindow, quantum)
	return &Monitor{
		collector: collector,
		reader:    reader,
		opts:      opts,
		close:     make(chan struct{}),
	}, nil
}

// Start collecting data, alerting, and writing summary data to stdout until
// the Monitor is closed.
func (m *Monitor) Start() error {
	go m.report()
	go m.alert()
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

// alert writes a message to stdout when traffic exceeds the alert threshold on
// average within the alert window. When traffic drops back below the threshold,
// it writes a recovered message. It does this until the Monitor is closed.
func (m *Monitor) alert() {
	var (
		c       = time.Tick(quantum * 2)
		alerted = false
	)
	for {
		select {
		case <-c:
		case <-m.close:
			return
		}
		var (
			avg = m.averager.average()
			now = time.Now()
		)
		if avg > m.opts.AlertThreshold && !alerted {
			fmt.Printf("High traffic generated an alert - hits = %.2f, triggered at %s\n", avg, now)
			alerted = true
		} else if avg <= m.opts.AlertThreshold && alerted {
			fmt.Printf("Traffic recovered - hits = %.2f, recovered at %s\n", avg, now)
			alerted = false
		}
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
	m.RLock()
	defer m.RUnlock()

	s.TopSections = m.topSections.Elements()
	s.DistinctIPs = m.ipHll.Count()
	s.SizeHist = hdrhistogram.Import(m.sizeHist.Merge().Export())
	s.StatusFreq = m.statusFreq
	s.HitsPerSecond = m.averager.latest()
	s.AvgHits = m.averager.average()
	s.Window = m.opts.AlertWindow
	return s
}
