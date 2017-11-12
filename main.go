package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tylertreat/httpmonitor/monitor"
)

const (
	defaultReportingInterval = 10 * time.Second
	defaultAlertWindow       = 2 * time.Minute
	defaultAlertThreshold    = 50
)

func main() {
	var (
		file string
		opts = monitor.MonitorOpts{Output: os.Stdout}
	)
	flag.StringVar(&file, "file", "", "Log file to read from")
	flag.UintVar(&opts.NumTopSections, "sections", 5, "Number of top sections to display")
	flag.Float64Var(&opts.AlertThreshold, "alert-threshold", defaultAlertThreshold,
		"Alert whenever traffic exceeds this value on average within alert-window")
	flag.DurationVar(&opts.AlertWindow, "alert-window", defaultAlertWindow,
		"Alert whenever traffic exceeds alert-threshold within this window on average")
	flag.DurationVar(&opts.ReportingInterval, "reporting-interval", defaultReportingInterval,
		"Interval at which to report summary data")
	flag.Parse()

	if file == "" {
		fmt.Println("Must provide --file flag")
		os.Exit(1)
	}

	m, err := monitor.New(file, opts)
	if err != nil {
		fmt.Printf("Failed to create monitor: %v\n", err)
		os.Exit(1)
	}

	handleSignals(m)

	fmt.Println("Starting monitor...")
	if err := m.Start(); err != nil {
		fmt.Printf("Failed to start monitor: %v\n", err)
		os.Exit(1)
	}
}

func handleSignals(m *monitor.Monitor) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-c
		fmt.Println("Stopping monitor...")
		if err := m.Stop(); err != nil {
			fmt.Printf("Error stopping monitor: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}()
}
