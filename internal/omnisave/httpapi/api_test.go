package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	"github.com/krisbaumgartner/omnisave/internal/omnisave/httpapi"
	omnisaveservice "github.com/krisbaumgartner/omnisave/internal/omnisave/service"
	"github.com/krisbaumgartner/omnisave/internal/storage/storagetest"
)

func TestNetworkClientStory(t *testing.T) {
	handler := httpapi.New(omnisaveservice.New(storagetest.NewMemoryRepository()))

	createBody := bytes.NewBufferString(`{"game_id":"pokemon-emerald-usa","slot":"default"}`)
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

	response = request(t, handler, http.MethodGet,
		"/api/v1/artifacts/"+revision.Artifact.SHA256, "", nil)
	if response.Code != http.StatusOK || response.Body.String() != "game-save contents" {
		t.Fatalf("unexpected artifact response: %d %q", response.Code, response.Body.String())
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
