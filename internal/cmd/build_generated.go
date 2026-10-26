package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/client"
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
	var idempotencyKey string
	var showETag bool
	headerFlags := map[string]*string{}
	fileFlags := map[string]*string{}
	fileArrayFlags := map[string]*[]string{}

	// `axisFillIndex` —no el índice del path— es lo que manda aquí: cubre el eje
	// de empresa y el de cuenta, que un comando nunca tiene a la vez.
	axisIdx := op.axisFillIndex()
	use := op.Action
	for i, p := range op.PathParams {
		// El eje (de empresa o de cuenta) se declara OPCIONAL en el uso porque la
		// flag persistente `--company` o `--account` puede aportarlo; el resto
		// sigue siendo obligatorio exactamente como antes.
		if i == axisIdx {
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
			" campos de texto con --data como objeto JSON plano. En lotes, repite --file-<campo> por archivo."
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
	if axisIdx >= 0 {
		minArgs--
		if op.accountFillIndex() >= 0 {
			long += accountAxisHelp(op, use, axisIdx)
		} else {
			long += "\n\nPuedes fijar la empresa con la flag persistente --company en vez de" +
				" pasarla como argumento posicional. Aportarla por las dos vías a la vez es un error de uso." +
				"\n\nForma canónica:  factuarea " + strings.Join(op.Groups, " ") + " " + use +
				"\nForma corta:     factuarea " + strings.Join(op.Groups, " ") + " " + shortUsage(op, use, axisIdx) +
				" --company <" + op.PathParams[axisIdx].Name + ">" +
				"\nSi tu credencial alcanza una sola empresa, ni siquiera hace falta --company: se resuelve sola."
		}
	}
	if op.requiresExplicitAccount() {
		long += "\n\nEsta operación es irreversible y nombra la cuenta como último argumento (en una transferencia de NIF es la cuenta que cede o recibe el NIF, no solo la que opera): la cuenta se escribe siempre como" +
			" argumento posicional <" + accountPathParam + ">. La flag persistente --account no la rellena a propósito."
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
	if op.requiresExplicitCompany() || op.requiresExplicitAccount() {
		// La aridad exacta ya obliga a escribir el eje; lo que falta es DECIR por
		// qué, porque el usuario que viene de otra operación del eje espera que
		// `--company` o `--account` se lo rellene.
		base := argsRule
		explicitErr := op.explicitCompanyError
		if op.requiresExplicitAccount() {
			explicitErr = op.explicitAccountError
		}
		argsRule = func(cmd *cobra.Command, args []string) error {
			if len(args) < nPath {
				return explicitErr()
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
				axis, err := op.resolveAxisForArgs(cmd.Context(), globalsFrom(cmd), args)
				if err != nil {
					return err
				}
				if _, extra, err = op.splitAxisPositionals(args, axis); err != nil {
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
			if err := validateHeaderFlags(cmd, op); err != nil {
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
				axis, err := op.resolveAxisForArgs(cmd.Context(), g, args)
				if err != nil {
					return err
				}
				values, _, serr := op.splitAxisPositionals(args, axis)
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
					values, _ := cmd.Flags().GetStringSlice(op.arrayQueryFlag(p.Name))
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
				body, headers, err = op.buildBody(resolvedData, dataFile, fileFlags, fileArrayFlags)
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
			// Cabeceras declaradas por el spec (If-Match, If-None-Match…): el valor
			// viaja literal, sin comillas añadidas ni recortes.
			for _, p := range op.HeaderParams {
				if cmd.Flags().Changed(headerFlagName(p)) {
					if headers == nil {
						headers = map[string]string{}
					}
					headers[p.Name] = *headerFlags[p.Name]
				}
			}
			if idempotencyKey != "" {
				if headers == nil {
					headers = map[string]string{}
				}
				headers["Idempotency-Key"] = idempotencyKey
			}
			// El spec exige `Idempotency-Key` en esta operación y el método no
			// es POST: `client.Do` ya la autogenera para todo verbo mutador
			// (POST/PUT/PATCH/DELETE, ver `design.md` D6 y el hallazgo de
			// `anchors/contract.md`), así que esto es aditivo y nunca pisa una
			// cabecera ya presente (ni la del flag de arriba, ni una explícita
			// de `--data`/multipart).
			if op.IdempotencyRequired && op.Method != "POST" {
				if headers == nil {
					headers = map[string]string{}
				}
				if _, present := headers["Idempotency-Key"]; !present {
					headers["Idempotency-Key"] = client.NewIdempotencyKey()
				}
			}
			httpClient := cc.client
			if op.NoRetry {
				// Una sola llamada: un reintento ciego podría duplicar el efecto.
				httpClient = httpClient.WithoutRetry()
			}
			resp, err := httpClient.Do(context.Background(), op.Method, full, body, headers)
			if err != nil {
				output.PrintError(cmd.ErrOrStderr(), err, cc.errorFormat)
				return &AlreadyReported{Err: err}
			}
			if showETag {
				if etag := resp.Header.Get("ETag"); etag != "" {
					fmt.Fprintf(cmd.ErrOrStderr(), "etag: %s\n", etag)
				}
			}
			if resp.StatusCode == http.StatusNotModified {
				fmt.Fprintln(cmd.ErrOrStderr(), "304: sin cambios; no se escribe salida")
				return nil
			}
			if op.BinaryContentType != "" && resp.StatusCode == http.StatusOK && (!op.DualBinary || !isJSONContentType(resp.ContentType)) {
				return writeBinary(cmd, resp.Body, outputPath)
			}
			if resp.StatusCode == http.StatusAccepted {
				if ra := resp.Header.Get("Retry-After"); ra != "" {
					fmt.Fprintf(cmd.ErrOrStderr(), "retry-after: %s\n", ra)
				}
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
		desc := flagUsage(p.Description)
		if p.Required {
			desc = strings.TrimSpace("(requerido) " + desc)
		}
		if strings.HasSuffix(p.Name, "[]") {
			name := op.arrayQueryFlag(p.Name)
			c.Flags().StringSlice(name, nil, desc)
			if name == p.Name {
				continue
			}
		}
		c.Flags().String(p.Name, "", desc)
	}
	for _, p := range op.HeaderParams {
		desc := flagUsage(p.Description)
		if desc == "" {
			desc = "cabecera " + p.Name + " (se envía tal cual)"
		}
		if p.Required {
			desc = strings.TrimSpace("(requerido) " + desc)
		}
		headerFlags[p.Name] = c.Flags().String(headerFlagName(p), "", desc)
	}
	if op.ReturnsETag {
		c.Flags().BoolVar(&showETag, "show-etag", false, "escribe el ETag de la respuesta en stderr (stdout no cambia)")
	}
	if op.isMutating() {
		c.Flags().StringVar(&idempotencyKey, "idempotency-key", "", "clave estable para repetir esta misma operación sin duplicarla")
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
		for _, ff := range op.Body.FileArrayFields {
			fileArrayFlags[ff] = c.Flags().StringArray("file-"+ff, nil, "ruta al fichero para "+ff+" (repite el flag por archivo)")
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
		c.Flags().StringVar(&confirmFlag, "confirm", "", flagUsage(confirmHelp))
	}
	if op.RequiredScope != "" {
		c.Flags().BoolVar(&skipScopeCheck, "skip-scope-check", false, "no verificar scopes localmente antes de la llamada")
	}
	return c
}

// isJSONContentType dice si un Content-Type es JSON (`application/json`,
// `application/problem+json`…), ignorando parámetros y mayúsculas.
func isJSONContentType(ct string) bool {
	mediaType, _, _ := strings.Cut(strings.ToLower(ct), ";")
	return strings.HasSuffix(strings.TrimSpace(mediaType), "json")
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

// resolveAxisForArgs resuelve los dos ejes de UNA invocación. Cada resolución
// devuelve sin tocar la red cuando la operación no tiene su slot rellenable, así
// que solo la del eje que la operación tiene puede consultar la identidad.
func (op genOp) resolveAxisForArgs(ctx context.Context, g *GlobalFlags, args []string) (axisValues, error) {
	company, err := op.resolveCompanyForArgs(ctx, g, args)
	if err != nil {
		return axisValues{}, err
	}
	account, err := op.resolveAccountForArgs(ctx, g, args)
	if err != nil {
		return axisValues{}, err
	}
	return axisValues{Company: company, Account: account}, nil
}

// accountAxisHelp es el párrafo de la ayuda larga de los comandos con slot de
// cuenta rellenable: explica `--account` y la regla de escrituras.
func accountAxisHelp(op genOp, use string, axisIdx int) string {
	groups := strings.Join(op.Groups, " ")
	help := "\n\nPuedes fijar la cuenta con la flag persistente --account en vez de" +
		" pasarla como argumento posicional. Aportarla por las dos vías a la vez es un error de uso." +
		"\n\nForma canónica:  factuarea " + groups + " " + use +
		"\nForma corta:     factuarea " + groups + " " + shortUsage(op, use, axisIdx) +
		" --account <" + op.PathParams[axisIdx].Name + ">"
	if op.autoResolvesAccount() {
		help += "\nEs una lectura: si no pasas la cuenta, se deduce de la identidad de tu credencial."
	} else {
		help += "\nEs una escritura: la cuenta no se deduce, hay que nombrarla (como argumento posicional o con --account)." +
			" Sin ella, la CLI termina con un error de uso, sin enviar nada, y te dice cuál es la cuenta de tu credencial."
	}
	return help
}

// shortUsage compone el literal del uso SIN el slot de eje, para enseñar en
// la ayuda larga la forma corta junto a la canónica.
func shortUsage(op genOp, use string, axisIdx int) string {
	if axisIdx < 0 {
		return use
	}
	slot := " [" + op.PathParams[axisIdx].Name + "]"
	return strings.Replace(use, slot, "", 1)
}

// flagUsage prepara un texto para el uso de un flag. pflag (UnquoteUsage) toma la
// primera palabra entre comillas invertidas como NOMBRE DEL TIPO del flag, así que
// una descripción del spec con `código` hacía que la ayuda mostrara «--flag código»
// en vez de su tipo real. Se sustituyen por comillas simples: el texto se lee igual
// y el tipo sale del valor del flag.
func flagUsage(s string) string {
	return strings.ReplaceAll(s, "`", "'")
}

// headerFlagName es el flag de una cabecera declarada: su nombre en minúsculas
// (`If-Match` -> `--if-match`).
func headerFlagName(p genParam) string { return strings.ToLower(p.Name) }

// validateHeaderFlags exige las cabeceras requeridas y rechaza un valor vacío:
// una cabecera condicional vacía no significa nada y el servidor la ignoraría.
func validateHeaderFlags(cmd *cobra.Command, op genOp) error {
	var missing []string
	for _, p := range op.HeaderParams {
		name := headerFlagName(p)
		if !cmd.Flags().Changed(name) {
			if p.Required {
				missing = append(missing, "--"+name)
			}
			continue
		}
		if v, _ := cmd.Flags().GetString(name); v == "" {
			return apierr.Usagef("--%s no puede estar vacío", name)
		}
	}
	if len(missing) > 0 {
		return apierr.Usagef("faltan flags requeridos: %s", strings.Join(missing, ", "))
	}
	return nil
}
