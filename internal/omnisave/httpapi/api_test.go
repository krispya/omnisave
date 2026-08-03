package httpapi_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	accessservice "github.com/krisbaumgartner/omnisave/internal/access/service"
	"github.com/krisbaumgartner/omnisave/internal/catalog"
	catalogservice "github.com/krisbaumgartner/omnisave/internal/catalog/service"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	"github.com/krisbaumgartner/omnisave/internal/omnisave/httpapi"
	omnisaveservice "github.com/krisbaumgartner/omnisave/internal/omnisave/service"
	"github.com/krisbaumgartner/omnisave/internal/storage/storagetest"
)

func TestNetworkClientStory(t *testing.T) {
	handler := newHandler(t, storagetest.NewMemoryRepository())

	createBody := bytes.NewBufferString(`{"game_id":"pokemon-emerald-usa"}`)
	response := request(t, handler, http.MethodPost, "/api/v1/omnisaves", "application/json", createBody)
	if response.Code != http.StatusCreated {
		t.Fatalf("create returned %d: %s", response.Code, response.Body.String())
	}
	var save omnisave.Omnisave
	decodeResponse(t, response, &save)
	missingBody := bytes.NewBufferString(`{
		"expected_head_id":null,
		"upserts":[{"path":"missing.sav","artifact":{
			"format":"application/octet-stream",
			"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"size":10
		}}]
	}`)
	response = request(t, handler, http.MethodPost,
		"/api/v1/omnisaves/"+save.ID+"/revisions", "application/json", missingBody)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "artifact_missing") {
		t.Fatalf("missing artifact returned %d: %s", response.Code, response.Body.String())
	}

	progress := uploadArtifact(t, handler, "game-save contents")
	settings := uploadArtifact(t, handler, "shared settings")
	revisionBody, err := json.Marshal(omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{
			{Path: "pokemon.sav", Artifact: progress},
			{Path: "settings.json", Artifact: settings},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	response = request(t, handler, http.MethodPost,
		"/api/v1/omnisaves/"+save.ID+"/revisions", "application/json", bytes.NewReader(revisionBody))
	if response.Code != http.StatusCreated {
		t.Fatalf("add revision returned %d: %s", response.Code, response.Body.String())
	}
	var revision omnisave.Revision
	decodeResponse(t, response, &revision)

	response = request(t, handler, http.MethodGet,
		"/api/v1/omnisaves/"+save.ID+"/revisions", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list revisions returned %d: %s", response.Code, response.Body.String())
	}
	var history []omnisave.Revision
	decodeResponse(t, response, &history)
	if len(history) != 1 || history[0].ID != revision.ID {
		t.Fatalf("unexpected history: %v", history)
	}
	if history[0].ParentID != nil {
		t.Fatal("an initial revision should not have a parent")
	}
	if len(history[0].Files) != 2 {
		t.Fatalf("expected a complete manifest, got %v", history[0].Files)
	}

	response = request(t, handler, http.MethodPatch,
		"/api/v1/omnisaves/"+save.ID+"/revisions/"+revision.ID,
		"application/json", bytes.NewBufferString(`{"display_name":"Before the Elite Four"}`))
	if response.Code != http.StatusOK {
		t.Fatalf("rename revision returned %d: %s", response.Code, response.Body.String())
	}
	var renamed omnisave.Revision
	decodeResponse(t, response, &renamed)
	if renamed.DisplayName != "Before the Elite Four" || renamed.ID != revision.ID || len(renamed.Files) != 2 {
		t.Fatalf("unexpected renamed revision: %+v", renamed)
	}

	response = request(t, handler, http.MethodGet, "/api/v1/omnisaves/"+save.ID, "", nil)
	var storedSave omnisave.Omnisave
	decodeResponse(t, response, &storedSave)
	if storedSave.HeadRevisionID == nil || *storedSave.HeadRevisionID != revision.ID {
		t.Fatalf("unexpected head: %v", storedSave.HeadRevisionID)
	}

	response = request(t, handler, http.MethodPost, "/api/v1/omnisaves/"+save.ID+"/revisions",
		"application/json", bytes.NewReader(revisionBody))
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"error":"head_conflict"`) ||
		!strings.Contains(response.Body.String(), `"actual_head_id":"`+revision.ID+`"`) {
		t.Fatalf("stale root returned %d: %s", response.Code, response.Body.String())
	}

	response = request(t, handler, http.MethodPost, "/api/v1/omnisaves/"+save.ID+"/forks",
		"application/json", bytes.NewBufferString(`{"revision_id":"`+revision.ID+`","display_name":"Alternate"}`))
	if response.Code != http.StatusCreated {
		t.Fatalf("fork returned %d: %s", response.Code, response.Body.String())
	}
	var fork omnisave.ForkResult
	decodeResponse(t, response, &fork)
	if fork.Omnisave.ForkedFrom == nil || fork.Omnisave.ForkedFrom.RevisionID != revision.ID ||
		len(fork.Revision.Files) != 2 {
		t.Fatalf("unexpected fork: %v", fork)
	}

	response = request(t, handler, http.MethodGet,
		"/api/v1/artifacts/"+progress.SHA256, "", nil)
	if response.Code != http.StatusOK || response.Body.String() != "game-save contents" {
		t.Fatalf("unexpected artifact response: %d %q", response.Code, response.Body.String())
	}
	response = request(t, handler, http.MethodHead, "/api/v1/artifacts/"+progress.SHA256, "", nil)
	if response.Code != http.StatusOK || response.Header().Get("Content-Length") != "18" {
		t.Fatalf("unexpected artifact head: %d %v", response.Code, response.Header())
	}

	response = request(t, handler, http.MethodDelete, "/api/v1/omnisaves/"+save.ID, "", nil)
	if response.Code != http.StatusNoContent {
		t.Fatalf("delete returned %d: %s", response.Code, response.Body.String())
	}
	response = request(t, handler, http.MethodGet, "/api/v1/omnisaves/"+save.ID, "", nil)
	if response.Code != http.StatusNotFound {
		t.Fatalf("deleted save returned %d: %s", response.Code, response.Body.String())
	}
	response = request(t, handler, http.MethodGet,
		"/api/v1/artifacts/"+progress.SHA256, "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("fork should retain the shared artifact: %d", response.Code)
	}
	response = request(t, handler, http.MethodDelete, "/api/v1/omnisaves/"+fork.Omnisave.ID, "", nil)
	if response.Code != http.StatusNoContent {
		t.Fatalf("delete fork returned %d: %s", response.Code, response.Body.String())
	}
	response = request(t, handler, http.MethodGet,
		"/api/v1/artifacts/"+progress.SHA256, "", nil)
	if response.Code != http.StatusNotFound {
		t.Fatalf("deleted artifact returned %d: %s", response.Code, response.Body.String())
	}
}

func uploadArtifact(t *testing.T, handler http.Handler, contents string) omnisave.Artifact {
	t.Helper()
	sum := sha256.Sum256([]byte(contents))
	artifact := omnisave.Artifact{
		Format: "application/octet-stream",
		SHA256: hex.EncodeToString(sum[:]),
		Size:   int64(len(contents)),
	}
	response := request(t, handler, http.MethodPut, "/api/v1/artifacts/"+artifact.SHA256,
		artifact.Format, bytes.NewBufferString(contents))
	if response.Code != http.StatusNoContent {
		t.Fatalf("upload returned %d: %s", response.Code, response.Body.String())
	}
	return artifact
}

func TestUpdateOmnisaveDisplayName(t *testing.T) {
	handler := newHandler(t, storagetest.NewMemoryRepository())

	response := request(t, handler, http.MethodPost, "/api/v1/omnisaves", "application/json",
		bytes.NewBufferString(`{"game_id":"pokemon-emerald-usa"}`))
	if response.Code != http.StatusCreated {
		t.Fatalf("create returned %d: %s", response.Code, response.Body.String())
	}
	var created omnisave.Omnisave
	decodeResponse(t, response, &created)

	response = request(t, handler, http.MethodPatch, "/api/v1/omnisaves/"+created.ID, "application/json",
		bytes.NewBufferString(`{"display_name":"  Before the final boss  "}`))
	if response.Code != http.StatusOK {
		t.Fatalf("update returned %d: %s", response.Code, response.Body.String())
	}
	var updated omnisave.Omnisave
	decodeResponse(t, response, &updated)
	if updated.DisplayName != "Before the final boss" {
		t.Fatalf("unexpected updated save: %v", updated)
	}

	response = request(t, handler, http.MethodGet, "/api/v1/omnisaves/"+created.ID, "", nil)
	var stored omnisave.Omnisave
	decodeResponse(t, response, &stored)
	if stored.DisplayName != "Before the final boss" {
		t.Fatalf("display name was not persisted: %v", stored)
	}
}

func TestDownloadSaveArchive(t *testing.T) {
	handler := newHandler(t, storagetest.NewMemoryRepository())

	createBody := bytes.NewBufferString(
		`{"game_id":"pokemon-emerald-usa","display_name":"Before the final boss"}`)
	response := request(t, handler, http.MethodPost, "/api/v1/omnisaves", "application/json", createBody)
	if response.Code != http.StatusCreated {
		t.Fatalf("create returned %d: %s", response.Code, response.Body.String())
	}
	var save omnisave.Omnisave
	decodeResponse(t, response, &save)

	response = request(t, handler, http.MethodGet, "/api/v1/omnisaves/"+save.ID+"/archive", "", nil)
	if response.Code != http.StatusNotFound {
		t.Fatalf("archive of a save without revisions returned %d: %s", response.Code, response.Body.String())
	}

	progress := uploadArtifact(t, handler, "game-save contents")
	settings := uploadArtifact(t, handler, "shared settings")
	revisionBody, err := json.Marshal(omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{
			{Path: "pokemon.sav", Artifact: progress},
			{Path: "config/settings.json", Artifact: settings},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	response = request(t, handler, http.MethodPost,
		"/api/v1/omnisaves/"+save.ID+"/revisions", "application/json", bytes.NewReader(revisionBody))
	if response.Code != http.StatusCreated {
		t.Fatalf("add revision returned %d: %s", response.Code, response.Body.String())
	}

	response = request(t, handler, http.MethodGet, "/api/v1/omnisaves/"+save.ID+"/archive", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("archive returned %d: %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "application/zip" {
		t.Fatalf("unexpected archive content type: %q", got)
	}
	if got := response.Header().Get("Content-Disposition"); got != `attachment; filename="Before the final boss.zip"` {
		t.Fatalf("unexpected content disposition: %q", got)
	}
	archive, err := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	contents := archiveContents(t, archive)
	if len(contents) != 2 || contents["pokemon.sav"] != "game-save contents" ||
		contents["config/settings.json"] != "shared settings" {
		t.Fatalf("unexpected archive contents: %v", contents)
	}
}

func TestDownloadRevisionArchive(t *testing.T) {
	handler := newHandler(t, storagetest.NewMemoryRepository())

	createBody := bytes.NewBufferString(
		`{"game_id":"pokemon-emerald-usa","display_name":"Before the final boss"}`)
	response := request(t, handler, http.MethodPost, "/api/v1/omnisaves", "application/json", createBody)
	if response.Code != http.StatusCreated {
		t.Fatalf("create returned %d: %s", response.Code, response.Body.String())
	}
	var save omnisave.Omnisave
	decodeResponse(t, response, &save)

	early := commitTestRevision(t, handler, save.ID, nil, "early progress")
	head := commitTestRevision(t, handler, save.ID, &early.ID, "late progress")

	response = request(t, handler, http.MethodGet,
		"/api/v1/omnisaves/"+save.ID+"/revisions/"+early.ID+"/archive", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("revision archive returned %d: %s", response.Code, response.Body.String())
	}
	wantDisposition := `attachment; filename="Before the final boss ` +
		early.CreatedAt.UTC().Format("2006-01-02 150405") + `.zip"`
	if got := response.Header().Get("Content-Disposition"); got != wantDisposition {
		t.Fatalf("unexpected content disposition: %q, want %q", got, wantDisposition)
	}
	archive, err := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	contents := archiveContents(t, archive)
	if len(contents) != 1 || contents["pokemon.sav"] != "early progress" {
		t.Fatalf("unexpected early revision contents: %v", contents)
	}

	response = request(t, handler, http.MethodGet,
		"/api/v1/omnisaves/"+save.ID+"/revisions/"+head.ID+"/archive", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("head revision archive returned %d: %s", response.Code, response.Body.String())
	}
	archive, err = zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	contents = archiveContents(t, archive)
	if len(contents) != 1 || contents["pokemon.sav"] != "late progress" {
		t.Fatalf("unexpected head revision contents: %v", contents)
	}

	response = request(t, handler, http.MethodGet,
		"/api/v1/omnisaves/"+save.ID+"/revisions/unknown/archive", "", nil)
	if response.Code != http.StatusNotFound {
		t.Fatalf("unknown revision archive returned %d: %s", response.Code, response.Body.String())
	}
}

func commitTestRevision(t *testing.T, handler http.Handler, saveID string, parentID *string, contents string) omnisave.Revision {
	t.Helper()
	artifact := uploadArtifact(t, handler, contents)
	body, err := json.Marshal(omnisave.CreateRevision{
		ExpectedHeadID: parentID,
		Upserts:        []omnisave.RevisionFile{{Path: "pokemon.sav", Artifact: artifact}},
	})
	if err != nil {
		t.Fatal(err)
	}
	response := request(t, handler, http.MethodPost,
		"/api/v1/omnisaves/"+saveID+"/revisions", "application/json", bytes.NewReader(body))
	if response.Code != http.StatusCreated {
		t.Fatalf("add revision returned %d: %s", response.Code, response.Body.String())
	}
	var revision omnisave.Revision
	decodeResponse(t, response, &revision)
	return revision
}

func archiveContents(t *testing.T, archive *zip.Reader) map[string]string {
	t.Helper()
	contents := make(map[string]string)
	for _, file := range archive.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		contents[file.Name] = string(data)
	}
	return contents
}

