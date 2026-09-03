package vpngate

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

const (
	MaxMirrorCount     = 64
	MaxMirrorTextBytes = 16 << 10
)

var (
	ErrMirrorTextTooLarge  = errors.New("mirror text is too large")
	ErrMirrorCountTooLarge = errors.New("mirror list is too large")
)

var (
	// Keep URL matching deliberately narrow: only an explicit HTTP(S) scheme
	// is accepted. The token body is cut separately so adjacent entries on one
	// line (for example, comma-separated URLs) are all discovered.
	mirrorURLRe     = regexp.MustCompile(`(?i)\bhttps?://`)
	bareHostRe      = regexp.MustCompile(`(?i)^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)*$`)
	bareIPv6Re      = regexp.MustCompile(`(?i)^\[[0-9a-f:]+\](?::[^\s/]+)?$`)
	bareCandidateRe = regexp.MustCompile(`(?i)(localhost(?::[0-9]{1,5})?\b|\[[0-9a-f:]+\](:[0-9]{1,5})?|[a-z0-9][a-z0-9-]{0,62}(\.[a-z0-9][a-z0-9-]{0,62})+(:[0-9]{1,5})?|[0-9]{1,3}(\.[0-9]{1,3}){3}(:[0-9]{1,5})?|[a-z0-9-]+:[0-9]{1,5})`)
)

type mirrorURLSpan struct {
	start int
	end   int
}

// MirrorIssue describes an ignored URL token from a user-provided source list.
type MirrorIssue struct {
	Token  string `json:"token"`
	Reason string `json:"reason"`
}

// ParseMirrorText extracts HTTP(S) URL tokens and normalizes them to source
// origins. URL userinfo is retained for HTTP Basic Auth, while paths, queries,
// and fragments are discarded. It deliberately accepts surrounding prose
// because mirror lists are commonly copied from a web page with location labels.
func ParseMirrorText(text string) (sources []string, issues []MirrorIssue) {
	sources = make([]string, 0)
	issues = make([]MirrorIssue, 0)
	seen := make(map[string]struct{})
	spans := make([]mirrorURLSpan, 0)
	for _, tokenInfo := range extractMirrorURLTokens(text) {
		token := tokenInfo.token
		spans = append(spans, tokenInfo.span)
		source, err := NormalizeMirrorSource(token)
		if err != nil {
			issues = append(issues, MirrorIssue{Token: RedactSourceURL(token), Reason: err.Error()})
			continue
		}
		origin, err := MirrorSourceOrigin(source)
		if err != nil {
			issues = append(issues, MirrorIssue{Token: RedactSourceURL(token), Reason: err.Error()})
			continue
		}
		if _, ok := seen[origin]; ok {
			continue
		}
		seen[origin] = struct{}{}
		sources = append(sources, source)
	}
	appendBareMirrorIssues(text, spans, &issues)
	return sources, issues
}

type mirrorURLToken struct {
	token string
	span  mirrorURLSpan
}

func extractMirrorURLTokens(text string) []mirrorURLToken {
	matches := mirrorURLRe.FindAllStringIndex(text, -1)
	tokens := make([]mirrorURLToken, 0, len(matches))
	coveredUntil := 0
	for _, match := range matches {
		start := match[0]
		if start < coveredUntil {
			continue
		}
		segment := text[start:]
		cut := mirrorSegmentCut(segment)
		if cut >= 0 {
			segment = segment[:cut]
		}
		token := trimURLToken(segment)
		end := start + len(segment)
		coveredUntil = end
		if token == "" {
			continue
		}
		tokens = append(tokens, mirrorURLToken{token: token, span: mirrorURLSpan{start: start, end: end}})
	}
	return tokens
}

func mirrorSegmentCut(segment string) int {
	for offset, r := range segment {
		if unicode.IsSpace(r) || strings.ContainsRune("<>\"'()（）},;|，；、｜", r) {
			return offset
		}
		if r == '[' || r == '{' || r == '【' {
			schemeEnd := strings.Index(strings.ToLower(segment), "://")
			if schemeEnd < 0 || offset > schemeEnd+3 {
				return offset
			}
		}
	}
	return -1
}

