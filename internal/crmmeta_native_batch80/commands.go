package crmmeta_native_batch80

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// Resolver must supply the existing profile's actual credential/server/version.
// No company, grants, environment, family or Review claim can be supplied in JSON.
type Resolver func(*cobra.Command) (Config, error)

func Register(root *cobra.Command, resolver Resolver) error {
	if resolver == nil {
		return invalidContract
	}
	var crm *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "crm" {
			crm = c
			break
		}
	}
	if crm == nil {
		crm = &cobra.Command{Use: "crm", Short: "CRM nativo"}
		root.AddCommand(crm)
	}
	for _, c := range crm.Commands() {
		if c.Name() == "meta" {
			return errors.New("native Meta command already registered")
		}
	}
	crm.AddCommand(NewCommand(resolver))
	return nil
}

// NewCommand is standalone until the owner wires Register to its current native auth resolver.
func NewCommand(resolver Resolver) *cobra.Command {
	g := &cobra.Command{Use: "meta", Short: "Configuración Meta humana (crm_connections:write; OAuth crm_connections.write)"}
	specs := []struct {
		name, use, short string
		a                Action
		n                int
	}{
		{"oauth-prepare", "oauth-prepare", "Prepara OAuth de la Page real; no abre ni completa el navegador", PrepareOAuth, 0},
		{"oauth-complete", "oauth-complete", "Instala la credencial desde state/code reales; no activa mapping ni consent", CompleteOAuth, 0},
		{"options", "options", "Lee opciones de conexión y credencial actuales", Options, 0},
		{"mapping", "mapping connection form", "Lee el mapping nativo publicado", GetMapping, 2},
		{"inspect", "inspect connection form", "Consulta Fields y CAS originales; App Review permanece unavailable", Inspect, 2},
		{"publish-mapping", "publish-mapping connection form", "Publica selección de mapping explícita con CAS/checksum reales", PublishMapping, 2},
		{"publish-consent", "publish-consent connection", "Solicita consentimiento humano; Review ausente conserva el rechazo nativo", PublishConsent, 1},
	}
	for _, s := range specs {
		s := s
		var bodyFile, confirm, output, outFile string
		var dry bool
		c := &cobra.Command{Use: s.use, Short: s.short, Args: cobra.ExactArgs(s.n), SilenceUsage: true, SilenceErrors: true}
		c.RunE = func(cmd *cobra.Command, args []string) error {
			if output != "summary" && output != "json" {
				return invalidContract
			}
			connection, form := "", ""
			if len(args) > 0 {
				connection = args[0]
			}
			if len(args) > 1 {
				form = args[1]
			}
			var wire []byte
			if s.a == Options || s.a == GetMapping {
				if bodyFile != "" {
					return invalidContract
				}
			} else {
				if bodyFile == "" {
					return errors.New("native Meta command requires --body-file with exact human inputs")
				}
				f, e := os.Open(bodyFile)
				if e != nil {
					return errors.New("unable to read native Meta body file")
				}
				st, e := f.Stat()
				if e != nil || !st.Mode().IsRegular() || st.Size() > 1048576 {
					f.Close()
					return invalidContract
				}
				wire, e = io.ReadAll(io.LimitReader(f, 1048577))
				f.Close()
				if e != nil {
					return errors.New("unable to read native Meta body file")
				}
			}
			intent, e := NewIntent(s.a, connection, form, wire)
			if e != nil {
				return e
			}
			r := Routes()[s.a]
			if r.Mutating {
				expected := connection
				if s.a == PrepareOAuth {
					expected = intent.body.(PrepareInput).PageID
				}
				if s.a == CompleteOAuth {
					expected = "oauth-complete"
				}
				if s.a == PublishMapping {
					expected = form
				}
				if confirm != expected {
					return errors.New("native Meta command requires independent --confirm for the selected Page/form/connection or oauth-complete")
				}
			} else if confirm != "" {
				return invalidContract
			}
			if dry {
				// OAuth completion preview excludes code/state. The actual body is never logged by errors.
				preview := map[string]any{"operation_id": r.OperationID, "method": r.Method, "path": intent.path, "fine_scope": FineScope, "oauth_scope": OAuthScope, "mutating": r.Mutating}
				if s.a == CompleteOAuth {
					preview["body"] = "actual state/code retained privately"
				} else if len(wire) > 0 {
					preview["body"] = json.RawMessage(wire)
				}
				return json.NewEncoder(cmd.OutOrStdout()).Encode(preview)
			}
			if resolver == nil {
				return errors.New("native credential resolver is required")
			}
			config, e := resolver(cmd)
			if e != nil {
				return errors.New("native current credential is unavailable")
			}
			client, e := NewClient(config)
			if e != nil {
				return e
			}
			var saved *os.File
			if outFile != "" {
				saved, e = os.OpenFile(outFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
				if e != nil {
					return errors.New("native Meta output-file must be a new private file")
				}
				defer saved.Close()
			}
			response, e := client.Execute(cmd.Context(), intent)
			if e != nil {
				if saved != nil {
					os.Remove(outFile)
				}
				return e
			}
			if saved != nil {
				if _, e = saved.Write(response.RawBody); e != nil {
					return errors.New("native Meta response accepted but private output could not be saved; do not replay automatically")
				}
				if e = saved.Sync(); e != nil {
					return errors.New("native Meta response accepted but private output sync failed; do not replay automatically")
				}
			}
			if output == "json" {
				_, e = cmd.OutOrStdout().Write(response.RawBody)
				if e == nil && !strings.HasSuffix(string(response.RawBody), "\n") {
					_, e = fmt.Fprintln(cmd.OutOrStdout())
				}
				return e
			}
			switch v := response.Data.(type) {
			case OAuthPreparation:
				_, e = fmt.Fprintln(cmd.OutOrStdout(), "OAuth preparado; abra manualmente:", v.AuthorizationURL)
			case OAuthInstallation:
				_, e = fmt.Fprintln(cmd.OutOrStdout(), "Credencial instalada:", v.CredentialUUID, "(mapping y consent requieren acciones humanas independientes)")
			case Inspection:
				_, e = fmt.Fprintln(cmd.OutOrStdout(), "Mapping inspeccionado; App Review unavailable; CAS reales conservados")
			case MappingPublication:
				_, e = fmt.Fprintf(cmd.OutOrStdout(), "Mapping publicado %s, versión %d\n", v.FormUUID, v.MappingVersion)
			case ConnectionOptions:
				_, e = fmt.Fprintf(cmd.OutOrStdout(), "Opciones actuales: %d\n", len(v.Options))
			case ConsentPublication:
				_, e = fmt.Fprintln(cmd.OutOrStdout(), "Consentimiento publicado por el emisor nativo:", v.ConsentVersion)
			}
			return e
		}
		c.Flags().StringVar(&bodyFile, "body-file", "", "JSON exacto; CAS y decisiones humanas reales; state/code sólo del navegador")
		c.Flags().StringVar(&confirm, "confirm", "", "Page ID para preparar; oauth-complete; form UUID para mapping; connection UUID para consent")
		c.Flags().BoolVar(&dry, "dry-run", false, "valida sin HTTP; preview OAuth complete redactado")
		c.Flags().StringVar(&output, "output", "summary", "summary o json; JSON conserva el cuerpo nativo original")
		c.Flags().StringVar(&outFile, "output-file", "", "nuevo archivo privado 0600 para conservar respuesta/state/CAS originales")
		g.AddCommand(c)
	}
	return g
}