func TestAuthenticationTakesTheOwnerTokenAndIssuedCredentialsOnly(t *testing.T) {
	credentials := accessservice.New(storagetest.NewMemoryRepository(), "secret")
	protected := httpapi.Authenticate(credentials, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := httpapi.PrincipalFrom(r.Context())
		if !ok || principal == nil {
			t.Error("an authenticated request carried no principal")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	issued, err := credentials.ExchangeOwnerToken(context.Background(), "Dash")
	if err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct {
		name   string
		token  string
		status int
	}{
		{name: "no token", token: "", status: http.StatusUnauthorized},
		{name: "the owner token", token: "secret", status: http.StatusNoContent},
		{name: "an issued credential", token: issued.Token, status: http.StatusNoContent},
		{name: "someone else's guess", token: "secret2", status: http.StatusUnauthorized},
	} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/omnisaves", nil)
		if testCase.token != "" {
			request.Header.Set("Authorization", "Bearer "+testCase.token)
		}
		response := httptest.NewRecorder()
		protected.ServeHTTP(response, request)
		if response.Code != testCase.status {
			t.Fatalf("%s returned %d, wanted %d", testCase.name, response.Code, testCase.status)
		}
	}

	// Revoking is what makes a credential stop working, and it must not touch
	// the owner token that issued it.
	if err := credentials.Revoke(context.Background(), issued.Credential.ID); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/omnisaves", nil)
	request.Header.Set("Authorization", "Bearer "+issued.Token)
	response := httptest.NewRecorder()
	protected.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("a revoked credential returned %d", response.Code)
	}
}

