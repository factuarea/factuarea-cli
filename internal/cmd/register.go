package cmd

import "github.com/spf13/cobra"

var overrides = map[string]func() *cobra.Command{}

func registerGeneratedCommands(root *cobra.Command) {
	groups := map[string]*cobra.Command{}
	groupFor := func(path []string) *cobra.Command {
		parent := root
		key := ""
		for _, seg := range path {
			if key != "" {
				key += " "
			}
			key += seg
			g, ok := groups[key]
			if !ok {
				g = &cobra.Command{
					Use:   seg,
					Short: "Comandos de " + seg,
					Args:  UsageArgs(cobra.NoArgs),
					RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
				}
				parent.AddCommand(g)
				groups[key] = g
			}
			parent = g
		}
		return parent
	}

	for _, op := range generatedOps() {
		var leaf *cobra.Command
		if mk, ok := overrides[op.OperationID]; ok {
			leaf = mk()
		} else {
			leaf = buildGeneratedCommand(op)
		}
		groupFor(op.Groups).AddCommand(leaf)
	}
}
