package cmd

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	reAcceptsArgs   = regexp.MustCompile(`accepts (\d+) arg\(s\), received (\d+)`)
	reAcceptsAtMost = regexp.MustCompile(`accepts at most (\d+) arg\(s\), received (\d+)`)
	reInvalidArg    = regexp.MustCompile(`invalid argument "([^"]*)" for "([^"]*)" flag: (.*)`)
	reRequiredFlag  = regexp.MustCompile(`^required flag\(s\) (.*) not set$`)
	reUnknownFlag   = regexp.MustCompile(`unknown flag: (.*)`)
	reUnknownShort  = regexp.MustCompile(`unknown shorthand flag: (.*)`)
	reNeedsArg      = regexp.MustCompile(`flag needs an argument: (.*)`)
)

func translateCobraError(msg string) string {
	switch {
	case strings.HasPrefix(msg, "requires at least"):
		return "faltan argumentos posicionales para este comando"
	case strings.Contains(msg, "accepts 0 arg(s)"):
		return "este comando no acepta argumentos posicionales"
	}
	if m := reAcceptsArgs.FindStringSubmatch(msg); m != nil {
		return fmt.Sprintf("este comando acepta %s argumento(s), recibió %s", m[1], m[2])
	}
	if m := reAcceptsAtMost.FindStringSubmatch(msg); m != nil {
		return fmt.Sprintf("este comando acepta como máximo %s argumento(s), recibió %s", m[1], m[2])
	}
	if m := reInvalidArg.FindStringSubmatch(msg); m != nil {
		return fmt.Sprintf("valor inválido %q para %s: %s", m[1], m[2], translateParseError(m[3]))
	}
	if m := reRequiredFlag.FindStringSubmatch(msg); m != nil {
		return "faltan flags requeridos: " + strings.ReplaceAll(m[1], `"`, "")
	}
	if m := reUnknownFlag.FindStringSubmatch(msg); m != nil {
		return "flag desconocido: " + m[1]
	}
	if m := reUnknownShort.FindStringSubmatch(msg); m != nil {
		return "flag corto desconocido: " + m[1]
	}
	if m := reNeedsArg.FindStringSubmatch(msg); m != nil {
		return "el flag necesita un valor: " + m[1]
	}
	return msg
}

func translateParseError(detail string) string {
	switch {
	case strings.Contains(detail, "ParseInt"), strings.Contains(detail, "ParseFloat"):
		return "se esperaba un número"
	case strings.Contains(detail, "ParseBool"):
		return "se esperaba true o false"
	case strings.Contains(detail, "ParseDuration"), strings.Contains(detail, "time:"):
		return "se esperaba una duración (ej. 2s, 500ms)"
	}
	return detail
}