func TestCatalogStory(t *testing.T) {
	repository := storagetest.NewMemoryRepository()
	games := catalogservice.New(repository, repository, catalogProviderStub{})
	handler := newHandler(t, repository, games)

	response := request(t, handler, http.MethodPost, "/api/v1/games/resolve", "application/json",
		bytes.NewBufferString(`{
			"fingerprints":[{"platform":"snes","algorithm":"sha1","value":"6b47bb75d16514b6a476aa0c73a683a2a4c18765"}]
		}`))
	if response.Code != http.StatusOK {
		t.Fatalf("identify returned %d: %s", response.Code, response.Body.String())
	}
	var identified struct {
		Status catalog.ResolutionStatus `json:"status"`
		Game   struct {
			ID    string `json:"id"`
			Media []struct {
				ID  string `json:"id"`
				URL string `json:"url"`
			} `json:"media"`
		} `json:"game"`
	}
	decodeResponse(t, response, &identified)
	if identified.Status != catalog.ResolutionCreated || identified.Game.ID == "" || len(identified.Game.Media) != 1 {
		t.Fatalf("unexpected identified game: %v", identified)
	}

	response = request(t, handler, http.MethodGet, identified.Game.Media[0].URL, "", nil)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("unexpected media response: %d %q", response.Code, response.Body.String())
	}
	if response.Body.String() != testCoverImage {
		t.Fatalf("unexpected media: %q", response.Body.String())
	}

	response = request(t, handler, http.MethodPost, "/api/v1/games/resolve", "application/json",
		bytes.NewBufferString(`{"identifiers":[{"namespace":"hasheous.game","value":"337"}]}`))
	if response.Code != http.StatusOK {
		t.Fatalf("resolve known identifier returned %d: %s", response.Code, response.Body.String())
	}
	var reused struct {
		Status catalog.ResolutionStatus `json:"status"`
		Game   struct {
			ID string `json:"id"`
		} `json:"game"`
	}
	decodeResponse(t, response, &reused)
	if reused.Status != catalog.ResolutionExisting || reused.Game.ID != identified.Game.ID {
		t.Fatalf("identifier did not resolve to the canonical game: %v", reused)
	}

	response = request(t, handler, http.MethodGet, "/api/v1/games", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list games returned %d: %s", response.Code, response.Body.String())
	}
	var listed []json.RawMessage
	decodeResponse(t, response, &listed)
	if len(listed) != 1 {
		t.Fatalf("expected one catalog game, got %d", len(listed))
	}
}

