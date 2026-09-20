// Package config loads the server configuration from the environment. Every
// secret arrives this way (a Kubernetes Secret mounted as environment
// variables); nothing sensitive is ever read from a values file.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the fully resolved server configuration.
type Config struct {
	Addr    string
	BaseURL string

	DiscordClientID     string
	DiscordClientSecret string

	SessionKey    []byte
	SessionTTL    time.Duration
	CookieSecure  bool
	SubmitPerHour int

	GitHubAppID          string
	GitHubInstallationID int64
	GitHubPrivateKey     []byte
	GitHubWebhookSecret  []byte
	GitHubOwner          string
	GitHubRepo           string
	GitHubBaseBranch     string
	GitHubAPIBase        string

	DatabaseURL          string
	BootstrapAdminID     string
	BootstrapAdminName   string
	StaticDir            string
	AllowInsecureCookies bool

	// DevSkipAuth bypasses Discord OAuth entirely and logs every browser in as
	// DevUserDiscordID/DevUserName. It only ever takes effect alongside
	// AllowInsecureCookies, so a misconfigured production deploy cannot
	// accidentally disable authentication.
	DevSkipAuth      bool
	DevUserDiscordID string
	DevUserName      string
}

// Getenv is the lookup used to read the environment, overridable in tests.
type Getenv func(string) string

// Load reads and validates the configuration.
func Load(getenv Getenv) (Config, error) {
	c := Config{
		Addr:                orDefault(getenv("ADDR"), ":8080"),
		BaseURL:             strings.TrimSuffix(getenv("BASE_URL"), "/"),
		DiscordClientID:     getenv("DISCORD_CLIENT_ID"),
		DiscordClientSecret: getenv("DISCORD_CLIENT_SECRET"),
		GitHubAppID:         getenv("GITHUB_APP_ID"),
		GitHubOwner:         orDefault(getenv("GITHUB_OWNER"), "ieyoukan"),
		GitHubRepo:          orDefault(getenv("GITHUB_REPO"), "PPLALE-web"),
		GitHubBaseBranch:    orDefault(getenv("GITHUB_BASE_BRANCH"), "main"),
		GitHubAPIBase:       getenv("GITHUB_API_BASE"),
		DatabaseURL:         getenv("DATABASE_URL"),
		BootstrapAdminID:    getenv("BOOTSTRAP_ADMIN_DISCORD_ID"),
		BootstrapAdminName:  orDefault(getenv("BOOTSTRAP_ADMIN_NAME"), "bootstrap admin"),
		StaticDir:           orDefault(getenv("STATIC_DIR"), "web/dist"),
	}

	c.DevSkipAuth = truthy(getenv("DEV_SKIP_AUTH"))

	var missing []string
	require := func(name, value string) {
		if value == "" {
			missing = append(missing, name)
		}
	}
	require("BASE_URL", c.BaseURL)
	if !c.DevSkipAuth {
		require("DISCORD_CLIENT_ID", c.DiscordClientID)
		require("DISCORD_CLIENT_SECRET", c.DiscordClientSecret)
	}
	require("GITHUB_APP_ID", c.GitHubAppID)
	require("GITHUB_APP_INSTALLATION_ID", getenv("GITHUB_APP_INSTALLATION_ID"))
	require("SESSION_KEY", getenv("SESSION_KEY"))
	require("GITHUB_WEBHOOK_SECRET", getenv("GITHUB_WEBHOOK_SECRET"))
	if getenv("GITHUB_APP_PRIVATE_KEY") == "" && getenv("GITHUB_APP_PRIVATE_KEY_FILE") == "" {
		missing = append(missing, "GITHUB_APP_PRIVATE_KEY または GITHUB_APP_PRIVATE_KEY_FILE")
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("config: 必須の環境変数が未設定です: %s", strings.Join(missing, ", "))
	}

	installationID, err := strconv.ParseInt(getenv("GITHUB_APP_INSTALLATION_ID"), 10, 64)
	if err != nil {
		return Config{}, fmt.Errorf("config: GITHUB_APP_INSTALLATION_ID は数値である必要があります: %w", err)
	}
	c.GitHubInstallationID = installationID

	c.SessionKey = []byte(getenv("SESSION_KEY"))
	if len(c.SessionKey) < 32 {
		return Config{}, errors.New("config: SESSION_KEY は32文字以上にしてください")
	}
	c.GitHubWebhookSecret = []byte(getenv("GITHUB_WEBHOOK_SECRET"))

	if key := getenv("GITHUB_APP_PRIVATE_KEY"); key != "" {
		c.GitHubPrivateKey = []byte(key)
	} else {
		raw, err := os.ReadFile(getenv("GITHUB_APP_PRIVATE_KEY_FILE"))
		if err != nil {
			return Config{}, fmt.Errorf("config: GitHub App 秘密鍵を読み込めません: %w", err)
		}
		c.GitHubPrivateKey = raw
	}

	c.SessionTTL = 12 * time.Hour
	if v := getenv("SESSION_TTL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return Config{}, fmt.Errorf("config: SESSION_TTL が不正です: %q", v)
		}
		c.SessionTTL = d
	}

	c.SubmitPerHour = 20
	if v := getenv("SUBMIT_LIMIT_PER_HOUR"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return Config{}, fmt.Errorf("config: SUBMIT_LIMIT_PER_HOUR が不正です: %q", v)
		}
		c.SubmitPerHour = n
	}

	// Cookies are Secure unless the operator explicitly opts out for local
	// HTTP development.
	c.AllowInsecureCookies = truthy(getenv("ALLOW_INSECURE_COOKIES"))
	c.CookieSecure = !c.AllowInsecureCookies
	if c.CookieSecure && !strings.HasPrefix(c.BaseURL, "https://") {
		return Config{}, errors.New("config: BASE_URL は https である必要があります (開発時は ALLOW_INSECURE_COOKIES=true)")
	}

	if c.DevSkipAuth {
		// A production deploy always sets CookieSecure=true (BASE_URL is https
		// unless ALLOW_INSECURE_COOKIES was explicitly set), so this refuses to
		// let DEV_SKIP_AUTH escape local development even if someone sets it by
		// mistake in a shared environment.
		if c.CookieSecure {
			return Config{}, errors.New("config: DEV_SKIP_AUTH は ALLOW_INSECURE_COOKIES=true のローカル開発でのみ使用できます")
		}
		c.DevUserDiscordID = orDefault(getenv("DEV_USER_DISCORD_ID"), c.BootstrapAdminID)
		c.DevUserName = orDefault(getenv("DEV_USER_NAME"), orDefault(c.BootstrapAdminName, "dev user"))
		if c.DevUserDiscordID == "" {
			return Config{}, errors.New("config: DEV_SKIP_AUTH を使うには DEV_USER_DISCORD_ID か BOOTSTRAP_ADMIN_DISCORD_ID が必要です")
		}
	}

	return c, nil
}

// RedirectURI is the OAuth2 callback registered with Discord.
func (c Config) RedirectURI() string { return c.BaseURL + "/auth/callback" }

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
