package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/output"
	"github.com/factuarea/factuarea-cli/internal/safety"
	"github.com/spf13/cobra"
)

func buildGeneratedCommand(op genOp) *cobra.Command {
	var data, dataFile, outputPath string
	var paginate bool
	var confirmFlag string
	var skipScopeCheck bool
	var dryRun, skeleton bool
	fileFlags := map[string]*string{}

	use := op.Action
	for _, p := range op.PathParams {
		use += " <" + p.Name + ">"
	}
	long := op.Summary
	if op.Body != nil && op.Body.Kind == "json" && op.Body.Example != "" {
		long += "\n\nEjemplo de body (--data):\n  " + op.Body.Example
	}
	long += bodyFieldsHelp(op)
	if op.Body != nil && op.Body.Kind == "multipart" {
		long += "\n\nSubida multipart: pasa el fichero con --file-<campo> y los" +
			" campos de texto con --data como objeto JSON plano."
	}

	posField, hasPosField := singlePositionalField(op)
	nPath := len(op.PathParams)
	argsRule := cobra.ExactArgs(nPath)
	if hasPosField {
		argsRule = cobra.RangeArgs(nPath, nPath+1)
		use += " [" + posField.flagName + "]"
		long += "\n\nPuedes pasar " + posField.flagName + " como argumento posicional en vez de --" + posField.flagName + "."
	}

	c := &cobra.Command{
		Use:        use,
		Short:      op.Summary,
		Long:       strings.TrimSpace(long),
		Args:       UsageArgs(argsRule),
		Deprecated: deprecatedMsg(op),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if hasPosField && len(args) > nPath {
				if cmd.Flags().Changed(posField.flagName) {
					return apierr.Usagef("no pases %s como argumento posicional y como --%s a la vez", posField.flagName, posField.flagName)
				}
				if err := cmd.Flags().Set(posField.flagName, args[nPath]); err != nil {
					return apierr.Usagef("valor inválido para %s: %v", posField.flagName, err)
				}
			}
			if strings.TrimSpace(data) != "" && dataFile != "" {
				return apierr.Usagef("no uses --data y --data-file a la vez: elige una sola fuente del cuerpo")
			}
			if err := validateRequiredQueryFlags(cmd, op); err != nil {
				return err
			}
			if err := validateEnumFlags(cmd, op); err != nil {
				return err
			}
			if skeleton {
				return nil
			}
			rawUsed := strings.TrimSpace(data) != "" || dataFile != ""
			if !rawUsed {
				if err := validateRequiredBodyFlags(cmd, op); err != nil {
					return err
				}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateResourceArgs(op, args); err != nil {
				return err
			}
			if skeleton && op.typedBody() {
				out, err := skeletonBody(op)
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(out))
				return nil
			}
			var typedBody []byte
			if op.typedBody() {
				b, berr := compileTypedBody(cmd, op, data, dataFile)
				if berr != nil {
					return berr
				}
				rawUsed := strings.TrimSpace(data) != "" || dataFile != ""
				if rawUsed {
					b, berr = validateRawJSONBody(b, true)
					if berr != nil {
						return berr
					}
				}
				typedBody = b
				if dryRun {
					fmt.Fprintln(cmd.OutOrStdout(), string(typedBody))
					return nil
				}
			}
			g := globalsFrom(cmd)
			cc, err := newCLIContext(g, "")
			if err != nil {
				return err
			}
			if op.isMutating() {
				if err := safety.RequireLive(cc.res.Environment, g.Live); err != nil {
					return err
				}
			}
			if op.RequiredScope != "" && !skipScopeCheck {
				scopes, serr := cc.scopes(context.Background())
				if serr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "aviso: no pude verificar scopes (%v); continúo\n", serr)
				} else if !safety.HasScope(scopes, op.RequiredScope) {
					perr := apierr.Permf("la API key no tiene el scope %q requerido por esta operación", op.RequiredScope)
					output.PrintError(cmd.ErrOrStderr(), perr, cc.format)
					return &AlreadyReported{Err: perr}
				}
			}
			if op.Irreversible {
				resourceID := op.confirmResourceID(args)
				if err := safety.Confirm(resourceID, confirmFlag, output.IsTTY(os.Stdin), g.NoInput, func(p string) (string, error) {
					fmt.Fprint(cmd.ErrOrStderr(), p)
					line, rerr := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
					return strings.TrimSpace(line), rerr
				}); err != nil {
					return err
				}
			}
			path := op.buildPath(args)
			q := url.Values{}
			for _, p := range op.QueryParams {
				if v, _ := cmd.Flags().GetString(p.Name); v != "" {
					q.Set(p.Name, v)
				}
			}

			if op.isPaginated() && paginate {
				return runPaginated(cmd, cc, path, q)
			}

			var body []byte
			var headers map[string]string
			if op.typedBody() {
				body = typedBody
			} else {
				resolvedData := data
				if op.Body != nil && op.Body.Kind == "json" && data != "" {
					b, rerr := readDataArg(data, cmd.InOrStdin())
					if rerr != nil {
						return rerr
					}
					resolvedData = string(b)
				}
				body, headers, err = op.buildBody(resolvedData, dataFile, fileFlags)
				if err != nil {
					return err
				}
				if op.Body != nil && op.Body.Kind == "json" {
					body, err = validateRawJSONBody(body, true)
					if err != nil {
						return err
					}
				}
				if dryRun {
					fmt.Fprintln(cmd.OutOrStdout(), string(body))
					return nil
				}
			}
			full := path
			if len(q) > 0 {
				full += "?" + q.Encode()
			}
			resp, err := cc.client.Do(context.Background(), op.Method, full, body, headers)
			if err != nil {
				output.PrintError(cmd.ErrOrStderr(), err, cc.format)
				return &AlreadyReported{Err: err}
			}
			if op.BinaryContentType != "" {
				return writeBinary(cmd, resp.Body, outputPath)
			}
			if g.Verbose && resp.RequestID != "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "request_id: %s\n", resp.RequestID)
			}
			if op.Method == "DELETE" && len(bytesTrim(resp.Body)) == 0 {
				return writeDeleteConfirmation(cmd, op, args, cc.format)
			}
			return output.PrintBody(cmd.OutOrStdout(), resp.Body, cc.format)
		},
	}

	for _, p := range op.QueryParams {
		desc := p.Description
		if p.Required {
			desc = strings.TrimSpace("(requerido) " + desc)
		}
		c.Flags().String(p.Name, "", desc)
	}
	if op.Body != nil && op.Body.Kind == "json" {
		c.Flags().StringVarP(&data, "data", "d", "", "cuerpo JSON de la petición (@fichero o - para stdin)")
		c.Flags().StringVar(&dataFile, "data-file", "", "ruta a un fichero con el cuerpo JSON")
		registerFieldFlags(c, op)
		c.Flags().BoolVar(&dryRun, "dry-run", false, "compila el cuerpo y lo imprime sin llamar a la API")
		if op.typedBody() {
			c.Flags().BoolVar(&skeleton, "skeleton", false, "imprime una plantilla del cuerpo con los campos y tipos, sin llamar a la API")
		}
	}
	if op.Body != nil && op.Body.Kind == "multipart" {
		c.Flags().StringVarP(&data, "data", "d", "", "campos de texto del multipart como objeto JSON plano")
		for _, ff := range op.Body.FileFields {
			v := c.Flags().String("file-"+ff, "", "ruta al fichero para el campo "+ff)
			fileFlags[ff] = v
		}
	}
	if op.BinaryContentType != "" {
		c.Flags().StringVarP(&outputPath, "output", "o", "", "escribe la respuesta binaria a este fichero")
	}
	if op.isPaginated() {
		c.Flags().BoolVar(&paginate, "paginate", false, "recorre todas las páginas (cursor) y emite un objeto JSON por línea (NDJSON), no el envelope {data, has_more, next_cursor}")
	}
	if op.Irreversible {
		confirmHelp := "confirma la operación irreversible pasando el id del recurso"
		if len(op.PathParams) == 0 {
			confirmHelp = "confirma la operación irreversible pasando el token literal " + op.Action
		}
		c.Flags().StringVar(&confirmFlag, "confirm", "", confirmHelp)
	}
	if op.RequiredScope != "" {
		c.Flags().BoolVar(&skipScopeCheck, "skip-scope-check", false, "no verificar scopes localmente antes de la llamada")
	}
	return c
}

func writeBinary(cmd *cobra.Command, body []byte, out string) error {
	if out != "" {
		return os.WriteFile(out, body, 0o644)
	}
	if output.IsTTY(os.Stdout) {
		return fmt.Errorf("la respuesta es binaria; usa -o <fichero> para guardarla")
	}
	_, err := cmd.OutOrStdout().Write(body)
	return err
}

func runPaginated(cmd *cobra.Command, cc *cliContext, path string, query url.Values) error {
	w := cmd.OutOrStdout()
	enc := json.NewEncoder(w)
	err := cc.client.Paginate(context.Background(), path, query, func(item json.RawMessage) error {
		return enc.Encode(item)
	})
	if err != nil {
		output.PrintError(cmd.ErrOrStderr(), err, cc.format)
		return &AlreadyReported{Err: err}
	}
	return nil
}

func deprecatedMsg(op genOp) string {
	if op.Deprecated {
		return "esta operación está deprecada en la API"
	}
	return ""
}
