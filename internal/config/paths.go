package config

import (
	"os"
	"path/filepath"
)

// ConfigDir devuelve ~/.config/factuarea (respeta XDG_CONFIG_HOME).
func ConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "factuarea"), nil
}

func ConfigFile() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}
