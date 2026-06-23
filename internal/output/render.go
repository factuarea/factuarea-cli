package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

func PrintBody(w io.Writer, body []byte, f Format) error {
	_, err := w.Write(body)
	if len(body) > 0 && body[len(body)-1] != '\n' {
		_, _ = w.Write([]byte("\n"))
	}
	return err
}

func PrintJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func WantsJSON(jsonFlag bool, stdout *os.File) bool {
	return ResolveFormat(jsonFlag, IsTTY(stdout)) == JSON
}

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
