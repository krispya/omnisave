// Package hasheous adapts the hosted Hasheous catalog API.
package hasheous

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
)

const maxLookupResponse = 4 << 20

var imageIDPattern = regexp.MustCompile(`^[A-Fa-f0-9]{16,128}$`)

type Provider struct {
	baseURL string
	client  *http.Client
}

// New creates a provider for a Hasheous server.
func New(baseURL string, client *http.Client) *Provider {
	if client == nil {
		client = http.DefaultClient
	}
	return &Provider{baseURL: strings.TrimRight(baseURL, "/"), client: client}
}

func (p *Provider) Identify(ctx context.Context, fingerprint catalog.Fingerprint) (*catalog.ProviderMatch, error) {
	hashes := make(map[string]string)
	for name, value := range map[string]string{
		"crc": fingerprint.CRC32, "md5": fingerprint.MD5,
		"sha1": fingerprint.SHA1, "sha256": fingerprint.SHA256,
	} {
		if value != "" {
			hashes[name] = value
		}
	}
	body, err := json.Marshal(hashes)
	if err != nil {
		return nil, err
	}
	endpoint, err := url.Parse(p.baseURL + "/api/v1/Lookup/ByHash")
	if err != nil {
		return nil, catalog.ErrUnavailable
	}
	query := endpoint.Query()
	query.Set("returnFields", "All")
	query.Set("returnSources", "NoIntros")
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "OmniSave/1")

	response, err := p.client.Do(request)
	if err != nil {
		return nil, catalog.ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, catalog.ErrNotFound
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, catalog.ErrUnavailable
	}
	var result lookupResponse
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxLookupResponse))
	if err := decoder.Decode(&result); err != nil {
		return nil, catalog.ErrUnavailable
	}
	signature, ok := bestSignature(result.Signatures.NoIntros, fingerprint)
	if !ok || result.ID == 0 || result.Name == "" {
		return nil, catalog.ErrNotFound
	}

	attributes, media := compileAttributes(result.Attributes)
	description, _ := attributes["description"].(string)
	delete(attributes, "description")
	attributes["external_references"] = result.Metadata
	return &catalog.ProviderMatch{
		Provider:    "hasheous",
		ProviderID:  strconv.FormatInt(result.ID, 10),
		Title:       result.Name,
		SortTitle:   signature.Game.SortingName,
		Platform:    result.Platform.Name,
		Publisher:   result.Publisher.Name,
		Description: description,
		Metadata:    attributes,
		ROM: catalog.ROMMatch{
			ProviderID: signature.ROM.ID,
			System:     signature.Game.System,
			Name:       signature.ROM.Name,
			Region:     joinValues(signature.ROM.Country),
			Languages:  sortedValues(signature.ROM.Language),
			Size:       signature.ROM.Size,
			CRC32:      signature.ROM.CRC,
			MD5:        signature.ROM.MD5,
			SHA1:       signature.ROM.SHA1,
			SHA256:     signature.ROM.SHA256,
			Source:     "no-intro",
			Attributes: signature.ROM.Attributes,
		},
		Media: media,
	}, nil
}

func (p *Provider) Search(ctx context.Context, input catalog.SearchGames) ([]catalog.GameCandidate, error) {
	arguments := map[string]any{
		"name":        strings.TrimSpace(input.Title),
		"limit":       input.Limit,
		"includeRoms": true,
	}
	if platform := searchPlatform(input.Platform); platform != "" {
		arguments["platform"] = platform
	}
	var result struct {
		Games []mcpGame `json:"games"`
	}
	if err := p.callMCP(ctx, "hasheous_search_games", arguments, &result); err != nil {
		return nil, err
	}
	candidates, err := compileCandidates(result.Games)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 && arguments["platform"] != nil {
		delete(arguments, "platform")
		if err := p.callMCP(ctx, "hasheous_search_games", arguments, &result); err != nil {
			return nil, err
		}
		return compileCandidates(result.Games)
	}
	return candidates, nil
}

func compileCandidates(games []mcpGame) ([]catalog.GameCandidate, error) {
	candidates := make([]catalog.GameCandidate, 0, len(games))
	for _, game := range games {
		rom, ok := representativeROM(game)
		if !ok || game.ID <= 0 || strings.TrimSpace(game.Name) == "" {
			continue
		}
		token, err := encodeSelection(matchSelection{
			GameID:   game.ID,
			Platform: game.Platform.Name,
			CRC32:    rom.CRC,
			MD5:      rom.MD5,
			SHA1:     rom.SHA1,
			SHA256:   rom.SHA256,
		})
		if err != nil {
			return nil, catalog.ErrUnavailable
		}
		edition := strings.TrimSpace(game.Description)
		if strings.EqualFold(edition, strings.TrimSpace(game.Name)) {
			edition = ""
		}
		candidates = append(candidates, catalog.GameCandidate{
			Provider:       "hasheous",
			ProviderID:     strconv.FormatInt(game.ID, 10),
			Title:          game.Name,
			Edition:        edition,
			Platform:       game.Platform.Name,
			Publisher:      game.Publisher.Name,
			Year:           game.Year,
			Region:         game.Country,
			Language:       game.Language,
			SelectionToken: token,
		})
	}
	return candidates, nil
}

