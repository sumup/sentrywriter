package sentrywriter

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/pkg/errors"
)

// SentryClient is an interface which represents the sentry-go package client.
type SentryClient interface {
	CaptureMessage(message string, hint *sentry.EventHint, scope sentry.EventModifier) *sentry.EventID
	Flush(timeout time.Duration) bool
}

// LogLevel is used to match the log level that you're using and then map it
// into a Sentry log level. For example, you may be logging at level "error",
// which corresponds to sentry.LevelError, so that would correspond to:
//     levelError := LogLevel{"error", sentry.LevelError}
//
// See https://godoc.org/github.com/getsentry/sentry-go#Level for the possible
// Sentry log levels.
type LogLevel struct {
	MatchingString string
	SentryLevel    sentry.Level
}

// SentryWriter implements the io.Writer interface. It is a wrapper over the
// sentry-go client and sends the supplied logs of the specified log level to
// Sentry. It assumes that the logs are json encoded. Writes are asynchronous,
// so remember to call Flush before exiting the program.
type SentryWriter struct {
	mu             sync.RWMutex
	client         SentryClient
	logLevels      []LogLevel
	levelFieldName string
	userID         string
}

// New returns a pointer to the SentryWriter, with the specified log levels set.
// The SentryWriter will write logs which match any of the supplied logs to
// Sentry. The default field that is checked for the log level is "level".
func New(dsn string) (*SentryWriter, error) {
	// The sentry-go package
	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn: dsn,
	})
	if err != nil {
		return nil, errors.Wrap(err, "sentry.NewClient")
	}

	return &SentryWriter{
		levelFieldName: "level",
		client:         client,
	}, nil
}

// AddLogLevel adds a LogLevel that triggers an event to be sent to Sentry.
func (s *SentryWriter) AddLogLevel(level LogLevel) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logLevels = append(s.logLevels, level)
}

func (s *SentryWriter) getLogLevels() []LogLevel {
	s.mu.RLock()
	defer s.mu.RUnlock()

	logLevels := make([]LogLevel, len(s.logLevels))
	copy(logLevels, s.logLevels)
	return logLevels
}

// SetLevelFieldName allows you to change the log level field name from the
// default of "level" to whatever you are using.
func (s *SentryWriter) SetLevelFieldName(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.levelFieldName = name
}

func (s *SentryWriter) getLevelFieldName() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.levelFieldName
}

// SetUserID sets a user ID that will be reported alongside each Sentry event.
// This is helpful for code that runs on client machines.
func (s *SentryWriter) SetUserID(userID string) {
	s.mu.Lock()
	defer s.mu.Lock()

	s.userID = userID
}

func (s *SentryWriter) getUserID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.userID
}

// SetClient allows you to substitute the client that is being used, rather
// than the default client from the sentry-go package.
func (s *SentryWriter) SetClient(client SentryClient) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.client = client
}

// Write is the implementation of the io.Writer interface. It checks if the log
// is at one of the preset log levels and if so it writes it to Sentry.
func (s *SentryWriter) Write(log []byte) (int, error) {
	var eventMap map[string]json.RawMessage
	if err := json.Unmarshal(log, &eventMap); err != nil {
		return 0, errors.Wrap(err, "json.Unmarshal log")
	}
	var level string
	if err := json.Unmarshal(eventMap[s.getLevelFieldName()], &level); err != nil {
		return 0, errors.Wrapf(err, `json.Unmarshal eventMap["%s"]`, s.getLevelFieldName())
	}

	logLevel, found := s.findMatchingLogLevel(level)
	if !found {
		return len(log), nil
	}

	scope := sentry.NewScope()
	scope.SetLevel(logLevel.SentryLevel)
	userID := s.getUserID()
	if userID != "" {
		scope.SetUser(sentry.User{ID: s.getUserID()})
	}

	s.client.CaptureMessage(string(log), nil, scope)

	return len(log), nil
}

func (s *SentryWriter) findMatchingLogLevel(level string) (LogLevel, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, logLevel := range s.logLevels {
		if logLevel.MatchingString == level {
			return logLevel, true
		}
	}
	return LogLevel{}, false
}

// Flush initiates the Flush method of the underlying Sentry client. Call this
// before exiting your program. The provided timeout is the maximum length of
// time to block until all the logs have been sent to Sentry. It returns false
// if the timeout is reached, which may signify that not all messages were sent
// to Sentry.
func (s *SentryWriter) Flush(timeout time.Duration) bool {
	return s.client.Flush(timeout)
}
