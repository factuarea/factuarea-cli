package output

import (
	"os"
)

type Format int

const (
	Human Format = iota
	JSON
)

func ResolveFormat(jsonFlag, isTTY bool) Format {
	if jsonFlag {
		return JSON
	}
	if isTTY {
		return Human
	}
	return JSON
}

func IsTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
