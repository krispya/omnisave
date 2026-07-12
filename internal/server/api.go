package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/shared"
)

// API holds dependencies for HTTP handlers.
type API struct {
	DB      *DB
	Storage *Storage
}

// RegisterRoutes attaches all routes to mux behind auth middleware.
func (a *API) RegisterRoutes(mux *http.ServeMux, token string) {
	auth := BearerAuth(token)

	mux.Handle("GET /health", auth(http.HandlerFunc(a.handleHealth)))
	mux.Handle("GET /systems", auth(http.HandlerFunc(a.handleListSystems)))
	mux.Handle("GET /systems/{system}/games", auth(http.HandlerFunc(a.handleListGames)))
	mux.Handle("GET /games/{system}/{game}/versions", auth(http.HandlerFunc(a.handleListVersions)))
	mux.Handle("GET /games/{system}/{game}/active", auth(http.HandlerFunc(a.handleGetActive)))
	mux.Handle("POST /games/{system}/{game}/push", auth(http.HandlerFunc(a.handlePush)))
	mux.Handle("GET /games/{system}/{game}/version/{sha256}", auth(http.HandlerFunc(a.handleGetVersion)))
	mux.Handle("PUT /games/{system}/{game}/active/{sha256}", auth(http.HandlerFunc(a.handleSetActive)))
}

func (a *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (a *API) handleListSystems(w http.ResponseWriter, r *http.Request) {
	systems, err := a.DB.ListSystems()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	entries := make([]shared.SystemEntry, len(systems))
	for i, s := range systems {
		entries[i] = shared.SystemEntry{System: s}
	}
	writeJSON(w, shared.ListSystemsResponse{Systems: entries})
}

func (a *API) handleListGames(w http.ResponseWriter, r *http.Request) {
	system := r.PathValue("system")
	games, err := a.DB.ListGames(system)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	entries := make([]shared.GameEntry, len(games))
	for i, g := range games {
		entries[i] = shared.GameEntry{System: system, Game: g}
	}
	writeJSON(w, shared.ListGamesResponse{Games: entries})
}

func (a *API) handleListVersions(w http.ResponseWriter, r *http.Request) {
	system := r.PathValue("system")
	game := r.PathValue("game")
	versions, err := a.DB.ListVersions(system, game)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if versions == nil {
		versions = []shared.VersionInfo{}
	}
	writeJSON(w, shared.ListVersionsResponse{Versions: versions})
}

func (a *API) handleGetActive(w http.ResponseWriter, r *http.Request) {
	system := r.PathValue("system")
	game := r.PathValue("game")
	v, err := a.DB.GetActiveVersion(system, game)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if v == nil {
		http.Error(w, "no active version", http.StatusNotFound)
		return
	}
	path := a.Storage.ActivePath(system, game, v.SHA256)
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-SHA256", v.SHA256)
	io.Copy(w, f)
}

func (a *API) handlePush(w http.ResponseWriter, r *http.Request) {
	system := r.PathValue("system")
	game := r.PathValue("game")

	if err := r.ParseMultipartForm(64 << 20); err != nil {
		http.Error(w, fmt.Sprintf("parse multipart: %v", err), http.StatusBadRequest)
		return
	}

	clientID := r.FormValue("client_id")
	parentSHA256 := r.FormValue("parent_sha256")

	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, fmt.Sprintf("get file: %v", err), http.StatusBadRequest)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "read file", http.StatusInternalServerError)
		return
	}

	sha256 := shared.HashBytes(data)

	if _, err := a.Storage.Store(system, game, data, sha256); err != nil {
		log.Printf("storage error: %v", err)
		http.Error(w, "store file", http.StatusInternalServerError)
		return
	}

	isActive, conflict, err := a.DB.AddVersion(system, game, sha256, clientID, parentSHA256, time.Now())
	if err != nil {
		log.Printf("db error: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, shared.PushResponse{
		SHA256:   sha256,
		IsActive: isActive,
		Conflict: conflict,
	})
}

func (a *API) handleGetVersion(w http.ResponseWriter, r *http.Request) {
	system := r.PathValue("system")
	game := r.PathValue("game")
	sha256 := r.PathValue("sha256")
	path := a.Storage.ActivePath(system, game, sha256)
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "version not found", http.StatusNotFound)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-SHA256", sha256)
	io.Copy(w, f)
}

func (a *API) handleSetActive(w http.ResponseWriter, r *http.Request) {
	system := r.PathValue("system")
	game := r.PathValue("game")
	sha256 := r.PathValue("sha256")
	if err := a.DB.SetActive(system, game, sha256); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON: %v", err)
	}
}
