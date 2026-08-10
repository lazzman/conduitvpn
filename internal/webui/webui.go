// Package webui serves the embedded management panel: secret-path auth,
// REST state/nodes/logs APIs, and an SSE log stream.
package webui

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"conduitvpn/internal/config"
	"conduitvpn/internal/logx"
	"conduitvpn/internal/manager"
	"conduitvpn/internal/node"
	"conduitvpn/internal/state"
)

//go:embed static
var staticFS embed.FS

// assetVersion busts proxy caches: bump it whenever the static assets
// change (Cloudflare overrides our no-cache with a 4h browser TTL, so
// the query string is the only reliable invalidation).
const assetVersion = "11"

type Server struct {
	cfg    config.Config
	store  *state.Store
	mgr    *manager.Manager
	secret string

	ln  net.Listener
	srv *http.Server
}

func New(cfg config.Config, store *state.Store, mgr *manager.Manager) *Server {
	return &Server{cfg: cfg, store: store, mgr: mgr, secret: store.SecretPath()}
}

func (s *Server) SecretPath() string { return s.secret }
func (s *Server) Addr() net.Addr     { return s.ln.Addr() }

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
	mux.HandleFunc("/", s.handleRoot)
	mux.Handle("/"+s.secret+"/", s.panelHandler())

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

// handleRoot redirects "/" to the secret path (fixing the 404 mystery
// the Python version shipped) and 404s everything else.
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" || r.URL.Path == "/"+s.secret {
		http.Redirect(w, r, "/"+s.secret+"/", http.StatusFound)
		return
	}
	http.NotFound(w, r)
}

// panelHandler registers every panel route with the full secret prefix on
// a single mux. Nested muxes under http.StripPrefix hit a Go 1.22 ServeMux
// redirect quirk (301 to the stripped path), so we avoid StripPrefix
// entirely and strip the prefix manually for static files.
func (s *Server) panelHandler() http.Handler {
	prefix := "/" + s.secret
	mux := http.NewServeMux()
	mux.HandleFunc(prefix+"/api/state", s.apiState)
	mux.HandleFunc(prefix+"/api/route", s.apiRoute)
	mux.HandleFunc(prefix+"/api/nodes", s.apiNodes)
	mux.HandleFunc(prefix+"/api/blacklist", s.apiBlacklist)
	mux.HandleFunc(prefix+"/api/logs", s.apiLogs)
	mux.HandleFunc(prefix+"/api/logs/stream", s.apiLogStream)
	mux.HandleFunc(prefix+"/api/actions/update-nodes", s.apiUpdateNodes)

	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	static := http.FileServer(http.FS(sub))
	mux.Handle(prefix+"/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, prefix)
		if p == "" {
			p = "/"
		}
		if p == "/" {
			serveIndex(w, r)
			return
		}
		r2 := new(http.Request)
		*r2 = *r
		r2.URL = new(url.URL)
		*r2.URL = *r.URL
		r2.URL.Path = p
		// Never let proxies (Cloudflare) cache static assets: the panel is
		// embedded in the binary and updates must propagate immediately.
		w.Header().Set("Cache-Control", "no-cache")
		static.ServeHTTP(w, r2)
	}))
	return withSecurityHeaders(mux)
}

// serveIndex renders the panel HTML with the asset version injected so a
// single const controls all cache busting.
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

func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) apiState(w http.ResponseWriter, r *http.Request) {
	snap := s.mgr.Snapshot()
	payload := map[string]any{
		"state":            snap.State,
		"detail":           snap.Detail,
		"current_node":     sanitizeNode(snap.CurrentNode),
		"blacklist_count":  snap.BlacklistCount,
		"uptime_sec":       snap.UptimeSec,
		"proxy_port":       s.cfg.LocalProxyPort,
		"ui_port":          s.cfg.UIPort,
		"route_mode":       snap.RouteMode,
		"route_country":    snap.RouteCountry,
		"route_node":       snap.RouteNode,
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
			// sanitize every snapshot — never leak the raw OpenVPN config
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
