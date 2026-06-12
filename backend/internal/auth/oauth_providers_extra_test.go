package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func allTestProviders(t *testing.T) []OAuthIdentityProvider {
	t.Helper()
	providers := []OAuthIdentityProvider{
		NewGoogleProvider("test-client-id", "test-secret"),
		NewGithubProvider("test-client-id", "test-secret"),
		NewMicrosoftProvider("test-client-id", "test-secret", ""),
		NewGitlabProvider("test-client-id", "test-secret", ""),
		NewDiscordProvider("test-client-id", "test-secret"),
	}
	for _, name := range StandardProviderNames() {
		p, ok := NewStandardProvider(name, "test-client-id", "test-secret")
		if !ok {
			t.Fatalf("catalog provider %q not constructible", name)
		}
		providers = append(providers, p)
	}
	return providers
}

func TestOAuthProviderCatalog(t *testing.T) {
	providers := allTestProviders(t)
	if len(providers) < 28 {
		t.Fatalf("expected at least 28 providers, got %d", len(providers))
	}

	reg := NewProviderRegistry(nil)
	seen := make(map[string]bool)
	for _, p := range providers {
		name := p.Name()
		if name == "" {
			t.Fatal("provider with empty name")
		}
		if seen[name] {
			t.Fatalf("duplicate provider name: %s", name)
		}
		seen[name] = true

		if !p.IsEnabled() {
			t.Fatalf("%s: expected enabled with credentials set", name)
		}
		reg.Register(p)

		authURL, err := p.GetAuthURL("state123", "https://example.com/callback")
		if err != nil {
			t.Fatalf("%s: GetAuthURL: %v", name, err)
		}
		u, err := url.Parse(authURL)
		if err != nil {
			t.Fatalf("%s: unparseable auth URL %q: %v", name, authURL, err)
		}
		q := u.Query()
		if q.Get("client_id") != "test-client-id" {
			t.Errorf("%s: missing client_id in auth URL %q", name, authURL)
		}
		if q.Get("state") != "state123" {
			t.Errorf("%s: missing state in auth URL %q", name, authURL)
		}
		if q.Get("redirect_uri") != "https://example.com/callback" {
			t.Errorf("%s: missing redirect_uri in auth URL %q", name, authURL)
		}
	}

	for name := range seen {
		if _, err := reg.Get(name); err != nil {
			t.Errorf("registry Get(%s): %v", name, err)
		}
	}
}

func TestOAuthProviderDisabledWithoutCredentials(t *testing.T) {
	for _, name := range StandardProviderNames() {
		p, _ := NewStandardProvider(name, "", "")
		if p.IsEnabled() {
			t.Errorf("%s: enabled without credentials", name)
		}
	}
}

