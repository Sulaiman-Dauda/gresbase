package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Table-driven standard OAuth2 providers
// ---------------------------------------------------------------------------

// userInfoMapper extracts normalized fields from a provider's decoded userinfo
// (or ID token claims) JSON. Provider and RawJSON are filled in by the caller.
type userInfoMapper func(data map[string]any) OAuthUserInfo

// standardDef declares a provider's endpoints and quirks. Credentials are
// injected at construction time via build/NewStandardProvider.
type standardDef struct {
	authURL     string
	tokenURL    string
	userInfoURL string
	scopes      []string
	// authParams are extra static query params for the authorize URL.
	authParams map[string]string
	// tokenBasicAuth sends client credentials as an HTTP Basic header instead
	// of form fields (Reddit, Notion, Zoom, Figma).
	tokenBasicAuth bool
	// tokenJSONBody posts the token request as a JSON object (Notion).
	tokenJSONBody bool
	// userFromIDToken skips the userinfo call and maps the id_token claims
	// instead (Apple). The token arrives over TLS directly from the provider,
	// so the JWT signature is not re-verified here.
	userFromIDToken bool
	// userInfoMethod overrides the userinfo HTTP method (Dropbox, Linear).
	userInfoMethod string
	// userInfoBody is the request body for non-GET userinfo calls (Linear).
	userInfoBody string
	// headers are applied to both the token and userinfo requests (Reddit
	// requires a User-Agent on every call).
	headers map[string]string
	// userInfoHeaders are applied to the userinfo request only.
	userInfoHeaders map[string]string
	// clientIDHeader names a userinfo header whose value is the client ID
	// (Twitch requires Client-Id alongside the Bearer token).
	clientIDHeader string
	mapUser        userInfoMapper
}

func (d standardDef) build(name, clientID, clientSecret string) *StandardProvider {
	return &StandardProvider{
		name:         name,
		clientID:     clientID,
		clientSecret: clientSecret,
		enabled:      clientID != "" && clientSecret != "",
		def:          d,
		httpClient:   &http.Client{Timeout: 15 * time.Second},
	}
}

// StandardProvider implements OAuthIdentityProvider for any provider that
// follows the plain OAuth2 authorization-code flow, with small declarative
// quirks handled via standardDef flags.
type StandardProvider struct {
	name         string
	clientID     string
	clientSecret string
	enabled      bool
	def          standardDef
	httpClient   *http.Client
}

// NewStandardProvider builds a catalog provider by name with credentials
// injected. The second return is false for unknown names.
func NewStandardProvider(name, clientID, clientSecret string) (*StandardProvider, bool) {
	d, ok := standardProviderDefs[name]
	if !ok {
		return nil, false
	}
	return d.build(name, clientID, clientSecret), true
}