// A device registers itself, tracks a resolved game, and the game's
// provenance travels with game reads; untracking annotates the record.
func TestDeviceTrackingStory(t *testing.T) {
	repository := storagetest.NewMemoryRepository()
	handler := newHandler(t, repository, catalogservice.New(repository, repository, catalogProviderStub{}))

	response := request(t, handler, http.MethodPut, "/api/v1/devices/device-1", "application/json",
		bytes.NewBufferString(`{"name":"Steam Deck","platform":"linux"}`))
	if response.Code != http.StatusOK {
		t.Fatalf("register device returned %d: %s", response.Code, response.Body.String())
	}

	response = request(t, handler, http.MethodPost, "/api/v1/games/resolve", "application/json",
		bytes.NewBufferString(`{
			"fingerprints":[{"platform":"snes","algorithm":"sha1","value":"6b47bb75d16514b6a476aa0c73a683a2a4c18765"}]
		}`))
	if response.Code != http.StatusOK {
		t.Fatalf("resolve returned %d: %s", response.Code, response.Body.String())
	}
	var resolved struct {
		Game struct {
			ID string `json:"id"`
		} `json:"game"`
	}
	decodeResponse(t, response, &resolved)

	trackPath := "/api/v1/games/" + resolved.Game.ID + "/tracking/device-1"
	response = request(t, handler, http.MethodPut, trackPath, "application/json",
		bytes.NewBufferString(`{"adapter":"retroarch","installed":true}`))
	if response.Code != http.StatusNoContent {
		t.Fatalf("track returned %d: %s", response.Code, response.Body.String())
	}

	response = request(t, handler, http.MethodGet, "/api/v1/games/"+resolved.Game.ID, "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("get game returned %d: %s", response.Code, response.Body.String())
	}
	var game struct {
		Provenance []struct {
			DeviceName  string  `json:"device_name"`
			Adapter     string  `json:"adapter"`
			Installed   bool    `json:"installed"`
			UntrackedAt *string `json:"untracked_at"`
		} `json:"provenance"`
	}
	decodeResponse(t, response, &game)
	if len(game.Provenance) != 1 || game.Provenance[0].DeviceName != "Steam Deck" ||
		game.Provenance[0].Adapter != "retroarch" || !game.Provenance[0].Installed ||
		game.Provenance[0].UntrackedAt != nil {
		t.Fatalf("unexpected provenance: %+v", game.Provenance)
	}

	response = request(t, handler, http.MethodDelete, trackPath, "", nil)
	if response.Code != http.StatusNoContent {
		t.Fatalf("untrack returned %d: %s", response.Code, response.Body.String())
	}
	response = request(t, handler, http.MethodGet, "/api/v1/games/"+resolved.Game.ID, "", nil)
	decodeResponse(t, response, &game)
	if len(game.Provenance) != 1 || game.Provenance[0].UntrackedAt == nil {
		t.Fatalf("untracking should keep an annotated record: %+v", game.Provenance)
	}
}