func TestOAuthProviderUserInfoMapping(t *testing.T) {
	cases := []struct {
		provider string
		fixture  string
		want     OAuthUserInfo
	}{
		{
			"facebook",
			`{"id":"fb1","name":"Face Book","email":"f@b.com","picture":{"data":{"url":"http://a/p.png"}}}`,
			OAuthUserInfo{ProviderID: "fb1", Email: "f@b.com", Name: "Face Book", AvatarURL: "http://a/p.png"},
		},
		{
			"instagram",
			`{"user_id":"ig1","username":"insta","profile_picture_url":"http://a/i.png"}`,
			OAuthUserInfo{ProviderID: "ig1", Name: "insta", AvatarURL: "http://a/i.png"},
		},
		{
			"bitbucket",
			`{"account_id":"bb1","display_name":"Bit Bucket","links":{"avatar":{"href":"http://a/b.png"}}}`,
			OAuthUserInfo{ProviderID: "bb1", Name: "Bit Bucket", AvatarURL: "http://a/b.png"},
		},
		{
			"gitea",
			`{"id":7,"login":"gt","full_name":"Gi Tea","email":"g@t.com","avatar_url":"http://a/g.png"}`,
			OAuthUserInfo{ProviderID: "7", Email: "g@t.com", Name: "Gi Tea", AvatarURL: "http://a/g.png"},
		},
		{
			"gitee",
			`{"id":8,"login":"ge","name":"Gi Tee","email":"g@e.com","avatar_url":"http://a/ge.png"}`,
			OAuthUserInfo{ProviderID: "8", Email: "g@e.com", Name: "Gi Tee", AvatarURL: "http://a/ge.png"},
		},
		{
			"spotify",
			`{"id":"sp1","email":"s@p.com","display_name":"Spot","images":[{"url":"http://a/s.png"}]}`,
			OAuthUserInfo{ProviderID: "sp1", Email: "s@p.com", Name: "Spot", AvatarURL: "http://a/s.png"},
		},
		{
			"strava",
			`{"id":9,"firstname":"Str","lastname":"Ava","profile":"http://a/st.png"}`,
			OAuthUserInfo{ProviderID: "9", Name: "Str Ava", AvatarURL: "http://a/st.png"},
		},
		{
			"twitch",
			`{"data":[{"id":"tw1","login":"tw","display_name":"Twitch","email":"t@w.com","profile_image_url":"http://a/t.png"}]}`,
			OAuthUserInfo{ProviderID: "tw1", Email: "t@w.com", Name: "Twitch", AvatarURL: "http://a/t.png"},
		},
		{
			"kakao",
			`{"id":10,"kakao_account":{"email":"k@k.com","profile":{"nickname":"Kak","profile_image_url":"http://a/k.png"}}}`,
			OAuthUserInfo{ProviderID: "10", Email: "k@k.com", Name: "Kak", AvatarURL: "http://a/k.png"},
		},
		{
			"line",
			`{"userId":"ln1","displayName":"Line","pictureUrl":"http://a/l.png"}`,
			OAuthUserInfo{ProviderID: "ln1", Name: "Line", AvatarURL: "http://a/l.png"},
		},
		{
			"vk",
			`{"response":[{"id":11,"first_name":"Vi","last_name":"Kay","photo_200":"http://a/v.png"}]}`,
			OAuthUserInfo{ProviderID: "11", Name: "Vi Kay", AvatarURL: "http://a/v.png"},
		},
		{
			"yandex",
			`{"id":"ya1","default_email":"y@a.com","real_name":"Yan Dex","default_avatar_id":"av1","is_avatar_empty":false}`,
			OAuthUserInfo{ProviderID: "ya1", Email: "y@a.com", Name: "Yan Dex", AvatarURL: "https://avatars.yandex.net/get-yapic/av1/islands-200"},
		},
		{
			"notion",
			`{"object":"user","id":"bot1","type":"bot","bot":{"owner":{"type":"user","user":{"id":"no1","name":"Not Ion","avatar_url":"http://a/n.png","person":{"email":"n@o.com"}}}}}`,
			OAuthUserInfo{ProviderID: "no1", Email: "n@o.com", Name: "Not Ion", AvatarURL: "http://a/n.png"},
		},
		{
			"linear",
			`{"data":{"viewer":{"id":"li1","name":"Lin Ear","email":"l@i.com","avatarUrl":"http://a/li.png"}}}`,
			OAuthUserInfo{ProviderID: "li1", Email: "l@i.com", Name: "Lin Ear", AvatarURL: "http://a/li.png"},
		},
		{
			"slack",
			`{"ok":true,"sub":"sl1","email":"s@l.com","name":"Sla Ck","picture":"http://a/sl.png"}`,
			OAuthUserInfo{ProviderID: "sl1", Email: "s@l.com", Name: "Sla Ck", AvatarURL: "http://a/sl.png"},
		},
		{
			"zoom",
			`{"id":"zo1","email":"z@o.com","first_name":"Zo","last_name":"Om","pic_url":"http://a/z.png"}`,
			OAuthUserInfo{ProviderID: "zo1", Email: "z@o.com", Name: "Zo Om", AvatarURL: "http://a/z.png"},
		},
		{
			"box",
			`{"type":"user","id":"bx1","name":"Bo X","login":"b@x.com","avatar_url":"http://a/bx.png"}`,
			OAuthUserInfo{ProviderID: "bx1", Email: "b@x.com", Name: "Bo X", AvatarURL: "http://a/bx.png"},
		},
		{
			"dropbox",
			`{"account_id":"db1","email":"d@b.com","name":{"display_name":"Drop Box"},"profile_photo_url":"http://a/d.png"}`,
			OAuthUserInfo{ProviderID: "db1", Email: "d@b.com", Name: "Drop Box", AvatarURL: "http://a/d.png"},
		},
		{
			"figma",
			`{"id":"fi1","email":"f@i.com","handle":"Fig Ma","img_url":"http://a/f.png"}`,
			OAuthUserInfo{ProviderID: "fi1", Email: "f@i.com", Name: "Fig Ma", AvatarURL: "http://a/f.png"},
		},
		{
			"linkedin",
			`{"sub":"lk1","email":"l@k.com","name":"Lin Ked","picture":"http://a/lk.png"}`,
			OAuthUserInfo{ProviderID: "lk1", Email: "l@k.com", Name: "Lin Ked", AvatarURL: "http://a/lk.png"},
		},
		{
			"patreon",
			`{"data":{"id":"pa1","type":"user","attributes":{"email":"p@a.com","full_name":"Pat Reon","image_url":"http://a/pa.png"}}}`,
			OAuthUserInfo{ProviderID: "pa1", Email: "p@a.com", Name: "Pat Reon", AvatarURL: "http://a/pa.png"},
		},
		{
			"reddit",
			`{"id":"rd1","name":"redditor","icon_img":"http://a/r.png"}`,
			OAuthUserInfo{ProviderID: "rd1", Name: "redditor", AvatarURL: "http://a/r.png"},
		},
		{
			"wakatime",
			`{"data":{"id":"wa1","email":"w@a.com","display_name":"Waka Time","photo":"http://a/w.png"}}`,
			OAuthUserInfo{ProviderID: "wa1", Email: "w@a.com", Name: "Waka Time", AvatarURL: "http://a/w.png"},
		},
		{
			"apple",
			`{"iss":"https://appleid.apple.com","sub":"ap1","email":"a@p.com"}`,
			OAuthUserInfo{ProviderID: "ap1", Email: "a@p.com"},
		},
	}

	covered := make(map[string]bool)
	for _, tc := range cases {
		def, ok := standardProviderDefs[tc.provider]
		if !ok {
			t.Fatalf("%s: no catalog definition", tc.provider)
		}
		covered[tc.provider] = true

		var data map[string]any
		if err := json.Unmarshal([]byte(tc.fixture), &data); err != nil {
			t.Fatalf("%s: bad fixture: %v", tc.provider, err)
		}
		got := def.mapUser(data)
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
	}

	for name := range standardProviderDefs {
		if !covered[name] {
			t.Errorf("no userinfo mapping fixture for provider %q", name)
		}
	}
}

