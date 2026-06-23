package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/client"
	"github.com/factuarea/factuarea-cli/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newLoginCmd() *cobra.Command {
	var apiKeySource string
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Guarda tu API key (fact_test_… o fact_live_…)",
		Long:  "Lee la API key por prompt oculto, por stdin (--api-key -) o por la env FACTUAREA_API_KEY.\nNUNCA la pases como valor literal de flag.",
		Args:  rejectLoginPositional,
		RunE: func(cmd *cobra.Command, _ []string) error {
			g := globalsFrom(cmd)
			if cmd.Flags().Changed("api-key") && apiKeySource != "-" {
				return apierr.Usagef("--api-key solo acepta '-' (lee la key por stdin); usa el prompt o define FACTUAREA_API_KEY. Nunca pases la key como valor literal")
			}
			fromStdin := cmd.Flags().Changed("api-key") && apiKeySource == "-"
			key, err := readKey(cmd, fromStdin, g.NoInput)
			if err != nil {
				return err
			}
			key = strings.TrimSpace(key)
			if !config.ValidKeyFormat(key) {
				return apierr.Usagef("formato de key inválido (se espera fact_test_… o fact_live_… seguido de 24 caracteres)")
			}
			profile := g.Profile
			if profile == "" {
				profile = "default"
			}
			opts, err := baseURLClientOptions(g.AllowInsecureTransport)
			if err != nil {
				return err
			}
			c := client.New(key, opts...)
			if _, err := c.Do(context.Background(), "GET", "/v1/account", nil, nil); err != nil {
				return fmt.Errorf("la key no validó contra la API: %w", err)
			}
			store, fallback := config.NewStore()
			if err := store.SetKey(profile, key); err != nil {
				return err
			}
			env := config.Environment(key)
			if !g.Quiet {
				fmt.Fprintf(cmd.ErrOrStderr(), "✓ Sesión guardada (perfil %q, entorno %s).\n", profile, strings.ToUpper(env))
			}
			if env == "live" {
				fmt.Fprintln(cmd.ErrOrStderr(), "⚠ ATENCIÓN: esta es una key LIVE. Las operaciones mutadoras afectarán datos reales y AEAT.")
			}
			if fallback {
				location := "el fichero de configuración local"
				if p, ok := store.(config.PathProvider); ok {
					location = p.Path()
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "⚠ El keyring del sistema no está disponible; la key se guardó en %s (permisos 600).\n", location)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&apiKeySource, "api-key", "", "lee la key por stdin cuando se pasa `-` (no acepta el valor literal)")
	cmd.Flags().Lookup("api-key").NoOptDefVal = "-"
	return cmd
}

func rejectLoginPositional(_ *cobra.Command, args []string) error {
	if len(args) > 0 {
		return apierr.Usagef("login no acepta la API key como argumento posicional (quedaría en el historial del shell); usa el prompt oculto, --api-key - (stdin) o la env %s", config.EnvAPIKey)
	}
	return nil
}

func readKey(cmd *cobra.Command, fromStdin, noInput bool) (string, error) {
	if fromStdin {
		b, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	if v := os.Getenv(config.EnvAPIKey); v != "" {
		return v, nil
	}
	if noInput || !term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return "", err
		}
		key := strings.TrimSpace(string(b))
		if key == "" {
			return "", apierr.Usagef("sin terminal interactivo y sin key: usa --api-key - (stdin) o define %s", config.EnvAPIKey)
		}
		return key, nil
	}
	fmt.Fprint(cmd.ErrOrStderr(), "API key: ")
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(cmd.ErrOrStderr())
	if err != nil {
		return "", err
	}
	return string(b), nil
}
