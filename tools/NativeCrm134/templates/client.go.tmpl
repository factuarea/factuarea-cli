// Generated from one private native export. Raw mutation bytes and literal keys are never replaced.
package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/spec"
)

// Reads use the owner's Original selector; they never call a write or infer confirmation from Current.
func (c *Client) CrmNative134Request(ctx context.Context, operation, identity string, raw []byte, key string, query url.Values) (*Response, error) {
	prepared, err := crmNative134Prepare(operation, identity, raw, key, query)
	if err != nil {
		return nil, err
	}
	one := *c
	WithSingleDispatch()(&one)
	response, err := one.Do(ctx, prepared.operation.Method, prepared.path, prepared.body, prepared.headers)
	if err != nil {
		return response, err
	}
	media, _, mediaErr := mime.ParseMediaType(response.ContentType)
	if mediaErr != nil || media != "application/json" || prepared.contract.ValidateResponseStatus(operation, response.StatusCode, response.Body) != nil {
		return response, crmJourney140Unknown()
	}
	if err := crmNative134Result(operation, identity, prepared.intent, response.Body); err != nil {
		return response, crmJourney140Unknown()
	}
	return response, nil
}

// Dry-run validates exactly the same owner selector and wire as dispatch, without auth or HTTP.
func ValidateCrmNative134Intent(operation, identity string, raw []byte, key string, query url.Values) error {
	_, err := crmNative134Prepare(operation, identity, raw, key, query)
	return err
}

type crmNative134Prepared struct {
	contract  *spec.CrmSource139Contract
	operation spec.CrmSource139Operation
	path      string
	body      []byte
	headers   map[string]string
	intent    map[string]any
}

func crmNative134Prepare(operation, identity string, raw []byte, key string, query url.Values) (*crmNative134Prepared, error) {
	contract, err := spec.NativeCrm134Contract()
	if err != nil {
		return nil, err
	}
	op, exists := contract.Operations[operation]
	if !exists || operation == "restore_intent" {
		return nil, apierr.Usagef("operación fuera del consumidor Native134")
	}
	path := "/v1" + op.Path
	parameter := ""
	switch operation {
	case "stage_update", "stage_original":
		parameter = "stage"
	case "bulk_original":
		parameter = "ticket"
	case "restore_original":
		parameter = "document"
	}
	if parameter != "" {
		if contract.ValidateParameter(operation, "path", parameter, identity) != nil {
			return nil, apierr.Usagef("identidad fuera del UUID opaco nativo")
		}
		path = strings.ReplaceAll(path, "{"+parameter+"}", url.PathEscape(identity))
	} else if identity != "" {
		return nil, apierr.Usagef("la operación no admite una identidad en ruta")
	}
	mutating := op.Method != http.MethodGet
	original := strings.HasSuffix(operation, "_original")
	if mutating || original {
		if !crmJourney140LiteralKey(key) || contract.ValidateParameter(operation, "header", "Idempotency-Key", key) != nil || len(raw) == 0 {
			return nil, apierr.Usagef("conserva cuerpo original y clave ASCII literal representable, sin generar ni recortar")
		}
	} else if len(raw) != 0 || key != "" {
		return nil, apierr.Usagef("la lectura actual no admite cuerpo o clave de efecto")
	}
	values := url.Values{}
	for name, entries := range query {
		values[name] = append([]string(nil), entries...)
	}
	var intent map[string]any
	if mutating || original {
		if ((operation == "stage_update" || operation == "stage_original") && len(raw) > 16384) || ((operation == "bulk_apply" || operation == "bulk_original") && len(raw) > 262144) {
			return nil, apierr.Usagef("el primer cuerpo supera el límite real del dueño")
		}
		intent, err = crmNative134Object(raw)
		if err != nil {
			return nil, apierr.Usagef("el cuerpo original debe conservar JSON cerrado sin campos duplicados")
		}
		inputOperation := operation
		if original {
			inputOperation = map[string]string{"stage_original": "stage_update", "bulk_original": "bulk_apply", "restore_original": "restore_intent"}[operation]
		}
		if contract.ValidateRequest(inputOperation, raw) != nil {
			return nil, apierr.Usagef("cuerpo fuera del esquema real del intento original")
		}
		if original && len(values) != 0 {
			return nil, apierr.Usagef("Original deriva sólo los selectores del mismo intento, sin query alternativa")
		}
		switch operation {
		case "stage_update", "stage_original":
			fingerprint, fingerprintErr := crmNative134StageFingerprint(identity, intent)
			if fingerprintErr != nil {
				return nil, fingerprintErr
			}
			if original {
				values.Set("request_fingerprint", fingerprint)
			}
		case "bulk_apply", "bulk_original":
			items, itemErr := crmNative134BulkSelection(intent)
			if itemErr != nil {
				return nil, itemErr
			}
			if original {
				var selected map[string]any
				for _, item := range items {
					if strings.EqualFold(item["id"].(string), identity) {
						selected = item
					}
				}
				if selected == nil {
					return nil, apierr.Usagef("el ticket no pertenece a la selección del primer POST")
				}
				values.Set("action", intent["action"].(string))
				values.Set("expected_version", string(selected["expected_version"].(json.Number)))
				for name, value := range intent {
					if name == "action" || name == "items" {
						continue
					}
					if value == nil {
						values.Set(name, "null")
					} else if text, ok := value.(string); ok {
						values.Set(name, text)
					} else {
						encoded, encodeErr := json.Marshal(value)
						if encodeErr != nil {
							return nil, apierr.Usagef("payload Original no representable")
						}
						values.Set(name, string(encoded))
					}
				}
			}
		case "restore_original":
			version, versionErr := crmNative134Version(intent["expected_version"])
			if versionErr != nil {
				return nil, versionErr
			}
			values.Set("expected_version", strconv.FormatInt(version, 10))
		}
	}
	if err := crmNative134Query(contract, operation, values); err != nil {
		return nil, err
	}
	if len(values) != 0 {
		path += "?" + values.Encode()
	}
	headers := map[string]string{}
	if key != "" {
		headers["Idempotency-Key"] = key
	}
	body := raw
	if op.Method == http.MethodGet {
		body = nil
	}
	return &crmNative134Prepared{contract: contract, operation: op, path: path, body: body, headers: headers, intent: intent}, nil
}

