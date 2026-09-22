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

	// `companyFillIndex` —no `companyPathParamIndex`— es lo que manda aquí.
	companyIdx := op.companyFillIndex()
	use := op.Action
	for i, p := range op.PathParams {
		// El eje de empresa se declara OPCIONAL en el uso porque la flag
		// persistente `--company` puede aportarlo; el resto sigue siendo
		// obligatorio exactamente como antes.
		if i == companyIdx {
			use += " [" + p.Name + "]"
			continue
		}
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
	// La aridad deja de ser exacta en cuanto uno de los posicionales es
	// opcional: el eje quita uno por abajo y el atajo del campo único de cuerpo
	// añade uno por arriba. Si algún día coincidieran, `len(args) == nPath`
	// tendría DOS lecturas y la invocación se RECHAZA nombrando las dos, nunca
	// se desambigua por la FORMA del argumento. Hoy el universo es vacío:
	// `singlePositionalField` exige cero parámetros de ruta.
	minArgs, maxArgs := nPath, nPath
	if companyIdx >= 0 {
		minArgs--
		long += "\n\nPuedes fijar la empresa con la flag persistente --company en vez de" +
			" pasarla como argumento posicional. Aportarla por las dos vías a la vez es un error de uso." +
			"\n\nForma canónica:  factuarea " + strings.Join(op.Groups, " ") + " " + use +
			"\nForma corta:     factuarea " + strings.Join(op.Groups, " ") + " " + shortUsage(op, use, companyIdx) +
			" --company <" + op.PathParams[companyIdx].Name + ">" +
			"\nSi tu credencial alcanza una sola empresa, ni siquiera hace falta --company: se resuelve sola."
	}
	if hasPosField {
		maxArgs++
		use += " [" + posField.flagName + "]"
		long += "\n\nPuedes pasar " + posField.flagName + " como argumento posicional en vez de --" + posField.flagName + "."
	}
	var argsRule cobra.PositionalArgs = cobra.ExactArgs(minArgs)
	if maxArgs != minArgs {
		argsRule = cobra.RangeArgs(minArgs, maxArgs)
	}
	if op.requiresExplicitCompany() {
		// La aridad exacta ya obliga a escribir el eje; lo que falta es DECIR por
		// qué, porque el usuario que viene de otra operación del eje espera que
		// `--company` se lo rellene.
		base := argsRule
		argsRule = func(cmd *cobra.Command, args []string) error {
			if len(args) < nPath {
				return op.explicitCompanyError()
			}
			return base(cmd, args)
		}
	}

	c := &cobra.Command{
		Use:        use,
		Short:      op.Summary,
		Long:       strings.TrimSpace(long),
		Args:       UsageArgs(argsRule),
		Deprecated: deprecatedMsg(op),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			// `--skeleton` y `--dry-run` prometen en su propia ayuda imprimir «sin
			// llamar a la API».
			printsOnly := skeleton || (dryRun && op.typedBody())
			var extra []string
			if printsOnly {
				extra = op.extraPositionals(args)
			} else {
				// La lista de argumentos se NORMALIZA antes de que nadie la lea.
				company, err := op.resolveCompanyForArgs(cmd.Context(), globalsFrom(cmd), args)
				if err != nil {
					return err
				}
				if _, extra, err = op.splitPositionals(args, company); err != nil {
					return err
				}
			}
			if hasPosField && len(extra) > 0 {
				if cmd.Flags().Changed(posField.flagName) {
					return apierr.Usagef("no pases %s como argumento posicional y como --%s a la vez", posField.flagName, posField.flagName)
				}
				if err := cmd.Flags().Set(posField.flagName, extra[0]); err != nil {
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
			// Mismo corte que en `PreRunE`, y por el mismo motivo: la plantilla se
			// deriva del contrato de la operación, no de la invocación, así que no
			// necesita empresa, ni path compuesto, ni red.
			if skeleton && op.typedBody() {
				out, err := skeletonBody(op)
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(out))
				return nil
			}
			g := globalsFrom(cmd)
			// Mismo gateo que en `PreRunE`: `--dry-run` imprime el cuerpo compilado y
			// vuelve, así que no hay path que componer ni empresa que resolver.
			var pathValues []string
			if !(dryRun && op.typedBody()) {
				company, err := op.resolveCompanyForArgs(cmd.Context(), g, args)
				if err != nil {
					return err
				}
				values, _, serr := op.splitPositionals(args, company)
				if serr != nil {
					return serr
				}
				if verr := validateResourceArgs(op, values); verr != nil {
					return verr
				}
				pathValues = values
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
					output.PrintError(cmd.ErrOrStderr(), perr, cc.errorFormat)
					return &AlreadyReported{Err: perr}
				}
			}
			if op.Irreversible {
				resourceID := op.confirmResourceID(pathValues)
				if err := safety.Confirm(resourceID, confirmFlag, output.IsTTY(os.Stdin), g.NoInput, func(p string) (string, error) {
					fmt.Fprint(cmd.ErrOrStderr(), p)
					line, rerr := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
					return strings.TrimSpace(line), rerr
				}); err != nil {
					return err
				}
			}
			path := op.buildPath(pathValues)
			q := url.Values{}
			for _, p := range op.QueryParams {
				if strings.HasSuffix(p.Name, "[]") {
					values, _ := cmd.Flags().GetStringSlice(strings.TrimSuffix(p.Name, "[]"))
					for _, value := range values {
						q.Add(p.Name, value)
					}
				}
				if v, _ := cmd.Flags().GetString(p.Name); v != "" {
					q.Add(p.Name, v)
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
				output.PrintError(cmd.ErrOrStderr(), err, cc.errorFormat)
				return &AlreadyReported{Err: err}
			}
			if op.BinaryContentType != "" {
				return writeBinary(cmd, resp.Body, outputPath)
			}
			if g.Verbose && resp.RequestID != "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "request_id: %s\n", resp.RequestID)
			}
			if op.isMutating() && len(bytesTrim(resp.Body)) == 0 {
				if op.Method == "DELETE" {
					return writeDeleteConfirmation(cmd, op, pathValues, cc.format)
				}
				return writeMutationConfirmation(cmd, op, pathValues, cc.format)
			}
			return output.PrintBody(cmd.OutOrStdout(), resp.Body, cc.format)
		},
	}

	for _, p := range op.QueryParams {
		desc := p.Description
		if p.Required {
			desc = strings.TrimSpace("(requerido) " + desc)
		}
		if strings.HasSuffix(p.Name, "[]") {
			c.Flags().StringSlice(strings.TrimSuffix(p.Name, "[]"), nil, desc)
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
		return writeOutputFile(out, body)
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
		output.PrintError(cmd.ErrOrStderr(), err, cc.errorFormat)
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

// shortUsage compone el literal del uso SIN el slot de eje, para enseñar en
// la ayuda larga la forma corta junto a la canónica.
func shortUsage(op genOp, use string, companyIdx int) string {
	if companyIdx < 0 {
		return use
	}
	slot := " [" + op.PathParams[companyIdx].Name + "]"
	return strings.Replace(use, slot, "", 1)
}
