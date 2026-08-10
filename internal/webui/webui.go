// Package webui serves the embedded management panel behind the same
// auth model as the original Python version: a random secret path, a
// username/password login persisted in ui_auth.json, and in-memory
// sessions issued via an HttpOnly cookie.
package webui

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"conduitvpn/internal/config"
	"conduitvpn/internal/logx"
	"conduitvpn/internal/manager"
	"conduitvpn/internal/node"
	"conduitvpn/internal/state"
)

//go:embed static
var staticFS embed.FS

var versionedAssets = []string{
	"static/index.html",
	"static/login.html",
	"static/styles.css",
	"static/app.js",
}

// assetVersion busts proxy caches. It is derived from the embedded assets so
// a new binary cannot accidentally serve a stale JS/CSS pair through a CDN.
var assetVersion = staticAssetVersion(staticFS)

func staticAssetVersion(fsys fs.FS) string {
	h := sha256.New()
	for _, name := range versionedAssets {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			panic(fmt.Sprintf("read embedded asset %q: %v", name, err))
		}
		_, _ = h.Write([]byte(name))
		_, _ = h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)[:8])
}

const sessionTTL = 30 * 24 * time.Hour

type Server struct {
	cfg    config.Config
	store  *state.Store
	mgr    *manager.Manager
	secret string

	ln  net.Listener
	srv *http.Server

	mu       sync.Mutex
	sessions map[string]time.Time // session token → expiry (in-memory, like the original)

	api    *http.ServeMux
	static http.Handler
}

func New(cfg config.Config, store *state.Store, mgr *manager.Manager) *Server {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	s := &Server{
		cfg:      cfg,
		store:    store,
		mgr:      mgr,
		secret:   store.SecretPath(),
		sessions: map[string]time.Time{},
		api:      http.NewServeMux(),
		static:   http.FileServer(http.FS(sub)),
	}
	s.registerAPI()

	// env credentials override the persisted ones (like editing
	// ui_auth.json, but from the environment). Only explicitly-set env
	// vars override — config defaults must not clobber generated creds.
	if auth, err := store.LoadAuth(); err == nil {
		changed := false
		if v := os.Getenv("UI_USER"); v != "" && v != auth.Username {
			auth.Username = v
			changed = true
		}
		if v := os.Getenv("UI_PASSWORD"); v != "" && v != auth.Password {
			auth.Password = v
			changed = true
		}
		if changed {
			_ = store.SaveAuth(auth)
		}
	}
	return s
}

func (s *Server) SecretPath() string { return s.secret }
func (s *Server) Addr() net.Addr     { return s.ln.Addr() }

func (s *Server) registerAPI() {
	s.api.HandleFunc("/api/state", s.apiState)
	s.api.HandleFunc("/api/route", s.apiRoute)
	s.api.HandleFunc("/api/nodes", s.apiNodes)
	s.api.HandleFunc("/api/blacklist", s.apiBlacklist)
	s.api.HandleFunc("/api/logs", s.apiLogs)
	s.api.HandleFunc("/api/logs/stream", s.apiLogStream)
	s.api.HandleFunc("/api/actions/update-nodes", s.apiUpdateNodes)
}

func (s *Server) Start() error {
	ln, err := net.Listen("tcp", net.JoinHostPort(s.cfg.UIHost, fmt.Sprint(s.cfg.UIPort)))
	if err != nil {
		return err
	}
	s.ln = ln

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/", s.handleAll)

	s.srv = &http.Server{Handler: mux}
	go func() { _ = s.srv.Serve(ln) }()
	return nil
}

func (s *Server) Close() error {
	if s.srv != nil {
		return s.srv.Close()
	}
	return nil
}

// handleAll routes every request under the secret path (root 404s, like
// the original — no auto-jump, no secret leak).
func (s *Server) handleAll(w http.ResponseWriter, r *http.Request) {
	eff := s.validatePath(w, r)
	if eff == "" {
		return
	}
	r2 := new(http.Request)
	*r2 = *r
	r2.URL = new(url.URL)
	*r2.URL = *r.URL
	r2.URL.Path = eff

	switch {
	case eff == "/api/login":
		s.apiLogin(w, r)
		return
	case eff == "/api/logout":
		s.apiLogout(w, r)
		return
	}

	if !s.isAuthorized(r) {
		if eff == "/" || eff == "/index.html" {
			serveLogin(w, r)
			return
		}
		// static assets (styles.css / app.js) carry no secrets and must
		// load for the login page itself; API + panel data stay gated.
		if isStaticAsset(eff) {
			w.Header().Set("Cache-Control", "no-cache")
			s.static.ServeHTTP(w, r2)
			return
		}
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
		return
	}

	switch {
	case strings.HasPrefix(eff, "/api/"):
		s.api.ServeHTTP(w, r2)
	case eff == "/" || eff == "/index.html":
		serveIndex(w, r)
	default:
		w.Header().Set("Cache-Control", "no-cache")
		s.static.ServeHTTP(w, r2)
	}
}

