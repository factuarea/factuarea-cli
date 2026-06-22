package config

import (
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/pelletier/go-toml/v2"
	"github.com/zalando/go-keyring"
)

const keyringService = "factuarea-cli"

// ErrNotFound se devuelve cuando no hay credencial guardada para el profile.
// Exportado para que los consumidores distingan "no logueado" con errors.Is.
var ErrNotFound = errors.New("credencial no encontrada")

// keyringStore guarda cada key en el keyring del SO bajo el servicio
// "factuarea-cli" y la cuenta = nombre del profile.
type keyringStore struct{}

func (keyringStore) GetKey(profile string) (string, error) {
	k, err := keyring.Get(keyringService, profile)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	return k, err
}
func (keyringStore) SetKey(profile, key string) error {
	return keyring.Set(keyringService, profile, key)
}
func (keyringStore) DeleteKey(profile string) error {
	err := keyring.Delete(keyringService, profile)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

// fileStore es el fallback: TOML en ~/.config/factuarea/config.toml, chmod 600.
type fileStore struct {
	path string
	mu   sync.Mutex
}

type fileDoc struct {
	Profiles map[string]profileEntry `toml:"profiles"`
}
type profileEntry struct {
	APIKey string `toml:"api_key"`
}

func NewFileStore(path string) Store { return &fileStore{path: path} }

func (s *fileStore) load() (fileDoc, error) {
	var doc fileDoc
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		doc.Profiles = map[string]profileEntry{}
		return doc, nil
	}
	if err != nil {
		return doc, err
	}
	if err := toml.Unmarshal(b, &doc); err != nil {
		return doc, err
	}
	if doc.Profiles == nil {
		doc.Profiles = map[string]profileEntry{}
	}
	return doc, nil
}

func (s *fileStore) save(doc fileDoc) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// Defensa: si el dir ya existía con permisos laxos, forzar 700 (contiene la key).
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	b, err := toml.Marshal(doc)
	if err != nil {
		return err
	}
	// Temp ÚNICO (O_EXCL: no reutiliza ni sigue symlink), mismo dir → Rename atómico.
	f, err := os.CreateTemp(dir, ".config-*.toml.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func (s *fileStore) GetKey(profile string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load()
	if err != nil {
		return "", err
	}
	e, ok := doc.Profiles[profile]
	if !ok || e.APIKey == "" {
		return "", ErrNotFound
	}
	return e.APIKey, nil
}

func (s *fileStore) SetKey(profile, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load()
	if err != nil {
		return err
	}
	doc.Profiles[profile] = profileEntry{APIKey: key}
	return s.save(doc)
}

func (s *fileStore) DeleteKey(profile string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load()
	if err != nil {
		return err
	}
	delete(doc.Profiles, profile)
	return s.save(doc)
}

// NewStore prueba el keyring del SO; si no está disponible, cae al archivo y
// devuelve usingFallback=true para que el comando avise (nunca degrada en silencio).
func NewStore() (Store, bool) {
	// Probe del keyring: set+get+delete de un valor centinela.
	const probe = "__factuarea_probe__"
	if err := keyring.Set(keyringService, probe, "1"); err == nil {
		_ = keyring.Delete(keyringService, probe)
		return keyringStore{}, false
	}
	path, err := ConfigFile()
	if err != nil {
		path = filepath.Join(os.TempDir(), "factuarea-config.toml")
	}
	return NewFileStore(path), true
}
