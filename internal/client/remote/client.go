// Package remote accesses the OmniSave server from an installed client.
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

// Client is an authenticated OmniSave API client.
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
		return nil, fmt.Errorf("contact OmniSave server: %w", err)
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
	return fmt.Sprintf("OmniSave server returned %s", http.StatusText(e.StatusCode))
}

// New creates a client for one OmniSave server.
func New(baseURL, token string, httpClient *http.Client) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("invalid OmniSave server URL")
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("OmniSave API token is required")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{baseURL: baseURL, token: token, httpClient: httpClient}, nil
}

// ListOmniSaves returns the server records available as binding destinations.
func (c *Client) ListOmniSaves(ctx context.Context) ([]omnisave.OmniSave, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/omnisaves", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("contact OmniSave server: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBody))
		return nil, &ResponseError{StatusCode: response.StatusCode}
	}
	var saves []omnisave.OmniSave
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBody))
	if err := decoder.Decode(&saves); err != nil {
		return nil, fmt.Errorf("decode OmniSave list: %w", err)
	}
	return saves, nil
}
