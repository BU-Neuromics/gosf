package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
	"github.com/zalando/go-keyring"
)

const (
	keychainService = "gosf"
	keychainUser    = "token"
)

// ConfigDir returns the gosf config directory path (~/.config/gosf).
func ConfigDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "gosf"), nil
}

func configFilePath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// InitViper configures viper to read from ~/.config/gosf/config.toml.
// Missing config file is not an error.
func InitViper() error {
	dir, err := ConfigDir()
	if err != nil {
		return fmt.Errorf("resolving config dir: %w", err)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	viper.SetConfigName("config")
	viper.SetConfigType("toml")
	viper.AddConfigPath(dir)
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("reading config: %w", err)
		}
	}
	return nil
}

// LoadToken returns the token using the priority chain:
// flagToken > OSF_TOKEN env > config file > OS keychain.
// Returns empty string if none found (unauthenticated mode).
func LoadToken(flagToken string) string {
	if flagToken != "" {
		return flagToken
	}
	if t := os.Getenv("OSF_TOKEN"); t != "" {
		return t
	}
	if t := viper.GetString("token"); t != "" {
		return t
	}
	t, err := keyring.Get(keychainService, keychainUser)
	if err == nil {
		return t
	}
	return ""
}

// SaveToken stores the token. It tries the OS keychain first unless
// noKeychain is true, falling back to the plaintext config file.
func SaveToken(token string, noKeychain bool) error {
	if !noKeychain {
		if err := keyring.Set(keychainService, keychainUser, token); err == nil {
			return nil
		}
		// Keychain unavailable (headless/HPC) — fall through to file
	}
	return writeTokenToFile(token)
}

// DeleteToken removes the token from both the keychain and the config file.
func DeleteToken() error {
	_ = keyring.Delete(keychainService, keychainUser)
	return writeTokenToFile("")
}

func writeTokenToFile(token string) error {
	p, err := configFilePath()
	if err != nil {
		return err
	}
	viper.Set("token", token)
	return viper.WriteConfigAs(p)
}

// TokenSource returns a human-readable description of where the active
// token came from, for display in `auth status`.
func TokenSource(flagToken string) string {
	if flagToken != "" {
		return "--token flag"
	}
	if os.Getenv("OSF_TOKEN") != "" {
		return "OSF_TOKEN environment variable"
	}
	if viper.GetString("token") != "" {
		return "config file"
	}
	if _, err := keyring.Get(keychainService, keychainUser); err == nil {
		return "OS keychain"
	}
	return ""
}
