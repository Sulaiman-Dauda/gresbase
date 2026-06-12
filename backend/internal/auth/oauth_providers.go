package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gresbase/gresbase/internal/database"
	"github.com/rs/zerolog/log"
)

// ---------------------------------------------------------------------------
// OAuth Provider Registry
// ---------------------------------------------------------------------------

// OAuthIdentityProvider defines the interface for an actual OAuth identity provider implementation.
type OAuthIdentityProvider interface {
	Name() string
	GetAuthURL(state, redirectURL string) (string, error)
	ExchangeCode(ctx context.Context, code, redirectURL string) (*OAuthUserInfo, error)
	IsEnabled() bool
}

// ProviderRegistry holds all configured OAuth providers.
type ProviderRegistry struct {
	providers map[string]OAuthIdentityProvider
	db        *database.DB
}

// NewProviderRegistry creates an OAuth provider registry.
func NewProviderRegistry(db *database.DB) *ProviderRegistry {
	return &ProviderRegistry{
		providers: make(map[string]OAuthIdentityProvider),
		db:        db,
	}
}

// Register adds a provider to the registry.
func (r *ProviderRegistry) Register(p OAuthIdentityProvider) {
	r.providers[p.Name()] = p
}

// Get returns a registered provider by name.
func (r *ProviderRegistry) Get(name string) (OAuthIdentityProvider, error) {
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("unknown OAuth provider: %s", name)
	}
	if !p.IsEnabled() {
		return nil, fmt.Errorf("OAuth provider not configured: %s", name)
	}
	return p, nil
}

// List returns names of all registered providers.
func (r *ProviderRegistry) List() []string {
	var names []string
	for name, p := range r.providers {
		if p.IsEnabled() {
			names = append(names, name)
		}
	}
	return names
}

// ---------------------------------------------------------------------------
// Google OAuth Provider
// ---------------------------------------------------------------------------

// GoogleProvider implements OAuth via Google.
type GoogleProvider struct {
	clientID     string
	clientSecret string
	enabled      bool
	httpClient   *http.Client
}

// NewGoogleProvider creates a Google OAuth provider.
func NewGoogleProvider(clientID, clientSecret string) *GoogleProvider {
	return &GoogleProvider{
		clientID:     clientID,
		clientSecret: clientSecret,
		enabled:      clientID != "" && clientSecret != "",
		httpClient:   &http.Client{Timeout: 15 * time.Second},
	}
}

func (p *GoogleProvider) Name() string    { return "google" }
func (p *GoogleProvider) IsEnabled() bool { return p.enabled }

