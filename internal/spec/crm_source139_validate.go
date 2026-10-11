package spec

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"math/big"
	"net/mail"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var crmSource139UUID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var errCrmSource139Shape = errors.New("respuesta no conserva el esquema nativo Source139; ningún resultado se confirma")

func decodeCrmSource139JSON(body []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	var value any
	if err := d.Decode(&value); err != nil {
		return nil, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, errCrmSource139Shape
	}
	return value, nil
}

// Validates schema primitives emitted by the exact selected native operations.
// This boundary never infers current authority or configured reference grants.
func (c *CrmSource139Contract) validate(schema map[string]any, value any, depth int) error {
	if depth > 64 {
		return errCrmSource139Shape
	}
	if ref, ok := schema["$ref"].(string); ok {
		parts := strings.Split(ref, "/")
		if len(parts) != 4 || parts[0] != "#" || parts[1] != "components" {
			return errCrmSource139Shape
		}
		resolved := c.Components[parts[2]][parts[3]]
		if resolved == nil {
			return errCrmSource139Shape
		}
		if err := c.validate(resolved, value, depth+1); err != nil {
			return err
		}
	}
	if schemas, ok := schema["allOf"].([]any); ok {
		for _, child := range schemas {
			s, ok := child.(map[string]any)
			if !ok || c.validate(s, value, depth+1) != nil {
				return errCrmSource139Shape
			}
		}
	}
	if schemas, ok := schema["oneOf"].([]any); ok {
		matches := 0
		for _, child := range schemas {
			s, ok := child.(map[string]any)
			if ok && c.validate(s, value, depth+1) == nil {
				matches++
			}
		}
		if matches != 1 {
			return errCrmSource139Shape
		}
	}
	if schemas, ok := schema["anyOf"].([]any); ok {
		matches := 0
		for _, child := range schemas {
			s, ok := child.(map[string]any)
			if ok && c.validate(s, value, depth+1) == nil {
				matches++
			}
		}
		if matches == 0 {
			return errCrmSource139Shape
		}
	}
	if condition, ok := schema["if"].(map[string]any); ok {
		branch := "else"
		if c.validate(condition, value, depth+1) == nil {
			branch = "then"
		}
		if child, ok := schema[branch].(map[string]any); ok && c.validate(child, value, depth+1) != nil {
			return errCrmSource139Shape
		}
	}
	if excluded, ok := schema["not"].(map[string]any); ok && c.validate(excluded, value, depth+1) == nil {
		return errCrmSource139Shape
	}
	if constant, exists := schema["const"]; exists && !reflect.DeepEqual(constant, value) {
		return errCrmSource139Shape
	}
	if values, ok := schema["enum"].([]any); ok {
		found := false
		for _, candidate := range values {
			if reflect.DeepEqual(candidate, value) {
				found = true
			}
		}
		if !found {
			return errCrmSource139Shape
		}
	}
	if kind, exists := schema["type"]; exists {
		valid := false
		switch kinds := kind.(type) {
		case string:
			valid = crmSource139Type(kinds, value)
		case []any:
			for _, kind := range kinds {
				text, ok := kind.(string)
				if ok && crmSource139Type(text, value) {
					valid = true
				}
			}
		}
		if !valid {
			return errCrmSource139Shape
		}
	}
	switch v := value.(type) {
	case map[string]any:
		if !crmSource139Size(schema, "minProperties", "maxProperties", len(v)) {
			return errCrmSource139Shape
		}
		properties, _ := schema["properties"].(map[string]any)
		if required, ok := schema["required"].([]any); ok {
			for _, key := range required {
				name, ok := key.(string)
				if !ok {
					return errCrmSource139Shape
				}
				if _, exists := v[name]; !exists {
					return errCrmSource139Shape
				}
			}
		}
		for key, child := range v {
			if names, ok := schema["propertyNames"].(map[string]any); ok && c.validate(names, key, depth+1) != nil {
				return errCrmSource139Shape
			}
			if property, exists := properties[key]; exists {
				s, ok := property.(map[string]any)
				if !ok || c.validate(s, child, depth+1) != nil {
					return errCrmSource139Shape
				}
			} else if schema["additionalProperties"] == false {
				return errCrmSource139Shape
			} else if extra, ok := schema["additionalProperties"].(map[string]any); ok && c.validate(extra, child, depth+1) != nil {
				return errCrmSource139Shape
			}
		}
	case []any:
		if !crmSource139Size(schema, "minItems", "maxItems", len(v)) {
			return errCrmSource139Shape
		}
		if schema["uniqueItems"] == true {
			for i := range v {
				for j := 0; j < i; j++ {
					if reflect.DeepEqual(v[i], v[j]) {
						return errCrmSource139Shape
					}
				}
			}
		}
		if item, exists := schema["items"]; exists {
			s, ok := item.(map[string]any)
			if !ok {
				return errCrmSource139Shape
			}
			for _, child := range v {
				if c.validate(s, child, depth+1) != nil {
					return errCrmSource139Shape
				}
			}
		}
	case string:
		if min, ok := crmSource139Number(schema["minLength"]); ok && float64(utf8.RuneCountInString(v)) < min {
			return errCrmSource139Shape
		}
		if max, ok := crmSource139Number(schema["maxLength"]); ok && float64(utf8.RuneCountInString(v)) > max {
			return errCrmSource139Shape
		}
		if pattern, ok := schema["pattern"].(string); ok {
			// Native PCRE uses this strict EOF assertion. RE2's \z has the same
			// meaning; unsupported expressions otherwise fail closed.
			pattern = strings.ReplaceAll(pattern, `$(?![\s\S])`, `\z`)
			r, err := regexp.Compile(pattern)
			if err != nil || !r.MatchString(v) {
				return errCrmSource139Shape
			}
		}
		if format, ok := schema["format"].(string); ok {
			switch format {
			case "date-time":
				if _, err := time.Parse(time.RFC3339Nano, v); err != nil {
					return errCrmSource139Shape
				}
			case "email":
				address, err := mail.ParseAddress(v)
				if err != nil || address.Address != v {
					return errCrmSource139Shape
				}
			case "date":
				if _, err := time.Parse("2006-01-02", v); err != nil {
					return errCrmSource139Shape
				}
			case "uuid":
				if !crmSource139UUID.MatchString(v) {
					return errCrmSource139Shape
				}
			}
		}
	case json.Number:
		n, ok := new(big.Rat).SetString(string(v))
		if !ok {
			return errCrmSource139Shape
		}
		if min, ok := crmSource139ExactNumber(schema["minimum"]); ok && n.Cmp(min) < 0 {
			return errCrmSource139Shape
		}
		if max, ok := crmSource139ExactNumber(schema["maximum"]); ok && n.Cmp(max) > 0 {
			return errCrmSource139Shape
		}
	}
	return nil
}

func crmSource139Size(schema map[string]any, minKey, maxKey string, size int) bool {
	if min, ok := crmSource139Number(schema[minKey]); ok && float64(size) < min {
		return false
	}
	if max, ok := crmSource139Number(schema[maxKey]); ok && float64(size) > max {
		return false
	}
	return true
}

func crmSource139Type(kind string, value any) bool {
	switch kind {
	case "null":
		return value == nil
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "integer":
		n, ok := crmSource139ExactNumber(value)
		return ok && n.IsInt()
	case "number":
		_, ok := crmSource139ExactNumber(value)
		return ok
	}
	return false
}

func crmSource139ExactNumber(value any) (*big.Rat, bool) {
	n, ok := value.(json.Number)
	if !ok {
		return nil, false
	}
	return new(big.Rat).SetString(string(n))
}

func crmSource139Number(value any) (float64, bool) {
	n, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	result, err := strconv.ParseFloat(string(n), 64)
	return result, err == nil && !math.IsInf(result, 0) && !math.IsNaN(result)
}
