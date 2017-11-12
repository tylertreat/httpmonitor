package monitor

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

const (
	testAlertWindow    = 2 * time.Second
	testAlertThreshold = 10.0
	dummyLog           = "::1 - - [%s] \"GET /customers/directory.html HTTP/1.1\" 200 17\n"
)

// rateInterval specifies a rate at which to produce logs and for how long.
type rateInterval struct {
	rate     rate.Limit
	duration time.Duration
}

// TestMonitorAlerting ensures alerting logic is triggered when the average
// traffic for the alert window exceeds the threshold, and it recovers when
// traffic drops back below the threshold.
func TestMonitorAlerting(t *testing.T) {
	var (
		alerts = make(chan Alert, 1)
		opts   = MonitorOpts{
			AlertWindow:    testAlertWindow,
			AlertThreshold: testAlertThreshold,
			AlertHook:      alerts,
			NumTopSections: 1,
			Output:         ioutil.Discard,
		}
	)

	// Setup temp log file.
	file, err := ioutil.TempFile("", "access_log")
	if err != nil {
		t.Fatalf("Error creating log file: %v", err)
	}
	defer os.Remove(file.Name())

	// Setup Monitor.
	m, err := New(file.Name(), opts)
	if err != nil {
		t.Fatalf("Error creating Monitor: %v", err)
	}
	go m.Start()
	defer m.Stop()

	// Start writing dummy logs.
	stop := make(chan struct{})
	defer func() { close(stop) }()
	go generateLogs(file, stop, []rateInterval{
		rateInterval{rate: 5, duration: 2 * time.Second},
		rateInterval{rate: 20, duration: 2 * time.Second},
		rateInterval{rate: 2, duration: 2 * time.Second},
	})

	// We shouldn't receive any alerts for the first 2 seconds.
	select {
	case <-alerts:
		t.Fatal("Unexpected alert triggered")
	case <-time.After(2 * time.Second):
	}

	// We should receive an alert.
	select {
	case a := <-alerts:
		if a.Recovered {
			t.Fatal("Expected alert triggered, got recovery")
		}
		if a.AvgHits <= testAlertThreshold {
			t.Fatalf("Expected avg greater than %f, got %f", testAlertThreshold, a.AvgHits)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Expected alert triggered")
	}

	// We should receive a recovery.
	select {
	case a := <-alerts:
		if !a.Recovered {
			t.Fatal("Expected alert recovery")
		}
		if a.AvgHits > testAlertThreshold {
			t.Fatalf("Expected avg less than or equal to %f, got %f", testAlertThreshold, a.AvgHits)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Expected alert recovery")
	}
}

// generateLogs writes dummy logs to the given file for each of the
// rateIntervals in sequential order.
func generateLogs(file *os.File, stop <-chan struct{}, rateConfig []rateInterval) {
	defer file.Close()
	ctx := context.Background()
LOOP:
	for _, c := range rateConfig {
		var (
			limiter  = rate.NewLimiter(c.rate, 1)
			deadline = time.After(c.duration)
		)
		for {
			select {
			case <-deadline:
				continue LOOP
			case <-stop:
				return
			default:
			}
			limiter.Wait(ctx)
			file.WriteString(fmt.Sprintf(dummyLog, time.Now().Format("02/Jan/2006:15:04:05 -0700")))
		}
	}
}
