package display

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"natproxy/golib/applog"
)

type logResponse struct {
	Cursor  int        `json:"c"`
	Entries []logEntry `json:"e"`
}

type logEntry struct {
	Time    string `json:"t"`
	Level   string `json:"l"`
	Message string `json:"m"`
}

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorGray   = "\033[90m"
)

func levelColor(level string) string {
	switch level {
	case "error":
		return colorRed
	case "warn":
		return colorYellow
	case "success":
		return colorGreen
	default:
		return colorGray
	}
}

// StreamLogs polls the applog ring buffer and prints entries to stderr.
// Runs until done is closed.
func StreamLogs(done <-chan struct{}) {
	cursor := 0
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			cursor = printLogs(cursor)
		}
	}
}

// FlushLogs drains all pending log entries immediately.
func FlushLogs() {
	printLogs(0)
}

func printLogs(cursor int) int {
	raw := applog.GetLogs(cursor)
	var resp logResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return cursor
	}
	for _, e := range resp.Entries {
		color := levelColor(e.Level)
		fmt.Fprintf(os.Stderr, "%s[%s] [%s]%s %s\n", color, e.Time, e.Level, colorReset, e.Message)
	}
	return resp.Cursor
}
