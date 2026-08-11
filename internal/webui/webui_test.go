package webui

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"conduitvpn/internal/config"
	"conduitvpn/internal/manager"
	"conduitvpn/internal/state"
)

func TestStaticAssetVersionStable(t *testing.T) {
	fsys := fstest.MapFS{
		"static/index.html":                 &fstest.MapFile{Data: []byte("index")},
		"static/login.html":                 &fstest.MapFile{Data: []byte("login")},
		"static/styles.css":                 &fstest.MapFile{Data: []byte("styles")},
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

func TestEmbeddedRegionLabels(t *testing.T) {
	index, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), "固定国家地区") || !strings.Contains(string(index), `<th data-sort="country">国家地区</th>`) {
		t.Fatal("index is missing country-region labels")
	}

	app, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`HK: "中国香港"`,
		`MO: "中国澳门"`,
		`TW: "中国台湾"`,
		`country: "固定国家地区"`,
	} {
		if !strings.Contains(string(app), want) {
			t.Errorf("app.js is missing %q", want)
		}
	}
}

func TestEmbeddedScriptsDoNotUseHTMLInjection(t *testing.T) {
	for _, name := range []string{"static/theme.js", "static/app.js", "static/login.js"} {
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
	if !strings.Contains(demo.Body.String(), "演示账号：admin") {
		t.Fatal("demo login is missing credential hint")
	}

	production := httptest.NewRecorder()
	serveLogin(production, request, false)
	if strings.Contains(production.Body.String(), "演示账号：admin") {
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
