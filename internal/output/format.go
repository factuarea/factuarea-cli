package output

import (
	"errors"
	"os"
)

type Format int

const (
	Human Format = iota
	JSON
	Plain
)

// ResolveFormat: flag explícito > autodetección TTY. --json y --plain son
// mutuamente excluyentes. Sin flags: Human en TTY, JSON fuera de TTY.
func ResolveFormat(jsonFlag, plainFlag, isTTY bool) (Format, error) {
	if jsonFlag && plainFlag {
		return Human, errors.New("--json y --plain son mutuamente excluyentes")
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
