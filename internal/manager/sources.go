package manager

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"conduitvpn/internal/benchmark"
	"conduitvpn/internal/logx"
	"conduitvpn/internal/node"
	"conduitvpn/internal/state"
	"conduitvpn/internal/vpngate"
)

const mirrorAPIPath = "/api/iphone/"

type VPNGateSourceAttempt struct {
	URL        string `json:"url"`
	OK         bool   `json:"ok"`
	Error      string `json:"error,omitempty"`
	FinishedAt string `json:"finished_at,omitempty"`
}

type VPNGateSourceStatus struct {
	OfficialURL   string                 `json:"official_url"`
	Mirrors       []string               `json:"mirrors"`
	CurrentSource string                 `json:"current_source"`
	LastAttemptAt string                 `json:"last_attempt_at"`
	LastSuccessAt string                 `json:"last_success_at"`
	Attempts      []VPNGateSourceAttempt `json:"attempts"`
	Refreshing    bool                   `json:"refreshing"`
}

type VPNGateMirrorUpdate struct {
	Mirrors []string              `json:"mirrors"`
	Issues  []vpngate.MirrorIssue `json:"issues,omitempty"`
}

var ErrVPNGateSourcesSave = errors.New("could not persist VPNGate mirror configuration")

func (m *Manager) loadVPNGateSources() {
	sources, err := m.store.LoadVPNGateSources()
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			logx.Debug("no VPNGate mirror configuration yet", "err", err)
		}
		return
	}
	valid := make([]string, 0, len(sources.Mirrors))
	seen := make(map[string]struct{})
	for _, raw := range sources.Mirrors {
		if len(valid) >= vpngate.MaxMirrorCount {
			break
		}
		source, err := vpngate.NormalizeMirrorSource(raw)
		if err != nil {
			continue
		}
		origin, err := vpngate.MirrorSourceOrigin(source)
		if err != nil {
			continue
		}
		if _, ok := seen[origin]; ok {
			continue
		}
		seen[origin] = struct{}{}
		valid = append(valid, source)
	}
	m.sourceMu.Lock()
	m.mirrors = valid
	m.sourceStatus.Mirrors = redactSourceURLs(valid)
	m.sourceMu.Unlock()
	if len(valid) > 0 {
		logx.Info("VPNGate mirrors loaded", "count", len(valid))
	}
}

func (m *Manager) VPNGateSourceStatus() VPNGateSourceStatus {
	m.sourceMu.Lock()
	defer m.sourceMu.Unlock()
	status := m.sourceStatus
	status.OfficialURL = redactSourceURL(m.cfg.APIURL)
	status.Mirrors = redactSourceURLs(m.mirrors)
	status.Attempts = append([]VPNGateSourceAttempt{}, m.sourceStatus.Attempts...)
	return status
}

// redactSourceURL removes userinfo before a source URL reaches the Web UI or
// structured logs. The original URL remains in sourceEndpoint for requests.
func redactSourceURL(raw string) string {
	return vpngate.RedactSourceURL(raw)
}

func redactSourceURLs(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	redacted := make([]string, 0, len(values))
	for _, value := range values {
		redacted = append(redacted, redactSourceURL(value))
	}
	return redacted
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	return append([]string{}, values...)
}

func cloneNode(n *node.Node) *node.Node {
	if n == nil {
		return nil
	}
	c := *n
	return &c
}

