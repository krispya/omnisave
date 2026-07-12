package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os"

	"golang.org/x/crypto/acme/autocert"
	"gopkg.in/yaml.v3"

	"github.com/krisbaumgartner/omnisave/internal/server"
)

type Config struct {
	RootDir  string `yaml:"root_dir"`
	Token    string `yaml:"token"`
	Domain   string `yaml:"domain"`
	CacheDir string `yaml:"cache_dir"`
	DBPath   string `yaml:"db_path"`
}

func main() {
	cfgPath := "server.yaml"
	if len(os.Args) > 1 {
		cfgPath = os.Args[1]
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		log.Fatalf("read config: %v", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		log.Fatalf("parse config: %v", err)
	}
	if cfg.Token == "" {
		log.Fatal("token must be set in config")
	}
	if cfg.RootDir == "" {
		cfg.RootDir = "./saves"
	}
	if cfg.DBPath == "" {
		cfg.DBPath = "./omnisave.db"
	}

	storage, err := server.NewStorage(cfg.RootDir)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}

	db, err := server.NewDB(cfg.DBPath)
	if err != nil {
		log.Fatalf("db: %v", err)
	}

	api := &server.API{DB: db, Storage: storage}
	mux := http.NewServeMux()
	api.RegisterRoutes(mux, cfg.Token)

	if cfg.Domain == "" {
		addr := ":8080"
		log.Printf("Starting plain HTTP server on %s (no TLS)", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Fatalf("server: %v", err)
		}
		return
	}

	// HTTPS with Let's Encrypt autocert.
	// NOTE: The server must be reachable on port 443 from the internet
	// for the ACME HTTP-01 challenge to succeed.
	if cfg.CacheDir == "" {
		cfg.CacheDir = "./certs"
	}
	m := &autocert.Manager{
		Cache:      autocert.DirCache(cfg.CacheDir),
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(cfg.Domain),
	}

	// Port 80: handle ACME challenges + redirect to HTTPS.
	go func() {
		log.Printf("Starting HTTP redirect server on :80")
		if err := http.ListenAndServe(":80", m.HTTPHandler(nil)); err != nil {
			log.Printf("HTTP redirect server: %v", err)
		}
	}()

	tlsCfg := &tls.Config{GetCertificate: m.GetCertificate}
	srv := &http.Server{
		Addr:      ":443",
		TLSConfig: tlsCfg,
		Handler:   mux,
	}
	log.Printf("Starting HTTPS server on :443 for domain %s", cfg.Domain)
	if err := srv.ListenAndServeTLS("", ""); err != nil {
		fmt.Fprintf(os.Stderr, "server: %v\n", err)
		os.Exit(1)
	}
}
