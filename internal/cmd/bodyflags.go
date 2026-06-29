package cmd

import (
	"encoding/json"
	"io"
	"strconv"
	"strings"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/spf13/cobra"
)

type fieldFlag struct {
	flagName string
	jsonPath []string
	kind     string
	jsonType string
}

func (op genOp) typedBody() bool {
	return op.Body != nil && op.Body.Kind == "json" && len(op.Body.Fields) > 0
}

func (op genOp) isUpdate() bool {
	return op.Method == "PUT" || op.Method == "PATCH"
}

var reservedBuiltinFlagNames = map[string]struct{}{
	"data":             {},
	"data-file":        {},
	"dry-run":          {},
	"skeleton":         {},
	"paginate":         {},
	"confirm":          {},
	"output":           {},
	"skip-scope-check": {},
}

func fieldFlagName(path []string) string {
	parts := make([]string, len(path))
	for i, p := range path {
		parts[i] = strings.ReplaceAll(p, "_", "-")
	}
	name := strings.Join(parts, ".")
	if _, reserved := reservedBuiltinFlagNames[name]; reserved {
		return "body-" + name
	}
	return name
}

func collectFieldFlags(fields []genBodyField, parent []string) []fieldFlag {
	var flags []fieldFlag
	for _, f := range fields {
		path := append(append([]string{}, parent...), f.Name)
		switch f.Kind {
		case "scalar":
			flags = append(flags, fieldFlag{flagName: fieldFlagName(path), jsonPath: path, kind: "scalar", jsonType: f.Type})
		case "scalar_array":
			flags = append(flags, fieldFlag{flagName: fieldFlagName(path), jsonPath: path, kind: "scalar_array", jsonType: f.Type})
		case "map":
			flags = append(flags, fieldFlag{flagName: fieldFlagName(path), jsonPath: path, kind: "map"})
		case "object":
			if len(parent) == 0 {
				flags = append(flags, collectFieldFlags(f.Children, path)...)
			}
		}
	}
	return flags
}

func registerFieldFlags(c *cobra.Command, op genOp) {
	if !op.typedBody() {
		return
	}
	for _, ff := range collectFieldFlags(op.Body.Fields, nil) {
		desc := fieldFlagDescription(op, ff)
		switch ff.kind {
		case "scalar":
			switch ff.jsonType {
			case "integer":
				c.Flags().Int64(ff.flagName, 0, desc)
			case "number":
				c.Flags().Float64(ff.flagName, 0, desc)
			case "boolean":
				c.Flags().Bool(ff.flagName, false, desc)
			default:
				c.Flags().String(ff.flagName, "", desc)
			}
		case "scalar_array":
			c.Flags().StringSlice(ff.flagName, nil, desc)
		case "map":
			c.Flags().StringToString(ff.flagName, nil, desc)
		}
	}
	registerEnumCompletions(c, op)
}

