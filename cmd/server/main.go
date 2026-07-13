package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"gopkg.in/yaml.v3"

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
}

func main() {
	configPath := "server.yaml"
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
		config.WebDir = "./web/dist"
	}

	repository, err := sqlitestorage.Open(config.DBPath, config.ArtifactDir)
	if err != nil {
		log.Fatalf("open storage: %v", err)
	}
	defer repository.Close()

	saves := omnisaveservice.New(repository)
	mux := http.NewServeMux()
	mux.Handle("/api/v1/", httpapi.BearerAuth(config.Token, httpapi.New(saves)))
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
