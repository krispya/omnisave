package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"

	"github.com/krisbaumgartner/omnisave/internal/shared"
)

// ListVersions fetches version history for a game from the server.
func (s *Syncer) ListVersions(system, game string) ([]shared.VersionInfo, error) {
	url := fmt.Sprintf("%s/games/%s/%s/versions", s.cfg.ServerURL, system, game)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.Token)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list versions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list versions: server returned %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}

	var result shared.ListVersionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode versions: %w", err)
	}
	return result.Versions, nil
}

// RestoreVersion downloads a specific version by sha256, sets it active on the server,
// and writes it to destPath.
func (s *Syncer) RestoreVersion(system, game, sha256, destPath string) error {
	// Set active on server.
	setURL := fmt.Sprintf("%s/games/%s/%s/active/%s", s.cfg.ServerURL, system, game, sha256)
	setReq, err := http.NewRequest("PUT", setURL, nil)
	if err != nil {
		return err
	}
	setReq.Header.Set("Authorization", "Bearer "+s.cfg.Token)
	setResp, err := s.client.Do(setReq)
	if err != nil {
		return fmt.Errorf("set active: %w", err)
	}
	setResp.Body.Close()
	if setResp.StatusCode != http.StatusOK {
		return fmt.Errorf("set active: server returned %d", setResp.StatusCode)
	}

	// Download the specific version file.
	getURL := fmt.Sprintf("%s/games/%s/%s/version/%s", s.cfg.ServerURL, system, game, sha256)
	req, err := http.NewRequest("GET", getURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.Token)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("restore request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("restore: server returned %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read restore response: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(destPath, data, 0644)
}

// Syncer pushes and pulls save files to/from the server.
type Syncer struct {
	cfg    *Config
	state  *State
	client *http.Client
}

// NewSyncer creates a Syncer.
func NewSyncer(cfg *Config, state *State) *Syncer {
	return &Syncer{
		cfg:    cfg,
		state:  state,
		client: &http.Client{},
	}
}

// SyncGame pushes a local save file to the server if it has changed.
// Returns (pushed bool, conflict bool, error).
func (s *Syncer) SyncGame(system, game, filePath string) (pushed bool, conflict bool, err error) {
	hash, err := shared.HashFile(filePath)
	if err != nil {
		return false, false, fmt.Errorf("hash file: %w", err)
	}

	if hash == s.state.LastHash(system, game) {
		return false, false, nil // unchanged
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return false, false, fmt.Errorf("read file: %w", err)
	}

	resp, err := s.push(system, game, filepath.Base(filePath), data, s.state.LastHash(system, game))
	if err != nil {
		return false, false, err
	}

	s.state.UpdateHash(system, game, resp.SHA256)
	return true, resp.Conflict, nil
}

// PullGame downloads the active save from the server and writes it to destPath.
func (s *Syncer) PullGame(system, game, destPath string) error {
	url := fmt.Sprintf("%s/games/%s/%s/active", s.cfg.ServerURL, system, game)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.Token)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("pull request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("pull: server returned %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(destPath, data, 0644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	sha256 := shared.HashBytes(data)
	s.state.UpdateHash(system, game, sha256)
	return nil
}

func (s *Syncer) push(system, game, filename string, data []byte, parentSHA256 string) (*shared.PushResponse, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return nil, err
	}
	if _, err := fw.Write(data); err != nil {
		return nil, err
	}
	mw.WriteField("client_id", s.cfg.ClientID)
	mw.WriteField("parent_sha256", parentSHA256)
	mw.Close()

	url := fmt.Sprintf("%s/games/%s/%s/push", s.cfg.ServerURL, system, game)
	req, err := http.NewRequest("POST", url, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.Token)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("push request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("push: server returned %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}

	var pushResp shared.PushResponse
	if err := json.NewDecoder(resp.Body).Decode(&pushResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &pushResp, nil
}
