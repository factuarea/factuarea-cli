package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

// CRMPagination describes wire locations obtained from native OpenAPI schemas.
// MaxPages is a local traversal budget, not an API quota or a result count.
type CRMPagination struct {
	Kind, QueryParameter, ItemsPath, MorePath, CursorPath string
	CurrentPagePath, LastPagePath, IdentityPath           string
	MaxPages                                              int
}

func (c *Client) PaginateCRM(ctx context.Context, path string, query url.Values, plan CRMPagination, each func(json.RawMessage) error) error {
	if plan.MaxPages < 1 || plan.QueryParameter == "" || plan.ItemsPath == "" || plan.IdentityPath == "" {
		return apierr.Usagef("la paginación CRM necesita un presupuesto positivo y un contrato nativo")
	}
	q := url.Values{}
	for k, values := range query {
		q[k] = append([]string{}, values...)
	}
	cursors := map[string]bool{}
	if current := q.Get(plan.QueryParameter); current != "" {
		cursors[current] = true
	}
	seen := map[string]bool{}
	for page := 0; page < plan.MaxPages; page++ {
		if err := ctx.Err(); err != nil {
			return &apierr.TransportError{Err: err}
		}
		full := path
		if len(q) > 0 {
			full += "?" + q.Encode()
		}
		response, err := c.Do(ctx, http.MethodGet, full, nil, nil)
		if err != nil {
			return err
		}
		rawItems, err := crmAt(response.Body, plan.ItemsPath)
		if err != nil {
			return err
		}
		var items []json.RawMessage
		if string(rawItems) == "null" || json.Unmarshal(rawItems, &items) != nil {
			return crmPageError("La colección CRM no es una lista autorizada.")
		}
		// Validate an entire page before emitting it. Repeated identities fail;
		// they are never silently deduplicated into an invented complete result.
		pageIDs := map[string]bool{}
		for _, item := range items {
			rawID, err := crmAt(item, plan.IdentityPath)
			var id string
			if err != nil || json.Unmarshal(rawID, &id) != nil || id == "" {
				return crmPageError("Una fila CRM carece de su identidad pública.")
			}
			if seen[id] || pageIDs[id] {
				return crmPageError("La identidad CRM se repite al recorrer las páginas; vuelve a consultar.")
			}
			pageIDs[id] = true
		}
		next := ""
		done := false
		switch plan.Kind {
		case "cursor":
			rawMore, err := crmAt(response.Body, plan.MorePath)
			if err != nil {
				return err
			}
			var more bool
			if string(rawMore) == "null" || json.Unmarshal(rawMore, &more) != nil {
				return crmPageError("La continuación CRM no tiene un estado válido.")
			}
			rawCursor, err := crmAt(response.Body, plan.CursorPath)
			if err != nil {
				return err
			}
			var cursor *string
			if json.Unmarshal(rawCursor, &cursor) != nil {
				return crmPageError("El cursor CRM no es texto opaco.")
			}
			done = !more
			if more {
				if cursor == nil || *cursor == "" || cursors[*cursor] {
					return crmPageError("El cursor CRM falta o se repite; no se confirma la colección completa.")
				}
				if len(items) == 0 {
					return crmPageError("La página CRM vacía anuncia una continuación incoherente.")
				}
				next = *cursor
			}
		case "page":
			current, err := crmPageNumber(response.Body, plan.CurrentPagePath)
			if err != nil {
				return err
			}
			last, err := crmPageNumber(response.Body, plan.LastPagePath)
			if err != nil {
				return err
			}
			expected := int64(1)
			if supplied := q.Get(plan.QueryParameter); supplied != "" {
				expected, err = strconv.ParseInt(supplied, 10, 64)
				if err != nil || expected < 1 {
					return apierr.Usagef("la página inicial debe ser un entero positivo")
				}
			}
			if current != expected || (last < current && len(items) > 0) {
				return crmPageError("La página CRM actual no corresponde a la petición original.")
			}
			done = current >= last
			if !done && len(items) == 0 {
				return crmPageError("La página CRM vacía anuncia una continuación incoherente.")
			}
			if !done {
				next = strconv.FormatInt(current+1, 10)
			}
		default:
			return apierr.Usagef("estrategia de paginación CRM desconocida")
		}
		for _, item := range items {
			if err := each(item); err != nil {
				return err
			}
		}
		for id := range pageIDs {
			seen[id] = true
		}
		if done {
			return nil
		}
		q.Set(plan.QueryParameter, next)
		cursors[next] = true
	}
	return apierr.Usagef("se alcanzó --max-pages antes de completar el recorrido CRM; la salida es parcial")
}

func crmAt(body []byte, path string) (json.RawMessage, error) {
	raw := json.RawMessage(body)
	for _, field := range strings.Split(path, ".") {
		var obj map[string]json.RawMessage
		if field == "" || json.Unmarshal(raw, &obj) != nil {
			return nil, crmPageError("El envelope CRM no corresponde al contrato publicado.")
		}
		next, exists := obj[field]
		if !exists {
			return nil, crmPageError("La respuesta CRM no declara su continuación o colección original.")
		}
		raw = next
	}
	return raw, nil
}
func crmPageNumber(body []byte, path string) (int64, error) {
	raw, err := crmAt(body, path)
	if err != nil {
		return 0, err
	}
	var value int64
	if json.Unmarshal(raw, &value) != nil || value < 1 {
		return 0, crmPageError("El índice de página CRM no es un entero positivo.")
	}
	return value, nil
}
func crmPageError(message string) error {
	return &apierr.APIError{Type: "api_error", Code: "invalid_crm_pagination", Message: fmt.Sprintf("%s No se publican detalles de la respuesta.", message)}
}
