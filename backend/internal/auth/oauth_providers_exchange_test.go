package auth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// routeFunc lets tests intercept a provider's hardcoded endpoints without any
// network access by swapping the provider's HTTP client transport.
type routeFunc func(req *http.Request) (*http.Response, error)

func (f routeFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func jsonHTTPResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func parseFormBody(t *testing.T, req *http.Request) url.Values {
	t.Helper()
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	vals, err := url.ParseQuery(string(raw))
	if err != nil {
		t.Fatalf("parse form: %v", err)
	}
	return vals
}

// ---------------------------------------------------------------------------
// GoogleProvider
// ---------------------------------------------------------------------------

func TestGoogleProviderExchangeCode(t *testing.T) {
	p := NewGoogleProvider("cid", "sec")
	var tokenForm url.Values
	p.httpClient = &http.Client{Transport: routeFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "oauth2.googleapis.com":
			tokenForm = parseFormBody(t, req)
			return jsonHTTPResponse(200, `{"access_token":"gtok","token_type":"Bearer"}`), nil
		case "www.googleapis.com":
			if req.Header.Get("Authorization") != "Bearer gtok" {
				return jsonHTTPResponse(401, `{}`), nil
			}
			return jsonHTTPResponse(200, `{"id":"g1","email":"g@x.com","name":"G User","picture":"pic","verified_email":true}`), nil
		default:
			return nil, fmt.Errorf("unexpected host %s", req.URL.Host)
		}
	})}

	info, err := p.ExchangeCode(context.Background(), "the-code", "https://cb")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if tokenForm.Get("client_id") != "cid" || tokenForm.Get("client_secret") != "sec" ||
		tokenForm.Get("code") != "the-code" || tokenForm.Get("grant_type") != "authorization_code" ||
		tokenForm.Get("redirect_uri") != "https://cb" {
		t.Errorf("token request form = %v", tokenForm)
	}
	if info.Provider != "google" || info.ProviderID != "g1" || info.Email != "g@x.com" ||
		info.Name != "G User" || info.AvatarURL != "pic" {
		t.Errorf("user info = %+v", info)
	}
}

func TestGoogleProviderExchangeCodeTokenError(t *testing.T) {
	p := NewGoogleProvider("cid", "sec")
	p.httpClient = &http.Client{Transport: routeFunc(func(req *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(400, `{"error":"invalid_grant"}`), nil
	})}
	if _, err := p.ExchangeCode(context.Background(), "bad", "https://cb"); err == nil || !strings.Contains(err.Error(), "invalid_grant") {
		t.Errorf("expected invalid_grant error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// GithubProvider
// ---------------------------------------------------------------------------

func githubTransport(t *testing.T, emailsJSON string) http.RoundTripper {
	t.Helper()
	return routeFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.URL.Host == "github.com":
			if req.Header.Get("Accept") != "application/json" {
				t.Error("token request must accept JSON (GitHub defaults to form encoding)")
			}
			return jsonHTTPResponse(200, `{"access_token":"ghtok","token_type":"bearer"}`), nil
		case req.URL.Host == "api.github.com" && req.URL.Path == "/user":
			if req.Header.Get("Authorization") != "Bearer ghtok" {
				return jsonHTTPResponse(401, `{}`), nil
			}
			return jsonHTTPResponse(200, `{"id":42,"login":"octo","name":"Octo Cat","email":"public@x.com","avatar_url":"av"}`), nil
		case req.URL.Host == "api.github.com" && req.URL.Path == "/user/emails":
			return jsonHTTPResponse(200, emailsJSON), nil
		default:
			return nil, fmt.Errorf("unexpected request %s", req.URL)
		}
	})
}

func TestGithubProviderExchangePrimaryEmail(t *testing.T) {
	p := NewGithubProvider("cid", "sec")
	p.httpClient = &http.Client{Transport: githubTransport(t, `[
		{"email":"old@x.com","primary":false,"verified":true},
		{"email":"primary@x.com","primary":true,"verified":true}
	]`)}

	info, err := p.ExchangeCode(context.Background(), "code", "https://cb")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if info.Email != "primary@x.com" {
		t.Errorf("Email = %q, want the primary verified address", info.Email)
	}
	if info.Provider != "github" || info.ProviderID != "42" || info.Name != "Octo Cat" || info.AvatarURL != "av" {
		t.Errorf("user info = %+v", info)
	}
}

