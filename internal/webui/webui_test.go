package webui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"conduitvpn/internal/config"
	"conduitvpn/internal/manager"
	"conduitvpn/internal/node"
	"conduitvpn/internal/state"
	"conduitvpn/internal/vpngate"
)

func TestStaticAssetVersionStable(t *testing.T) {
	fsys := fstest.MapFS{
		"static/index.html":                 &fstest.MapFile{Data: []byte("index")},
		"static/login.html":                 &fstest.MapFile{Data: []byte("login")},
		"static/styles.css":                 &fstest.MapFile{Data: []byte("styles")},
		"static/i18n.js":                    &fstest.MapFile{Data: []byte("i18n script")},
		"static/theme.js":                   &fstest.MapFile{Data: []byte("theme script")},
		"static/app.js":                     &fstest.MapFile{Data: []byte("app")},
		"static/login.js":                   &fstest.MapFile{Data: []byte("login script")},
		"static/brand-mark.png":             &fstest.MapFile{Data: []byte("brand mark")},
		"static/favicon.ico":                &fstest.MapFile{Data: []byte("favicon")},
		"static/favicon-16x16.png":          &fstest.MapFile{Data: []byte("favicon 16")},
		"static/favicon-32x32.png":          &fstest.MapFile{Data: []byte("favicon 32")},
		"static/apple-touch-icon.png":       &fstest.MapFile{Data: []byte("apple icon")},
		"static/android-chrome-192x192.png": &fstest.MapFile{Data: []byte("android 192")},
		"static/android-chrome-512x512.png": &fstest.MapFile{Data: []byte("android 512")},
		"static/site.webmanifest":           &fstest.MapFile{Data: []byte("manifest")},
	}

	first := staticAssetVersion(fsys)
	second := staticAssetVersion(fsys)
	if first != second {
		t.Fatalf("same assets produced different versions: %q != %q", first, second)
	}
	if len(first) != 16 {
		t.Fatalf("expected 8-byte hex version, got %q", first)
	}
}

func TestStaticAssetVersionChangesWithContent(t *testing.T) {
	fsys := fstest.MapFS{
		"static/index.html":                 &fstest.MapFile{Data: []byte("index")},
		"static/login.html":                 &fstest.MapFile{Data: []byte("login")},
		"static/styles.css":                 &fstest.MapFile{Data: []byte("styles")},
		"static/i18n.js":                    &fstest.MapFile{Data: []byte("i18n script")},
		"static/theme.js":                   &fstest.MapFile{Data: []byte("theme script")},
		"static/app.js":                     &fstest.MapFile{Data: []byte("app")},
		"static/login.js":                   &fstest.MapFile{Data: []byte("login script")},
		"static/brand-mark.png":             &fstest.MapFile{Data: []byte("brand mark")},
		"static/favicon.ico":                &fstest.MapFile{Data: []byte("favicon")},
		"static/favicon-16x16.png":          &fstest.MapFile{Data: []byte("favicon 16")},
		"static/favicon-32x32.png":          &fstest.MapFile{Data: []byte("favicon 32")},
		"static/apple-touch-icon.png":       &fstest.MapFile{Data: []byte("apple icon")},
		"static/android-chrome-192x192.png": &fstest.MapFile{Data: []byte("android 192")},
		"static/android-chrome-512x512.png": &fstest.MapFile{Data: []byte("android 512")},
		"static/site.webmanifest":           &fstest.MapFile{Data: []byte("manifest")},
	}

	for _, name := range versionedAssets {
		before := staticAssetVersion(fsys)
		fsys[name] = &fstest.MapFile{Data: append(fsys[name].Data, 'x')}
		after := staticAssetVersion(fsys)
		if before == after {
			t.Fatalf("changing %s did not change asset version: %q", name, before)
		}
	}
}

func TestEmbeddedBrandAssets(t *testing.T) {
	for _, name := range []string{
		"static/brand-mark.png",
		"static/favicon.ico",
		"static/favicon-16x16.png",
		"static/favicon-32x32.png",
		"static/apple-touch-icon.png",
		"static/android-chrome-192x192.png",
		"static/android-chrome-512x512.png",
		"static/site.webmanifest",
	} {
		if _, err := staticFS.ReadFile(name); err != nil {
			t.Errorf("read embedded asset %s: %v", name, err)
		}
	}

	for _, name := range []string{"static/index.html", "static/login.html"} {
		page, err := staticFS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{
			`./brand-mark.png?v=__VER__`,
			`./favicon.ico?v=__VER__`,
			`./favicon-16x16.png?v=__VER__`,
			`./favicon-32x32.png?v=__VER__`,
			`./apple-touch-icon.png?v=__VER__`,
			`./site.webmanifest?v=__VER__`,
		} {
			if !strings.Contains(string(page), want) {
				t.Errorf("%s is missing %q", name, want)
			}
		}
	}
}

