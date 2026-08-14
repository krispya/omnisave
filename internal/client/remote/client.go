// Package remote accesses the Omnisave server from an installed client.
package remote

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/client/activity"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

const maxResponseBody = 1 << 20

// Client is an authenticated Omnisave API client.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
	// transferClient carries artifact bodies. API calls ride a client whose
	// overall timeout keeps a dead server from hanging a pass, but that same
	// timeout would abort a large save transfer mid-flight, so transfers get
	// a client bounded at the dial and response-header stages instead.
	transferClient *http.Client
	// eventSilence is how long the event stream may say nothing before it is
	// treated as dead. Zero means the default.
	eventSilence time.Duration
}

// ResolveGame maps local identity evidence to a server-owned catalog Game.
func (c *Client) ResolveGame(ctx context.Context, input catalog.ResolveGame) (*catalog.GameResolution, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/games/resolve", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("contact Omnisave server: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBody))
		return nil, &ResponseError{StatusCode: response.StatusCode}
	}
	var resolution catalog.GameResolution
	if err := json.NewDecoder(io.LimitReader(response.Body, maxResponseBody)).Decode(&resolution); err != nil {
		return nil, fmt.Errorf("decode game resolution: %w", err)
	}
	return &resolution, nil
}

// ResponseError reports a non-successful API response.
type ResponseError struct {
	StatusCode int
}

func (e *ResponseError) Error() string {
	return fmt.Sprintf("Omnisave server returned %s", http.StatusText(e.StatusCode))
}

// CurrentRevisionConflict carries the actual pointer after a stale commit or restore.
type CurrentRevisionConflict struct {
	ActualCurrentRevisionID *string
}

func (e *CurrentRevisionConflict) Error() string {
	return "Omnisave current revision moved on the server"
}

// NormalizeServerURL trims a server URL to the form the client talks to and
// rejects anything that is not one. Pairing needs this before a client holds
// any credential, so it lives apart from the authenticated client.
func NormalizeServerURL(baseURL string) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("invalid Omnisave server URL")
	}
	return baseURL, nil
}

// New creates a client for one Omnisave server.
func New(baseURL, token string, httpClient *http.Client) (*Client, error) {
	baseURL, err := NormalizeServerURL(baseURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("Omnisave API token is required")
	}
	transferClient := httpClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.ResponseHeaderTimeout = 30 * time.Second
		transferClient = &http.Client{Transport: transport}
	}
	return &Client{baseURL: baseURL, token: token, httpClient: httpClient, transferClient: transferClient}, nil
}

// RegisterDevice reports this installation's self-minted identity to the server.
func (c *Client) RegisterDevice(ctx context.Context, id string, input catalog.RegisterDevice) error {
	return c.send(ctx, http.MethodPut, "/api/v1/devices/"+url.PathEscape(id), input)
}

// DeviceStatus is what a device reports about itself between syncs: the
// games it sees being played right now. An empty list clears the report.
type DeviceStatus struct {
	PlayingGameIDs []string `json:"playing_game_ids"`
}

// ReportDeviceStatus tells the server which games this device is playing.
// Presence, not provenance: the server holds it briefly and in memory, so
// reports are re-affirmed while a game runs.
func (c *Client) ReportDeviceStatus(ctx context.Context, deviceID string, playingGameIDs []string) error {
	if playingGameIDs == nil {
		playingGameIDs = []string{}
	}
	return c.send(ctx, http.MethodPut, "/api/v1/devices/"+url.PathEscape(deviceID)+"/status",
		DeviceStatus{PlayingGameIDs: playingGameIDs})
}

// TrackGame records that this device tracks a game, refreshing its provenance.
func (c *Client) TrackGame(ctx context.Context, gameID, deviceID string, input catalog.TrackGame) error {
	return c.send(ctx, http.MethodPut,
		"/api/v1/games/"+url.PathEscape(gameID)+"/tracking/"+url.PathEscape(deviceID), input)
}

// UntrackGame marks this device's provenance record inactive without removing it.
func (c *Client) UntrackGame(ctx context.Context, gameID, deviceID string) error {
	return c.send(ctx, http.MethodDelete,
		"/api/v1/games/"+url.PathEscape(gameID)+"/tracking/"+url.PathEscape(deviceID), nil)
}

func (c *Client) send(ctx context.Context, method, path string, payload any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("contact Omnisave server: %w", err)
	}
	defer response.Body.Close()
	io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBody))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &ResponseError{StatusCode: response.StatusCode}
	}
	return nil
}

