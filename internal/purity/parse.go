package purity

import (
	"encoding/json"
	"fmt"
	"strings"
)

type apiResponse struct {
	Status      string `json:"status"`
	Message     string `json:"message"`
	Query       string `json:"query"`
	Country     string `json:"country"`
	CountryCode string `json:"countryCode"`
	RegionName  string `json:"regionName"`
	City        string `json:"city"`
	Zip         string `json:"zip"`
	ISP         string `json:"isp"`
	Org         string `json:"org"`
	AS          string `json:"as"`
	ASName      string `json:"asname"`
	Mobile      bool   `json:"mobile"`
	Proxy       bool   `json:"proxy"`
	Hosting     bool   `json:"hosting"`
}

// Parse maps an ip-api.com JSON body onto a Record.
func Parse(raw []byte) (Record, error) {
	var d apiResponse
	if err := json.Unmarshal(raw, &d); err != nil {
		return Record{}, fmt.Errorf("decode ip-api: %w", err)
	}
	if strings.EqualFold(strings.TrimSpace(d.Status), "fail") {
		msg := strings.TrimSpace(d.Message)
		if msg == "" {
			msg = "lookup failed"
		}
		return Record{}, fmt.Errorf("ip-api: %s", msg)
	}
	if strings.TrimSpace(d.Query) == "" && d.CountryCode == "" && d.ISP == "" && d.Org == "" && d.AS == "" {
		return Record{}, fmt.Errorf("ip-api response missing data")
	}
	source := "isp"
	if d.Hosting {
		source = "hosting"
	}
	attrs := make([]string, 0, 3)
	if d.Proxy {
		attrs = append(attrs, "proxy")
	}
	if d.Hosting {
		attrs = append(attrs, "hosting")
	}
	if d.Mobile {
		attrs = append(attrs, "mobile")
	}
	org := strings.TrimSpace(d.Org)
	if org == "" {
		org = strings.TrimSpace(d.ISP)
	}
	if org == "" {
		org = strings.TrimSpace(d.AS)
	}
	return Record{
		Source:  source,
		Hosting: d.Hosting,
		Attrs:   attrs,
		Country: strings.ToUpper(strings.TrimSpace(d.CountryCode)),
		Postal:  strings.TrimSpace(d.Zip),
		City:    strings.TrimSpace(d.City),
		Region:  strings.TrimSpace(d.RegionName),
		Org:     org,
	}, nil
}
