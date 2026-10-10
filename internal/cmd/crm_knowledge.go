package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/spf13/cobra"
)

// Knowledge and help-center intents have native byte fingerprints. Validate
// their closed contract without normalizing a caller's original JSON bytes.
func validateKnowledgeBody(op genOp, body []byte) error {
	if op.NativeContract == nil {
		return nil
	}
	if limit := op.NativeContract.MaxBodyBytes; limit > 0 && len(body) > limit {
		return apierr.Usagef("el cuerpo nativo excede %d bytes", limit)
	}
	value, err := knowledgeJSON(body)
	if err != nil {
		return apierr.Usagef("JSON nativo inválido: %v", err)
	}
	if err := knowledgeSchema(op.NativeContract.RequestSchema, value); err != nil {
		return apierr.Usagef("cuerpo nativo inválido: %v", err)
	}
	return nil
}

func validateKnowledgeFlags(cmd *cobra.Command, op genOp, key string) error {
	if op.NativeContract == nil {
		return nil
	}
	if !op.isMutating() && op.IdempotencyRequired && key == "" {
		return apierr.Usagef("la recuperación requiere --idempotency-key con la clave original; no se genera ni se reenvía una escritura")
	}
	if key != "" && (len(key) > 255 || !regexp.MustCompile(`^[ -~]+$`).MatchString(key)) {
		return apierr.Usagef("--idempotency-key debe contener entre 1 y 255 caracteres ASCII imprimibles de la clave original")
	}
	for name, schema := range op.NativeContract.QuerySchemas {
		if !cmd.Flags().Changed(name) {
			continue
		}
		value, _ := cmd.Flags().GetString(name)
		var constraint map[string]any
		if err := decodeKnowledgeJSON([]byte(schema), &constraint); err != nil {
			return err
		}
		var typed any = value
		if constraint["type"] == "integer" {
			typed = json.Number(value)
		}
		if err := checkKnowledgeSchema(constraint, typed, "--"+name); err != nil {
			return apierr.Usagef("consulta nativa inválida: %v", err)
		}
	}
	return nil
}

func validateKnowledgeResponse(op genOp, status int, response, request []byte, args []string) error {
	if op.NativeContract == nil {
		return nil
	}
	invalid := func() error {
		if op.isMutating() {
			return &apierr.TransportError{Err: fmt.Errorf("el acuse no confirma el recibo nativo de la intención original")}
		}
		return &apierr.APIError{Type: "api_error", Code: "invalid_crm_response", Message: "La respuesta no cumple el contrato nativo del productor."}
	}
	schema, declared := op.NativeContract.ResponseSchemas[strconv.Itoa(status)]
	value, err := knowledgeJSON(response)
	if !declared || err != nil || knowledgeSchema(schema, value) != nil {
		return invalid()
	}
	envelope, _ := value.(map[string]any)
	data, _ := envelope["data"].(map[string]any)
	if _, receipt := data["effect_id"]; !receipt {
		return nil
	}
	// These relations are documented by the original receipt schemas. They
	// distinguish the original snapshot from a later head with valid fields.
	snapshotName := "article"
	if _, ok := data["center"]; ok {
		snapshotName = "center"
	}
	if _, ok := data["category"]; ok {
		snapshotName = "category"
	}
	snapshot, _ := data[snapshotName].(map[string]any)
	expected, expectedOK := knowledgeInt(data["expected_version"])
	if snapshotName == "category" && data["expected_version"] == nil {
		expected, expectedOK = 0, true
	}
	version, versionOK := knowledgeInt(snapshot["version"])
	if !expectedOK || !versionOK || expected == int64(^uint64(0)>>1) || version != expected+1 {
		return invalid()
	}
	if snapshotName == "category" {
		taxExpected, ok := knowledgeInt(data["expected_taxonomy_version"])
		taxVersion, vOK := knowledgeInt(snapshot["taxonomy_version"])
		if !ok || !vOK || taxExpected == int64(^uint64(0)>>1) || taxVersion != taxExpected+1 {
			return invalid()
		}
	}
	if op.isMutating() {
		intent, err := knowledgeJSON(request)
		if err != nil {
			return invalid()
		}
		original, _ := intent.(map[string]any)
		if !reflect.DeepEqual(data["expected_version"], original["expected_version"]) {
			return invalid()
		}
		if snapshotName == "category" && !reflect.DeepEqual(data["expected_taxonomy_version"], original["expected_taxonomy_version"]) {
			return invalid()
		}
		if id, exists := original["id"]; exists && id != nil && !sameKnowledgeID(id, snapshot["id"]) {
			return invalid()
		}
		if len(args) > 0 && !sameKnowledgeID(args[0], snapshot["id"]) {
			return invalid()
		}
	}
	return nil
}

func sameKnowledgeID(a, b any) bool {
	first, firstOK := a.(string)
	second, secondOK := b.(string)
	return firstOK && secondOK && strings.EqualFold(first, second)
}

func knowledgeJSON(body []byte) (any, error) {
	var value any
	err := decodeKnowledgeJSON(body, &value)
	return value, err
}

func decodeKnowledgeJSON(body []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("se requiere un único valor JSON")
	}
	return nil
}

func knowledgeInt(value any) (int64, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	integer, err := strconv.ParseInt(number.String(), 10, 64)
	return integer, err == nil
}