func fieldFlagDescription(op genOp, ff fieldFlag) string {
	f := findField(op.Body.Fields, ff.jsonPath)
	if f == nil {
		return ""
	}
	parts := []string{}
	if f.Required {
		parts = append(parts, "(requerido)")
	}
	if len(f.Enum) > 0 {
		parts = append(parts, "valores: "+strings.Join(f.Enum, ", "))
	}
	switch ff.kind {
	case "scalar_array":
		parts = append(parts, "lista separada por comas")
	case "map":
		parts = append(parts, "pares clave=valor")
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func registerEnumCompletions(c *cobra.Command, op genOp) {
	for _, ff := range collectFieldFlags(op.Body.Fields, nil) {
		f := findField(op.Body.Fields, ff.jsonPath)
		if f == nil || len(f.Enum) == 0 {
			continue
		}
		enum := append([]string{}, f.Enum...)
		_ = c.RegisterFlagCompletionFunc(ff.flagName, func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			return enum, cobra.ShellCompDirectiveNoFileComp
		})
	}
}

type enumConstraint struct {
	flagName string
	values   []string
}

func enumConstraints(op genOp) []enumConstraint {
	var out []enumConstraint
	if op.typedBody() {
		for _, ff := range collectFieldFlags(op.Body.Fields, nil) {
			f := findField(op.Body.Fields, ff.jsonPath)
			if f == nil || len(f.Enum) == 0 || ff.kind != "scalar" {
				continue
			}
			out = append(out, enumConstraint{flagName: ff.flagName, values: append([]string{}, f.Enum...)})
		}
	}
	return out
}

func validateEnumFlags(cmd *cobra.Command, op genOp) error {
	for _, ec := range enumConstraints(op) {
		if !cmd.Flags().Changed(ec.flagName) {
			continue
		}
		v, err := cmd.Flags().GetString(ec.flagName)
		if err != nil || v == "" {
			continue
		}
		if !contains(ec.values, v) {
			return apierr.Usagef("--%s sólo acepta %s (recibido %q)", ec.flagName, strings.Join(ec.values, ", "), v)
		}
	}
	return nil
}

func contains(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}

func validateRequiredQueryFlags(cmd *cobra.Command, op genOp) error {
	var missing []string
	for _, p := range op.QueryParams {
		if !p.Required {
			continue
		}
		v, _ := cmd.Flags().GetString(p.Name)
		if !cmd.Flags().Changed(p.Name) || strings.TrimSpace(v) == "" {
			missing = append(missing, "--"+p.Name)
		}
	}
	if len(missing) > 0 {
		return apierr.Usagef("faltan flags requeridos: %s", strings.Join(missing, ", "))
	}
	return nil
}

func requiredBodyFlags(op genOp) []fieldFlag {
	if !op.typedBody() {
		return nil
	}
	var out []fieldFlag
	for _, ff := range collectFieldFlags(op.Body.Fields, nil) {
		if ff.kind != "scalar" && ff.kind != "scalar_array" {
			continue
		}
		f := findField(op.Body.Fields, ff.jsonPath)
		if f != nil && f.Required {
			out = append(out, ff)
		}
	}
	return out
}

func singlePositionalField(op genOp) (fieldFlag, bool) {
	if !op.typedBody() || op.isUpdate() || len(op.PathParams) > 0 {
		return fieldFlag{}, false
	}
	var found fieldFlag
	count := 0
	requiredScalars := 0
	for _, ff := range collectFieldFlags(op.Body.Fields, nil) {
		if ff.kind != "scalar" {
			return fieldFlag{}, false
		}
		count++
		f := findField(op.Body.Fields, ff.jsonPath)
		if f != nil && f.Required {
			requiredScalars++
			found = ff
		}
	}
	if count == 1 && requiredScalars == 1 {
		return found, true
	}
	return fieldFlag{}, false
}

func validateRequiredBodyFlags(cmd *cobra.Command, op genOp) error {
	var missing []string
	for _, ff := range requiredBodyFlags(op) {
		if !cmd.Flags().Changed(ff.flagName) || flagIsEmpty(cmd, ff) {
			missing = append(missing, "--"+ff.flagName)
		}
	}
	if len(missing) > 0 {
		return apierr.Usagef("faltan campos requeridos: %s (o usa -d/--data-file con el cuerpo completo)", strings.Join(missing, ", "))
	}
	return nil
}

func flagIsEmpty(cmd *cobra.Command, ff fieldFlag) bool {
	switch ff.kind {
	case "scalar_array":
		v, err := cmd.Flags().GetStringSlice(ff.flagName)
		return err == nil && len(v) == 0
	case "scalar":
		if ff.jsonType == "integer" || ff.jsonType == "number" || ff.jsonType == "boolean" {
			return false
		}
		v, err := cmd.Flags().GetString(ff.flagName)
		return err == nil && strings.TrimSpace(v) == ""
	}
	return false
}

func findField(fields []genBodyField, path []string) *genBodyField {
	if len(path) == 0 {
		return nil
	}
	for i := range fields {
		if fields[i].Name != path[0] {
			continue
		}
		if len(path) == 1 {
			return &fields[i]
		}
		return findField(fields[i].Children, path[1:])
	}
	return nil
}

func anyFieldFlagChanged(cmd *cobra.Command, op genOp) bool {
	for _, ff := range collectFieldFlags(op.Body.Fields, nil) {
		if cmd.Flags().Changed(ff.flagName) {
			return true
		}
	}
	return false
}

func bodyFromFieldFlags(cmd *cobra.Command, op genOp) (map[string]any, error) {
	root := map[string]any{}
	for _, ff := range collectFieldFlags(op.Body.Fields, nil) {
		if !cmd.Flags().Changed(ff.flagName) {
			continue
		}
		val, err := fieldFlagValue(cmd, ff)
		if err != nil {
			return nil, err
		}
		setPath(root, ff.jsonPath, val)
	}
	return root, nil
}

func fieldFlagValue(cmd *cobra.Command, ff fieldFlag) (any, error) {
	switch ff.kind {
	case "scalar":
		switch ff.jsonType {
		case "integer":
			return cmd.Flags().GetInt64(ff.flagName)
		case "number":
			return cmd.Flags().GetFloat64(ff.flagName)
		case "boolean":
			return cmd.Flags().GetBool(ff.flagName)
		default:
			return cmd.Flags().GetString(ff.flagName)
		}
	case "scalar_array":
		raw, err := cmd.Flags().GetStringSlice(ff.flagName)
		if err != nil {
			return nil, err
		}
		return castScalarSlice(raw, ff.jsonType), nil
	case "map":
		m, err := cmd.Flags().GetStringToString(ff.flagName)
		if err != nil {
			return nil, err
		}
		out := map[string]any{}
		for k, v := range m {
			out[k] = v
		}
		return out, nil
	}
	return nil, nil
}

func castScalarSlice(raw []string, jsonType string) any {
	switch jsonType {
	case "integer":
		out := make([]any, 0, len(raw))
		for _, v := range raw {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				out = append(out, n)
			} else {
				out = append(out, v)
			}
		}
		return out
	case "number":
		out := make([]any, 0, len(raw))
		for _, v := range raw {
			if n, err := strconv.ParseFloat(v, 64); err == nil {
				out = append(out, n)
			} else {
				out = append(out, v)
			}
		}
		return out
	case "boolean":
		out := make([]any, 0, len(raw))
		for _, v := range raw {
			if b, err := strconv.ParseBool(v); err == nil {
				out = append(out, b)
			} else {
				out = append(out, v)
			}
		}
		return out
	default:
		out := make([]any, 0, len(raw))
		for _, v := range raw {
			out = append(out, v)
		}
		return out
	}
}

