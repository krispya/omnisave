// Package httpapi is the server's /api/v1 surface: it authenticates each
// request and adapts every domain service — saves, the Library, Devices,
// access, and settings — to HTTP, publishing change events as it goes.
package httpapi

import (
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/access"
	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/device"
	"github.com/krisbaumgartner/omnisave/internal/httpapi/contract"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	"github.com/krisbaumgartner/omnisave/internal/settings"
)

const (
	maxJSONBody     = 1 << 20
	maxRevisionBody = 64 << 20
)

type API struct {
	saves    omnisave.Service
	catalog  catalog.Service
	devices  device.Service
	access   access.Service
	settings settings.Service
	events   *eventBroker
}

// Config assembles the API. Saves is required; Catalog and Devices serve the
// Library routes together and Settings the owner settings, each only when
// supplied.
type Config struct {
	Saves    omnisave.Service
	Catalog  catalog.Service
	Devices  device.Service
	Settings settings.Service
}

// New creates the authenticated /api/v1 surface plus the credential-acquisition endpoints.
func New(credentials access.Service, config Config) http.Handler {
	api := &API{
		saves:    config.Saves,
		catalog:  config.Catalog,
		devices:  config.Devices,
		access:   credentials,
		settings: config.Settings,
		events:   newEventBroker(),
	}
	if api.devices != nil {
		// Expiry is a state change like any other: when a playing report ages
		// out, watchers hear devices.changed instead of running clocks of
		// their own against the server's.
		api.devices.OnPresenceExpired(api.publishDevicesChanged)
	}

	root := http.NewServeMux()
	root.Handle("/api/v1/", Authenticate(credentials, api.guardedRoutes()))
	root.HandleFunc("POST /api/v1/pairing/requests", api.requestPairing)
	root.HandleFunc("POST /api/v1/pairing/collect", api.collectPairing)
	// Claiming is unauthenticated but restricted to an unclaimed local server.
	root.HandleFunc("GET /api/v1/claim", api.claimStatus)
	root.HandleFunc("POST /api/v1/claim", api.claim)
	root.HandleFunc("POST /api/v1/session", api.signIn)
	return root
}