func appendBareMirrorIssues(text string, spans []mirrorURLSpan, issues *[]MirrorIssue) {
	seen := make(map[string]struct{}, len(*issues))
	for _, issue := range *issues {
		seen[issue.Token+"\x00"+issue.Reason] = struct{}{}
	}
	for _, match := range bareCandidateRe.FindAllStringIndex(text, -1) {
		if overlapsMirrorURL(match[0], match[1], spans) {
			continue
		}
		raw := trimBareToken(text[match[0]:match[1]])
		reason := "URL must include http:// or https://"
		if strings.Contains(raw, "://") {
			scheme := strings.ToLower(raw[:strings.Index(raw, "://")])
			if scheme == "http" || scheme == "https" {
				continue
			}
			reason = "URL must use http or https"
		} else if !isBareMirrorToken(raw) {
			continue
		}
		key := raw + "\x00" + reason
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		*issues = append(*issues, MirrorIssue{Token: raw, Reason: reason})
	}
}

func trimBareToken(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.Trim(raw, ",.;:!?<>\"'，。；：！？》")
	for len(raw) > 0 {
		runes := []rune(raw)
		if last := runes[len(runes)-1]; last == ')' && countRune(runes, ')') > countRune(runes, '(') {
			raw = string(runes[:len(runes)-1])
			continue
		}
		if last := runes[len(runes)-1]; last == ']' && countRune(runes, ']') > countRune(runes, '[') {
			raw = string(runes[:len(runes)-1])
			continue
		}
		if last := runes[len(runes)-1]; last == '）' && countRune(runes, '）') > countRune(runes, '（') {
			raw = string(runes[:len(runes)-1])
			continue
		}
		if last := runes[len(runes)-1]; last == '】' && countRune(runes, '】') > countRune(runes, '【') {
			raw = string(runes[:len(runes)-1])
			continue
		}
		if len(runes) < 2 {
			break
		}
		if runes[0] == '(' && runes[len(runes)-1] == ')' {
			raw = string(runes[1 : len(runes)-1])
			continue
		}
		if runes[0] == '（' && runes[len(runes)-1] == '）' {
			raw = string(runes[1 : len(runes)-1])
			continue
		}
		break
	}
	return raw
}

func overlapsMirrorURL(start, end int, spans []mirrorURLSpan) bool {
	for _, span := range spans {
		if start < span.end && end > span.start {
			return true
		}
	}
	return false
}

func isBareMirrorToken(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(raw, "://") || strings.ContainsAny(raw, "@<>\"'") {
		return false
	}
	// A bare URL may include a path; only the authority-like prefix matters
	// for deciding whether to explain why it was ignored.
	if cut := strings.IndexAny(raw, "/?#"); cut >= 0 {
		raw = raw[:cut]
	}
	if raw == "" || bareIPv6Re.MatchString(raw) {
		return true
	}
	host := raw
	port := ""
	if strings.Count(raw, ":") == 1 {
		parts := strings.SplitN(raw, ":", 2)
		host, port = parts[0], parts[1]
	}
	if host == "" {
		return false
	}
	if net.ParseIP(host) != nil {
		return true
	}
	if !bareHostRe.MatchString(host) {
		return false
	}
	return strings.Contains(host, ".") || port != "" || strings.EqualFold(host, "localhost")
}

func trimURLToken(token string) string {
	for len(token) > 0 {
		runes := []rune(token)
		last := runes[len(runes)-1]
		if strings.ContainsRune(".,;:!?}>'\"`’‘”“〉》】》，。；：！？…", last) {
			runes = runes[:len(runes)-1]
			token = string(runes)
			continue
		}
		switch last {
		case ')':
			if countRune(runes, ')') > countRune(runes, '(') {
				token = string(runes[:len(runes)-1])
				continue
			}
		case ']':
			if countRune(runes, ']') > countRune(runes, '[') {
				token = string(runes[:len(runes)-1])
				continue
			}
		case '）':
			if countRune(runes, '）') > countRune(runes, '（') {
				token = string(runes[:len(runes)-1])
				continue
			}
		case '】':
			if countRune(runes, '】') > countRune(runes, '【') {
				token = string(runes[:len(runes)-1])
				continue
			}
		}
		break
	}
	return token
}

