package gen

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// El punto de entrada del generador (`main.go`) lleva `//go:build ignore`.
func buildGeneratorEntrypoint(t *testing.T, document string) string {
	t.Helper()

	dir := t.TempDir()
	testDoc := filepath.Join(dir, "openapi.json")
	if err := os.WriteFile(testDoc, []byte(document), 0o644); err != nil {
		t.Fatalf("escribiendo el documento de prueba: %v", err)
	}

	embedded, err := filepath.Abs(filepath.Join("..", "spec", "openapi.json"))
	if err != nil {
		t.Fatalf("resolviendo la ruta del spec embebido: %v", err)
	}
	entrypoint, err := filepath.Abs("main.go")
	if err != nil {
		t.Fatalf("resolviendo la ruta de main.go: %v", err)
	}

	overlay := filepath.Join(dir, "overlay.json")
	body, err := json.Marshal(struct {
		Replace map[string]string
	}{Replace: map[string]string{embedded: testDoc}})
	if err != nil {
		t.Fatalf("serializando el overlay: %v", err)
	}
	if err := os.WriteFile(overlay, body, 0o644); err != nil {
		t.Fatalf("escribiendo el overlay: %v", err)
	}

	bin := filepath.Join(dir, "generator")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-overlay", overlay, "-o", bin, entrypoint)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("compilando %s: %v\n%s", entrypoint, err, out)
	}
	return bin
}

// runGeneratorEntrypoint ejecuta el binario con el directorio de trabajo en un
// temporal.
func runGeneratorEntrypoint(t *testing.T, bin string) (exitCode int, stderr, generated string) {
	t.Helper()

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "internal", "cmd"), 0o755); err != nil {
		t.Fatalf("preparando el directorio de salida: %v", err)
	}

	var captured bytes.Buffer
	run := exec.Command(bin)
	run.Dir = dir
	run.Stderr = &captured

	var exitErr *exec.ExitError
	switch err := run.Run(); {
	case err == nil:
		exitCode = 0
	case errors.As(err, &exitErr):
		exitCode = exitErr.ExitCode()
	default:
		t.Fatalf("ejecutando el generador: %v", err)
	}

	src, err := os.ReadFile(filepath.Join(dir, "internal", "cmd", "resources_gen.go"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("leyendo el fichero generado: %v", err)
	}
	return exitCode, captured.String(), string(src)
}

// Un identificador fuera de la convención tiene que ROMPER la generación.
func TestGeneratorEntrypointExitsNonZeroNamingTheNonCanonicalOperationID(t *testing.T) {
	exitCode, stderr, generated := runGeneratorEntrypoint(t, buildGeneratorEntrypoint(t, specWithNonCanonicalOperation))

	if exitCode == 0 {
		t.Fatalf("el generador terminó con ÉXITO pese al identificador no canónico; stderr:\n%s", stderr)
	}
	if exitCode != 1 {
		t.Errorf("código de salida = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr, "widgets.purge") {
		t.Errorf("la salida de error no nombra al culpable; stderr:\n%s", stderr)
	}
	// El mensaje manda el arreglo al BACKEND y niega la lista de excepciones.
	if !strings.Contains(stderr, "BACKEND") {
		t.Errorf("la salida de error no dice que el arreglo va en el backend; stderr:\n%s", stderr)
	}
	if !strings.Contains(stderr, "overrides") {
		t.Errorf("la salida de error no prohíbe tapar el hueco con overrides; stderr:\n%s", stderr)
	}
	// El fichero se escribe SIEMPRE y el fallo llega DESPUÉS.
	if !strings.Contains(generated, "OperationID: \"public-api.v1.widgets.list\"") {
		t.Errorf("el fichero generado debe escribirse igualmente con la operación canónica; contenido:\n%s", generated)
	}
	if strings.Contains(generated, "widgets.purge") {
		t.Errorf("la operación no canónica NO debe aparecer en el fichero generado; contenido:\n%s", generated)
	}
}

// El complemento del gate: con todos los identificadores conformes, la
// generación termina con ÉXITO.
func TestGeneratorEntrypointSucceedsWhenEveryOperationIDIsCanonical(t *testing.T) {
	document := strings.ReplaceAll(specWithNonCanonicalOperation, "\"widgets.purge\"", "\"public-api.v1.widgets.purge\"")
	if !strings.Contains(document, "\"public-api.v1.widgets.purge\"") {
		t.Fatal("el documento conforme no se derivó del documento de prueba")
	}

	exitCode, stderr, generated := runGeneratorEntrypoint(t, buildGeneratorEntrypoint(t, document))

	if exitCode != 0 {
		t.Fatalf("código de salida = %d, want 0; stderr:\n%s", exitCode, stderr)
	}
	if stderr != "" {
		t.Errorf("sin identificadores no conformes la salida de error debe quedar vacía; stderr:\n%s", stderr)
	}
	for _, id := range []string{"public-api.v1.widgets.list", "public-api.v1.widgets.purge"} {
		if !strings.Contains(generated, "OperationID: \""+id+"\"") {
			t.Errorf("falta %s en el fichero generado; contenido:\n%s", id, generated)
		}
	}
}
