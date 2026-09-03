package vpngate

import (
	"bytes"
	"encoding/base64"
	"encoding/csv"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"

	"conduitvpn/internal/node"
)

// VPNGate API layout: an optional metadata/preamble section, a
// "#HostName,IP,..." CSV header, then data rows. The last column is the
// base64-encoded OpenVPN config of the node.
var remoteRe = regexp.MustCompile(`(?m)^remote\s+(\S+)\s+(\d+)(?:\s+(udp|tcp))?`)

func Parse(raw []byte) ([]*node.Node, error) {
	records, err := readCSV(raw)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, errors.New("empty csv payload")
	}

	headerAt := findHeaderRecord(records)
	if headerAt < 0 {
		return nil, errors.New("VPNGate CSV header not found")
	}
	col := indexColumns(records[headerAt])
	need := []string{
		"IP", "Score", "Ping", "Speed",
		"CountryLong", "CountryShort", "NumVpnSessions", "Uptime",
		"LogType", "Operator", "Message", "OpenVPN_ConfigData_Base64",
	}
	for _, k := range need {
		if _, ok := lookupColumn(col, k); !ok {
			return nil, fmt.Errorf("missing csv column %q", k)
		}
	}
	get := func(row []string, key string) string {
		i, ok := lookupColumn(col, key)
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}

	var nodes []*node.Node
	for _, row := range records[headerAt+1:] {
		if len(row) == 0 || strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(row[0], "\ufeff")), "*") {
			continue // trailing "*END" etc.
		}
		cfgText, err := decodeConfig(get(row, "OpenVPN_ConfigData_Base64"))
		if err != nil {
			continue // skip rows with corrupt configs
		}
		cfgText, err = ValidateOpenVPNProfile(cfgText, get(row, "IP"))
		if err != nil {
			continue // untrusted profile uses unsupported or unsafe directives
		}
		n := &node.Node{
			IP:           get(row, "IP"),
			Score:        atoi(get(row, "Score")),
			Ping:         atoi(get(row, "Ping")),
			Speed:        atoi(get(row, "Speed")),
			CountryLong:  get(row, "CountryLong"),
			CountryShort: get(row, "CountryShort"),
			Sessions:     atoi(get(row, "NumVpnSessions")),
			Uptime:       atoi64(get(row, "Uptime")),
			LogType:      get(row, "LogType"),
			Operator:     get(row, "Operator"),
			Message:      get(row, "Message"),
			ConfigText:   cfgText,
		}
		// HostName has historically been part of the API, but some mirrors
		// omit it or spell it differently. The IP is still a stable and
		// validated node identity, so use it as a safe display/blacklist key.
		n.HostName = get(row, "HostName")
		if n.HostName == "" {
			n.HostName = n.IP
		}
		n.RemoteHost, n.RemotePort, n.RemoteProto = parseRemote(cfgText, n.IP)
		nodes = append(nodes, n)
	}
	return nodes, nil
}

var safeTokenRe = regexp.MustCompile(`^[A-Za-z0-9_.:+,=/@-]+$`)

// ValidateOpenVPNProfile accepts the narrow client-profile subset used by
// VPNGate. Profiles are network input and must never be allowed to specify
// scripts, plugins, arbitrary files, management endpoints, or a different
// remote server than the CSV row advertised.
func ValidateOpenVPNProfile(profile, expectedIP string) (string, error) {
	if len(profile) == 0 || len(profile) > 1<<20 {
		return "", errors.New("invalid OpenVPN profile size")
	}
	if !isPublicIPv4(expectedIP) {
		return "", fmt.Errorf("invalid node IP %q", expectedIP)
	}
	lines := strings.Split(strings.ReplaceAll(profile, "\r\n", "\n"), "\n")
	var out []string
	block := ""
	remoteSeen := false
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if block != "" {
			if line == "</"+block+">" {
				out = append(out, line)
				block = ""
				continue
			}
			if line == "" {
				continue
			}
			out = append(out, raw)
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "<") {
			name := strings.TrimSuffix(strings.TrimPrefix(line, "<"), ">")
			switch name {
			case "ca", "cert", "key", "tls-auth", "tls-crypt":
				if line != "<"+name+">" {
					return "", fmt.Errorf("invalid inline block %q", line)
				}
				block = name
				out = append(out, line)
				continue
			default:
				return "", fmt.Errorf("unsupported inline block %q", line)
			}
		}

		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if err := validateDirective(fields, expectedIP, &remoteSeen); err != nil {
			return "", err
		}
		out = append(out, strings.Join(fields, " "))
	}
	if block != "" {
		return "", fmt.Errorf("unterminated <%s> block", block)
	}
	if !remoteSeen {
		return "", errors.New("profile has no permitted remote directive")
	}
	return strings.Join(out, "\n") + "\n", nil
}

