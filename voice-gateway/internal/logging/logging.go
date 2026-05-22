// Package logging provides structured logging via zerolog.
package logging

import (
	"os"
	"time"

	"github.com/rs/zerolog"
)

var Logger zerolog.Logger

func init() {
	level := os.Getenv("LOG_LEVEL")
	lvl, err := zerolog.ParseLevel(level)
	if err != nil {
		lvl = zerolog.InfoLevel
	}

	Logger = zerolog.New(zerolog.ConsoleWriter{
		Out:        os.Stderr,
		TimeFormat: time.RFC3339,
	}).
		Level(lvl).
		With().
		Timestamp().
		Caller().
		Logger()
}

// SessionLogger returns a logger with call session context.
func SessionLogger(callSid, tenantID, convState string) zerolog.Logger {
	return Logger.With().
		Str("callSid", callSid).
		Str("tenantId", tenantID).
		Str("convState", convState).
		Logger()
}
