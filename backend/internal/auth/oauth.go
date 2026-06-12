package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gresbase/gresbase/internal/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// OAuthProvider defines an OAuth2 provider configuration.
type OAuthProvider struct {
	Name         string   `json:"name"`
	DisplayName  string   `json:"display_name"`
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"-"`
	RedirectURL  string   `json:"redirect_url,omitempty"`
	AuthURL      string   `json:"auth_url"`
	TokenURL     string   `json:"token_url"`
	UserInfoURL  string   `json:"user_info_url"`
	Scopes       []string `json:"scopes"`
	Enabled      bool     `json:"enabled"`
}

// OAuthUserInfo is the normalized user info from any OAuth provider.
type OAuthUserInfo struct {
	Provider   string          `json:"provider"`
	ProviderID string          `json:"provider_id"`
	Email      string          `json:"email"`
	Name       string          `json:"name"`
	AvatarURL  string          `json:"avatar_url"`
	RawJSON    json.RawMessage `json:"raw"`
}

// OAuthState stores an in-flight OAuth authorization.
type OAuthState struct {
	ID           string    `json:"id"`
	Provider     string    `json:"provider"`
	Redirect     string    `json:"redirect"`
	State        string    `json:"state"`
	CodeVerifier string    `json:"code_verifier"`
	CreatedAt    time.Time `json:"created_at"`
}

// OAuthAuthMethodProvider is the provider descriptor returned
// by the auth-methods endpoint.
type OAuthAuthMethodProvider struct {
	Name                string `json:"name"`
	DisplayName         string `json:"displayName"`
	State               string `json:"state"`
	CodeVerifier        string `json:"codeVerifier,omitempty"`
	CodeChallenge       string `json:"codeChallenge,omitempty"`
	CodeChallengeMethod string `json:"codeChallengeMethod,omitempty"`
	AuthURL             string `json:"authURL"`
	AuthUrl             string `json:"authUrl"`
	RedirectURL         string `json:"redirectURL,omitempty"`
}

// OAuthProviderCatalogItem is the public provider metadata returned by the
// meta providers route.
type OAuthProviderCatalogItem struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Configured  bool   `json:"configured"`
}

// OAuthService manages OAuth2 authentication flows.
type OAuthService struct {
	db        *database.DB
	providers map[string]*OAuthProvider
}

// NewOAuthService creates an OAuth service with built-in providers.
func NewOAuthService(db *database.DB) *OAuthService {
	s := &OAuthService{
		db:        db,
		providers: make(map[string]*OAuthProvider),
	}

	// Register built-in providers
	s.RegisterProvider("google", &OAuthProvider{
		Name:        "google",
		DisplayName: "Google",
		AuthURL:     "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:    "https://oauth2.googleapis.com/token",
		UserInfoURL: "https://www.googleapis.com/oauth2/v3/userinfo",
		Scopes:      []string{"openid", "profile", "email"},
	})

	s.RegisterProvider("github", &OAuthProvider{
		Name:        "github",
		DisplayName: "GitHub",
		AuthURL:     "https://github.com/login/oauth/authorize",
		TokenURL:    "https://github.com/login/oauth/access_token",
		UserInfoURL: "https://api.github.com/user",
		Scopes:      []string{"read:user", "user:email"},
	})

	s.RegisterProvider("apple", &OAuthProvider{
		Name:        "apple",
		DisplayName: "Apple",
		AuthURL:     "https://appleid.apple.com/auth/authorize",
		TokenURL:    "https://appleid.apple.com/auth/token",
		UserInfoURL: "",
		Scopes:      []string{"name", "email"},
	})

	s.RegisterProvider("microsoft", &OAuthProvider{
		Name:        "microsoft",
		DisplayName: "Microsoft",
		AuthURL:     "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
		TokenURL:    "https://login.microsoftonline.com/common/oauth2/v2.0/token",
		UserInfoURL: "https://graph.microsoft.com/v1.0/me",
		Scopes:      []string{"openid", "profile", "email", "User.Read"},
	})

	s.RegisterProvider("discord", &OAuthProvider{
		Name:        "discord",
		DisplayName: "Discord",
		AuthURL:     "https://discord.com/api/oauth2/authorize",
		TokenURL:    "https://discord.com/api/oauth2/token",
		UserInfoURL: "https://discord.com/api/users/@me",
		Scopes:      []string{"identify", "email"},
	})

	s.RegisterProvider("gitlab", &OAuthProvider{
		Name:        "gitlab",
		DisplayName: "GitLab",
		AuthURL:     "https://gitlab.com/oauth/authorize",
		TokenURL:    "https://gitlab.com/oauth/token",
		UserInfoURL: "https://gitlab.com/api/v4/user",
		Scopes:      []string{"read_user"},
	})

	s.RegisterProvider("slack", &OAuthProvider{
		Name:        "slack",
		DisplayName: "Slack",
		AuthURL:     "https://slack.com/openid/connect/authorize",
		TokenURL:    "https://slack.com/api/openid.connect.token",
		UserInfoURL: "https://slack.com/api/openid.connect.userInfo",
		Scopes:      []string{"openid", "profile", "email"},
	})

	s.RegisterProvider("facebook", &OAuthProvider{
		Name:        "facebook",
		DisplayName: "Facebook",
		AuthURL:     "https://www.facebook.com/v18.0/dialog/oauth",
		TokenURL:    "https://graph.facebook.com/v18.0/oauth/access_token",
		UserInfoURL: "https://graph.facebook.com/me?fields=id,name,email,picture",
		Scopes:      []string{"email", "public_profile"},
	})

	s.RegisterProvider("twitter", &OAuthProvider{
		Name:        "twitter",
		DisplayName: "Twitter / X",
		AuthURL:     "https://twitter.com/i/oauth2/authorize",
		TokenURL:    "https://api.twitter.com/2/oauth2/token",
		UserInfoURL: "https://api.twitter.com/2/users/me",
		Scopes:      []string{"users.read", "tweet.read"},
	})

	s.RegisterProvider("spotify", &OAuthProvider{
		Name:        "spotify",
		DisplayName: "Spotify",
		AuthURL:     "https://accounts.spotify.com/authorize",
		TokenURL:    "https://accounts.spotify.com/api/token",
		UserInfoURL: "https://api.spotify.com/v1/me",
		Scopes:      []string{"user-read-email", "user-read-private"},
	})

	s.RegisterProvider("twitch", &OAuthProvider{
		Name:        "twitch",
		DisplayName: "Twitch",
		AuthURL:     "https://id.twitch.tv/oauth2/authorize",
		TokenURL:    "https://id.twitch.tv/oauth2/token",
		UserInfoURL: "https://api.twitch.tv/helix/users",
		Scopes:      []string{"user:read:email"},
	})

	s.RegisterProvider("reddit", &OAuthProvider{
		Name:        "reddit",
		DisplayName: "Reddit",
		AuthURL:     "https://www.reddit.com/api/v1/authorize",
		TokenURL:    "https://www.reddit.com/api/v1/access_token",
		UserInfoURL: "https://oauth.reddit.com/api/v1/me",
		Scopes:      []string{"identity"},
	})

	s.RegisterProvider("linkedin", &OAuthProvider{
		Name:        "linkedin",
		DisplayName: "LinkedIn",
		AuthURL:     "https://www.linkedin.com/oauth/v2/authorization",
		TokenURL:    "https://www.linkedin.com/oauth/v2/accessToken",
		UserInfoURL: "https://api.linkedin.com/v2/userinfo",
		Scopes:      []string{"openid", "profile", "email"},
	})

	s.RegisterProvider("dropbox", &OAuthProvider{
		Name:        "dropbox",
		DisplayName: "Dropbox",
		AuthURL:     "https://www.dropbox.com/oauth2/authorize",
		TokenURL:    "https://api.dropboxapi.com/oauth2/token",
		UserInfoURL: "https://api.dropboxapi.com/2/users/get_current_account",
		Scopes:      []string{},
	})

	s.RegisterProvider("bitbucket", &OAuthProvider{
		Name:        "bitbucket",
		DisplayName: "Bitbucket",
		AuthURL:     "https://bitbucket.org/site/oauth2/authorize",
		TokenURL:    "https://bitbucket.org/site/oauth2/access_token",
		UserInfoURL: "https://api.bitbucket.org/2.0/user",
		Scopes:      []string{"account"},
	})

	s.RegisterProvider("patreon", &OAuthProvider{
		Name:        "patreon",
		DisplayName: "Patreon",
		AuthURL:     "https://www.patreon.com/oauth2/authorize",
		TokenURL:    "https://www.patreon.com/api/oauth2/token",
		UserInfoURL: "https://www.patreon.com/api/oauth2/v2/identity",
		Scopes:      []string{"identity"},
	})

	s.RegisterProvider("strava", &OAuthProvider{
		Name:        "strava",
		DisplayName: "Strava",
		AuthURL:     "https://www.strava.com/oauth/authorize",
		TokenURL:    "https://www.strava.com/oauth/token",
		UserInfoURL: "https://www.strava.com/api/v3/athlete",
		Scopes:      []string{"read"},
	})

	s.RegisterProvider("vk", &OAuthProvider{
		Name:        "vk",
		DisplayName: "VK",
		AuthURL:     "https://oauth.vk.com/authorize",
		TokenURL:    "https://oauth.vk.com/access_token",
		UserInfoURL: "https://api.vk.com/method/users.get",
		Scopes:      []string{"email"},
	})

	s.RegisterProvider("yandex", &OAuthProvider{
		Name:        "yandex",
		DisplayName: "Yandex",
		AuthURL:     "https://oauth.yandex.com/authorize",
		TokenURL:    "https://oauth.yandex.com/token",
		UserInfoURL: "https://login.yandex.ru/info",
		Scopes:      []string{"login:email", "login:info"},
	})

	s.RegisterProvider("kakao", &OAuthProvider{
		Name:        "kakao",
		DisplayName: "Kakao",
		AuthURL:     "https://kauth.kakao.com/oauth/authorize",
		TokenURL:    "https://kauth.kakao.com/oauth/token",
		UserInfoURL: "https://kapi.kakao.com/v2/user/me",
		Scopes:      []string{"profile_nickname", "account_email"},
	})

	s.RegisterProvider("naver", &OAuthProvider{
		Name:        "naver",
		DisplayName: "Naver",
		AuthURL:     "https://nid.naver.com/oauth2.0/authorize",
		TokenURL:    "https://nid.naver.com/oauth2.0/token",
		UserInfoURL: "https://openapi.naver.com/v1/nid/me",
		Scopes:      []string{"name", "email"},
	})

	s.RegisterProvider("line", &OAuthProvider{
		Name:        "line",
		DisplayName: "LINE",
		AuthURL:     "https://access.line.me/oauth2/v2.1/authorize",
		TokenURL:    "https://api.line.me/oauth2/v2.1/token",
		UserInfoURL: "https://api.line.me/v2/profile",
		Scopes:      []string{"profile", "openid", "email"},
	})

	s.RegisterProvider("yahoo", &OAuthProvider{
		Name:        "yahoo",
		DisplayName: "Yahoo",
		AuthURL:     "https://api.login.yahoo.com/oauth2/request_auth",
		TokenURL:    "https://api.login.yahoo.com/oauth2/get_token",
		UserInfoURL: "https://api.login.yahoo.com/openid/v1/userinfo",
		Scopes:      []string{"openid", "profile", "email"},
	})

	s.RegisterProvider("auth0", &OAuthProvider{
		Name:        "auth0",
		DisplayName: "Auth0",
		AuthURL:     "https://{DOMAIN}/authorize",
		TokenURL:    "https://{DOMAIN}/oauth/token",
		UserInfoURL: "https://{DOMAIN}/userinfo",
		Scopes:      []string{"openid", "profile", "email"},
	})

	s.RegisterProvider("okta", &OAuthProvider{
		Name:        "okta",
		DisplayName: "Okta",
		AuthURL:     "https://{DOMAIN}/oauth2/v1/authorize",
		TokenURL:    "https://{DOMAIN}/oauth2/v1/token",
		UserInfoURL: "https://{DOMAIN}/oauth2/v1/userinfo",
		Scopes:      []string{"openid", "profile", "email"},
	})

	s.RegisterProvider("keycloak", &OAuthProvider{
		Name:        "keycloak",
		DisplayName: "Keycloak",
		AuthURL:     "https://{DOMAIN}/realms/{REALM}/protocol/openid-connect/auth",
		TokenURL:    "https://{DOMAIN}/realms/{REALM}/protocol/openid-connect/token",
		UserInfoURL: "https://{DOMAIN}/realms/{REALM}/protocol/openid-connect/userinfo",
		Scopes:      []string{"openid", "profile", "email"},
	})

	s.RegisterProvider("authentik", &OAuthProvider{
		Name:        "authentik",
		DisplayName: "Authentik",
		AuthURL:     "https://{DOMAIN}/application/o/authorize/",
		TokenURL:    "https://{DOMAIN}/application/o/token/",
		UserInfoURL: "https://{DOMAIN}/application/o/userinfo/",
		Scopes:      []string{"openid", "profile", "email"},
	})

	s.RegisterProvider("zitadel", &OAuthProvider{
		Name:        "zitadel",
		DisplayName: "Zitadel",
		AuthURL:     "https://{DOMAIN}/oauth/v2/authorize",
		TokenURL:    "https://{DOMAIN}/oauth/v2/token",
		UserInfoURL: "https://{DOMAIN}/oidc/v1/userinfo",
		Scopes:      []string{"openid", "profile", "email"},
	})

	s.RegisterProvider("notion", &OAuthProvider{
		Name:        "notion",
		DisplayName: "Notion",
		AuthURL:     "https://api.notion.com/v1/oauth/authorize",
		TokenURL:    "https://api.notion.com/v1/oauth/token",
		UserInfoURL: "",
		Scopes:      []string{},
	})

	s.RegisterProvider("linear", &OAuthProvider{
		Name:        "linear",
		DisplayName: "Linear",
		AuthURL:     "https://linear.app/oauth/authorize",
		TokenURL:    "https://api.linear.app/oauth/token",
		UserInfoURL: "https://api.linear.app/graphql",
		Scopes:      []string{"read"},
	})

	s.RegisterProvider("figma", &OAuthProvider{
		Name:        "figma",
		DisplayName: "Figma",
		AuthURL:     "https://www.figma.com/oauth",
		TokenURL:    "https://www.figma.com/api/oauth/token",
		UserInfoURL: "https://api.figma.com/v1/me",
		Scopes:      []string{"file_read"},
	})

	s.RegisterProvider("gitea", &OAuthProvider{
		Name:        "gitea",
		DisplayName: "Gitea",
		AuthURL:     "https://gitea.com/login/oauth/authorize",
		TokenURL:    "https://gitea.com/login/oauth/access_token",
		UserInfoURL: "https://gitea.com/api/v1/user",
		Scopes:      []string{},
	})

	s.RegisterProvider("gitee", &OAuthProvider{
		Name:        "gitee",
		DisplayName: "Gitee",
		AuthURL:     "https://gitee.com/oauth/authorize",
		TokenURL:    "https://gitee.com/oauth/token",
		UserInfoURL: "https://gitee.com/api/v5/user",
		Scopes:      []string{"user_info"},
	})

	s.RegisterProvider("instagram", &OAuthProvider{
		Name:        "instagram",
		DisplayName: "Instagram",
		AuthURL:     "https://www.instagram.com/oauth/authorize",
		TokenURL:    "https://api.instagram.com/oauth/access_token",
		UserInfoURL: "https://graph.instagram.com/me?fields=id,user_id,username,account_type,profile_picture_url",
		Scopes:      []string{"instagram_business_basic"},
	})

	s.RegisterProvider("zoom", &OAuthProvider{
		Name:        "zoom",
		DisplayName: "Zoom",
		AuthURL:     "https://zoom.us/oauth/authorize",
		TokenURL:    "https://zoom.us/oauth/token",
		UserInfoURL: "https://api.zoom.us/v2/users/me",
		Scopes:      []string{},
	})

	s.RegisterProvider("box", &OAuthProvider{
		Name:        "box",
		DisplayName: "Box",
		AuthURL:     "https://account.box.com/api/oauth2/authorize",
		TokenURL:    "https://api.box.com/oauth2/token",
		UserInfoURL: "https://api.box.com/2.0/users/me",
		Scopes:      []string{"root_readonly"},
	})

	s.RegisterProvider("wakatime", &OAuthProvider{
		Name:        "wakatime",
		DisplayName: "WakaTime",
		AuthURL:     "https://wakatime.com/oauth/authorize",
		TokenURL:    "https://wakatime.com/oauth/token",
		UserInfoURL: "https://wakatime.com/api/v1/users/current",
		Scopes:      []string{"email"},
	})

	return s
}

func (s *OAuthService) exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return s.db.ExecResult(ctx, sql, args...)
}

func (s *OAuthService) queryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return s.db.QueryRow(ctx, sql, args...)
}

// RegisterProvider adds an OAuth provider configuration.
func (s *OAuthService) RegisterProvider(name string, p *OAuthProvider) {
	p.Name = name
	s.providers[name] = p
}

// GetProvider returns a provider by name.
func (s *OAuthService) GetProvider(name string) (*OAuthProvider, error) {
	p, ok := s.providers[name]
	if !ok {
		return nil, fmt.Errorf("unknown OAuth provider: %s", name)
	}
	if !p.Enabled && p.ClientID == "" {
		return nil, fmt.Errorf("provider %s is not configured", name)
	}
	return p, nil
}

// GetProviderByName returns a provider by name with a bool for existence.
func (s *OAuthService) GetProviderByName(name string) (*OAuthProvider, bool) {
	p, ok := s.providers[name]
	return p, ok
}

// GetProviders returns all registered providers.
func (s *OAuthService) GetProviders() []*OAuthProvider {
	result := make([]*OAuthProvider, 0, len(s.providers))
	for _, p := range s.providers {
		result = append(result, p)
	}
	return result
}

// GetAvailableProviders returns the list of enabled providers in
// the format expected by the auth-methods endpoint.
func (s *OAuthService) GetAvailableProviders() []map[string]any {
	providers := s.ProviderCatalog(true)
	result := make([]map[string]any, 0, len(providers))
	for _, p := range providers {
		result = append(result, map[string]any{
			"name":        p.Name,
			"displayName": p.DisplayName,
		})
	}
	return result
}

// ProviderCatalog returns the public metadata for registered OAuth providers.
// When configuredOnly is true, only enabled/configured providers are returned.
func (s *OAuthService) ProviderCatalog(configuredOnly bool) []OAuthProviderCatalogItem {
	providers := s.GetProviders()
	sort.Slice(providers, func(i, j int) bool {
		if providers[i].DisplayName == providers[j].DisplayName {
			return providers[i].Name < providers[j].Name
		}
		return providers[i].DisplayName < providers[j].DisplayName
	})

	result := make([]OAuthProviderCatalogItem, 0, len(providers))
	for _, p := range providers {
		configured := p.Enabled || strings.TrimSpace(p.ClientID) != ""
		if configuredOnly && !configured {
			continue
		}
		displayName := strings.TrimSpace(p.DisplayName)
		if displayName == "" {
			displayName = p.Name
		}
		result = append(result, OAuthProviderCatalogItem{
			Name:        p.Name,
			DisplayName: displayName,
			Configured:  configured,
		})
	}
	return result
}

// AuthMethodProviders prepares OAuth provider descriptors for
// a collection auth-methods response.
func (s *OAuthService) AuthMethodProviders(ctx context.Context, redirectURL string) []OAuthAuthMethodProvider {
	catalog := s.ProviderCatalog(true)
	result := make([]OAuthAuthMethodProvider, 0, len(catalog))
	for _, item := range catalog {
		prepared, err := s.prepareAuthRequestContext(ctx, item.Name, redirectURL)
		if err != nil {
			continue
		}
		result = append(result, OAuthAuthMethodProvider{
			Name:                item.Name,
			DisplayName:         item.DisplayName,
			State:               prepared.State.State,
			CodeVerifier:        prepared.State.CodeVerifier,
			CodeChallenge:       prepared.CodeChallenge,
			CodeChallengeMethod: prepared.CodeChallengeMethod,
			AuthURL:             prepared.AuthURL,
			AuthUrl:             prepared.AuthURL,
			RedirectURL:         prepared.State.Redirect,
		})
	}
	return result
}

type preparedOAuthRequest struct {
	AuthURL             string
	State               *OAuthState
	CodeChallenge       string
	CodeChallengeMethod string
}

// GetAuthURL generates the authorization URL for a provider.
func (s *OAuthService) GetAuthURL(providerName, redirectURL string) (string, string, error) {
	return s.GetAuthURLContext(context.Background(), providerName, redirectURL)
}

func (s *OAuthService) GetAuthURLContext(ctx context.Context, providerName, redirectURL string) (string, string, error) {
	prepared, err := s.prepareAuthRequestContext(ctx, providerName, redirectURL)
	if err != nil {
		return "", "", err
	}
	return prepared.AuthURL, prepared.State.State, nil
}

func (s *OAuthService) prepareAuthRequestContext(ctx context.Context, providerName, redirectURL string) (*preparedOAuthRequest, error) {
	p, err := s.GetProvider(providerName)
	if err != nil {
		return nil, err
	}

	state := generateOAuthRandomString(32)
	codeVerifier := generateCodeVerifier()
	codeChallenge := generateCodeChallenge(codeVerifier)

	authURL, err := url.Parse(p.AuthURL)
	if err != nil {
		return nil, err
	}

	q := authURL.Query()
	q.Set("client_id", p.ClientID)
	q.Set("redirect_uri", redirectURL)
	q.Set("response_type", "code")
	q.Set("scope", strings.Join(p.Scopes, " "))
	q.Set("state", state)
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", "S256")
	authURL.RawQuery = q.Encode()

	oauthState := &OAuthState{
		ID:           uuid.New().String(),
		Provider:     providerName,
		Redirect:     redirectURL,
		State:        state,
		CodeVerifier: codeVerifier,
		CreatedAt:    time.Now(),
	}

	stateJSON, _ := json.Marshal(oauthState)
	if _, err := s.exec(ctx,
		`INSERT INTO _oauth_states (id, provider, state, data, created_at) VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (id) DO UPDATE SET data = $4`,
		oauthState.ID, providerName, state, stateJSON, oauthState.CreatedAt); err != nil {
		return nil, err
	}

	return &preparedOAuthRequest{
		AuthURL:             authURL.String(),
		State:               oauthState,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: "S256",
	}, nil
}

// ExchangeCode exchanges an OAuth authorization code for user info.
func (s *OAuthService) ExchangeCode(ctx context.Context, providerName, code, state, redirectURL string) (*OAuthUserInfo, error) {
	return s.ExchangeCodeWithVerifier(ctx, providerName, code, state, redirectURL, "")
}

// ExchangeCodeWithVerifier exchanges an OAuth authorization code for user info
// while optionally validating an explicit PKCE code verifier.
func (s *OAuthService) ExchangeCodeWithVerifier(ctx context.Context, providerName, code, state, redirectURL, codeVerifier string) (*OAuthUserInfo, error) {
	p, err := s.GetProvider(providerName)
	if err != nil {
		return nil, err
	}

	var storedState OAuthState
	var dataJSON []byte
	err = s.queryRow(ctx,
		`SELECT data FROM _oauth_states WHERE provider = $1 AND state = $2`,
		providerName, state).Scan(&dataJSON)
	if err != nil {
		return nil, fmt.Errorf("invalid OAuth state: %w", err)
	}
	if err := json.Unmarshal(dataJSON, &storedState); err != nil {
		return nil, fmt.Errorf("invalid OAuth state payload: %w", err)
	}

	if storedState.Redirect != "" {
		if strings.TrimSpace(redirectURL) != "" && storedState.Redirect != redirectURL {
			return nil, fmt.Errorf("oauth redirect mismatch")
		}
		redirectURL = storedState.Redirect
	}
	if strings.TrimSpace(codeVerifier) != "" && storedState.CodeVerifier != "" && strings.TrimSpace(codeVerifier) != storedState.CodeVerifier {
		return nil, fmt.Errorf("oauth code verifier mismatch")
	}

	// Consume the single-use state. If the delete fails we abort rather than
	// risk the state being replayed.
	if _, err := s.exec(ctx, "DELETE FROM _oauth_states WHERE provider = $1 AND state = $2", providerName, state); err != nil {
		return nil, fmt.Errorf("consume oauth state: %w", err)
	}

	verifier := storedState.CodeVerifier
	if verifier == "" {
		verifier = strings.TrimSpace(codeVerifier)
	}

	tokenData := url.Values{
		"client_id":     {p.ClientID},
		"client_secret": {p.ClientSecret},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {redirectURL},
		"code_verifier": {verifier},
	}

	tokenResp, err := http.PostForm(p.TokenURL, tokenData)
	if err != nil {
		return nil, fmt.Errorf("token exchange failed: %w", err)
	}
	defer tokenResp.Body.Close()

	var tokenResult struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		Error        string `json:"error"`
	}
	if err := json.NewDecoder(tokenResp.Body).Decode(&tokenResult); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}
	if tokenResult.Error != "" {
		return nil, fmt.Errorf("token error: %s", tokenResult.Error)
	}

	user, err := s.fetchUserInfo(ctx, p, tokenResult.AccessToken)
	if err != nil {
		return nil, err
	}
	user.Provider = providerName

	return user, nil
}

// fetchUserInfo calls the provider's user info endpoint.
func (s *OAuthService) fetchUserInfo(ctx context.Context, p *OAuthProvider, accessToken string) (*OAuthUserInfo, error) {
	if p.UserInfoURL == "" {
		// Some providers (like Apple) put user info in the ID token
		return &OAuthUserInfo{}, nil
	}

	req, err := http.NewRequestWithContext(ctx, "GET", p.UserInfoURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("user info request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("failed to parse user info: %w", err)
	}

	user := &OAuthUserInfo{RawJSON: raw}

	// Normalize based on provider
	switch p.Name {
	case "google":
		user.ProviderID = fmt.Sprint(result["sub"])
		user.Email = fmt.Sprint(result["email"])
		user.Name = fmt.Sprint(result["name"])
		user.AvatarURL = fmt.Sprint(result["picture"])
	case "github":
		user.ProviderID = fmt.Sprintf("%.0f", result["id"])
		user.Name = fmt.Sprint(result["login"])
		if email, ok := result["email"].(string); ok {
			user.Email = email
		}
		user.AvatarURL = fmt.Sprint(result["avatar_url"])
		// GitHub may need separate email API call
		if user.Email == "" || user.Email == "<nil>" {
			user.Email = fmt.Sprint(result["login"]) + "@github.users"
		}
	case "microsoft":
		user.ProviderID = fmt.Sprint(result["id"])
		user.Email = fmt.Sprint(result["mail"])
		if user.Email == "" || user.Email == "<nil>" {
			user.Email = fmt.Sprint(result["userPrincipalName"])
		}
		user.Name = fmt.Sprint(result["displayName"])
	case "discord":
		user.ProviderID = fmt.Sprint(result["id"])
		user.Email = fmt.Sprint(result["email"])
		user.Name = fmt.Sprint(result["username"])
		if avatar, ok := result["avatar"].(string); ok && avatar != "" {
			user.AvatarURL = fmt.Sprintf("https://cdn.discordapp.com/avatars/%s/%s.png", user.ProviderID, avatar)
		}
	case "gitlab":
		user.ProviderID = fmt.Sprintf("%.0f", result["id"])
		user.Email = fmt.Sprint(result["email"])
		user.Name = fmt.Sprint(result["name"])
		user.AvatarURL = fmt.Sprint(result["avatar_url"])
	case "slack":
		user.ProviderID = fmt.Sprint(result["sub"])
		user.Email = fmt.Sprint(result["email"])
		user.Name = fmt.Sprint(result["name"])
		user.AvatarURL = fmt.Sprint(result["picture"])
	case "facebook":
		user.ProviderID = fmt.Sprint(result["id"])
		user.Email = fmt.Sprint(result["email"])
		user.Name = fmt.Sprint(result["name"])
		if pic, ok := result["picture"].(map[string]any); ok {
			if data, ok := pic["data"].(map[string]any); ok {
				user.AvatarURL = fmt.Sprint(data["url"])
			}
		}
	case "twitter":
		if data, ok := result["data"].(map[string]any); ok {
			user.ProviderID = fmt.Sprint(data["id"])
			user.Name = fmt.Sprint(data["name"])
			user.Email = fmt.Sprint(data["email"])
		} else {
			user.ProviderID = fmt.Sprint(result["id"])
			user.Name = fmt.Sprint(result["name"])
		}
	case "spotify":
		user.ProviderID = fmt.Sprint(result["id"])
		user.Email = fmt.Sprint(result["email"])
		user.Name = fmt.Sprint(result["display_name"])
		if images, ok := result["images"].([]any); ok && len(images) > 0 {
			if img, ok := images[0].(map[string]any); ok {
				user.AvatarURL = fmt.Sprint(img["url"])
			}
		}
	case "twitch":
		if data, ok := result["data"].([]any); ok && len(data) > 0 {
			if d, ok := data[0].(map[string]any); ok {
				user.ProviderID = fmt.Sprint(d["id"])
				user.Name = fmt.Sprint(d["display_name"])
				user.Email = fmt.Sprint(d["email"])
				user.AvatarURL = fmt.Sprint(d["profile_image_url"])
			}
		}
	case "reddit":
		user.ProviderID = fmt.Sprint(result["id"])
		user.Name = fmt.Sprint(result["name"])
		user.AvatarURL = fmt.Sprint(result["icon_img"])
	case "linkedin":
		user.ProviderID = fmt.Sprint(result["sub"])
		user.Email = fmt.Sprint(result["email"])
		user.Name = fmt.Sprint(result["name"])
		user.AvatarURL = fmt.Sprint(result["picture"])
	case "dropbox":
		user.ProviderID = fmt.Sprint(result["account_id"])
		user.Email = fmt.Sprint(result["email"])
		if name, ok := result["name"].(map[string]any); ok {
			user.Name = fmt.Sprint(name["display_name"])
		}
	case "bitbucket":
		user.ProviderID = fmt.Sprint(result["account_id"])
		user.Name = fmt.Sprint(result["display_name"])
		if links, ok := result["links"].(map[string]any); ok {
			if avatar, ok := links["avatar"].(map[string]any); ok {
				user.AvatarURL = fmt.Sprint(avatar["href"])
			}
		}
	case "vk":
		if data, ok := result["response"].([]any); ok && len(data) > 0 {
			if d, ok := data[0].(map[string]any); ok {
				user.ProviderID = fmt.Sprint(d["id"])
				user.Name = fmt.Sprintf("%s %s", d["first_name"], d["last_name"])
				user.AvatarURL = fmt.Sprint(d["photo_200"])
			}
		}
		user.Email = fmt.Sprint(result["email"])
	case "kakao":
		user.ProviderID = fmt.Sprintf("%.0f", result["id"])
		if acc, ok := result["kakao_account"].(map[string]any); ok {
			user.Email = fmt.Sprint(acc["email"])
			if profile, ok := acc["profile"].(map[string]any); ok {
				user.Name = fmt.Sprint(profile["nickname"])
				user.AvatarURL = fmt.Sprint(profile["profile_image_url"])
			}
		}
	case "naver":
		if resp, ok := result["response"].(map[string]any); ok {
			user.ProviderID = fmt.Sprint(resp["id"])
			user.Email = fmt.Sprint(resp["email"])
			user.Name = fmt.Sprint(resp["name"])
			user.AvatarURL = fmt.Sprint(resp["profile_image"])
		}
	case "line":
		user.ProviderID = fmt.Sprint(result["userId"])
		user.Name = fmt.Sprint(result["displayName"])
		user.AvatarURL = fmt.Sprint(result["pictureUrl"])
		user.Email = fmt.Sprint(result["email"])
	case "yahoo":
		user.ProviderID = fmt.Sprint(result["sub"])
		user.Email = fmt.Sprint(result["email"])
		user.Name = fmt.Sprint(result["name"])
		user.AvatarURL = fmt.Sprint(result["picture"])
	case "yandex":
		user.ProviderID = fmt.Sprint(result["id"])
		user.Email = fmt.Sprint(result["default_email"])
		user.Name = fmt.Sprint(result["real_name"])
		if user.Name == "" || user.Name == "<nil>" {
			user.Name = fmt.Sprint(result["display_name"])
		}
	case "auth0", "okta", "keycloak", "authentik", "zitadel":
		user.ProviderID = fmt.Sprint(result["sub"])
		user.Email = fmt.Sprint(result["email"])
		user.Name = fmt.Sprint(result["name"])
		if user.Name == "" || user.Name == "<nil>" {
			user.Name = fmt.Sprintf("%s %s", result["given_name"], result["family_name"])
		}
		user.AvatarURL = fmt.Sprint(result["picture"])
	case "gitea", "gitee":
		user.ProviderID = fmt.Sprintf("%.0f", result["id"])
		user.Email = fmt.Sprint(result["email"])
		user.Name = fmt.Sprint(result["name"])
		if user.Name == "" || user.Name == "<nil>" {
			user.Name = fmt.Sprint(result["full_name"])
		}
		if user.Name == "" || user.Name == "<nil>" {
			user.Name = fmt.Sprint(result["login"])
		}
		user.AvatarURL = fmt.Sprint(result["avatar_url"])
	case "instagram":
		user.ProviderID = fmt.Sprint(result["user_id"])
		if user.ProviderID == "" || user.ProviderID == "<nil>" {
			user.ProviderID = fmt.Sprint(result["id"])
		}
		user.Name = fmt.Sprint(result["username"])
		user.AvatarURL = fmt.Sprint(result["profile_picture_url"])
	case "zoom":
		user.ProviderID = fmt.Sprint(result["id"])
		user.Email = fmt.Sprint(result["email"])
		user.Name = strings.TrimSpace(fmt.Sprintf("%v %v", result["first_name"], result["last_name"]))
		user.AvatarURL = fmt.Sprint(result["pic_url"])
	case "box":
		user.ProviderID = fmt.Sprint(result["id"])
		user.Email = fmt.Sprint(result["login"])
		user.Name = fmt.Sprint(result["name"])
		user.AvatarURL = fmt.Sprint(result["avatar_url"])
	case "wakatime":
		if data, ok := result["data"].(map[string]any); ok {
			user.ProviderID = fmt.Sprint(data["id"])
			user.Email = fmt.Sprint(data["email"])
			user.Name = fmt.Sprint(data["display_name"])
			user.AvatarURL = fmt.Sprint(data["photo"])
		}
	case "strava":
		user.ProviderID = fmt.Sprintf("%.0f", result["id"])
		user.Name = strings.TrimSpace(fmt.Sprintf("%v %v", result["firstname"], result["lastname"]))
		user.AvatarURL = fmt.Sprint(result["profile"])
	case "figma":
		user.ProviderID = fmt.Sprint(result["id"])
		user.Email = fmt.Sprint(result["email"])
		user.Name = fmt.Sprint(result["handle"])
		user.AvatarURL = fmt.Sprint(result["img_url"])
	case "patreon":
		if data, ok := result["data"].(map[string]any); ok {
			user.ProviderID = fmt.Sprint(data["id"])
			if attrs, ok := data["attributes"].(map[string]any); ok {
				user.Email = fmt.Sprint(attrs["email"])
				user.Name = fmt.Sprint(attrs["full_name"])
				user.AvatarURL = fmt.Sprint(attrs["image_url"])
			}
		}
	case "notion":
		if owner, ok := result["owner"].(map[string]any); ok {
			if u, ok := owner["user"].(map[string]any); ok {
				user.ProviderID = fmt.Sprint(u["id"])
				user.Name = fmt.Sprint(u["name"])
				if person, ok := u["person"].(map[string]any); ok {
					user.Email = fmt.Sprint(person["email"])
				}
				user.AvatarURL = fmt.Sprint(u["avatar_url"])
			}
		}
	default:
		user.ProviderID = fmt.Sprint(result["sub"])
		user.Email = fmt.Sprint(result["email"])
		user.Name = fmt.Sprint(result["name"])
		user.AvatarURL = fmt.Sprint(result["picture"])
	}

	return user, nil
}

// Ensure OAuth table exists
func (s *OAuthService) EnsureTable(ctx context.Context) error {
	return s.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS _oauth_states (
			id          TEXT PRIMARY KEY,
			provider    TEXT NOT NULL,
			state       TEXT NOT NULL,
			data        JSONB,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_oauth_states_provider_state ON _oauth_states(provider, state);
	`)
}

// ---------------------------------------------------------------------------
// PKCE helpers
// ---------------------------------------------------------------------------

func generateOAuthRandomString(length int) string {
	b := make([]byte, length)
	mustRandRead(b)
	return base64.RawURLEncoding.EncodeToString(b)[:length]
}

func generateCodeVerifier() string {
	b := make([]byte, 32)
	mustRandRead(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func generateCodeChallenge(verifier string) string {
	// SHA256 of verifier, base64url-encoded
	return base64.RawURLEncoding.EncodeToString(sha256Sum([]byte(verifier)))
}

func sha256Sum(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}
