package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

var serviceLevelUUID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var serviceLevelDate = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)

var serviceLevelExamples = map[string]string{
	"configure":       `{"cycle_id":null,"expected_version":null,"policy_id":null,"policy_version":1,"first_response_minutes":60,"first_response_risk_minutes":10,"next_response_minutes":60,"next_response_risk_minutes":null,"resolution_minutes":480,"resolution_risk_minutes":60,"pause_first_response":false,"pause_next_response":true,"pause_resolution":true,"calendar_id":null,"calendar_version":null}`,
	"pause":           `{"cycle_id":"01900000-0000-7000-8000-000000000001","expected_version":1,"reason":"approval","first_response":false,"next_response":true,"resolution":true}`,
	"resume":          `{"cycle_id":"01900000-0000-7000-8000-000000000001","expected_version":2,"reason":"approval"}`,
	"update-calendar": `{"id":null,"expected_version":null,"name":"Servicio","timezone":"Europe/Madrid","mode":"business","windows":[{"weekday":1,"start_minute":540,"end_minute":1020}],"holidays":[],"exceptions":[]}`,
}

type serviceLevelField struct {
	kind       string
	nullable   bool
	min, max   int64
	properties map[string]serviceLevelField
	items      *serviceLevelField
}

func serviceLevelObject(properties map[string]serviceLevelField) serviceLevelField {
	return serviceLevelField{kind: "object", properties: properties}
}

func serviceLevelArray(item serviceLevelField, max int64) serviceLevelField {
	return serviceLevelField{kind: "array", items: &item, max: max}
}

// These closed shapes are the native owner's publicSchema(..., false), not the
// internal Command DTOs. Authority, effect UUIDs and *_uuid keys are absent.
func serviceLevelSchema(action string) serviceLevelField {
	uuid := serviceLevelField{kind: "uuid"}
	nullUUID := serviceLevelField{kind: "uuid", nullable: true}
	version := serviceLevelField{kind: "integer", min: 1}
	nullVersion := serviceLevelField{kind: "integer", min: 1, nullable: true}
	minute := serviceLevelField{kind: "integer"}
	risk := serviceLevelField{kind: "integer", nullable: true}
	boolean := serviceLevelField{kind: "boolean"}
	text := serviceLevelField{kind: "string", min: 1, max: 255}
	switch action {
	case "configure":
		return serviceLevelObject(map[string]serviceLevelField{
			"cycle_id": nullUUID, "expected_version": nullVersion, "policy_id": nullUUID, "policy_version": version,
			"first_response_minutes": minute, "first_response_risk_minutes": risk, "next_response_minutes": minute, "next_response_risk_minutes": risk,
			"resolution_minutes": minute, "resolution_risk_minutes": risk, "pause_first_response": boolean, "pause_next_response": boolean,
			"pause_resolution": boolean, "calendar_id": nullUUID, "calendar_version": nullVersion,
		})
	case "pause":
		return serviceLevelObject(map[string]serviceLevelField{"cycle_id": uuid, "expected_version": version, "reason": text, "first_response": boolean, "next_response": boolean, "resolution": boolean})
	case "resume":
		return serviceLevelObject(map[string]serviceLevelField{"cycle_id": uuid, "expected_version": version, "reason": text})
	case "update-calendar":
		window := serviceLevelObject(map[string]serviceLevelField{"start_minute": {kind: "integer", max: 1439}, "end_minute": {kind: "integer", min: 1, max: 1440}})
		weekly := serviceLevelObject(map[string]serviceLevelField{"weekday": {kind: "integer", min: 1, max: 7}, "start_minute": window.properties["start_minute"], "end_minute": window.properties["end_minute"]})
		exception := serviceLevelObject(map[string]serviceLevelField{"date": {kind: "date"}, "windows": serviceLevelArray(window, 1000)})
		return serviceLevelObject(map[string]serviceLevelField{"id": nullUUID, "expected_version": nullVersion, "name": text, "timezone": text,
			"mode": {kind: "mode"}, "windows": serviceLevelArray(weekly, 1000), "holidays": serviceLevelArray(serviceLevelField{kind: "date"}, 10000), "exceptions": serviceLevelArray(exception, 10000)})
	}
	return serviceLevelField{}
}

func decodeServiceLevelJSON(body []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var result map[string]any
	if err := decoder.Decode(&result); err != nil || result == nil {
		return nil, errors.New("debe ser un objeto JSON válido")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("debe contener un único objeto JSON")
	}
	return result, nil
}

