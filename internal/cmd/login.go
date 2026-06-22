package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/factuarea/factuarea-cli/internal/client"
	"github.com/factuarea/factuarea-cli/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newLoginCmd() *cobra.Command {
	var fromStdin bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Guarda tu API key (fact_test_… o fact_live_…)",
		Long:  "Lee la API key por prompt oculto, por stdin (--api-key -) o por la env FACTUAREA_API_KEY.\nNUNCA la pases como valor literal de flag.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			g := globalsFrom(cmd)
			key, err := readKey(cmd, fromStdin, g.NoInput)
			if err != nil {
				return err
			}
			key = strings.TrimSpace(key)
			if !config.ValidKeyFormat(key) {
				return fmt.Errorf("formato de key inválido (se espera fact_test_… o fact_live_… con 24 caracteres)")
			}
			profile := g.Profile
			if profile == "" {
				profile = "default"
			}
			opts := []client.Option{}
			if base := os.Getenv(envBaseURL); base != "" {
				opts = append(opts, client.WithBaseURL(base))
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
				fmt.Fprintln(cmd.ErrOrStderr(), "⚠ El keyring del sistema no está disponible; la key se guardó en ~/.config/factuarea/config.toml (permisos 600).")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&fromStdin, "api-key", false, "lee la key por stdin cuando se pasa `-` (no acepta el valor literal)")
	cmd.Flags().Lookup("api-key").NoOptDefVal = "false"
	return cmd
}

func readKey(cmd *cobra.Command, fromStdin, noInput bool) (string, error) {
	if fromStdin || noInput {
		b, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	fmt.Fprint(cmd.ErrOrStderr(), "API key: ")
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(cmd.ErrOrStderr())
	if err != nil {
		b2, err2 := io.ReadAll(cmd.InOrStdin())
		if err2 != nil {
			return "", err
		}
		return string(b2), nil
	}
	return string(b), nil
}
