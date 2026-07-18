// Package remote accesses the Omnisave server from an installed client.
package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

const maxResponseBody = 1 << 20

// Client is an authenticated Omnisave API client.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
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

// New creates a client for one Omnisave server.
func New(baseURL, token string, httpClient *http.Client) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("invalid Omnisave server URL")
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("Omnisave API token is required")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{baseURL: baseURL, token: token, httpClient: httpClient}, nil
}

// RegisterDevice reports this installation's self-minted identity to the server.
func (c *Client) RegisterDevice(ctx context.Context, id string, input catalog.RegisterDevice) error {
	return c.send(ctx, http.MethodPut, "/api/v1/devices/"+url.PathEscape(id), input)
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

// CommitRevision commits file changes against the Omnisave's expected head.
func (c *Client) CommitRevision(ctx context.Context, omnisaveID string, input omnisave.CreateRevision) (*omnisave.Revision, error) {
	var revision omnisave.Revision
	path := "/api/v1/omnisaves/" + url.PathEscape(omnisaveID) + "/revisions"
	if err := c.postJSON(ctx, path, input, &revision); err != nil {
		return nil, err
	}
	return &revision, nil
}

// DeleteOmnisave removes a save and its revision history.
func (c *Client) DeleteOmnisave(ctx context.Context, id string) error {
	return c.send(ctx, http.MethodDelete, "/api/v1/omnisaves/"+url.PathEscape(id), nil)
}

// UploadArtifact stores content-addressed bytes the server verifies by hash.
func (c *Client) UploadArtifact(ctx context.Context, artifact omnisave.Artifact, content io.Reader) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPut,
		c.baseURL+"/api/v1/artifacts/"+url.PathEscape(artifact.SHA256), content)
	if err != nil {
		return err
	}
	// The server rejects uploads with an unknown length, and a plain reader
	// advertises none — the artifact's measured size is the claim it checks.
	request.ContentLength = artifact.Size
	request.Header.Set("Authorization", "Bearer "+c.token)
	format := artifact.Format
	if format == "" {
		format = "application/octet-stream"
	}
	request.Header.Set("Content-Type", format)
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

func (c *Client) postJSON(ctx context.Context, path string, payload, result any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("contact Omnisave server: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBody))
		return &ResponseError{StatusCode: response.StatusCode}
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maxResponseBody)).Decode(result); err != nil {
		return fmt.Errorf("decode Omnisave server response: %w", err)
	}
	return nil
}

// ListOmnisaves returns the server records available as binding destinations.
func (c *Client) ListOmnisaves(ctx context.Context) ([]omnisave.Omnisave, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/omnisaves", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("contact Omnisave server: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBody))
		return nil, &ResponseError{StatusCode: response.StatusCode}
	}
	var saves []omnisave.Omnisave
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBody))
	if err := decoder.Decode(&saves); err != nil {
		return nil, fmt.Errorf("decode Omnisave list: %w", err)
	}
	return saves, nil
}