func countRune(values []rune, want rune) int {
	count := 0
	for _, value := range values {
		if value == want {
			count++
		}
	}
	return count
}

// NormalizeMirrorOrigin converts a credential-free URL to scheme://host[:port].
// It is used by the DNS validation boundary, which must never consume userinfo.
func NormalizeMirrorOrigin(raw string) (string, error) {
	return normalizeMirrorURL(raw, false)
}

// NormalizeMirrorSource converts a source URL to
// scheme://[userinfo@]host[:port]. It retains URL userinfo for HTTP Basic Auth
// and strips paths, queries, and fragments before the source is persisted.
func NormalizeMirrorSource(raw string) (string, error) {
	return normalizeMirrorURL(raw, true)
}

func normalizeMirrorURL(raw string, allowUserInfo bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("empty URL")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("invalid URL")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", errors.New("URL must use http or https")
	}
	if u.User != nil {
		if !allowUserInfo {
			return "", errors.New("URL credentials are not allowed")
		}
		if u.User.Username() == "" {
			return "", errors.New("URL username must not be empty")
		}
	}
	if u.Hostname() == "" || u.Opaque != "" {
		return "", errors.New("URL must include a host")
	}
	if strings.HasSuffix(u.Host, ":") {
		return "", errors.New("URL port is invalid")
	}
	if strings.Contains(u.Hostname(), "%") {
		return "", errors.New("URL host zones are not allowed")
	}
	port := u.Port()
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", errors.New("URL port is invalid")
		}
		port = strconv.Itoa(n)
		if (scheme == "http" && n == 80) || (scheme == "https" && n == 443) {
			port = ""
		}
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "" || strings.ContainsAny(host, " \t\r\n") {
		return "", errors.New("URL host is invalid")
	}
	if ip := parseMirrorIPLiteral(host); ip != nil {
		host = ip.String()
	} else if looksNumericHost(host) || looksAlternateNumericHost(host) {
		return "", errors.New("URL host is invalid")
	} else if !validMirrorHostname(host) {
		return "", errors.New("URL host is invalid")
	}
	normalized := &url.URL{Scheme: scheme}
	if port == "" {
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
		normalized.Host = host
	} else {
		normalized.Host = net.JoinHostPort(host, port)
	}
	if u.User != nil {
		if password, ok := u.User.Password(); ok {
			normalized.User = url.UserPassword(u.User.Username(), password)
		} else {
			normalized.User = url.User(u.User.Username())
		}
	}
	return normalized.String(), nil
}

// MirrorSourceOrigin returns the credential-free origin used for duplicate
// detection and SSRF validation. It accepts the same source syntax as
// NormalizeMirrorSource.
func MirrorSourceOrigin(raw string) (string, error) {
	source, err := NormalizeMirrorSource(raw)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(source)
	if err != nil {
		return "", errors.New("invalid URL")
	}
	u.User = nil
	return NormalizeMirrorOrigin(u.String())
}

// HasMirrorSourceCredentials reports whether a normalized source includes
// non-empty URL userinfo for HTTP Basic Auth.
func HasMirrorSourceCredentials(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.User != nil && u.User.Username() != ""
}

// RedactSourceURL removes URL userinfo before a source reaches an API response
// or log record. Invalid input is intentionally replaced rather than echoed.
func RedactSourceURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<invalid source URL>"
	}
	u.User = nil
	return u.String()
}