func validateServiceLevelBody(action string, body []byte) error {
	if len(body) > 262144 {
		return apierr.Usagef("el cuerpo SLA supera 262144 bytes")
	}
	value, err := decodeServiceLevelJSON(body)
	if err != nil {
		return apierr.Usagef("el cuerpo SLA %s", err)
	}
	if err := validateServiceLevelField(serviceLevelSchema(action), value, "payload"); err != nil {
		return err
	}
	switch action {
	case "configure":
		if (value["calendar_id"] == nil) != (value["calendar_version"] == nil) {
			return apierr.Usagef("calendar_id y calendar_version deben declararse juntos")
		}
		if value["policy_id"] == nil && value["policy_version"] != json.Number("1") {
			return apierr.Usagef("una política nueva comienza en policy_version=1")
		}
		if value["cycle_id"] != nil && value["expected_version"] == nil {
			return apierr.Usagef("revisar un ciclo exige expected_version")
		}
	case "update-calendar":
		if (value["id"] == nil) != (value["expected_version"] == nil) {
			return apierr.Usagef("un calendario nuevo exige id y expected_version null; revisar exige ambos valores")
		}
	case "pause":
		if value["first_response"] == false && value["next_response"] == false && value["resolution"] == false {
			return apierr.Usagef("la pausa debe seleccionar al menos un reloj")
		}
	}
	if action == "pause" || action == "resume" {
		reason, _ := value["reason"].(string)
		if strings.TrimSpace(reason) == "" || reason == "waiting_customer" {
			return apierr.Usagef("reason requiere una pausa manual; waiting_customer pertenece al hecho canónico del cliente")
		}
	}
	return nil
}

func validateServiceLevelField(field serviceLevelField, value any, path string) error {
	if value == nil && field.nullable {
		return nil
	}
	valid := false
	switch field.kind {
	case "object":
		object, ok := value.(map[string]any)
		if !ok || len(object) != len(field.properties) {
			break
		}
		for key, child := range field.properties {
			v, exists := object[key]
			if !exists {
				return apierr.Usagef("%s.%s debe estar presente, incluido null cuando corresponda", path, key)
			}
			if err := validateServiceLevelField(child, v, path+"."+key); err != nil {
				return err
			}
		}
		return nil
	case "array":
		items, ok := value.([]any)
		if !ok || int64(len(items)) > field.max {
			break
		}
		for _, item := range items {
			if err := validateServiceLevelField(*field.items, item, path+"[]"); err != nil {
				return err
			}
		}
		return nil
	case "integer":
		number, ok := value.(json.Number)
		if ok {
			n, err := number.Int64()
			valid = err == nil && n >= field.min && (field.max == 0 || n <= field.max)
		}
	case "boolean":
		_, valid = value.(bool)
	case "string", "uuid", "date", "mode":
		text, ok := value.(string)
		if ok {
			length := int64(utf8.RuneCountInString(text))
			valid = length >= field.min && (field.max == 0 || length <= field.max)
			if field.kind == "uuid" {
				valid = serviceLevelUUID.MatchString(text)
			} else if field.kind == "date" {
				valid = serviceLevelDate.MatchString(text)
			} else if field.kind == "mode" {
				valid = text == "business" || text == "always_open"
			}
		}
	}
	if !valid {
		return apierr.Usagef("%s no conserva el tipo, formato, campos obligatorios o límites del contrato SLA", path)
	}
	return nil
}

func validateServiceLevelIdempotencyKey(key string) error {
	if len(key) < 1 || len(key) > 255 {
		return apierr.Usagef("--idempotency-key es obligatoria y debe contener 1–255 caracteres ASCII imprimibles")
	}
	for _, char := range key {
		if char < 32 || char > 126 {
			return apierr.Usagef("--idempotency-key sólo admite ASCII imprimible")
		}
	}
	return nil
}

func validateServiceLevelReceipt(body []byte) error {
	value, err := decodeServiceLevelJSON(body)
	if err == nil {
		data, ok := value["data"].(map[string]any)
		if ok {
			err = validateServiceLevelField(serviceLevelObject(map[string]serviceLevelField{"id": {kind: "uuid"}, "version": {kind: "integer", min: 1}}), data, "data")
			if err == nil {
				return nil
			}
		}
	}
	return &apierr.TransportError{Err: errors.New("resultado SLA sin confirmar: la respuesta no contiene el recibo original con id UUIDv7 y version positiva; conserva el cuerpo y la clave original para reconciliarlo mediante replay explícito")}
}
