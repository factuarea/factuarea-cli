package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

// PrintBody vuelca el cuerpo de respuesta. En JSON/Plain se emite crudo (es el
// body exacto de la API). En Human, por ahora también crudo; el render de
// tablas se añade en el Plan 2 sobre el spec.
func PrintBody(w io.Writer, body []byte, f Format) error {
	_, err := w.Write(body)
	if len(body) > 0 && body[len(body)-1] != '\n' {
		_, _ = w.Write([]byte("\n"))
	}
	return err
}

// PrintError escribe el error a stderr: JSON estructurado bajo --json, legible
// en humano. Mantiene el shape del backend.
func PrintError(stderr io.Writer, err error, f Format) {
	var api *apierr.APIError
	if errors.As(err, &api) {
		if f == JSON {
			payload := map[string]any{"error": map[string]any{
				"type": api.Type, "code": api.Code, "message": api.Message,
				"subcode": api.Subcode, "param": api.Param,
				"doc_url": api.DocURL, "request_id": api.RequestID,
			}}
			enc := json.NewEncoder(stderr)
			_ = enc.Encode(payload)
			return
		}
		fmt.Fprintf(stderr, "Error: %s\n", api.Message)
		if api.Code != "" {
			fmt.Fprintf(stderr, "  código: %s\n", api.Code)
		}
		if api.RequestID != "" {
			fmt.Fprintf(stderr, "  request_id: %s\n", api.RequestID)
		}
		if api.DocURL != "" {
			fmt.Fprintf(stderr, "  más info: %s\n", api.DocURL)
		}
		return
	}
	if f == JSON {
		_ = json.NewEncoder(stderr).Encode(map[string]any{"error": map[string]any{"message": err.Error()}})
		return
	}
	fmt.Fprintf(stderr, "Error: %s\n", err.Error())
}
