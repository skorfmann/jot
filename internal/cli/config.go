package cli

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	DefaultServer string `toml:"default_server"`
}

func configDir() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "jot"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "jot"), nil
}

func loadConfig() (Config, error) {
	var cfg Config
	dir, err := configDir()
	if err != nil {
		return cfg, err
	}
	path := filepath.Join(dir, "config.toml")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	_, err = toml.DecodeFile(path, &cfg)
	return cfg, err
}

func saveConfig(cfg Config) error {
	dir, err := configDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "config.toml"), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
}

func resolveServer(flagValue string) (string, error) {
	server := flagValue
	if server == "" {
		server = os.Getenv("JOT_SERVER")
	}
	if server == "" {
		cfg, err := loadConfig()
		if err != nil {
			return "", err
		}
		server = cfg.DefaultServer
	}
	if server == "" {
		return "", errors.New("server is required; pass --server, set JOT_SERVER, or run jot login --server URL")
	}
	u, err := url.Parse(server)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errors.New("server must be an absolute URL")
	}
	return strings.TrimRight(server, "/"), nil
}
