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
	var unknown *apierr.WriteUnconfirmed
	if errors.As(err, &unknown) {
		if f == JSON {
			_ = json.NewEncoder(stderr).Encode(map[string]any{"error": map[string]any{
				"type": "write_unconfirmed", "state": "unconfirmed", "message": unknown.Error(),
				"method": unknown.Method, "path": unknown.Path,
				"idempotency_key": unknown.IdempotencyKey, "original_body_sha256": unknown.OriginalBodySHA256,
			}})
			return
		}
		fmt.Fprintf(stderr, "Error: %s\n  clave original: %s\n  SHA256 original: %s\n", unknown.Error(), unknown.IdempotencyKey, unknown.OriginalBodySHA256)
		return
	}
	var api *apierr.APIError
	if errors.As(err, &api) {
		if f == JSON {
			payload := map[string]any{"error": map[string]any{
				"type": api.Type, "code": api.Code, "message": api.Message,
				"subcode": api.Subcode, "param": api.Param,
				"doc_url": api.DocURL, "request_id": api.RequestID,
			}}
			if len(api.Data) > 0 {
				payload["data"] = api.Data
			}
			errorPayload := payload["error"].(map[string]any)
			errorPayload["http_status"] = api.StatusCode
			if api.RetryAfter != "" {
				errorPayload["retry_after"] = api.RetryAfter
			}
			if api.Details != nil {
				errorPayload["details"] = api.Details
			}
			if api.Errors != nil {
				errorPayload["errors"] = api.Errors
			}
			enc := json.NewEncoder(stderr)
			_ = enc.Encode(payload)
			return
		}
		fmt.Fprintf(stderr, "Error: %s\n", api.Message)
		if api.RetryAfter != "" {
			fmt.Fprintf(stderr, "  Retry-After: %s\n", api.RetryAfter)
		}
		if len(api.Data) > 0 {
			fmt.Fprintf(stderr, "  detalle: %s\n", api.Data)
		}
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