// guardedRoutes is every route that requires a credential.
func (api *API) guardedRoutes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/events", api.streamEvents)
	mux.HandleFunc("POST /api/v1/omnisaves", api.create)
	mux.HandleFunc("GET /api/v1/omnisaves", api.list)
	mux.HandleFunc("GET /api/v1/omnisaves/{id}", api.get)
	mux.HandleFunc("PATCH /api/v1/omnisaves/{id}", api.update)
	mux.HandleFunc("DELETE /api/v1/omnisaves/{id}", api.delete)
	mux.HandleFunc("PUT /api/v1/omnisaves/{id}/current-revision", api.restore)
	mux.HandleFunc("POST /api/v1/omnisaves/{id}/migrate-locations", api.migrateLocations)
	mux.HandleFunc("POST /api/v1/omnisaves/{id}/revisions", api.addRevision)
	mux.HandleFunc("GET /api/v1/omnisaves/{id}/revisions", api.listRevisions)
	mux.HandleFunc("GET /api/v1/omnisaves/{id}/revisions/{revisionID}", api.getRevision)
	mux.HandleFunc("PATCH /api/v1/omnisaves/{id}/revisions/{revisionID}", api.updateRevision)
	mux.HandleFunc("DELETE /api/v1/omnisaves/{id}/revisions/{revisionID}", api.deleteRevision)
	mux.HandleFunc("POST /api/v1/omnisaves/{id}/revisions/{revisionID}/label", api.labelRevision)
	mux.HandleFunc("POST /api/v1/omnisaves/{id}/achievements", api.recordAchievements)
	mux.HandleFunc("GET /api/v1/omnisaves/{id}/achievements", api.listAchievements)
	mux.HandleFunc("POST /api/v1/omnisaves/{id}/forks", api.fork)
	mux.HandleFunc("GET /api/v1/omnisaves/{id}/archive", api.archive)
	mux.HandleFunc("GET /api/v1/omnisaves/{id}/revisions/archive", api.archiveRevisions)
	mux.HandleFunc("GET /api/v1/omnisaves/{id}/revisions/{revisionID}/archive", api.archiveRevision)
	mux.HandleFunc("PUT /api/v1/artifacts/{sha256}", api.putArtifact)
	mux.HandleFunc("HEAD /api/v1/artifacts/{sha256}", api.headArtifact)
	mux.HandleFunc("GET /api/v1/artifacts/{sha256}", api.getArtifact)
	if api.catalog != nil && api.devices != nil {
		mux.HandleFunc("POST /api/v1/games/resolve", api.resolveGame)
		mux.HandleFunc("GET /api/v1/games", api.listGames)
		mux.HandleFunc("GET /api/v1/games/{id}/match-candidates", api.searchGameMatches)
		mux.HandleFunc("PUT /api/v1/games/{id}/match", api.matchGame)
		mux.HandleFunc("GET /api/v1/games/{id}", api.getGame)
		mux.HandleFunc("DELETE /api/v1/games/{id}", api.deleteGame)
		mux.HandleFunc("GET /api/v1/games/{id}/media/{mediaID}", api.getGameMedia)
		mux.HandleFunc("PUT /api/v1/devices/{id}", api.registerDevice)
		mux.HandleFunc("PUT /api/v1/devices/{id}/status", api.reportDeviceStatus)
		mux.HandleFunc("GET /api/v1/presence", api.listPresence)
		mux.HandleFunc("PUT /api/v1/games/{id}/tracking/{deviceID}", api.trackGame)
		mux.HandleFunc("DELETE /api/v1/games/{id}/tracking/{deviceID}", api.untrackGame)
	}
	// Deciding who may reach the server, and how the owner gets back in, is
	// the owner's alone: a Device credential syncs and uses the Library, and
	// cannot admit, revoke, or reconfigure anything, however valid it is.
	owner := func(pattern string, handler http.HandlerFunc) {
		mux.Handle(pattern, RequireOwner(handler))
	}
	owner("GET /api/v1/pairing/requests", api.listPairingRequests)
	owner("POST /api/v1/pairing/requests/{id}/approve", api.approvePairingRequest)
	owner("POST /api/v1/pairing/requests/{id}/deny", api.denyPairingRequest)
	owner("PUT /api/v1/pin", api.setPIN)
	owner("GET /api/v1/credentials", api.listCredentials)
	owner("DELETE /api/v1/credentials/{id}", api.revokeCredential)
	if api.settings != nil {
		owner("GET /api/v1/settings", api.listSettings)
		owner("PATCH /api/v1/settings/{key}", api.updateSetting)
	}
	// Only the owner token mints a credential out of nothing; an issued one
	// cannot use this to grow another.
	mux.Handle("POST /api/v1/credentials/exchange",
		RequireOwnerToken(http.HandlerFunc(api.exchangeOwnerToken)))
	return mux
}

func (a *API) create(w http.ResponseWriter, r *http.Request) {
	var input omnisave.CreateOmnisave
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, err)
		return
	}
	save, err := a.saves.Create(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	a.publishLibraryChanged()
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
		saves = []omnisave.Omnisave{}
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
	var input omnisave.UpdateOmnisave
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, err)
		return
	}
	save, err := a.saves.Update(r.Context(), r.PathValue("id"), input)
	if err != nil {
		writeError(w, err)
		return
	}
	a.publishLibraryChanged()
	writeJSON(w, http.StatusOK, save)
}