func TestOAuthStandardProviderExchangeForm(t *testing.T) {
	var gotAccept, gotContentType, gotUA string
	var gotForm url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		gotContentType = r.Header.Get("Content-Type")
		gotUA = r.Header.Get("User-Agent")
		r.ParseForm()
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"tok123","token_type":"bearer"}`))
	})
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok123" {
			http.Error(w, "bad token", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":42,"name":"Test User","email":"t@u.com","avatar_url":"http://a/u.png"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	def := standardDef{
		authURL:     srv.URL + "/authorize",
		tokenURL:    srv.URL + "/token",
		userInfoURL: srv.URL + "/user",
		headers:     map[string]string{"User-Agent": "gresbase-test"},
		mapUser: func(d map[string]any) OAuthUserInfo {
			return OAuthUserInfo{ProviderID: jStr(d, "id"), Email: jStr(d, "email"), Name: jStr(d, "name"), AvatarURL: jStr(d, "avatar_url")}
		},
	}
	p := def.build("testform", "cid", "sec")

	info, err := p.ExchangeCode(context.Background(), "code123", "https://example.com/cb")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if gotAccept != "application/json" {
		t.Errorf("token Accept = %q, want application/json", gotAccept)
	}
	if gotContentType != "application/x-www-form-urlencoded" {
		t.Errorf("token Content-Type = %q", gotContentType)
	}
	if gotUA != "gresbase-test" {
		t.Errorf("token User-Agent = %q", gotUA)
	}
	if gotForm.Get("client_id") != "cid" || gotForm.Get("client_secret") != "sec" ||
		gotForm.Get("code") != "code123" || gotForm.Get("grant_type") != "authorization_code" {
		t.Errorf("unexpected token form: %v", gotForm)
	}
	if info.Provider != "testform" || info.ProviderID != "42" || info.Email != "t@u.com" {
		t.Errorf("unexpected user info: %+v", info)
	}
}

func TestOAuthStandardProviderExchangeBasicJSON(t *testing.T) {
	var gotUser, gotPass string
	var gotBasic bool
	var gotBody map[string]string
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, gotBasic = r.BasicAuth()
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"tok456"}`))
	})
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Extra") != "extra-val" {
			http.Error(w, "missing header", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"sub":"u1","email":"e@x.com","name":"N"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	def := standardDef{
		authURL:         srv.URL + "/authorize",
		tokenURL:        srv.URL + "/token",
		userInfoURL:     srv.URL + "/user",
		tokenBasicAuth:  true,
		tokenJSONBody:   true,
		userInfoHeaders: map[string]string{"X-Extra": "extra-val"},
		mapUser:         oidcUserMapper,
	}
	p := def.build("testbasic", "cid", "sec")

	info, err := p.ExchangeCode(context.Background(), "code456", "https://example.com/cb")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if !gotBasic || gotUser != "cid" || gotPass != "sec" {
		t.Errorf("expected basic auth cid/sec, got %q/%q (ok=%v)", gotUser, gotPass, gotBasic)
	}
	if gotBody["grant_type"] != "authorization_code" || gotBody["code"] != "code456" {
		t.Errorf("unexpected JSON token body: %v", gotBody)
	}
	if gotBody["client_secret"] != "" {
		t.Error("client_secret leaked into JSON body despite basic auth")
	}
	if info.ProviderID != "u1" || info.Email != "e@x.com" {
		t.Errorf("unexpected user info: %+v", info)
	}
}

func TestOAuthStandardProviderExchangeIDToken(t *testing.T) {
	claims := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"apl1","email":"a@e.com"}`))
	idToken := "eyJhbGciOiJFUzI1NiJ9." + claims + ".sig"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"access_token": "tok789", "id_token": idToken})
	}))
	defer srv.Close()

	def := standardProviderDefs["apple"]
	def.tokenURL = srv.URL
	p := def.build("apple", "cid", "sec")

	info, err := p.ExchangeCode(context.Background(), "code789", "https://example.com/cb")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if info.ProviderID != "apl1" || info.Email != "a@e.com" {
		t.Errorf("unexpected user info from id_token: %+v", info)
	}
}
