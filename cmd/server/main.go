package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/access"
	accessservice "github.com/krisbaumgartner/omnisave/internal/access/service"
	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/catalog/hasheous"
	"github.com/krisbaumgartner/omnisave/internal/catalog/igdb"
	catalogservice "github.com/krisbaumgartner/omnisave/internal/catalog/service"
	"github.com/krisbaumgartner/omnisave/internal/discovery"
	"github.com/krisbaumgartner/omnisave/internal/omnisave/httpapi"
	omnisaveservice "github.com/krisbaumgartner/omnisave/internal/omnisave/service"
	"github.com/krisbaumgartner/omnisave/internal/settings"
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

	repository, err := sqlitestorage.Open(config.DBPath, config.StoreDir)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer repository.Close()

	saves := omnisaveservice.New(repository)
	hasheousProvider := hasheous.New(config.Hasheous.BaseURL, &http.Client{Timeout: hasheousTimeout})
	// IGDB is registered whether or not it has credentials yet: the owner can
	// supply them later, and the catalog skips a provider that cannot answer
	// (ADR-011). Search prefers it because its titles and art are better;
	// resolution asks Hasheous first because it answers from a hash.
	igdbProvider := catalog.NewReloadable(igdb.ProviderName)
	games := catalogservice.NewWithProviders(repository, repository,
		[]catalog.Provider{hasheousProvider, igdbProvider},
		[]catalog.Provider{igdbProvider, hasheousProvider},
	)
	credentials := accessservice.New(repository, config.Token)
	ownerSettings := settings.New(repository, config.SettingsPins)

	igdbOptions := igdb.Config{
		BaseURL:           config.IGDB.BaseURL,
		TokenURL:          config.IGDB.TokenURL,
		ImageBaseURL:      config.IGDB.ImageBaseURL,
		RequestsPerSecond: config.IGDB.RequestsPerSecond,
		SearchCacheTTL:    igdbCacheTTL,
	}
	igdbClient := &http.Client{Timeout: igdbTimeout}
	reloadIGDB := func() {
		if err := loadIGDB(ctx, igdbProvider, ownerSettings, igdbOptions, igdbClient); err != nil {
			log.Printf("IGDB: %v", err)
		}
	}
	ownerSettings.OnChange(func(changed settings.Setting) {
		if changed.Key == settings.IGDBClientID || changed.Key == settings.IGDBClientSecret {
			reloadIGDB()
		}
	})
	reloadIGDB()

	announcer, err := startAnnouncing(ctx, config, ownerSettings)
	if err != nil {
		return err
	}
	defer announcer.Stop()

	mux := http.NewServeMux()
	registerHealthRoutes(mux)
	mux.Handle("/api/v1/", httpapi.New(credentials, httpapi.Config{
		Saves:    saves,
		Catalog:  games,
		Settings: ownerSettings,
	}))
	mux.Handle("/", dashHandler(config.WebDir))
	server := &http.Server{
		Addr:              config.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	reportOwnerToken(config)
	reportUnclaimed(ctx, credentials)
	log.Printf("Omnisave API listening on %s", config.ListenAddr)
	err = serveUntilStopped(ctx, server)
	if err != nil {
		return fmt.Errorf("server: %w", err)
	}
	return nil
}

// loadIGDB points the catalog's IGDB provider at whatever credentials the
// owner currently has, or turns it off when they have none. Credentials that
// do not work are the owner's to notice and fix, so a bad pair leaves the
// provider off rather than stopping the server.
func loadIGDB(
	ctx context.Context, provider *catalog.Reloadable, ownerSettings settings.Service,
	options igdb.Config, client *http.Client,
) error {
	options.ClientID = ownerSettings.Value(ctx, settings.IGDBClientID)
	options.ClientSecret = ownerSettings.Value(ctx, settings.IGDBClientSecret)
	if options.ClientID == "" || options.ClientSecret == "" {
		provider.Set(nil)
		return nil
	}
	configured, err := igdb.New(options, client)
	if err != nil {
		provider.Set(nil)
		return err
	}
	provider.Set(configured)
	log.Print("IGDB is configured")
	return nil
}

// reportOwnerToken tells the operator how to get in, on the one start where
// they cannot already know.
//
// A generated token is printed once, on the start that minted it, because a
// secret nobody can read is a server nobody can reach. Every later start
// prints only where it lives, and a deployment that configured its own token
// prints nothing at all (ADR-010).
func reportOwnerToken(config serverConfig) {
	switch {
	case config.TokenGenerated:
		log.Printf("Generated an owner token and saved it to %s:\n\n    %s\n\n"+
			"Enter it once in the Dash, which trades it for a credential of its own. "+
			"Set OMNISAVE_TOKEN to supply your own instead.", config.TokenPath, config.Token)
	case config.TokenPath != "":
		log.Printf("Using the owner token in %s", config.TokenPath)
	}
}

// reportUnclaimed says so while this server still belongs to nobody. An
// unclaimed server can be claimed by anyone who reaches it from the local
// network (ADR-010), which is the right trade for a home network and worth
// saying out loud on every start until someone takes it.
func reportUnclaimed(ctx context.Context, credentials access.Service) {
	claimable, err := credentials.Claimable(ctx)
	if err != nil || !claimable {
		return
	}
	log.Print("This server has no owner yet. Open the Dash from this network to claim it; " +
		"until then anyone who can reach it there can.")
}

// startAnnouncing puts the server on the local network when the owner's
// setting says to, and keeps following that setting for as long as the server
// runs: turning the switch off in the Dash stops the announcement rather than
// scheduling a restart (ADR-008, ADR-009).
func startAnnouncing(
	ctx context.Context, config serverConfig, ownerSettings settings.Service,
) (*discovery.Announcer, error) {
	port, err := listenPort(config.ListenAddr)
	if err != nil {
		return nil, err
	}
	announcer := discovery.NewAnnouncer(discovery.Announcement{Name: config.Name, Port: port})
	ownerSettings.OnChange(func(changed settings.Setting) {
		if changed.Key != settings.AnnounceDiscovery {
			return
		}
		if err := announcer.Set(changed.Value); err != nil {
			log.Printf("local network discovery: %v", err)
		}
	})
	if !ownerSettings.Enabled(ctx, settings.AnnounceDiscovery) {
		return announcer, nil
	}
	// A network that will not carry an announcement — a bridged container is
	// the ordinary case — is not a reason to refuse to serve. Say so once and
	// carry on; connecting with an address works either way.
	if err := announcer.Start(); err != nil {
		log.Printf("not announcing on the local network: %v", err)
		return announcer, nil
	}
	log.Printf("Announcing %q on the local network", config.Name)
	return announcer, nil
}

func listenPort(listenAddr string) (int, error) {
	_, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return 0, fmt.Errorf("listen address %q must carry a port", listenAddr)
	}
	value, err := strconv.Atoi(port)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("listen address %q must carry a numeric port", listenAddr)
	}
	return value, nil
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