func TestGithubProviderExchangeEmailFallbacks(t *testing.T) {
	// no primary: first verified email wins
	p := NewGithubProvider("cid", "sec")
	p.httpClient = &http.Client{Transport: githubTransport(t, `[
		{"email":"unverified@x.com","primary":true,"verified":false},
		{"email":"verified@x.com","primary":false,"verified":true}
	]`)}
	info, err := p.ExchangeCode(context.Background(), "code", "https://cb")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if info.Email != "verified@x.com" {
		t.Errorf("Email = %q, want first verified", info.Email)
	}

	// emails endpoint broken: the profile email is kept
	p = NewGithubProvider("cid", "sec")
	p.httpClient = &http.Client{Transport: githubTransport(t, `not-json`)}
	info, err = p.ExchangeCode(context.Background(), "code", "https://cb")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if info.Email != "public@x.com" {
		t.Errorf("Email = %q, want profile email fallback", info.Email)
	}

	// no verified emails at all: profile email is kept
	p = NewGithubProvider("cid", "sec")
	p.httpClient = &http.Client{Transport: githubTransport(t, `[{"email":"nv@x.com","primary":true,"verified":false}]`)}
	info, err = p.ExchangeCode(context.Background(), "code", "https://cb")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if info.Email != "public@x.com" {
		t.Errorf("Email = %q, want profile email", info.Email)
	}
}

func TestGithubProviderExchangeTokenError(t *testing.T) {
	p := NewGithubProvider("cid", "sec")
	p.httpClient = &http.Client{Transport: routeFunc(func(req *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(200, `{"error":"bad_verification_code"}`), nil
	})}
	if _, err := p.ExchangeCode(context.Background(), "x", "https://cb"); err == nil || !strings.Contains(err.Error(), "bad_verification_code") {
		t.Errorf("expected token error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// MicrosoftProvider
// ---------------------------------------------------------------------------

func TestMicrosoftProviderExchangeAndTenantDefault(t *testing.T) {
	p := NewMicrosoftProvider("cid", "sec", "")
	if p.tenant != "common" {
		t.Errorf("empty tenant should default to common, got %q", p.tenant)
	}
	authURL, err := p.GetAuthURL("st", "https://cb")
	if err != nil || !strings.Contains(authURL, "/common/oauth2/v2.0/authorize") {
		t.Errorf("auth url = %q, %v", authURL, err)
	}

	p.httpClient = &http.Client{Transport: routeFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "login.microsoftonline.com":
			if !strings.Contains(req.URL.Path, "/common/") {
				t.Errorf("token URL should use the common tenant: %s", req.URL.Path)
			}
			return jsonHTTPResponse(200, `{"access_token":"mstok"}`), nil
		case "graph.microsoft.com":
			if req.Header.Get("Authorization") != "Bearer mstok" {
				return jsonHTTPResponse(401, `{}`), nil
			}
			// no mail attribute → userPrincipalName is the fallback
			return jsonHTTPResponse(200, `{"id":"m1","userPrincipalName":"upn@x.com","displayName":"MS User","mail":""}`), nil
		default:
			return nil, fmt.Errorf("unexpected host %s", req.URL.Host)
		}
	})}

	info, err := p.ExchangeCode(context.Background(), "code", "https://cb")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if info.Provider != "microsoft" || info.ProviderID != "m1" || info.Email != "upn@x.com" || info.Name != "MS User" {
		t.Errorf("user info = %+v", info)
	}
}

func TestMicrosoftProviderExchangeTokenError(t *testing.T) {
	p := NewMicrosoftProvider("cid", "sec", "common")
	p.httpClient = &http.Client{Transport: routeFunc(func(req *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(400, `{"error":"invalid_client"}`), nil
	})}
	if _, err := p.ExchangeCode(context.Background(), "x", "https://cb"); err == nil {
		t.Error("expected token error")
	}
}

// ---------------------------------------------------------------------------
// GitlabProvider (configurable base URL — no transport tricks needed)
// ---------------------------------------------------------------------------