func (p *GoogleProvider) GetAuthURL(state, redirectURL string) (string, error) {
	u, _ := url.Parse("https://accounts.google.com/o/oauth2/v2/auth")
	q := u.Query()
	q.Set("client_id", p.clientID)
	q.Set("redirect_uri", redirectURL)
	q.Set("response_type", "code")
	q.Set("scope", "https://www.googleapis.com/auth/userinfo.email https://www.googleapis.com/auth/userinfo.profile")
	q.Set("state", state)
	q.Set("access_type", "offline")
	q.Set("prompt", "consent")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (p *GoogleProvider) ExchangeCode(ctx context.Context, code, redirectURL string) (*OAuthUserInfo, error) {
	// Exchange the authorization code for a token
	tokenURL := "https://oauth2.googleapis.com/token"
	data := url.Values{
		"client_id":     {p.clientID},
		"client_secret": {p.clientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURL},
		"grant_type":    {"authorization_code"},
	}

	req, _ := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		TokenType    string `json:"token_type"`
		Error        string `json:"error"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}
	if tokenResp.Error != "" {
		return nil, fmt.Errorf("token exchange error: %s", tokenResp.Error)
	}

	// Fetch user info
	userInfo, err := p.fetchUserInfo(ctx, tokenResp.AccessToken)
	if err != nil {
		return nil, err
	}

	userInfo.Provider = "google"
	userInfo.RawJSON, _ = json.Marshal(tokenResp)
	return userInfo, nil
}

func (p *GoogleProvider) fetchUserInfo(ctx context.Context, accessToken string) (*OAuthUserInfo, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("userinfo fetch failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var googleUser struct {
		ID       string `json:"id"`
		Email    string `json:"email"`
		Name     string `json:"name"`
		Picture  string `json:"picture"`
		Verified bool   `json:"verified_email"`
	}
	if err := json.Unmarshal(body, &googleUser); err != nil {
		return nil, fmt.Errorf("failed to parse user info: %w", err)
	}

	rawJSON, _ := json.Marshal(googleUser)

	return &OAuthUserInfo{
		Provider:   "google",
		ProviderID: googleUser.ID,
		Email:      googleUser.Email,
		Name:       googleUser.Name,
		AvatarURL:  googleUser.Picture,
		RawJSON:    rawJSON,
	}, nil
}

// ---------------------------------------------------------------------------
// GitHub OAuth Provider
// ---------------------------------------------------------------------------

// GithubProvider implements OAuth via GitHub.
type GithubProvider struct {
	clientID     string
	clientSecret string
	enabled      bool
	httpClient   *http.Client
}

// NewGithubProvider creates a GitHub OAuth provider.
func NewGithubProvider(clientID, clientSecret string) *GithubProvider {
	return &GithubProvider{
		clientID:     clientID,
		clientSecret: clientSecret,
		enabled:      clientID != "" && clientSecret != "",
		httpClient:   &http.Client{Timeout: 15 * time.Second},
	}
}

func (p *GithubProvider) Name() string    { return "github" }
func (p *GithubProvider) IsEnabled() bool { return p.enabled }

func (p *GithubProvider) GetAuthURL(state, redirectURL string) (string, error) {
	u, _ := url.Parse("https://github.com/login/oauth/authorize")
	q := u.Query()
	q.Set("client_id", p.clientID)
	q.Set("redirect_uri", redirectURL)
	q.Set("scope", "user:email")
	q.Set("state", state)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (p *GithubProvider) ExchangeCode(ctx context.Context, code, redirectURL string) (*OAuthUserInfo, error) {
	// Exchange code for token
	tokenURL := "https://github.com/login/oauth/access_token"
	data := url.Values{
		"client_id":     {p.clientID},
		"client_secret": {p.clientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURL},
	}

	req, _ := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var tokenResp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		Scope       string `json:"scope"`
		Error       string `json:"error"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}
	if tokenResp.Error != "" {
		return nil, fmt.Errorf("token error: %s", tokenResp.Error)
	}

	// Fetch user info
	userInfo, err := p.fetchUserInfo(ctx, tokenResp.AccessToken)
	if err != nil {
		return nil, err
	}

	// Fetch primary email
	email, err := p.fetchPrimaryEmail(ctx, tokenResp.AccessToken)
	if err == nil && email != "" {
		userInfo.Email = email
	}

	userInfo.Provider = "github"
	return userInfo, nil
}

func (p *GithubProvider) fetchUserInfo(ctx context.Context, accessToken string) (*OAuthUserInfo, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github user fetch failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var ghUser struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		Email     string `json:"email"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.Unmarshal(body, &ghUser); err != nil {
		return nil, fmt.Errorf("failed to parse GitHub user: %w", err)
	}

	rawJSON, _ := json.Marshal(ghUser)

	return &OAuthUserInfo{
		Provider:   "github",
		ProviderID: fmt.Sprintf("%d", ghUser.ID),
		Email:      ghUser.Email,
		Name:       ghUser.Name,
		AvatarURL:  ghUser.AvatarURL,
		RawJSON:    rawJSON,
	}, nil
}

func (p *GithubProvider) fetchPrimaryEmail(ctx context.Context, accessToken string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user/emails", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := json.Unmarshal(body, &emails); err != nil {
		return "", err
	}

	for _, e := range emails {
		if e.Primary && e.Verified {
			return e.Email, nil
		}
	}
	// Fallback to first verified email
	for _, e := range emails {
		if e.Verified {
			return e.Email, nil
		}
	}
	return "", nil
}

// ---------------------------------------------------------------------------
// Microsoft OAuth Provider
// ---------------------------------------------------------------------------

// MicrosoftProvider implements OAuth via Microsoft (Azure AD).
type MicrosoftProvider struct {
	clientID     string
	clientSecret string
	tenant       string // "common", "organizations", "consumers", or tenant ID
	enabled      bool
	httpClient   *http.Client
}

// NewMicrosoftProvider creates a Microsoft OAuth provider.
func NewMicrosoftProvider(clientID, clientSecret, tenant string) *MicrosoftProvider {
	if tenant == "" {
		tenant = "common"
	}
	return &MicrosoftProvider{
		clientID:     clientID,
		clientSecret: clientSecret,
		tenant:       tenant,
		enabled:      clientID != "" && clientSecret != "",
		httpClient:   &http.Client{Timeout: 15 * time.Second},
	}
}

func (p *MicrosoftProvider) Name() string    { return "microsoft" }
func (p *MicrosoftProvider) IsEnabled() bool { return p.enabled }

func (p *MicrosoftProvider) GetAuthURL(state, redirectURL string) (string, error) {
	u, _ := url.Parse(fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/authorize", p.tenant))
	q := u.Query()
	q.Set("client_id", p.clientID)
	q.Set("redirect_uri", redirectURL)
	q.Set("response_type", "code")
	q.Set("scope", "openid profile email User.Read")
	q.Set("state", state)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (p *MicrosoftProvider) ExchangeCode(ctx context.Context, code, redirectURL string) (*OAuthUserInfo, error) {
	tokenURL := fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", p.tenant)
	data := url.Values{
		"client_id":     {p.clientID},
		"client_secret": {p.clientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURL},
		"grant_type":    {"authorization_code"},
		"scope":         {"openid profile email User.Read"},
	}

	req, _ := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		Error        string `json:"error"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}
	if tokenResp.Error != "" {
		return nil, fmt.Errorf("token error: %s — %s", tokenResp.Error, string(body))
	}

	// Fetch user info from Microsoft Graph
	userInfo, err := p.fetchUserInfo(ctx, tokenResp.AccessToken)
	if err != nil {
		return nil, err
	}

	userInfo.Provider = "microsoft"
	return userInfo, nil
}

