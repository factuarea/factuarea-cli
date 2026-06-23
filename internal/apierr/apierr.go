package apierr

import "fmt"

type APIError struct {
	StatusCode int
	Type       string
	Code       string
	Message    string
	Subcode    string
	Param      string
	DocURL     string
	RequestID  string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return e.Message + " (" + e.Code + ")"
	}
	return e.Message
}

type TransportError struct{ Err error }

func (e *TransportError) Error() string { return e.Err.Error() }
func (e *TransportError) Unwrap() error { return e.Err }

type UsageError struct{ Err error }

func (e *UsageError) Error() string { return e.Err.Error() }
func (e *UsageError) Unwrap() error { return e.Err }

func Usagef(format string, a ...any) *UsageError { return &UsageError{Err: fmt.Errorf(format, a...)} }

type PermissionError struct{ Err error }

func (e *PermissionError) Error() string { return e.Err.Error() }
func (e *PermissionError) Unwrap() error { return e.Err }

func Permf(format string, a ...any) *PermissionError {
	return &PermissionError{Err: fmt.Errorf(format, a...)}
}
