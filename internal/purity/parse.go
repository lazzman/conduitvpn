package purity

import (
	"encoding/json"
	"fmt"
	"strings"
)

type widgetResponse struct {
	Data widgetData `json:"data"`
}

type widgetData struct {
	IP          string        `json:"ip"`
	City        string        `json:"city"`
	Region      string        `json:"region"`
	Country     string        `json:"country"`
	Org         string        `json:"org"`
	Postal      string        `json:"postal"`
	ASN         widgetTyped   `json:"asn"`
	Company     widgetTyped   `json:"company"`
	Privacy     widgetPrivacy `json:"privacy"`
	IsAnycast   bool          `json:"is_anycast"`
	IsMobile    bool          `json:"is_mobile"`
	IsAnonymous bool          `json:"is_anonymous"`
	IsSatellite bool          `json:"is_satellite"`
	IsHosting   bool          `json:"is_hosting"`
}

type widgetTyped struct {
	Type string `json:"type"`
}

type widgetPrivacy struct {
	VPN     bool `json:"vpn"`
	Proxy   bool `json:"proxy"`
	Tor     bool `json:"tor"`
	Relay   bool `json:"relay"`
	Hosting bool `json:"hosting"`
}

var attrOrder = []struct {
	name string
	on   func(widgetData) bool
}{
	{"vpn", func(d widgetData) bool { return d.Privacy.VPN }},
	{"proxy", func(d widgetData) bool { return d.Privacy.Proxy }},
	{"tor", func(d widgetData) bool { return d.Privacy.Tor }},
	{"relay", func(d widgetData) bool { return d.Privacy.Relay }},
	{"hosting", func(d widgetData) bool { return d.Privacy.Hosting || d.IsHosting }},
	{"mobile", func(d widgetData) bool { return d.IsMobile }},
	{"anycast", func(d widgetData) bool { return d.IsAnycast }},
	{"anonymous", func(d widgetData) bool { return d.IsAnonymous }},
	{"satellite", func(d widgetData) bool { return d.IsSatellite }},
}

// Parse maps an ipinfo widget/demo JSON body onto a Record.
func Parse(raw []byte) (Record, error) {
	var resp widgetResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return Record{}, fmt.Errorf("decode ipinfo: %w", err)
	}
	d := resp.Data
	if strings.TrimSpace(d.IP) == "" && d.Country == "" && d.ASN.Type == "" && d.Company.Type == "" {
		return Record{}, fmt.Errorf("ipinfo response missing data")
	}
	source := strings.ToLower(strings.TrimSpace(d.ASN.Type))
	if source == "" {
		source = strings.ToLower(strings.TrimSpace(d.Company.Type))
	}
	hosting := d.Privacy.Hosting || d.IsHosting || source == "hosting" || strings.ToLower(d.Company.Type) == "hosting"
	attrs := make([]string, 0, len(attrOrder))
	for _, attr := range attrOrder {
		if attr.on(d) {
			attrs = append(attrs, attr.name)
		}
	}
	return Record{
		Source:  source,
		Hosting: hosting,
		Attrs:   attrs,
		Country: strings.ToUpper(strings.TrimSpace(d.Country)),
		Postal:  strings.TrimSpace(d.Postal),
		City:    strings.TrimSpace(d.City),
		Region:  strings.TrimSpace(d.Region),
		Org:     strings.TrimSpace(d.Org),
	}, nil
}
