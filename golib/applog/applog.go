// Package applog provides a thread-safe ring buffer for in-app log viewing.
package applog

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sync"
	"sync/atomic"
	"time"
)

const maxEntries = 500

type entry struct {
	Time    string `json:"t"`
	Level   string `json:"l"`
	Message string `json:"m"`
}

type response struct {
	Cursor  int     `json:"c"`
	Entries []entry `json:"e"`
}

var (
	mu      sync.Mutex
	buf     [maxEntries]entry
	head    int // next write position
	count   int // total entries written (monotonic cursor)

	maskIPs atomic.Bool // when true, mask last octet of IPv4 addresses
)

// SetMaskIPs enables or disables IPv4 address masking in log messages.
// When enabled, the last octet of every IPv4 address is replaced
// with * (e.g. 192.168.1.100 → 192.168.1.*).
func SetMaskIPs(enabled bool) { maskIPs.Store(enabled) }

var ipv4Re = regexp.MustCompile(`(\d{1,3}\.\d{1,3}\.\d{1,3})\.\d{1,3}`)

func maskIPAddresses(msg string) string {
	return ipv4Re.ReplaceAllString(msg, "${1}.*")
}

func add(level, msg string) {
	if maskIPs.Load() {
		msg = maskIPAddresses(msg)
	}
	mu.Lock()
	defer mu.Unlock()
	buf[head%maxEntries] = entry{
		Time:    time.Now().Format("15:04:05.000"),
		Level:   level,
		Message: msg,
	}
	head++
	count++
}

// Info logs an informational message.
func Info(msg string) { add("info", msg) }

// Infof logs a formatted informational message.
func Infof(format string, args ...any) { add("info", fmt.Sprintf(format, args...)) }

// Warn logs a warning message.
func Warn(msg string) { add("warn", msg) }

// Warnf logs a formatted warning message.
func Warnf(format string, args ...any) { add("warn", fmt.Sprintf(format, args...)) }

// Error logs an error message.
func Error(msg string) { add("error", msg) }

// Errorf logs a formatted error message.
func Errorf(format string, args ...any) { add("error", fmt.Sprintf(format, args...)) }

// Success logs a success message.
func Success(msg string) { add("success", msg) }

// Successf logs a formatted success message.
func Successf(format string, args ...any) { add("success", fmt.Sprintf(format, args...)) }

// GetLogs returns a JSON string with all entries since the given cursor.
func GetLogs(cursor int) string {
	mu.Lock()
	defer mu.Unlock()

	// Oldest available cursor
	oldest := count - maxEntries
	oldest = max(oldest, 0)
	cursor = max(cursor, oldest)

	n := count - cursor
	entries := make([]entry, 0, n)
	for i := cursor; i < count; i++ {
		entries = append(entries, buf[i%maxEntries])
	}

	data, _ := json.Marshal(response{Cursor: count, Entries: entries})
	return string(data)
}

// ClearLogs resets the log buffer.
func ClearLogs() {
	mu.Lock()
	defer mu.Unlock()
	head = 0
	count = 0
}
