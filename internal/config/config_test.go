package config

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/zalando/go-keyring"
)

// reset puts global state (viper + keyring + config dir) into a clean,
// isolated state for a single test.
func reset(t *testing.T) {
	t.Helper()
	viper.Reset()
	keyring.MockInit() // in-memory keychain
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OSF_TOKEN", "")
}

func TestLoadToken_FlagWins(t *testing.T) {
	reset(t)
	t.Setenv("OSF_TOKEN", "from-env")
	viper.Set("token", "from-config")
	_ = keyring.Set(keychainService, keychainUser, "from-keychain")

	if got := LoadToken("from-flag"); got != "from-flag" {
		t.Errorf("LoadToken = %q, want from-flag", got)
	}
}

func TestLoadToken_EnvBeatsConfigAndKeychain(t *testing.T) {
	reset(t)
	t.Setenv("OSF_TOKEN", "from-env")
	viper.Set("token", "from-config")
	_ = keyring.Set(keychainService, keychainUser, "from-keychain")

	if got := LoadToken(""); got != "from-env" {
		t.Errorf("LoadToken = %q, want from-env", got)
	}
}

func TestLoadToken_ConfigBeatsKeychain(t *testing.T) {
	reset(t)
	viper.Set("token", "from-config")
	_ = keyring.Set(keychainService, keychainUser, "from-keychain")

	if got := LoadToken(""); got != "from-config" {
		t.Errorf("LoadToken = %q, want from-config", got)
	}
}

func TestLoadToken_KeychainFallback(t *testing.T) {
	reset(t)
	_ = keyring.Set(keychainService, keychainUser, "from-keychain")

	if got := LoadToken(""); got != "from-keychain" {
		t.Errorf("LoadToken = %q, want from-keychain", got)
	}
}

func TestLoadToken_NoneReturnsEmpty(t *testing.T) {
	reset(t)
	if got := LoadToken(""); got != "" {
		t.Errorf("LoadToken = %q, want empty", got)
	}
}

func TestSaveToken_Keychain(t *testing.T) {
	reset(t)
	if err := SaveToken("stored-tok", false); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}
	got, err := keyring.Get(keychainService, keychainUser)
	if err != nil {
		t.Fatalf("keyring.Get: %v", err)
	}
	if got != "stored-tok" {
		t.Errorf("keychain token = %q, want stored-tok", got)
	}
}

func TestSaveToken_FileFallback(t *testing.T) {
	reset(t)
	if err := InitViper(); err != nil {
		t.Fatalf("InitViper: %v", err)
	}
	if err := SaveToken("file-tok", true); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}
	// noKeychain=true must not write to the keychain.
	if _, err := keyring.Get(keychainService, keychainUser); err == nil {
		t.Error("expected keychain to be empty when --no-keychain used")
	}
	// LoadToken should find it via the config file.
	viper.Reset()
	if err := InitViper(); err != nil {
		t.Fatalf("InitViper reload: %v", err)
	}
	if got := LoadToken(""); got != "file-tok" {
		t.Errorf("LoadToken after file save = %q, want file-tok", got)
	}
}

func TestDeleteToken_ClearsKeychainAndFile(t *testing.T) {
	reset(t)
	if err := InitViper(); err != nil {
		t.Fatalf("InitViper: %v", err)
	}
	_ = keyring.Set(keychainService, keychainUser, "from-keychain")

	if err := DeleteToken(); err != nil {
		t.Fatalf("DeleteToken: %v", err)
	}
	if _, err := keyring.Get(keychainService, keychainUser); err == nil {
		t.Error("expected keychain token to be deleted")
	}
	if got := LoadToken(""); got != "" {
		t.Errorf("LoadToken after delete = %q, want empty", got)
	}
}

func TestTokenSource(t *testing.T) {
	reset(t)
	if got := TokenSource("flag-val"); got != "--token flag" {
		t.Errorf("flag source = %q", got)
	}

	reset(t)
	t.Setenv("OSF_TOKEN", "x")
	if got := TokenSource(""); got != "OSF_TOKEN environment variable" {
		t.Errorf("env source = %q", got)
	}

	reset(t)
	viper.Set("token", "x")
	if got := TokenSource(""); got != "config file" {
		t.Errorf("config source = %q", got)
	}

	reset(t)
	_ = keyring.Set(keychainService, keychainUser, "x")
	if got := TokenSource(""); got != "OS keychain" {
		t.Errorf("keychain source = %q", got)
	}

	reset(t)
	if got := TokenSource(""); got != "" {
		t.Errorf("empty source = %q, want empty", got)
	}
}
