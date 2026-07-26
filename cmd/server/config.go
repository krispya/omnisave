package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/krisbaumgartner/omnisave/internal/settings"
)

// ownerTokenFile is where a generated owner token is kept, beside the database.
const ownerTokenFile = "owner-token"

type serverConfig struct {
	ListenAddr string
	Token      string
	// TokenPath is where a generated owner token lives, and is empty when the
	// deployment supplied one. TokenGenerated is true only on the start that
	// minted it — the one start that has to tell the operator what it is.
	TokenPath      string
	TokenGenerated bool
	// Name is what this server calls itself when it announces on the local
	// network (ADR-009). Two servers on one network are told apart by it, so
	// it defaults to the host's own name rather than to "Omnisave".
	Name        string
	DBPath      string
	ArtifactDir string
	WebDir      string
	// SettingsPins are owner settings the deployment has taken over. An
	// operator who pins one keeps a fleet identical; an owner who pins
	// nothing edits them in the Dash (ADR-008).
	SettingsPins map[string]string
	Hasheous     struct {
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
		// Nothing was configured, so the server mints its own rather than
		// refusing to start (ADR-010). The environment still wins: this only
		// runs when neither token variable is set.
		config.Token, config.TokenPath, config.TokenGenerated, err = ownerToken(config.DBPath)
		if err != nil {
			return serverConfig{}, err
		}
	}
	if len(config.Token) < 32 {
		return serverConfig{}, errors.New("token must contain at least 32 characters")
	}
	if (config.IGDB.ClientID == "") != (config.IGDB.ClientSecret == "") {
		return serverConfig{}, errors.New("both IGDB client_id and client_secret must be set")
	}

	pins, err := settings.ParsePins(os.LookupEnv)
	if err != nil {
		return serverConfig{}, err
	}
	config.SettingsPins = pins
	return config, nil
}

func defaultConfig() serverConfig {
	config := serverConfig{
		ListenAddr:  ":8080",
		Name:        defaultServerName(),
		DBPath:      "./omnisave.db",
		ArtifactDir: "./artifacts",
		WebDir:      "./apps/dash/dist",
	}
	config.Hasheous.BaseURL = "https://hasheous.org"
	return config
}

// defaultServerName is the host's own name, which is what an owner already
// calls the machine. A container with a hexadecimal host name gets the plain
// product name instead, since a random string helps nobody choose.
func defaultServerName() string {
	host, err := os.Hostname()
	if err != nil {
		return "Omnisave"
	}
	host = strings.TrimSuffix(strings.TrimSpace(host), ".local")
	if host == "" || isHexadecimal(host) {
		return "Omnisave"
	}
	return host
}

func isHexadecimal(value string) bool {
	for _, character := range value {
		if !strings.ContainsRune("0123456789abcdefABCDEF", character) {
			return false
		}
	}
	return len(value) >= 12
}

func applyEnvironment(config *serverConfig) {
	setFromEnvironment(&config.ListenAddr, "OMNISAVE_LISTEN_ADDR")
	setFromEnvironment(&config.Name, "OMNISAVE_NAME")
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

// ownerToken reads the token the server generated for itself, minting one on
// the first start that finds none (ADR-010).
//
// It lives beside the database rather than behind a variable of its own: it is
// state this deployment owns, like the database and the artifacts, and it
// belongs in the same place they get backed up from. It is never merged with
// the environment — a deployment that sets a token never reaches this.
func ownerToken(databasePath string) (token, path string, generated bool, err error) {
	path = filepath.Join(filepath.Dir(databasePath), ownerTokenFile)
	data, err := os.ReadFile(path)
	if err == nil {
		if token = strings.TrimSpace(string(data)); token != "" {
			return token, path, false, nil
		}
		// An empty file is a half-finished first start, not a token.
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", "", false, fmt.Errorf("read owner token: %w", err)
	}

	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", "", false, fmt.Errorf("generate owner token: %w", err)
	}
	token = hex.EncodeToString(buffer)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", "", false, fmt.Errorf("create data directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", "", false, fmt.Errorf("write owner token: %w", err)
	}
	return token, path, true, nil
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
