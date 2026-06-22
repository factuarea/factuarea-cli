package cmd

import "github.com/spf13/cobra"

// GlobalFlags contiene los flags persistentes compartidos por todos los comandos.
type GlobalFlags struct {
	JSON    bool
	Plain   bool
	NoColor bool
	NoInput bool
	Profile string
	Live    bool
	Verbose bool
	Quiet   bool
}

func NewRootCmd() *cobra.Command {
	g := &GlobalFlags{}
	root := &cobra.Command{
		Use:           "factuarea",
		Short:         "CLI oficial de Factuarea — maneja la API pública v1 desde la terminal",
		SilenceUsage:  true, // los errores se imprimen una vez, sin volcar el usage entero
		SilenceErrors: true, // el control de exit code lo lleva main.go
	}
	pf := root.PersistentFlags()
	pf.BoolVar(&g.JSON, "json", false, "salida JSON cruda (para scripts/agentes)")
	pf.BoolVar(&g.Plain, "plain", false, "salida en texto plano sin formato")
	pf.BoolVar(&g.NoColor, "no-color", false, "desactiva el color")
	pf.BoolVar(&g.NoInput, "no-input", false, "no preguntar nada de forma interactiva")
	pf.StringVar(&g.Profile, "profile", "", "perfil de configuración a usar")
	pf.BoolVar(&g.Live, "live", false, "permite operaciones mutadoras en entorno LIVE")
	pf.BoolVarP(&g.Verbose, "verbose", "v", false, "salida detallada")
	pf.BoolVarP(&g.Quiet, "quiet", "q", false, "silencia mensajes informativos")

	root.AddCommand(newVersionCmd())
	return root
}
