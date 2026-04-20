package config

import (
	"os"
	"path/filepath"
	yaml "gopkg.in/yaml.v2"
)

const (
	configDirName  = ".firesh"
	configFileName = "config.yaml"
)

type Config struct {
	DefaultProjectID  string `yaml:"default_project_id"`
	DefaultDatabaseID string `yaml:"default_database_id"`
	OutputFormat      string `yaml:"output_format"`
}

// NewConfig returns a Config with default values.
func NewConfig() *Config {
	return &Config{
		DefaultProjectID:  "",
		DefaultDatabaseID: "(default)",
		OutputFormat:      "table",
	}
}

func LoadConfig() (*Config, error) {
	configPath, err := getConfigFilePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return NewConfig(), nil // Return default config if file doesn't exist
		}
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// Update updates the Config with non-empty values from another Config. Saves to disk if any changes were made.
func (c *Config) Update(newConfig *Config) error {
	updated := false
	if newConfig.DefaultProjectID != "" && newConfig.DefaultProjectID != c.DefaultProjectID {
		c.DefaultProjectID = newConfig.DefaultProjectID
		updated = true
	}
	if newConfig.DefaultDatabaseID != "" && newConfig.DefaultDatabaseID != c.DefaultDatabaseID {
		c.DefaultDatabaseID = newConfig.DefaultDatabaseID
		updated = true
	}
	if newConfig.OutputFormat != "" && newConfig.OutputFormat != c.OutputFormat {
		c.OutputFormat = newConfig.OutputFormat
		updated = true
	}

	if updated {
		return saveConfig(c)
	}

	return nil
}

func saveConfig(c *Config) error {
	configPath, err := getConfigFilePath()
	if err != nil {
		return err
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0600)
}


func getConfigFilePath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	configDir := filepath.Join(homeDir, configDirName)
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return "", err
	}
	return filepath.Join(configDir, configFileName), nil
}