func (a *API) delete(w http.ResponseWriter, r *http.Request) {
	if err := a.saves.Delete(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	a.publishLibraryChanged()
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) restore(w http.ResponseWriter, r *http.Request) {
	var input omnisave.RestoreRevision
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, err)
		return
	}
	save, err := a.saves.Restore(r.Context(), r.PathValue("id"), input)
	if err != nil {
		writeError(w, err)
		return
	}
	a.publishLibraryChanged()
	writeJSON(w, http.StatusOK, save)
}

// migrateLocations renames one lineage's location vocabulary in place.
func (a *API) migrateLocations(w http.ResponseWriter, r *http.Request) {
	var input omnisave.MigrateLocations
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, err)
		return
	}
	result, err := a.saves.MigrateLocations(r.Context(), r.PathValue("id"), input)
	if err != nil {
		writeError(w, err)
		return
	}
	a.publishLibraryChanged()
	writeJSON(w, http.StatusOK, result)
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
	a.publishLibraryChanged()
	w.Header().Set("Location", "/api/v1/omnisaves/"+revision.OmnisaveID+"/revisions/"+revision.ID)
	writeJSON(w, http.StatusCreated, revision)
}

// recordAchievements accepts one Device's report of what it watched unlock.
// The response is what this report added, so a Device repeating itself can
// tell that nothing was new.
func (a *API) recordAchievements(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Achievements []omnisave.AchievementUnlock `json:"achievements"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, err)
		return
	}
	recorded, err := a.saves.RecordAchievements(r.Context(), r.PathValue("id"), input.Achievements)
	if err != nil {
		writeError(w, err)
		return
	}
	if recorded == nil {
		recorded = []omnisave.Achievement{}
	}
	// A report that told the server nothing it did not know moves nothing, so
	// it must not wake every watching Device into another pass.
	if len(recorded) > 0 {
		a.publishLibraryChanged()
	}
	writeJSON(w, http.StatusOK, map[string]any{"achievements": recorded})
}

func (a *API) listAchievements(w http.ResponseWriter, r *http.Request) {
	achievements, err := a.saves.ListAchievements(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if achievements == nil {
		achievements = []omnisave.Achievement{}
	}
	writeJSON(w, http.StatusOK, achievements)
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

func (a *API) updateRevision(w http.ResponseWriter, r *http.Request) {
	var input omnisave.UpdateRevision
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, err)
		return
	}
	revision, err := a.saves.UpdateRevision(
		r.Context(), r.PathValue("id"), r.PathValue("revisionID"), input,
	)
	if err != nil {
		writeError(w, err)
		return
	}
	a.publishLibraryChanged()
	writeJSON(w, http.StatusOK, revision)
}

func (a *API) labelRevision(w http.ResponseWriter, r *http.Request) {
	revision, err := a.saves.LabelRevision(
		r.Context(), r.PathValue("id"), r.PathValue("revisionID"),
	)
	if err != nil {
		writeError(w, err)
		return
	}
	a.publishLibraryChanged()
	writeJSON(w, http.StatusOK, revision)
}

func (a *API) deleteRevision(w http.ResponseWriter, r *http.Request) {
	if err := a.saves.DeleteRevision(r.Context(), r.PathValue("id"), r.PathValue("revisionID")); err != nil {
		writeError(w, err)
		return
	}
	a.publishLibraryChanged()
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) fork(w http.ResponseWriter, r *http.Request) {
	var input omnisave.ForkOmnisave
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, err)
		return
	}
	result, err := a.saves.Fork(r.Context(), r.PathValue("id"), input)
	if err != nil {
		writeError(w, err)
		return
	}
	a.publishLibraryChanged()
	w.Header().Set("Location", "/api/v1/omnisaves/"+result.Omnisave.ID)
	writeJSON(w, http.StatusCreated, result)
}

func (a *API) archive(w http.ResponseWriter, r *http.Request) {
	save, err := a.saves.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if save.CurrentRevisionID == nil {
		writeError(w, omnisave.ErrNotFound)
		return
	}
	revision, err := a.saves.GetRevision(r.Context(), save.ID, *save.CurrentRevisionID)
	if err != nil {
		writeError(w, err)
		return
	}
	a.serveArchive(w, r, []archivedRevision{{revision: *revision}}, archiveFilename(save.DisplayName))
}

func (a *API) archiveRevisions(w http.ResponseWriter, r *http.Request) {
	save, err := a.saves.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	revisions, err := a.saves.ListRevisions(r.Context(), save.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	if len(revisions) == 0 {
		writeError(w, omnisave.ErrNotFound)
		return
	}
	// The service's public contract does not promise an order. Oldest first
	// gives every snapshot a stable, readable directory even across branches.
	sort.Slice(revisions, func(i, j int) bool {
		if revisions[i].CreatedAt.Equal(revisions[j].CreatedAt) {
			return revisions[i].ID < revisions[j].ID
		}
		return revisions[i].CreatedAt.Before(revisions[j].CreatedAt)
	})
	archived := make([]archivedRevision, len(revisions))
	for index, revision := range revisions {
		archived[index] = archivedRevision{
			revision:  revision,
			directory: fmt.Sprintf("%04d %s", index+1, revision.CreatedAt.UTC().Format(archiveStampLayout)),
		}
	}
	a.serveArchive(w, r, archived, archiveFilename(save.DisplayName+" All Revisions"))
}

func (a *API) archiveRevision(w http.ResponseWriter, r *http.Request) {
	save, err := a.saves.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	revision, err := a.saves.GetRevision(r.Context(), save.ID, r.PathValue("revisionID"))
	if err != nil {
		writeError(w, err)
		return
	}
	// Downloads of several revisions of one save should not collide on
	// disk, so the snapshot's commit time joins the name.
	name := save.DisplayName + " " + revision.CreatedAt.UTC().Format(archiveStampLayout)
	a.serveArchive(w, r, []archivedRevision{{revision: *revision}}, archiveFilename(name))
}

type archivedRevision struct {
	revision  omnisave.Revision
	directory string
}

func (a *API) serveArchive(w http.ResponseWriter, r *http.Request, revisions []archivedRevision, filename string) {
	// Confirm every artifact before the first body byte; after that an
	// error can only truncate the stream.
	checkedArtifacts := make(map[string]struct{})
	for _, archived := range revisions {
		for _, file := range archived.revision.Files {
			if _, checked := checkedArtifacts[file.Artifact.SHA256]; checked {
				continue
			}
			if _, err := a.saves.StatArtifact(r.Context(), file.Artifact.SHA256); err != nil {
				writeError(w, err)
				return
			}
			checkedArtifacts[file.Artifact.SHA256] = struct{}{}
		}
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment",
		map[string]string{"filename": filename}))
	archive := zip.NewWriter(w)
	defer archive.Close()
	for _, archived := range revisions {
		if archived.directory != "" {
			if _, err := archive.CreateHeader(&zip.FileHeader{
				Name:     archived.directory + "/",
				Method:   zip.Store,
				Modified: archived.revision.CreatedAt,
			}); err != nil {
				return
			}
		}
		for _, file := range archived.revision.Files {
			path := file.Path
			if archived.directory != "" {
				path = archived.directory + "/" + path
			}
			if err := a.archiveFile(r.Context(), archive, archived.revision.CreatedAt, path, file); err != nil {
				return
			}
		}
	}
}

const archiveStampLayout = "2006-01-02 150405"

func (a *API) archiveFile(ctx context.Context, archive *zip.Writer, modified time.Time, path string, file omnisave.RevisionFile) error {
	payload, err := a.saves.OpenArtifact(ctx, file.Artifact.SHA256)
	if err != nil {
		return err
	}
	defer payload.Close()
	writer, err := archive.CreateHeader(&zip.FileHeader{
		Name:     path,
		Method:   zip.Deflate,
		Modified: modified,
	})
	if err != nil {
		return err
	}
	_, err = io.Copy(writer, payload)
	return err
}

func archiveFilename(displayName string) string {
	name := strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r < 0x20 {
			return -1
		}
		return r
	}, displayName))
	if name == "" {
		name = "omnisave"
	}
	return name + ".zip"
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
	// The artifact's identity is its uncompressed content. A compressed
	// upload carries the logical size in a header, since Content-Length
	// then measures the wire bytes instead of the claim to verify.
	size := r.ContentLength
	var payload io.Reader = r.Body
	if r.Header.Get("Content-Encoding") == "gzip" {
		declared, err := strconv.ParseInt(r.Header.Get("X-Omnisave-Size"), 10, 64)
		if err != nil || declared < 0 || declared > maxRevisionBody {
			writeError(w, omnisave.ErrInvalid)
			return
		}
		decompressor, err := gzip.NewReader(r.Body)
		if err != nil {
			writeError(w, omnisave.ErrInvalid)
			return
		}
		defer decompressor.Close()
		size = declared
		payload = io.LimitReader(decompressor, declared+1)
	}
	err := a.saves.StoreArtifact(r.Context(), omnisave.Artifact{
		Format: format,
		SHA256: r.PathValue("sha256"),
		Size:   size,
	}, payload)
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
	w.Header().Set("ETag", `"`+hash+`"`)
	// Compress the wire when the client accepts it; Go clients do by
	// default and decompress transparently, so save content — often very
	// compressible — travels small with no client-side handling.
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Set("Content-Encoding", "gzip")
		compressor := gzip.NewWriter(w)
		defer compressor.Close()
		if _, err := io.Copy(compressor, payload); err != nil {
			return
		}
		return
	}
	if size, statErr := a.saves.StatArtifact(r.Context(), hash); statErr == nil {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	}
	if _, err := io.Copy(w, payload); err != nil {
		return
	}
}

func (a *API) resolveGame(w http.ResponseWriter, r *http.Request) {
	var input catalog.Evidence
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, err)
		return
	}
	resolution, err := a.catalog.Resolve(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	a.publishLibraryChanged()
	writeJSON(w, http.StatusOK, catalogResolutionResponse{
		Game:   a.gameResponse(r.Context(), &resolution.Game),
		Status: resolution.Status,
	})
}

func (a *API) listGames(w http.ResponseWriter, r *http.Request) {
	games, err := a.catalog.List(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	response := make([]catalogGameResponse, len(games))
	for index := range games {
		response[index] = a.gameResponse(r.Context(), &games[index])
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
	a.publishLibraryChanged()
	writeJSON(w, http.StatusOK, a.gameResponse(r.Context(), game))
}

func (a *API) getGame(w http.ResponseWriter, r *http.Request) {
	game, err := a.catalog.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a.gameResponse(r.Context(), game))
}

func (a *API) deleteGame(w http.ResponseWriter, r *http.Request) {
	if err := a.catalog.Delete(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	a.publishLibraryChanged()
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) registerDevice(w http.ResponseWriter, r *http.Request) {
	var input device.Registration
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, err)
		return
	}
	registered, err := a.devices.Register(r.Context(), r.PathValue("id"), input)
	if err != nil {
		writeError(w, err)
		return
	}
	a.publishLibraryChanged()
	writeJSON(w, http.StatusOK, registered)
}

// reportDeviceStatus replaces a Device's expiring in-memory playing report.
func (a *API) reportDeviceStatus(w http.ResponseWriter, r *http.Request) {
	var input struct {
		PlayingGameIDs []string `json:"playing_game_ids"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, err)
		return
	}
	// Only a registered device gets a presence entry; anything else is a 404,
	// like every other /devices/{id} route.
	changed, err := a.devices.ReportPlaying(r.Context(), r.PathValue("id"), input.PlayingGameIDs)
	if err != nil {
		writeError(w, err)
		return
	}
	if changed {
		a.publishDevicesChanged()
	}
	w.WriteHeader(http.StatusNoContent)
}

