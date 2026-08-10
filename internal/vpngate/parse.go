package vpngate

import (
	"bytes"
	"encoding/base64"
	"encoding/csv"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"aimilivpn/internal/node"
)

// VPNGate API layout: a leading "*vpn_servers" line, a "#HostName,IP,..."
// CSV header, then data rows. The last column is the base64-encoded
// OpenVPN config of the node.
var remoteRe = regexp.MustCompile(`(?m)^remote\s+(\S+)\s+(\d+)(?:\s+(udp|tcp))?`)

func Parse(raw []byte) ([]*node.Node, error) {
	records, err := readCSV(raw)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, errors.New("empty csv payload")
	}

	col := indexColumns(records[0])
	need := []string{
		"HostName", "IP", "Score", "Ping", "Speed",
		"CountryLong", "CountryShort", "NumVpnSessions", "Uptime",
		"LogType", "Operator", "Message", "OpenVPN_ConfigData_Base64",
	}
	for _, k := range need {
		if _, ok := col[k]; !ok {
			return nil, fmt.Errorf("missing csv column %q", k)
		}
	}
	get := func(row []string, key string) string {
		i, ok := col[key]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}

	var nodes []*node.Node
	for _, row := range records[1:] {
		if len(row) == 0 || strings.HasPrefix(row[0], "*") {
			continue // trailing "*END" etc.
		}
		cfgText, err := decodeConfig(get(row, "OpenVPN_ConfigData_Base64"))
		if err != nil {
			continue // skip rows with corrupt configs
		}
		n := &node.Node{
			HostName:     get(row, "HostName"),
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
		n.RemoteHost, n.RemotePort, n.RemoteProto = parseRemote(cfgText, n.IP)
		nodes = append(nodes, n)
	}
	return nodes, nil
}

// readCSV strips the leading "*vpn_servers" comment line and parses the
// rest with a lenient CSV reader (LazyQuotes handles odd quoting in
// operator/message fields).
func readCSV(raw []byte) ([][]string, error) {
	lines := bytes.Split(raw, []byte("\n"))
	start := 0
	for i, line := range lines {
		t := bytes.TrimSpace(line)
		if len(t) > 0 {
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
		name := strings.TrimPrefix(strings.TrimSpace(h), "#")
		idx[name] = i
	}
	return idx
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