func knowledgeSchema(raw string, value any) error {
	var schema map[string]any
	if err := decodeKnowledgeJSON([]byte(raw), &schema); err != nil {
		return err
	}
	return checkKnowledgeSchema(schema, value, "$")
}

// This boundary checks the keywords present in the frozen native KB/PCH
// contract. Authorization, availability and workflow remain server-owned.
func checkKnowledgeSchema(schema map[string]any, value any, path string) error {
	fail := func(message string) error { return fmt.Errorf("%s: %s", path, message) }
	for _, keyword := range []string{"anyOf", "oneOf", "allOf"} {
		variants, _ := schema[keyword].([]any)
		if len(variants) == 0 {
			continue
		}
		matches := 0
		for _, variant := range variants {
			child, _ := variant.(map[string]any)
			if checkKnowledgeSchema(child, value, path) == nil {
				matches++
			}
		}
		if (keyword == "anyOf" && matches == 0) || (keyword == "oneOf" && matches != 1) || (keyword == "allOf" && matches != len(variants)) {
			return fail("no cumple " + keyword + " nativo")
		}
	}
	if denied, ok := schema["not"].(map[string]any); ok && checkKnowledgeSchema(denied, value, path) == nil {
		return fail("variante rechazada por el contrato")
	}
	if condition, ok := schema["if"].(map[string]any); ok {
		branch := "else"
		if checkKnowledgeSchema(condition, value, path) == nil {
			branch = "then"
		}
		if selected, ok := schema[branch].(map[string]any); ok {
			if err := checkKnowledgeSchema(selected, value, path); err != nil {
				return err
			}
		}
	}
	if constant, exists := schema["const"]; exists && !reflect.DeepEqual(constant, value) {
		return fail("valor literal nativo incorrecto")
	}
	if enum, ok := schema["enum"].([]any); ok {
		found := false
		for _, item := range enum {
			if reflect.DeepEqual(item, value) {
				found = true
				break
			}
		}
		if !found {
			return fail("valor fuera del enum nativo")
		}
	}
	if expected, present := schema["type"]; present {
		allowed := []any{expected}
		if types, ok := expected.([]any); ok {
			allowed = types
		}
		matches := false
		for _, typ := range allowed {
			switch typ {
			case "null":
				matches = matches || value == nil
			case "object":
				_, ok := value.(map[string]any)
				matches = matches || ok
			case "array":
				_, ok := value.([]any)
				matches = matches || ok
			case "string":
				_, ok := value.(string)
				matches = matches || ok
			case "boolean":
				_, ok := value.(bool)
				matches = matches || ok
			case "integer":
				_, ok := knowledgeInt(value)
				matches = matches || ok
			case "number":
				_, ok := value.(json.Number)
				matches = matches || ok
			}
		}
		if !matches {
			return fail("tipo nativo incorrecto")
		}
	}
	if object, ok := value.(map[string]any); ok {
		if required, ok := schema["required"].([]any); ok {
			for _, item := range required {
				name, _ := item.(string)
				if _, exists := object[name]; !exists {
					return fail("falta " + name)
				}
			}
		}
		properties, _ := schema["properties"].(map[string]any)
		for name, item := range object {
			if constraint, exists := properties[name]; exists {
				child, _ := constraint.(map[string]any)
				if err := checkKnowledgeSchema(child, item, path+"."+name); err != nil {
					return err
				}
			} else if schema["additionalProperties"] == false {
				return fail("campo no declarado: " + name)
			}
		}
	}
	if text, ok := value.(string); ok {
		length := utf8.RuneCountInString(text)
		if minimum, ok := knowledgeInt(schema["minLength"]); ok && int64(length) < minimum {
			return fail("texto demasiado corto")
		}
		if maximum, ok := knowledgeInt(schema["maxLength"]); ok && int64(length) > maximum {
			return fail("texto demasiado largo")
		}
		if pattern, ok := schema["pattern"].(string); ok {
			compiled, err := regexp.Compile(pattern)
			if err != nil || !compiled.MatchString(text) {
				return fail("patrón nativo incorrecto")
			}
		}
	}
	if integer, ok := knowledgeInt(value); ok {
		if minimum, ok := knowledgeInt(schema["minimum"]); ok && integer < minimum {
			return fail("entero inferior al mínimo")
		}
		if maximum, ok := knowledgeInt(schema["maximum"]); ok && integer > maximum {
			return fail("entero superior al máximo")
		}
	}
	if array, ok := value.([]any); ok {
		if maximum, ok := knowledgeInt(schema["maxItems"]); ok && int64(len(array)) > maximum {
			return fail("lista demasiado larga")
		}
		if minimum, ok := knowledgeInt(schema["minItems"]); ok && int64(len(array)) < minimum {
			return fail("lista demasiado corta")
		}
		if schema["uniqueItems"] == true {
			seen := map[string]bool{}
			for _, item := range array {
				raw, _ := json.Marshal(item)
				if seen[string(raw)] {
					return fail("lista con duplicados")
				}
				seen[string(raw)] = true
			}
		}
		if itemSchema, ok := schema["items"].(map[string]any); ok {
			for i, item := range array {
				if err := checkKnowledgeSchema(itemSchema, item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
