package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDashboardSharing(t *testing.T) {
	a := testApp(t)
	for _, view := range []string{"modern", "kindle"} {
		r := httptest.NewRequest("GET", "http://192.168.1.2:18888/api/v1/qrcode?view="+view, nil)
		w := httptest.NewRecorder()
		a.qrcodePage(w, r)
		img, err := png.Decode(bytes.NewReader(w.Body.Bytes()))
		if err != nil || w.Code != 200 || img.Bounds().Dx() != 256 || img.Bounds().Dy() != 256 {
			t.Fatal("QR image", view, err)
		}
		want := "http://192.168.1.2:18888/?mode=mobile"
		if view == "kindle" {
			want = "http://192.168.1.2:18888/basic"
		}
		if dashboardURL(r, view) != want {
			t.Fatal("entry URL")
		}
		r.TLS = &tls.ConnectionState{}
		if !strings.HasPrefix(dashboardURL(r, view), "https://") {
			t.Fatal("TLS entry")
		}
	}
	r := httptest.NewRequest("GET", "http://127.0.0.1/api/v1/qrcode?view=https://attacker.test", nil)
	w := httptest.NewRecorder()
	a.qrcodePage(w, r)
	if w.Code != 400 {
		t.Fatal("arbitrary QR target allowed")
	}
	r = httptest.NewRequest("GET", "http://attacker.test/api/v1/qrcode?view=modern", nil)
	r.RemoteAddr = "192.168.1.3:60000"
	w = httptest.NewRecorder()
	a.webHandler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("untrusted Host allowed")
	}
}

func TestDetailedAddressAndSearchCache(t *testing.T) {
	a := testApp(t)
	if _, err := a.searchAddresses(context.Background(), "测试地址"); err == nil {
		t.Fatal("must be disabled by default")
	}
	c := City{Name: "地址示例", Address: "测试省测试市示例路 123 号", Latitude: 31.23, Longitude: 121.47, Timezone: "Asia/Shanghai"}
	if err := a.store.Update(func(conf *Config) error { conf.City = &c; return nil }); err != nil {
		t.Fatal(err)
	}
	reopened, err := openStore(a.store.dir, a.store.dataDir)
	if err != nil || reopened.Get().City.Address != c.Address {
		t.Fatal("address persistence", err)
	}
	calls := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("format") != "jsonv2" || r.Header.Get("User-Agent") == "" {
			t.Error("provider protocol")
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{"name": "示例路", "display_name": "测试市，示例路 123 号", "lat": "31.23", "lon": "121.47", "address": map[string]string{"city": "测试市", "country": "测试国"}}})
	}))
	defer provider.Close()
	if err := a.store.Update(func(conf *Config) error { conf.GeocoderURL = provider.URL; return nil }); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		matches, err := a.searchAddresses(context.Background(), "测试地址")
		if err != nil || len(matches) != 1 || matches[0].Timezone != "" || matches[0].AddressSource != "osm" || matches[0].Name != "测试市" {
			t.Fatal("address parsing", matches, err)
		}
	}
	if calls != 1 {
		t.Fatal("cache missed", calls)
	}
	if _, err := a.searchAddresses(context.Background(), "另一测试地址"); err == nil {
		t.Fatal("rate limit missing")
	}
	for _, bad := range []string{"http://public.example/search", "https://user:password@example.com/search", "https://example.com/search?token=secret"} {
		if a.store.Update(func(conf *Config) error { conf.GeocoderURL = bad; return nil }) == nil {
			t.Fatal("unsafe provider accepted", bad)
		}
	}
}