// CreateOmnisave asks the server to create a new logical save for a game.
func (c *Client) CreateOmnisave(ctx context.Context, input omnisave.CreateOmnisave) (*omnisave.Omnisave, error) {
	var created omnisave.Omnisave
	if err := c.postJSON(ctx, "/api/v1/omnisaves", input, &created); err != nil {
		return nil, err
	}
	return &created, nil
}

// CommitRevision commits file changes against the expected current revision.
func (c *Client) CommitRevision(ctx context.Context, omnisaveID string, input omnisave.CreateRevision) (*omnisave.Revision, error) {
	var revision omnisave.Revision
	path := "/api/v1/omnisaves/" + url.PathEscape(omnisaveID) + "/revisions"
	if err := c.postJSON(ctx, path, input, &revision); err != nil {
		return nil, err
	}
	return &revision, nil
}

// ReportAchievements tells the server what this Device watched a game unlock.
// The server decides which revision each unlock lands on and ignores any it
// already holds, so repeating a report is safe. It answers with what the
// report added.
func (c *Client) ReportAchievements(ctx context.Context, omnisaveID string, unlocks []omnisave.AchievementUnlock) ([]omnisave.Achievement, error) {
	var recorded struct {
		Achievements []omnisave.Achievement `json:"achievements"`
	}
	path := "/api/v1/omnisaves/" + url.PathEscape(omnisaveID) + "/achievements"
	input := struct {
		Achievements []omnisave.AchievementUnlock `json:"achievements"`
	}{Achievements: unlocks}
	if err := c.postJSON(ctx, path, input, &recorded); err != nil {
		return nil, err
	}
	return recorded.Achievements, nil
}

// ListAchievements returns a save's recorded unlocks and where each one
// landed in its history.
func (c *Client) ListAchievements(ctx context.Context, omnisaveID string) ([]omnisave.Achievement, error) {
	var achievements []omnisave.Achievement
	path := "/api/v1/omnisaves/" + url.PathEscape(omnisaveID) + "/achievements"
	if err := c.getJSON(ctx, path, &achievements); err != nil {
		return nil, err
	}
	return achievements, nil
}

// RestoreCurrentRevision moves an Omnisave's global current pointer to an
// existing revision in its tree (FDR-005). The server rejects a stale
// expectation with a CurrentRevisionConflict.
func (c *Client) RestoreCurrentRevision(ctx context.Context, omnisaveID string, input omnisave.RestoreRevision) (*omnisave.Omnisave, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPut,
		c.baseURL+"/api/v1/omnisaves/"+url.PathEscape(omnisaveID)+"/current-revision", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("contact Omnisave server: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, decodeErrorResponse(response)
	}
	var restored omnisave.Omnisave
	if err := json.NewDecoder(io.LimitReader(response.Body, maxResponseBody)).Decode(&restored); err != nil {
		return nil, fmt.Errorf("decode Omnisave server response: %w", err)
	}
	return &restored, nil
}

// DeleteOmnisave removes a save and its revision history.
func (c *Client) DeleteOmnisave(ctx context.Context, id string) error {
	return c.send(ctx, http.MethodDelete, "/api/v1/omnisaves/"+url.PathEscape(id), nil)
}

// ForkOmnisave creates a new lineage from one revision of an existing save.
func (c *Client) ForkOmnisave(ctx context.Context, id string, input omnisave.ForkOmnisave) (*omnisave.ForkResult, error) {
	var fork omnisave.ForkResult
	path := "/api/v1/omnisaves/" + url.PathEscape(id) + "/forks"
	if err := c.postJSON(ctx, path, input, &fork); err != nil {
		return nil, err
	}
	return &fork, nil
}

