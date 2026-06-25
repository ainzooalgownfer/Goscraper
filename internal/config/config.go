// internal/config/config.go
package config

import (
	"fmt"
	"os"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	Server  ServerConfig  `yaml:"server"`
	Scraper ScraperConfig `yaml:"scraper"`
	Proxy   ProxyConfig   `yaml:"proxy"`
}

type ServerConfig struct {
	Port string `yaml:"port" env:"SERVER_PORT" env-default:"8080"`
}

type ScraperConfig struct {
	Delay       time.Duration `yaml:"delay" env:"SCRAPER_DELAY" env-default:"2s"`
	Parallelism int           `yaml:"parallelism" env:"SCRAPER_PARALLELISM" env-default:"4"`
}

type ProxyConfig struct {
	Enabled bool `yaml:"enabled" env:"PROXY_ENABLED" env-default:"false"`
}

// Load reads the config file and overrides it with environment variables if present
func Load(configPath string) (*Config, error) {
	var cfg Config

	
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("config file not found: %s", configPath)
	}

	
	err := cleanenv.ReadConfig(configPath, &cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	return &cfg, nil
}