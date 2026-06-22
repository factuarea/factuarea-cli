//go:build ignore

package main

import (
	"log"
	"os"

	"github.com/factuarea/factuarea-cli/internal/cmd"
	"github.com/spf13/cobra/doc"
)

func main() {
	root := cmd.NewRootCmd()
	root.DisableAutoGenTag = true

	if err := os.MkdirAll("completions", 0o755); err != nil {
		log.Fatal(err)
	}
	for _, sh := range []string{"bash", "zsh", "fish", "powershell"} {
		f, err := os.Create("completions/factuarea." + sh)
		if err != nil {
			log.Fatal(err)
		}
		switch sh {
		case "bash":
			err = root.GenBashCompletionV2(f, true)
		case "zsh":
			err = root.GenZshCompletion(f)
		case "fish":
			err = root.GenFishCompletion(f, true)
		case "powershell":
			err = root.GenPowerShellCompletionWithDesc(f)
		}
		f.Close()
		if err != nil {
			log.Fatal(err)
		}
	}

	if err := os.MkdirAll("manpages", 0o755); err != nil {
		log.Fatal(err)
	}
	hdr := &doc.GenManHeader{Title: "FACTUAREA", Section: "1"}
	if err := doc.GenManTree(root, hdr, "manpages"); err != nil {
		log.Fatal(err)
	}
}
