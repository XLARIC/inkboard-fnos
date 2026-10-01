package main

import "time"

const version = "0.1.0-alpha.2"
const schemaVersion = 1

type Metric struct {
	Value  *float64 `json:"value"`
	Unit   string   `json:"unit"`
	Status string   `json:"status"`
	Reason string   `json:"reason,omitempty"`
}

func measured(v float64, unit string) Metric { return Metric{Value: &v, Unit: unit, Status: "ok"} }
func unavailable(unit, reason string) Metric {
	return Metric{Unit: unit, Status: "unavailable", Reason: reason}
}

type CPU struct {
	Model       string `json:"model"`
	Cores       int    `json:"cores"`
	Usage       Metric `json:"usage"`
	Temperature Metric `json:"temperature"`
}
type Memory struct {
	Total uint64 `json:"total"`
	Used  uint64 `json:"used"`
	Usage Metric `json:"usage"`
}
type GPU struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Usage       Metric `json:"usage"`
	Temperature Metric `json:"temperature"`
	MemoryUsed  Metric `json:"memory_used"`
	MemoryTotal Metric `json:"memory_total"`
}
type Disk struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Model        string     `json:"model"`
	Kind         string     `json:"kind"`
	Interface    string     `json:"interface"`
	External     bool       `json:"external"`
	Size         uint64     `json:"size"`
	Temperature  Metric     `json:"temperature"`
	Health       string     `json:"health"`
	HealthReason string     `json:"health_reason,omitempty"`
	CheckedAt    *time.Time `json:"checked_at,omitempty"`
}
type Volume struct {
	Filesystem string `json:"filesystem"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	Total      uint64 `json:"total"`
	Used       uint64 `json:"used"`
	Usage      Metric `json:"usage"`
}
type Traffic struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Read  Metric `json:"read"`
	Write Metric `json:"write"`
}
type Sensor struct {
	Name  string `json:"name"`
	Value Metric `json:"value"`
}
type Snapshot struct {
	SchemaVersion int               `json:"schema_version"`
	Version       string            `json:"version"`
	ObservedAt    time.Time         `json:"observed_at"`
	Hostname      string            `json:"hostname"`
	Platform      string            `json:"platform"`
	CPU           CPU               `json:"cpu"`
	Memory        Memory            `json:"memory"`
	GPUs          []GPU             `json:"gpus"`
	Disks         []Disk            `json:"disks"`
	Volumes       []Volume          `json:"volumes"`
	DiskIO        []Traffic         `json:"disk_io"`
	Network       []Traffic         `json:"network"`
	Sensors       []Sensor          `json:"sensors"`
	Uptime        Metric            `json:"uptime"`
	Capabilities  map[string]string `json:"capabilities"`
}
type NASView struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Status       string     `json:"status"`
	Reason       string     `json:"reason,omitempty"`
	LastReceived *time.Time `json:"last_received,omitempty"`
	Snapshot     *Snapshot  `json:"snapshot"`
}
type City struct {
	Name          string  `json:"name"`
	Address       string  `json:"address,omitempty"`
	AddressSource string  `json:"address_source,omitempty"`
	Country       string  `json:"country,omitempty"`
	Latitude      float64 `json:"latitude"`
	Longitude     float64 `json:"longitude"`
	Timezone      string  `json:"timezone"`
}
type NASConfig struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Endpoint    string `json:"endpoint"`
	Fingerprint string `json:"fingerprint"`
	Secret      string `json:"encrypted_secret,omitempty"`
}
type Config struct {
	SchemaVersion int         `json:"schema_version"`
	Role          string      `json:"role"`
	Listen        string      `json:"listen"`
	AgentListen   string      `json:"agent_listen"`
	AllowedCIDRs  []string    `json:"allowed_cidrs"`
	City          *City       `json:"city"`
	ClockCities   []ClockCity `json:"clock_cities"`
	GeocoderURL   string      `json:"geocoder_url,omitempty"`
	MonitorLocal  bool        `json:"monitor_local"`
	LocalName     string      `json:"local_name"`
	NAS           []NASConfig `json:"nas"`
	PasswordHash  string      `json:"password_hash,omitempty"`
	AgentSecret   string      `json:"encrypted_agent_secret,omitempty"`
	HTTPSListen   string      `json:"https_listen,omitempty"`
	HTTPSCert     string      `json:"https_cert,omitempty"`
	HTTPSKey      string      `json:"https_key,omitempty"`
}
type ClockCity struct {
	Name     string `json:"name"`
	Timezone string `json:"timezone"`
}
type Conditions struct {
	Time          time.Time `json:"time"`
	Temperature   *float64  `json:"temperature"`
	Apparent      *float64  `json:"apparent"`
	Humidity      *float64  `json:"humidity"`
	Wind          *float64  `json:"wind"`
	Direction     *float64  `json:"direction"`
	Gusts         *float64  `json:"gusts"`
	Precipitation *float64  `json:"precipitation"`
	Probability   *float64  `json:"probability"`
	Pressure      *float64  `json:"pressure"`
	Code          int       `json:"code"`
}
type Day struct {
	Date          string   `json:"date"`
	Minimum       *float64 `json:"minimum"`
	Maximum       *float64 `json:"maximum"`
	Probability   *float64 `json:"probability"`
	Precipitation *float64 `json:"precipitation"`
	Wind          *float64 `json:"wind"`
	UV            *float64 `json:"uv"`
	Sunrise       string   `json:"sunrise"`
	Sunset        string   `json:"sunset"`
	Code          int      `json:"code"`
}
type Weather struct {
	SchemaVersion int          `json:"schema_version"`
	City          City         `json:"city"`
	FetchedAt     time.Time    `json:"fetched_at"`
	Current       Conditions   `json:"current"`
	Hourly        []Conditions `json:"hourly"`
	Days          []Day        `json:"days"`
	Error         string       `json:"error,omitempty"`
}
type Clock struct {
	Name       string `json:"name"`
	Zone       string `json:"zone"`
	Date       string `json:"date"`
	Weekday    string `json:"weekday"`
	Time       string `json:"time"`
	Difference string `json:"difference"`
	Offset     int    `json:"offset"`
}
type Display struct {
	WeatherStatus string    `json:"weather_status,omitempty"`
	SchemaVersion int       `json:"schema_version"`
	Version       string    `json:"version"`
	Now           time.Time `json:"now"`
	Role          string    `json:"role"`
	Demo          bool      `json:"demo"`
	City          *City     `json:"city"`
	Clocks        []Clock   `json:"clocks"`
	Weather       *Weather  `json:"weather"`
	NAS           []NASView `json:"nas"`
}
