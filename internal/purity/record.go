package purity

// Record is the persisted lookup for one public IPv4 address.
type Record struct {
	Source    string   `json:"source,omitempty"`
	Hosting   bool     `json:"hosting"`
	Attrs     []string `json:"attrs,omitempty"`
	Country   string   `json:"country,omitempty"`
	Postal    string   `json:"postal,omitempty"`
	City      string   `json:"city,omitempty"`
	Region    string   `json:"region,omitempty"`
	Org       string   `json:"org,omitempty"`
	CheckedAt string   `json:"checked_at,omitempty"`
	Error     string   `json:"error,omitempty"`
}

// Info is the sanitized view attached to a node in the API.
type Info struct {
	Status  string   `json:"status"`
	Source  string   `json:"source,omitempty"`
	Hosting bool     `json:"hosting,omitempty"`
	Attrs   []string `json:"attrs,omitempty"`
	Country string   `json:"country,omitempty"`
	Postal  string   `json:"postal,omitempty"`
	City    string   `json:"city,omitempty"`
	Region  string   `json:"region,omitempty"`
	Org     string   `json:"org,omitempty"`
}

const (
	StatusPending = "pending"
	StatusOK      = "ok"
	StatusError   = "error"
)

// View returns the API payload for a cached record.
func (r Record) View() Info {
	status := StatusOK
	if r.Error != "" {
		status = StatusError
	}
	return Info{
		Status:  status,
		Source:  r.Source,
		Hosting: r.Hosting,
		Attrs:   append([]string(nil), r.Attrs...),
		Country: r.Country,
		Postal:  r.Postal,
		City:    r.City,
		Region:  r.Region,
		Org:     r.Org,
	}
}

// PendingInfo is used when an IP has not been looked up yet.
func PendingInfo() Info {
	return Info{Status: StatusPending}
}
