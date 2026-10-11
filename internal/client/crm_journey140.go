package client

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/spec"
)

// No normalization, generated key, local receipt or cached authority is used.
func crmJourney140LiteralKey(key string) bool {
	if len(key) < 1 || len(key) > 255 || strings.TrimSpace(key) != key {
		return false
	}
	for _, char := range key {
		if char < 32 || char > 126 {
			return false
		}
	}
	return true
}

func (c *Client) CrmJourney140Request(ctx context.Context, operation, identity string, raw []byte, key string) (*Response, error) {
	contract, err := spec.NativeCrmSource139Contract()
	if err != nil {
		return nil, err
	}
	op, ok := contract.Operations[operation]
	if !ok || !crmJourney140Operation(operation) {
		return nil, apierr.Usagef("operación fuera del viaje nativo140")
	}
	path := "/v1" + op.Path
	if strings.Contains(path, "{") {
		entity := strings.Split(operation, "_")[0]
		if contract.ValidateParameter(operation, "path", entity, identity) != nil {
			return nil, apierr.Usagef("identidad incompatible con el UUID nativo del paso")
		}
		path = strings.ReplaceAll(path, "{"+entity+"}", url.PathEscape(identity))
	} else if identity != "" {
		return nil, apierr.Usagef("esta operación no admite identidad en ruta")
	}
	original := strings.HasSuffix(operation, "_original")
	if op.Method != http.MethodGet || original {
		if !crmJourney140LiteralKey(key) || contract.ValidateParameter(operation, "header", "Idempotency-Key", key) != nil {
			return nil, apierr.Usagef("conserva clave explícita ASCII nativa, sin espacios de borde")
		}
		if len(raw) == 0 {
			return nil, apierr.Usagef("el cuerpo original no puede estar vacío")
		}
	}
	if original {
		path += "?" + url.Values{"original_body_sha256": {CrmOriginalBodyHash(raw)}}.Encode()
	} else if op.Method != http.MethodGet && contract.ValidateRequest(operation, raw) != nil {
		return nil, apierr.Usagef("el cuerpo no conserva el esquema cerrado de la operación nativa")
	}
	if op.Method == http.MethodGet && !original && (len(raw) > 0 || key != "") {
		return nil, apierr.Usagef("Current GET no admite cuerpo ni clave de un efecto")
	}
	// The same canonical Do gets a private per-call policy, including when this
	// helper is used without Cobra. No retry or redirect can replay these bytes.
	one := *c
	WithSingleDispatch()(&one)
	headers := map[string]string{}
	if key != "" {
		headers["Idempotency-Key"] = key
	}
	body := raw
	if op.Method == http.MethodGet {
		body = nil
	}
	response, err := one.Do(ctx, op.Method, path, body, headers)
	if err != nil {
		return response, err
	}
	media, _, err := mime.ParseMediaType(response.ContentType)
	if err != nil || media != "application/json" || contract.ValidateResponseStatus(operation, response.StatusCode, response.Body) != nil {
		return response, crmJourney140Unknown()
	}
	if original {
		var envelope struct {
			Data struct {
				Hash string `json:"original_body_sha256"`
			} `json:"data"`
		}
		if json.Unmarshal(response.Body, &envelope) != nil || envelope.Data.Hash != CrmOriginalBodyHash(raw) {
			return response, crmJourney140Unknown()
		}
	}
	return response, nil
}

func crmJourney140Operation(key string) bool {
	switch key {
	case "lead_create", "lead_current", "lead_original", "opportunity_create", "opportunity_current", "opportunity_original", "activity_create", "activity_current", "activity_original", "activity_update":
		return true
	}
	return false
}
func crmJourney140Unknown() error {
	return &apierr.TransportError{Err: errors.New("resultado UNKNOWN: respuesta nativa incompleta/incompatible; conserva intento, sin reenviar ni inferir confirmación desde Current")}
}
