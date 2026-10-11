package client

import (
	"context"
	"encoding/json"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/spec"
)

func (c *Client) CrmSource143Request(ctx context.Context, operation, identity string, raw []byte, key string) (*Response, error) {
	contract, err := spec.NativeCrmSource143Contract()
	if err != nil {
		return nil, err
	}
	op, ok := contract.Operations[operation]
	if !ok {
		return nil, apierr.Usagef("operación Source143 ausente")
	}
	parameter := "run"
	if strings.HasPrefix(operation, "document_") {
		parameter = "document"
	}
	if contract.ValidateParameter(operation, "path", parameter, identity) != nil {
		return nil, apierr.Usagef("identidad no conserva el UUID nativo del recurso")
	}
	path := "/v1" + strings.ReplaceAll(op.Path, "{"+parameter+"}", url.PathEscape(identity))
	original := operation == "import_original"
	if original || op.Method == http.MethodPost {
		if !crmJourney140LiteralKey(key) || contract.ValidateParameter(operation, "header", "Idempotency-Key", key) != nil {
			return nil, apierr.Usagef("se requiere clave literal ASCII explícita y representable")
		}
		if len(raw) == 0 {
			return nil, apierr.Usagef("se requiere el cuerpo original no vacío")
		}
	}
	if original {
		path += "?" + url.Values{"original_body_sha256": {CrmOriginalBodyHash(raw)}}.Encode()
	} else if op.Method == http.MethodPost && contract.ValidateRequest(operation, raw) != nil {
		return nil, apierr.Usagef("cuerpo fuera del esquema nativo Source143")
	}
	if op.Method == http.MethodGet && !original && (len(raw) > 0 || key != "") {
		return nil, apierr.Usagef("Current no admite cuerpo o clave de efecto")
	}
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
	media, _, mediaErr := mime.ParseMediaType(response.ContentType)
	if mediaErr != nil || media != "application/json" || contract.ValidateResponseStatus(operation, response.StatusCode, response.Body) != nil {
		return response, crmJourney140Unknown()
	}
	var envelope struct {
		Data struct {
			ID      string      `json:"id"`
			Version json.Number `json:"version"`
			Hash    string      `json:"original_body_sha256"`
			Errors  *struct {
				Version json.Number `json:"version"`
			} `json:"errors"`
		} `json:"data"`
	}
	if json.Unmarshal(response.Body, &envelope) != nil {
		return response, crmJourney140Unknown()
	}
	if original {
		if envelope.Data.Hash != CrmOriginalBodyHash(raw) {
			return response, crmJourney140Unknown()
		}
	} else {
		if !strings.EqualFold(envelope.Data.ID, identity) {
			return response, crmJourney140Unknown()
		}
		if operation == "import_current" && (envelope.Data.Errors == nil || envelope.Data.Errors.Version != envelope.Data.Version) {
			return response, crmJourney140Unknown()
		}
	}
	return response, nil
}
