package monitor

import (
	"bytes"
	"fmt"
	"strconv"
	"time"

	"github.com/codahale/hdrhistogram"
	"github.com/olekukonko/tablewriter"
	"github.com/tylertreat/BoomFilters"
)

// Summary is a point-in-time snapshot of the traffic data.
type Summary struct {
	Timestamp     time.Time
	TopSections   []*boom.Element
	DistinctIPs   uint64
	SizeHist      *hdrhistogram.Histogram
	StatusFreq    statusFreq
	HitsPerSecond uint64
	AvgHits       float64
	Window        time.Duration
}

// String returns a string representation of the summary suitable for printing.
func (s *Summary) String() string {
	str := fmt.Sprintf("===== SUMMARY [%s] =================>\n", s.Timestamp.Format("01/02/06 15:04:05"))
	str += s.topHitsString()
	str += fmt.Sprintf("Unique visitors:\t%d\n", s.DistinctIPs)
	str += fmt.Sprintf("Hits/s:\t\t\t%d\n", s.HitsPerSecond)
	str += fmt.Sprintf("Mean hits (%s):\t%.2f\n", s.Window, s.AvgHits)
	str += "------- Responses -----------------------\n"
	str += fmt.Sprintf("1xx: %d, 2xx: %d, 3xx: %d, 4xx: %d, 5xx: %d\n",
		s.StatusFreq.Informational,
		s.StatusFreq.Successful,
		s.StatusFreq.Redirection,
		s.StatusFreq.ClientError,
		s.StatusFreq.ServerError,
	)
	str += fmt.Sprintf("Min response size:\t%dB\n", s.SizeHist.Min())
	str += fmt.Sprintf("Median response size:\t%dB\n", s.SizeHist.ValueAtQuantile(50))
	str += fmt.Sprintf("Max response size:\t%dB\n", s.SizeHist.Max())
	str += fmt.Sprintf("p99 response size:\t%dB\n", s.SizeHist.ValueAtQuantile(99))
	str += fmt.Sprintf("Mean response size:\t%.2fB\n", s.SizeHist.Mean())
	str += fmt.Sprintf("Response size std dev:\t%.2fB\n", s.SizeHist.StdDev())
	str += "-----------------------------------------\n"
	return str
}

// topHitsString returns a table containing the most frequently visited
// sections in table form.
func (s *Summary) topHitsString() string {
	var buf bytes.Buffer
	table := tablewriter.NewWriter(&buf)
	table.SetHeader([]string{"Section", "Hits"})
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	data := [][]string{}
	for i := len(s.TopSections) - 1; i >= 0; i-- {
		element := s.TopSections[i]
		data = append(data, []string{string(element.Data), strconv.FormatInt(int64(element.Freq), 10)})
	}
	table.AppendBulk(data)
	table.Render()
	return buf.String()
}