func TestManualCatalogMatchStory(t *testing.T) {
	repository := storagetest.NewMemoryRepository()
	handler := newHandler(t, repository, catalogservice.New(repository, repository, catalogProviderStub{}))

	response := request(t, handler, http.MethodGet,
		"/api/v1/games/debug-game/match-candidates?q=Super+Mario+World&platform=SNES", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("search returned %d: %s", response.Code, response.Body.String())
	}
	var candidates []catalog.GameCandidate
	decodeResponse(t, response, &candidates)
	if len(candidates) != 1 || candidates[0].SelectionToken == "" {
		t.Fatalf("unexpected candidates: %v", candidates)
	}

	body, err := json.Marshal(catalog.MatchGame{SelectionToken: candidates[0].SelectionToken})
	if err != nil {
		t.Fatal(err)
	}
	response = request(t, handler, http.MethodPut, "/api/v1/games/debug-game/match",
		"application/json", bytes.NewReader(body))
	if response.Code != http.StatusOK {
		t.Fatalf("match returned %d: %s", response.Code, response.Body.String())
	}
	var matched struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	decodeResponse(t, response, &matched)
	if matched.ID != "debug-game" || matched.Title != "Super Mario World" {
		t.Fatalf("unexpected manual match: %v", matched)
	}
}