func crmNative134Query(contract *spec.CrmSource139Contract, operation string, values url.Values) error {
	op := contract.Operations[operation]
	parameters, _ := op.Operation["parameters"].([]any)
	required := map[string]bool{}
	for _, entry := range parameters {
		parameter, ok := entry.(map[string]any)
		if !ok || parameter["in"] != "query" {
			continue
		}
		name, _ := parameter["name"].(string)
		if parameter["required"] == true {
			required[name] = true
		}
	}
	for name, entries := range values {
		if len(entries) != 1 {
			return apierr.Usagef("un selector query no puede repetirse")
		}
		var value any = entries[0]
		if name == "page" || name == "per_page" || name == "expected_version" {
			integer, err := strconv.ParseInt(entries[0], 10, 64)
			if err != nil || strconv.FormatInt(integer, 10) != entries[0] {
				return apierr.Usagef("el selector entero debe conservar su forma decimal")
			}
			value = json.Number(entries[0])
		}
		if contract.ValidateParameter(operation, "query", name, value) != nil {
			return apierr.Usagef("selector fuera del esquema query nativo: %s", name)
		}
		delete(required, name)
	}
	if len(required) != 0 {
		return apierr.Usagef("faltan selectores query nativos obligatorios")
	}
	return nil
}

func crmNative134StageFingerprint(identity string, intent map[string]any) (string, error) {
	for _, field := range []string{"expected_version", "expected_pipeline_version"} {
		if _, err := crmNative134Version(intent[field]); err != nil {
			return "", err
		}
	}
	if intent["wip_limit"] != nil {
		if _, err := crmNative134Version(intent["wip_limit"]); err != nil {
			return "", err
		}
	}
	pipeline, ok := intent["pipeline_id"].(string)
	if !ok {
		return "", apierr.Usagef("pipeline del intento Stage ausente")
	}
	normalized := map[string]any{"stage_uuid": strings.ToLower(identity), "pipeline_uuid": strings.ToLower(pipeline),
		"expected_version": intent["expected_version"], "expected_pipeline_version": intent["expected_pipeline_version"],
		"name": intent["name"], "mapped_status": intent["mapped_status"], "wip_limit": intent["wip_limit"]}
	var canonical bytes.Buffer
	encoder := json.NewEncoder(&canonical)
	encoder.SetEscapeHTML(false)
	if encoder.Encode(normalized) != nil {
		return "", apierr.Usagef("intención Stage no representable")
	}
	digest := sha256.Sum256(bytes.TrimSuffix(canonical.Bytes(), []byte("\n")))
	return hex.EncodeToString(digest[:]), nil
}

