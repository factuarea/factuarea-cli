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
	// Company es el eje de empresa del contrato v1. Es ADITIVO: sólo lo
	// consumen las operaciones cuyo path declara el parámetro `company`, y
	// cuando está vacío el binario se comporta exactamente como antes (el
	// valor se sigue pasando como argumento posicional).
	Company string
	// Account es el eje de cuenta del contrato v1: rellena el parámetro de ruta
	// {account} de las operaciones del eje de cuenta. Solo se deduce solo en las
	// lecturas; en el grupo `managed-accounts` no aplica.
	Account string
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
	// Sin enlace a entorno ni a `config.toml`: ese eslabón no existe en esta CLI.
	// Cuando la flag no se pasa, la cadena de `company.go` resuelve el valor
	// desde el ámbito de la credencial si alcanza un solo NIF.
	pf.StringVar(&g.Company, "company", "", "empresa sobre la que operar; rellena el parámetro de ruta {company} y hace opcional su argumento posicional. Si tu credencial alcanza una sola empresa, se deduce sola")

	pf.StringVar(&g.Account, "account", "", "cuenta sobre la que operar; rellena el parámetro de ruta {account} y hace opcional su argumento posicional. Solo se deduce sola en las lecturas (de la identidad de tu credencial); en las escrituras hay que nombrarla. No aplica a las cuentas gestionadas (managed-accounts), donde la cuenta es el recurso y se escribe siempre")

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
