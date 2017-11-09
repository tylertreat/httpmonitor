package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/tylertreat/httpmonitor/monitor"
)

func main() {
	var file string
	flag.StringVar(&file, "file", "", "Log file to read from")
	flag.Parse()

	if file == "" {
		log.Fatal("Must provide --file flag")
	}

	f, err := os.Open(file)
	if err != nil {
		log.Fatalf("Failed to open log file %s: %v", file, err)
	}

	reader, err := monitor.NewCommonLogFormatReader(f)
	if err != nil {
		log.Fatalf("Failed to create log file reader: %v", err)
	}

	handleSignals(reader)

	logs := reader.Read()
	for l := range logs {
		fmt.Printf("%+v\n", l)
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