func TestGitlabProviderExchangeSelfHosted(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"gltok"}`))
	})
	mux.HandleFunc("/api/v4/user", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer gltok" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":7,"username":"gl","name":"GL User","email":"gl@x.com","avatar_url":"glav"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := NewGitlabProvider("cid", "sec", srv.URL)
	authURL, err := p.GetAuthURL("st", "https://cb")
	if err != nil || !strings.HasPrefix(authURL, srv.URL+"/oauth/authorize") {
		t.Errorf("self-hosted auth url = %q, %v", authURL, err)
	}

	info, err := p.ExchangeCode(context.Background(), "code", "https://cb")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if info.Provider != "gitlab" || info.ProviderID != "7" || info.Email != "gl@x.com" ||
		info.Name != "GL User" || info.AvatarURL != "glav" {
		t.Errorf("user info = %+v", info)
	}

	// default base URL
	def := NewGitlabProvider("cid", "sec", "")
	u, _ := def.GetAuthURL("s", "https://cb")
	if !strings.HasPrefix(u, "https://gitlab.com/oauth/authorize") {
		t.Errorf("default base url = %q", u)
	}
}

func TestGitlabProviderExchangeTokenError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer srv.Close()
	p := NewGitlabProvider("cid", "sec", srv.URL)
	if _, err := p.ExchangeCode(context.Background(), "x", "https://cb"); err == nil {
		t.Error("expected token error")
	}
}

// ---------------------------------------------------------------------------
// DiscordProvider
// ---------------------------------------------------------------------------

func TestDiscordProviderExchangeCode(t *testing.T) {
	p := NewDiscordProvider("cid", "sec")
	p.httpClient = &http.Client{Transport: routeFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/oauth2/token":
			return jsonHTTPResponse(200, `{"access_token":"dtok"}`), nil
		case "/api/users/@me":
			if req.Header.Get("Authorization") != "Bearer dtok" {
				return jsonHTTPResponse(401, `{}`), nil
			}
			return jsonHTTPResponse(200, `{"id":"d1","username":"disc","email":"d@x.com","avatar":"abc","discriminator":"0001"}`), nil
		default:
			return nil, fmt.Errorf("unexpected path %s", req.URL.Path)
		}
	})}

	info, err := p.ExchangeCode(context.Background(), "code", "https://cb")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if info.Provider != "discord" || info.ProviderID != "d1" || info.Email != "d@x.com" || info.Name != "disc" {
		t.Errorf("user info = %+v", info)
	}
	if info.AvatarURL != "https://cdn.discordapp.com/avatars/d1/abc.png" {
		t.Errorf("avatar = %q", info.AvatarURL)
	}
}

func TestDiscordProviderExchangeTokenError(t *testing.T) {
	p := NewDiscordProvider("cid", "sec")
	p.httpClient = &http.Client{Transport: routeFunc(func(req *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(400, `{"error":"invalid_request"}`), nil
	})}
	if _, err := p.ExchangeCode(context.Background(), "x", "https://cb"); err == nil {
		t.Error("expected token error")
	}
}

// ---------------------------------------------------------------------------
// Registry construction from config
// ---------------------------------------------------------------------------

func TestBuildOAuthRegistry(t *testing.T) {
	type creds = struct {
		ClientID     string
		ClientSecret string
	}
	reg := BuildOAuthRegistry(nil, map[string]creds{
		"google":    {"a", "b"},
		"github":    {"a", "b"},
		"microsoft": {"a", "b"},
		"gitlab":    {"a", "b"},
		"discord":   {"a", "b"},
		"apple":     {"a", "b"}, // standard catalog provider
		"bogus":     {"a", "b"}, // unknown: skipped with a warning
	})

	for _, name := range []string{"google", "github", "microsoft", "gitlab", "discord", "apple"} {
		if _, err := reg.Get(name); err != nil {
			t.Errorf("Get(%s): %v", name, err)
		}
	}
	if _, err := reg.Get("bogus"); err == nil {
		t.Error("unknown provider should not be registered")
	}

	names := reg.List()
	if len(names) != 6 {
		t.Errorf("List() = %v, want 6 providers", names)
	}

	// providers without credentials are registered but disabled
	reg2 := NewProviderRegistry(nil)
	reg2.Register(NewGoogleProvider("", ""))
	if _, err := reg2.Get("google"); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Errorf("disabled provider Get = %v", err)
	}
	if got := reg2.List(); len(got) != 0 {
		t.Errorf("List should skip disabled providers, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// Extra provider constructors and helpers (oauth_providers_extra.go)
// ---------------------------------------------------------------------------

func TestNewGiteaProviderBaseURL(t *testing.T) {
	def := NewGiteaProvider("cid", "sec", "")
	u, err := def.GetAuthURL("st", "https://cb")
	if err != nil || !strings.HasPrefix(u, "https://gitea.com/login/oauth/authorize") {
		t.Errorf("default gitea auth url = %q, %v", u, err)
	}

	custom := NewGiteaProvider("cid", "sec", "https://git.example.com")
	u, _ = custom.GetAuthURL("st", "https://cb")
	if !strings.HasPrefix(u, "https://git.example.com/login/oauth/authorize") {
		t.Errorf("custom gitea auth url = %q", u)
	}
	if custom.Name() != "gitea" || !custom.IsEnabled() {
		t.Errorf("gitea provider identity wrong: %s enabled=%v", custom.Name(), custom.IsEnabled())
	}
}

func TestNewOIDCProviderFullExchange(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"oidctok"}`))
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// no "name" claim: the mapper must combine given/family names
		w.Write([]byte(`{"sub":"u1","email":"u@x.com","given_name":"Ada","family_name":"Lovelace"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := NewOIDCProvider("corp-sso", "cid", "sec", srv.URL+"/auth", srv.URL+"/token", srv.URL+"/userinfo")
	if p.Name() != "corp-sso" || !p.IsEnabled() {
		t.Fatalf("provider identity wrong: %s enabled=%v", p.Name(), p.IsEnabled())
	}

	authURL, err := p.GetAuthURL("st", "https://cb")
	if err != nil || !strings.Contains(authURL, "scope=openid+profile+email") {
		t.Errorf("auth url = %q, %v", authURL, err)
	}

	info, err := p.ExchangeCode(context.Background(), "code", "https://cb")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if info.Provider != "corp-sso" || info.ProviderID != "u1" || info.Email != "u@x.com" || info.Name != "Ada Lovelace" {
		t.Errorf("user info = %+v", info)
	}
}

func TestStandardProviderUserInfoErrors(t *testing.T) {
	// non-2xx userinfo status must fail the exchange
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"tok"}`))
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := NewOIDCProvider("erroring", "cid", "sec", srv.URL+"/auth", srv.URL+"/token", srv.URL+"/userinfo")
	if _, err := p.ExchangeCode(context.Background(), "c", "https://cb"); err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("expected 500 userinfo error, got %v", err)
	}

	// invalid JSON userinfo must fail too
	mux2 := http.NewServeMux()
	mux2.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"tok"}`))
	})
	mux2.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html>"))
	})
	srv2 := httptest.NewServer(mux2)
	defer srv2.Close()

	p2 := NewOIDCProvider("badjson", "cid", "sec", srv2.URL+"/auth", srv2.URL+"/token", srv2.URL+"/userinfo")
	if _, err := p2.ExchangeCode(context.Background(), "c", "https://cb"); err == nil {
		t.Error("expected JSON parse error from userinfo")
	}
}

func TestNewStandardProviderUnknownName(t *testing.T) {
	if _, ok := NewStandardProvider("not-in-catalog", "a", "b"); ok {
		t.Error("unknown catalog name should return ok=false")
	}
}

func TestDecodeJWTClaimsErrors(t *testing.T) {
	if _, err := decodeJWTClaims("only-one-part"); err == nil {
		t.Error("malformed token should error")
	}
	if _, err := decodeJWTClaims("head.!!!notbase64!!!.sig"); err == nil {
		t.Error("invalid base64 payload should error")
	}
	if _, err := decodeJWTClaims("head." + "bm90LWpzb24" + ".sig"); err == nil {
		t.Error("non-JSON payload should error")
	}
	claims, err := decodeJWTClaims("h.eyJzdWIiOiJ4In0.s")
	if err != nil || claims["sub"] != "x" {
		t.Errorf("valid payload = %v, %v", claims, err)
	}
}

func TestFirstNonEmptyAndJSub(t *testing.T) {
	if got := firstNonEmpty("", "x", "y"); got != "x" {
		t.Errorf("firstNonEmpty = %q", got)
	}
	if got := firstNonEmpty(); got != "" {
		t.Errorf("firstNonEmpty() = %q", got)
	}
	if got := firstNonEmpty("", ""); got != "" {
		t.Errorf("firstNonEmpty all empty = %q", got)
	}

	data := map[string]any{"a": map[string]any{"b": map[string]any{"c": 1}}}
	if got := jSub(data, "a", "b"); got["c"] != 1 {
		t.Errorf("jSub nested = %v", got)
	}
	// missing or non-map steps yield an empty (never nil) map
	if got := jSub(data, "a", "missing"); got == nil || len(got) != 0 {
		t.Errorf("jSub missing = %v", got)
	}
	if got := jSub(data, "a", "b", "c"); got == nil || len(got) != 0 {
		t.Errorf("jSub non-map leaf = %v", got)
	}
}
