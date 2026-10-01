package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	root := t.TempDir()
	s, e := openStore(filepath.Join(root, "config"), filepath.Join(root, "data"))
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func testApp(t *testing.T) *App {
	t.Helper()
	s := testStore(t)
	a, e := newApp(s, s.Get(), false, "")
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func TestConfigAndSecret(t *testing.T) {
	s := testStore(t)
	secret := "private-pair-secret"
	encrypted, e := s.Seal(secret)
	if e != nil {
		t.Fatal(e)
	}
	plain, e := s.Open(encrypted)
	if e != nil || plain != secret {
		t.Fatal("secret roundtrip")
	}
	if strings.Contains(encrypted, secret) {
		t.Fatal("plaintext secret")
	}
	for _, cidr := range []string{"0.0.0.0/0", "10.0.0.0/1", "172.16.0.0/8", "192.168.0.0/8", "127.0.0.0/1", "8.8.8.0/24", "fc00::/1"} {
		c := s.Get()
		c.AllowedCIDRs = []string{cidr}
		if validateConfig(c) == nil {
			t.Errorf("accepted unsafe CIDR %s", cidr)
		}
	}
	before := s.Get()
	if s.Update(func(c *Config) error { c.Role = "invalid"; return nil }) == nil {
		t.Fatal("invalid config saved")
	}
	if s.Get().Role != before.Role {
		t.Fatal("rollback")
	}
	reopened, e := openStore(s.dir, s.dataDir)
	if e != nil || reopened.Get().SchemaVersion != 1 {
		t.Fatal("restart persistence")
	}
}
func TestDefaultEmptyNASAndConfiguredClocks(t *testing.T) {
	s := testStore(t)
	a, e := newApp(s, s.Get(), false, "")
	if e != nil {
		t.Fatal(e)
	}
	if s.Get().MonitorLocal || len(a.display().NAS) != 0 {
		t.Fatal("monitoring must require explicit addition")
	}
	city := City{Name: "上海", Timezone: "Asia/Shanghai"}
	custom := []ClockCity{{"伦敦", "Europe/London"}, {"加德满都", "Asia/Kathmandu"}}
	if e = s.Update(func(c *Config) error { c.City = &city; c.ClockCities = custom; return nil }); e != nil {
		t.Fatal(e)
	}
	d := a.display()
	if len(d.Clocks) != 3 || d.Clocks[1].Name != "伦敦" || d.Clocks[2].Difference != "比本地慢 2 小时 15 分" {
		t.Fatal("configured clocks", d.Clocks)
	}
	if e = s.Update(func(c *Config) error { c.ClockCities = []ClockCity{}; return nil }); e != nil {
		t.Fatal(e)
	}
	reopened, e := openStore(s.dir, s.dataDir)
	if e != nil {
		t.Fatal(e)
	}
	if len(configuredClocks(time.Now(), reopened.Get().City, reopened.Get().ClockCities)) != 1 {
		t.Fatal("empty clock list must survive restart")
	}
	if s.Update(func(c *Config) error { c.ClockCities = []ClockCity{{"bad", "Invalid/Zone"}}; return nil }) == nil {
		t.Fatal("invalid clock zone accepted")
	}
}
func TestPassword(t *testing.T) {
	h, e := hashPassword("safe-testing-password")
	if e != nil || !checkPassword("safe-testing-password", h) || checkPassword("wrong-password", h) {
		t.Fatal("password verification")
	}
	if _, e = hashPassword("short"); e == nil {
		t.Fatal("weak password")
	}
}
func TestClockDatesAndDST(t *testing.T) {
	city := City{Name: "纽约", Timezone: "America/New_York"}
	winter := clocksAt(time.Date(2026, 1, 15, 4, 30, 0, 0, time.UTC), &city)
	summer := clocksAt(time.Date(2026, 7, 15, 4, 30, 0, 0, time.UTC), &city)
	if winter[0].Date != "2026-01-14" || summer[0].Date != "2026-07-15" {
		t.Fatal("cross-date time")
	}
	if winter[2].Difference != "比本地快 13 小时" || summer[2].Difference != "比本地快 12 小时" {
		t.Fatal("DST offset")
	}
	c := City{Name: "加德满都", Timezone: "Asia/Kathmandu"}
	if clocksAt(time.Now(), &c)[2].Difference != "比本地快 2 小时 15 分" {
		t.Fatal("fractional timezone")
	}
}
func TestHourlyDSTDays(t *testing.T) {
	loc, _ := time.LoadLocation("America/New_York")
	for _, tc := range []struct {
		date  string
		count int
	}{{"2026-03-08", 23}, {"2026-11-01", 25}} {
		start, _ := time.ParseInLocation("2006-01-02", tc.date, loc)
		w := &Weather{City: City{Timezone: "America/New_York"}}
		for stamp := start; stamp.Before(start.AddDate(0, 0, 1)); stamp = stamp.Add(time.Hour) {
			w.Hourly = append(w.Hourly, Conditions{Time: stamp.UTC()})
		}
		if len(dayHours(w, tc.date)) != tc.count {
			t.Fatalf("%s count", tc.date)
		}
	}
}
func TestWeatherNullAnd17Days(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	c := City{Name: "纽约", Timezone: "America/New_York"}
	loc, _ := time.LoadLocation(c.Timezone)
	start := time.Date(2026, 10, 31, 0, 0, 0, 0, loc)
	r := omResponse{Current: map[string]json.RawMessage{"temperature_2m": json.RawMessage("15"), "time": json.RawMessage(fmt.Sprint(now.Unix()))}, Hourly: map[string][]json.RawMessage{}, Daily: map[string][]json.RawMessage{}}
	for i := 0; i < 17; i++ {
		r.Daily["time"] = append(r.Daily["time"], json.RawMessage(fmt.Sprint(start.AddDate(0, 0, i).Unix())))
	}
	for ts := start; ts.Before(start.AddDate(0, 0, 17)); ts = ts.Add(time.Hour) {
		r.Hourly["time"] = append(r.Hourly["time"], json.RawMessage(fmt.Sprint(ts.Unix())))
	}
	w, e := normalizeWeather(r, c, now)
	if e != nil {
		t.Fatal(e)
	}
	if len(w.Days) != 17 || w.Days[0].Date != "2026-10-31" || w.Days[16].Date != "2026-11-16" {
		t.Fatal("civil dates")
	}
	if w.Days[0].UV != nil || w.Current.Humidity != nil {
		t.Fatal("null became zero")
	}
	if len(dayHours(w, "2026-11-01")) != 25 {
		t.Fatal("DST hourly normalization")
	}
}
func TestCounters(t *testing.T) {
	total, idle := cpuCounters("cpu  100 20 30 400 50 5 6 7 90 100")
	if total != 618 || idle != 450 {
		t.Fatal(total, idle)
	}
	m := cpuPercent([2]uint64{100, 40}, [2]uint64{200, 90})
	if m.Value == nil || *m.Value != 50 {
		t.Fatal("CPU delta")
	}
	if cpuPercent([2]uint64{200, 100}, [2]uint64{100, 50}).Value != nil {
		t.Fatal("reset")
	}
	memory := parseMemory("MemTotal: 1000 kB\nMemAvailable: 400 kB")
	if memory.Used != 600*1024 || *memory.Usage.Value != 60 {
		t.Fatal("memory")
	}
	io := parseDiskIO("8 0 sda 1 0 20 0 1 0 30 0 0 0 0")
	if io["sda"].Read != 20*512 || io["sda"].Write != 30*512 {
		t.Fatal("sectors")
	}
	net := parseNetwork("eth0: 1000 0 0 0 0 0 0 0 2000 0")
	if net["eth0"].Write != 2000 {
		t.Fatal("network")
	}
	r, w := rate(counter{100, 200}, counter{200, 400}, 5)
	if *r.Value != 20 || *w.Value != 40 {
		t.Fatal("rate")
	}
	if x, _ := rate(counter{100, 200}, counter{1, 1}, 5); x.Value != nil {
		t.Fatal("network reset")
	}
}
func TestPhysicalDisksSeparateFromPartitions(t *testing.T) {
	root := t.TempDir()
	write := func(p, s string) {
		t.Helper()
		p = filepath.Join(root, p)
		if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(p, []byte(s), 0600); e != nil {
			t.Fatal(e)
		}
	}
	for _, n := range []string{"sda", "sdb", "sdc", "nvme0n1", "nvme1n1", "sda1", "nvme0n1p1", "loop0", "md0", "dm-0"} {
		write("/sys/class/block/"+n+"/size", "1000")
	}
	write("/sys/class/block/sda1/partition", "1")
	write("/sys/class/block/nvme0n1p1/partition", "1")
	c := newCollector(root)
	if len(c.disks(time.Now())) != 5 {
		t.Fatal("physical disks include system/unallocated, exclude partitions/RAID")
	}
}
func TestPinnedTLSAndAuth(t *testing.T) {
	s := Snapshot{SchemaVersion: 1, ObservedAt: time.Now()}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer valid-token" {
			http.Error(w, "", 401)
			return
		}
		writeJSON(w, s)
	}))
	defer server.Close()
	cert := server.Certificate()
	sum := sha256.Sum256(cert.Raw)
	n := NASConfig{Endpoint: server.URL, Fingerprint: hex.EncodeToString(sum[:])}
	if _, e := fetchSnapshot(context.Background(), n, "valid-token"); e != nil {
		t.Fatal(e)
	}
	if _, e := fetchSnapshot(context.Background(), n, "wrong-token"); e != errAgentAuth {
		t.Fatal("wrong token accepted")
	}
	n.Fingerprint = strings.Repeat("0", 64)
	if _, e := fetchSnapshot(context.Background(), n, "valid-token"); e == nil {
		t.Fatal("wrong certificate accepted")
	}
	for _, address := range []string{"http://192.168.0.1:1234", "https://8.8.8.8:1234", "https://localhost:1234", "https://192.168.0.1:1234/path", "https://user:pass@192.168.0.1:1234", "https://192.168.0.1:80"} {
		if validateAgentURL(address) == nil {
			t.Fatal("unsafe endpoint", address)
		}
	}
}
func TestAdminAuthCSRFAndLAN(t *testing.T) {
	a := testApp(t)
	h := a.webHandler()
	request := func(method, path, body string, cookie *http.Cookie, csrf, origin, peer string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.RemoteAddr = peer
		r.Host = "192.168.0.1:18888"
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", csrf)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if request("GET", "/api/admin/config", "", nil, "", "", "192.168.0.2:1234").Code != 401 {
		t.Fatal("unauthenticated admin")
	}
	if request("GET", "/api/v1/display", "", nil, "", "", "8.8.8.8:1234").Code != 403 {
		t.Fatal("public peer")
	}
	login, _ := json.Marshal(map[string]string{"Password": "safe-testing-password", "SetupToken": a.bootstrap})
	w := request("POST", "/api/admin/login", string(login), nil, "", "", "192.168.0.2:1234")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("cookie flags")
	}
	var sess session
	_ = json.Unmarshal(w.Body.Bytes(), &sess)
	body := "{\"Enabled\":false,\"Name\":\"test\"}"
	if request("POST", "/api/admin/local", body, cookies[0], "bad", "http://192.168.0.1:18888", "192.168.0.2:1234").Code != 403 {
		t.Fatal("CSRF bypass")
	}
	if request("POST", "/api/admin/local", body, cookies[0], sess.CSRF, "https://evil.invalid", "192.168.0.2:1234").Code != 403 {
		t.Fatal("origin bypass")
	}
	if request("POST", "/api/admin/local", body, cookies[0], sess.CSRF, "http://192.168.0.1:18888", "192.168.0.2:1234").Code != 200 {
		t.Fatal("valid update")
	}
	config := request("GET", "/api/admin/config", "", cookies[0], "", "", "192.168.0.2:1234")
	if strings.Contains(config.Body.String(), "pbkdf2$") || strings.Contains(config.Body.String(), a.token) {
		t.Fatal("secret leak")
	}
}
func TestOfflineSnapshotAndBackoff(t *testing.T) {
	a := testApp(t)
	s := a.store
	if e := s.Update(func(c *Config) error {
		c.NAS = []NASConfig{{ID: "high", Name: "高配", Endpoint: "https://127.0.0.1:19999"}}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	a.nas["high"] = NASView{Status: "offline", Snapshot: &Snapshot{SchemaVersion: 1, CPU: CPU{Usage: measured(67, "%")}}}
	d := a.display()
	html := string(a.groups(d, false)[1].Blocks[1].Content)
	if !strings.Contains(html, "CPU —") || strings.Contains(html, "67") {
		t.Fatal("historical live value")
	}
	if retryDelay(3) != 15*time.Second || retryDelay(4) != 30*time.Second || retryDelay(5) != 60*time.Second || retryDelay(9) != 120*time.Second {
		t.Fatal("retry schedule")
	}
}
func TestHTMLRoutesAndDemo(t *testing.T) {
	s := testStore(t)
	a, e := newApp(s, s.Get(), true, "")
	if e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"/", "/basic", "/basic?main=local", "/basic?main=powerful&history=1", "/hourly?date=" + time.Now().In(mustLocation("Pacific/Auckland")).Format("2006-01-02"), "/device", "/admin", "/assets/dashboard.js", "/assets/console.css", "/install/collector.sh", "/fragments"} {
		r := httptest.NewRequest("GET", path, nil)
		r.RemoteAddr = "127.0.0.1:9999"
		r.Host = "127.0.0.1:18888"
		w := httptest.NewRecorder()
		a.webHandler().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Error(path, w.Code, w.Body.String())
		}
	}
	d := a.display()
	if !d.Demo || len(d.Weather.Days) != 17 || len(d.NAS) != 2 {
		t.Fatal("demo")
	}
}
func mustLocation(s string) *time.Location { l, _ := time.LoadLocation(s); return l }
func TestLiveWeather(t *testing.T) {
	if os.Getenv("INKBOARD_LIVE_WEATHER") != "1" {
		t.Skip("explicit live integration test")
	}
	c := City{Name: "奥克兰", Latitude: -36.85, Longitude: 174.76, Timezone: "Pacific/Auckland"}
	w, e := fetchWeather(context.Background(), &http.Client{Timeout: 20 * time.Second}, c, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if len(w.Days) != 17 || len(w.Hourly) < 391 || w.Current.Temperature == nil {
		t.Fatal("incomplete real weather response")
	}
	located, e := locateCity(context.Background(), &http.Client{Timeout: 20 * time.Second}, c.Latitude, c.Longitude)
	if e != nil || located.Timezone != c.Timezone {
		t.Fatal("location timezone", located, e)
	}
	t.Logf("Open-Meteo: %d dates, %d hourly rows; current timestamp %s", len(w.Days), len(w.Hourly), w.Current.Time)
}
func TestPollingOfflineRecoveryAndAuthentication(t *testing.T) {
	a := testApp(t)
	token := "valid-poll-token"
	mode := "ok"
	snap := Snapshot{SchemaVersion: 1, ObservedAt: time.Now()}
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch mode {
		case "fail":
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
		case "protocol":
			http.Error(w, "", 503)
		case "auth":
			http.Error(w, "", 401)
		default:
			writeJSON(w, snap)
		}
	}))
	defer remote.Close()
	pin := sha256.Sum256(remote.Certificate().Raw)
	sealed, _ := a.store.Seal(token)
	n := NASConfig{ID: "remote", Name: "远端", Endpoint: remote.URL, Fingerprint: hex.EncodeToString(pin[:]), Secret: sealed}
	_ = a.store.Update(func(c *Config) error { c.NAS = []NASConfig{n}; return nil })
	a.pollOne(context.Background(), n)
	if a.nas[n.ID].Status != "online" {
		t.Fatal("initial online")
	}
	mode = "protocol"
	a.pollOne(context.Background(), n)
	if a.nas[n.ID].Status != "error" {
		t.Fatal("collector error classification")
	}
	mode = "ok"
	a.pollOne(context.Background(), n)
	mode = "fail"
	for i := 0; i < 3; i++ {
		a.pollOne(context.Background(), n)
	}
	if a.nas[n.ID].Status != "offline" || a.nas[n.ID].Snapshot == nil {
		t.Fatal("offline and snapshot")
	}
	mode = "ok"
	a.pollOne(context.Background(), n)
	if a.nas[n.ID].Status != "online" || a.polls[n.ID].Failures != 0 {
		t.Fatal("recovery")
	}
	mode = "auth"
	a.pollOne(context.Background(), n)
	if a.nas[n.ID].Status != "auth" {
		t.Fatal("authentication separate")
	}
	mode = "ok"
	snap.ObservedAt = time.Now().Add(-time.Hour)
	a.pollOne(context.Background(), n)
	if a.nas[n.ID].Status != "stale" {
		t.Fatal("stale collector")
	}
}
