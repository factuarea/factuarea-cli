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
	defer doc.Release()
	model, err := doc.BuildV3Model()
	if err != nil {
		return nil, nil, fmt.Errorf("build v3 model: %w", err)
	}
	// Operations contain detached values; release the parser's index after
	// extraction as well as the document to avoid retaining its YAML tree.
	defer model.Index.Release()
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
	o.IdempotencyRequired = idempotencyRequired(op)
	applyOverrides(&o)
	return o
}

// idempotencyRequired detecta el parámetro `in: header`, `name: Idempotency-Key`
// (sin distinguir mayúsculas) y `required: true`. Es solo informativo: el
// cliente (`internal/client/client.go`) ya autogenera la cabecera en todo
// verbo mutador (POST/PUT/PATCH/DELETE), un superconjunto seguro de lo que
// exige el spec — ver `design.md` D6 y el hallazgo de `anchors/contract.md`.
func idempotencyRequired(op *v3.Operation) bool {
	for _, p := range op.Parameters {
		if p == nil || p.In != "header" {
			continue
		}
		if !strings.EqualFold(p.Name, "Idempotency-Key") {
			continue
		}
		if p.Required != nil && *p.Required {
			return true
		}
	}
	return false
}

// Ya no hay lista de correcciones binarias a mano: el spec declara el
// Content-Type real de cada descarga (`OpenApiSchemaRegistry::BINARY_DOWNLOAD_PATHS`
// en el backend), así que `buildBinaryResponse` las detecta todas. Las seis
// entradas que hubo aquí —los PDF de presupuesto, proforma y borrador de
// factura, el recibo y el adjunto de la factura de compra, y la descarga de
// informe fiscal— parcheaban en el cliente un contrato que mentía en el origen;
// el arreglo correcto era el spec. Si vuelve a aparecer una descarga tratada
// como JSON, corrígela allí: aquí solo se volvería a esconder.

func applyOverrides(o *Operation) {
	if o.OperationID == "public-api.v1.series.default" && !hasQueryParam(o, "document_type") {
		o.QueryParams = append(o.QueryParams, Param{
			Name:        "document_type",
			In:          "query",
			Required:    true,
			Type:        "string",
			Description: "tipo de documento (invoice, quote, proforma, delivery_note)",
		})
	}
	if o.OperationID == "public-api.v1.quotes.convert" {
		narrowFieldEnum(o.Body, "target", []string{"invoice"})
	}
	if o.OperationID == "public-api.v1.taxes.by_type" {
		markQueryParamRequired(o, "type")
	}
}

func markQueryParamRequired(o *Operation, name string) {
	for i := range o.QueryParams {
		if o.QueryParams[i].Name == name {
			o.QueryParams[i].Required = true
			return
		}
	}
}

func narrowFieldEnum(body *Body, field string, enum []string) {
	if body == nil {
		return
	}
	for i := range body.Fields {
		if body.Fields[i].Name == field {
			body.Fields[i].Enum = enum
			return
		}
	}
}

func hasQueryParam(o *Operation, name string) bool {
	for _, p := range o.QueryParams {
		if p.Name == name {
			return true
		}
	}
	return false
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
		if b.Example == "" && mt.Examples != nil {
			if first := mt.Examples.First(); first != nil {
				if ex := first.Value(); ex != nil {
					b.Example = exampleJSON(ex.Value)
				}
			}
		}
		if mt.Schema != nil {
			b.Fields = bodyFields(mt.Schema.Schema(), 0)
		}
		return b
	}

	if mt, ok := content.Get("multipart/form-data"); ok && mt != nil && mt.Schema != nil {
		b := &Body{Kind: "multipart"}
		if sc := mt.Schema.Schema(); sc != nil {
			b.FileFields = binaryFields(sc, false)
			b.FileArrayFields = binaryFields(sc, true)
		}
		return b
	}
	return nil
}