func crmNative134BulkSelection(intent map[string]any) ([]map[string]any, error) {
	selection, ok := intent["items"].([]any)
	if !ok || len(selection) < 1 || len(selection) > 100 {
		return nil, apierr.Usagef("la selección Bulk debe conservar 1..100 tickets")
	}
	items := []map[string]any{}
	seen := map[string]bool{}
	for _, raw := range selection {
		item, ok := raw.(map[string]any)
		if !ok {
			return nil, apierr.Usagef("elemento Bulk no tipado")
		}
		identity, ok := item["id"].(string)
		if !ok || seen[strings.ToLower(identity)] {
			return nil, apierr.Usagef("Bulk no admite un ticket repetido")
		}
		if _, err := crmNative134Version(item["expected_version"]); err != nil {
			return nil, err
		}
		seen[strings.ToLower(identity)] = true
		items = append(items, item)
	}
	return items, nil
}

func crmNative134Result(operation, identity string, intent map[string]any, raw []byte) error {
	envelope, err := crmNative134Object(raw)
	if err != nil {
		return err
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		return fmt.Errorf("resultado Native134 sin data tipado")
	}
	switch operation {
	case "stage_update", "stage_original":
		id, _ := data["id"].(string)
		pipeline, _ := data["pipeline_id"].(string)
		expected, expectedErr := crmNative134Version(intent["expected_version"])
		if expectedErr != nil || !strings.EqualFold(id, identity) || !strings.EqualFold(pipeline, intent["pipeline_id"].(string)) || data["version"] != json.Number(strconv.FormatInt(expected+1, 10)) || data["pipeline_version"] != intent["expected_pipeline_version"] || data["name"] != intent["name"] || data["mapped_status"] != intent["mapped_status"] || data["wip_limit"] != intent["wip_limit"] || data["status"] != "active" {
			return fmt.Errorf("Stage Original no conserva identidad, CAS y resultado de la intención")
		}
	case "bulk_apply":
		selection, err := crmNative134BulkSelection(intent)
		if err != nil || data["action"] != intent["action"] {
			return fmt.Errorf("resultado Bulk de otra intención")
		}
		results, _ := data["results"].([]any)
		total, err := crmNative134Version(data["total"])
		if err != nil || total != int64(len(selection)) || len(results) != len(selection) {
			return fmt.Errorf("resultado Bulk incompleto")
		}
		counts := map[string]int64{"confirmed": 0, "rejected": 0, "unknown": 0}
		for index, raw := range results {
			item, ok := raw.(map[string]any)
			if !ok || item["index"] != json.Number(strconv.Itoa(index)) || !strings.EqualFold(item["id"].(string), selection[index]["id"].(string)) || item["expected_version"] != selection[index]["expected_version"] || item["can_retry"] != false {
				return fmt.Errorf("resultado Bulk no conserva selección/CAS/no-retry")
			}
			if err := crmNative134BulkItemResult(item); err != nil {
				return err
			}
			counts[item["state"].(string)]++
		}
		for state, count := range counts {
			if data[state] != json.Number(strconv.FormatInt(count, 10)) {
				return fmt.Errorf("contadores Bulk no corresponden a sus elementos")
			}
		}
		if data["partial"] != (counts["confirmed"] > 0 && counts["confirmed"] < total) {
			return fmt.Errorf("estado parcial Bulk incoherente")
		}
	case "bulk_original":
		selection, err := crmNative134BulkSelection(intent)
		if err != nil || !strings.EqualFold(data["id"].(string), identity) || data["can_retry"] != false {
			return fmt.Errorf("Original Bulk de otro ticket")
		}
		if err := crmNative134BulkItemResult(data); err != nil {
			return err
		}
		for _, selected := range selection {
			if strings.EqualFold(selected["id"].(string), identity) && data["expected_version"] == selected["expected_version"] {
				return nil
			}
		}
		return fmt.Errorf("Original Bulk de otro CAS")
	case "restore_original":
		document, _ := data["document"].(map[string]any)
		if document == nil || !strings.EqualFold(document["id"].(string), identity) || document["expected_version"] != intent["expected_version"] {
			return fmt.Errorf("Restore Original de otra identidad o revisión")
		}
		if data["state"] == "confirmed" {
			original, _ := data["restore_original"].(map[string]any)
			expected, expectedErr := crmNative134Version(intent["expected_version"])
			if original == nil || expectedErr != nil || !strings.EqualFold(original["id"].(string), identity) || original["version"] != json.Number(strconv.FormatInt(expected+1, 10)) {
				return fmt.Errorf("Restore no conserva la revisión original confirmada")
			}
		}
	}
	return nil
}

