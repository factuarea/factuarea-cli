package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestFlagUsageReplacesBackticks(t *testing.T) {
	got := flagUsage("Rechazo `series_invoice_kind_invalid` y `422`.")
	if strings.Contains(got, "`") || got != "Rechazo 'series_invoice_kind_invalid' y '422'." {
		t.Fatalf("flagUsage = %q", got)
	}
}

// pflag toma la primera palabra entre comillas invertidas del uso como NOMBRE DEL
// TIPO del flag. Ningún flag de ningún comando puede llevarlas, salvo `login
// --api-key`, que las usa a propósito para mostrar «--api-key -».
func TestNoFlagUsageCarriesBackticks(t *testing.T) {
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		c.Flags().VisitAll(func(f *pflag.Flag) {
			if strings.Contains(f.Usage, "`") && !(c.CommandPath() == "factuarea login" && f.Name == "api-key") {
				t.Errorf("%s --%s: el uso lleva comillas invertidas (pflag las toma como tipo): %q", c.CommandPath(), f.Name, f.Usage)
			}
		})
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(NewRootCmd())
}

func TestHelpShowsTheRealFlagType(t *testing.T) {
	cases := []struct {
		args []string
		want []string
		deny string
	}{
		{[]string{"series", "active", "--help"}, []string{"--invoice_kind string", "'series_invoice_kind_invalid'"}, "invoice_kind series_invoice_kind_invalid"},
		{[]string{"invoices", "create", "--help"}, []string{"--prices-include-tax", "--payment.method string"}, "`"},
		{[]string{"series", "update", "--help"}, []string{"--counter-reset string", "--number-format string", "--initial-number int", "--invoice-kind string"}, "`"},
		{[]string{"invoices", "corrective", "--help"}, []string{"--correction-nature string", "--series-id string"}, "`"},
	}
	for _, tc := range cases {
		out, err := runCmd(t, "", tc.args...)
		if err != nil {
			t.Fatalf("%v: %v", tc.args, err)
		}
		for _, w := range tc.want {
			if !strings.Contains(out, w) {
				t.Errorf("%v: falta %q en la ayuda:\n%s", tc.args, w, out)
			}
		}
		if strings.Contains(out, tc.deny) {
			t.Errorf("%v: la ayuda no debe contener %q", tc.args, tc.deny)
		}
	}
}
