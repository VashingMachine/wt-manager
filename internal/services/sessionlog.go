package services

import "context"

// commandContext makes the commands run for a request appear in the session log.
func (s *Service) commandContext(ctx context.Context) context.Context {
	return s.sessionLog.Context(ctx)
}

// ReadSessionLog returns a consistent snapshot and the temporary file to inspect outside the TUI.
func (s *Service) ReadSessionLog() (string, string, error) {
	return s.sessionLog.Read()
}

func (s *Service) RecordSessionEvent(message string, failed bool) {
	s.sessionLog.Record(message, failed)
}
