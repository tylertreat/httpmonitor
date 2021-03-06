// Package monitor provides an API for monitoring HTTP traffic processed from a
// configured HTTP log file. The Monitor provides facilities for reading logs,
// aggregating traffic data, and alerting on traffic events.
package monitor

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/codahale/hdrhistogram"
	"github.com/pkg/errors"
)

// quantum is the granularity of time-series measurements.
const quantum = time.Second

// Alert is used to emit traffic alert notifications.
type Alert struct {
	Recovered bool
	AvgHits   float64
	Time      time.Time
}

// MonitorOpts contains options for configuring a Monitor.
type MonitorOpts struct {
	NumTopSections    uint
	AlertWindow       time.Duration
	AlertThreshold    float64
	AlertHook         chan<- Alert
	ReportingInterval time.Duration
	Output            io.Writer
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
	if opts.Output == nil {
		opts.Output = os.Stdout
	}
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

// Start collecting data, alerting, and writing summary data until the Monitor
// is closed. This is a blocking call.
func (m *Monitor) Start() error {
	go m.report()
	go m.alert()
	err := m.collector.Start(m.reader)
	m.Stop()
	return errors.Wrap(err, "failed to start collector")
}

// report prints summary data on the configured interval until the Monitor is
// closed.
func (m *Monitor) report() {
	// Don't report if the interval is zero.
	if m.opts.ReportingInterval <= 0 {
		return
	}
	t := time.NewTicker(m.opts.ReportingInterval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
		case <-m.close:
			return
		}
		fmt.Fprintln(m.opts.Output, m.summary())
	}
}

// alert writes a message when traffic exceeds the alert threshold on average
// within the alert window. When traffic drops back below the threshold, it
// writes a recovered message. It does this until the Monitor is closed.
func (m *Monitor) alert() {
	var (
		t       = time.NewTicker(quantum * 2)
		alerted = false
	)
	defer t.Stop()
	for {
		select {
		case <-t.C:
		case <-m.close:
			return
		}
		var (
			avg = m.averager.average()
			now = time.Now()
		)
		if avg > m.opts.AlertThreshold && !alerted {
			fmt.Fprintf(m.opts.Output, "High traffic generated an alert - hits = %.2f, triggered at %s\n",
				avg, now)
			alerted = true
			select {
			case m.opts.AlertHook <- Alert{AvgHits: avg, Time: now}:
			default:
			}
		} else if avg <= m.opts.AlertThreshold && alerted {
			fmt.Fprintf(m.opts.Output, "Traffic recovered - hits = %.2f, recovered at %s\n", avg, now)
			alerted = false
			select {
			case m.opts.AlertHook <- Alert{Recovered: true, AvgHits: avg, Time: now}:
			default:
			}
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
