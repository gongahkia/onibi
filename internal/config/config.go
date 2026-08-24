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
	Enabled             bool           `toml:"enabled"`
	RefreshMinutes      int            `toml:"refresh_minutes"`
	AvailabilityMaxDays int            `toml:"availability_max_days"`
	API                 SourceAPI      `toml:"api"`
	Browser             SourceBrowser  `toml:"browser"`
	Public              SourcePublic   `toml:"public"`
	ActiveSG            SourceActiveSG `toml:"activesg"`
}

// SourceAPI describes a partner-provided, read-only availability endpoint.
// Its responses must be either a JSON array of slots or an object containing
// a slots array. Venue discovery follows the same shape with a venues array.
type SourceAPI struct {
	Enabled          bool   `toml:"enabled"`
	BaseURL          string `toml:"base_url"`
	VenuesPath       string `toml:"venues_path"`
	AvailabilityPath string `toml:"availability_path"`
	BearerToken      string `toml:"bearer_token"`
}

// SourceBrowser provides the approved, non-interactive browser access path.
// SessionState is base64-encoded Playwright storage-state JSON. It is never
// persisted by kaypoh. Login selectors are optional when a session is supplied.
type SourceBrowser struct {
	Enabled            bool   `toml:"enabled"`
	AvailabilityURL    string `toml:"availability_url"`
	LoginURL           string `toml:"login_url"`
	Username           string `toml:"username"`
	Password           string `toml:"password"`
	UsernameSelector   string `toml:"username_selector"`
	PasswordSelector   string `toml:"password_selector"`
	SubmitSelector     string `toml:"submit_selector"`
	ReadySelector      string `toml:"ready_selector"`
	SlotJSONSelector   string `toml:"slot_json_selector"`
	SessionStateBase64 string `toml:"session_state_base64"`
}

// SourcePublic is the final read-only path for a partner-approved public
// availability page. Its JSON selectors use the text of a script or element.
type SourcePublic struct {
	Enabled          bool   `toml:"enabled"`
	AvailabilityURL  string `toml:"availability_url"`
	SlotJSONSelector string `toml:"slot_json_selector"`
}

