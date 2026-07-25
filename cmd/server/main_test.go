package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestServerIsConfiguredFromEnvironment(t *testing.T) {
	tokenPath := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenPath, []byte("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OMNISAVE_TOKEN_FILE", tokenPath)
	t.Setenv("OMNISAVE_DB_PATH", "/data/omnisave.db")
	t.Setenv("OMNISAVE_ARTIFACT_DIR", "/data/artifacts")
	t.Setenv("OMNISAVE_WEB_DIR", "/app/web")
	t.Setenv("OMNISAVE_HASHEOUS_TIMEOUT", "10s")
	t.Setenv("OMNISAVE_IGDB_REQUESTS_PER_SECOND", "3")

	config, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.Token != "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" || config.DBPath != "/data/omnisave.db" ||
		config.ArtifactDir != "/data/artifacts" || config.WebDir != "/app/web" || config.Hasheous.Timeout != "10s" ||
		config.IGDB.RequestsPerSecond != 3 {
		t.Fatal("environment configuration was not applied")
	}
}

func TestServerUsesDefaultsForOptionalEnvironment(t *testing.T) {
	t.Setenv("OMNISAVE_TOKEN", "abcdef0123456789abcdef0123456789")

	config, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.ListenAddr != ":8080" || config.DBPath != "./omnisave.db" || config.ArtifactDir != "./artifacts" ||
		config.WebDir != "./apps/dash/dist" || config.Hasheous.BaseURL != "https://hasheous.org" {
		t.Fatal("optional environment defaults were not applied")
	}
}

func TestServerRejectsAPlaceholderToken(t *testing.T) {
	t.Setenv("OMNISAVE_TOKEN", "replace-me")

	_, err := loadConfig()
	if err == nil || !strings.Contains(err.Error(), "at least 32 characters") {
		t.Fatalf("expected a short deployment token to be rejected, got %v", err)
	}
}

func TestServerRejectsAnInvalidIGDBRequestRate(t *testing.T) {
	t.Setenv("OMNISAVE_TOKEN", "abcdef0123456789abcdef0123456789")
	t.Setenv("OMNISAVE_IGDB_REQUESTS_PER_SECOND", "fast")

	_, err := loadConfig()
	if err == nil || !strings.Contains(err.Error(), "must be an integer") {
		t.Fatalf("expected a non-numeric request rate to be rejected, got %v", err)
	}

	for _, value := range []int{0, 5} {
		t.Run(strconv.Itoa(value), func(t *testing.T) {
			t.Setenv("OMNISAVE_IGDB_REQUESTS_PER_SECOND", strconv.Itoa(value))
			_, err := loadConfig()
			if err == nil || !strings.Contains(err.Error(), "between 1 and 4") {
				t.Fatalf("expected request rate %d to be rejected, got %v", value, err)
			}
		})
	}
}

func TestDashRoutesSurviveAReload(t *testing.T) {
	webDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<!doctype html>dash"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(webDir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "assets", "index.js"), []byte("export {}"), 0o600); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	registerHealthRoutes(mux)
	mux.Handle("/api/v1/", http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusTeapot)
	}))
	mux.Handle("/", dashHandler(webDir))

	get := func(path string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		return response
	}

	// A link to a game is a Dash route: the server has no such file and answers with the app.
	if response := get("/games/1b8a17bf"); response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), "dash") {
		t.Fatalf("expected a Dash route to serve the app, got %d %q", response.Code, response.Body.String())
	}
	if response := get("/"); response.Code != http.StatusOK {
		t.Fatalf("expected the Dash to be served at the root, got %d", response.Code)
	}
	if response := get("/assets/index.js"); response.Code != http.StatusOK ||
		response.Body.String() != "export {}" {
		t.Fatalf("expected the asset itself, got %d %q", response.Code, response.Body.String())
	}
	// A missing asset stays missing; answering with HTML would only confuse the browser.
	if response := get("/assets/gone.js"); response.Code != http.StatusNotFound {
		t.Fatalf("expected a missing asset to be a 404, got %d", response.Code)
	}
	// The API keeps its paths whether or not it has a route registered under them.
	if response := get("/api/v1/omnisaves"); response.Code != http.StatusTeapot {
		t.Fatalf("expected the API to answer its own path, got %d", response.Code)
	}
}

func TestHealthChecksDoNotRequireTheAPIToken(t *testing.T) {
	mux := http.NewServeMux()
	registerHealthRoutes(mux)

	for _, path := range []string{"/healthz", "/readyz"} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || response.Body.String() != "ok\n" {
			t.Fatalf("expected %s to report healthy, got %d %q", path, response.Code, response.Body.String())
		}
	}
}
