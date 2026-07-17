// Package httpapi exposes the OmniSave service over HTTP.
package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

const (
	maxJSONBody     = 1 << 20
	maxRevisionBody = 64 << 20
)

type API struct {
	saves   omnisave.Service
	catalog catalog.Service
}

func New(saves omnisave.Service, catalogs ...catalog.Service) http.Handler {
	api := &API{saves: saves}
	if len(catalogs) > 0 {
		api.catalog = catalogs[0]
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/omnisaves", api.create)
	mux.HandleFunc("GET /api/v1/omnisaves", api.list)
	mux.HandleFunc("GET /api/v1/omnisaves/{id}", api.get)
	mux.HandleFunc("PATCH /api/v1/omnisaves/{id}", api.update)
	mux.HandleFunc("DELETE /api/v1/omnisaves/{id}", api.delete)
	mux.HandleFunc("POST /api/v1/omnisaves/{id}/revisions", api.addRevision)
	mux.HandleFunc("GET /api/v1/omnisaves/{id}/revisions", api.listRevisions)
	mux.HandleFunc("GET /api/v1/omnisaves/{id}/revisions/{revisionID}", api.getRevision)
	mux.HandleFunc("POST /api/v1/omnisaves/{id}/forks", api.fork)
	mux.HandleFunc("PUT /api/v1/artifacts/{sha256}", api.putArtifact)
	mux.HandleFunc("HEAD /api/v1/artifacts/{sha256}", api.headArtifact)
	mux.HandleFunc("GET /api/v1/artifacts/{sha256}", api.getArtifact)
	if api.catalog != nil {
		mux.HandleFunc("POST /api/v1/games/identify", api.identifyGame)
		mux.HandleFunc("GET /api/v1/games", api.listGames)
		mux.HandleFunc("GET /api/v1/games/{id}/match-candidates", api.searchGameMatches)
		mux.HandleFunc("PUT /api/v1/games/{id}/match", api.matchGame)
		mux.HandleFunc("GET /api/v1/games/{id}", api.getGame)
		mux.HandleFunc("GET /api/v1/games/{id}/media/{mediaID}", api.getGameMedia)
	}
	return mux
}

func (a *API) create(w http.ResponseWriter, r *http.Request) {
	var input omnisave.CreateOmniSave
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, err)
		return
	}
	save, err := a.saves.Create(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Location", "/api/v1/omnisaves/"+save.ID)
	writeJSON(w, http.StatusCreated, save)
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	saves, err := a.saves.List(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	if saves == nil {
		saves = []omnisave.OmniSave{}
	}
	writeJSON(w, http.StatusOK, saves)
}

func (a *API) get(w http.ResponseWriter, r *http.Request) {
	save, err := a.saves.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, save)
}

func (a *API) update(w http.ResponseWriter, r *http.Request) {
	var input omnisave.UpdateOmniSave
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, err)
		return
	}
	save, err := a.saves.Update(r.Context(), r.PathValue("id"), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, save)
}

func (a *API) delete(w http.ResponseWriter, r *http.Request) {
	if err := a.saves.Delete(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) addRevision(w http.ResponseWriter, r *http.Request) {
	var input omnisave.CreateRevision
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, err)
		return
	}
	revision, err := a.saves.CommitRevision(r.Context(), r.PathValue("id"), input)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Location", "/api/v1/omnisaves/"+revision.OmniSaveID+"/revisions/"+revision.ID)
	writeJSON(w, http.StatusCreated, revision)
}

func (a *API) getRevision(w http.ResponseWriter, r *http.Request) {
	revision, err := a.saves.GetRevision(r.Context(), r.PathValue("id"), r.PathValue("revisionID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, revision)
}

func (a *API) listRevisions(w http.ResponseWriter, r *http.Request) {
	revisions, err := a.saves.ListRevisions(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if revisions == nil {
		revisions = []omnisave.Revision{}
	}
	writeJSON(w, http.StatusOK, revisions)
}

func (a *API) fork(w http.ResponseWriter, r *http.Request) {
	var input omnisave.ForkOmniSave
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, err)
		return
	}
	result, err := a.saves.Fork(r.Context(), r.PathValue("id"), input)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Location", "/api/v1/omnisaves/"+result.OmniSave.ID)
	writeJSON(w, http.StatusCreated, result)
}

func (a *API) putArtifact(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength < 0 {
		writeError(w, omnisave.ErrInvalid)
		return
	}
	if r.ContentLength > maxRevisionBody {
		writeError(w, &http.MaxBytesError{Limit: maxRevisionBody})
		return
	}
	format := r.Header.Get("Content-Type")
	if format == "" {
		format = "application/octet-stream"
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRevisionBody)
	err := a.saves.StoreArtifact(r.Context(), omnisave.Artifact{
		Format: format,
		SHA256: r.PathValue("sha256"),
		Size:   r.ContentLength,
	}, r.Body)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("ETag", `"`+r.PathValue("sha256")+`"`)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) headArtifact(w http.ResponseWriter, r *http.Request) {
	size, err := a.saves.StatArtifact(r.Context(), r.PathValue("sha256"))
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("ETag", `"`+r.PathValue("sha256")+`"`)
	w.WriteHeader(http.StatusOK)
}

func (a *API) getArtifact(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("sha256")
	payload, err := a.saves.OpenArtifact(r.Context(), hash)
	if err != nil {
		writeError(w, err)
		return
	}
	defer payload.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	if size, statErr := a.saves.StatArtifact(r.Context(), hash); statErr == nil {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	}
	w.Header().Set("ETag", `"`+hash+`"`)
	if _, err := io.Copy(w, payload); err != nil {
		return
	}
}

func (a *API) identifyGame(w http.ResponseWriter, r *http.Request) {
	var input catalog.IdentifyGame
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, err)
		return
	}
	game, err := a.catalog.Identify(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, gameResponse(game))
}

