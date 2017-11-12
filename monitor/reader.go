package monitor

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/pkg/errors"
)

// clfNumParts is the number of components in a Common Log Format entry.
const clfNumParts = 7

// clfRegexp matches a line in Common Log Format, i.e. "host ident authuser date request status bytes".
var clfRegexp = regexp.MustCompile(`^(\S+) (\S+) (\S+) \[([\w:/]+\s[+\-]\d{4})\] "(.*)" (\d{3}|-) (\d+|-)( ".*" ".*")?`)

// log is an HTTP log entry, e.g. as parsed from Common Log Format.
type log struct {
	// remoteAddr is the IP address of the remote client.
	remoteAddr string

	// identity is the RFC 1413 identity of the client.
	identity string

	// userID is the userid of the person requesting the document.
	userID string

	// timestamp of the request.
	timestamp time.Time

	// request is the document requested.
	request string

	// status is the HTTP status code returned to the client.
	status int

	// size is the size of the response returned to the client in bytes.
	size int64
}

// reader reads log entries from an actively written to HTTP log file.
type reader interface {
	// Open begins reading log entries from the file starting at the beginning
	// and places them on the channel. If the reader reaches the end of the
	// file, it will wait for new log entries to be appended until Close is
	// called.
	Open() (<-chan *log, error)

	// Close stops the reader.
	Close() error
}

// clfReader implements the reader interface for log files using Common Log
// Format.
type clfReader struct {
	file    string
	watcher *fsnotify.Watcher
	logs    chan *log
	close   chan struct{}
}

// NewCommonLogFormatReader returns a new reader for log files using Common Log
// Format.
func NewCommonLogFormatReader(file string) (reader, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, errors.Wrap(err, "failed to create file watcher")
	}
	if err := watcher.Add(file); err != nil {
		watcher.Close()
		return nil, errors.Wrap(err, "failed to add file watch")
	}
	return &clfReader{
		file:    file,
		watcher: watcher,
		logs:    make(chan *log),
		close:   make(chan struct{}),
	}, nil
}

// Open begins reading log entries from the file starting at the beginning and
// places them on the channel. If the reader reaches the end of the file, it
// will wait for new log entries to be appended until Close is called.
func (c *clfReader) Open() (<-chan *log, error) {
	file, err := os.Open(c.file)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open file")
	}
	go c.read(file)
	return c.logs, nil
}

// Close stops the reader.
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
func (c *clfReader) read(file *os.File) {
	reader := bufio.NewReader(file)
	defer file.Close()
READLOOP:
	for {
		line, err := reader.ReadString('\n')
		if err == io.EOF {
			// If we reach EOF, wait for new logs to be written.
			if c.waitForLogs() {
				// The file was written, so try reading again.
				continue READLOOP
			}
			break
		}
		if err != nil {
			fmt.Printf("Error reading from file %s: %v\n", c.file, err)
			os.Exit(1)
		}

		parts := clfRegexp.FindStringSubmatch(string(line))
		// Add 1 because the first part is the entire expression.
		if len(parts) < clfNumParts+1 {
			fmt.Printf("Skipping log not in Common Log Format: %s\n", line)
			continue
		}

		l := &log{
			remoteAddr: parts[1],
			identity:   parts[2],
			userID:     parts[3],
			request:    parts[5],
		}

		// Parse timestamp.
		l.timestamp, _ = time.Parse("02/Jan/2006:15:04:05 -0700", parts[4])

		// Parse status code and size (don't handle errors since we'll accept zero).
		l.status, _ = strconv.Atoi(parts[6])
		l.size, _ = strconv.ParseInt(parts[7], 10, 64)

		c.logs <- l
	}
}

// waitForLogs blocks until the log file is updated or the reader is closed. It
// returns true if the file was updated and false if the reader was closed.
func (c *clfReader) waitForLogs() bool {
	select {
	case <-c.watcher.Events:
		return true
	case err, ok := <-c.watcher.Errors:
		if ok {
			fmt.Printf("Watcher error on file %s: %v\n", c.file, err)
			os.Exit(1)
		}
		return false
	case <-c.close:
		return false
	}
}
