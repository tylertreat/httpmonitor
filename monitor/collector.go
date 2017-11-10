package monitor

import (
	"regexp"
	"sync"
	"time"

	"github.com/codahale/hdrhistogram"
	"github.com/pkg/errors"
	"github.com/tylertreat/BoomFilters"
)

const (
	// numRequestParts is the number of components in the request line.
	numRequestParts = 4

	// maxRecordableSize is the maximum recordable size of a response.
	maxRecordableSize = 1000000000000
)

// requestRegexp matches the HTTP request line, e.g. "GET /index.html HTTP/1.1".
var requestRegexp = regexp.MustCompile(`(\S+)\s+([^?\s]+)((?:[?&][^&\s]+)*)\s+(HTTP\/.*)`)

// StatusFreq tracks frequencies of HTTP status codes.
type StatusFreq struct {
	Informational uint64
	Successful    uint64
	Redirection   uint64
	ClientError   uint64
	ServerError   uint64
}

// CollectorOpts contains options for configuring a Collector.
type CollectorOpts struct {
	NumTopSections uint
}

// Collector receives Logs from a Reader and tracks summary statistics.
type Collector struct {
	mu          sync.RWMutex
	topSections *boom.TopK
	ipHll       *boom.HyperLogLog
	count       uint64
	sizeHist    *hdrhistogram.WindowedHistogram
	statusFreq  StatusFreq
}

// NewCollector creates a Collector used to receive and summarize Log data.
func NewCollector(opts CollectorOpts) *Collector {
	ipHll, _ := boom.NewDefaultHyperLogLog(0.01)
	return &Collector{
		topSections: boom.NewTopK(0.001, 0.99, opts.NumTopSections),
		ipHll:       ipHll,
		sizeHist:    hdrhistogram.NewWindowed(3, 1, maxRecordableSize, 5),
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

// Summary returns a point-in-time snapshot of the data.
func (c *Collector) Summary() *Summary {
	s := &Summary{Timestamp: time.Now()}
	c.mu.RLock()
	defer c.mu.RUnlock()

	s.TopSections = c.topSections.Elements()
	s.DistinctIPs = c.ipHll.Count()
	s.SizeHist = hdrhistogram.Import(c.sizeHist.Merge().Export())
	s.StatusFreq = c.statusFreq
	return s
}

// TopSectionHits returns the top five sections with the most hits.
func (c *Collector) TopSectionHits() []*boom.Element {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.topSections.Elements()
}

// process a single Log.
func (c *Collector) process(l *Log) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.count++
	c.processRequest(l.Request)
	c.processIP(l.RemoteAddr)
	c.processSize(l.Size)
	c.processStatus(l.Status)
}

// processIP updates summary data pertaining to the remote IP address.
func (c *Collector) processIP(ip string) {
	// Count distinct.
	c.ipHll.Add([]byte(ip))
}

// processSize updates summary data pertaining to the response size.
func (c *Collector) processSize(size int64) {
	c.sizeHist.Current.RecordValue(int64(size))
	if c.count%100000 == 0 {
		c.sizeHist.Rotate()
	}
}

// processStatus updates summary data pertaining to the request status.
func (c *Collector) processStatus(status int) {
	switch {
	case status >= 100 && status < 200:
		c.statusFreq.Informational++
	case status >= 200 && status < 300:
		c.statusFreq.Successful++
	case status >= 300 && status < 400:
		c.statusFreq.Redirection++
	case status >= 400 && status < 500:
		c.statusFreq.ClientError++
	case status >= 500 && status < 600:
		c.statusFreq.ServerError++
	}
}

// processRequest updates summary data pertaining to the request line.
func (c *Collector) processRequest(request string) {
	if !requestRegexp.MatchString(request) {
		// Malformed request, skip it.
		return
	}
	parts := requestRegexp.FindStringSubmatch(request)
	// Add 1 because the first part is the entire expression.
	if len(parts) != numRequestParts+1 {
		// Malformed request, skip it.
		return
	}

	// Summarize section. A section is defined as being what's before the
	// second '/' in a URL, i.e. the section for "/pages/create" is "/pages".
	section := sectionFromDocument(parts[2])
	c.topSections.Add([]byte(section))
}

// sectionFromDocument gets the section from a full document URL.
func sectionFromDocument(document string) string {
	slashIndexes := []int{}
	for i, c := range document {
		if c == '/' {
			slashIndexes = append(slashIndexes, i)
		}
	}
	switch len(slashIndexes) {
	case 0, 1:
		return "/"
	default:
		return document[:slashIndexes[1]]
	}
}
