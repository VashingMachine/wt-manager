// Package configfile reads and writes ~/.wt-manager.json. What the settings
// mean, their defaults and their validation are rules of core; this package
// only moves them to and from disk.
package configfile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/VashingMachine/wt-manager/internal/core"
)

// Store is the config file on disk.
type Store struct{}

func New() *Store {
	return &Store{}
}

// Load reads the config file in homeDir and reports whether it existed. A
// missing file gives the default config and writes nothing; a file that
// needed normalizing is written back.
func (s *Store) Load(homeDir string) (core.AppConfig, string, bool, error) {
	return loadAppConfig(homeDir)
}

// Read reads and normalizes a config file without writing it back, so edits
// made outside the app can be picked up or reported.
func (s *Store) Read(configPath string) (core.AppConfig, error) {
	return readAppConfig(configPath)
}

// Write saves the config, readable by the user only.
func (s *Store) Write(configPath string, appConfig core.AppConfig) error {
	return writeAppConfig(configPath, appConfig)
}

func loadAppConfig(homeDir string) (core.AppConfig, string, bool, error) {
	configPath := core.DefaultConfigPath(homeDir)
	data, err := os.ReadFile(configPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return core.AppConfig{}, "", false, err
		}
		return core.DefaultAppConfig(), configPath, false, nil
	}

	var appConfig core.AppConfig
	if err := json.Unmarshal(data, &appConfig); err != nil {
		return core.AppConfig{}, "", true, fmt.Errorf("read %s: %w", configPath, err)
	}

	appConfig, changed, err := core.NormalizeAppConfig(appConfig)
	if err != nil {
		return core.AppConfig{}, "", true, err
	}
	if changed {
		if err := writeAppConfig(configPath, appConfig); err != nil {
			return core.AppConfig{}, "", true, err
		}
	}

	return appConfig, configPath, true, nil
}

// readAppConfig reads and normalizes a config file without writing it back.
func readAppConfig(configPath string) (core.AppConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return core.AppConfig{}, err
	}
	var appConfig core.AppConfig
	if err := json.Unmarshal(data, &appConfig); err != nil {
		return core.AppConfig{}, fmt.Errorf("read %s: %w", configPath, err)
	}
	appConfig, _, err = core.NormalizeAppConfig(appConfig)
	return appConfig, err
}

func writeAppConfig(configPath string, appConfig core.AppConfig) error {
	data, err := json.MarshalIndent(appConfig, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(configPath, data, 0o600)
}
