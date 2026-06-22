package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/factuarea/factuarea-cli/internal/output"
	"github.com/factuarea/factuarea-cli/internal/safety"
	"github.com/spf13/cobra"
)

func buildGeneratedCommand(op genOp) *cobra.Command {
	var data, dataFile, outputPath string
	var paginate bool
	fileFlags := map[string]*string{}

	use := op.Action
	for _, p := range op.PathParams {
		use += " <" + p.Name + ">"
	}
	long := op.Summary
	if op.Body != nil && op.Body.Kind == "json" && op.Body.Example != "" {
		long += "\n\nEjemplo de body (--data):\n  " + op.Body.Example
	}
	if op.Body != nil && op.Body.Kind == "multipart" {
		long += "\n\nSubida multipart: pasa el fichero con --file-<campo> y los" +
			" campos de texto con --data como objeto JSON plano."
	}

	c := &cobra.Command{
		Use:        use,
		Short:      op.Summary,
		Long:       strings.TrimSpace(long),
		Args:       UsageArgs(cobra.ExactArgs(len(op.PathParams))),
		Deprecated: deprecatedMsg(op),
		RunE: func(cmd *cobra.Command, args []string) error {
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

			body, headers, err := op.buildBody(data, dataFile, fileFlags)
			if err != nil {
				return err
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
			return output.PrintBody(cmd.OutOrStdout(), resp.Body, cc.format)
		},
	}

	for _, p := range op.QueryParams {
		c.Flags().String(p.Name, "", p.Description)
	}
	if op.Body != nil && op.Body.Kind == "json" {
		c.Flags().StringVarP(&data, "data", "d", "", "cuerpo JSON de la petición")
		c.Flags().StringVar(&dataFile, "data-file", "", "ruta a un fichero con el cuerpo JSON")
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
		c.Flags().BoolVar(&paginate, "paginate", false, "recorre todas las páginas (cursor)")
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
