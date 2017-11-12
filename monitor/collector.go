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

// statusFreq tracks frequencies of HTTP status codes.
type statusFreq struct {
	Informational uint64
	Successful    uint64
	Redirection   uint64
	ClientError   uint64
	ServerError   uint64
}

// collector receives logs from a Reader and tracks summary statistics.
type collector struct {
	sync.RWMutex
	topSections *boom.TopK
	ipHll       *boom.HyperLogLog
	count       uint64
	sizeHist    *hdrhistogram.WindowedHistogram
	statusFreq  statusFreq
	averager    *windowedAverager
}

// newCollector creates a collector used to receive and summarize log data.
func newCollector(numTopSections uint, window, quantum time.Duration) *collector {
	ipHll, _ := boom.NewDefaultHyperLogLog(0.01)
	return &collector{
		topSections: boom.NewTopK(0.001, 0.99, numTopSections),
		ipHll:       ipHll,
		sizeHist:    hdrhistogram.NewWindowed(3, 1, maxRecordableSize, 5),
		averager:    newWindowedAverager(window, quantum),
	}
}

// Start collecting logs from the Reader and performing summary statistics.
// This runs until the reader is closed.
func (c *collector) Start(reader reader) error {
	logs, err := reader.Open()
	if err != nil {
		return errors.Wrap(err, "failed to open Reader")
	}

	hits := make(chan time.Time, 1024)
	go c.averager.quantize(hits)

	for l := range logs {
		c.process(l, hits)
	}

	close(hits)
	return nil
}

// process a single log.
func (c *collector) process(l *log, hits chan<- time.Time) {
	c.Lock()
	c.count++
	hits <- l.timestamp
	c.processRequest(l.request)
	c.processIP(l.remoteAddr)
	c.processSize(l.size)
	c.processStatus(l.status)
	c.Unlock()
}

// processIP updates summary data pertaining to the remote IP address.
func (c *collector) processIP(ip string) {
	// Count distinct.
	c.ipHll.Add([]byte(ip))
}

// processSize updates summary data pertaining to the response size.
func (c *collector) processSize(size int64) {
	c.sizeHist.Current.RecordValue(int64(size))
	if c.count%100000 == 0 {
		c.sizeHist.Rotate()
	}
}

// processStatus updates summary data pertaining to the request status.
func (c *collector) processStatus(status int) {
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
func (c *collector) processRequest(request string) {
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