// Keep the real owner's state/receipt invariant; an envelope is not a second receipt.
func crmNative134BulkItemResult(item map[string]any) error {
	expected, err := crmNative134Version(item["expected_version"])
	if err != nil {
		return err
	}
	switch item["state"] {
	case "confirmed":
		version, versionErr := crmNative134Version(item["result_version"])
		if versionErr != nil || item["reason"] != nil || item["effect_id"] == nil || (version != expected && version-1 != expected) {
			return fmt.Errorf("confirmed Bulk sin Original/version coherente")
		}
	case "rejected":
		if item["reason"] == nil || item["reason"] == "original_unconfirmed" || item["effect_id"] != nil || item["result_version"] != nil {
			return fmt.Errorf("rejected Bulk no conserva su rechazo sin efecto")
		}
	case "unknown":
		if item["reason"] != "original_unconfirmed" || item["effect_id"] != nil || item["result_version"] != nil {
			return fmt.Errorf("unknown Bulk no puede contener un resultado confirmado")
		}
	default:
		return fmt.Errorf("estado Bulk no declarado")
	}
	return nil
}

func crmNative134Version(value any) (int64, error) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, apierr.Usagef("la revisión conserva su tipo entero JSON")
	}
	integer, err := strconv.ParseInt(string(number), 10, 64)
	if err != nil || integer < 1 || strconv.FormatInt(integer, 10) != string(number) {
		return 0, apierr.Usagef("la revisión debe ser un entero decimal positivo")
	}
	return integer, nil
}

// Detect duplicate keys before schema validation while keeping the original wire untouched.
func crmNative134Object(raw []byte) (map[string]any, error) {
	if !utf8.Valid(raw) {
		return nil, fmt.Errorf("JSON requiere UTF-8 válido")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var read func(int) (any, error)
	read = func(depth int) (any, error) {
		if depth > 32 {
			return nil, fmt.Errorf("JSON fuera de profundidad Native134")
		}
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		switch token {
		case json.Delim('{'):
			object := map[string]any{}
			for decoder.More() {
				key, err := decoder.Token()
				name, ok := key.(string)
				if err != nil || !ok {
					return nil, fmt.Errorf("campo JSON inválido")
				}
				if _, exists := object[name]; exists {
					return nil, fmt.Errorf("campo JSON duplicado")
				}
				value, err := read(depth + 1)
				if err != nil {
					return nil, err
				}
				object[name] = value
			}
			_, err := decoder.Token()
			return object, err
		case json.Delim('['):
			array := []any{}
			for decoder.More() {
				value, err := read(depth + 1)
				if err != nil {
					return nil, err
				}
				array = append(array, value)
			}
			_, err := decoder.Token()
			return array, err
		default:
			return token, nil
		}
	}
	value, err := read(0)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("JSON contiene más de un valor")
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("JSON requiere un objeto")
	}
	return object, nil
}
