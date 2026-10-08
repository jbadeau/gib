package gib

import (
	"fmt"
	"strings"
)

// LogLevel is how much a log message matters, Jib's LogEvent levels.
type LogLevel int

const (
	LevelError LogLevel = iota
	LevelWarn
	LevelLifecycle
	LevelProgress
	LevelInfo
	LevelDebug
)

// LogEvent is a message gib logs as it builds, as Jib dispatches its
// LogEvents.
type LogEvent struct {
	Level   LogLevel
	Message string
}

// LogHandler receives what gib logs.
type LogHandler func(LogEvent)

func (h LogHandler) log(l LogLevel, format string, args ...any) {
	if h != nil {
		h(LogEvent{Level: l, Message: fmt.Sprintf(format, args...)})
	}
}

// javaList is a list as Java's List.toString prints it.
func javaList(s []string) string {
	return "[" + strings.Join(s, ", ") + "]"
}
