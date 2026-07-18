// Package igdb adapts the IGDB API for game metadata and search.
package igdb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
)

const (
	defaultBaseURL      = "https://api.igdb.com/v4"
	defaultTokenURL     = "https://id.twitch.tv/oauth2/token"
	defaultImageBaseURL = "https://images.igdb.com/igdb/image/upload"
	maxResponseSize     = 4 << 20
)

var imageIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// Config contains the server-owned IGDB credentials and provider controls.
type Config struct {
	ClientID          string
	ClientSecret      string
	BaseURL           string
	TokenURL          string
	ImageBaseURL      string
	RequestsPerSecond int
	SearchCacheTTL    time.Duration
}

type Provider struct {
	config  Config
	client  *http.Client
	limiter *requestLimiter
	slots   chan struct{}

	tokenMu     sync.Mutex
	accessToken string
	tokenExpiry time.Time

	cacheMu sync.Mutex
	search  map[string]cachedSearch
}

type cachedSearch struct {
	expires    time.Time
	candidates []catalog.GameCandidate
}

// New creates an IGDB provider using Twitch client-credentials authentication.
func New(config Config, client *http.Client) (*Provider, error) {
	config.ClientID = strings.TrimSpace(config.ClientID)
	config.ClientSecret = strings.TrimSpace(config.ClientSecret)
	if config.ClientID == "" || config.ClientSecret == "" {
		return nil, fmt.Errorf("igdb credentials are required")
	}
	if config.BaseURL == "" {
		config.BaseURL = defaultBaseURL
	}
	if config.TokenURL == "" {
		config.TokenURL = defaultTokenURL
	}
	if config.ImageBaseURL == "" {
		config.ImageBaseURL = defaultImageBaseURL
	}
	for _, endpoint := range []string{config.BaseURL, config.TokenURL, config.ImageBaseURL} {
		parsed, err := url.Parse(endpoint)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return nil, fmt.Errorf("invalid IGDB endpoint")
		}
	}
	if config.RequestsPerSecond == 0 {
		config.RequestsPerSecond = 4
	}
	if config.RequestsPerSecond < 1 || config.RequestsPerSecond > 4 {
		return nil, fmt.Errorf("IGDB requests per second must be between 1 and 4")
	}
	if config.SearchCacheTTL == 0 {
		config.SearchCacheTTL = 5 * time.Minute
	}
	if config.SearchCacheTTL < 0 {
		return nil, fmt.Errorf("IGDB search cache TTL must not be negative")
	}
	if client == nil {
		client = http.DefaultClient
	}
	config.BaseURL = strings.TrimRight(config.BaseURL, "/")
	config.TokenURL = strings.TrimRight(config.TokenURL, "/")
	config.ImageBaseURL = strings.TrimRight(config.ImageBaseURL, "/")
	return &Provider{
		config:  config,
		client:  client,
		limiter: newRequestLimiter(config.RequestsPerSecond),
		slots:   make(chan struct{}, 8),
		search:  make(map[string]cachedSearch),
	}, nil
}

func (p *Provider) Name() string { return "igdb" }

func (p *Provider) Resolve(ctx context.Context, evidence catalog.ResolveGame) (*catalog.ProviderMatch, error) {
	if value := identifierValue(evidence.Identifiers, "igdb.game"); value != "" {
		gameID, valid := positiveID(value)
		if !valid {
			return nil, catalog.ErrInvalid
		}
		game, err := p.gameByID(ctx, gameID)
		if err != nil {
			return nil, err
		}
		return game.match(nil), nil
	}
	steamID := identifierValue(evidence.Identifiers, "steam.app")
	if steamID == "" {
		return nil, catalog.ErrNotFound
	}
	if _, valid := positiveID(steamID); !valid {
		return nil, catalog.ErrInvalid
	}
	query := fmt.Sprintf("fields uid,%s; where external_game_source = 1 & uid = \"%s\"; limit 1;", gameFields("game."), escapeQuery(steamID))
	var results []externalGame
	if err := p.query(ctx, "external_games", query, &results); err != nil {
		return nil, err
	}
	if len(results) == 0 || results[0].Game.ID == 0 {
		return nil, catalog.ErrNotFound
	}
	return results[0].Game.match([]catalog.GameIdentifier{{Namespace: "steam.app", Value: steamID}}), nil
}

func (p *Provider) Search(ctx context.Context, input catalog.SearchGames) ([]catalog.GameCandidate, error) {
	title := strings.TrimSpace(input.Title)
	if title == "" || input.Limit < 1 || input.Limit > 25 {
		return nil, catalog.ErrInvalid
	}
	cacheKey := strings.ToLower(fmt.Sprintf("%s\x00%s\x00%d", title, strings.TrimSpace(input.Platform), input.Limit))
	if candidates, found := p.cached(cacheKey); found {
		return candidates, nil
	}
	query := fmt.Sprintf("fields %s; search \"%s\"; limit %d;", gameFields(""), escapeQuery(title), input.Limit)
	var games []game
	if err := p.query(ctx, "games", query, &games); err != nil {
		return nil, err
	}
	candidates := make([]catalog.GameCandidate, 0, len(games))
	for _, game := range games {
		if game.ID == 0 || strings.TrimSpace(game.Name) == "" {
			continue
		}
		candidates = append(candidates, game.candidate(input.Platform))
	}
	p.storeCached(cacheKey, candidates)
	return slices.Clone(candidates), nil
}