func setPath(root map[string]any, path []string, val any) {
	cur := root
	for i, key := range path {
		if i == len(path)-1 {
			cur[key] = val
			return
		}
		next, ok := cur[key].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[key] = next
		}
		cur = next
	}
}

func readDataArg(data string, stdin io.Reader) ([]byte, error) {
	switch {
	case data == "-":
		return readInputStream(stdin, "el cuerpo desde stdin")
	case strings.HasPrefix(data, "@"):
		return readInputFile(strings.TrimPrefix(data, "@"))
	default:
		return []byte(data), nil
	}
}

func skeletonBody(op genOp) ([]byte, error) {
	obj := skeletonObject(op.Body.Fields)
	return marshalNoEscape(obj, true)
}

func marshalNoEscape(v any, indent bool) ([]byte, error) {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if indent {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return []byte(strings.TrimRight(b.String(), "\n")), nil
}

func skeletonObject(fields []genBodyField) map[string]any {
	ordered := append([]genBodyField{}, fields...)
	required := []genBodyField{}
	optional := []genBodyField{}
	for _, f := range ordered {
		if f.Required {
			required = append(required, f)
		} else {
			optional = append(optional, f)
		}
	}
	out := map[string]any{}
	for _, f := range append(required, optional...) {
		out[f.Name] = skeletonValue(f)
	}
	return out
}

func skeletonValue(f genBodyField) any {
	switch f.Kind {
	case "scalar":
		if len(f.Enum) > 0 {
			return "<" + strings.Join(f.Enum, "|") + ">"
		}
		return "<" + f.Type + ">"
	case "scalar_array":
		return []any{"<" + f.Type + ">"}
	case "object_array":
		return []any{}
	case "map":
		return map[string]any{"<key>": "<string>"}
	case "object":
		return skeletonObject(f.Children)
	}
	return nil
}

func bodyFieldsHelp(op genOp) string {
	if !op.typedBody() {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nCampos del cuerpo (flags tipados):")
	writeFieldsHelp(&b, op.Body.Fields, nil)
	if op.HasObjectArrayBody() {
		b.WriteString("\n\nEsta operación incluye una lista de objetos: pásala con --data-file/-d (JSON).")
	}
	if op.isUpdate() {
		b.WriteString("\n\nEdición parcial: solo se actualizan los campos que envíes; los omitidos se conservan. Para vaciar un campo, pásalo con valor vacío/null.")
	}
	return b.String()
}

func writeFieldsHelp(b *strings.Builder, fields []genBodyField, parent []string) {
	for _, f := range fields {
		path := append(append([]string{}, parent...), f.Name)
		switch f.Kind {
		case "scalar", "scalar_array", "map":
			b.WriteString("\n  --" + fieldFlagName(path))
			b.WriteString(" (" + fieldHelpType(f) + ")")
			if f.Required {
				b.WriteString(" requerido")
			}
			if len(f.Enum) > 0 {
				b.WriteString(" [" + strings.Join(f.Enum, ", ") + "]")
			}
		case "object":
			if len(parent) == 0 {
				writeFieldsHelp(b, f.Children, path)
			}
		}
	}
}

func fieldHelpType(f genBodyField) string {
	switch f.Kind {
	case "scalar_array":
		return f.Type + "[]"
	case "map":
		return "clave=valor"
	default:
		return f.Type
	}
}

func (op genOp) HasObjectArrayBody() bool {
	return op.Body != nil && op.Body.HasObjectArray
}

func compileTypedBody(cmd *cobra.Command, op genOp, data, dataFile string) ([]byte, error) {
	rawData := strings.TrimSpace(data) != "" || dataFile != ""
	flagsUsed := anyFieldFlagChanged(cmd, op)
	if rawData && flagsUsed {
		return nil, apierr.Usagef("no mezcles flags de campo con -d/--data-file: usa uno u otro")
	}
	if rawData {
		if dataFile != "" {
			return readInputFile(dataFile)
		}
		return readDataArg(data, cmd.InOrStdin())
	}
	obj, err := bodyFromFieldFlags(cmd, op)
	if err != nil {
		return nil, err
	}
	return marshalNoEscape(obj, false)
}
