package spec

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
)

func isKnowledgeOperation(native string) bool {
	return strings.HasPrefix(native, "knowledge_articles.") || strings.HasPrefix(native, "public_help_centers.")
}

func resolveKnowledgeCRM(op *v3.Operation, method, path, family, suffix string) ([]string, string, bool) {
	nativePath := strings.TrimPrefix(path, "/v1")
	scope := stringExt(op.Extensions, "x-required-scope")
	if scope != family+":read" && scope != family+":write" {
		return nil, "", false
	}
	if family == "public_help_centers" && (nativePath == "/crm/public-help-center" || strings.HasPrefix(nativePath, "/crm/public-help-center/")) {
		if action := map[string]string{"administration_get": "show", "publish": "publish", "unpublish": "unpublish", "publish_receipt": "publish-receipt", "unpublish_receipt": "unpublish-receipt"}[suffix]; action != "" {
			return []string{"crm", "public-help-center"}, action, true
		}
	}
	if family == "knowledge_articles" && strings.HasPrefix(nativePath, "/crm/knowledge/") {
		if action := map[string]string{"categories": "list", "category_save": "save", "category_receipt": "receipt"}[suffix]; action != "" {
			return []string{"crm", "knowledge-categories"}, action, true
		}
		switch suffix {
		case "search", "create", "show", "save", "versions", "suggest", "submit", "approve", "publish", "unpublish", "archive", "link", "receipt_create", "receipt_save", "receipt_submit", "receipt_approve", "receipt_publish", "receipt_unpublish", "receipt_archive", "receipt_link":
			return []string{"crm", "knowledge-articles"}, ToKebab(suffix), true
		}
	}
	return nil, "", false
}

// Resolve only the new native boundary schemas. Existing operations and their
// body classification remain unchanged. Nullable anyOf scalars retain their
// real scalar flag type; explicit null is supplied through the JSON body.
func knowledgeBodyFields(sc *base.Schema, depth int) []BodyField {
	fields := bodyFields(sc, depth)
	props := mergedProperties(sc)
	if props == nil {
		return fields
	}
	for i := range fields {
		proxy, ok := props.Get(fields[i].Name)
		if !ok || proxy == nil {
			continue
		}
		s := proxy.Schema()
		if s == nil {
			continue
		}
		nullableField := fields[i].Nullable
		for _, variant := range append(append([]*base.SchemaProxy{}, s.AnyOf...), s.OneOf...) {
			if variant == nil || variant.Schema() == nil {
				continue
			}
			typ, nullable := scalarType(variant.Schema().Type)
			nullableField = nullableField || nullable
			if typ != "" {
				classifyField(&fields[i], variant.Schema(), depth)
			}
		}
		fields[i].Nullable = nullableField
		if fields[i].Kind == "object" {
			fields[i].Children = knowledgeBodyFields(s, depth+1)
		}
	}
	return fields
}

func knowledgeContract(wire map[string]any, path, method string) (*NativeContract, []string, error) {
	paths, _ := wire["paths"].(map[string]any)
	item, _ := paths[path].(map[string]any)
	op, _ := item[strings.ToLower(method)].(map[string]any)
	contract := &NativeContract{ResponseSchemas: map[string]string{}, QuerySchemas: map[string]string{}}
	encode := func(value any) (string, error) {
		resolved, err := resolveNativeSchema(wire, value, 0)
		if err != nil {
			return "", err
		}
		body, err := json.Marshal(resolved)
		return string(body), err
	}
	if request, ok := op["requestBody"].(map[string]any); ok {
		description, _ := request["description"].(string)
		if match := regexp.MustCompile(`(?:maximum |body limit is )(\d+) bytes`).FindStringSubmatch(description); len(match) == 2 {
			contract.MaxBodyBytes, _ = strconv.Atoi(match[1])
		}
		content, _ := request["content"].(map[string]any)
		media, _ := content["application/json"].(map[string]any)
		var err error
		contract.RequestSchema, err = encode(media["schema"])
		if err != nil {
			return nil, nil, err
		}
	}
	responses, _ := op["responses"].(map[string]any)
	for code, value := range responses {
		if !strings.HasPrefix(code, "2") {
			continue
		}
		response, _ := value.(map[string]any)
		content, _ := response["content"].(map[string]any)
		media, _ := content["application/json"].(map[string]any)
		schema, err := encode(media["schema"])
		if err != nil {
			return nil, nil, err
		}
		contract.ResponseSchemas[code] = schema
	}
	parameters, _ := op["parameters"].([]any)
	for _, value := range parameters {
		p, _ := value.(map[string]any)
		if p["in"] != "query" {
			continue
		}
		name, _ := p["name"].(string)
		schema, err := encode(p["schema"])
		if err != nil {
			return nil, nil, err
		}
		contract.QuerySchemas[name] = schema
	}
	var scopes []string
	values, _ := op["x-required-scopes"].([]any)
	for _, value := range values {
		if scope, ok := value.(string); ok {
			scopes = append(scopes, scope)
		}
	}
	return contract, scopes, nil
}

func resolveNativeSchema(wire map[string]any, value any, depth int) (any, error) {
	if depth > 40 {
		return nil, fmt.Errorf("native schema reference depth exceeded")
	}
	switch node := value.(type) {
	case map[string]any:
		if ref, ok := node["$ref"].(string); ok {
			if !strings.HasPrefix(ref, "#/") {
				return nil, fmt.Errorf("external native schema reference %s", ref)
			}
			var resolved any = wire
			for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
				object, _ := resolved.(map[string]any)
				resolved = object[strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")]
			}
			if resolved == nil {
				return nil, fmt.Errorf("missing native schema reference %s", ref)
			}
			return resolveNativeSchema(wire, resolved, depth+1)
		}
		result := map[string]any{}
		for key, item := range node {
			resolved, err := resolveNativeSchema(wire, item, depth+1)
			if err != nil {
				return nil, err
			}
			result[key] = resolved
		}
		return result, nil
	case []any:
		result := make([]any, len(node))
		for i, item := range node {
			resolved, err := resolveNativeSchema(wire, item, depth+1)
			if err != nil {
				return nil, err
			}
			result[i] = resolved
		}
		return result, nil
	default:
		return value, nil
	}
}