// listPresence serves the whole live playing picture in one answer, so a
// presence change costs readers a light fetch instead of a library reload.
func (a *API) listPresence(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, struct {
		Devices []device.Presence `json:"devices"`
	}{Devices: a.devices.Presence()})
}

func (a *API) trackGame(w http.ResponseWriter, r *http.Request) {
	var input catalog.TrackGame
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, err)
		return
	}
	if err := a.catalog.TrackGame(r.Context(), r.PathValue("id"), r.PathValue("deviceID"), input); err != nil {
		writeError(w, err)
		return
	}
	a.publishLibraryChanged()
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) untrackGame(w http.ResponseWriter, r *http.Request) {
	if err := a.catalog.UntrackGame(r.Context(), r.PathValue("id"), r.PathValue("deviceID")); err != nil {
		writeError(w, err)
		return
	}
	a.publishLibraryChanged()
	w.WriteHeader(http.StatusNoContent)
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
	ID               string                    `json:"id"`
	Title            string                    `json:"title"`
	SortTitle        string                    `json:"sort_title,omitempty"`
	Platform         string                    `json:"platform,omitempty"`
	PlatformCompany  string                    `json:"platform_company,omitempty"`
	Publisher        string                    `json:"publisher,omitempty"`
	Description      string                    `json:"description,omitempty"`
	LabelerAvailable bool                      `json:"labeler_available"`
	MetadataSource   string                    `json:"metadata_source"`
	Identifiers      []catalog.GameIdentifier  `json:"identifiers"`
	Fingerprints     []catalog.GameFingerprint `json:"fingerprints"`
	Metadata         map[string]any            `json:"metadata,omitempty"`
	Media            []catalogMediaResponse    `json:"media"`
	Provenance       []provenanceResponse      `json:"provenance"`
	RefreshedAt      string                    `json:"refreshed_at"`
}

