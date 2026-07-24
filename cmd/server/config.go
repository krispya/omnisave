package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type serverConfig struct {
	ListenAddr  string
	Token       string
	DBPath      string
	ArtifactDir string
	WebDir      string
	Hasheous    struct {
		BaseURL string
		Timeout string
	}
	IGDB struct {
		ClientID          string
		ClientSecret      string
		BaseURL           string
		TokenURL          string
		ImageBaseURL      string
		Timeout           string
		RequestsPerSecond int
		SearchCacheTTL    string
	}
}

// loadConfig reads the server's complete runtime contract from the environment.
func loadConfig() (serverConfig, error) {
	config := defaultConfig()
	applyEnvironment(&config)
	requestsPerSecond := os.Getenv("OMNISAVE_IGDB_REQUESTS_PER_SECOND")
	if requestsPerSecond != "" {
		value, err := strconv.Atoi(requestsPerSecond)
		if err != nil {
			return serverConfig{}, errors.New("OMNISAVE_IGDB_REQUESTS_PER_SECOND must be an integer")
		}
		if value < 1 || value > 4 {
			return serverConfig{}, errors.New("OMNISAVE_IGDB_REQUESTS_PER_SECOND must be between 1 and 4")
		}
		config.IGDB.RequestsPerSecond = value
	}

	token, err := configuredToken()
	if err != nil {
		return serverConfig{}, err
	}
	config.Token = token
	if config.Token == "" {
		return serverConfig{}, errors.New("token must be set with OMNISAVE_TOKEN or OMNISAVE_TOKEN_FILE")
	}
	if len(config.Token) < 32 {
		return serverConfig{}, errors.New("token must contain at least 32 characters")
	}
	if (config.IGDB.ClientID == "") != (config.IGDB.ClientSecret == "") {
		return serverConfig{}, errors.New("both IGDB client_id and client_secret must be set")
	}
	return config, nil
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
	setFromEnvironment(&config.DBPath, "OMNISAVE_DB_PATH")
	setFromEnvironment(&config.ArtifactDir, "OMNISAVE_ARTIFACT_DIR")
	setFromEnvironment(&config.WebDir, "OMNISAVE_WEB_DIR")
	setFromEnvironment(&config.Hasheous.BaseURL, "OMNISAVE_HASHEOUS_BASE_URL")
	setFromEnvironment(&config.Hasheous.Timeout, "OMNISAVE_HASHEOUS_TIMEOUT")
	setFromEnvironment(&config.IGDB.ClientID, "OMNISAVE_IGDB_CLIENT_ID")
	setFromEnvironment(&config.IGDB.ClientSecret, "OMNISAVE_IGDB_CLIENT_SECRET")
	setFromEnvironment(&config.IGDB.BaseURL, "OMNISAVE_IGDB_BASE_URL")
	setFromEnvironment(&config.IGDB.TokenURL, "OMNISAVE_IGDB_TOKEN_URL")
	setFromEnvironment(&config.IGDB.ImageBaseURL, "OMNISAVE_IGDB_IMAGE_BASE_URL")
	setFromEnvironment(&config.IGDB.Timeout, "OMNISAVE_IGDB_TIMEOUT")
	setFromEnvironment(&config.IGDB.SearchCacheTTL, "OMNISAVE_IGDB_SEARCH_CACHE_TTL")
}

func setFromEnvironment(destination *string, name string) {
	if value := os.Getenv(name); value != "" {
		*destination = value
	}
}

func configuredToken() (string, error) {
	token := strings.TrimSpace(os.Getenv("OMNISAVE_TOKEN"))
	path := os.Getenv("OMNISAVE_TOKEN_FILE")
	if path == "" {
		return token, nil
	}
	if token != "" {
		return "", errors.New("set only one of OMNISAVE_TOKEN and OMNISAVE_TOKEN_FILE")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read token file: %w", err)
	}
	token = strings.TrimSpace(string(data))
	if token == "" {
		return "", errors.New("token file must not be empty")
	}
	return token, nil
}
