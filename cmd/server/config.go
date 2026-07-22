package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type serverConfig struct {
	ListenAddr  string `yaml:"listen_addr"`
	Token       string `yaml:"token"`
	DBPath      string `yaml:"db_path"`
	ArtifactDir string `yaml:"artifact_dir"`
	WebDir      string `yaml:"web_dir"`
	Hasheous    struct {
		BaseURL string `yaml:"base_url"`
		Timeout string `yaml:"timeout"`
	} `yaml:"hasheous"`
	IGDB struct {
		ClientID          string `yaml:"client_id"`
		ClientSecret      string `yaml:"client_secret"`
		BaseURL           string `yaml:"base_url"`
		TokenURL          string `yaml:"token_url"`
		ImageBaseURL      string `yaml:"image_base_url"`
		Timeout           string `yaml:"timeout"`
		RequestsPerSecond int    `yaml:"requests_per_second"`
		SearchCacheTTL    string `yaml:"search_cache_ttl"`
	} `yaml:"igdb"`
}

func loadConfig(arguments []string) (serverConfig, error) {
	if len(arguments) > 1 {
		return serverConfig{}, errors.New("usage: omnisave-server [config.yaml]")
	}
	config := defaultConfig()
	configPath := os.Getenv("OMNISAVE_CONFIG")
	if len(arguments) == 1 {
		configPath = arguments[0]
	}
	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return serverConfig{}, fmt.Errorf("read config: %w", err)
		}
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		decoder.KnownFields(true)
		if err := decoder.Decode(&config); err != nil {
			return serverConfig{}, fmt.Errorf("parse config: %w", err)
		}
	}

	applyEnvironment(&config)
	applyDefaults(&config)
	token, err := configuredToken(config.Token)
	if err != nil {
		return serverConfig{}, err
	}
	config.Token = token
	if config.Token == "" {
		return serverConfig{}, errors.New("token must be set with token, OMNISAVE_TOKEN, or OMNISAVE_TOKEN_FILE")
	}
	if len(config.Token) < 32 {
		return serverConfig{}, errors.New("token must contain at least 32 characters")
	}
	if (config.IGDB.ClientID == "") != (config.IGDB.ClientSecret == "") {
		return serverConfig{}, errors.New("both IGDB client_id and client_secret must be set")
	}
	return config, nil
}

func applyDefaults(config *serverConfig) {
	defaults := defaultConfig()
	if config.ListenAddr == "" {
		config.ListenAddr = defaults.ListenAddr
	}
	if config.DBPath == "" {
		config.DBPath = defaults.DBPath
	}
	if config.ArtifactDir == "" {
		config.ArtifactDir = defaults.ArtifactDir
	}
	if config.WebDir == "" {
		config.WebDir = defaults.WebDir
	}
	if config.Hasheous.BaseURL == "" {
		config.Hasheous.BaseURL = defaults.Hasheous.BaseURL
	}
}

func defaultConfig() serverConfig {
	config := serverConfig{
		ListenAddr:  ":8080",
		DBPath:      "./omnisave.db",
		ArtifactDir: "./artifacts",
		WebDir:      "./apps/dash/dist",
	}
	config.Hasheous.BaseURL = "https://hasheous.org"
	return config
}

func applyEnvironment(config *serverConfig) {
	setFromEnvironment(&config.ListenAddr, "OMNISAVE_LISTEN_ADDR")
	setFromEnvironment(&config.Token, "OMNISAVE_TOKEN")
	setFromEnvironment(&config.DBPath, "OMNISAVE_DB_PATH")
	setFromEnvironment(&config.ArtifactDir, "OMNISAVE_ARTIFACT_DIR")
	setFromEnvironment(&config.WebDir, "OMNISAVE_WEB_DIR")
	setFromEnvironment(&config.IGDB.ClientID, "OMNISAVE_IGDB_CLIENT_ID")
	setFromEnvironment(&config.IGDB.ClientSecret, "OMNISAVE_IGDB_CLIENT_SECRET")
}

func setFromEnvironment(destination *string, name string) {
	if value := os.Getenv(name); value != "" {
		*destination = value
	}
}

func configuredToken(fallback string) (string, error) {
	path := os.Getenv("OMNISAVE_TOKEN_FILE")
	if path == "" {
		return strings.TrimSpace(fallback), nil
	}
	if os.Getenv("OMNISAVE_TOKEN") != "" {
		return "", errors.New("set only one of OMNISAVE_TOKEN and OMNISAVE_TOKEN_FILE")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read token file: %w", err)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", errors.New("token file must not be empty")
	}
	return token, nil
}
