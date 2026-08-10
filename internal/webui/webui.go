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
	"crypto/tls"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
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
	"static/login.js",
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

const (
	sessionTTL       = 12 * time.Hour
	maxSessions      = 256
	maxSSEClients    = 32
	maxLoginFailures = 5
	loginWindow      = 15 * time.Minute
)

type loginAttempt struct {
	failed int
	until  time.Time
}

type Server struct {
	cfg    config.Config
	store  *state.Store
	mgr    *manager.Manager
	secret string
	tls    bool

	ln  net.Listener
	srv *http.Server

	mu       sync.Mutex
	sessions map[string]time.Time
	attempts map[string]loginAttempt
	sseLimit chan struct{}

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
		attempts: map[string]loginAttempt{},
		sseLimit: make(chan struct{}, maxSSEClients),
		api:      http.NewServeMux(),
		static:   http.FileServer(http.FS(sub)),
	}
	s.registerAPI()

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
		s.securityHeaders(w)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/", s.handleAll)

	s.srv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	if s.cfg.UITLSCert != "" {
		cert, err := tls.LoadX509KeyPair(s.cfg.UITLSCert, s.cfg.UITLSKey)
		if err != nil {
			_ = ln.Close()
			return fmt.Errorf("load UI TLS certificate: %w", err)
		}
		s.tls = true
		s.srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}
		go func() { _ = s.srv.ServeTLS(ln, "", "") }()
		return nil
	}
	go func() { _ = s.srv.Serve(ln) }()
	return nil
}

func (s *Server) Close() error {
	if s.srv != nil {
		return s.srv.Close()
	}
	return nil
}

// handleAll routes every request under the secret path. The demo root is an
// intentional exception so a shared preview URL can be opened directly.
func (s *Server) handleAll(w http.ResponseWriter, r *http.Request) {
	s.securityHeaders(w)
	if s.cfg.Demo && r.URL.Path == "/" {
		http.Redirect(w, r, "/"+s.secret+"/", http.StatusFound)
		return
	}
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
			serveLogin(w, r, s.cfg.Demo)
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
	c, err := r.Cookie(s.sessionCookieName())
	if err != nil {
		return false
	}
	s.mu.Lock()
	exp, ok := s.sessions[c.Value]
	if ok && !exp.After(time.Now()) {
		delete(s.sessions, c.Value)
		ok = false
	}
	s.mu.Unlock()
	return ok && exp.After(time.Now())
}

func (s *Server) apiLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST only"})
		return
	}
	remote := clientIP(r)
	if s.loginBlocked(remote) {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"ok": false, "error": "登录失败，请稍后重试"})
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
		return
	}
	auth, err := s.store.LoadAuth()
	if err != nil || auth.Username == "" || auth.PasswordHash == "" || auth.PasswordSalt == "" {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "auth not initialized"})
		return
	}
	userOK := subtle.ConstantTimeCompare([]byte(body.Username), []byte(auth.Username)) == 1
	passOK := auth.VerifyPassword(body.Password)
	if !userOK || !passOK {
		s.recordLoginFailure(remote)
		writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "登录失败"})
		return
	}

	token, err := randToken()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "登录失败"})
		return
	}
	s.mu.Lock()
	s.pruneSessionsLocked(time.Now())
	if len(s.sessions) >= maxSessions {
		s.mu.Unlock()
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "会话容量已满"})
		return
	}
	s.sessions[token] = time.Now().Add(sessionTTL)
	delete(s.attempts, remote)
	s.mu.Unlock()
	cookieName := "conduitvpn_session"
	if s.tls {
		cookieName = "__Secure-conduitvpn-session"
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/" + s.secret + "/",
		HttpOnly: true,
		Secure:   s.tls,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) apiLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST only"})
		return
	}
	if c, err := r.Cookie(s.sessionCookieName()); err == nil {
		s.mu.Lock()
		delete(s.sessions, c.Value)
		s.mu.Unlock()
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) sessionCookieName() string {
	if s.tls {
		return "__Secure-conduitvpn-session"
	}
	return "conduitvpn_session"
}

func randToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *Server) securityHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	if s.tls {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("multiple JSON values")
	}
	return nil
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) loginBlocked(remote string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.attempts[remote]
	return ok && a.failed >= maxLoginFailures && a.until.After(time.Now())
}

func (s *Server) recordLoginFailure(remote string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.attempts[remote]
	if a.until.Before(time.Now()) {
		a = loginAttempt{}
	}
	a.failed++
	if a.failed >= maxLoginFailures {
		a.until = time.Now().Add(loginWindow)
	}
	if len(s.attempts) < 4096 || s.attempts[remote].failed > 0 {
		s.attempts[remote] = a
	}
}

func (s *Server) pruneSessionsLocked(now time.Time) {
	for token, exp := range s.sessions {
		if !exp.After(now) {
			delete(s.sessions, token)
		}
	}
}

// --- pages ---

func serveLogin(w http.ResponseWriter, r *http.Request, demo bool) {
	data, err := staticFS.ReadFile("static/login.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data = bytes.ReplaceAll(data, []byte("__VER__"), []byte(assetVersion))
	demoHint := []byte{}
	if demo {
		demoHint = []byte(`<p class="login-demo-hint">演示账号：admin <span>密码：demo</span></p>`)
	}
	data = bytes.ReplaceAll(data, []byte("__DEMO_HINT__"), demoHint)
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
		if err := decodeJSON(w, r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
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
	select {
	case s.sseLimit <- struct{}{}:
		defer func() { <-s.sseLimit }()
	default:
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many log streams"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	sub, unsubscribe := logx.Subscribe()
	defer unsubscribe()
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