func (p *Provider) Match(ctx context.Context, selectionToken string) (*catalog.ProviderMatch, error) {
	selection, err := decodeSelection(selectionToken)
	if err != nil {
		return nil, catalog.ErrInvalid
	}
	match, err := p.Identify(ctx, catalog.Fingerprint{
		Platform: selection.Platform,
		CRC32:    strings.ToLower(selection.CRC32),
		MD5:      strings.ToLower(selection.MD5),
		SHA1:     strings.ToLower(selection.SHA1),
		SHA256:   strings.ToLower(selection.SHA256),
	})
	if err != nil {
		return nil, err
	}
	if match.Metadata == nil {
		match.Metadata = make(map[string]any)
	}
	match.Metadata["signature_game_id"] = selection.GameID
	return match, nil
}

func (p *Provider) callMCP(ctx context.Context, tool string, arguments any, destination any) error {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      tool,
			"arguments": arguments,
		},
	})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/api/v1/Mcp", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("User-Agent", "OmniSave/1")
	response, err := p.client.Do(request)
	if err != nil {
		return catalog.ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return catalog.ErrUnavailable
	}
	var envelope struct {
		Result struct {
			IsError           bool            `json:"isError"`
			StructuredContent json.RawMessage `json:"structuredContent"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxLookupResponse))
	if err := decoder.Decode(&envelope); err != nil || envelope.Error != nil || envelope.Result.IsError || len(envelope.Result.StructuredContent) == 0 {
		return catalog.ErrUnavailable
	}
	if err := json.Unmarshal(envelope.Result.StructuredContent, destination); err != nil {
		return catalog.ErrUnavailable
	}
	return nil
}

func (p *Provider) OpenMedia(ctx context.Context, reference catalog.MediaReference) (string, io.ReadCloser, error) {
	if !imageIDPattern.MatchString(reference.ProviderID) {
		return "", nil, catalog.ErrInvalid
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		p.baseURL+"/api/v1/images/"+url.PathEscape(reference.ProviderID), nil)
	if err != nil {
		return "", nil, err
	}
	request.Header.Set("Accept", "image/*")
	request.Header.Set("User-Agent", "OmniSave/1")
	response, err := p.client.Do(request)
	if err != nil {
		return "", nil, catalog.ErrUnavailable
	}
	if response.StatusCode == http.StatusNotFound {
		response.Body.Close()
		return "", nil, catalog.ErrNotFound
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		response.Body.Close()
		return "", nil, catalog.ErrUnavailable
	}
	return response.Header.Get("Content-Type"), response.Body, nil
}

type lookupResponse struct {
	ID         int64          `json:"id"`
	Name       string         `json:"name"`
	Platform   namedObject    `json:"platform"`
	Publisher  namedObject    `json:"publisher"`
	Metadata   []metadataItem `json:"metadata"`
	Signatures struct {
		NoIntros []signatureResult `json:"NoIntros"`
	} `json:"signatures"`
	Attributes []attributeItem `json:"attributes"`
}

type namedObject struct {
	Name string `json:"name"`
}

type metadataItem struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Status string `json:"status"`
	Link   string `json:"link"`
}

type signatureResult struct {
	Game struct {
		SortingName string `json:"sortingName"`
		System      string `json:"system"`
	} `json:"game"`
	ROM struct {
		ID         string            `json:"id"`
		Name       string            `json:"name"`
		Size       int64             `json:"size"`
		CRC        string            `json:"crc"`
		MD5        string            `json:"md5"`
		SHA1       string            `json:"sha1"`
		SHA256     string            `json:"sha256"`
		Country    map[string]string `json:"country"`
		Language   map[string]string `json:"language"`
		Attributes map[string]string `json:"attributes"`
	} `json:"rom"`
}

type attributeItem struct {
	Name  string          `json:"attributeName"`
	Value json.RawMessage `json:"value"`
}

type mcpGame struct {
	ID          int64       `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Year        string      `json:"year"`
	Publisher   namedObject `json:"publisher"`
	Platform    struct {
		Name string `json:"name"`
	} `json:"platform"`
	Country  string   `json:"country"`
	Language string   `json:"language"`
	ROMs     []mcpROM `json:"roms"`
}

type mcpROM struct {
	Name   string `json:"name"`
	CRC    string `json:"crc"`
	MD5    string `json:"md5"`
	SHA1   string `json:"sha1"`
	SHA256 string `json:"sha256"`
}

type matchSelection struct {
	GameID   int64  `json:"game_id"`
	Platform string `json:"platform"`
	CRC32    string `json:"crc32,omitempty"`
	MD5      string `json:"md5,omitempty"`
	SHA1     string `json:"sha1,omitempty"`
	SHA256   string `json:"sha256,omitempty"`
}

func searchPlatform(platform string) string {
	platform = strings.TrimSpace(platform)
	normalized := strings.ToLower(platform)
	switch {
	case normalized == "snes",
		normalized == "super famicom",
		strings.Contains(normalized, "super nintendo entertainment system"):
		return "Super Nintendo Entertainment System"
	default:
		return platform
	}
}

func representativeROM(game mcpGame) (mcpROM, bool) {
	bestScore := -1
	var best mcpROM
	target := strings.ToLower(strings.TrimSpace(game.Description))
	for _, rom := range game.ROMs {
		if !validROMHashes(rom) {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(rom.Name))
		if dot := strings.LastIndexByte(name, '.'); dot > 0 {
			name = name[:dot]
		}
		score := 0
		if target != "" && name == target {
			score += 10
		} else if target != "" && strings.Contains(name, target) {
			score += 5
		}
		if rom.SHA256 != "" {
			score += 4
		} else if rom.SHA1 != "" {
			score += 3
		} else if rom.MD5 != "" {
			score += 2
		} else {
			score++
		}
		if score > bestScore {
			bestScore = score
			best = rom
		}
	}
	return best, bestScore >= 0
}

func validROMHashes(rom mcpROM) bool {
	provided := false
	for value, size := range map[string]int{rom.CRC: 8, rom.MD5: 32, rom.SHA1: 40, rom.SHA256: 64} {
		if value == "" {
			continue
		}
		provided = true
		if len(value) != size {
			return false
		}
		if _, err := hex.DecodeString(value); err != nil {
			return false
		}
	}
	return provided
}

func encodeSelection(selection matchSelection) (string, error) {
	data, err := json.Marshal(selection)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeSelection(token string) (matchSelection, error) {
	var selection matchSelection
	if len(token) == 0 || len(token) > 2048 {
		return selection, catalog.ErrInvalid
	}
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || json.Unmarshal(data, &selection) != nil || selection.GameID <= 0 || len(selection.Platform) > 256 {
		return matchSelection{}, catalog.ErrInvalid
	}
	if !validROMHashes(mcpROM{CRC: selection.CRC32, MD5: selection.MD5, SHA1: selection.SHA1, SHA256: selection.SHA256}) {
		return matchSelection{}, catalog.ErrInvalid
	}
	return selection, nil
}

func bestSignature(signatures []signatureResult, fingerprint catalog.Fingerprint) (signatureResult, bool) {
	bestScore := 0
	bestIndex := -1
	for index, signature := range signatures {
		score, matches := signatureScore(signature, fingerprint)
		if matches && score > bestScore {
			bestScore = score
			bestIndex = index
		}
	}
	if bestIndex < 0 {
		return signatureResult{}, false
	}
	return signatures[bestIndex], true
}

func signatureScore(signature signatureResult, fingerprint catalog.Fingerprint) (int, bool) {
	pairs := [][2]string{
		{fingerprint.CRC32, signature.ROM.CRC},
		{fingerprint.MD5, signature.ROM.MD5},
		{fingerprint.SHA1, signature.ROM.SHA1},
		{fingerprint.SHA256, signature.ROM.SHA256},
	}
	score := 0
	for _, pair := range pairs {
		if pair[0] == "" || pair[1] == "" {
			continue
		}
		if !strings.EqualFold(pair[0], pair[1]) {
			return 0, false
		}
		score++
	}
	return score, score > 0
}

func compileAttributes(items []attributeItem) (map[string]any, []catalog.MediaReference) {
	metadata := make(map[string]any)
	attributions := make(map[string]string)
	for _, item := range items {
		if !strings.HasSuffix(item.Name, "Attribution") {
			continue
		}
		var value string
		if json.Unmarshal(item.Value, &value) == nil {
			attributions[strings.TrimSuffix(item.Name, "Attribution")] = value
		}
	}
	var media []catalog.MediaReference
	for _, item := range items {
		switch item.Name {
		case "AIDescription":
			var value string
			if json.Unmarshal(item.Value, &value) == nil {
				metadata["description"] = value
				metadata["description_source"] = "hasheous-ai"
			}
		case "Tags", "Language", "SearchAliases":
			var value any
			if json.Unmarshal(item.Value, &value) == nil {
				metadata[strings.ToLower(item.Name)] = value
			}
		case "Logo", "Screenshot1", "Screenshot2", "Screenshot3", "Screenshot4":
			var imageID string
			if json.Unmarshal(item.Value, &imageID) != nil || !imageIDPattern.MatchString(imageID) {
				continue
			}
			kind, position := "screenshot", 0
			if item.Name == "Logo" {
				kind = "cover"
			} else if _, err := fmt.Sscanf(item.Name, "Screenshot%d", &position); err == nil {
				position--
			}
			media = append(media, catalog.MediaReference{
				Kind:        kind,
				Position:    position,
				ProviderID:  imageID,
				Attribution: attributions[item.Name],
			})
		}
	}
	return metadata, media
}

func sortedValues(values map[string]string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !slices.Contains(result, value) {
			result = append(result, value)
		}
	}
	slices.Sort(result)
	return result
}

func joinValues(values map[string]string) string {
	return strings.Join(sortedValues(values), ", ")
}

var _ catalog.Provider = (*Provider)(nil)
