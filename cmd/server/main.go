package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/catalog/hasheous"
	"github.com/krisbaumgartner/omnisave/internal/catalog/igdb"
	catalogservice "github.com/krisbaumgartner/omnisave/internal/catalog/service"
	"github.com/krisbaumgartner/omnisave/internal/omnisave/httpapi"
	omnisaveservice "github.com/krisbaumgartner/omnisave/internal/omnisave/service"
	sqlitestorage "github.com/krisbaumgartner/omnisave/internal/storage/sqlite"
)

type Config struct {
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

func main() {
	configPath := "config/server.local.yaml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		log.Fatalf("read config: %v", err)
	}
	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		log.Fatalf("parse config: %v", err)
	}
	if config.Token == "" {
		log.Fatal("token must be set in config")
	}
	if config.ListenAddr == "" {
		config.ListenAddr = ":8080"
	}
	if config.DBPath == "" {
		config.DBPath = "./omnisave.db"
	}
	if config.ArtifactDir == "" {
		config.ArtifactDir = "./artifacts"
	}
	if config.WebDir == "" {
		config.WebDir = "./apps/dash/dist"
	}
	if config.Hasheous.BaseURL == "" {
		config.Hasheous.BaseURL = "https://hasheous.org"
	}
	hasheousTimeout := 15 * time.Second
	if config.Hasheous.Timeout != "" {
		hasheousTimeout, err = time.ParseDuration(config.Hasheous.Timeout)
		if err != nil || hasheousTimeout <= 0 {
			log.Fatal("hasheous timeout must be a positive duration")
		}
	}
	if value := os.Getenv("OMNISAVE_IGDB_CLIENT_ID"); value != "" {
		config.IGDB.ClientID = value
	}
	if value := os.Getenv("OMNISAVE_IGDB_CLIENT_SECRET"); value != "" {
		config.IGDB.ClientSecret = value
	}
	if (config.IGDB.ClientID == "") != (config.IGDB.ClientSecret == "") {
		log.Fatal("both IGDB client_id and client_secret must be set")
	}
	igdbTimeout := 15 * time.Second
	if config.IGDB.Timeout != "" {
		igdbTimeout, err = time.ParseDuration(config.IGDB.Timeout)
		if err != nil || igdbTimeout <= 0 {
			log.Fatal("IGDB timeout must be a positive duration")
		}
	}
	igdbCacheTTL := 5 * time.Minute
	if config.IGDB.SearchCacheTTL != "" {
		igdbCacheTTL, err = time.ParseDuration(config.IGDB.SearchCacheTTL)
		if err != nil || igdbCacheTTL < 0 {
			log.Fatal("IGDB search cache TTL must not be negative")
		}
	}

	repository, err := sqlitestorage.Open(config.DBPath, config.ArtifactDir)
	if err != nil {
		log.Fatalf("open storage: %v", err)
	}
	defer repository.Close()

	saves := omnisaveservice.New(repository)
	hasheousProvider := hasheous.New(config.Hasheous.BaseURL, &http.Client{Timeout: hasheousTimeout})
	resolutionProviders := []catalog.Provider{hasheousProvider}
	searchProviders := []catalog.Provider{hasheousProvider}
	if config.IGDB.ClientID != "" {
		igdbProvider, providerErr := igdb.New(igdb.Config{
			ClientID:          config.IGDB.ClientID,
			ClientSecret:      config.IGDB.ClientSecret,
			BaseURL:           config.IGDB.BaseURL,
			TokenURL:          config.IGDB.TokenURL,
			ImageBaseURL:      config.IGDB.ImageBaseURL,
			RequestsPerSecond: config.IGDB.RequestsPerSecond,
			SearchCacheTTL:    igdbCacheTTL,
		}, &http.Client{Timeout: igdbTimeout})
		if providerErr != nil {
			log.Fatalf("configure IGDB: %v", providerErr)
		}
		resolutionProviders = append(resolutionProviders, igdbProvider)
		searchProviders = []catalog.Provider{igdbProvider, hasheousProvider}
	}
	games := catalogservice.NewWithProviders(repository, repository, resolutionProviders, searchProviders)
	mux := http.NewServeMux()
	mux.Handle("/api/v1/", httpapi.BearerAuth(config.Token, httpapi.New(saves, games)))
	mux.Handle("/", http.FileServer(http.Dir(config.WebDir)))
	server := &http.Server{
		Addr:              config.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("OmniSave API listening on %s", config.ListenAddr)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("server: %v", err)
	}
}
