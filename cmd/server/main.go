package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/catalog/hasheous"
	"github.com/krisbaumgartner/omnisave/internal/catalog/igdb"
	catalogservice "github.com/krisbaumgartner/omnisave/internal/catalog/service"
	"github.com/krisbaumgartner/omnisave/internal/omnisave/httpapi"
	omnisaveservice "github.com/krisbaumgartner/omnisave/internal/omnisave/service"
	sqlitestorage "github.com/krisbaumgartner/omnisave/internal/storage/sqlite"
)

func main() {
	config, err := loadConfig(os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runServer(ctx, config); err != nil {
		log.Fatal(err)
	}
}

func runServer(ctx context.Context, config serverConfig) error {
	hasheousTimeout := 15 * time.Second
	if config.Hasheous.Timeout != "" {
		var err error
		hasheousTimeout, err = time.ParseDuration(config.Hasheous.Timeout)
		if err != nil || hasheousTimeout <= 0 {
			return errors.New("hasheous timeout must be a positive duration")
		}
	}
	igdbTimeout := 15 * time.Second
	if config.IGDB.Timeout != "" {
		var err error
		igdbTimeout, err = time.ParseDuration(config.IGDB.Timeout)
		if err != nil || igdbTimeout <= 0 {
			return errors.New("IGDB timeout must be a positive duration")
		}
	}
	igdbCacheTTL := 5 * time.Minute
	if config.IGDB.SearchCacheTTL != "" {
		var err error
		igdbCacheTTL, err = time.ParseDuration(config.IGDB.SearchCacheTTL)
		if err != nil || igdbCacheTTL < 0 {
			return errors.New("IGDB search cache TTL must not be negative")
		}
	}

	repository, err := sqlitestorage.Open(config.DBPath, config.ArtifactDir)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
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
			return fmt.Errorf("configure IGDB: %w", providerErr)
		}
		resolutionProviders = append(resolutionProviders, igdbProvider)
		searchProviders = []catalog.Provider{igdbProvider, hasheousProvider}
	}
	games := catalogservice.NewWithProviders(repository, repository, resolutionProviders, searchProviders)
	mux := http.NewServeMux()
	registerHealthRoutes(mux)
	mux.Handle("/api/v1/", httpapi.BearerAuth(config.Token, httpapi.New(saves, games)))
	mux.Handle("/", http.FileServer(http.Dir(config.WebDir)))
	server := &http.Server{
		Addr:              config.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("Omnisave API listening on %s", config.ListenAddr)
	err = serveUntilStopped(ctx, server)
	if err != nil {
		return fmt.Errorf("server: %w", err)
	}
	return nil
}

func registerHealthRoutes(mux *http.ServeMux) {
	health := func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte("ok\n"))
	}
	mux.HandleFunc("GET /healthz", health)
	mux.HandleFunc("GET /readyz", health)
}

func serveUntilStopped(ctx context.Context, server *http.Server) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return err
		}
		err := <-errCh
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