func binaryFields(sc *base.Schema, multiple bool) []string {
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
			s := ps.Schema()
			if s != nil && multiple {
				s = arrayItemSchema(s)
			}
			if s != nil && s.Format == "binary" {
				// El backend nombra la propiedad multipart de array con el
				// sufijo `[]` ya incluido en la clave (p. ej. `files[]`, el
				// nombre de la PARTE HTTP real, ver `anchors/contract.md`
				// §1.4). Aquí se guarda el campo pelado: quien construye la
				// petición (`genop_helpers.go`) es quien vuelve a añadir el
				// sufijo al formar el nombre de la parte.
				fields = append(fields, strings.TrimSuffix(prop.Key(), "[]"))
			}
		}
	}
	for _, sub := range sc.AllOf {
		if sub == nil {
			continue
		}
		fields = append(fields, binaryFields(sub.Schema(), multiple)...)
	}
	return fields
}

func bodyFields(sc *base.Schema, depth int) []BodyField {
	if sc == nil {
		return nil
	}
	props := mergedProperties(sc)
	if props == nil {
		return nil
	}
	required := requiredSet(sc)
	var fields []BodyField
	for prop := props.First(); prop != nil; prop = prop.Next() {
		ps := prop.Value()
		if ps == nil {
			continue
		}
		s := fieldSchema(ps)
		if s == nil {
			continue
		}
		f := BodyField{Name: prop.Key(), Required: required[prop.Key()]}
		classifyField(&f, s, depth)
		fields = append(fields, f)
	}
	return fields
}

// fieldSchema devuelve el esquema que clasifica un campo del cuerpo. Un campo
// OpenAPI 3.1 que es un `$ref` con palabras clave hermanas (por ejemplo
// `remission_mode`: `description` más `$ref` a un enum) lo entrega libopenapi
// como la vista de solo los hermanos, sin tipo ni enum. En ese caso se clasifica
// por el esquema referido.
func fieldSchema(ps *base.SchemaProxy) *base.Schema {
	s := ps.Schema()
	if s == nil || !ps.IsTransformedRefWithSiblings() {
		return s
	}
	semantic, err := ps.BuildTransformedRefSemanticSchema(s)
	if err != nil || semantic == nil || len(semantic.AllOf) != 2 {
		return s
	}
	if target := semantic.AllOf[1].Schema(); target != nil {
		return target
	}
	return s
}

func classifyField(f *BodyField, s *base.Schema, depth int) {
	jsonType, nullable, union := fieldType(s)
	if s.Nullable != nil && *s.Nullable {
		nullable = true
	}
	f.Nullable = nullable
	f.Enum = enumValues(s.Enum)

	if union {
		// Un tipo unión (`string|number|boolean|array|null`) no es un array ni un
		// objeto aunque uno de sus miembros lo sea: es un escalar de tipo libre.
		f.Kind = "scalar"
		f.Type = jsonType
		return
	}

	switch jsonType {
	case "array":
		item := arrayItemSchema(s)
		itemType, _ := scalarType(itemSchemaType(item))
		if itemType == "object" {
			f.Kind = "object_array"
		} else {
			f.Kind = "scalar_array"
			f.Type = itemType
		}
	case "object":
		if isFreeMap(s) {
			f.Kind = "map"
		} else if depth < 1 {
			f.Kind = "object"
			f.Children = bodyFields(s, depth+1)
		} else {
			f.Kind = "object"
		}
	default:
		f.Kind = "scalar"
		f.Type = jsonType
	}
}

func mergedProperties(sc *base.Schema) *orderedmap.Map[string, *base.SchemaProxy] {
	if sc == nil {
		return nil
	}
	if sc.Properties != nil && sc.Properties.Len() > 0 {
		return sc.Properties
	}
	for _, sub := range sc.AllOf {
		if sub == nil {
			continue
		}
		if p := mergedProperties(sub.Schema()); p != nil {
			return p
		}
	}
	return nil
}