func (p *Provider) Match(ctx context.Context, selectionToken string) (*catalog.ProviderMatch, error) {
	gameID, valid := positiveID(strings.TrimSpace(selectionToken))
	if !valid {
		return nil, catalog.ErrInvalid
	}
	game, err := p.gameByID(ctx, gameID)
	if err != nil {
		return nil, err
	}
	return game.match(nil), nil
}

func (p *Provider) OpenMedia(ctx context.Context, reference catalog.MediaReference) (string, io.ReadCloser, error) {
	if reference.Kind != "cover" || !imageIDPattern.MatchString(reference.ProviderID) {
		return "", nil, catalog.ErrInvalid
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		p.config.ImageBaseURL+"/t_cover_big/"+reference.ProviderID+".jpg", nil)
	if err != nil {
		return "", nil, err
	}
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

func (p *Provider) gameByID(ctx context.Context, gameID int64) (*game, error) {
	query := fmt.Sprintf("fields %s; where id = %d; limit 1;", gameFields(""), gameID)
	var games []game
	if err := p.query(ctx, "games", query, &games); err != nil {
		return nil, err
	}
	if len(games) == 0 || games[0].ID == 0 {
		return nil, catalog.ErrNotFound
	}
	return &games[0], nil
}

func (p *Provider) query(ctx context.Context, resource, query string, output any) error {
	for attempt := 0; attempt < 2; attempt++ {
		token, err := p.token(ctx)
		if err != nil {
			return err
		}
		if err := p.limiter.Wait(ctx); err != nil {
			return err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.config.BaseURL+"/"+resource, bytes.NewBufferString(query))
		if err != nil {
			return err
		}
		request.Header.Set("Client-ID", p.config.ClientID)
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Content-Type", "text/plain")
		if err := p.acquire(ctx); err != nil {
			return err
		}
		response, err := p.client.Do(request)
		if err != nil {
			p.release()
			return catalog.ErrUnavailable
		}
		if response.StatusCode == http.StatusUnauthorized && attempt == 0 {
			response.Body.Close()
			p.release()
			p.clearToken()
			continue
		}
		if response.StatusCode == http.StatusNotFound {
			response.Body.Close()
			p.release()
			return catalog.ErrNotFound
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			response.Body.Close()
			p.release()
			return catalog.ErrUnavailable
		}
		err = json.NewDecoder(io.LimitReader(response.Body, maxResponseSize)).Decode(output)
		response.Body.Close()
		p.release()
		if err != nil {
			return catalog.ErrUnavailable
		}
		return nil
	}
	return catalog.ErrUnavailable
}

func (p *Provider) acquire(ctx context.Context) error {
	select {
	case p.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *Provider) release() { <-p.slots }

func (p *Provider) token(ctx context.Context) (string, error) {
	p.tokenMu.Lock()
	defer p.tokenMu.Unlock()
	if p.accessToken != "" && time.Now().Add(30*time.Second).Before(p.tokenExpiry) {
		return p.accessToken, nil
	}
	endpoint, err := url.Parse(p.config.TokenURL)
	if err != nil {
		return "", catalog.ErrUnavailable
	}
	query := endpoint.Query()
	query.Set("client_id", p.config.ClientID)
	query.Set("client_secret", p.config.ClientSecret)
	query.Set("grant_type", "client_credentials")
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), nil)
	if err != nil {
		return "", err
	}
	response, err := p.client.Do(request)
	if err != nil {
		return "", catalog.ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", catalog.ErrUnavailable
	}
	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil || result.AccessToken == "" || result.ExpiresIn < 1 {
		return "", catalog.ErrUnavailable
	}
	p.accessToken = result.AccessToken
	p.tokenExpiry = time.Now().Add(time.Duration(result.ExpiresIn) * time.Second)
	return p.accessToken, nil
}

func (p *Provider) clearToken() {
	p.tokenMu.Lock()
	p.accessToken = ""
	p.tokenExpiry = time.Time{}
	p.tokenMu.Unlock()
}

func (p *Provider) cached(key string) ([]catalog.GameCandidate, bool) {
	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()
	entry, found := p.search[key]
	if !found || time.Now().After(entry.expires) {
		delete(p.search, key)
		return nil, false
	}
	return slices.Clone(entry.candidates), true
}

func (p *Provider) storeCached(key string, candidates []catalog.GameCandidate) {
	if p.config.SearchCacheTTL == 0 {
		return
	}
	p.cacheMu.Lock()
	p.search[key] = cachedSearch{
		expires:    time.Now().Add(p.config.SearchCacheTTL),
		candidates: slices.Clone(candidates),
	}
	p.cacheMu.Unlock()
}