func TestEmbeddedI18nUI(t *testing.T) {
	for _, name := range []string{"static/index.html", "static/login.html"} {
		page, err := staticFS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`src="./i18n.js?v=__VER__"`, "data-language-control", "data-language-menu", "data-i18n"} {
			if !strings.Contains(string(page), want) {
				t.Errorf("%s is missing %q", name, want)
			}
		}
	}
	data, err := staticFS.ReadFile("static/i18n.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"zh-CN", "zh-TW", "navigator.languages", "Intl.DisplayNames", "conduit-language",
		"中国香港", "中国澳门", "中国台湾",
		"中國香港", "中國澳門", "中國台灣",
		"Hong Kong, China", "Macao, China", "Taiwan, China",
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("i18n.js is missing %q", want)
		}
	}
}

func TestEmbeddedScriptsDoNotUseHTMLInjection(t *testing.T) {
	for _, name := range []string{"static/i18n.js", "static/theme.js", "static/app.js", "static/login.js"} {
		data, err := staticFS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "innerHTML") || strings.Contains(string(data), "insertAdjacentHTML") {
			t.Fatalf("%s contains an HTML injection API", name)
		}
	}
}

func TestEmbeddedLoginThemeUI(t *testing.T) {
	login, err := staticFS.ReadFile("static/login.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(login)
	for _, want := range []string{
		`src="./theme.js?v=__VER__"`,
		`data-theme-control`,
		`data-theme-button`,
		`data-theme-menu`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("login.html is missing %q", want)
		}
	}
	if strings.Contains(page, "<script>\n") {
		t.Fatal("login.html contains an inline script blocked by CSP")
	}
}

func TestDemoRootRedirectsToSecretPath(t *testing.T) {
	s := testServer(t, true)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	s.handleAll(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("demo root status = %d, want %d", w.Code, http.StatusFound)
	}
	if want := "/" + s.SecretPath() + "/"; w.Header().Get("Location") != want {
		t.Fatalf("demo redirect = %q, want %q", w.Header().Get("Location"), want)
	}
}

func TestProductionRootRemainsNotFound(t *testing.T) {
	s := testServer(t, false)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	s.handleAll(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("production root status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestUnauthenticatedBrandAssetsLoadFromSecretPath(t *testing.T) {
	s := testServer(t, false)
	for _, name := range []string{
		"brand-mark.png",
		"favicon.ico",
		"favicon-16x16.png",
		"favicon-32x32.png",
		"apple-touch-icon.png",
		"android-chrome-192x192.png",
		"android-chrome-512x512.png",
		"site.webmanifest",
	} {
		r := httptest.NewRequest(http.MethodGet, "/"+s.SecretPath()+"/"+name, nil)
		w := httptest.NewRecorder()
		s.handleAll(w, r)
		if w.Code != http.StatusOK {
			t.Errorf("GET %s status = %d, want %d", name, w.Code, http.StatusOK)
		}
		if got := w.Header().Get("Cache-Control"); got != "no-cache" {
			t.Errorf("GET %s Cache-Control = %q, want no-cache", name, got)
		}
		if name == "site.webmanifest" && !strings.HasPrefix(w.Header().Get("Content-Type"), "application/manifest+json") {
			t.Errorf("GET %s Content-Type = %q, want application/manifest+json", name, w.Header().Get("Content-Type"))
		}
	}
}

func TestDemoLoginShowsCredentialsOnlyInDemo(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	demo := httptest.NewRecorder()
	serveLogin(demo, request, true)
	if !strings.Contains(demo.Body.String(), "Demo account: admin") {
		t.Fatal("demo login is missing credential hint")
	}

	production := httptest.NewRecorder()
	serveLogin(production, request, false)
	if strings.Contains(production.Body.String(), "Demo account: admin") {
		t.Fatal("production login leaked demo credential hint")
	}
}

func TestLoginUsesPrivateHashedCredentialsAndStrictCookie(t *testing.T) {
	dir := t.TempDir()
	store := state.NewStore(dir)
	password := "0123456789abcdef"
	if err := store.EnsureAuthConfigured("admin", password, false); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{DataDir: dir}
	s := New(cfg, store, manager.New(cfg))
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"username":"admin","password":"0123456789abcdef"}`))
	r.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	s.apiLogin(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].MaxAge != int(sessionTTL.Seconds()) {
		t.Fatalf("unexpected session cookie: %+v", cookies)
	}
}

func TestAPIErrorsHaveStableCodes(t *testing.T) {
	s := testServer(t, false)
	w := httptest.NewRecorder()
	s.apiLogin(w, httptest.NewRequest(http.MethodGet, "/api/login", nil))
	if w.Code != http.StatusMethodNotAllowed || !strings.Contains(w.Body.String(), `"code":"method_not_allowed"`) {
		t.Fatalf("login method error = %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	s.apiRoute(w, httptest.NewRequest(http.MethodPut, "/api/route", bytes.NewBufferString(`{"mode":"invalid"}`)))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"code":"route_invalid"`) {
		t.Fatalf("route error = %d %s", w.Code, w.Body.String())
	}
}

