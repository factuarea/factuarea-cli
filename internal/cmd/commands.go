package cmd

import (
	"encoding/json"
	"strings"

	"github.com/spf13/cobra"
)

type flagInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type manifestField struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Kind     string   `json:"kind"`
	Required bool     `json:"required"`
	Enum     []string `json:"enum,omitempty"`
}

type manifestEntry struct {
	Command        string          `json:"command"`
	Summary        string          `json:"summary"`
	Args           []string        `json:"args"`
	Flags          []flagInfo      `json:"flags"`
	Mutating       bool            `json:"mutating"`
	Deprecated     bool            `json:"deprecated"`
	Binary         bool            `json:"binary"`
	Paginated      bool            `json:"paginated"`
	Irreversible   bool            `json:"irreversible"`
	RequiredScope  string          `json:"required_scope,omitempty"`
	Example        string          `json:"example,omitempty"`
	BodyFields     []manifestField `json:"body_fields,omitempty"`
	BodyHasObjects bool            `json:"body_has_object_array,omitempty"`
	FullReplace    bool            `json:"full_replace,omitempty"`
}

func newCommandsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "commands",
		Short: "Vuelca el manifiesto de todos los comandos (discovery para agentes)",
		Args:  UsageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ops := generatedOps()
			manifest := make([]manifestEntry, 0, len(ops))
			for _, op := range ops {
				e := manifestEntry{
					Command:       commandPath(op),
					Summary:       op.Summary,
					Args:          []string{},
					Flags:         []flagInfo{},
					Mutating:      op.isMutating(),
					Deprecated:    op.Deprecated,
					Binary:        op.BinaryContentType != "",
					Paginated:     op.isPaginated(),
					Irreversible:  op.Irreversible,
					RequiredScope: op.RequiredScope,
				}
				for _, p := range op.PathParams {
					e.Args = append(e.Args, p.Name)
				}
				for _, p := range op.QueryParams {
					e.Flags = append(e.Flags, flagInfo{Name: p.Name, Type: p.Type})
				}
				if op.Body != nil {
					e.Example = op.Body.Example
				}
				if op.typedBody() {
					e.BodyFields = manifestFields(op.Body.Fields, nil)
					e.BodyHasObjects = op.Body.HasObjectArray
					e.FullReplace = op.isUpdate()
				}
				manifest = append(manifest, e)
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetEscapeHTML(false)
			enc.SetIndent("", "  ")
			return enc.Encode(manifest)
		},
	}
}

func manifestFields(fields []genBodyField, parent []string) []manifestField {
	var out []manifestField
	for _, f := range fields {
		path := append(append([]string{}, parent...), f.Name)
		switch f.Kind {
		case "scalar", "scalar_array", "map":
			out = append(out, manifestField{
				Name:     fieldFlagName(path),
				Type:     fieldHelpType(f),
				Kind:     f.Kind,
				Required: f.Required,
				Enum:     f.Enum,
			})
		case "object":
			if len(parent) == 0 {
				out = append(out, manifestFields(f.Children, path)...)
			}
		}
	}
	return out
}

func commandPath(op genOp) string {
	parts := append([]string{"factuarea"}, op.Groups...)
	parts = append(parts, op.Action)
	return strings.Join(parts, " ")
}