func (a *API) listGames(w http.ResponseWriter, r *http.Request) {
	games, err := a.catalog.List(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	response := make([]catalogGameResponse, len(games))
	for index := range games {
		response[index] = gameResponse(&games[index])
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *API) searchGameMatches(w http.ResponseWriter, r *http.Request) {
	limit := 10
	if value := r.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			writeError(w, catalog.ErrInvalid)
			return
		}
		limit = parsed
	}
	candidates, err := a.catalog.Search(r.Context(), catalog.SearchGames{
		Title:    r.URL.Query().Get("q"),
		Platform: r.URL.Query().Get("platform"),
		Limit:    limit,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, candidates)
}

func (a *API) matchGame(w http.ResponseWriter, r *http.Request) {
	var input catalog.MatchGame
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, err)
		return
	}
	game, err := a.catalog.Match(r.Context(), r.PathValue("id"), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, gameResponse(game))
}

func (a *API) getGame(w http.ResponseWriter, r *http.Request) {
	game, err := a.catalog.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, gameResponse(game))
}

func (a *API) getGameMedia(w http.ResponseWriter, r *http.Request) {
	media, payload, err := a.catalog.OpenMedia(r.Context(), r.PathValue("id"), r.PathValue("mediaID"))
	if err != nil {
		writeError(w, err)
		return
	}
	defer payload.Close()
	w.Header().Set("Content-Type", media.Format)
	w.Header().Set("Content-Length", strconv.FormatInt(media.Size, 10))
	w.Header().Set("ETag", `"`+media.SHA256+`"`)
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	if _, err := io.Copy(w, payload); err != nil {
		return
	}
}

type catalogGameResponse struct {
	ID          string                 `json:"id"`
	Title       string                 `json:"title"`
	SortTitle   string                 `json:"sort_title,omitempty"`
	Platform    string                 `json:"platform,omitempty"`
	Publisher   string                 `json:"publisher,omitempty"`
	Description string                 `json:"description,omitempty"`
	Provider    string                 `json:"provider"`
	ProviderID  string                 `json:"provider_id"`
	Metadata    map[string]any         `json:"metadata,omitempty"`
	Media       []catalogMediaResponse `json:"media"`
	RefreshedAt string                 `json:"refreshed_at"`
}

type catalogMediaResponse struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Position    int    `json:"position"`
	Format      string `json:"format"`
	Size        int64  `json:"size"`
	URL         string `json:"url"`
	Attribution string `json:"attribution,omitempty"`
}

func gameResponse(game *catalog.Game) catalogGameResponse {
	media := make([]catalogMediaResponse, len(game.Media))
	for index, item := range game.Media {
		media[index] = catalogMediaResponse{
			ID:          item.ID,
			Kind:        item.Kind,
			Position:    item.Position,
			Format:      item.Format,
			Size:        item.Size,
			URL:         "/api/v1/games/" + url.PathEscape(game.ID) + "/media/" + url.PathEscape(item.ID),
			Attribution: item.Attribution,
		}
	}
	return catalogGameResponse{
		ID:          game.ID,
		Title:       game.Title,
		SortTitle:   game.SortTitle,
		Platform:    game.Platform,
		Publisher:   game.Publisher,
		Description: game.Description,
		Provider:    game.Provider,
		ProviderID:  game.ProviderID,
		Metadata:    game.Metadata,
		Media:       media,
		RefreshedAt: game.RefreshedAt.Format(time.RFC3339Nano),
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return omnisave.ErrInvalid
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return omnisave.ErrInvalid
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, err error) {
	var conflict *omnisave.HeadConflict
	if errors.As(err, &conflict) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":            "head_conflict",
			"status":           http.StatusConflict,
			"expected_head_id": conflict.ExpectedHeadID,
			"actual_head_id":   conflict.ActualHeadID,
		})
		return
	}
	var missing *omnisave.MissingArtifacts
	if errors.As(err, &missing) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error":          "artifact_missing",
			"status":         http.StatusUnprocessableEntity,
			"missing_sha256": missing.SHA256,
		})
		return
	}
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, omnisave.ErrInvalid), errors.Is(err, catalog.ErrInvalid):
		status = http.StatusBadRequest
	case errors.Is(err, omnisave.ErrNotFound), errors.Is(err, catalog.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, catalog.ErrUnavailable):
		status = http.StatusServiceUnavailable
	default:
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"error":  http.StatusText(status),
		"status": status,
	})
}
