package cmd

import (
	"context"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	crmmeta "github.com/factuarea/factuarea-cli/internal/crmmeta_native_batch80"
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
	registerCRMCoreNativeCommands(root)
	registerServiceLevelCommands(root)
	registerCRMDocumentCommands(root)
	registerCRMServiceTicketCommands(root)
	registerCRMOAuthOriginalCommands(root)
	registerCRMWhatsAppCommands(root)
	registerCRMMetaNativeBatch80(root)
	return root
}

func registerCRMMetaNativeBatch80(root *cobra.Command) {
	if err := crmmeta.Register(root, resolveCRMMetaNativeBatch80); err != nil {
		panic("duplicate native CRM Meta command registration")
	}
	group, _, err := root.Find([]string{"crm", "meta"})
	if err != nil {
		panic("native CRM Meta command registration unavailable")
	}
	group.PersistentFlags().String("oauth-token-env", "", "nombre de variable con bearer OAuth nativo actual; vacío conserva API key/perfil real")
	group.PersistentFlags().String("api-version", "", "Factuarea-Version (YYYY-MM-DD)")
}

// Actual profile/API-key and actual issuer bearer remain distinct credential sources.
func resolveCRMMetaNativeBatch80(command *cobra.Command) (crmmeta.Config, error) {
	g := globalsFrom(command)
	tokenEnv, _ := command.Flags().GetString("oauth-token-env")
	version, _ := command.Flags().GetString("api-version")
	if err := validateCRMDocumentHeaders(version, ""); err != nil {
		return crmmeta.Config{}, err
	}
	base := os.Getenv(envBaseURL)
	if base == "" {
		base = "https://api.factuarea.com"
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return crmmeta.Config{}, apierr.Usagef("FACTUAREA_BASE_URL debe ser el servidor original sin credenciales, query ni fragmento")
	}
	if err := validateBaseURLTransport(base, g.AllowInsecureTransport); err != nil {
		return crmmeta.Config{}, err
	}
	config := crmmeta.Config{BaseURL: strings.TrimRight(base, "/") + "/v1", Version: version}
	if tokenEnv != "" {
		if g.Profile != "" || !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(tokenEnv) {
			return crmmeta.Config{}, apierr.Usagef("OAuth exige oauth-token-env real sin perfil de API key")
		}
		config.OAuthBearer = os.Getenv(tokenEnv)
		if config.OAuthBearer == "" || !regexp.MustCompile(`^[\x21-\x7e]+$`).MatchString(config.OAuthBearer) || strings.HasPrefix(config.OAuthBearer, "fact_test_") || strings.HasPrefix(config.OAuthBearer, "fact_live_") {
			return crmmeta.Config{}, apierr.Usagef("la variable debe contener un access token OAuth nativo actual")
		}
	} else {
		current, err := newCLIContext(g, "")
		if err != nil {
			return crmmeta.Config{}, err
		}
		config.APIKey = current.res.APIKey
		if strings.HasPrefix(config.APIKey, "fact_live_") && (command.Name() == "oauth-prepare" || command.Name() == "oauth-complete" || command.Name() == "publish-mapping" || command.Name() == "publish-consent") && !g.Live {
			return crmmeta.Config{}, apierr.Usagef("la configuración humana con API key LIVE requiere --live")
		}
	}
	if g.JSON && !command.Flags().Changed("output") {
		if err := command.Flags().Set("output", "json"); err != nil {
			return crmmeta.Config{}, err
		}
	}
	return config, nil
}