func (m *Manager) SetVPNGateMirrors(ctx context.Context, text string) (VPNGateMirrorUpdate, error) {
	ctx = nonNilContext(ctx)
	if len(text) > vpngate.MaxMirrorTextBytes {
		return VPNGateMirrorUpdate{}, fmt.Errorf("%w: mirror text exceeds %d bytes (got %d)", vpngate.ErrMirrorTextTooLarge, vpngate.MaxMirrorTextBytes, len(text))
	}
	if err := ctx.Err(); err != nil {
		return VPNGateMirrorUpdate{}, err
	}
	if strings.TrimSpace(text) == "" {
		m.sourceMu.Lock()
		err := m.store.SaveVPNGateSources(state.VPNGateSources{Mirrors: []string{}})
		if err == nil {
			m.mirrors = []string{}
			m.sourceStatus.Mirrors = []string{}
		}
		m.sourceMu.Unlock()
		if err != nil {
			return VPNGateMirrorUpdate{}, fmt.Errorf("%w: %v", ErrVPNGateSourcesSave, err)
		}
		m.triggerSourceRefresh()
		return VPNGateMirrorUpdate{Mirrors: []string{}}, nil
	}

	sources, issues := vpngate.ParseMirrorText(text)
	if len(sources) > vpngate.MaxMirrorCount {
		return VPNGateMirrorUpdate{}, fmt.Errorf("%w: mirror list exceeds %d entries (got %d)", vpngate.ErrMirrorCountTooLarge, vpngate.MaxMirrorCount, len(sources))
	}
	m.sourceMu.Lock()
	existing := cloneStrings(m.mirrors)
	m.sourceMu.Unlock()
	sources = preserveMirrorCredentials(sources, existing)
	if !m.demo {
		var checkedIssues []vpngate.MirrorIssue
		sources, checkedIssues = m.validateMirrorOrigins(ctx, sources, 5*time.Second)
		issues = append(issues, checkedIssues...)
		if err := ctx.Err(); err != nil {
			return VPNGateMirrorUpdate{Mirrors: redactSourceURLs(sources), Issues: issues}, err
		}
	}
	if len(sources) == 0 {
		return VPNGateMirrorUpdate{Issues: issues}, errors.New("no valid HTTP(S) mirror URL found")
	}
	if err := ctx.Err(); err != nil {
		return VPNGateMirrorUpdate{Mirrors: redactSourceURLs(sources), Issues: issues}, err
	}
	m.sourceMu.Lock()
	err := m.store.SaveVPNGateSources(state.VPNGateSources{Mirrors: sources})
	if err == nil {
		m.mirrors = append([]string(nil), sources...)
		m.sourceStatus.Mirrors = redactSourceURLs(sources)
	}
	m.sourceMu.Unlock()
	if err != nil {
		return VPNGateMirrorUpdate{}, fmt.Errorf("%w: %v", ErrVPNGateSourcesSave, err)
	}
	m.triggerSourceRefresh()
	return VPNGateMirrorUpdate{Mirrors: redactSourceURLs(sources), Issues: issues}, nil
}

// preserveMirrorCredentials keeps existing Basic Auth for a redacted source
// returned by the settings API. Pasting the same source with userinfo replaces
// its credentials; deleting it and saving removes them.
func preserveMirrorCredentials(sources, existing []string) []string {
	protectedByOrigin := make(map[string]string)
	for _, source := range existing {
		if !vpngate.HasMirrorSourceCredentials(source) {
			continue
		}
		origin, err := vpngate.MirrorSourceOrigin(source)
		if err == nil {
			protectedByOrigin[origin] = source
		}
	}
	preserved := cloneStrings(sources)
	for i, source := range preserved {
		if vpngate.HasMirrorSourceCredentials(source) {
			continue
		}
		origin, err := vpngate.MirrorSourceOrigin(source)
		if err != nil {
			continue
		}
		if protected, ok := protectedByOrigin[origin]; ok {
			preserved[i] = protected
		}
	}
	return preserved
}

