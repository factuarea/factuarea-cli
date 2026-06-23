package cmd

import (
	"context"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/spf13/cobra"
)

type GlobalFlags struct {
	JSON                   bool
	NoColor                bool
	NoInput                bool
	Profile                string
	Live                   bool
	Verbose                bool
	Quiet                  bool
	AllowInsecureTransport bool
}

func NewRootCmd() *cobra.Command {
	g := &GlobalFlags{}
	root := &cobra.Command{
		Use:                        "factuarea",
		Short:                      "CLI oficial de Factuarea — maneja la API pública v1 desde la terminal",
		SilenceUsage:               true,
		SilenceErrors:              true,
		SuggestionsMinimumDistance: 2,
		Args:                       groupArgs,
		RunE:                       func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return apierr.Usagef("%s", translateCobraError(err.Error()))
	})
	pf := root.PersistentFlags()
	pf.BoolVar(&g.JSON, "json", false, "salida JSON cruda (para scripts/agentes)")
	pf.BoolVar(&g.NoColor, "no-color", false, "desactiva el color")
	pf.BoolVar(&g.NoInput, "no-input", false, "no preguntar nada de forma interactiva")
	pf.StringVar(&g.Profile, "profile", "", "perfil de configuración a usar")
	pf.BoolVar(&g.Live, "live", false, "permite operaciones mutadoras en entorno LIVE")
	pf.BoolVarP(&g.Verbose, "verbose", "v", false, "salida detallada")
	pf.BoolVarP(&g.Quiet, "quiet", "q", false, "silencia mensajes informativos")
	pf.BoolVar(&g.AllowInsecureTransport, "allow-insecure-transport", false, "permite enviar la API key sobre http:// a hosts no-loopback (inseguro)")

	root.PersistentPreRun = func(cmd *cobra.Command, _ []string) {
		ctx := context.WithValue(cmd.Context(), globalsKey{}, g)
		cmd.SetContext(ctx)
	}

	root.AddCommand(
		newVersionCmd(),
		newLoginCmd(),
		newLogoutCmd(),
		newWhoamiCmd(),
		newAPICmd(),
		newListenCmd(),
		newTriggerCmd(),
		newCommandsCmd(),
		newDocsCmd(),
	)
	registerGeneratedCommands(root)
	return root
}