// SourceActiveSG configures the dedicated, read-only ActiveSG badminton
// reader. It uses only an imported browser session and does not automate
// login, booking, ballot review, payment, confirmation, CAPTCHA, or OTP.
type SourceActiveSG struct {
	Enabled            bool     `toml:"enabled"`
	VenueListURL       string   `toml:"venue_list_url"`
	VenueNames         []string `toml:"venue_names"`
	ScanAll            bool     `toml:"scan_all"`
	SessionStateBase64 string   `toml:"session_state_base64"`
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
			"sportsg-facilities":       {Enabled: true},
			"onemap":                   {Enabled: true},
			"myactivesg":               {Enabled: false, RefreshMinutes: 60},
			"onepa":                    {Enabled: false, RefreshMinutes: 30},
			"the-kallang":              {Enabled: false, RefreshMinutes: 30},
			"sba-stadium":              {Enabled: true, RefreshMinutes: 60, AvailabilityMaxDays: 7},
			"singapore-badminton-hall": {Enabled: true, RefreshMinutes: 60, AvailabilityMaxDays: 7},
			"smash-arena":              {Enabled: true, RefreshMinutes: 60, AvailabilityMaxDays: 1},
			"wyse-active":              {Enabled: true, RefreshMinutes: 60, AvailabilityMaxDays: 7},
			"trusmash":                 {Enabled: false, RefreshMinutes: 30},
		},
		Routing:  Routing{Provider: "onemap"},
		Webhooks: map[string]Webhook{},
		API:      API{Address: "127.0.0.1:8373"},
		MCP:      MCP{},
		Daemon:   Daemon{RefreshMinutes: 30},
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
	for id, source := range config.Sources {
		if source.RefreshMinutes < 0 {
			return fmt.Errorf("sources.%s.refresh_minutes cannot be negative", id)
		}
		if source.AvailabilityMaxDays < 0 {
			return fmt.Errorf("sources.%s.availability_max_days cannot be negative", id)
		}
		if !source.Enabled {
			continue
		}
		if source.API.Enabled && (strings.TrimSpace(source.API.BaseURL) == "" || strings.TrimSpace(source.API.AvailabilityPath) == "") {
			return fmt.Errorf("sources.%s.api needs base_url and availability_path when enabled", id)
		}
		if source.Browser.Enabled {
			if strings.TrimSpace(source.Browser.AvailabilityURL) == "" || strings.TrimSpace(source.Browser.SlotJSONSelector) == "" {
				return fmt.Errorf("sources.%s.browser needs availability_url and slot_json_selector when enabled", id)
			}
			if strings.TrimSpace(source.Browser.SessionStateBase64) == "" && (strings.TrimSpace(source.Browser.LoginURL) == "" || strings.TrimSpace(source.Browser.Username) == "" || strings.TrimSpace(source.Browser.Password) == "" || strings.TrimSpace(source.Browser.UsernameSelector) == "" || strings.TrimSpace(source.Browser.PasswordSelector) == "" || strings.TrimSpace(source.Browser.SubmitSelector) == "") {
				return fmt.Errorf("sources.%s.browser needs an imported session or login credentials and selectors", id)
			}
		}
		if source.Public.Enabled && (strings.TrimSpace(source.Public.AvailabilityURL) == "" || strings.TrimSpace(source.Public.SlotJSONSelector) == "") {
			return fmt.Errorf("sources.%s.public needs availability_url and slot_json_selector when enabled", id)
		}
		if source.ActiveSG.Enabled {
			if id != "myactivesg" {
				return fmt.Errorf("sources.%s.activesg is only supported for myactivesg", id)
			}
			if strings.TrimSpace(source.ActiveSG.VenueListURL) == "" || strings.TrimSpace(source.ActiveSG.SessionStateBase64) == "" {
				return fmt.Errorf("sources.%s.activesg needs venue_list_url and session_state_base64 when enabled", id)
			}
			if !source.ActiveSG.ScanAll && len(normalizedStrings(source.ActiveSG.VenueNames)) == 0 {
				return fmt.Errorf("sources.%s.activesg needs venue_names or scan_all = true", id)
			}
		}
		if !builtInPublicReader(id) && id != "sportsg-facilities" && id != "onemap" && !source.API.Enabled && !source.Browser.Enabled && !source.Public.Enabled && !source.ActiveSG.Enabled {
			return fmt.Errorf("sources.%s is enabled without an availability access mode", id)
		}
	}
	if config.API.Address == "" {
		return errors.New("api.address cannot be empty")
	}
	if config.API.AllowRemote && strings.TrimSpace(config.API.AuthToken) == "" {
		return errors.New("api.auth_token is required when api.allow_remote is true")
	}
	return nil
}

func builtInPublicReader(sourceID string) bool {
	switch sourceID {
	case "sba-stadium", "singapore-badminton-hall", "smash-arena", "wyse-active":
		return true
	default:
		return false
	}
}

func normalizedStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
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

[sources.sportsg-facilities]
enabled = true

[sources.onemap]
enabled = true

# Partner readers are read-only. Configure API first; otherwise configure an
# approved service account or imported Playwright storage state. Values holding
# credentials must use env: references.
[sources.myactivesg]
enabled = false
refresh_minutes = 60
# availability_max_days = 0 # 0 uses the source's supported maximum

# The dedicated ActiveSG badminton reader is session-import only. It is tried
# after any enabled generic API, browser, and public reader. It reads the venue
# list, clicks date cards, and records visible instant hourly slots. It never
# selects a slot or opens ballot, checkout, or payment flows.
[sources.myactivesg.activesg]
enabled = false
venue_list_url = "https://activesg.gov.sg/facility-bookings/activities/YLONatwvqJfikKOmB5N9U/venues"
# venue_names = ["Jurong East Sport Hall"]
scan_all = false
# session_state_base64 = "env:KAYPOH_ACTIVESG_SESSION_STATE_B64"

[sources.myactivesg.api]
enabled = false
# base_url = "https://partner.example"
# availability_path = "/availability"
# bearer_token = "env:KAYPOH_ACTIVESG_API_TOKEN"

[sources.myactivesg.browser]
enabled = false
# availability_url = "https://partner.example/availability"
# username = "env:KAYPOH_ACTIVESG_USERNAME"
# password = "env:KAYPOH_ACTIVESG_PASSWORD"
# session_state_base64 = "env:KAYPOH_ACTIVESG_SESSION_STATE_B64"
# slot_json_selector = "script#kaypoh-slots"

[sources.myactivesg.public]
enabled = false
# availability_url = "https://partner.example/availability"
# slot_json_selector = "script#kaypoh-slots"

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
refresh_minutes = 30
`
	if err := os.WriteFile(path, []byte(example), 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}