// StandardProviderNames returns the sorted names of all catalog providers.
func StandardProviderNames() []string {
	names := make([]string, 0, len(standardProviderDefs))
	for name := range standardProviderDefs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (p *StandardProvider) Name() string    { return p.name }
func (p *StandardProvider) IsEnabled() bool { return p.enabled }

func (p *StandardProvider) GetAuthURL(state, redirectURL string) (string, error) {
	u, err := url.Parse(p.def.authURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("client_id", p.clientID)
	q.Set("redirect_uri", redirectURL)
	q.Set("response_type", "code")
	q.Set("state", state)
	if len(p.def.scopes) > 0 {
		q.Set("scope", strings.Join(p.def.scopes, " "))
	}
	for k, v := range p.def.authParams {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (p *StandardProvider) ExchangeCode(ctx context.Context, code, redirectURL string) (*OAuthUserInfo, error) {
	var req *http.Request
	if p.def.tokenJSONBody {
		body, _ := json.Marshal(map[string]string{
			"grant_type":   "authorization_code",
			"code":         code,
			"redirect_uri": redirectURL,
		})
		req, _ = http.NewRequestWithContext(ctx, "POST", p.def.tokenURL, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		data := url.Values{
			"grant_type":   {"authorization_code"},
			"code":         {code},
			"redirect_uri": {redirectURL},
		}
		if !p.def.tokenBasicAuth {
			data.Set("client_id", p.clientID)
			data.Set("client_secret", p.clientSecret)
		}
		req, _ = http.NewRequestWithContext(ctx, "POST", p.def.tokenURL, strings.NewReader(data.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	// GitHub-style token endpoints return form-encoded bodies without this.
	req.Header.Set("Accept", "application/json")
	if p.def.tokenBasicAuth {
		req.SetBasicAuth(p.clientID, p.clientSecret)
	}
	for k, v := range p.def.headers {
		req.Header.Set(k, v)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s token exchange failed: %w", p.name, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var token map[string]any
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, fmt.Errorf("%s: failed to parse token response: %w", p.name, err)
	}
	if errStr := jStr(token, "error"); errStr != "" {
		return nil, fmt.Errorf("%s token error: %s", p.name, errStr)
	}

	var raw []byte
	var data map[string]any
	if p.def.userFromIDToken {
		data, err = decodeJWTClaims(jStr(token, "id_token"))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p.name, err)
		}
		raw, _ = json.Marshal(data)
	} else {
		accessToken := jStr(token, "access_token")
		if accessToken == "" {
			return nil, fmt.Errorf("%s: token response missing access_token", p.name)
		}
		raw, data, err = p.fetchUserInfo(ctx, accessToken)
		if err != nil {
			return nil, err
		}
	}

	info := p.def.mapUser(data)
	info.Provider = p.name
	info.RawJSON = raw
	if info.Email == "" {
		// Some providers (VK) return the email with the token response.
		info.Email = jStr(token, "email")
	}
	return &info, nil
}

func (p *StandardProvider) fetchUserInfo(ctx context.Context, accessToken string) ([]byte, map[string]any, error) {
	method := p.def.userInfoMethod
	if method == "" {
		method = "GET"
	}
	var body io.Reader
	if p.def.userInfoBody != "" {
		body = strings.NewReader(p.def.userInfoBody)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.def.userInfoURL, body)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	for k, v := range p.def.headers {
		req.Header.Set(k, v)
	}
	for k, v := range p.def.userInfoHeaders {
		req.Header.Set(k, v)
	}
	if p.def.clientIDHeader != "" {
		req.Header.Set(p.def.clientIDHeader, p.clientID)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("%s userinfo fetch failed: %w", p.name, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, nil, fmt.Errorf("%s userinfo returned %d", p.name, resp.StatusCode)
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, nil, fmt.Errorf("%s: failed to parse userinfo: %w", p.name, err)
	}
	return raw, data, nil
}

// NewGiteaProvider creates a Gitea provider. baseURL defaults to
// https://gitea.com; self-hosted instances pass their own root URL.
func NewGiteaProvider(clientID, clientSecret, baseURL string) *StandardProvider {
	if baseURL == "" {
		baseURL = "https://gitea.com"
	}
	return giteaDef(baseURL).build("gitea", clientID, clientSecret)
}

// NewOIDCProvider creates a generic OpenID Connect provider. It is not part
// of the registered catalog because OAuthProviderConfig carries only client
// credentials; callers must supply the endpoint URLs explicitly.
func NewOIDCProvider(name, clientID, clientSecret, authURL, tokenURL, userInfoURL string) *StandardProvider {
	def := standardDef{
		authURL:     authURL,
		tokenURL:    tokenURL,
		userInfoURL: userInfoURL,
		scopes:      []string{"openid", "profile", "email"},
		mapUser:     oidcUserMapper,
	}
	return def.build(name, clientID, clientSecret)
}

func giteaDef(baseURL string) standardDef {
	return standardDef{
		authURL:     baseURL + "/login/oauth/authorize",
		tokenURL:    baseURL + "/login/oauth/access_token",
		userInfoURL: baseURL + "/api/v1/user",
		mapUser: func(d map[string]any) OAuthUserInfo {
			return OAuthUserInfo{
				ProviderID: jStr(d, "id"),
				Email:      jStr(d, "email"),
				Name:       firstNonEmpty(jStr(d, "full_name"), jStr(d, "login")),
				AvatarURL:  jStr(d, "avatar_url"),
			}
		},
	}
}

func oidcUserMapper(d map[string]any) OAuthUserInfo {
	name := jStr(d, "name")
	if name == "" {
		name = strings.TrimSpace(jStr(d, "given_name") + " " + jStr(d, "family_name"))
	}
	return OAuthUserInfo{
		ProviderID: jStr(d, "sub"),
		Email:      jStr(d, "email"),
		Name:       name,
		AvatarURL:  jStr(d, "picture"),
	}
}

// standardProviderDefs is the built-in catalog of standard-flow providers.
// Twitter/X is intentionally absent: it mandates PKCE and the
// OAuthIdentityProvider flow has no code_verifier plumbing.
var standardProviderDefs = map[string]standardDef{
	"facebook": {
		authURL:     "https://www.facebook.com/dialog/oauth",
		tokenURL:    "https://graph.facebook.com/oauth/access_token",
		userInfoURL: "https://graph.facebook.com/me?fields=id,name,email,picture.type(large)",
		scopes:      []string{"email"},
		mapUser: func(d map[string]any) OAuthUserInfo {
			return OAuthUserInfo{
				ProviderID: jStr(d, "id"),
				Email:      jStr(d, "email"),
				Name:       jStr(d, "name"),
				AvatarURL:  jStr(jSub(d, "picture", "data"), "url"),
			}
		},
	},
	"instagram": {
		authURL:     "https://www.instagram.com/oauth/authorize",
		tokenURL:    "https://api.instagram.com/oauth/access_token",
		userInfoURL: "https://graph.instagram.com/me?fields=id,user_id,username,account_type,profile_picture_url",
		scopes:      []string{"instagram_business_basic"},
		mapUser: func(d map[string]any) OAuthUserInfo {
			// The Instagram API exposes no email.
			return OAuthUserInfo{
				ProviderID: firstNonEmpty(jStr(d, "user_id"), jStr(d, "id")),
				Name:       jStr(d, "username"),
				AvatarURL:  jStr(d, "profile_picture_url"),
			}
		},
	},
	"bitbucket": {
		authURL:     "https://bitbucket.org/site/oauth2/authorize",
		tokenURL:    "https://bitbucket.org/site/oauth2/access_token",
		userInfoURL: "https://api.bitbucket.org/2.0/user",
		scopes:      []string{"account", "email"},
		mapUser: func(d map[string]any) OAuthUserInfo {
			// Email requires a second call to /2.0/user/emails; not fetched.
			return OAuthUserInfo{
				ProviderID: firstNonEmpty(jStr(d, "account_id"), jStr(d, "uuid")),
				Name:       jStr(d, "display_name"),
				AvatarURL:  jStr(jSub(d, "links", "avatar"), "href"),
			}
		},
	},
	"gitea": giteaDef("https://gitea.com"),
	"gitee": {
		authURL:     "https://gitee.com/oauth/authorize",
		tokenURL:    "https://gitee.com/oauth/token",
		userInfoURL: "https://gitee.com/api/v5/user",
		scopes:      []string{"user_info"},
		mapUser: func(d map[string]any) OAuthUserInfo {
			return OAuthUserInfo{
				ProviderID: jStr(d, "id"),
				Email:      jStr(d, "email"),
				Name:       firstNonEmpty(jStr(d, "name"), jStr(d, "login")),
				AvatarURL:  jStr(d, "avatar_url"),
			}
		},
	},
	"spotify": {
		authURL:     "https://accounts.spotify.com/authorize",
		tokenURL:    "https://accounts.spotify.com/api/token",
		userInfoURL: "https://api.spotify.com/v1/me",
		scopes:      []string{"user-read-email", "user-read-private"},
		mapUser: func(d map[string]any) OAuthUserInfo {
			info := OAuthUserInfo{
				ProviderID: jStr(d, "id"),
				Email:      jStr(d, "email"),
				Name:       jStr(d, "display_name"),
			}
			if images, ok := d["images"].([]any); ok && len(images) > 0 {
				if img, ok := images[0].(map[string]any); ok {
					info.AvatarURL = jStr(img, "url")
				}
			}
			return info
		},
	},
	"strava": {
		authURL:     "https://www.strava.com/oauth/authorize",
		tokenURL:    "https://www.strava.com/api/v3/oauth/token",
		userInfoURL: "https://www.strava.com/api/v3/athlete",
		scopes:      []string{"read"},
		mapUser: func(d map[string]any) OAuthUserInfo {
			// Strava exposes no email.
			return OAuthUserInfo{
				ProviderID: jStr(d, "id"),
				Name:       strings.TrimSpace(jStr(d, "firstname") + " " + jStr(d, "lastname")),
				AvatarURL:  jStr(d, "profile"),
			}
		},
	},
	"twitch": {
		authURL:        "https://id.twitch.tv/oauth2/authorize",
		tokenURL:       "https://id.twitch.tv/oauth2/token",
		userInfoURL:    "https://api.twitch.tv/helix/users",
		scopes:         []string{"user:read:email"},
		clientIDHeader: "Client-Id",
		mapUser: func(d map[string]any) OAuthUserInfo {
			info := OAuthUserInfo{}
			if list, ok := d["data"].([]any); ok && len(list) > 0 {
				if u, ok := list[0].(map[string]any); ok {
					info.ProviderID = jStr(u, "id")
					info.Email = jStr(u, "email")
					info.Name = firstNonEmpty(jStr(u, "display_name"), jStr(u, "login"))
					info.AvatarURL = jStr(u, "profile_image_url")
				}
			}
			return info
		},
	},
	"kakao": {
		authURL:     "https://kauth.kakao.com/oauth/authorize",
		tokenURL:    "https://kauth.kakao.com/oauth/token",
		userInfoURL: "https://kapi.kakao.com/v2/user/me",
		scopes:      []string{"profile_nickname", "account_email"},
		mapUser: func(d map[string]any) OAuthUserInfo {
			profile := jSub(d, "kakao_account", "profile")
			return OAuthUserInfo{
				ProviderID: jStr(d, "id"),
				Email:      jStr(jSub(d, "kakao_account"), "email"),
				Name:       jStr(profile, "nickname"),
				AvatarURL:  jStr(profile, "profile_image_url"),
			}
		},
	},
	"line": {
		authURL:     "https://access.line.me/oauth2/v2.1/authorize",
		tokenURL:    "https://api.line.me/oauth2/v2.1/token",
		userInfoURL: "https://api.line.me/v2/profile",
		scopes:      []string{"profile", "openid"},
		mapUser: func(d map[string]any) OAuthUserInfo {
			// Email is only available via the id_token email scope flow.
			return OAuthUserInfo{
				ProviderID: jStr(d, "userId"),
				Name:       jStr(d, "displayName"),
				AvatarURL:  jStr(d, "pictureUrl"),
			}
		},
	},
	"vk": {
		authURL:     "https://oauth.vk.com/authorize",
		tokenURL:    "https://oauth.vk.com/access_token",
		userInfoURL: "https://api.vk.com/method/users.get?fields=photo_200&v=5.131",
		scopes:      []string{"email"},
		mapUser: func(d map[string]any) OAuthUserInfo {
			// Email arrives in the token response, picked up by ExchangeCode.
			info := OAuthUserInfo{}
			if list, ok := d["response"].([]any); ok && len(list) > 0 {
				if u, ok := list[0].(map[string]any); ok {
					info.ProviderID = jStr(u, "id")
					info.Name = strings.TrimSpace(jStr(u, "first_name") + " " + jStr(u, "last_name"))
					info.AvatarURL = jStr(u, "photo_200")
				}
			}
			return info
		},
	},
	"yandex": {
		authURL:     "https://oauth.yandex.com/authorize",
		tokenURL:    "https://oauth.yandex.com/token",
		userInfoURL: "https://login.yandex.ru/info",
		scopes:      []string{"login:email", "login:info", "login:avatar"},
		mapUser: func(d map[string]any) OAuthUserInfo {
			info := OAuthUserInfo{
				ProviderID: jStr(d, "id"),
				Email:      jStr(d, "default_email"),
				Name:       firstNonEmpty(jStr(d, "real_name"), jStr(d, "display_name")),
			}
			if id := jStr(d, "default_avatar_id"); id != "" && jStr(d, "is_avatar_empty") != "true" {
				info.AvatarURL = "https://avatars.yandex.net/get-yapic/" + id + "/islands-200"
			}
			return info
		},
	},
	"notion": {
		authURL:        "https://api.notion.com/v1/oauth/authorize",
		tokenURL:       "https://api.notion.com/v1/oauth/token",
		userInfoURL:    "https://api.notion.com/v1/users/me",
		authParams:     map[string]string{"owner": "user"},
		tokenBasicAuth: true,
		tokenJSONBody:  true,
		userInfoHeaders: map[string]string{
			"Notion-Version": "2022-06-28",
		},
		mapUser: func(d map[string]any) OAuthUserInfo {
			// /v1/users/me returns the bot user; the human is under bot.owner.user.
			owner := jSub(d, "bot", "owner", "user")
			if len(owner) == 0 {
				owner = d
			}
			return OAuthUserInfo{
				ProviderID: jStr(owner, "id"),
				Email:      jStr(jSub(owner, "person"), "email"),
				Name:       jStr(owner, "name"),
				AvatarURL:  jStr(owner, "avatar_url"),
			}
		},
	},
	"linear": {
		authURL:        "https://linear.app/oauth/authorize",
		tokenURL:       "https://api.linear.app/oauth/token",
		userInfoURL:    "https://api.linear.app/graphql",
		scopes:         []string{"read"},
		userInfoMethod: "POST",
		userInfoBody:   `{"query":"query { viewer { id name email avatarUrl } }"}`,
		userInfoHeaders: map[string]string{
			"Content-Type": "application/json",
		},
		mapUser: func(d map[string]any) OAuthUserInfo {
			viewer := jSub(d, "data", "viewer")
			return OAuthUserInfo{
				ProviderID: jStr(viewer, "id"),
				Email:      jStr(viewer, "email"),
				Name:       jStr(viewer, "name"),
				AvatarURL:  jStr(viewer, "avatarUrl"),
			}
		},
	},
	"slack": {
		authURL:     "https://slack.com/openid/connect/authorize",
		tokenURL:    "https://slack.com/api/openid.connect.token",
		userInfoURL: "https://slack.com/api/openid.connect.userInfo",
		scopes:      []string{"openid", "profile", "email"},
		mapUser:     oidcUserMapper,
	},
	"zoom": {
		authURL:        "https://zoom.us/oauth/authorize",
		tokenURL:       "https://zoom.us/oauth/token",
		userInfoURL:    "https://api.zoom.us/v2/users/me",
		tokenBasicAuth: true,
		mapUser: func(d map[string]any) OAuthUserInfo {
			name := strings.TrimSpace(jStr(d, "first_name") + " " + jStr(d, "last_name"))
			return OAuthUserInfo{
				ProviderID: jStr(d, "id"),
				Email:      jStr(d, "email"),
				Name:       firstNonEmpty(name, jStr(d, "display_name")),
				AvatarURL:  jStr(d, "pic_url"),
			}
		},
	},
	"box": {
		authURL:     "https://account.box.com/api/oauth2/authorize",
		tokenURL:    "https://api.box.com/oauth2/token",
		userInfoURL: "https://api.box.com/2.0/users/me",
		scopes:      []string{"root_readonly"},
		mapUser: func(d map[string]any) OAuthUserInfo {
			return OAuthUserInfo{
				ProviderID: jStr(d, "id"),
				Email:      jStr(d, "login"),
				Name:       jStr(d, "name"),
				AvatarURL:  jStr(d, "avatar_url"),
			}
		},
	},
	"dropbox": {
		authURL:        "https://www.dropbox.com/oauth2/authorize",
		tokenURL:       "https://api.dropboxapi.com/oauth2/token",
		userInfoURL:    "https://api.dropboxapi.com/2/users/get_current_account",
		userInfoMethod: "POST",
		mapUser: func(d map[string]any) OAuthUserInfo {
			return OAuthUserInfo{
				ProviderID: jStr(d, "account_id"),
				Email:      jStr(d, "email"),
				Name:       jStr(jSub(d, "name"), "display_name"),
				AvatarURL:  jStr(d, "profile_photo_url"),
			}
		},
	},
	"figma": {
		authURL:        "https://www.figma.com/oauth",
		tokenURL:       "https://api.figma.com/v1/oauth/token",
		userInfoURL:    "https://api.figma.com/v1/me",
		scopes:         []string{"file_read"},
		tokenBasicAuth: true,
		mapUser: func(d map[string]any) OAuthUserInfo {
			return OAuthUserInfo{
				ProviderID: jStr(d, "id"),
				Email:      jStr(d, "email"),
				Name:       jStr(d, "handle"),
				AvatarURL:  jStr(d, "img_url"),
			}
		},
	},
	"linkedin": {
		authURL:     "https://www.linkedin.com/oauth/v2/authorization",
		tokenURL:    "https://www.linkedin.com/oauth/v2/accessToken",
		userInfoURL: "https://api.linkedin.com/v2/userinfo",
		scopes:      []string{"openid", "profile", "email"},
		mapUser:     oidcUserMapper,
	},
	"patreon": {
		authURL:     "https://www.patreon.com/oauth2/authorize",
		tokenURL:    "https://www.patreon.com/api/oauth2/token",
		userInfoURL: "https://www.patreon.com/api/oauth2/v2/identity?fields%5Buser%5D=email,full_name,image_url",
		scopes:      []string{"identity", "identity[email]"},
		mapUser: func(d map[string]any) OAuthUserInfo {
			data := jSub(d, "data")
			attrs := jSub(data, "attributes")
			return OAuthUserInfo{
				ProviderID: jStr(data, "id"),
				Email:      jStr(attrs, "email"),
				Name:       jStr(attrs, "full_name"),
				AvatarURL:  jStr(attrs, "image_url"),
			}
		},
	},
	"reddit": {
		authURL:        "https://www.reddit.com/api/v1/authorize",
		tokenURL:       "https://www.reddit.com/api/v1/access_token",
		userInfoURL:    "https://oauth.reddit.com/api/v1/me",
		scopes:         []string{"identity"},
		authParams:     map[string]string{"duration": "temporary"},
		tokenBasicAuth: true,
		headers: map[string]string{
			// Reddit rejects requests without a descriptive User-Agent.
			"User-Agent": "gresbase-oauth/1.0",
		},
		mapUser: func(d map[string]any) OAuthUserInfo {
			// The identity scope exposes no email.
			return OAuthUserInfo{
				ProviderID: jStr(d, "id"),
				Name:       jStr(d, "name"),
				AvatarURL:  jStr(d, "icon_img"),
			}
		},
	},
	"wakatime": {
		authURL:     "https://wakatime.com/oauth/authorize",
		tokenURL:    "https://wakatime.com/oauth/token",
		userInfoURL: "https://wakatime.com/api/v1/users/current",
		scopes:      []string{"email"},
		mapUser: func(d map[string]any) OAuthUserInfo {
			u := jSub(d, "data")
			return OAuthUserInfo{
				ProviderID: jStr(u, "id"),
				Email:      jStr(u, "email"),
				Name:       firstNonEmpty(jStr(u, "display_name"), jStr(u, "username")),
				AvatarURL:  jStr(u, "photo"),
			}
		},
	},
	// Apple has no userinfo endpoint; identity comes from the id_token. The
	// config shape carries only client_id/client_secret, so clientSecret must
	// be a pre-generated Apple client-secret JWT (ES256, signed with the team
	// key) rather than the raw PEM key — note Apple caps its validity at six
	// months. Requesting the name/email scopes requires response_mode=
	// form_post, so the callback endpoint must accept POST.
	"apple": {
		authURL:         "https://appleid.apple.com/auth/authorize",
		tokenURL:        "https://appleid.apple.com/auth/token",
		scopes:          []string{"name", "email"},
		authParams:      map[string]string{"response_mode": "form_post"},
		userFromIDToken: true,
		mapUser: func(d map[string]any) OAuthUserInfo {
			return OAuthUserInfo{
				ProviderID: jStr(d, "sub"),
				Email:      jStr(d, "email"),
			}
		},
	},
}

// ---------------------------------------------------------------------------
// JSON helpers
// ---------------------------------------------------------------------------

// jStr reads a key as a string, formatting numeric IDs without decimals.
func jStr(data map[string]any, key string) string {
	switch v := data[key].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

// jSub walks nested objects, returning an empty map when any step is missing.
func jSub(data map[string]any, path ...string) map[string]any {
	cur := data
	for _, key := range path {
		next, _ := cur[key].(map[string]any)
		if next == nil {
			return map[string]any{}
		}
		cur = next
	}
	return cur
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// decodeJWTClaims extracts the claims of a JWT without verifying the
// signature; only used for tokens received directly from the provider's TLS
// token endpoint.
func decodeJWTClaims(token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("malformed id_token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("malformed id_token payload: %w", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("malformed id_token claims: %w", err)
	}
	return claims, nil
}
