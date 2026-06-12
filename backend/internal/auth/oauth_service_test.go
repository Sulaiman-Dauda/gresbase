package auth

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// OAuthService provider catalog (no DB required)
// ---------------------------------------------------------------------------

func TestOAuthServiceBuiltinCatalog(t *testing.T) {
	svc := NewOAuthService(nil)

	providers := svc.GetProviders()
	if len(providers) < 30 {
		t.Fatalf("expected at least 30 built-in providers, got %d", len(providers))
	}

	// every registered provider must carry its map key as Name
	for _, p := range providers {
		if p.Name == "" {
			t.Fatalf("provider with empty name: %+v", p)
		}
		got, ok := svc.GetProviderByName(p.Name)
		if !ok || got != p {
			t.Errorf("GetProviderByName(%s) mismatch", p.Name)
		}
	}

	if _, ok := svc.GetProviderByName("definitely-not-registered"); ok {
		t.Error("unknown provider should not resolve")
	}
}

func TestOAuthServiceGetProviderGuards(t *testing.T) {
	svc := NewOAuthService(nil)

	if _, err := svc.GetProvider("nope"); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Errorf("unknown provider error = %v", err)
	}

	// built-ins ship unconfigured: no client id, not enabled
	if _, err := svc.GetProvider("google"); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Errorf("unconfigured provider error = %v", err)
	}

	// a client id configures the provider
	p, _ := svc.GetProviderByName("google")
	p.ClientID = "cid"
	if _, err := svc.GetProvider("google"); err != nil {
		t.Errorf("configured provider: %v", err)
	}

	// the Enabled flag alone also configures it
	gh, _ := svc.GetProviderByName("github")
	gh.Enabled = true
	if _, err := svc.GetProvider("github"); err != nil {
		t.Errorf("enabled provider: %v", err)
	}
}

func TestOAuthServiceRegisterProviderForcesName(t *testing.T) {
	svc := NewOAuthService(nil)
	svc.RegisterProvider("custom", &OAuthProvider{Name: "mismatched", ClientID: "x"})
	p, ok := svc.GetProviderByName("custom")
	if !ok || p.Name != "custom" {
		t.Errorf("RegisterProvider should force the map key as Name, got %+v", p)
	}
}

func TestOAuthServiceProviderCatalog(t *testing.T) {
	svc := NewOAuthService(nil)

	all := svc.ProviderCatalog(false)
	if len(all) != len(svc.GetProviders()) {
		t.Errorf("catalog(false) length = %d, want %d", len(all), len(svc.GetProviders()))
	}
	// sorted by display name
	for i := 1; i < len(all); i++ {
		if all[i-1].DisplayName > all[i].DisplayName {
			t.Errorf("catalog not sorted: %q before %q", all[i-1].DisplayName, all[i].DisplayName)
			break
		}
	}
	for _, item := range all {
		if item.Configured {
			t.Errorf("fresh provider %q should not be configured", item.Name)
		}
	}

	// nothing configured → empty filtered catalog and no auth-method providers
	if got := svc.ProviderCatalog(true); len(got) != 0 {
		t.Errorf("catalog(true) with nothing configured = %v", got)
	}
	if got := svc.AuthMethodProviders(context.Background(), "https://cb"); len(got) != 0 {
		t.Errorf("AuthMethodProviders with nothing configured = %v", got)
	}

	// whitespace-only client id does not count as configured
	g, _ := svc.GetProviderByName("google")
	g.ClientID = "   "
	if got := svc.ProviderCatalog(true); len(got) != 0 {
		t.Errorf("whitespace client id should not configure: %v", got)
	}

	g.ClientID = "real-id"
	// a registered provider without display name falls back to its name
	svc.RegisterProvider("bare", &OAuthProvider{ClientID: "y"})
	catalog := svc.ProviderCatalog(true)
	if len(catalog) != 2 {
		t.Fatalf("catalog(true) = %v, want google + bare", catalog)
	}
	names := map[string]string{}
	for _, item := range catalog {
		if !item.Configured {
			t.Errorf("%q should be configured", item.Name)
		}
		names[item.Name] = item.DisplayName
	}
	if names["bare"] != "bare" {
		t.Errorf("empty DisplayName should fall back to name, got %q", names["bare"])
	}
	if names["google"] != "Google" {
		t.Errorf("google display name = %q", names["google"])
	}

	available := svc.GetAvailableProviders()
	if len(available) != 2 {
		t.Fatalf("GetAvailableProviders = %v", available)
	}
	for _, entry := range available {
		if entry["name"] == "" || entry["displayName"] == "" {
			t.Errorf("available provider entry incomplete: %v", entry)
		}
	}
}