// validatePath mirrors the original: /{secret} redirects to /{secret}/,
// /{secret}/... strips the prefix, everything else 404s.
func (s *Server) validatePath(w http.ResponseWriter, r *http.Request) string {
	path := r.URL.Path
	if path == "/"+s.secret {
		http.Redirect(w, r, "/"+s.secret+"/", http.StatusFound)
		return ""
	}
	prefix := "/" + s.secret + "/"
	if strings.HasPrefix(path, prefix) {
		return "/" + strings.TrimPrefix(path, prefix)
	}
	http.NotFound(w, r)
	return ""
}

// --- auth ---

func (s *Server) isAuthorized(r *http.Request) bool {
	c, err := r.Cookie("session")
	if err != nil {
		return false
	}
	s.mu.Lock()
	exp, ok := s.sessions[c.Value]
	s.mu.Unlock()
	return ok && exp.After(time.Now())
}

func (s *Server) apiLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST only"})
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
		return
	}
	auth, err := s.store.LoadAuth()
	if err != nil || auth.Username == "" || auth.Password == "" {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "auth not initialized"})
		return
	}
	userOK := subtle.ConstantTimeCompare([]byte(body.Username), []byte(auth.Username)) == 1
	passOK := subtle.ConstantTimeCompare([]byte(body.Password), []byte(auth.Password)) == 1
	if !userOK || !passOK {
		writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "用户名或密码不正确，请重新输入"})
		return
	}

	token := randToken()
	s.mu.Lock()
	s.sessions[token] = time.Now().Add(sessionTTL)
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/" + s.secret + "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) apiLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("session"); err == nil {
		s.mu.Lock()
		delete(s.sessions, c.Value)
		s.mu.Unlock()
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func randToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// --- pages ---

func serveLogin(w http.ResponseWriter, r *http.Request) {
	data, err := staticFS.ReadFile("static/login.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data = bytes.ReplaceAll(data, []byte("__VER__"), []byte(assetVersion))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	data, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data = bytes.ReplaceAll(data, []byte("__VER__"), []byte(assetVersion))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}

func isStaticAsset(path string) bool {
	last := path[strings.LastIndex(path, "/")+1:]
	return strings.Contains(last, ".")
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// --- api handlers ---

func (s *Server) apiState(w http.ResponseWriter, r *http.Request) {
	snap := s.mgr.Snapshot()
	payload := map[string]any{
		"state":           snap.State,
		"detail":          snap.Detail,
		"current_node":    sanitizeNode(snap.CurrentNode),
		"blacklist_count": snap.BlacklistCount,
		"uptime_sec":      snap.UptimeSec,
		"proxy_port":      s.cfg.LocalProxyPort,
		"ui_port":         s.cfg.UIPort,
		"route_mode":      snap.RouteMode,
		"route_country":   snap.RouteCountry,
		"route_node":      snap.RouteNode,
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) apiRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		mode, country, node := s.mgr.RouteConfig()
		writeJSON(w, http.StatusOK, map[string]string{"mode": mode, "country": country, "node": node})
	case http.MethodPut, http.MethodPost:
		var body struct {
			Mode    string `json:"mode"`
			Country string `json:"country"`
			Node    string `json:"node"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json: " + err.Error()})
			return
		}
		if err := s.mgr.SetRouteConfig(body.Mode, body.Country, body.Node); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET or PUT"})
	}
}

func (s *Server) apiNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.store.LoadNodes()
	if err != nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, nodes)
}

func (s *Server) apiBlacklist(w http.ResponseWriter, r *http.Request) {
	bl, err := s.store.LoadBlacklist()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}
	writeJSON(w, http.StatusOK, bl)
}

func (s *Server) apiLogs(w http.ResponseWriter, r *http.Request) {
	n := 200
	if v := r.URL.Query().Get("n"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			n = p
		}
	}
	writeJSON(w, http.StatusOK, logx.Recent(n))
}

func (s *Server) apiLogStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	sub := logx.Subscribe()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	send := func(typ string, v any) {
		data, _ := json.Marshal(v)
		_, _ = fmt.Fprintf(w, "data: {\"type\":%q,\"payload\":%s}\n\n", typ, data)
		flusher.Flush()
	}

	// The SSE state snapshot must not leak the raw OpenVPN config.
	snap := s.mgr.Snapshot()
	snap.CurrentNode = sanitizeNode(snap.CurrentNode)
	send("state", snap)
	for {
		select {
		case <-r.Context().Done():
			return
		case entry := <-sub:
			send("log", entry)
		case <-ticker.C:
			s := s.mgr.Snapshot()
			s.CurrentNode = sanitizeNode(s.CurrentNode)
			send("state", s)
		}
	}
}

func (s *Server) apiUpdateNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST only"})
		return
	}
	s.mgr.TriggerFetch()
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
}

// sanitizeNode strips the embedded OpenVPN config (certs + private keys)
// before a node leaves the process.
func sanitizeNode(n *node.Node) *node.Node {
	if n == nil {
		return nil
	}
	c := *n
	c.ConfigText = ""
	return &c
}
