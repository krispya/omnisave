package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServerCanBeConfiguredFromEnvironmentWithoutAFile(t *testing.T) {
	tokenPath := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenPath, []byte("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OMNISAVE_TOKEN_FILE", tokenPath)
	t.Setenv("OMNISAVE_DB_PATH", "/data/omnisave.db")
	t.Setenv("OMNISAVE_ARTIFACT_DIR", "/data/artifacts")
	t.Setenv("OMNISAVE_WEB_DIR", "/app/web")

	config, err := loadConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if config.Token != "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" || config.DBPath != "/data/omnisave.db" ||
		config.ArtifactDir != "/data/artifacts" || config.WebDir != "/app/web" {
		t.Fatal("environment configuration was not applied")
	}
}

func TestEnvironmentOverridesTheDeploymentConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "server.yaml")
	contents := strings.Join([]string{
		"listen_addr: :9000",
		"token: 0123456789abcdef0123456789abcdef",
		"db_path: /configured/omnisave.db",
	}, "\n")
	if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OMNISAVE_TOKEN", "abcdef0123456789abcdef0123456789")
	t.Setenv("OMNISAVE_DB_PATH", "/environment/omnisave.db")

	config, err := loadConfig([]string{configPath})
	if err != nil {
		t.Fatal(err)
	}
	if config.Token != "abcdef0123456789abcdef0123456789" || config.DBPath != "/environment/omnisave.db" || config.ListenAddr != ":9000" {
		t.Fatal("environment values did not take precedence")
	}
}

func TestServerRejectsAPlaceholderToken(t *testing.T) {
	t.Setenv("OMNISAVE_TOKEN", "replace-me")

	_, err := loadConfig(nil)
	if err == nil || !strings.Contains(err.Error(), "at least 32 characters") {
		t.Fatalf("expected a short deployment token to be rejected, got %v", err)
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