func TestOAuthServiceAuthURLUnknownProvider(t *testing.T) {
	svc := NewOAuthService(nil)
	if _, _, err := svc.GetAuthURL("nope", "https://cb"); err == nil {
		t.Error("unknown provider should error before any DB access")
	}
	if _, err := svc.ExchangeCodeWithVerifier(context.Background(), "nope", "c", "s", "https://cb", ""); err == nil {
		t.Error("unknown provider should error before any DB access")
	}
}

// ---------------------------------------------------------------------------
// PKCE helpers
// ---------------------------------------------------------------------------

func TestGenerateCodeChallengeRFC7636Vector(t *testing.T) {
	// Test vector from RFC 7636 appendix B.
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	want := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if got := generateCodeChallenge(verifier); got != want {
		t.Errorf("generateCodeChallenge = %q, want %q", got, want)
	}
}

func TestGenerateCodeVerifier(t *testing.T) {
	v1 := generateCodeVerifier()
	v2 := generateCodeVerifier()
	if v1 == v2 {
		t.Error("code verifiers should be random")
	}
	raw, err := base64.RawURLEncoding.DecodeString(v1)
	if err != nil {
		t.Fatalf("verifier is not base64url: %v", err)
	}
	if len(raw) != 32 {
		t.Errorf("verifier entropy = %d bytes, want 32", len(raw))
	}
}

func TestGenerateOAuthRandomString(t *testing.T) {
	s := generateOAuthRandomString(32)
	if len(s) != 32 {
		t.Errorf("length = %d, want 32", len(s))
	}
	if s == generateOAuthRandomString(32) {
		t.Error("random strings should differ")
	}
}

func TestSha256Sum(t *testing.T) {
	got := sha256Sum([]byte("abc"))
	want := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	hex := ""
	for _, b := range got {
		const digits = "0123456789abcdef"
		hex += string(digits[b>>4]) + string(digits[b&0xf])
	}
	if hex != want {
		t.Errorf("sha256Sum(abc) = %s, want %s", hex, want)
	}
}

// ---------------------------------------------------------------------------
// OAuthService.fetchUserInfo — provider userinfo normalization
// ---------------------------------------------------------------------------

