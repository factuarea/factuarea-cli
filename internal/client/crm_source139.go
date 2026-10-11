package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/spec"
)

type CrmImportApplyOriginal struct {
	BodySHA256           string `json:"original_body_sha256"`
	AuthorizationVersion string `json:"authorization_version"`
	State                string `json:"state"`
	OriginalResponse     *struct {
		Status int    `json:"status"`
		Body   string `json:"body"`
	} `json:"original_response"`
}

func CrmOriginalBodyHash(raw []byte) string {
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

// The bytes are hashed verbatim and never sent/stored. Recovery is a GET with
// the original key; it cannot apply rows, mint a claim or use Current as Original.
func (c *Client) ReadCrmImportApplyOriginal(ctx context.Context, run string, raw []byte, key string) (*CrmImportApplyOriginal, *Response, error) {
	contract, err := spec.NativeCrmSource139Contract()
	if err != nil {
		return nil, nil, err
	}
	if contract.ValidateParameter("import_apply_original", "path", "run", run) != nil {
		return nil, nil, apierr.Usagef("run debe conservar el UUID nativo original")
	}
	if contract.ValidateParameter("import_apply_original", "header", "Idempotency-Key", key) != nil {
		return nil, nil, apierr.Usagef("conserva el Idempotency-Key original de 1–255 caracteres")
	}
	for _, char := range key {
		if char < 32 || char > 126 {
			return nil, nil, apierr.Usagef("la clave original debe conservar ASCII imprimible sin recortar")
		}
	}
	// HTTP normalizes edge whitespace in field values. Refuse an unrepresentable
	// literal instead of silently recovering a different native intent.
	if strings.TrimSpace(key) != key {
		return nil, nil, apierr.Usagef("HTTP no conserva espacios al inicio/final del header original; no se consultó otra clave ni se normalizó el intento")
	}
	if len(raw) == 0 {
		return nil, nil, apierr.Usagef("declara los bytes no vacíos del cuerpo POST original")
	}
	op := contract.Operations["import_apply_original"]
	hash := CrmOriginalBodyHash(raw)
	path := "/v1" + strings.ReplaceAll(op.Path, "{run}", url.PathEscape(run)) + "?" + url.Values{"original_body_sha256": []string{hash}}.Encode()
	response, err := c.Do(ctx, op.Method, path, nil, map[string]string{"Idempotency-Key": key})
	if err != nil {
		return nil, response, err
	}
	if err := validateCrmSource139Response(contract, "import_apply_original", response); err != nil {
		return nil, response, err
	}
	var envelope struct {
		Data CrmImportApplyOriginal `json:"data"`
	}
	if json.Unmarshal(response.Body, &envelope) != nil || envelope.Data.BodySHA256 != hash {
		return nil, response, crmSource139Unconfirmed()
	}
	return &envelope.Data, response, nil
}

// The agenda is one native page. It does not create activities or call a
// calendar provider; omitted scope/module layers remain omitted in the output.
func (c *Client) ReadCrmSource139Agenda(ctx context.Context, query url.Values) (*Response, error) {
	contract, err := spec.NativeCrmSource139Contract()
	if err != nil {
		return nil, err
	}
	for key, values := range query {
		if len(values) != 1 || contract.ValidateParameter("agenda", "query", key, values[0]) != nil {
			return nil, apierr.Usagef("filtro de agenda no declarado por el contrato nativo")
		}
	}
	from, errFrom := time.Parse("2006-01-02", query.Get("from"))
	to, errTo := time.Parse("2006-01-02", query.Get("to"))
	if errFrom != nil || errTo != nil || to.Before(from) || to.Sub(from) > 92*24*time.Hour {
		return nil, apierr.Usagef("agenda exige from/to YYYY-MM-DD, ordenados y máximo 93 días incluidos")
	}
	if assignee, present := query["assignee_id"]; present && assignee[0] != "me" {
		if contract.ValidateParameter("import_apply_original", "path", "run", assignee[0]) != nil {
			return nil, apierr.Usagef("assignee_id debe ser me o el UUID nativo de un miembro")
		}
	}
	if requested, present := query["sources"]; present && requested[0] != "" {
		allowed, err := contract.AgendaSources()
		if err != nil {
			return nil, err
		}
		for _, value := range strings.Split(requested[0], ",") {
			found := false
			for _, source := range allowed {
				if source == value {
					found = true
				}
			}
			if !found {
				return nil, apierr.Usagef("sources debe conservar capas publicadas por la agenda nativa")
			}
		}
	}
	op := contract.Operations["agenda"]
	response, err := c.Do(ctx, op.Method, "/v1"+op.Path+"?"+query.Encode(), nil, nil)
	if err != nil {
		return response, err
	}
	if err := validateCrmSource139Response(contract, "agenda", response); err != nil {
		return response, err
	}
	return response, nil
}

func validateCrmSource139Response(contract *spec.CrmSource139Contract, key string, response *Response) error {
	if response == nil || response.StatusCode != http.StatusOK {
		return crmSource139Unconfirmed()
	}
	media, _, err := mime.ParseMediaType(response.ContentType)
	if err != nil || media != "application/json" || contract.ValidateResponse(key, response.Body) != nil {
		return crmSource139Unconfirmed()
	}
	return nil
}

func crmSource139Unconfirmed() error {
	return &apierr.TransportError{Err: errors.New("lector Source139 sin resultado nativo confirmado; no se envió ni reintentó un POST, y Current no acredita Original")}
}
