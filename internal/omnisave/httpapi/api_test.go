package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	catalogservice "github.com/krisbaumgartner/omnisave/internal/catalog/service"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	"github.com/krisbaumgartner/omnisave/internal/omnisave/httpapi"
	omnisaveservice "github.com/krisbaumgartner/omnisave/internal/omnisave/service"
	"github.com/krisbaumgartner/omnisave/internal/storage/storagetest"
)

func TestNetworkClientStory(t *testing.T) {
	handler := httpapi.New(omnisaveservice.New(storagetest.NewMemoryRepository()))

	createBody := bytes.NewBufferString(`{"game_id":"pokemon-emerald-usa"}`)
	response := request(t, handler, http.MethodPost, "/api/v1/omnisaves", "application/json", createBody)
	if response.Code != http.StatusCreated {
		t.Fatalf("create returned %d: %s", response.Code, response.Body.String())
	}
	var save omnisave.OmniSave
	decodeResponse(t, response, &save)

	revisionBody := &bytes.Buffer{}
	writer := multipart.NewWriter(revisionBody)
	if err := writer.WriteField("revision", `{"format":"application/vnd.omnisave.raw-save.v1"}`); err != nil {
		t.Fatal(err)
	}
	payload, err := writer.CreateFormFile("payload", "pokemon.sav")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := payload.Write([]byte("game-save contents")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	response = request(t, handler, http.MethodPost,
		"/api/v1/omnisaves/"+save.ID+"/revisions", writer.FormDataContentType(), revisionBody)
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
	if history[0].ParentIDs == nil {
		t.Fatal("an initial revision should have an empty parent list")
	}

	response = request(t, handler, http.MethodGet,
		"/api/v1/artifacts/"+revision.Artifact.SHA256, "", nil)
	if response.Code != http.StatusOK || response.Body.String() != "game-save contents" {
		t.Fatalf("unexpected artifact response: %d %q", response.Code, response.Body.String())
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
		"/api/v1/artifacts/"+revision.Artifact.SHA256, "", nil)
	if response.Code != http.StatusNotFound {
		t.Fatalf("deleted artifact returned %d: %s", response.Code, response.Body.String())
	}
}

func TestUpdateOmniSaveDisplayName(t *testing.T) {
	handler := httpapi.New(omnisaveservice.New(storagetest.NewMemoryRepository()))

	response := request(t, handler, http.MethodPost, "/api/v1/omnisaves", "application/json",
		bytes.NewBufferString(`{"game_id":"pokemon-emerald-usa"}`))
	if response.Code != http.StatusCreated {
		t.Fatalf("create returned %d: %s", response.Code, response.Body.String())
	}
	var created omnisave.OmniSave
	decodeResponse(t, response, &created)

	response = request(t, handler, http.MethodPatch, "/api/v1/omnisaves/"+created.ID, "application/json",
		bytes.NewBufferString(`{"display_name":"  Before the final boss  "}`))
	if response.Code != http.StatusOK {
		t.Fatalf("update returned %d: %s", response.Code, response.Body.String())
	}
	var updated omnisave.OmniSave
	decodeResponse(t, response, &updated)
	if updated.DisplayName != "Before the final boss" {
		t.Fatalf("unexpected updated save: %v", updated)
	}

	response = request(t, handler, http.MethodGet, "/api/v1/omnisaves/"+created.ID, "", nil)
	var stored omnisave.OmniSave
	decodeResponse(t, response, &stored)
	if stored.DisplayName != "Before the final boss" {
		t.Fatalf("display name was not persisted: %v", stored)
	}
}

func TestBearerAuth(t *testing.T) {
	protected := httpapi.BearerAuth("secret", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	withoutToken := httptest.NewRequest(http.MethodGet, "/api/v1/omnisaves", nil)
	response := httptest.NewRecorder()
	protected.ServeHTTP(response, withoutToken)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("request without token returned %d", response.Code)
	}

	withToken := httptest.NewRequest(http.MethodGet, "/api/v1/omnisaves", nil)
	withToken.Header.Set("Authorization", "Bearer secret")
	response = httptest.NewRecorder()
	protected.ServeHTTP(response, withToken)
	if response.Code != http.StatusNoContent {
		t.Fatalf("authorized request returned %d", response.Code)
	}
}

func TestCatalogStory(t *testing.T) {
	repository := storagetest.NewMemoryRepository()
	saves := omnisaveservice.New(repository)
	games := catalogservice.New(repository, repository, catalogProviderStub{})
	handler := httpapi.New(saves, games)

	response := request(t, handler, http.MethodPost, "/api/v1/games/identify", "application/json",
		bytes.NewBufferString(`{
			"game_id":"super-mario-world",
			"fingerprint":{"platform":"snes","sha1":"6b47bb75d16514b6a476aa0c73a683a2a4c18765"}
		}`))
	if response.Code != http.StatusOK {
		t.Fatalf("identify returned %d: %s", response.Code, response.Body.String())
	}
	var identified struct {
		ID    string `json:"id"`
		Media []struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		} `json:"media"`
	}
	decodeResponse(t, response, &identified)
	if identified.ID != "super-mario-world" || len(identified.Media) != 1 {
		t.Fatalf("unexpected identified game: %v", identified)
	}

	response = request(t, handler, http.MethodGet, identified.Media[0].URL, "", nil)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("unexpected media response: %d %q", response.Code, response.Body.String())
	}
	if response.Body.String() != testCoverImage {
		t.Fatalf("unexpected media: %q", response.Body.String())
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

func TestManualCatalogMatchStory(t *testing.T) {
	repository := storagetest.NewMemoryRepository()
	handler := httpapi.New(
		omnisaveservice.New(repository),
		catalogservice.New(repository, repository, catalogProviderStub{}),
	)

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

const testCoverImage = "\x89PNG\r\n\x1a\ncover image"

func (catalogProviderStub) Identify(_ context.Context, fingerprint catalog.Fingerprint) (*catalog.ProviderMatch, error) {
	match := catalogStubMatch()
	match.ROM.SHA1 = fingerprint.SHA1
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
		Provider:   "hasheous",
		ProviderID: "337",
		Title:      "Super Mario World",
		Platform:   "Super Nintendo Entertainment System",
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

func request(t *testing.T, handler http.Handler, method, path, contentType string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(context.Background(), method, path, body)
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