func validateDirective(fields []string, expectedIP string, remoteSeen *bool) error {
	name := fields[0]
	args := fields[1:]
	noArg := map[string]bool{"client": true, "nobind": true, "persist-key": true, "persist-tun": true, "auth-nocache": true, "pull": true, "fast-io": true}
	if noArg[name] {
		if len(args) != 0 {
			return fmt.Errorf("%s does not accept arguments", name)
		}
		return nil
	}
	switch name {
	case "dev":
		if len(args) != 1 || args[0] != "tun" {
			return errors.New("only dev tun is allowed")
		}
	case "dev-type":
		if len(args) != 1 || args[0] != "tun" {
			return errors.New("only dev-type tun is allowed")
		}
	case "proto":
		if len(args) != 1 || !map[string]bool{"udp": true, "udp4": true, "tcp": true, "tcp4": true, "tcp-client": true}[args[0]] {
			return errors.New("unsupported proto")
		}
	case "remote":
		if len(args) < 2 || len(args) > 3 || args[0] != expectedIP || !isPublicIPv4(args[0]) {
			return errors.New("remote must match the node public IPv4 address")
		}
		port, err := strconv.Atoi(args[1])
		if err != nil || port < 1 || port > 65535 {
			return errors.New("invalid remote port")
		}
		if len(args) == 3 && !map[string]bool{"udp": true, "udp4": true, "tcp": true, "tcp4": true, "tcp-client": true}[args[2]] {
			return errors.New("unsupported remote proto")
		}
		*remoteSeen = true
	case "resolv-retry":
		if len(args) != 1 || (args[0] != "infinite" && !isPositiveInt(args[0])) {
			return errors.New("invalid resolv-retry")
		}
	case "remote-cert-tls":
		if len(args) != 1 || args[0] != "server" {
			return errors.New("remote-cert-tls must be server")
		}
	case "cipher", "data-ciphers", "data-ciphers-fallback", "auth", "tls-cipher", "tls-ciphersuites":
		if len(args) != 1 || !safeTokenRe.MatchString(args[0]) {
			return fmt.Errorf("invalid %s", name)
		}
	case "comp-lzo":
		if len(args) > 1 || (len(args) == 1 && !map[string]bool{"yes": true, "no": true, "adaptive": true}[args[0]]) {
			return errors.New("invalid comp-lzo")
		}
	case "compress":
		if len(args) != 1 || !map[string]bool{"lz4": true, "lz4-v2": true, "stub": true, "stub-v2": true, "no": true}[args[0]] {
			return errors.New("invalid compress")
		}
	case "key-direction":
		if len(args) != 1 || (args[0] != "0" && args[0] != "1") {
			return errors.New("invalid key-direction")
		}
	case "tls-version-min":
		if len(args) != 1 || (args[0] != "1.2" && args[0] != "1.3") {
			return errors.New("invalid tls-version-min")
		}
	case "verb":
		if len(args) != 1 || !isBoundedInt(args[0], 0, 6) {
			return errors.New("invalid verb")
		}
	case "reneg-sec", "connect-timeout", "connect-retry", "sndbuf", "rcvbuf", "explicit-exit-notify":
		if len(args) != 1 || !isPositiveInt(args[0]) {
			return fmt.Errorf("invalid %s", name)
		}
	default:
		return fmt.Errorf("unsupported OpenVPN directive %q", name)
	}
	return nil
}