// ValidateMirrorOrigin resolves the origin and rejects targets that are not
// publicly routable. It is intentionally called both when saving and before
// each request because DNS answers can change after configuration time.
func ValidateMirrorOrigin(ctx context.Context, origin string) error {
	ctx = nonNilMirrorContext(ctx)
	normalized, err := NormalizeMirrorOrigin(origin)
	if err != nil {
		return fmt.Errorf("invalid mirror URL: %w", err)
	}
	u, err := url.Parse(normalized)
	if err != nil {
		return errors.New("invalid mirror URL")
	}
	host := u.Hostname()
	if host == "" {
		return errors.New("mirror URL has no host")
	}
	if ip := parseMirrorIPLiteral(host); ip != nil {
		if !isPublicMirrorIP(ip) {
			return fmt.Errorf("mirror host resolves to non-public IP %s", ip)
		}
		return nil
	}
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return fmt.Errorf("mirror host DNS lookup failed: %w", err)
	}
	if len(ips) == 0 {
		return errors.New("mirror host has no IP address")
	}
	for _, ip := range ips {
		if !isPublicMirrorIP(ip) {
			return fmt.Errorf("mirror host resolves to non-public IP %s", ip)
		}
	}
	return nil
}

func nonNilMirrorContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func validMirrorHostname(host string) bool {
	if len(host) > 253 || !bareHostRe.MatchString(host) {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
	}
	return true
}

func parseMirrorIPLiteral(host string) net.IP {
	if ip := net.ParseIP(host); ip != nil {
		return ip
	}
	if !looksNumericIPv4(host) {
		return nil
	}
	parts := strings.Split(host, ".")
	var octets [4]byte
	for i, part := range parts {
		if len(part) > 1 && part[0] == '0' {
			return nil
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || n > 255 {
			return nil
		}
		octets[i] = byte(n)
	}
	return net.IPv4(octets[0], octets[1], octets[2], octets[3])
}

func looksNumericIPv4(host string) bool {
	parts := strings.Split(host, ".")
	if len(parts) != 4 {
		return false
	}
	for _, part := range parts {
		if part == "" || !allDigits(part) {
			return false
		}
	}
	return true
}

func looksNumericHost(host string) bool {
	if allDigits(host) {
		return true
	}
	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if !allDigits(part) {
			return false
		}
	}
	return true
}

func looksAlternateNumericHost(host string) bool {
	host = strings.ToLower(host)
	foundPrefix := false
	for _, label := range strings.Split(host, ".") {
		if strings.HasPrefix(label, "0x") {
			foundPrefix = true
			label = label[2:]
			if label == "" {
				return false
			}
			for _, r := range label {
				if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
					return false
				}
			}
			continue
		}
		if !allDigits(label) && !allHexDigits(label) {
			return false
		}
	}
	return foundPrefix
}

func allHexDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range strings.ToLower(value) {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isPublicMirrorIP(ip net.IP) bool {
	ip = ip.To16()
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		for _, cidr := range []string{
			// Unroutable, shared, protocol-assignment, documentation,
			// benchmarking, and reserved IPv4 ranges. IsPrivate does not
			// cover all of these special-purpose allocations.
			"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24",
			"192.31.196.0/24", "192.52.193.0/24", "192.88.99.0/24", "192.175.48.0/24",
			"198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4",
		} {
			_, block, _ := net.ParseCIDR(cidr)
			if block.Contains(v4) {
				return false
			}
		}
	} else {
		for _, cidr := range []string{
			// IPv4-compatible and special-purpose IPv6 ranges. Keep the
			// list explicit because IsGlobalUnicast intentionally includes
			// several of these ranges.
			"::/96",         // deprecated IPv4-compatible addresses
			"100::/64",      // discard-only
			"2001::/32",     // TEREDO
			"2001:1::/32",   // IETF protocol assignments
			"2001:2::/48",   // benchmarking
			"2001:10::/28",  // ORCHID
			"2001:20::/28",  // ORCHIDv2
			"2001:db8::/32", // documentation
			"3fff::/20",     // documentation (RFC 9637)
			"5f00::/16",     // discard-only / special use
			"fec0::/10",     // deprecated site-local
		} {
			_, block, _ := net.ParseCIDR(cidr)
			if block.Contains(ip) {
				return false
			}
		}
	}
	return true
}