func (m *Manager) validateMirrorOrigins(ctx context.Context, sources []string, timeout time.Duration) ([]string, []vpngate.MirrorIssue) {
	ctx = nonNilContext(ctx)
	validator := m.mirrorValidator
	if validator == nil {
		validator = vpngate.ValidateMirrorOrigin
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	results := make([]error, len(sources))
	semaphore := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, source := range sources {
		i, source := i, source
		wg.Add(1)
		go func() {
			defer wg.Done()
			origin, err := vpngate.MirrorSourceOrigin(source)
			if err != nil {
				results[i] = err
				return
			}
			select {
			case semaphore <- struct{}{}:
			case <-ctx.Done():
				results[i] = ctx.Err()
				return
			}
			defer func() { <-semaphore }()
			checkCtx, cancel := context.WithTimeout(ctx, timeout)
			results[i] = validator(checkCtx, origin)
			cancel()
		}()
	}
	wg.Wait()
	checked := make([]string, 0, len(sources))
	issues := make([]vpngate.MirrorIssue, 0)
	for i, source := range sources {
		if err := results[i]; err != nil {
			issues = append(issues, vpngate.MirrorIssue{Token: redactSourceURL(source), Reason: err.Error()})
			continue
		}
		checked = append(checked, source)
	}
	return checked, issues
}

func (m *Manager) validateMirror(ctx context.Context, origin string, timeout time.Duration) error {
	ctx = nonNilContext(ctx)
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	validator := m.mirrorValidator
	if validator == nil {
		validator = vpngate.ValidateMirrorOrigin
	}
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return validator(checkCtx, origin)
}

func mirrorEndpoint(source string) string {
	if normalized, err := vpngate.NormalizeMirrorSource(source); err == nil {
		source = normalized
	}
	return strings.TrimRight(source, "/") + mirrorAPIPath
}

type sourceEndpoint struct {
	URL    string
	Origin string
}

func (m *Manager) sourceList() []sourceEndpoint {
	m.sourceMu.Lock()
	mirrors := append([]string(nil), m.mirrors...)
	m.sourceMu.Unlock()
	return buildSourceList(m.cfg.APIURL, mirrors)
}

func buildSourceList(officialURL string, mirrors []string) []sourceEndpoint {
	sources := make([]sourceEndpoint, 0, len(mirrors)+1)
	sources = append(sources, sourceEndpoint{URL: officialURL})
	for _, mirror := range mirrors {
		origin, err := vpngate.MirrorSourceOrigin(mirror)
		if err != nil {
			continue
		}
		sources = append(sources, sourceEndpoint{URL: mirrorEndpoint(mirror), Origin: origin})
	}
	return sources
}

// beginSourceRefresh snapshots the configured source list before any network
// work starts. A configuration update that races with this call is therefore
// guaranteed to apply on the next refresh round, never halfway through one.
func (m *Manager) beginSourceRefresh() []sourceEndpoint {
	m.sourceMu.Lock()
	mirrors := append([]string(nil), m.mirrors...)
	m.sourceStatus.Refreshing = true
	m.sourceStatus.OfficialURL = redactSourceURL(m.cfg.APIURL)
	m.sourceStatus.Mirrors = redactSourceURLs(mirrors)
	m.sourceStatus.Attempts = []VPNGateSourceAttempt{}
	m.sourceStatus.LastAttemptAt = time.Now().Format(time.RFC3339)
	m.sourceMu.Unlock()
	return buildSourceList(m.cfg.APIURL, mirrors)
}

func (m *Manager) finishSourceRefresh(attempts []VPNGateSourceAttempt, current string, success bool) {
	now := time.Now().Format(time.RFC3339)
	m.sourceMu.Lock()
	m.sourceStatus.Refreshing = false
	if success {
		m.sourceStatus.CurrentSource = current
	}
	m.sourceStatus.Attempts = append([]VPNGateSourceAttempt{}, attempts...)
	if success {
		m.sourceStatus.LastSuccessAt = now
	}
	m.sourceMu.Unlock()
}

func (m *Manager) requestRefresh() {
	m.refreshPendingMu.Lock()
	wasPending := m.refreshSeq != m.refreshConsumed
	m.refreshSeq++
	m.refreshPendingMu.Unlock()
	if wasPending {
		return
	}
	select {
	case m.refreshCh <- struct{}{}:
	default:
	}
}

// takeRefreshRequest consumes one coalesced refresh request. Keeping the
// request sequence separate from the wake-up channel prevents a request that
// arrives while a refresh is starting or running from being lost.
func (m *Manager) takeRefreshRequest() bool {
	m.refreshPendingMu.Lock()
	defer m.refreshPendingMu.Unlock()
	if m.refreshSeq == m.refreshConsumed {
		return false
	}
	m.refreshConsumed = m.refreshSeq
	return true
}

func (m *Manager) triggerSourceRefresh() {
	if m.demo {
		m.refreshDemoNodes()
		return
	}
	m.requestRefresh()
}

func (m *Manager) refreshLoop(ctx context.Context) {
	ctx = nonNilContext(ctx)
	ticker := time.NewTicker(m.refreshInterval())
	defer ticker.Stop()
	defer func() {
		m.refreshPendingMu.Lock()
		m.refreshConsumed = m.refreshSeq
		m.refreshPendingMu.Unlock()
		// Do not leave a queued indicator behind when the manager is shutting
		// down before the pending request can be serviced.
		m.sourceMu.Lock()
		m.sourceStatus.Refreshing = false
		m.sourceMu.Unlock()
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.requestRefresh()
		case <-m.refreshCh:
			for m.takeRefreshRequest() {
				m.refreshNodes(ctx, false)
				if ctx.Err() != nil {
					return
				}
			}
		}
	}
}