func isPositiveInt(s string) bool { return isBoundedInt(s, 1, 1<<31-1) }

func isBoundedInt(s string, min, max int) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n >= min && n <= max
}

func isPublicIPv4(s string) bool {
	ip := net.ParseIP(s)
	if ip == nil || ip.To4() == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	v4 := ip.To4()
	return !(v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127)
}

// readCSV locates the first line that looks like a VPNGate header before
// parsing the rest with a lenient CSV reader. The endpoint occasionally adds
// a UTF-8 BOM or an informational preamble, and mirrors may prepend their own
// metadata; anchoring on the header keeps those variants parseable.
func readCSV(raw []byte) ([][]string, error) {
	raw = bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})
	lines := bytes.Split(raw, []byte("\n"))
	start := -1
	for i, line := range lines {
		if fields := parseCSVLine(line); isVPNGateHeader(fields) {
			start = i
			break
		}
	}
	if start < 0 {
		// Preserve the old behavior for malformed payloads so Parse can return
		// a useful header error instead of failing inside the CSV reader.
		start = 0
		for i, line := range lines {
			t := bytes.TrimSpace(bytes.TrimPrefix(line, []byte{0xef, 0xbb, 0xbf}))
			if len(t) == 0 {
				continue
			}
			if t[0] == '*' {
				start = i + 1
				continue
			}
			break
		}
	}
	r := csv.NewReader(bytes.NewReader(bytes.Join(lines[start:], []byte("\n"))))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	return r.ReadAll()
}

func indexColumns(header []string) map[string]int {
	idx := make(map[string]int, len(header))
	for i, h := range header {
		name := strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(h, "\ufeff")), "#")
		if name == "" {
			continue
		}
		idx[name] = i
		if canonical := canonicalColumnName(name); canonical != "" {
			idx[canonical] = i
		}
	}
	return idx
}

func lookupColumn(columns map[string]int, key string) (int, bool) {
	i, ok := columns[canonicalColumnName(key)]
	return i, ok
}

func canonicalColumnName(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(value, "\ufeff"))
	value = strings.TrimPrefix(value, "#")
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	name := b.String()
	switch name {
	case "ddnshostname":
		return "hostname"
	case "ipaddress":
		return "ip"
	case "sessions":
		return "numvpnsessions"
	case "openvpnconfigbase64":
		return "openvpnconfigdatabase64"
	default:
		return name
	}
}

func parseCSVLine(line []byte) []string {
	r := csv.NewReader(bytes.NewReader(line))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	fields, err := r.Read()
	if err != nil {
		return nil
	}
	return fields
}

func isVPNGateHeader(fields []string) bool {
	if len(fields) == 0 {
		return false
	}
	columns := indexColumns(fields)
	_, hasIP := lookupColumn(columns, "IP")
	_, hasConfig := lookupColumn(columns, "OpenVPN_ConfigData_Base64")
	if !hasIP || !hasConfig {
		// A malformed but recognizable header should still be returned to
		// Parse so it can report the specific missing column.
		_, hasHost := lookupColumn(columns, "HostName")
		_, hasScore := lookupColumn(columns, "Score")
		return hasIP && (hasHost || hasScore)
	}
	return true
}

func findHeaderRecord(records [][]string) int {
	for i, record := range records {
		if isVPNGateHeader(record) {
			return i
		}
	}
	return -1
}

func decodeConfig(b64 string) (string, error) {
	if b64 == "" {
		return "", errors.New("empty config data")
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// parseRemote extracts the first "remote host port [proto]" line from the
// OpenVPN config; falls back to the node IP with UDP/1194.
func parseRemote(cfg string, fallbackHost string) (string, int, string) {
	m := remoteRe.FindStringSubmatch(cfg)
	if m == nil {
		return fallbackHost, 1194, "udp"
	}
	port, _ := strconv.Atoi(m[2])
	proto := m[3]
	if proto == "" {
		proto = "udp"
	}
	return m[1], port, proto
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func atoi64(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}
