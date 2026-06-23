package client

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

func parseError(resp *Response) error {
	if strings.Contains(resp.ContentType, "json") {
		var raw struct {
			Error map[string]any `json:"error"`
		}
		if json.Unmarshal(resp.Body, &raw) == nil && raw.Error != nil {
			get := func(k string) string {
				if v, ok := raw.Error[k].(string); ok {
					return v
				}
				return ""
			}
			api := &apierr.APIError{
				StatusCode: resp.StatusCode,
				Type:       get("type"),
				Code:       get("code"),
				Message:    get("message"),
				Subcode:    get("subcode"),
				Param:      get("param"),
				DocURL:     get("doc_url"),
				RequestID:  get("request_id"),
			}
			if api.RequestID == "" {
				api.RequestID = resp.RequestID
			}
			if api.Type == "" {
				api.Type = synthesizeType(resp.StatusCode)
			}
			if api.Message == "" {
				api.Message = "Error " + httpStatus(resp.StatusCode)
			}
			return api
		}
	}
	return &apierr.APIError{
		StatusCode: resp.StatusCode,
		Type:       synthesizeType(resp.StatusCode),
		Message:    nonJSONMessage(resp),
		RequestID:  resp.RequestID,
	}
}

func nonJSONMessage(resp *Response) string {
	return "el servidor devolvió " + httpStatus(resp.StatusCode) + " (respuesta no-JSON)"
}

func synthesizeType(status int) string {
	switch {
	case status == 401:
		return "authentication_error"
	case status == 403:
		return "authorization_error"
	case status == 404:
		return "not_found_error"
	case status == 409:
		return "conflict_error"
	case status == 429:
		return "rate_limit_error"
	case status == 503:
		return "service_unavailable_error"
	case status >= 500:
		return "api_error"
	default:
		return "invalid_request_error"
	}
}

func httpStatus(code int) string {
	switch code {
	case 503:
		return "503 (servicio no disponible)"
	default:
		return "HTTP " + strconv.Itoa(code)
	}
}
