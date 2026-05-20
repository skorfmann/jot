package server

import (
	"errors"
	"os"
	"strconv"
	"time"

	"github.com/skorfmann/jot/internal/auth"
	"github.com/skorfmann/jot/internal/storage"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Server struct {
		Addr         string `yaml:"addr"`
		BaseURL      string `yaml:"base_url"`
		HistorySize  int    `yaml:"history_size"`
		InsecureHTTP bool   `yaml:"insecure_http"`
	} `yaml:"server"`
	Storage storage.Config `yaml:"storage"`
	Auth    auth.RawConfig `yaml:"auth"`
	Limits  Limits         `yaml:"limits"`
}

type Limits struct {
	FilesPerPush int   `yaml:"files_per_push"`
	BytesPerFile int64 `yaml:"bytes_per_file"`
	BytesPerPush int64 `yaml:"bytes_per_push"`
}

func DefaultConfig() Config {
	var cfg Config
	cfg.Server.Addr = ":8080"
	cfg.Server.HistorySize = 10
	cfg.Storage.Region = "auto"
	cfg.Auth.SessionTTL = 8 * time.Hour
	cfg.Limits.FilesPerPush = 100
	cfg.Limits.BytesPerFile = 10 * 1024 * 1024
	cfg.Limits.BytesPerPush = 50 * 1024 * 1024
	return cfg
}

func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()
	if path != "" {
		body, err := os.ReadFile(path)
		if err != nil {
			return cfg, err
		}
		if err := yaml.Unmarshal(body, &cfg); err != nil {
			return cfg, err
		}
	}
	applyEnv(&cfg)
	if cfg.Server.BaseURL == "" {
		return cfg, errors.New("server.base_url is required")
	}
	return cfg, nil
}

func applyEnv(cfg *Config) {
	setString(&cfg.Server.Addr, "JOT_SERVER_ADDR")
	setString(&cfg.Server.BaseURL, "JOT_SERVER_BASE_URL")
	setInt(&cfg.Server.HistorySize, "JOT_SERVER_HISTORY_SIZE")
	setBool(&cfg.Server.InsecureHTTP, "JOT_SERVER_INSECURE_HTTP")
	setString(&cfg.Storage.Endpoint, "JOT_STORAGE_ENDPOINT")
	setString(&cfg.Storage.Region, "JOT_STORAGE_REGION")
	setString(&cfg.Storage.Bucket, "JOT_STORAGE_BUCKET")
	setString(&cfg.Storage.AccessKeyID, "JOT_STORAGE_ACCESS_KEY_ID")
	setString(&cfg.Storage.SecretAccessKey, "JOT_STORAGE_SECRET_ACCESS_KEY")
	setBool(&cfg.Storage.ForcePathStyle, "JOT_STORAGE_FORCE_PATH_STYLE")
	setString(&cfg.Auth.Mode, "JOT_AUTH_MODE")
	setString(&cfg.Auth.Issuer, "JOT_AUTH_ISSUER")
	setString(&cfg.Auth.Audience, "JOT_AUTH_AUDIENCE")
	setString(&cfg.Auth.ClientID, "JOT_AUTH_CLIENT_ID")
	setString(&cfg.Auth.CLIClientID, "JOT_AUTH_CLI_CLIENT_ID")
	setString(&cfg.Auth.CLIClientSecret, "JOT_AUTH_CLI_CLIENT_SECRET")
	setString(&cfg.Auth.ClientSecret, "JOT_AUTH_CLIENT_SECRET")
	setString(&cfg.Auth.CookieSecret, "JOT_AUTH_COOKIE_SECRET")
	if v := os.Getenv("JOT_AUTH_SESSION_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Auth.SessionTTL = d
		}
	}
	if v := os.Getenv("JOT_AUTH_AUTHORIZE_HD"); v != "" {
		if cfg.Auth.Authorize.RequiredClaims == nil {
			cfg.Auth.Authorize.RequiredClaims = map[string]any{}
		}
		cfg.Auth.Authorize.RequiredClaims["hd"] = v
	}
	setInt(&cfg.Limits.FilesPerPush, "JOT_LIMITS_FILES_PER_PUSH")
	setInt64(&cfg.Limits.BytesPerFile, "JOT_LIMITS_BYTES_PER_FILE")
	setInt64(&cfg.Limits.BytesPerPush, "JOT_LIMITS_BYTES_PER_PUSH")
}

func setString(dst *string, name string) {
	if v := os.Getenv(name); v != "" {
		*dst = v
	}
}

func setBool(dst *bool, name string) {
	if v := os.Getenv(name); v != "" {
		parsed, err := strconv.ParseBool(v)
		if err == nil {
			*dst = parsed
		}
	}
}

func setInt(dst *int, name string) {
	if v := os.Getenv(name); v != "" {
		parsed, err := strconv.Atoi(v)
		if err == nil {
			*dst = parsed
		}
	}
}

func setInt64(dst *int64, name string) {
	if v := os.Getenv(name); v != "" {
		parsed, err := strconv.ParseInt(v, 10, 64)
		if err == nil {
			*dst = parsed
		}
	}
}

func ServerConfigScaffold() string {
	return `# jot.yaml
server:
  addr: ":8080"
  base_url: https://jot.example.com
  history_size: 10
  insecure_http: false

storage:
  endpoint: https://s3.amazonaws.com
  region: auto
  bucket: jot-prod
  access_key_id: replace-me
  secret_access_key: replace-me
  force_path_style: false

auth:
  issuer: https://accounts.google.com
  audience: 1234567890-abc.apps.googleusercontent.com
  client_id: 1234567890-abc.apps.googleusercontent.com
  cli_client_id: 1234567890-cli.apps.googleusercontent.com
  cli_client_secret: GOCSPX-cli-replace-me
  client_secret: GOCSPX-replace-me
  cookie_secret: replace-with-openssl-rand-hex-32
  session_ttl: 8h
  authorize:
    required_claims:
      hd: example.com

limits:
  files_per_push: 100
  bytes_per_file: 10485760
  bytes_per_push: 52428800
`
}