// provenanceResponse is one Provenance record as served: the durable record
// plus the Device's live presence, which the catalog never stores.
type provenanceResponse struct {
	catalog.GameTracking
	// Playing reports that the Device recently reported a live session of
	// this Game.
	Playing           bool       `json:"playing,omitempty"`
	PlayingReportedAt *time.Time `json:"playing_reported_at,omitempty"`
}

type catalogResolutionResponse struct {
	Game   catalogGameResponse      `json:"game"`
	Status catalog.ResolutionStatus `json:"status"`
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

func (a *API) gameResponse(ctx context.Context, game *catalog.Game) catalogGameResponse {
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
	// Presence is stitched in at serve time: playing is a live report with
	// a short credibility window, not a fact the catalog stores.
	provenance := make([]provenanceResponse, len(game.Provenance))
	for index, record := range game.Provenance {
		provenance[index] = provenanceResponse{GameTracking: record}
		if at, playing := a.devices.Playing(record.DeviceID, game.ID); playing {
			provenance[index].Playing = true
			provenance[index].PlayingReportedAt = &at
		}
	}
	return catalogGameResponse{
		ID:               game.ID,
		Title:            game.Title,
		SortTitle:        game.SortTitle,
		Platform:         game.Platform,
		PlatformCompany:  game.PlatformCompany,
		Publisher:        game.Publisher,
		Description:      game.Description,
		LabelerAvailable: a.saves.HasLabeler(ctx, game.ID),
		MetadataSource:   game.MetadataSource,
		Identifiers:      game.Identifiers,
		Fingerprints:     game.Fingerprints,
		Metadata:         game.Metadata,
		Media:            media,
		Provenance:       provenance,
		RefreshedAt:      game.RefreshedAt.Format(time.RFC3339Nano),
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

// writeError answers with the wire contract's encoding of err.
func writeError(w http.ResponseWriter, err error) {
	status, body := contract.EncodeError(err)
	if seconds, ok := body["retry_after"].(int); ok {
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
	}
	writeJSON(w, status, body)
}