type catalogProviderStub struct{}

func (catalogProviderStub) Name() string { return "stub" }

const testCoverImage = "\x89PNG\r\n\x1a\ncover image"

func (catalogProviderStub) Resolve(_ context.Context, evidence catalog.ResolveGame) (*catalog.ProviderMatch, error) {
	match := catalogStubMatch()
	match.Fingerprints = append(match.Fingerprints, evidence.Fingerprints...)
	return match, nil
}

func (catalogProviderStub) Search(context.Context, catalog.SearchGames) ([]catalog.GameCandidate, error) {
	return []catalog.GameCandidate{{
		Provider:       "hasheous",
		ProviderID:     "962167",
		Title:          "Super Mario World",
		Edition:        "Super Mario World (USA)",
		Platform:       "Super Nintendo Entertainment System",
		SelectionToken: "known-selection",
	}}, nil
}

func (catalogProviderStub) Match(context.Context, string) (*catalog.ProviderMatch, error) {
	return catalogStubMatch(), nil
}

func catalogStubMatch() *catalog.ProviderMatch {
	return &catalog.ProviderMatch{
		Source:      "hasheous",
		Identifiers: []catalog.GameIdentifier{{Namespace: "hasheous.game", Value: "337"}},
		Title:       "Super Mario World",
		Platform:    "Super Nintendo Entertainment System",
		ROM: catalog.ROMMatch{
			ProviderID: "1628019",
			Source:     "no-intro",
		},
		Media: []catalog.MediaReference{{Kind: "cover", ProviderID: "cover-id"}},
	}
}

func (catalogProviderStub) OpenMedia(context.Context, catalog.MediaReference) (string, io.ReadCloser, error) {
	return "image/png", io.NopCloser(strings.NewReader(testCoverImage)), nil
}

// newHandler builds the handler the server ships: authenticated, over one
// repository, with the owner token every request helper here sends.
func newHandler(
	t *testing.T, repository *storagetest.MemoryRepository, catalogs ...catalog.Service,
) http.Handler {
	t.Helper()
	config := httpapi.Config{Saves: omnisaveservice.New(repository)}
	if len(catalogs) > 0 {
		config.Catalog = catalogs[0]
	}
	return httpapi.New(accessservice.New(repository, owner), config)
}

func request(t *testing.T, handler http.Handler, method, path, contentType string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(context.Background(), method, path, body)
	request.Header.Set("Authorization", "Bearer "+owner)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func decodeResponse(t *testing.T, response *httptest.ResponseRecorder, destination any) {
	t.Helper()
	if err := json.NewDecoder(response.Body).Decode(destination); err != nil {
		t.Fatal(err)
	}
}
