package cmd

import (
	"os"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/client"
	"github.com/factuarea/factuarea-cli/internal/config"
	"github.com/factuarea/factuarea-cli/internal/output"
	"github.com/spf13/cobra"
)

// cliContext resuelve credenciales, cliente y formato a partir de los flags.
type cliContext struct {
	res    config.Resolution
	client *client.Client
	format output.Format
	g      *GlobalFlags
}

const envBaseURL = "FACTUAREA_BASE_URL" // override para tests/staging

func newCLIContext(g *GlobalFlags, stdinKey string) (*cliContext, error) {
	store, _ := config.NewStore()
	res, err := config.ResolveAPIKey(stdinKey, g.Profile, os.Getenv, store)
	if err != nil {
		return nil, err
	}
	if !config.ValidKeyFormat(res.APIKey) {
		return nil, apierr.Usagef("la API key resuelta no tiene formato válido (se espera fact_test_… o fact_live_… de 24 caracteres). Ejecuta `factuarea login`.")
	}
	opts := []client.Option{}
	if base := os.Getenv(envBaseURL); base != "" {
		opts = append(opts, client.WithBaseURL(base))
	}
	f, err := output.ResolveFormat(g.JSON, g.Plain, output.IsTTY(os.Stdout))
	if err != nil {
		return nil, err
	}
	return &cliContext{
		res:    res,
		client: client.New(res.APIKey, opts...),
		format: f,
		g:      g,
	}, nil
}

// globalsFrom recupera el puntero a GlobalFlags guardado por NewRootCmd.
func globalsFrom(cmd *cobra.Command) *GlobalFlags {
	if g, ok := cmd.Context().Value(globalsKey{}).(*GlobalFlags); ok && g != nil {
		return g
	}
	return &GlobalFlags{}
}

type globalsKey struct{}

// AlreadyReported envuelve un error cuyo mensaje YA se imprimió (vía
// output.PrintError). main.go lo detecta para no imprimirlo dos veces, pero
// exit.ForError sigue derivando el código correcto a través de Unwrap.
type AlreadyReported struct{ Err error }

func (e *AlreadyReported) Error() string { return e.Err.Error() }
func (e *AlreadyReported) Unwrap() error { return e.Err }