func TestSecurityHeadersAreSet(t *testing.T) {
	s := testServer(t, false)
	r := httptest.NewRequest(http.MethodGet, "/"+s.SecretPath()+"/", nil)
	w := httptest.NewRecorder()
	s.handleAll(w, r)
	if got := w.Header().Get("Content-Security-Policy"); got == "" {
		t.Fatal("CSP header is missing")
	}
	if got := w.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q", got)
	}
}

func TestBlacklistActionAPIs(t *testing.T) {
	s := testServer(t, false)

	get := httptest.NewRecorder()
	s.apiTestBlacklist(get, httptest.NewRequest(http.MethodGet, "/api/actions/test-blacklist", nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"running"`) {
		t.Fatalf("GET test-blacklist = %d %s", get.Code, get.Body.String())
	}

	post := httptest.NewRecorder()
	s.apiTestBlacklist(post, httptest.NewRequest(http.MethodPost, "/api/actions/test-blacklist", nil))
	if post.Code != http.StatusAccepted {
		t.Fatalf("POST test-blacklist = %d %s", post.Code, post.Body.String())
	}

	badMethod := httptest.NewRecorder()
	s.apiTestBlacklist(badMethod, httptest.NewRequest(http.MethodPut, "/api/actions/test-blacklist", nil))
	if badMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT test-blacklist = %d", badMethod.Code)
	}

	restoreServer := testServer(t, false)
	restore := httptest.NewRecorder()
	restoreServer.apiRestoreAvailableBlacklist(restore, httptest.NewRequest(http.MethodPost, "/api/actions/restore-available-blacklist", nil))
	if restore.Code != http.StatusOK || !strings.Contains(restore.Body.String(), `"restored":0`) {
		t.Fatalf("POST restore blacklist = %d %s", restore.Code, restore.Body.String())
	}
}

func TestEmbeddedBlacklistManagerUI(t *testing.T) {
	page, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, selector := range []string{"btn-blacklist", "blacklist-dialog", "btn-blacklist-test", "btn-blacklist-restore"} {
		if !strings.Contains(string(page), selector) {
			t.Fatalf("index.html missing %q", selector)
		}
	}
}

func TestVPNGateSourcesAPIGetPutAndClear(t *testing.T) {
	dir := t.TempDir()
	store := state.NewStore(dir)
	if _, _, err := store.EnsureAuth(); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{DataDir: dir, Demo: true, APIURL: "https://official.example/api/iphone/"}
	mgr := manager.NewDemo(cfg)
	s := New(cfg, store, mgr)

	get := httptest.NewRecorder()
	s.apiVPNGateSources(get, httptest.NewRequest(http.MethodGet, "/api/vpngate-sources", nil))
	if get.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", get.Code, get.Body.String())
	}
	var initial manager.VPNGateSourceStatus
	if err := json.Unmarshal(get.Body.Bytes(), &initial); err != nil {
		t.Fatal(err)
	}
	if initial.OfficialURL != cfg.APIURL || initial.Mirrors == nil || initial.Attempts == nil {
		t.Fatalf("initial source status = %+v", initial)
	}

	putBody := `{"text":"http://EXAMPLE.com/cn/ (Japan)\nhttps://example.com:443/api/iphone/\nexample.net:8080"}`
	put := httptest.NewRecorder()
	s.apiVPNGateSources(put, httptest.NewRequest(http.MethodPut, "/api/vpngate-sources", bytes.NewBufferString(putBody)))
	if put.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", put.Code, put.Body.String())
	}
	var saved map[string]any
	if err := json.Unmarshal(put.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved["ok"] != true {
		t.Fatalf("PUT response = %#v", saved)
	}
	mirrors, ok := saved["mirrors"].([]any)
	if !ok || len(mirrors) != 2 || mirrors[0] != "http://example.com" || mirrors[1] != "https://example.com" {
		t.Fatalf("normalized mirrors = %#v", saved["mirrors"])
	}
	if issues, ok := saved["issues"].([]any); !ok || len(issues) == 0 {
		t.Fatalf("expected bare-address issue, response = %#v", saved)
	}

	clear := httptest.NewRecorder()
	s.apiVPNGateSources(clear, httptest.NewRequest(http.MethodPut, "/api/vpngate-sources", bytes.NewBufferString(`{"text":""}`)))
	if clear.Code != http.StatusOK || !strings.Contains(clear.Body.String(), `"mirrors":[]`) {
		t.Fatalf("clear response = %d %s", clear.Code, clear.Body.String())
	}
	loaded, err := store.LoadVPNGateSources()
	if err != nil || loaded.Mirrors == nil || len(loaded.Mirrors) != 0 {
		t.Fatalf("persisted clear = %#v, err=%v", loaded, err)
	}
}

func TestVPNGateSourcesAPIRedactsMirrorCredentialsAndPreservesThem(t *testing.T) {
	dir := t.TempDir()
	store := state.NewStore(dir)
	if _, _, err := store.EnsureAuth(); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{DataDir: dir, Demo: true}
	mgr := manager.NewDemo(cfg)
	s := New(cfg, store, mgr)

	const password = "mirror-password"
	const authenticated = "https://mirror-password@secure.example/cn/"
	put := httptest.NewRecorder()
	s.apiVPNGateSources(put, httptest.NewRequest(http.MethodPut, "/api/vpngate-sources", strings.NewReader(fmt.Sprintf(`{"text":%q}`, authenticated))))
	if put.Code != http.StatusOK {
		t.Fatalf("authenticated PUT = %d %s", put.Code, put.Body.String())
	}
	if strings.Contains(put.Body.String(), password) {
		t.Fatalf("PUT response leaked mirror password: %s", put.Body.String())
	}
	var response struct {
		Mirrors []string `json:"mirrors"`
	}
	if err := json.Unmarshal(put.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Mirrors) != 1 || response.Mirrors[0] != "https://secure.example" {
		t.Fatalf("public mirrors = %#v", response.Mirrors)
	}
	persisted, err := store.LoadVPNGateSources()
	if err != nil || len(persisted.Mirrors) != 1 || persisted.Mirrors[0] != "https://mirror-password@secure.example" {
		t.Fatalf("stored mirrors = %#v, err=%v", persisted.Mirrors, err)
	}

	get := httptest.NewRecorder()
	s.apiVPNGateSources(get, httptest.NewRequest(http.MethodGet, "/api/vpngate-sources", nil))
	if get.Code != http.StatusOK || strings.Contains(get.Body.String(), password) {
		t.Fatalf("GET response leaked mirror password: %d %s", get.Code, get.Body.String())
	}

	// The browser receives the redacted value, so saving it unchanged must not
	// remove the persisted credential.
	resave := httptest.NewRecorder()
	s.apiVPNGateSources(resave, httptest.NewRequest(http.MethodPut, "/api/vpngate-sources", strings.NewReader(`{"text":"https://secure.example"}`)))
	if resave.Code != http.StatusOK {
		t.Fatalf("redacted resave = %d %s", resave.Code, resave.Body.String())
	}
	persisted, err = store.LoadVPNGateSources()
	if err != nil || len(persisted.Mirrors) != 1 || persisted.Mirrors[0] != "https://mirror-password@secure.example" {
		t.Fatalf("redacted resave lost credential: %#v, err=%v", persisted.Mirrors, err)
	}
}

func TestVPNGateSourcesAPIErrorsPreserveOldConfig(t *testing.T) {
	dir := t.TempDir()
	store := state.NewStore(dir)
	if _, _, err := store.EnsureAuth(); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{DataDir: dir, Demo: true}
	mgr := manager.NewDemo(cfg)
	if _, err := mgr.SetVPNGateMirrors(nil, "http://old.example"); err != nil {
		t.Fatal(err)
	}
	s := New(cfg, store, mgr)

	bad := httptest.NewRecorder()
	s.apiVPNGateSources(bad, httptest.NewRequest(http.MethodPut, "/api/vpngate-sources", bytes.NewBufferString(`{"text":"not-a-url"}`)))
	if bad.Code != http.StatusBadRequest || !strings.Contains(bad.Body.String(), `"code":"sources_invalid"`) {
		t.Fatalf("invalid PUT = %d %s", bad.Code, bad.Body.String())
	}
	missing := httptest.NewRecorder()
	s.apiVPNGateSources(missing, httptest.NewRequest(http.MethodPut, "/api/vpngate-sources", bytes.NewBufferString(`{}`)))
	if missing.Code != http.StatusBadRequest || !strings.Contains(missing.Body.String(), `"code":"sources_invalid"`) {
		t.Fatalf("missing text PUT = %d %s", missing.Code, missing.Body.String())
	}
	status := mgr.VPNGateSourceStatus()
	if len(status.Mirrors) != 1 || status.Mirrors[0] != "http://old.example" {
		t.Fatalf("old config was changed: %+v", status)
	}

	var b strings.Builder
	for i := 0; i < vpngate.MaxMirrorCount+1; i++ {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "http://mirror-%d.example", i)
	}
	tooMany := httptest.NewRecorder()
	s.apiVPNGateSources(tooMany, httptest.NewRequest(http.MethodPut, "/api/vpngate-sources", strings.NewReader(fmt.Sprintf(`{"text":%q}`, b.String()))))
	if tooMany.Code != http.StatusBadRequest || !strings.Contains(tooMany.Body.String(), `"code":"sources_too_large"`) {
		t.Fatalf("too many mirrors = %d %s", tooMany.Code, tooMany.Body.String())
	}

	method := httptest.NewRecorder()
	s.apiVPNGateSources(method, httptest.NewRequest(http.MethodPost, "/api/vpngate-sources", nil))
	if method.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, body = %s", method.Code, method.Body.String())
	}

	largeBody := strings.NewReader(`{"text":"` + strings.Repeat("x", 150000) + `"}`)
	tooLarge := httptest.NewRecorder()
	s.apiVPNGateSources(tooLarge, httptest.NewRequest(http.MethodPut, "/api/vpngate-sources", largeBody))
	if tooLarge.Code != http.StatusBadRequest || !strings.Contains(tooLarge.Body.String(), `"code":"sources_too_large"`) {
		t.Fatalf("oversized request = %d %s", tooLarge.Code, tooLarge.Body.String())
	}
}

func TestEmbeddedVPNGateSourcesUI(t *testing.T) {
	page, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		t.Fatal(err)
	}
	text := string(page)
	for _, want := range []string{"btn-sources", "sources-dialog", "sources-text", "data-i18n=\"sources.title\"", "data-i18n=\"sources.authHint\"", "data-i18n-placeholder=\"sources.placeholder\""} {
		if !strings.Contains(text, want) {
			t.Errorf("index.html missing %q", want)
		}
	}
	js, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"normalizePastedURLs", "api/vpngate-sources", "clipboardData", "const userinfo", "url.username"} {
		if !strings.Contains(string(js), want) {
			t.Errorf("app.js missing %q", want)
		}
	}
	i18n, err := staticFS.ReadFile("static/i18n.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"sources.authHint", "https://<password>@host"} {
		if !strings.Contains(string(i18n), want) {
			t.Errorf("i18n.js missing %q", want)
		}
	}
}

func TestAPINodesStripsOpenVPNConfig(t *testing.T) {
	dir := t.TempDir()
	store := state.NewStore(dir)
	nodes := []*node.Node{
		{HostName: "safe-node", IP: "8.8.8.8", ConfigText: "<key>private material</key>"},
		nil,
	}
	if err := store.SaveNodes(nodes); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{DataDir: dir}
	s := New(cfg, store, manager.New(cfg))
	w := httptest.NewRecorder()
	s.apiNodes(w, httptest.NewRequest(http.MethodGet, "/api/nodes", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET nodes status = %d, body = %s", w.Code, w.Body.String())
	}
	var got []*node.Node
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ConfigText != "" {
		t.Fatalf("nodes response leaked config: %#v", got)
	}
}

func testServer(t *testing.T, demo bool) *Server {
	t.Helper()
	dir := t.TempDir()
	store := state.NewStore(dir)
	if _, _, err := store.EnsureAuth(); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{DataDir: dir, Demo: demo}
	return New(cfg, store, manager.New(cfg))
}
