package apierr

// APIError representa el sobre de error del backend de Factuarea.
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

// TransportError representa un fallo de red/timeout/TLS: no hubo respuesta HTTP
// con sobre de error. Es transitorio y reintentable.
type TransportError struct{ Err error }

func (e *TransportError) Error() string { return e.Err.Error() }
func (e *TransportError) Unwrap() error { return e.Err }
