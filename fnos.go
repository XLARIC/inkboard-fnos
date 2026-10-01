package main

import (
	"embed"
	"encoding/json"
	"net"
)

//go:embed packaging/fnos/wizard/config
var wizardFiles embed.FS

//go:embed scripts/install-collector.sh
var collectorInstallScript []byte

func desktopEntry(c Config) any {
	_, port, _ := net.SplitHostPort(c.Listen)
	entries := map[string]any{}
	for _, v := range [][3]string{{"Application", "InkBoard 看板", "/"}, {"Admin", "InkBoard 管理", "/admin"}} {
		entries["inkboard-fnos."+v[0]] = map[string]any{"title": v[1], "icon": "images/icon_{0}.png", "type": "url", "protocol": "http", "port": port, "url": v[2], "allUsers": false}
	}
	return map[string]any{".url": entries}
}
func configuredWizard(c Config) ([]byte, error) {
	raw, e := wizardFiles.ReadFile("packaging/fnos/wizard/config")
	if e != nil {
		return nil, e
	}
	var steps []map[string]any
	if e = json.Unmarshal(raw, &steps); e != nil {
		return nil, e
	}
	host, port, _ := net.SplitHostPort(c.Listen)
	for _, step := range steps {
		for _, item := range step["items"].([]any) {
			field := item.(map[string]any)
			switch field["field"] {
			case "wizard_role":
				field["initValue"] = c.Role
			case "wizard_port":
				field["initValue"] = port
			case "wizard_listen":
				field["initValue"] = host
			}
		}
	}
	return json.MarshalIndent(steps, "", "  ")
}