func requiredSet(sc *base.Schema) map[string]bool {
	set := map[string]bool{}
	if sc == nil {
		return set
	}
	for _, r := range sc.Required {
		set[r] = true
	}
	for _, sub := range sc.AllOf {
		if sub == nil {
			continue
		}
		for k := range requiredSet(sub.Schema()) {
			set[k] = true
		}
	}
	return set
}

// unionSeparator une los tipos de un campo de tipo unión: `string|number|null`.
const unionSeparator = "|"

// maxUnionDepth acota la recursión por `anyOf`/`oneOf` anidados.
const maxUnionDepth = 4

// fieldType devuelve el tipo de un campo de cuerpo. Un campo de un solo tipo
// (`type: string`, `type: [string, null]`, `anyOf: [string, null]`) devuelve ese
// tipo y su nulabilidad, con `union` a false. Un campo con VARIOS tipos distintos
// —`type: [string, number]` o `anyOf`/`oneOf` de miembros de tipos distintos—
// devuelve la unión de sus tipos en orden de declaración, con `null` al final si
// alguno lo admite (`string|number|boolean|array|null`), y `union` a true.
func fieldType(s *base.Schema) (jsonType string, nullable, union bool) {
	names := schemaTypeNames(s, 0)
	var concrete []string
	for _, n := range names {
		if n == "null" {
			nullable = true
			continue
		}
		concrete = append(concrete, n)
	}
	switch len(concrete) {
	case 0:
		return "", nullable, false
	case 1:
		return concrete[0], nullable, false
	}
	if nullable {
		concrete = append(concrete, "null")
	}
	return strings.Join(concrete, unionSeparator), nullable, true
}

// schemaTypeNames recoge, sin repetir y en orden de declaración, los tipos que
// declara el esquema: su `type` y los de sus miembros `anyOf`/`oneOf`.
func schemaTypeNames(s *base.Schema, depth int) []string {
	if s == nil || depth > maxUnionDepth {
		return nil
	}
	var names []string
	seen := map[string]bool{}
	add := func(list []string) {
		for _, n := range list {
			if n != "" && !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
	}
	add(s.Type)
	for _, members := range [][]*base.SchemaProxy{s.AnyOf, s.OneOf} {
		for _, m := range members {
			if m == nil {
				continue
			}
			add(schemaTypeNames(m.Schema(), depth+1))
		}
	}
	return names
}

func scalarType(types []string) (jsonType string, nullable bool) {
	for _, t := range types {
		if t == "null" {
			nullable = true
			continue
		}
		if jsonType == "" {
			jsonType = t
		}
	}
	return jsonType, nullable
}

// enumValues aplana los valores de un `enum` a texto para el flag. El `null`
// que el backend añade al enum de todo campo nullable (`enum: [a, b, null]`) NO
// es un valor que el usuario pueda teclear: se omite en vez de convertirse en
// una cadena vacía que aparecía como opción fantasma en la ayuda y en la
// validación del flag.
func enumValues(nodes []*yaml.Node) []string {
	if len(nodes) == 0 {
		return nil
	}
	var vals []string
	for _, n := range nodes {
		if n == nil || n.ShortTag() == "!!null" {
			continue
		}
		var s string
		if err := n.Decode(&s); err == nil {
			vals = append(vals, s)
		}
	}
	return vals
}

func arrayItemSchema(s *base.Schema) *base.Schema {
	if s == nil || s.Items == nil || !s.Items.IsA() {
		return nil
	}
	proxy := s.Items.A
	if proxy == nil {
		return nil
	}
	return proxy.Schema()
}

func itemSchemaType(item *base.Schema) []string {
	if item == nil {
		return nil
	}
	return item.Type
}

func isFreeMap(s *base.Schema) bool {
	if s == nil || s.AdditionalProperties == nil {
		return false
	}
	if s.AdditionalProperties.IsB() {
		return s.AdditionalProperties.B
	}
	return s.AdditionalProperties.A != nil
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
