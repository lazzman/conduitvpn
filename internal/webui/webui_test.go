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
		"static/index.html": &fstest.MapFile{Data: []byte("index")},
		"static/login.html": &fstest.MapFile{Data: []byte("login")},
		"static/styles.css": &fstest.MapFile{Data: []byte("styles")},
		"static/app.js":     &fstest.MapFile{Data: []byte("app")},
		"static/login.js":   &fstest.MapFile{Data: []byte("login script")},
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
		"static/index.html": &fstest.MapFile{Data: []byte("index")},
		"static/login.html": &fstest.MapFile{Data: []byte("login")},
		"static/styles.css": &fstest.MapFile{Data: []byte("styles")},
		"static/app.js":     &fstest.MapFile{Data: []byte("app")},
		"static/login.js":   &fstest.MapFile{Data: []byte("login script")},
	}

	before := staticAssetVersion(fsys)
	fsys["static/app.js"] = &fstest.MapFile{Data: []byte("app changed")}
	after := staticAssetVersion(fsys)
	if before == after {
		t.Fatalf("asset content change did not change version: %q", before)
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
	for _, name := range []string{"static/app.js", "static/login.js"} {
		data, err := staticFS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "innerHTML") || strings.Contains(string(data), "insertAdjacentHTML") {
			t.Fatalf("%s contains an HTML injection API", name)
		}
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
