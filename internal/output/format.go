package output

import (
	"errors"
	"os"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

type Format int

const (
	Human Format = iota
	JSON
	Plain
)

func ResolveFormat(jsonFlag, plainFlag, isTTY bool) (Format, error) {
	if jsonFlag && plainFlag {
		return Human, &apierr.UsageError{Err: errors.New("--json y --plain son mutuamente excluyentes")}
	}
	if jsonFlag {
		return JSON, nil
	}
	if plainFlag {
		return Plain, nil
	}
	if isTTY {
		return Human, nil
	}
	return JSON, nil
}

func IsTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
