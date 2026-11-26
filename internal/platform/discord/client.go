// Package discord is the OAuth2 client used by Discord account linking. It only
// performs the three calls the link flow needs; no session or account state is
// created here.
package discord

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

const (
	// DefaultTokenURL is Discord's token endpoint.
	DefaultTokenURL = "https://discord.com/api/oauth2/token"
	// DefaultAPIBaseURL is the prefix for the user endpoints.
	DefaultAPIBaseURL = "https://discord.com/api"
	// requestTimeout bounds one provider call.
	requestTimeout = 10 * time.Second
	// maxErrorBody bounds how much of a failure response may be logged.
	maxErrorBody = 512
)

// Client is the live Discord API implementation of service.DiscordAPI.
type Client struct {
	HTTP       *http.Client
	TokenURL   string
	APIBaseURL string
}

// NewClient builds a client with Discord's endpoints. A nil http.Client gets a
// bounded one.
func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: requestTimeout}
	}
	return &Client{HTTP: httpClient, TokenURL: DefaultTokenURL, APIBaseURL: DefaultAPIBaseURL}
}

// ExchangeCode trades the authorization code for an access token. The client
// credentials come from the caller so an administrator's settings change is
// honoured without restarting the process.
func (c *Client) ExchangeCode(ctx context.Context, clientID, clientSecret, redirectURI, code string) (domain.DiscordToken, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return domain.DiscordToken{}, fmt.Errorf("discord: build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	credentials := base64.StdEncoding.EncodeToString([]byte(clientID + ":" + clientSecret))
	req.Header.Set("Authorization", "Basic "+credentials)

	var payload struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := c.do(req, &payload); err != nil {
		return domain.DiscordToken{}, err
	}
	if payload.AccessToken == "" {
		return domain.DiscordToken{}, fmt.Errorf("discord: token response had no access token")
	}
	return domain.DiscordToken{AccessToken: payload.AccessToken, TokenType: payload.TokenType,
		ExpiresIn: payload.ExpiresIn}, nil
}

// CurrentUser reads GET /users/@me, the identity that will be stored.
func (c *Client) CurrentUser(ctx context.Context, accessToken string) (domain.DiscordProfile, error) {
	req, err := c.bearerRequest(ctx, http.MethodGet, "/users/@me", accessToken)
	if err != nil {
		return domain.DiscordProfile{}, err
	}
	var payload struct {
		ID         string `json:"id"`
		Username   string `json:"username"`
		GlobalName string `json:"global_name"`
	}
	if err := c.do(req, &payload); err != nil {
		return domain.DiscordProfile{}, err
	}
	if payload.ID == "" {
		return domain.DiscordProfile{}, fmt.Errorf("discord: profile response had no id")
	}
	return domain.DiscordProfile{ID: payload.ID, UserName: payload.Username, GlobalName: payload.GlobalName}, nil
}

// GuildMember reports whether the token's user is a member of guildID. It reads
// GET /users/@me/guilds, which per Discord's documentation "requires the guilds
// OAuth2 scope": a token minted with identify alone gets a 403, which is returned
// here as an error so the caller records "unverified" rather than "not a member".
func (c *Client) GuildMember(ctx context.Context, accessToken, guildID string) (bool, error) {
	req, err := c.bearerRequest(ctx, http.MethodGet, "/users/@me/guilds", accessToken)
	if err != nil {
		return false, err
	}
	var guilds []struct {
		ID string `json:"id"`
	}
	if err := c.do(req, &guilds); err != nil {
		return false, err
	}
	for _, guild := range guilds {
		if guild.ID == guildID {
			return true, nil
		}
	}
	return false, nil
}

func (c *Client) bearerRequest(ctx context.Context, method, path, accessToken string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.APIBaseURL, "/")+path, nil)
	if err != nil {
		return nil, fmt.Errorf("discord: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	return req, nil
}

// do executes the request and decodes a 2xx JSON body. Failures are reduced to a
// status and a bounded body excerpt so no token or secret can reach the logs.
func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("discord: %s %s: %w", req.Method, req.URL.Path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return fmt.Errorf("discord: %s %s returned %d: %s", req.Method, req.URL.Path,
			resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
		return fmt.Errorf("discord: decode %s %s: %w", req.Method, req.URL.Path, err)
	}
	return nil
}
