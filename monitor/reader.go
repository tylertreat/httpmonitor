package monitor

import (
	"bufio"
	"io"
	"log"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/pkg/errors"
)

// clfNumParts is the number of components in a Common Log Format entry.
const clfNumParts = 7

// clfRegexp matches a line in Common Log Format, e.g.
// 127.0.0.1 user-identifier frank [10/Oct/2000:13:55:36 -0700] "GET /apache_pb.gif HTTP/1.0" 200 2326
var clfRegexp = regexp.MustCompile(`^(\S+) (\S+) (\S+) \[([\w:/]+\s[+\-]\d{4})\] "(.*)" (\d{3}|-) (\d+|-)`)

// Log is an HTTP log entry, e.g. as parsed from Common Log Format.
type Log struct {
	// RemoteAddr is the IP address of the remote client.
	RemoteAddr string

	// Identity is the RFC 1413 identity of the client.
	Identity string

	// UserID is the userid of the person requesting the document.
	UserID string

	// Timestamp of the request.
	Timestamp time.Time

	// Request is the document requested.
	Request string

	// Status is the HTTP status code returned to the client.
	Status int

	// Size is the size of the response returned to the client in bytes.
	Size int
}

// Reader reads log entries from an actively written to HTTP log file.
type Reader interface {
	// Read begins reading log entries from the file starting at the beginning
	// and places them on the channel. If the Reader reaches the end of the
	// file, it will wait for new log entries to be appended until Close is
	// called.
	Read() <-chan *Log

	// Close stops the Reader.
	Close() error
}

// clfReader implements the Reader interface for log files using Common Log
// Format.
type clfReader struct {
	file    *os.File
	reader  *bufio.Reader
	watcher *fsnotify.Watcher
	logs    chan *Log
	close   chan struct{}
}

// NewCommonLogFormatReader returns a new Reader for log files using Common Log
// Format.
func NewCommonLogFormatReader(file *os.File) (Reader, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, errors.Wrap(err, "failed to create file watcher")
	}
	if err := watcher.Add(file.Name()); err != nil {
		watcher.Close()
		return nil, errors.Wrap(err, "failed to add file watch")
	}
	return &clfReader{
		file:    file,
		reader:  bufio.NewReader(file),
		watcher: watcher,
		logs:    make(chan *Log),
		close:   make(chan struct{}),
	}, nil
}

// Read begins reading log entries from the file starting at the beginning and
// places them on the channel. If the Reader reaches the end of the file, it
// will wait for new log entries to be appended until Close is called.
func (c *clfReader) Read() <-chan *Log {
	go c.read()
	return c.logs
}

// Close stops the Reader.
func (c *clfReader) Close() error {
	if err := c.watcher.Close(); err != nil {
		return errors.Wrap(err, "failed to close file watcher")
	}
	close(c.close)
	return nil
}

// read is a long-running loop that reads and parses log entries from the file
// and places them on the channel. It starts by parsing the current contents of
// the file, then once it reaches the end of the file, it waits for new logs to
// be written. It runs until Close is called.
func (c *clfReader) read() {
READLOOP:
	for {
		// TODO: handle lines that exceed the reader's buffer size.
		line, _, err := c.reader.ReadLine()
		if err == io.EOF {
			// If we reach EOF, wait for new logs to be written.
			if c.waitForLogs() {
				// The file was written, so try reading again.
				continue READLOOP
			}
			break
		}
		if err != nil {
			log.Fatalf("Error reading from file %s: %v\n", c.file.Name(), err)
		}

		parts := clfRegexp.FindStringSubmatch(string(line))
		// TODO: could make this more robust.
		// Add 1 because the first part is the entire expression.
		if len(parts) != clfNumParts+1 {
			log.Fatalf("File %s is not in Common Log Format\n", c.file.Name())
		}

		l := &Log{
			RemoteAddr: parts[1],
			Identity:   parts[2],
			UserID:     parts[3],
			Request:    parts[5],
		}

		// Parse timestamp.
		timestamp, err := time.Parse("02/Jan/2006:15:04:05 -0700", parts[4])
		if err != nil {
			l.Timestamp = timestamp
		}

		// Parse status code and size (ignoring error since we'll just accept zero).
		l.Status, _ = strconv.Atoi(parts[6])
		l.Size, _ = strconv.Atoi(parts[7])

		c.logs <- l
	}
}

// waitForLogs blocks until the log file is updated or the Reader is closed. It
// returns true if the file was updated and false if the Reader was closed.
func (c *clfReader) waitForLogs() bool {
	select {
	case <-c.watcher.Events:
		return true
	case err, ok := <-c.watcher.Errors:
		if ok {
			log.Fatalf("Watcher error on file %s: %v", c.file.Name(), err)
		}
		return false
	case <-c.close:
		return false
	}
}
