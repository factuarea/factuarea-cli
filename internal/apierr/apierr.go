package apierr

import (
	"errors"
	"fmt"
	"io/fs"
)

func FileIOReason(err error) string {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "no existe"
	case errors.Is(err, fs.ErrPermission):
		return "permiso denegado"
	}
	var perr *fs.PathError
	if errors.As(err, &perr) {
		switch perr.Err.Error() {
		case "read-only file system":
			return "sistema de ficheros de solo lectura"
		case "is a directory":
			return "es un directorio, no un fichero"
		}
		return perr.Err.Error()
	}
	return err.Error()
}

func ReadFileUsagef(path string, err error) *UsageError {
	return Usagef("no se pudo leer el fichero %s: %s", path, FileIOReason(err))
}

func WriteFileUsagef(path string, err error) *UsageError {
	return Usagef("no se pudo escribir el fichero %s: %s", path, FileIOReason(err))
}

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
