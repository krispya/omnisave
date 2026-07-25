package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
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
	if len(os.Args) > 1 {
		log.Fatal("usage: omnisave-server")
	}
	config, err := loadConfig()
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
	mux.Handle("/", dashHandler(config.WebDir))
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

// dashHandler serves the built Dash, answering its own routes with index.html so a
// link to one of them survives a reload. Only extensionless paths fall back: a missing
// asset must stay a 404 rather than become an HTML document with the wrong media type.
func dashHandler(webDir string) http.Handler {
	files := http.FileServer(http.Dir(webDir))
	index := filepath.Join(webDir, "index.html")

	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if dashRouteRequest(webDir, request) {
			http.ServeFile(response, request, index)
			return
		}
		files.ServeHTTP(response, request)
	})
}

func dashRouteRequest(webDir string, request *http.Request) bool {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		return false
	}
	path := path.Clean(request.URL.Path)
	if path == "/" || filepath.Ext(path) != "" {
		return false
	}
	// A path that names something on disk is that thing, not a route.
	_, err := os.Stat(filepath.Join(webDir, filepath.FromSlash(path)))
	return errors.Is(err, fs.ErrNotExist)
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
