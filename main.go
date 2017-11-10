package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tylertreat/httpmonitor/monitor"
)

func main() {
	var (
		file string
		opts monitor.CollectorOpts
	)
	flag.StringVar(&file, "file", "", "Log file to read from")
	flag.UintVar(&opts.NumTopSections, "sections", 5, "Number of top sections to display")
	flag.Parse()

	if file == "" {
		log.Fatal("Must provide --file flag")
	}

	reader, err := monitor.NewCommonLogFormatReader(file)
	if err != nil {
		log.Fatalf("Failed to create log file reader: %v", err)
	}

	handleSignals(reader)

	collector := monitor.NewCollector(opts)
	go func() {
		c := time.Tick(10 * time.Second)
		for _ = range c {
			fmt.Println(collector.Summary())
		}
	}()

	fmt.Println("Starting monitor...")
	if err := collector.Start(reader); err != nil {
		log.Fatalf("Failed to start log collector: %v", err)
	}
}

func handleSignals(reader monitor.Reader) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-c
		log.Println("Stopping monitor...")
		if err := reader.Close(); err != nil {
			log.Fatalf("Error closing log file reader: %v", err)
		}
		os.Exit(0)
	}()
}
