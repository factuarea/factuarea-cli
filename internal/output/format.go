package output

import (
	"os"

	"golang.org/x/term"
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

func ResolveErrorFormat(jsonFlag bool, stderr *os.File) Format {
	if jsonFlag {
		return JSON
	}
	if IsTTY(stderr) {
		return Human
	}
	return JSON
}

func IsTTY(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}