// UploadArtifact stores content-addressed bytes the server verifies by hash.
func (c *Client) UploadArtifact(ctx context.Context, artifact omnisave.Artifact, content io.Reader) error {
	// Save content compresses well and travels compressed; the artifact's
	// logical size rides a header since Content-Length now measures wire
	// bytes. Saves are small, so buffering the compressed body to learn
	// its length costs less than a chunked upload would complicate.
	var compressed bytes.Buffer
	activity.Report(ctx, "compressing")
	compressor := gzip.NewWriter(&compressed)
	if _, err := io.Copy(compressor, content); err != nil {
		return fmt.Errorf("compress artifact: %w", err)
	}
	if err := compressor.Close(); err != nil {
		return fmt.Errorf("compress artifact: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPut,
		c.baseURL+"/api/v1/artifacts/"+url.PathEscape(artifact.SHA256), &compressed)
	if err != nil {
		return err
	}
	request.ContentLength = int64(compressed.Len())
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Encoding", "gzip")
	request.Header.Set("X-Omnisave-Size", strconv.FormatInt(artifact.Size, 10))
	format := artifact.Format
	if format == "" {
		format = "application/octet-stream"
	}
	request.Header.Set("Content-Type", format)
	activity.Report(ctx, "uploading")
	response, err := c.transferClient.Do(request)
	if err != nil {
		return fmt.Errorf("contact Omnisave server: %w", err)
	}
	defer response.Body.Close()
	io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBody))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &ResponseError{StatusCode: response.StatusCode}
	}
	return nil
}

// OpenArtifact streams content-addressed bytes from the server. The caller
// must close the returned reader.
func (c *Client) OpenArtifact(ctx context.Context, sha256 string) (io.ReadCloser, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/api/v1/artifacts/"+url.PathEscape(sha256), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	activity.Report(ctx, "downloading")
	response, err := c.transferClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("contact Omnisave server: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		defer response.Body.Close()
		io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBody))
		return nil, &ResponseError{StatusCode: response.StatusCode}
	}
	return response.Body, nil
}

func (c *Client) postJSON(ctx context.Context, path string, payload, result any) error {
	return postJSON(ctx, c.httpClient, c.baseURL+path, c.token, payload, result)
}

// postJSON sends one JSON request and decodes the answer. It takes its client,
// URL, and token rather than reading them off a Client, because pairing has to
// make the same call before it holds either a credential or a Client — an
// empty token sends no Authorization header.
func postJSON(ctx context.Context, httpClient *http.Client, url, token string, payload, result any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("contact Omnisave server: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return decodeErrorResponse(response)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maxResponseBody)).Decode(result); err != nil {
		return fmt.Errorf("decode Omnisave server response: %w", err)
	}
	return nil
}

// decodeErrorResponse surfaces structured API errors the client acts on —
// a commit rejected for missing artifacts carries exactly which content to
// upload, a stale commit carries where the Current Revision actually is —
// and reports everything else by status.
func decodeErrorResponse(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, maxResponseBody))
	var details struct {
		Error                   string   `json:"error"`
		MissingSHA256           []string `json:"missing_sha256"`
		ActualCurrentRevisionID *string  `json:"actual_current_revision_id"`
	}
	if json.Unmarshal(body, &details) == nil {
		switch details.Error {
		case "artifact_missing":
			return &omnisave.MissingArtifacts{SHA256: details.MissingSHA256}
		case "current_revision_conflict":
			return &CurrentRevisionConflict{ActualCurrentRevisionID: details.ActualCurrentRevisionID}
		}
	}
	return &ResponseError{StatusCode: response.StatusCode}
}

func (c *Client) getJSON(ctx context.Context, path string, result any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("contact Omnisave server: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBody))
		return &ResponseError{StatusCode: response.StatusCode}
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maxResponseBody)).Decode(result); err != nil {
		return fmt.Errorf("decode Omnisave server response: %w", err)
	}
	return nil
}

// ListOmnisaves returns the server records available as binding destinations.
// ListGameIDs returns the identity of every game in the server's Library.
func (c *Client) ListGameIDs(ctx context.Context) ([]string, error) {
	var games []struct {
		ID string `json:"id"`
	}
	if err := c.getJSON(ctx, "/api/v1/games", &games); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(games))
	for _, game := range games {
		ids = append(ids, game.ID)
	}
	return ids, nil
}

func (c *Client) ListOmnisaves(ctx context.Context) ([]omnisave.Omnisave, error) {
	var saves []omnisave.Omnisave
	if err := c.getJSON(ctx, "/api/v1/omnisaves", &saves); err != nil {
		return nil, err
	}
	return saves, nil
}

// ListRevisions returns one Omnisave's complete history for content matching.
func (c *Client) ListRevisions(ctx context.Context, omnisaveID string) ([]omnisave.Revision, error) {
	var revisions []omnisave.Revision
	path := "/api/v1/omnisaves/" + url.PathEscape(omnisaveID) + "/revisions"
	if err := c.getJSON(ctx, path, &revisions); err != nil {
		return nil, err
	}
	return revisions, nil
}
