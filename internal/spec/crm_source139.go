package spec

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
)

// Generated mechanically from the private native export, never the published spec.
//
//go:embed crm_source139.openapi.json
var crmSource139Raw []byte

const CrmSource139NativeHash = "438b12b53be7ae7535cd6c85ba7dea06c76d84a33024db0e0232ac8ba279be63"

type CrmSource139Operation struct {
	Path      string         `json:"path"`
	Method    string         `json:"method"`
	Operation map[string]any `json:"operation"`
}

type CrmSource139Contract struct {
	NativeHash string `json:"source_artifact_sha256"`
	Profile    struct {
		State           string `json:"state"`
		Ratified        bool   `json:"ratified"`
		Published       bool   `json:"published"`
		DeliveryEnabled bool   `json:"delivery_enabled"`
	} `json:"native_profile"`
	Capabilities []string                             `json:"capabilities"`
	Operations   map[string]CrmSource139Operation     `json:"operations"`
	Components   map[string]map[string]map[string]any `json:"components"`
}

func NativeCrmSource139Contract() (*CrmSource139Contract, error) {
	var contract CrmSource139Contract
	d := json.NewDecoder(bytes.NewReader(crmSource139Raw))
	d.UseNumber()
	if err := d.Decode(&contract); err != nil {
		return nil, errors.New("contrato Source139 nativo ilegible")
	}
	if contract.NativeHash != CrmSource139NativeHash || contract.Profile.State != "unreleased" || contract.Profile.Ratified || contract.Profile.Published || contract.Profile.DeliveryEnabled || len(contract.Capabilities) != 0 {
		return nil, errors.New("contrato Source139 sin su gate privado nativo")
	}
	for _, key := range []string{"import_apply_original", "agenda"} {
		op, exists := contract.Operations[key]
		if !exists || op.Method != "GET" || op.ID() == "" || op.Scope() == "" {
			return nil, fmt.Errorf("lector Source139 no declarado: %s", key)
		}
	}
	return &contract, nil
}

func (o CrmSource139Operation) ID() string {
	value, _ := o.Operation["operationId"].(string)
	return value
}
func (o CrmSource139Operation) Scope() string {
	value, _ := o.Operation["x-required-scope"].(string)
	return value
}

func (c *CrmSource139Contract) ValidateParameter(key, location, name string, value any) error {
	op, exists := c.Operations[key]
	if !exists {
		return errors.New("operación Source139 ausente")
	}
	parameters, _ := op.Operation["parameters"].([]any)
	for _, raw := range parameters {
		p, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if p["name"] != name || p["in"] != location {
			continue
		}
		schema, _ := p["schema"].(map[string]any)
		if schema == nil {
			return errors.New("parámetro sin esquema nativo Source139")
		}
		return c.validate(schema, value, 0)
	}
	return errors.New("parámetro no declarado por el lector Source139")
}

func (c *CrmSource139Contract) AgendaSources() ([]string, error) {
	properties, _ := c.Components["schemas"]["AgendaList"]["properties"].(map[string]any)
	sources, _ := properties["sources"].(map[string]any)
	items, _ := sources["items"].(map[string]any)
	values, _ := items["enum"].([]any)
	if len(values) == 0 {
		return nil, errors.New("fuentes de agenda ausentes en el contrato nativo")
	}
	result := make([]string, 0, len(values))
	for _, raw := range values {
		value, ok := raw.(string)
		if !ok {
			return nil, errors.New("fuente de agenda nativa inválida")
		}
		result = append(result, value)
	}
	return result, nil
}

// Derive the success boundary from the exported schema rather than a parallel DTO catalog.
func (c *CrmSource139Contract) ValidateResponse(key string, body []byte) error {
	op, exists := c.Operations[key]
	if !exists {
		return errors.New("operación Source139 ausente")
	}
	responses, _ := op.Operation["responses"].(map[string]any)
	response, _ := responses["200"].(map[string]any)
	content, _ := response["content"].(map[string]any)
	media, _ := content["application/json"].(map[string]any)
	schema, _ := media["schema"].(map[string]any)
	value, err := decodeCrmSource139JSON(body)
	if err != nil || schema == nil {
		return errors.New("respuesta Source139 sin esquema/JSON nativo")
	}
	return c.validate(schema, value, 0)
}
