package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

const CurrentVersion = 1

type Config struct {
	Version      int                `toml:"version"`
	DataDir      string             `toml:"data_dir"`
	DatabasePath string             `toml:"database_path"`
	Sports       []string           `toml:"sports"`
	Sources      map[string]Source  `toml:"sources"`
	Routing      Routing            `toml:"routing"`
	Telegram     Telegram           `toml:"telegram"`
	Webhooks     map[string]Webhook `toml:"webhooks"`
	API          API                `toml:"api"`
	MCP          MCP                `toml:"mcp"`
	Daemon       Daemon             `toml:"daemon"`
	Profiles     map[string]Profile `toml:"profiles"`
}

type Source struct {
	Enabled bool `toml:"enabled"`
}

type Routing struct {
	Provider    string `toml:"provider"`
	Email       string `toml:"email"`
	Password    string `toml:"password"`
	AccessToken string `toml:"access_token"`
}

type Telegram struct {
	Enabled        bool    `toml:"enabled"`
	BotToken       string  `toml:"bot_token"`
	AllowedChatIDs []int64 `toml:"allowed_chat_ids"`
	DefaultChatID  int64   `toml:"default_chat_id"`
}

type Webhook struct {
	Enabled bool   `toml:"enabled"`
	URL     string `toml:"url"`
	Secret  string `toml:"secret"`
}

type API struct {
	Address     string `toml:"address"`
	AllowRemote bool   `toml:"allow_remote"`
	AuthToken   string `toml:"auth_token"`
}

type MCP struct {
	AllowWrites bool `toml:"allow_writes"`
}

type Daemon struct {
	RefreshMinutes int `toml:"refresh_minutes"`
}

type Profile struct {
	Origin      string `toml:"origin"`
	Destination string `toml:"destination"`
	TravelMode  string `toml:"travel_mode"`
}

func Default() (Config, error) {
	dataDir, err := userDataDir()
	if err != nil {
		return Config{}, fmt.Errorf("resolve user data directory: %w", err)
	}
	dataDir = filepath.Join(dataDir, "kaypoh")
	return Config{
		Version:      CurrentVersion,
		DataDir:      dataDir,
		DatabasePath: filepath.Join(dataDir, "kaypoh.db"),
		Sources: map[string]Source{
			"sportsg-facilities": {Enabled: true},
			"onemap":             {Enabled: true},
		},
		Routing:  Routing{Provider: "onemap"},
		Webhooks: map[string]Webhook{},
		API:      API{Address: "127.0.0.1:8373"},
		MCP:      MCP{},
		Daemon:   Daemon{RefreshMinutes: 10},
		Profiles: map[string]Profile{},
	}, nil
}

func userDataDir() (string, error) {
	if value := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); value != "" {
		return value, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share"), nil
}

func DefaultPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}
	return filepath.Join(configDir, "kaypoh", "config.toml"), nil
}

func Load(path string) (Config, error) {
	config, err := Default()
	if err != nil {
		return Config{}, err
	}
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return config, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	if _, err := toml.Decode(string(contents), &config); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (config Config) Validate() error {
	if config.Version != CurrentVersion {
		return fmt.Errorf("unsupported config version %d", config.Version)
	}
	if config.DatabasePath == "" {
		return errors.New("database_path cannot be empty")
	}
	if config.Daemon.RefreshMinutes < 1 {
		return errors.New("daemon.refresh_minutes must be at least 1")
	}
	if config.API.Address == "" {
		return errors.New("api.address cannot be empty")
	}
	if config.API.AllowRemote && strings.TrimSpace(config.API.AuthToken) == "" {
		return errors.New("api.auth_token is required when api.allow_remote is true")
	}
	return nil
}

func (config Config) ResolveSecret(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "env:") {
		return value, nil
	}
	key := strings.TrimPrefix(value, "env:")
	if key == "" {
		return "", errors.New("empty environment-variable reference")
	}
	resolved, ok := os.LookupEnv(key)
	if !ok || resolved == "" {
		return "", fmt.Errorf("environment variable %q is not set", key)
	}
	return resolved, nil
}

func WriteExample(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("refusing to overwrite existing config %q", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check config path: %w", err)
	}
	const example = `version = 1
# data_dir = ""
# database_path = ""
sports = ["badminton", "pickleball"]

[sources.sportsg-facilities]
enabled = true

[sources.onemap]
enabled = true

[routing]
provider = "onemap"
# email = "env:ONEMAP_EMAIL"
# password = "env:ONEMAP_PASSWORD"
# access_token = "env:ONEMAP_ACCESS_TOKEN"

[telegram]
enabled = false
# bot_token = "env:KAYPOH_TELEGRAM_BOT_TOKEN"
# allowed_chat_ids = [123456789]

[api]
address = "127.0.0.1:8373"
allow_remote = false

[mcp]
allow_writes = false

[daemon]
refresh_minutes = 10
`
	if err := os.WriteFile(path, []byte(example), 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}
