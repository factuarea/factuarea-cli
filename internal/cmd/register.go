package cmd

import "github.com/spf13/cobra"

// overrides: comandos generados que se reemplazan por una versión a mano,
// keyed por operationId. VACÍO en Plan 2 (punto de extensión): cuando exista
// un override, el comando generado correspondiente NO se construye.
var overrides = map[string]func() *cobra.Command{}

// registerGeneratedCommands construye el árbol de recursos desde generatedOps()
// y lo cuelga del root. Para cada operación: si hay override por operationId,
// usa el override (exclusión en origen, el generado no se construye); si no, usa
// el generado. Los comandos-grupo intermedios se crean on-demand y se cachean
// por su ruta completa (p.ej. "verifactu records") para no recrearlos.
func registerGeneratedCommands(root *cobra.Command) {
	groups := map[string]*cobra.Command{} // clave = ruta de grupo unida por espacio
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
				// RunE (mostrar ayuda) hace el grupo "runnable" para que Cobra
				// VALIDE Args; sin RunE, Cobra ignora Args y muestra ayuda con
				// exit 0. Con Args NoArgs (envuelto en UsageError → exit 2), un
				// subcomando inexistente (p.ej. `invoices get`) falla con "unknown
				// command"; el grupo solo (`invoices`, 0 args) muestra su ayuda.
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
			leaf = mk() // override gana; el generado no se construye
		} else {
			leaf = buildGeneratedCommand(op)
		}
		groupFor(op.Groups).AddCommand(leaf)
	}
}