func TestOAuthServiceFetchUserInfoMapping(t *testing.T) {
	cases := []struct {
		provider string
		fixture  string
		want     OAuthUserInfo
	}{
		{"google", `{"sub":"g1","email":"g@x.com","name":"G","picture":"pic"}`,
			OAuthUserInfo{ProviderID: "g1", Email: "g@x.com", Name: "G", AvatarURL: "pic"}},
		{"github", `{"id":42,"login":"octo","email":"o@x.com","avatar_url":"av"}`,
			OAuthUserInfo{ProviderID: "42", Email: "o@x.com", Name: "octo", AvatarURL: "av"}},
		// no public email → synthetic placeholder, never "<nil>"
		{"github", `{"id":43,"login":"oct2","avatar_url":"av2"}`,
			OAuthUserInfo{ProviderID: "43", Email: "oct2@github.users", Name: "oct2", AvatarURL: "av2"}},
		{"microsoft", `{"id":"m1","mail":"m@x.com","displayName":"MS"}`,
			OAuthUserInfo{ProviderID: "m1", Email: "m@x.com", Name: "MS"}},
		// no mail → userPrincipalName fallback
		{"microsoft", `{"id":"m2","userPrincipalName":"upn@x.com","displayName":"MS2"}`,
			OAuthUserInfo{ProviderID: "m2", Email: "upn@x.com", Name: "MS2"}},
		{"discord", `{"id":"d1","email":"d@x.com","username":"disc","avatar":"abc"}`,
			OAuthUserInfo{ProviderID: "d1", Email: "d@x.com", Name: "disc", AvatarURL: "https://cdn.discordapp.com/avatars/d1/abc.png"}},
		{"gitlab", `{"id":7,"email":"gl@x.com","name":"GL","avatar_url":"glav"}`,
			OAuthUserInfo{ProviderID: "7", Email: "gl@x.com", Name: "GL", AvatarURL: "glav"}},
		{"slack", `{"sub":"s1","email":"s@x.com","name":"S","picture":"sp"}`,
			OAuthUserInfo{ProviderID: "s1", Email: "s@x.com", Name: "S", AvatarURL: "sp"}},
		{"facebook", `{"id":"f1","email":"f@x.com","name":"F","picture":{"data":{"url":"fp"}}}`,
			OAuthUserInfo{ProviderID: "f1", Email: "f@x.com", Name: "F", AvatarURL: "fp"}},
		{"twitter", `{"data":{"id":"t1","name":"T","email":"t@x.com"}}`,
			OAuthUserInfo{ProviderID: "t1", Email: "t@x.com", Name: "T"}},
		// v1-style payload without the data wrapper
		{"twitter", `{"id":"t2","name":"T2"}`,
			OAuthUserInfo{ProviderID: "t2", Name: "T2"}},
		{"spotify", `{"id":"sp1","email":"sp@x.com","display_name":"SP","images":[{"url":"spi"}]}`,
			OAuthUserInfo{ProviderID: "sp1", Email: "sp@x.com", Name: "SP", AvatarURL: "spi"}},
		{"twitch", `{"data":[{"id":"tw1","display_name":"TW","email":"tw@x.com","profile_image_url":"twp"}]}`,
			OAuthUserInfo{ProviderID: "tw1", Email: "tw@x.com", Name: "TW", AvatarURL: "twp"}},
		{"reddit", `{"id":"r1","name":"red","icon_img":"ri"}`,
			OAuthUserInfo{ProviderID: "r1", Name: "red", AvatarURL: "ri"}},
		{"linkedin", `{"sub":"l1","email":"l@x.com","name":"L","picture":"lp"}`,
			OAuthUserInfo{ProviderID: "l1", Email: "l@x.com", Name: "L", AvatarURL: "lp"}},
		{"dropbox", `{"account_id":"db1","email":"db@x.com","name":{"display_name":"DB"}}`,
			OAuthUserInfo{ProviderID: "db1", Email: "db@x.com", Name: "DB"}},
		{"bitbucket", `{"account_id":"bb1","display_name":"BB","links":{"avatar":{"href":"bba"}}}`,
			OAuthUserInfo{ProviderID: "bb1", Name: "BB", AvatarURL: "bba"}},
		{"vk", `{"response":[{"id":11,"first_name":"V","last_name":"K","photo_200":"vp"}],"email":"v@x.com"}`,
			OAuthUserInfo{ProviderID: "11", Email: "v@x.com", Name: "V K", AvatarURL: "vp"}},
		{"kakao", `{"id":10,"kakao_account":{"email":"k@x.com","profile":{"nickname":"KK","profile_image_url":"kp"}}}`,
			OAuthUserInfo{ProviderID: "10", Email: "k@x.com", Name: "KK", AvatarURL: "kp"}},
		{"naver", `{"response":{"id":"n1","email":"n@x.com","name":"N","profile_image":"np"}}`,
			OAuthUserInfo{ProviderID: "n1", Email: "n@x.com", Name: "N", AvatarURL: "np"}},
		{"line", `{"userId":"ln1","displayName":"LN","pictureUrl":"lnp","email":"ln@x.com"}`,
			OAuthUserInfo{ProviderID: "ln1", Email: "ln@x.com", Name: "LN", AvatarURL: "lnp"}},
		{"yahoo", `{"sub":"y1","email":"y@x.com","name":"Y","picture":"yp"}`,
			OAuthUserInfo{ProviderID: "y1", Email: "y@x.com", Name: "Y", AvatarURL: "yp"}},
		{"yandex", `{"id":"ya1","default_email":"ya@x.com","real_name":"YA"}`,
			OAuthUserInfo{ProviderID: "ya1", Email: "ya@x.com", Name: "YA"}},
		{"auth0", `{"sub":"a1","email":"a@x.com","name":"A","picture":"ap"}`,
			OAuthUserInfo{ProviderID: "a1", Email: "a@x.com", Name: "A", AvatarURL: "ap"}},
		// OIDC group falls back to given+family name
		{"okta", `{"sub":"o1","email":"o@x.com","given_name":"G","family_name":"F","picture":"op"}`,
			OAuthUserInfo{ProviderID: "o1", Email: "o@x.com", Name: "G F", AvatarURL: "op"}},
		// gitea name fallback chain: name → full_name → login
		{"gitea", `{"id":5,"login":"lg","email":"ge@x.com","avatar_url":"ga"}`,
			OAuthUserInfo{ProviderID: "5", Email: "ge@x.com", Name: "lg", AvatarURL: "ga"}},
		{"gitee", `{"id":6,"name":"GiteeN","email":"gt@x.com","avatar_url":"gta"}`,
			OAuthUserInfo{ProviderID: "6", Email: "gt@x.com", Name: "GiteeN", AvatarURL: "gta"}},
		{"instagram", `{"user_id":"ig1","username":"insta","profile_picture_url":"igp"}`,
			OAuthUserInfo{ProviderID: "ig1", Name: "insta", AvatarURL: "igp"}},
		// older instagram payloads use "id"
		{"instagram", `{"id":"ig2","username":"insta2","profile_picture_url":"ig2p"}`,
			OAuthUserInfo{ProviderID: "ig2", Name: "insta2", AvatarURL: "ig2p"}},
		{"zoom", `{"id":"z1","email":"z@x.com","first_name":"Zo","last_name":"Om","pic_url":"zp"}`,
			OAuthUserInfo{ProviderID: "z1", Email: "z@x.com", Name: "Zo Om", AvatarURL: "zp"}},
		{"box", `{"id":"bx1","login":"bx@x.com","name":"BX","avatar_url":"bxa"}`,
			OAuthUserInfo{ProviderID: "bx1", Email: "bx@x.com", Name: "BX", AvatarURL: "bxa"}},
		{"wakatime", `{"data":{"id":"w1","email":"w@x.com","display_name":"W","photo":"wp"}}`,
			OAuthUserInfo{ProviderID: "w1", Email: "w@x.com", Name: "W", AvatarURL: "wp"}},
		{"strava", `{"id":9,"firstname":"St","lastname":"Ra","profile":"stp"}`,
			OAuthUserInfo{ProviderID: "9", Name: "St Ra", AvatarURL: "stp"}},
		{"figma", `{"id":"fg1","email":"fg@x.com","handle":"FG","img_url":"fgp"}`,
			OAuthUserInfo{ProviderID: "fg1", Email: "fg@x.com", Name: "FG", AvatarURL: "fgp"}},
		{"patreon", `{"data":{"id":"pt1","attributes":{"email":"pt@x.com","full_name":"PT","image_url":"ptp"}}}`,
			OAuthUserInfo{ProviderID: "pt1", Email: "pt@x.com", Name: "PT", AvatarURL: "ptp"}},
		{"notion", `{"owner":{"user":{"id":"nt1","name":"NT","avatar_url":"nta","person":{"email":"nt@x.com"}}}}`,
			OAuthUserInfo{ProviderID: "nt1", Email: "nt@x.com", Name: "NT", AvatarURL: "nta"}},
		// unregistered provider names use the generic OIDC mapping
		{"customx", `{"sub":"c1","email":"c@x.com","name":"C","picture":"cp"}`,
			OAuthUserInfo{ProviderID: "c1", Email: "c@x.com", Name: "C", AvatarURL: "cp"}},
	}

	svc := NewOAuthService(nil)

	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "missing bearer", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Accept") != "application/json" {
			http.Error(w, "missing accept", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	defer srv.Close()

	for _, tc := range cases {
		body = tc.fixture
		p := &OAuthProvider{Name: tc.provider, UserInfoURL: srv.URL}
		got, err := svc.fetchUserInfo(context.Background(), p, "test-token")
		if err != nil {
			t.Errorf("%s: fetchUserInfo: %v", tc.provider, err)
			continue
		}
		if got.ProviderID != tc.want.ProviderID {
			t.Errorf("%s: ProviderID = %q, want %q", tc.provider, got.ProviderID, tc.want.ProviderID)
		}
		if got.Email != tc.want.Email {
			t.Errorf("%s: Email = %q, want %q", tc.provider, got.Email, tc.want.Email)
		}
		if got.Name != tc.want.Name {
			t.Errorf("%s: Name = %q, want %q", tc.provider, got.Name, tc.want.Name)
		}
		if got.AvatarURL != tc.want.AvatarURL {
			t.Errorf("%s: AvatarURL = %q, want %q", tc.provider, got.AvatarURL, tc.want.AvatarURL)
		}
		if len(got.RawJSON) == 0 {
			t.Errorf("%s: RawJSON should carry the raw payload", tc.provider)
		}
	}
}

func TestOAuthServiceFetchUserInfoEdgeCases(t *testing.T) {
	svc := NewOAuthService(nil)
	ctx := context.Background()

	// providers without a userinfo endpoint (e.g. Apple) return an empty shell
	got, err := svc.fetchUserInfo(ctx, &OAuthProvider{Name: "apple", UserInfoURL: ""}, "tok")
	if err != nil || got == nil || got.ProviderID != "" {
		t.Errorf("empty UserInfoURL = %+v, %v", got, err)
	}

	// non-JSON payload is an error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html>nope</html>"))
	}))
	if _, err := svc.fetchUserInfo(ctx, &OAuthProvider{Name: "google", UserInfoURL: srv.URL}, "tok"); err == nil {
		t.Error("non-JSON userinfo should error")
	}
	srv.Close()

	// unreachable endpoint is an error
	if _, err := svc.fetchUserInfo(ctx, &OAuthProvider{Name: "google", UserInfoURL: srv.URL}, "tok"); err == nil {
		t.Error("unreachable userinfo endpoint should error")
	}
}
