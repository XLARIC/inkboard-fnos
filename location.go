package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func isPrivateIP(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsPrivate() || ip.IsLoopback())
}

type addressCache struct {
	SchemaVersion int       `json:"schema_version"`
	FetchedAt     time.Time `json:"fetched_at"`
	Cities        []City    `json:"cities"`
}

func (a *App) searchAddresses(ctx context.Context, query string) ([]City, error) {
	provider := a.config().GeocoderURL
	if provider == "" {
		return nil, errors.New("尚未配置地址搜索服务；可直接填写详细地址、经纬度和时区")
	}
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 600 {
		return nil, errors.New("请输入有效地址（最多 600 字节）")
	}
	key := sha256.Sum256([]byte(provider + "\x00" + query))
	path := filepath.Join(a.store.dataDir, "addresses", hex.EncodeToString(key[:])+".json")
	var cache addressCache
	if raw, err := os.ReadFile(path); err == nil && json.Unmarshal(raw, &cache) == nil && cache.SchemaVersion == 1 && time.Since(cache.FetchedAt) >= 0 && time.Since(cache.FetchedAt) < 30*24*time.Hour {
		return cache.Cities, nil
	}
	a.mu.Lock()
	if time.Now().Before(a.nextGeocoding) {
		a.mu.Unlock()
		return nil, errors.New("地址搜索每秒最多一次，请稍后再试")
	}
	a.nextGeocoding = time.Now().Add(time.Second)
	a.mu.Unlock()
	u, err := url.Parse(provider)
	if err != nil {
		return nil, errors.New("地址服务配置无效")
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("format", "jsonv2")
	q.Set("addressdetails", "1")
	q.Set("limit", "5")
	q.Set("accept-language", "zh-CN,en")
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, errors.New("地址请求无效")
	}
	req.Header.Set("User-Agent", "InkBoard/"+version+" (+https://github.com/XLARIC/inkboard-fnos)")
	client := *a.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		return nil, errors.New("地址服务连接失败，请检查配置")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, errors.New("地址服务返回错误，请检查服务状态")
	}
	var matches []struct {
		Name        string                                        `json:"name"`
		DisplayName string                                        `json:"display_name"`
		Lat         string                                        `json:"lat"`
		Lon         string                                        `json:"lon"`
		Address     struct{ City, Town, Village, Country string } `json:"address"`
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 2*1024*1024+1))
	if err != nil || len(raw) > 2*1024*1024 || json.Unmarshal(raw, &matches) != nil {
		return nil, errors.New("地址服务响应无效")
	}
	cities := []City{}
	for _, m := range matches {
		lat, e1 := strconv.ParseFloat(m.Lat, 64)
		lon, e2 := strconv.ParseFloat(m.Lon, 64)
		name := m.Address.City
		for _, value := range []string{m.Address.Town, m.Address.Village, m.Name} {
			if name == "" {
				name = value
			}
		}
		if name == "" {
			name = "所选地址"
		}
		c := City{Name: name, Address: m.DisplayName, AddressSource: "osm", Country: m.Address.Country, Latitude: lat, Longitude: lon, Timezone: "UTC"}
		if e1 == nil && e2 == nil && validateCity(c) == nil {
			c.Timezone = ""
			cities = append(cities, c)
		}
		if len(cities) == 5 {
			break
		}
	}
	cache = addressCache{SchemaVersion: 1, FetchedAt: time.Now().UTC(), Cities: cities}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err == nil {
		_ = atomicJSON(path, cache)
	}
	return cities, nil
}
