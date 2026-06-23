package spec

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

func Load() (ops []Operation, nonConforming []string, err error) {
	doc, err := libopenapi.NewDocument(Raw)
	if err != nil {
		return nil, nil, fmt.Errorf("parse spec: %w", err)
	}
	model, err := doc.BuildV3Model()
	if err != nil {
		return nil, nil, fmt.Errorf("build v3 model: %w", err)
	}
	if model.Model.Paths == nil || model.Model.Paths.PathItems == nil {
		return nil, nil, fmt.Errorf("spec sin paths")
	}

	for pair := model.Model.Paths.PathItems.First(); pair != nil; pair = pair.Next() {
		path := pair.Key()
		item := pair.Value()
		for method, op := range methodsOf(item) {
			if op == nil || op.OperationId == "" {
				continue
			}
			groups, action, ok := Resolve(op.OperationId)
			if !ok {
				nonConforming = append(nonConforming, op.OperationId)
				continue
			}
			ops = append(ops, buildOperation(op, method, path, groups, action))
		}
	}

	sort.Slice(ops, func(i, j int) bool {
		if ops[i].Path != ops[j].Path {
			return ops[i].Path < ops[j].Path
		}
		return ops[i].Method < ops[j].Method
	})
	sort.Strings(nonConforming)
	return ops, nonConforming, nil
}

func methodsOf(item *v3.PathItem) map[string]*v3.Operation {
	m := map[string]*v3.Operation{}
	if item.Get != nil {
		m["GET"] = item.Get
	}
	if item.Post != nil {
		m["POST"] = item.Post
	}
	if item.Put != nil {
		m["PUT"] = item.Put
	}
	if item.Patch != nil {
		m["PATCH"] = item.Patch
	}
	if item.Delete != nil {
		m["DELETE"] = item.Delete
	}
	return m
}

func buildOperation(op *v3.Operation, method, path string, groups []string, action string) Operation {
	o := Operation{
		OperationID: op.OperationId,
		Method:      method,
		Path:        path,
		Groups:      groups,
		Action:      action,
		Summary:     op.Summary,
		Deprecated:  op.Deprecated != nil && *op.Deprecated,
	}
	for _, p := range op.Parameters {
		if p == nil {
			continue
		}
		param := Param{Name: p.Name, In: p.In, Description: p.Description}
		if p.Required != nil {
			param.Required = *p.Required
		}
		if p.Schema != nil {
			if sc := p.Schema.Schema(); sc != nil && len(sc.Type) > 0 {
				param.Type = sc.Type[0]
			}
		}
		switch p.In {
		case "path":
			o.PathParams = append(o.PathParams, param)
		case "query":
			o.QueryParams = append(o.QueryParams, param)
		}
	}
	o.Body = buildBody(op)
	o.BinaryResponse = buildBinaryResponse(op)
	o.RequiredScope = stringExt(op.Extensions, "x-required-scope")
	o.Irreversible = boolExt(op.Extensions, "x-irreversible")
	return o
}

func stringExt(ext *orderedmap.Map[string, *yaml.Node], key string) string {
	if ext == nil {
		return ""
	}
	node, ok := ext.Get(key)
	if !ok || node == nil {
		return ""
	}
	var s string
	if err := node.Decode(&s); err != nil {
		return ""
	}
	return s
}

func boolExt(ext *orderedmap.Map[string, *yaml.Node], key string) bool {
	if ext == nil {
		return false
	}
	node, ok := ext.Get(key)
	if !ok || node == nil {
		return false
	}
	var b bool
	if err := node.Decode(&b); err != nil {
		return false
	}
	return b
}

func buildBody(op *v3.Operation) *Body {
	if op.RequestBody == nil || op.RequestBody.Content == nil {
		return nil
	}
	content := op.RequestBody.Content

	if mt, ok := content.Get("application/json"); ok && mt != nil {
		b := &Body{Kind: "json"}
		b.Example = exampleJSON(mt.Example)
		return b
	}

	if mt, ok := content.Get("multipart/form-data"); ok && mt != nil && mt.Schema != nil {
		b := &Body{Kind: "multipart"}
		if sc := mt.Schema.Schema(); sc != nil {
			b.FileFields = binaryFields(sc)
		}
		return b
	}
	return nil
}

func binaryFields(sc *base.Schema) []string {
	if sc == nil {
		return nil
	}
	var fields []string
	if sc.Properties != nil {
		for prop := sc.Properties.First(); prop != nil; prop = prop.Next() {
			ps := prop.Value()
			if ps == nil {
				continue
			}
			if s := ps.Schema(); s != nil && s.Format == "binary" {
				fields = append(fields, prop.Key())
			}
		}
	}
	for _, sub := range sc.AllOf {
		if sub == nil {
			continue
		}
		fields = append(fields, binaryFields(sub.Schema())...)
	}
	return fields
}

func buildBinaryResponse(op *v3.Operation) *BinaryResponse {
	if op.Responses == nil {
		return nil
	}
	resp := op.Responses.FindResponseByCode(200)
	if resp == nil || resp.Content == nil {
		return nil
	}
	var firstNonJSON string
	for ct := resp.Content.First(); ct != nil; ct = ct.Next() {
		mediaType := ct.Key()
		if strings.Contains(mediaType, "json") {
			return nil
		}
		if firstNonJSON == "" {
			firstNonJSON = mediaType
		}
	}
	if firstNonJSON == "" {
		return nil
	}
	return &BinaryResponse{ContentType: firstNonJSON}
}

func exampleJSON(node *yaml.Node) string {
	if node == nil {
		return ""
	}
	var v any
	if err := node.Decode(&v); err != nil {
		return ""
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(raw)
}
