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

type manifestEntry struct {
	Command    string     `json:"command"`
	Summary    string     `json:"summary"`
	Args       []string   `json:"args"`
	Flags      []flagInfo `json:"flags"`
	Mutating   bool       `json:"mutating"`
	Deprecated bool       `json:"deprecated"`
	Binary     bool       `json:"binary"`
	Paginated  bool       `json:"paginated"`
	Example    string     `json:"example,omitempty"`
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
					Command:    commandPath(op),
					Summary:    op.Summary,
					Args:       []string{},
					Flags:      []flagInfo{},
					Mutating:   op.isMutating(),
					Deprecated: op.Deprecated,
					Binary:     op.BinaryContentType != "",
					Paginated:  op.isPaginated(),
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
				manifest = append(manifest, e)
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetEscapeHTML(false)
			enc.SetIndent("", "  ")
			return enc.Encode(manifest)
		},
	}
}

func commandPath(op genOp) string {
	parts := append([]string{"factuarea"}, op.Groups...)
	parts = append(parts, op.Action)
	return strings.Join(parts, " ")
}
