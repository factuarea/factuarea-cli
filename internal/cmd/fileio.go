package cmd

import (
	"io"
	"os"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

func readInputFile(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, apierr.ReadFileUsagef(path, err)
	}
	return b, nil
}

func readInputStream(r io.Reader, source string) ([]byte, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, apierr.Usagef("no se pudo leer %s: %s", source, apierr.FileIOReason(err))
	}
	return b, nil
}

func writeOutputFile(path string, body []byte) error {
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return apierr.WriteFileUsagef(path, err)
	}
	return nil
}