func (p *MicrosoftProvider) fetchUserInfo(ctx context.Context, accessToken string) (*OAuthUserInfo, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://graph.microsoft.com/v1.0/me", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("graph API fetch failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var msUser struct {
		ID                string `json:"id"`
		UserPrincipalName string `json:"userPrincipalName"`
		DisplayName       string `json:"displayName"`
		Mail              string `json:"mail"`
	}
	if err := json.Unmarshal(body, &msUser); err != nil {
		return nil, fmt.Errorf("failed to parse MS user: %w", err)
	}

	email := msUser.Mail
	if email == "" {
		email = msUser.UserPrincipalName
	}

	rawJSON, _ := json.Marshal(msUser)
	return &OAuthUserInfo{
		Provider:   "microsoft",
		ProviderID: msUser.ID,
		Email:      email,
		Name:       msUser.DisplayName,
		RawJSON:    rawJSON,
	}, nil
}

// ---------------------------------------------------------------------------
// GitLab OAuth Provider
// ---------------------------------------------------------------------------

// GitlabProvider implements OAuth via GitLab.
type GitlabProvider struct {
	clientID     string
	clientSecret string
	baseURL      string
	enabled      bool
	httpClient   *http.Client
}

// NewGitlabProvider creates a GitLab OAuth provider.
func NewGitlabProvider(clientID, clientSecret, baseURL string) *GitlabProvider {
	if baseURL == "" {
		baseURL = "https://gitlab.com"
	}
	return &GitlabProvider{
		clientID:     clientID,
		clientSecret: clientSecret,
		baseURL:      baseURL,
		enabled:      clientID != "" && clientSecret != "",
		httpClient:   &http.Client{Timeout: 15 * time.Second},
	}
}

func (p *GitlabProvider) Name() string    { return "gitlab" }
func (p *GitlabProvider) IsEnabled() bool { return p.enabled }

