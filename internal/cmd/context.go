package cmd

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"sync"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/client"
	"github.com/factuarea/factuarea-cli/internal/config"
	"github.com/factuarea/factuarea-cli/internal/output"
	"github.com/spf13/cobra"
)

type cliContext struct {
	res         config.Resolution
	client      *client.Client
	format      output.Format
	errorFormat output.Format
	g           *GlobalFlags

	scopesOnce sync.Once
	scopesVal  []string
	scopesErr  error
}

func (cc *cliContext) scopes(ctx context.Context) ([]string, error) {
	cc.scopesOnce.Do(func() {
		resp, err := cc.client.Do(ctx, "GET", "/v1/account", nil, nil)
		if err != nil {
			cc.scopesErr = err
			return
		}
		var payload struct {
			Data struct {
				APIKey struct {
					Scopes []string `json:"scopes"`
				} `json:"api_key"`
			} `json:"data"`
		}
		if err := json.Unmarshal(resp.Body, &payload); err != nil {
			cc.scopesErr = err
			return
		}
		cc.scopesVal = payload.Data.APIKey.Scopes
	})
	return cc.scopesVal, cc.scopesErr
}

const envBaseURL = "FACTUAREA_BASE_URL"

func baseURLClientOptions(allowInsecure bool) ([]client.Option, error) {
	base := os.Getenv(envBaseURL)
	if base == "" {
		return nil, nil
	}
	if err := validateBaseURLTransport(base, allowInsecure); err != nil {
		return nil, err
	}
	return []client.Option{client.WithBaseURL(base)}, nil
}

func validateBaseURLTransport(base string, allowInsecure bool) error {
	u, err := url.Parse(base)
	if err != nil {
		return apierr.Usagef("%s no es una URL válida: %v", envBaseURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return apierr.Usagef("%s debe usar http:// o https:// (got %q)", envBaseURL, u.Scheme)
	}
	if u.Scheme == "http" && !isLoopbackHost(u.Hostname()) && !allowInsecure {
		return apierr.Usagef("%s usa http:// hacia un host no-loopback (%s); la API key viajaría en claro. Usa https:// o pasa --allow-insecure-transport si es intencional", envBaseURL, u.Hostname())
	}
	return nil
}

func newCLIContext(g *GlobalFlags, stdinKey string) (*cliContext, error) {
	store, _ := config.NewStore()
	res, err := config.ResolveAPIKey(stdinKey, g.Profile, os.Getenv, store)
	if err != nil {
		return nil, err
	}
	if !config.ValidKeyFormat(res.APIKey) {
		return nil, apierr.Usagef("la API key resuelta no tiene formato válido (se espera fact_test_… o fact_live_… seguido de 24 caracteres). Ejecuta `factuarea login`.")
	}
	opts, err := baseURLClientOptions(g.AllowInsecureTransport)
	if err != nil {
		return nil, err
	}
	f := output.ResolveFormat(g.JSON, output.IsTTY(os.Stdout))
	return &cliContext{
		res:         res,
		client:      client.New(res.APIKey, opts...),
		format:      f,
		errorFormat: output.ResolveErrorFormat(g.JSON, os.Stderr),
		g:           g,
	}, nil
}

func globalsFrom(cmd *cobra.Command) *GlobalFlags {
	if g, ok := cmd.Context().Value(globalsKey{}).(*GlobalFlags); ok && g != nil {
		return g
	}
	return &GlobalFlags{}
}

type globalsKey struct{}

type AlreadyReported struct{ Err error }

func (e *AlreadyReported) Error() string { return e.Err.Error() }
func (e *AlreadyReported) Unwrap() error { return e.Err }