type requestLimiter struct {
	mu       sync.Mutex
	next     time.Time
	interval time.Duration
}

func newRequestLimiter(requestsPerSecond int) *requestLimiter {
	return &requestLimiter{interval: time.Second / time.Duration(requestsPerSecond)}
}

func (l *requestLimiter) Wait(ctx context.Context) error {
	l.mu.Lock()
	now := time.Now()
	when := now
	if l.next.After(now) {
		when = l.next
	}
	l.next = when.Add(l.interval)
	l.mu.Unlock()
	if !when.After(now) {
		return nil
	}
	timer := time.NewTimer(time.Until(when))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type externalGame struct {
	UID  string `json:"uid"`
	Game game   `json:"game"`
}

type game struct {
	ID               int64             `json:"id"`
	Name             string            `json:"name"`
	Summary          string            `json:"summary"`
	FirstReleaseDate int64             `json:"first_release_date"`
	Platforms        []namedValue      `json:"platforms"`
	Cover            cover             `json:"cover"`
	Companies        []involvedCompany `json:"involved_companies"`
	Genres           []namedValue      `json:"genres"`
}

type namedValue struct {
	Name string `json:"name"`
}

type cover struct {
	ImageID string `json:"image_id"`
}

type involvedCompany struct {
	Company   namedValue `json:"company"`
	Publisher bool       `json:"publisher"`
}

func (g game) match(additional []catalog.GameIdentifier) *catalog.ProviderMatch {
	identifiers := append([]catalog.GameIdentifier{{Namespace: "igdb.game", Value: strconv.FormatInt(g.ID, 10)}}, additional...)
	metadata := make(map[string]any)
	if g.FirstReleaseDate > 0 {
		metadata["release_year"] = strconv.Itoa(time.Unix(g.FirstReleaseDate, 0).UTC().Year())
	}
	genres := names(g.Genres)
	if len(genres) > 0 {
		metadata["genres"] = genres
	}
	platformCompany, platformName := catalog.SplitPlatform(choosePlatform(g.Platforms, ""))
	match := &catalog.ProviderMatch{
		Source:          "igdb",
		Identifiers:     identifiers,
		Title:           strings.TrimSpace(g.Name),
		Platform:        platformName,
		PlatformCompany: platformCompany,
		Publisher:       publisher(g.Companies),
		Description:     strings.TrimSpace(g.Summary),
		Metadata:        metadata,
	}
	if imageIDPattern.MatchString(g.Cover.ImageID) {
		match.Media = []catalog.MediaReference{{
			Provider: "igdb", Kind: "cover", ProviderID: g.Cover.ImageID, Attribution: "IGDB",
		}}
	}
	return match
}

func (g game) candidate(platformHint string) catalog.GameCandidate {
	year := ""
	if g.FirstReleaseDate > 0 {
		year = strconv.Itoa(time.Unix(g.FirstReleaseDate, 0).UTC().Year())
	}
	id := strconv.FormatInt(g.ID, 10)
	return catalog.GameCandidate{
		Provider:       "igdb",
		ProviderID:     id,
		Title:          strings.TrimSpace(g.Name),
		Platform:       choosePlatform(g.Platforms, platformHint),
		Publisher:      publisher(g.Companies),
		Year:           year,
		SelectionToken: id,
	}
}

func gameFields(prefix string) string {
	fields := []string{
		"id", "name", "summary", "first_release_date", "platforms.name", "cover.image_id",
		"involved_companies.company.name", "involved_companies.publisher", "genres.name",
	}
	for index := range fields {
		fields[index] = prefix + fields[index]
	}
	return strings.Join(fields, ",")
}

func identifierValue(identifiers []catalog.GameIdentifier, namespace string) string {
	for _, identifier := range identifiers {
		if strings.EqualFold(strings.TrimSpace(identifier.Namespace), namespace) {
			return strings.TrimSpace(identifier.Value)
		}
	}
	return ""
}

func positiveID(value string) (int64, bool) {
	id, err := strconv.ParseInt(value, 10, 64)
	return id, err == nil && id > 0
}

func escapeQuery(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
}

func choosePlatform(platforms []namedValue, hint string) string {
	hint = strings.TrimSpace(hint)
	for _, platform := range platforms {
		if hint != "" && strings.EqualFold(strings.TrimSpace(platform.Name), hint) {
			return strings.TrimSpace(platform.Name)
		}
	}
	if len(platforms) > 0 {
		return strings.TrimSpace(platforms[0].Name)
	}
	return hint
}

func publisher(companies []involvedCompany) string {
	for _, company := range companies {
		if company.Publisher && strings.TrimSpace(company.Company.Name) != "" {
			return strings.TrimSpace(company.Company.Name)
		}
	}
	return ""
}

func names(values []namedValue) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		name := strings.TrimSpace(value.Name)
		if name != "" && !slices.Contains(result, name) {
			result = append(result, name)
		}
	}
	return result
}

var _ catalog.Provider = (*Provider)(nil)