func (p *GitlabProvider) GetAuthURL(state, redirectURL string) (string, error) {
	u, _ := url.Parse(p.baseURL + "/oauth/authorize")
	q := u.Query()
	q.Set("client_id", p.clientID)
	q.Set("redirect_uri", redirectURL)
	q.Set("response_type", "code")
	q.Set("scope", "read_user")
	q.Set("state", state)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (p *GitlabProvider) ExchangeCode(ctx context.Context, code, redirectURL string) (*OAuthUserInfo, error) {
	tokenURL := p.baseURL + "/oauth/token"
	data := url.Values{
		"client_id":     {p.clientID},
		"client_secret": {p.clientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURL},
		"grant_type":    {"authorization_code"},
	}

	req, _ := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Error        string `json:"error"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse GitLab token: %w", err)
	}
	if tokenResp.Error != "" {
		return nil, fmt.Errorf("GitLab error: %s", tokenResp.Error)
	}

	// Fetch user info
	req2, _ := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/api/v4/user", nil)
	req2.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)

	resp2, err := p.httpClient.Do(req2)
	if err != nil {
		return nil, fmt.Errorf("GitLab user fetch failed: %w", err)
	}
	defer resp2.Body.Close()

	body2, _ := io.ReadAll(resp2.Body)
	var glUser struct {
		ID        int64  `json:"id"`
		Username  string `json:"username"`
		Name      string `json:"name"`
		Email     string `json:"email"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.Unmarshal(body2, &glUser); err != nil {
		return nil, fmt.Errorf("failed to parse GitLab user: %w", err)
	}

	rawJSON, _ := json.Marshal(glUser)
	return &OAuthUserInfo{
		Provider:   "gitlab",
		ProviderID: fmt.Sprintf("%d", glUser.ID),
		Email:      glUser.Email,
		Name:       glUser.Name,
		AvatarURL:  glUser.AvatarURL,
		RawJSON:    rawJSON,
	}, nil
}

// ---------------------------------------------------------------------------
// Discord OAuth Provider
// ---------------------------------------------------------------------------

// DiscordProvider implements OAuth via Discord.
type DiscordProvider struct {
	clientID     string
	clientSecret string
	enabled      bool
	httpClient   *http.Client
}

// NewDiscordProvider creates a Discord OAuth provider.
func NewDiscordProvider(clientID, clientSecret string) *DiscordProvider {
	return &DiscordProvider{
		clientID:     clientID,
		clientSecret: clientSecret,
		enabled:      clientID != "" && clientSecret != "",
		httpClient:   &http.Client{Timeout: 15 * time.Second},
	}
}

func (p *DiscordProvider) Name() string    { return "discord" }
func (p *DiscordProvider) IsEnabled() bool { return p.enabled }

func (p *DiscordProvider) GetAuthURL(state, redirectURL string) (string, error) {
	u, _ := url.Parse("https://discord.com/api/oauth2/authorize")
	q := u.Query()
	q.Set("client_id", p.clientID)
	q.Set("redirect_uri", redirectURL)
	q.Set("response_type", "code")
	q.Set("scope", "identify email")
	q.Set("state", state)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (p *DiscordProvider) ExchangeCode(ctx context.Context, code, redirectURL string) (*OAuthUserInfo, error) {
	tokenURL := "https://discord.com/api/oauth2/token"
	data := url.Values{
		"client_id":     {p.clientID},
		"client_secret": {p.clientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURL},
		"grant_type":    {"authorization_code"},
	}

	req, _ := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var tokenResp struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	json.Unmarshal(body, &tokenResp)
	if tokenResp.Error != "" {
		return nil, fmt.Errorf("discord error: %s", tokenResp.Error)
	}

	// Fetch user
	req2, _ := http.NewRequestWithContext(ctx, "GET", "https://discord.com/api/users/@me", nil)
	req2.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)

	resp2, err := p.httpClient.Do(req2)
	if err != nil {
		return nil, err
	}
	defer resp2.Body.Close()

	body2, _ := io.ReadAll(resp2.Body)
	var dcUser struct {
		ID            string `json:"id"`
		Username      string `json:"username"`
		Email         string `json:"email"`
		Avatar        string `json:"avatar"`
		Discriminator string `json:"discriminator"`
	}
	json.Unmarshal(body2, &dcUser)

	avatarURL := ""
	if dcUser.Avatar != "" {
		avatarURL = fmt.Sprintf("https://cdn.discordapp.com/avatars/%s/%s.png", dcUser.ID, dcUser.Avatar)
	}

	rawJSON, _ := json.Marshal(dcUser)
	userInfo := &OAuthUserInfo{
		Provider:   "discord",
		ProviderID: dcUser.ID,
		Email:      dcUser.Email,
		Name:       dcUser.Username,
		AvatarURL:  avatarURL,
		RawJSON:    rawJSON,
	}

	return userInfo, nil
}

// ---------------------------------------------------------------------------
// Build registry from config
// ---------------------------------------------------------------------------

// BuildOAuthRegistry creates a provider registry from the app config.
func BuildOAuthRegistry(db *database.DB, providers map[string]struct {
	ClientID     string
	ClientSecret string
}) *ProviderRegistry {
	reg := NewProviderRegistry(db)

	for name, p := range providers {
		switch name {
		case "google":
			reg.Register(NewGoogleProvider(p.ClientID, p.ClientSecret))
		case "github":
			reg.Register(NewGithubProvider(p.ClientID, p.ClientSecret))
		case "microsoft":
			reg.Register(NewMicrosoftProvider(p.ClientID, p.ClientSecret, "common"))
		case "gitlab":
			reg.Register(NewGitlabProvider(p.ClientID, p.ClientSecret, ""))
		case "discord":
			reg.Register(NewDiscordProvider(p.ClientID, p.ClientSecret))
		default:
			if sp, ok := NewStandardProvider(name, p.ClientID, p.ClientSecret); ok {
				reg.Register(sp)
			} else {
				log.Warn().Str("provider", name).Msg("Unknown OAuth provider, skipping")
			}
		}
	}

	return reg
}

// Ensure unused import compiles
var _ = log.Logger
