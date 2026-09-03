package vpngate

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestParseFindsHeaderAfterPreamble(t *testing.T) {
	config := base64.StdEncoding.EncodeToString([]byte(safeProfile))
	raw := "\ufeff[VPNGate mirror export]\r\n" +
		"20260903_023602\r\n" +
		"*vpn_servers\r\n" +
		"\ufeff#Hostname,IP,Score,Ping,Speed,CountryLong,CountryShort,NumVpnSessions,Uptime,TotalUsers,TotalTraffic,LogType,Operator,Message,OpenVPN_ConfigData_Base64\r\n" +
		"vpn.example,8.8.8.8,100,10,1000,United States,US,2,30,3,4,2weeks,Operator,," + config + "\r\n*END\r\n"

	nodes, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("node count = %d, want 1", len(nodes))
	}
	if got := nodes[0].HostName; got != "vpn.example" {
		t.Errorf("HostName = %q, want vpn.example", got)
	}
	if got := nodes[0].Sessions; got != 2 {
		t.Errorf("Sessions = %d, want 2", got)
	}
}

func TestParseUsesIPWhenMirrorOmitsHostName(t *testing.T) {
	config := base64.StdEncoding.EncodeToString([]byte(safeProfile))
	raw := "*vpn_servers\n" +
		"#IP,Score,Ping,Speed,CountryLong,CountryShort,NumVpnSessions,Uptime,LogType,Operator,Message,OpenVPN_ConfigData_Base64\n" +
		"8.8.8.8,100,10,1000,United States,US,2,30,2weeks,Operator,," + config + "\n"

	nodes, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("node count = %d, want 1", len(nodes))
	}
	if got := nodes[0].HostName; got != "8.8.8.8" {
		t.Errorf("HostName fallback = %q, want node IP", got)
	}
}

func TestParseAcceptsColumnNameVariants(t *testing.T) {
	config := base64.StdEncoding.EncodeToString([]byte(safeProfile))
	raw := "#DDNS_HostName,IPAddress,Score,Ping,Speed,CountryLong,CountryShort,Sessions,Uptime,LogType,Operator,Message,OpenVPNConfigBase64\n" +
		"vpn.example,8.8.8.8,100,10,1000,United States,US,2,30,2weeks,Operator,," + config + "\n"

	nodes, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(nodes) != 1 || nodes[0].HostName != "vpn.example" {
		t.Fatalf("nodes = %#v, want one named node", nodes)
	}
}

func TestParseRejectsPayloadWithoutVPNGateHeader(t *testing.T) {
	_, err := Parse([]byte("<!doctype html><html><body>upstream error</body></html>"))
	if err == nil || !strings.Contains(err.Error(), "CSV header") {
		t.Fatalf("Parse() error = %v, want missing VPNGate CSV header", err)
	}
}
