package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

func baseEnv() map[string]string {
	return map[string]string{
		"BASE_URL":                   "https://cms.example",
		"DISCORD_CLIENT_ID":          "cid",
		"DISCORD_CLIENT_SECRET":      "secret",
		"GITHUB_APP_ID":              "12345",
		"GITHUB_APP_INSTALLATION_ID": "67890",
		"GITHUB_APP_PRIVATE_KEY":     "-----BEGIN RSA PRIVATE KEY-----\nx\n-----END RSA PRIVATE KEY-----",
		"GITHUB_WEBHOOK_SECRET":      "webhook",
		"SESSION_KEY":                strings.Repeat("k", 32),
	}
}

func lookup(env map[string]string) Getenv {
	return func(key string) string { return env[key] }
}

func TestLoadDefaults(t *testing.T) {
	got, err := Load(lookup(baseEnv()))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Addr != ":8080" {
		t.Errorf("Addr = %q", got.Addr)
	}
	if got.GitHubOwner != "ieyoukan" || got.GitHubRepo != "PPLALE-web" || got.GitHubBaseBranch != "main" {
		t.Errorf("upstream defaults = %s/%s@%s", got.GitHubOwner, got.GitHubRepo, got.GitHubBaseBranch)
	}
	if got.RedirectURI() != "https://cms.example/auth/callback" {
		t.Errorf("RedirectURI = %q", got.RedirectURI())
	}
	if !got.CookieSecure {
		t.Error("cookies must default to Secure")
	}
	if got.SessionTTL != 12*time.Hour || got.SubmitPerHour != 20 {
		t.Errorf("TTL = %v, limit = %d", got.SessionTTL, got.SubmitPerHour)
	}
}

func TestLoadReportsEveryMissingVariableAtOnce(t *testing.T) {
	_, err := Load(lookup(map[string]string{}))
	if err == nil {
		t.Fatal("expected an error for an empty environment")
	}
	for _, name := range []string{"BASE_URL", "DISCORD_CLIENT_ID", "GITHUB_APP_ID", "SESSION_KEY", "GITHUB_WEBHOOK_SECRET"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error does not mention %s: %v", name, err)
		}
	}
}

func TestLoadRejectsWeakSessionKey(t *testing.T) {
	env := baseEnv()
	env["SESSION_KEY"] = "too-short"
	if _, err := Load(lookup(env)); err == nil {
		t.Fatal("a short session key was accepted")
	}
}

// Serving an admin UI over plain HTTP would send the session cookie in the
// clear, so it has to be an explicit opt in.
func TestLoadRequiresHTTPSUnlessExplicitlyRelaxed(t *testing.T) {
	env := baseEnv()
	env["BASE_URL"] = "http://localhost:8080"
	if _, err := Load(lookup(env)); err == nil {
		t.Fatal("an http base URL was accepted with secure cookies")
	}

	env["ALLOW_INSECURE_COOKIES"] = "true"
	got, err := Load(lookup(env))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.CookieSecure {
		t.Error("CookieSecure should be false when insecure cookies are allowed")
	}
}

func TestLoadRejectsMalformedNumbers(t *testing.T) {
	for name, value := range map[string]string{
		"GITHUB_APP_INSTALLATION_ID": "not-a-number",
		"SESSION_TTL":                "yesterday",
		"SUBMIT_LIMIT_PER_HOUR":      "-3",
	} {
		env := baseEnv()
		env[name] = value
		if _, err := Load(lookup(env)); err == nil {
			t.Errorf("%s=%q was accepted", name, value)
		}
	}
}

func TestLoadAcceptsPrivateKeyFromFile(t *testing.T) {
	env := baseEnv()
	delete(env, "GITHUB_APP_PRIVATE_KEY")
	env["GITHUB_APP_PRIVATE_KEY_FILE"] = writeTempKey(t)

	got, err := Load(lookup(env))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(string(got.GitHubPrivateKey), "PRIVATE KEY") {
		t.Errorf("private key = %q", got.GitHubPrivateKey)
	}

	env["GITHUB_APP_PRIVATE_KEY_FILE"] = "/nonexistent/key.pem"
	if _, err := Load(lookup(env)); err == nil {
		t.Error("a missing key file was accepted")
	}
}

func TestLoadRejectsDevSkipAuthWithSecureCookies(t *testing.T) {
	env := baseEnv()
	env["DEV_SKIP_AUTH"] = "true"
	env["BOOTSTRAP_ADMIN_DISCORD_ID"] = "123456789012345678"
	// CookieSecure defaults to true here (no ALLOW_INSECURE_COOKIES), which
	// must never coexist with an auth bypass.
	if _, err := Load(lookup(env)); err == nil {
		t.Fatal("DEV_SKIP_AUTH was accepted alongside secure cookies")
	}
}

func TestLoadDevSkipAuthRequiresInsecureCookiesAndADevUser(t *testing.T) {
	env := baseEnv()
	env["DEV_SKIP_AUTH"] = "true"
	env["ALLOW_INSECURE_COOKIES"] = "true"
	delete(env, "DISCORD_CLIENT_ID")
	delete(env, "DISCORD_CLIENT_SECRET")

	// No dev user identity available at all.
	if _, err := Load(lookup(env)); err == nil {
		t.Fatal("DEV_SKIP_AUTH was accepted with no DEV_USER_DISCORD_ID or BOOTSTRAP_ADMIN_DISCORD_ID")
	}

	env["BOOTSTRAP_ADMIN_DISCORD_ID"] = "123456789012345678"
	got, err := Load(lookup(env))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Discord credentials must not be required once login is bypassed.
	if got.DiscordClientID != "" {
		t.Errorf("DiscordClientID = %q, want empty", got.DiscordClientID)
	}
	if !got.DevSkipAuth || got.DevUserDiscordID != "123456789012345678" || got.DevUserName != "123456789012345678" {
		t.Errorf("dev skip auth config = %+v", got)
	}

	env["DEV_USER_DISCORD_ID"] = "999999999999999999"
	env["DEV_USER_NAME"] = "someone else"
	got, err = Load(lookup(env))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.DevUserDiscordID != "999999999999999999" || got.DevUserName != "someone else" {
		t.Errorf("DEV_USER_* did not override the bootstrap admin: %+v", got)
	}
}

func writeTempKey(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/key.pem"
	if err := os.WriteFile(path, []byte("-----BEGIN RSA PRIVATE KEY-----\nx\n-----END RSA PRIVATE KEY-----"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return path
}
