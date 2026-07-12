// Package httpapi exposes the OmniSave service over HTTP.
package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

const (
	maxJSONBody     = 1 << 20
	maxRevisionBody = 64 << 20
)

type API struct {
	saves omnisave.Service
}

func New(saves omnisave.Service) http.Handler {
	api := &API{saves: saves}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/omnisaves", api.create)
	mux.HandleFunc("GET /api/v1/omnisaves", api.list)
	mux.HandleFunc("GET /api/v1/omnisaves/{id}", api.get)
	mux.HandleFunc("POST /api/v1/omnisaves/{id}/revisions", api.addRevision)
	mux.HandleFunc("GET /api/v1/omnisaves/{id}/revisions", api.listRevisions)
	mux.HandleFunc("GET /api/v1/omnisaves/{id}/revisions/{revisionID}", api.getRevision)
	mux.HandleFunc("DELETE /api/v1/omnisaves/{id}/revisions/{revisionID}", api.deleteRevision)
	mux.HandleFunc("GET /api/v1/artifacts/{sha256}", api.getArtifact)
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

func (a *API) addRevision(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRevisionBody)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		writeError(w, err)
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}

	var input omnisave.CreateRevision
	decoder := json.NewDecoder(strings.NewReader(r.FormValue("revision")))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(w, omnisave.ErrInvalid)
		return
	}
	payload, _, err := r.FormFile("payload")
	if err != nil {
		writeError(w, omnisave.ErrInvalid)
		return
	}
	defer payload.Close()

	revision, err := a.saves.AddRevision(r.Context(), r.PathValue("id"), input, payload)
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

func (a *API) deleteRevision(w http.ResponseWriter, r *http.Request) {
	if err := a.saves.DeleteRevision(r.Context(), r.PathValue("id"), r.PathValue("revisionID")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
	w.Header().Set("ETag", `"`+hash+`"`)
	if _, err := io.Copy(w, payload); err != nil {
		return
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
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, omnisave.ErrInvalid):
		status = http.StatusBadRequest
	case errors.Is(err, omnisave.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, omnisave.ErrInUse):
		status = http.StatusConflict
	default:
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"error":  http.StatusText(status),
		"status": strconv.Itoa(status),
	})
}