func (m *Manager) refreshNodes(ctx context.Context, foreground bool) []*node.Node {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
	ctx = nonNilContext(ctx)

	if m.demo {
		m.refreshDemoNodes()
		return m.candidateSnapshot()
	}
	if foreground {
		m.mu.Lock()
		connected := m.state == StateConnected
		m.mu.Unlock()
		if !connected {
			m.setState(StateFetching, "")
		}
	}
	sources := m.beginSourceRefresh()
	timeout := m.fetchTimeout()
	validationTimeout := mirrorValidationTimeout(timeout)
	client := vpngate.NewClient(m.fetchUpstream, timeout)
	attempts := make([]VPNGateSourceAttempt, 0)
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			break
		}
		displayURL := redactSourceURL(source.URL)
		attempt := VPNGateSourceAttempt{URL: displayURL}
		if source.Origin != "" {
			if err := m.validateMirror(ctx, source.Origin, validationTimeout); err != nil {
				attempt.Error = err.Error()
				attempt.FinishedAt = time.Now().Format(time.RFC3339)
				attempts = append(attempts, attempt)
				logx.Warn("VPNGate mirror rejected", "url", displayURL, "err", err)
				if ctx.Err() != nil {
					break
				}
				continue
			}
		}

		raw, err := client.Fetch(ctx, source.URL)
		if err != nil {
			attempt.Error = err.Error()
			attempt.FinishedAt = time.Now().Format(time.RFC3339)
			attempts = append(attempts, attempt)
			logx.Warn("VPNGate source fetch failed", "url", displayURL, "err", err)
			if ctx.Err() != nil {
				break
			}
			continue
		}
		parsed, err := vpngate.Parse(raw)
		if err != nil {
			attempt.Error = "parse: " + err.Error()
			attempt.FinishedAt = time.Now().Format(time.RFC3339)
			attempts = append(attempts, attempt)
			logx.Warn("VPNGate source parse failed", "url", displayURL, "err", err)
			continue
		}
		candidates := node.SortByScore(parsed)
		if len(candidates) == 0 {
			attempt.Error = "parse: no valid nodes"
			attempt.FinishedAt = time.Now().Format(time.RFC3339)
			attempts = append(attempts, attempt)
			logx.Warn("VPNGate source returned no valid nodes", "url", displayURL)
			continue
		}
		if len(candidates) > m.cfg.MaxScanRows && m.cfg.MaxScanRows > 0 {
			candidates = candidates[:m.cfg.MaxScanRows]
		}
		logx.Info("benchmarking candidates", "source", displayURL, "count", len(candidates))
		benchmark.Run(ctx, candidates, m.benchConcurrency(), m.benchTimeout())
		if ctx.Err() != nil {
			attempt.Error = ctx.Err().Error()
			attempt.FinishedAt = time.Now().Format(time.RFC3339)
			attempts = append(attempts, attempt)
			break
		}
		if err := m.store.SaveNodes(candidates); err != nil {
			logx.Warn("save nodes failed", "err", err)
		}
		if ctx.Err() != nil {
			attempt.Error = ctx.Err().Error()
			attempt.FinishedAt = time.Now().Format(time.RFC3339)
			attempts = append(attempts, attempt)
			break
		}
		m.setCandidatePool(candidates)
		attempt.OK = true
		attempt.FinishedAt = time.Now().Format(time.RFC3339)
		attempts = append(attempts, attempt)
		m.finishSourceRefresh(attempts, displayURL, true)
		logx.Info("VPNGate source selected", "url", displayURL, "nodes", len(candidates))
		return candidates
	}

	logx.Warn("all VPNGate sources failed; falling back to cached nodes")
	// An in-memory pool may be newer than the last successful disk write.
	// Keep it authoritative during a transient source outage; only fall back
	// to the persisted list when no candidate cache exists yet.
	cached := m.candidateSnapshot()
	if len(cached) == 0 {
		persisted, err := m.store.LoadNodes()
		if err == nil && len(persisted) > 0 {
			cached = persisted
			m.setCandidatePool(cached)
		} else if err != nil {
			logx.Debug("cached VPNGate nodes unavailable", "err", err)
		}
	}
	m.finishSourceRefresh(attempts, "", false)
	return cached
}

func (m *Manager) fetchTimeout() time.Duration {
	if m.cfg.FetchTimeout > 0 {
		return m.cfg.FetchTimeout
	}
	return 30 * time.Second
}

func mirrorValidationTimeout(fetchTimeout time.Duration) time.Duration {
	const max = 5 * time.Second
	if fetchTimeout <= 0 || fetchTimeout > max {
		return max
	}
	return fetchTimeout
}

func (m *Manager) benchConcurrency() int {
	if m.cfg.BenchConcurrency > 0 {
		return m.cfg.BenchConcurrency
	}
	return 1
}

func (m *Manager) benchTimeout() time.Duration {
	if m.cfg.BenchTimeout > 0 {
		return m.cfg.BenchTimeout
	}
	return 10 * time.Second
}

func nonNilContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func (m *Manager) setCandidatePool(nodes []*node.Node) {
	clean := make([]*node.Node, 0, len(nodes))
	for _, n := range nodes {
		if n != nil {
			clean = append(clean, n)
		}
	}
	m.candidateMu.Lock()
	m.candidatePool = append([]*node.Node(nil), clean...)
	m.candidateMu.Unlock()
	select {
	case m.candidateCh <- struct{}{}:
	default:
	}
}

func (m *Manager) candidateSnapshot() []*node.Node {
	m.candidateMu.RLock()
	defer m.candidateMu.RUnlock()
	return append([]*node.Node(nil), m.candidatePool...)
}
