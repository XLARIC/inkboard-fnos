package main

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type session struct {
	CSRF  string
	Until time.Time
}
type pollState struct {
	Failures int
	Next     time.Time
}
type App struct {
	store         *Store
	runtime       Config
	demo          bool
	snapshotFile  string
	client        *http.Client
	collector     *Collector
	mu            sync.RWMutex
	weather       *Weather
	weatherError  string
	nextWeather   time.Time
	nextGeocoding time.Time
	local         *Snapshot
	nas           map[string]NASView
	polls         map[string]pollState
	sessions      map[string]session
	attempts      map[string]time.Time
	bootstrap     string
	cert          tls.Certificate
	fingerprint   string
	token         string
}

func newApp(s *Store, c Config, demo bool, snapshot string) (*App, error) {
	a := &App{store: s, runtime: c, demo: demo, snapshotFile: snapshot, client: &http.Client{Timeout: 12 * time.Second}, collector: newCollector(""), nas: map[string]NASView{}, polls: map[string]pollState{}, sessions: map[string]session{}, attempts: map[string]time.Time{}}
	if demo {
		return a, nil
	}
	var e error
	a.cert, a.fingerprint, e = agentCertificate(s.dir)
	if e != nil {
		return nil, e
	}
	if c.AgentSecret == "" {
		a.token = randomToken()
		sealed, e := s.Seal(a.token)
		if e != nil {
			return nil, e
		}
		if e = s.Update(func(c *Config) error { c.AgentSecret = sealed; return nil }); e != nil {
			return nil, e
		}
	} else {
		a.token, e = s.Open(c.AgentSecret)
		if e != nil {
			return nil, e
		}
	}
	if c.PasswordHash == "" {
		p := filepath.Join(s.dir, "setup-token")
		b, e := os.ReadFile(p)
		if os.IsNotExist(e) {
			a.bootstrap = randomToken()
			e = atomicBytes(p, []byte(a.bootstrap+"\n"), 0600)
		} else if e == nil {
			a.bootstrap = strings.TrimSpace(string(b))
		}
		if e != nil {
			return nil, e
		}
	}
	if b, e := os.ReadFile(filepath.Join(s.dataDir, "weather.json")); e == nil {
		var w Weather
		if json.Unmarshal(b, &w) == nil && w.SchemaVersion == 1 && c.City != nil && w.City == *c.City {
			a.weather = &w
		}
	}
	if b, e := os.ReadFile(filepath.Join(s.dataDir, "nas.json")); e == nil {
		var cache struct {
			SchemaVersion int
			NAS           []NASView
		}
		if json.Unmarshal(b, &cache) == nil && cache.SchemaVersion == 1 {
			for _, n := range cache.NAS {
				n.Status = "pending"
				n.Reason = "等待重新连接；最后快照为历史数据"
				a.nas[n.ID] = n
			}
		}
	}
	return a, nil
}
func (a *App) config() Config { c := a.store.Get(); c.Role = a.runtime.Role; return c }
func (a *App) start(ctx context.Context) {
	if a.demo {
		return
	}
	go every(ctx, 5*time.Second, func() {
		c := a.config()
		if c.Role != "agent" && !c.MonitorLocal {
			return
		}
		var s *Snapshot
		if a.snapshotFile != "" {
			b, e := os.ReadFile(a.snapshotFile)
			if e == nil {
				var x Snapshot
				if json.Unmarshal(b, &x) == nil && x.SchemaVersion == 1 {
					s = &x
				}
			}
		} else {
			x := a.collector.Collect(time.Now())
			s = &x
		}
		a.mu.Lock()
		a.local = s
		a.mu.Unlock()
	})
	if a.runtime.Role == "hub" {
		go every(ctx, 15*time.Second, func() { a.refreshWeather(ctx) })
		go every(ctx, time.Second, func() { a.pollNAS(ctx) })
	}
}
func (a *App) refreshWeather(ctx context.Context) {
	c := a.config()
	if c.City == nil {
		return
	}
	a.mu.Lock()
	if time.Now().Before(a.nextWeather) {
		a.mu.Unlock()
		return
	}
	a.nextWeather = time.Now().Add(30 * time.Minute)
	a.mu.Unlock()
	w, e := fetchWeather(ctx, a.client, *c.City, time.Now())
	a.mu.Lock()
	defer a.mu.Unlock()
	if e != nil {
		a.weatherError = "天气更新失败，保留缓存；将稍后重试"
		a.nextWeather = time.Now().Add(2 * time.Minute)
		return
	}
	if a.config().City == nil || *a.config().City != *c.City {
		return
	}
	a.weather = w
	a.weatherError = ""
	if e = atomicJSON(filepath.Join(a.store.dataDir, "weather.json"), w); e != nil {
		log.Print("天气缓存保存失败")
	}
}
func (a *App) pollNAS(ctx context.Context) {
	// Each collector has its own retry schedule; a powered-off NAS cannot delay others.
	c := a.config()
	for _, n := range c.NAS {
		a.mu.Lock()
		p := a.polls[n.ID]
		if time.Now().Before(p.Next) {
			a.mu.Unlock()
			continue
		}
		p.Next = time.Now().Add(15 * time.Second)
		a.polls[n.ID] = p
		a.mu.Unlock()
		go a.pollOne(ctx, n)
	}
}
func (a *App) pollOne(ctx context.Context, n NASConfig) {
	token, e := a.store.Open(n.Secret)
	var snap *Snapshot
	if e == nil {
		snap, e = fetchSnapshot(ctx, n, token)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	active := false
	for _, current := range a.config().NAS {
		if current == n {
			active = true
			break
		}
	}
	if !active {
		return
	}
	p := a.polls[n.ID]
	v := a.nas[n.ID]
	v.ID = n.ID
	v.Name = n.Name
	if e == nil && time.Since(snap.ObservedAt) <= 45*time.Second {
		now := time.Now()
		v.Status = "online"
		v.Reason = ""
		v.Snapshot = snap
		v.LastReceived = &now
		p.Failures = 0
		p.Next = now.Add(15 * time.Second)
	} else {
		p.Failures++
		v.Status = "connecting"
		v.Reason = "连接暂时失败，正在重试"
		if errors.Is(e, errAgentCert) {
			v.Status = "auth"
			v.Reason = "采集器证书异常，请重新配对"
		} else if errors.Is(e, errAgentProtocol) {
			v.Status = "error"
			v.Reason = "采集器返回异常，请检查采集器"
		} else if errors.Is(e, errAgentAuth) {
			v.Status = "auth"
			v.Reason = "配对凭据失效，请重新配对"
		} else if e == nil {
			v.Status = "stale"
			v.Reason = "采集助手未更新；最后快照为历史数据"
		} else if p.Failures >= 3 {
			v.Status = "offline"
			v.Reason = "NAS 已离线；开机后会自动恢复"
		}
		p.Next = time.Now().Add(retryDelay(p.Failures))
	}
	a.polls[n.ID] = p
	a.nas[n.ID] = v
	cache := struct {
		SchemaVersion int
		NAS           []NASView
	}{SchemaVersion: 1}
	for _, item := range a.nas {
		cache.NAS = append(cache.NAS, item)
	}
	if e = atomicJSON(filepath.Join(a.store.dataDir, "nas.json"), cache); e != nil {
		log.Print("NAS 快照保存失败")
	}
}
func retryDelay(failures int) time.Duration {
	if failures <= 3 {
		return 15 * time.Second
	}
	n := failures - 3
	if n > 3 {
		n = 3
	}
	return time.Duration(15*(1<<n)) * time.Second
}
func (a *App) display() Display {
	now := time.Now()
	if a.demo {
		return demoDisplay(now)
	}
	c := a.config()
	a.mu.RLock()
	defer a.mu.RUnlock()
	d := Display{SchemaVersion: 1, Version: version, Now: now, Role: c.Role, City: c.City, Clocks: configuredClocks(now, c.City, c.ClockCities), NAS: []NASView{}}
	d.WeatherStatus = a.weatherError
	if a.weather != nil && c.City != nil && a.weather.City == *c.City {
		w := *a.weather
		w.Error = a.weatherError
		if time.Since(w.FetchedAt) > 65*time.Minute && w.Error == "" {
			w.Error = "缓存超过一小时，请检查网络"
		}
		d.Weather = &w
	}
	if c.MonitorLocal || c.Role == "agent" {
		v := NASView{ID: "local", Name: c.LocalName, Status: "online", Snapshot: a.local}
		if a.local == nil || time.Since(a.local.ObservedAt) > 45*time.Second {
			v.Status = "stale"
			v.Reason = "本机采集尚未更新，检查采集助手"
		}
		if a.local != nil {
			t := a.local.ObservedAt
			v.LastReceived = &t
		}
		d.NAS = append(d.NAS, v)
	}
	for _, n := range c.NAS {
		v, ok := a.nas[n.ID]
		if !ok {
			v = NASView{Status: "pending", Reason: "等待首次连接"}
		}
		v.ID = n.ID
		v.Name = n.Name
		d.NAS = append(d.NAS, v)
	}
	return d
}
func (a *App) serve(ctx context.Context) error {
	servers := []*http.Server{}
	web := &http.Server{Addr: a.runtime.Listen, Handler: a.webHandler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second}
	servers = append(servers, web)
	if !a.demo && (a.runtime.Role == "agent" || a.runtime.MonitorLocal) {
		servers = append(servers, &http.Server{Addr: a.runtime.AgentListen, Handler: a.lan(http.HandlerFunc(a.metrics)), TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{a.cert}}, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second})
	}
	if a.runtime.HTTPSListen != "" {
		servers = append(servers, &http.Server{Addr: a.runtime.HTTPSListen, Handler: a.webHandler(), ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 20 * time.Second})
	}
	errs := make(chan error, len(servers))
	for i, s := range servers {
		go func(i int, s *http.Server) {
			var e error
			if i == 1 && s.TLSConfig != nil {
				e = s.ListenAndServeTLS("", "")
			} else if s.Addr == a.runtime.HTTPSListen && a.runtime.HTTPSListen != "" {
				e = s.ListenAndServeTLS(a.runtime.HTTPSCert, a.runtime.HTTPSKey)
			} else {
				e = s.ListenAndServe()
			}
			errs <- e
		}(i, s)
	}
	select {
	case <-ctx.Done():
	case e := <-errs:
		if e != nil && !errors.Is(e, http.ErrServerClosed) {
			for _, s := range servers {
				_ = s.Close()
			}
			return e
		}
	}
	stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, s := range servers {
		_ = s.Shutdown(stop)
	}
	return nil
}
func (a *App) lan(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := r.Host
		if host, _, err := net.SplitHostPort(name); err == nil {
			name = host
		}
		hostIP := net.ParseIP(strings.Trim(name, "[]"))
		if name != "localhost" && (hostIP == nil || !hostIP.IsPrivate() && !hostIP.IsLoopback()) {
			http.Error(w, "请使用局域网 IP 访问", 403)
			return
		}
		host, _, e := net.SplitHostPort(r.RemoteAddr)
		ip := net.ParseIP(host)
		allowed := false
		if e == nil && ip != nil {
			for _, c := range a.config().AllowedCIDRs {
				_, n, e := net.ParseCIDR(c)
				if e == nil && n.Contains(ip) {
					allowed = true
					break
				}
			}
		}
		// Forwarded headers never change the trusted peer address.
		if !allowed {
			http.Error(w, "仅允许配置的局域网访问", 403)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'; object-src 'none'")
		if a.demo {
			w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; frame-ancestors 'self'; base-uri 'none'; form-action 'self'; object-src 'none'")
		}
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
func (a *App) metrics(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/v1/metrics" {
		http.NotFound(w, r)
		return
	}
	if r.Method != "GET" {
		http.Error(w, "方法不允许", 405)
		return
	}
	a.mu.RLock()
	token := a.token
	a.mu.RUnlock()
	if !constantEqual(r.Header.Get("Authorization"), "Bearer "+token) {
		http.Error(w, "需要配对凭据", 401)
		return
	}
	a.mu.RLock()
	s := a.local
	a.mu.RUnlock()
	if s == nil {
		http.Error(w, "采集尚未就绪", 503)
		return
	}
	writeJSON(w, s)
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}
func bodyJSON(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return errors.New("请求格式或大小无效")
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("请求必须只有一个对象")
	}
	return nil
}
func (a *App) authenticated(r *http.Request) (session, bool) {
	cookie, e := r.Cookie("inkboard_session")
	if e != nil {
		return session{}, false
	}
	a.mu.RLock()
	s, ok := a.sessions[cookie.Value]
	a.mu.RUnlock()
	return s, ok && time.Now().Before(s.Until)
}
func (a *App) sessionCookie(w http.ResponseWriter, r *http.Request) session {
	id := randomToken()
	s := session{CSRF: randomToken(), Until: time.Now().Add(12 * time.Hour)}
	a.mu.Lock()
	for k, v := range a.sessions {
		if time.Now().After(v.Until) {
			delete(a.sessions, k)
		}
	}
	a.sessions[id] = s
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "inkboard_session", Value: id, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: 43200})
	return s
}
func (a *App) adminAPI(w http.ResponseWriter, r *http.Request) {
	if a.demo {
		http.Error(w, "演示模式不允许修改共享配置", 403)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/admin/")
	if path == "login" && r.Method == "POST" {
		var b struct {
			Password   string
			SetupToken string
		}
		if e := bodyJSON(w, r, &b); e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		a.mu.Lock()
		until := a.attempts[ip]
		if time.Now().Before(until) {
			a.mu.Unlock()
			http.Error(w, "请稍后再试", 429)
			return
		}
		if len(a.attempts) > 256 {
			for k, t := range a.attempts {
				if time.Now().After(t) {
					delete(a.attempts, k)
				}
			}
		}
		a.attempts[ip] = time.Now().Add(2 * time.Second)
		a.mu.Unlock()
		c := a.config()
		if c.PasswordHash == "" {
			if a.bootstrap == "" || !constantEqual(b.SetupToken, a.bootstrap) {
				http.Error(w, "首次设置需要安装目录内的 setup-token", 401)
				return
			}
			h, e := hashPassword(b.Password)
			if e != nil {
				http.Error(w, e.Error(), 400)
				return
			}
			if e = a.store.Update(func(c *Config) error {
				if c.PasswordHash != "" {
					return errors.New("已完成设置，请登录")
				}
				c.PasswordHash = h
				return nil
			}); e != nil {
				http.Error(w, "保存失败", 500)
				return
			}
			_ = os.Remove(filepath.Join(a.store.dir, "setup-token"))
		} else if !checkPassword(b.Password, c.PasswordHash) {
			http.Error(w, "密码错误", 401)
			return
		}
		writeJSON(w, a.sessionCookie(w, r))
		return
	}
	s, ok := a.authenticated(r)
	if !ok {
		http.Error(w, "请先登录", 401)
		return
	}
	if r.Method == "GET" {
		switch path {
		case "config":
			c := a.config()
			for i := range c.NAS {
				c.NAS[i].Secret = ""
				c.NAS[i].Fingerprint = ""
			}
			c.PasswordHash = ""
			c.AgentSecret = ""
			writeJSON(w, struct {
				Config    Config
				CSRF      string
				Bootstrap bool
			}{c, s.CSRF, a.config().PasswordHash == ""})
		case "cities":
			v, e := searchCities(r.Context(), a.client, r.URL.Query().Get("q"))
			if e != nil {
				http.Error(w, "城市搜索失败，检查网络后重试", 502)
				return
			}
			writeJSON(w, v)
		case "addresses":
			v, e := a.searchAddresses(r.Context(), r.URL.Query().Get("q"))
			if e != nil {
				http.Error(w, safeError(e), 400)
				return
			}
			writeJSON(w, v)
		default:
			http.NotFound(w, r)
		}
		return
	}
	if r.Method != "POST" {
		http.Error(w, "方法不允许", 405)
		return
	}
	if !constantEqual(r.Header.Get("X-CSRF-Token"), s.CSRF) {
		http.Error(w, "会话校验失败", 403)
		return
	}
	expected := "http://" + r.Host
	if r.TLS != nil {
		expected = "https://" + r.Host
	}
	if r.Header.Get("Origin") != expected {
		http.Error(w, "来源校验失败", 403)
		return
	}
	var e error
	switch path {
	case "map-location":
		var b struct{ Input string }
		e = bodyJSON(w, r, &b)
		if e == nil {
			var point MapLocation
			point, e = parseMapLocation(b.Input)
			if e == nil {
				writeJSON(w, point)
				return
			}
		}
	case "geocoder":
		var b struct{ URL string }
		e = bodyJSON(w, r, &b)
		if e == nil {
			e = a.store.Update(func(c *Config) error { c.GeocoderURL = strings.TrimSpace(b.URL); return nil })
		}
	case "clocks":
		var b struct{ Cities []ClockCity }
		e = bodyJSON(w, r, &b)
		if e == nil {
			e = a.store.Update(func(c *Config) error { c.ClockCities = append([]ClockCity{}, b.Cities...); return nil })
		}
	case "location":
		var b struct{ Latitude, Longitude float64 }
		e = bodyJSON(w, r, &b)
		if e == nil {
			var city City
			city, e = locateCity(r.Context(), a.client, b.Latitude, b.Longitude)
			if e == nil {
				writeJSON(w, city)
				return
			}
		}
	case "logout":
		if c, err := r.Cookie("inkboard_session"); err == nil {
			a.mu.Lock()
			delete(a.sessions, c.Value)
			a.mu.Unlock()
		}
		http.SetCookie(w, &http.Cookie{Name: "inkboard_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	case "city":
		var c City
		e = bodyJSON(w, r, &c)
		if e == nil && c.Timezone == "" {
			c, e = resolveTimezone(r.Context(), a.client, c)
		}
		if e == nil {
			e = validateCity(c)
		}
		if e == nil {
			e = a.store.Update(func(v *Config) error { v.City = &c; return nil })
			a.mu.Lock()
			a.nextWeather = time.Time{}
			a.mu.Unlock()
		}
	case "local":
		var b struct {
			Enabled bool
			Name    string
		}
		e = bodyJSON(w, r, &b)
		if e == nil {
			if len(b.Name) == 0 || len(b.Name) > 120 {
				e = errors.New("名称须为 1–120 字节")
			} else {
				e = a.store.Update(func(c *Config) error { c.MonitorLocal = b.Enabled; c.LocalName = b.Name; return nil })
			}
		}
	case "pair":
		var b struct {
			Name string
			Code string
		}
		e = bodyJSON(w, r, &b)
		if e == nil {
			var p PairCode
			p, e = decodePairCode(b.Code)
			if e == nil {
				var sealed string
				sealed, e = a.store.Seal(p.Token)
				if e == nil {
					n := NASConfig{ID: randomToken()[:12], Name: b.Name, Endpoint: p.Endpoint, Fingerprint: p.Fingerprint, Secret: sealed}
					_, e = fetchSnapshot(r.Context(), n, p.Token)
					if e == nil {
						e = a.store.Update(func(c *Config) error {
							if len(c.NAS) >= 16 {
								return errors.New("最多配置 16 台 NAS")
							}
							c.NAS = append(c.NAS, n)
							return nil
						})
					}
				}
			}
		}
	case "nas":
		var b struct {
			IDs    []string
			Remove string
		}
		e = bodyJSON(w, r, &b)
		if e == nil {
			e = a.store.Update(func(c *Config) error {
				if b.Remove != "" {
					v := []NASConfig{}
					for _, n := range c.NAS {
						if n.ID != b.Remove {
							v = append(v, n)
						}
					}
					c.NAS = v
					return nil
				}
				if len(b.IDs) != len(c.NAS) {
					return errors.New("排序列表无效")
				}
				seen := map[string]bool{}
				out := []NASConfig{}
				for _, id := range b.IDs {
					found := false
					for _, n := range c.NAS {
						if n.ID == id && !seen[id] {
							out = append(out, n)
							found = true
							seen[id] = true
							break
						}
					}
					if !found {
						return errors.New("排序列表无效")
					}
				}
				c.NAS = out
				return nil
			})
		}
	case "pair-code":
		if a.runtime.Role != "agent" && !a.runtime.MonitorLocal {
			http.Error(w, "先在网页开启本机采集并重启应用，再生成配对码", 400)
			return
		}
		var b struct {
			Address string
			Rotate  bool
		}
		e = bodyJSON(w, r, &b)
		if e == nil {
			_, port, _ := net.SplitHostPort(a.runtime.AgentListen)
			endpoint := "https://" + net.JoinHostPort(b.Address, port)
			e = validateAgentURL(endpoint)
			if e == nil {
				if b.Rotate {
					token := randomToken()
					sealed, err := a.store.Seal(token)
					e = err
					if e == nil {
						e = a.store.Update(func(c *Config) error { c.AgentSecret = sealed; return nil })
						if e == nil {
							a.mu.Lock()
							a.token = token
							a.mu.Unlock()
						}
					}
				}
				if e == nil {
					a.mu.RLock()
					p := PairCode{Version: 1, Endpoint: endpoint, Fingerprint: a.fingerprint, Token: a.token}
					a.mu.RUnlock()
					raw, _ := json.Marshal(p)
					writeJSON(w, struct{ Code string }{base64.RawURLEncoding.EncodeToString(raw)})
					return
				}
			}
		}
	default:
		http.NotFound(w, r)
		return
	}
	if e != nil {
		http.Error(w, "操作失败："+safeError(e), 400)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
func safeError(e error) string {
	// HTTP client errors can contain endpoints; never include pairing tokens.
	text := e.Error()
	if len(text) > 300 {
		return "请检查配置和采集器连接"
	}
	return text
}
func (a *App) webHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/display", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "方法不允许", 405)
			return
		}
		writeJSON(w, a.display())
	})
	mux.HandleFunc("/api/admin/", a.adminAPI)
	mux.HandleFunc("/api/v1/qrcode", a.qrcodePage)
	mux.HandleFunc("/install/collector.sh", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "方法不允许", 405)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=collector.sh")
		_, _ = w.Write(collectorInstallScript)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok\n") })
	mux.HandleFunc("/admin", a.adminPage)
	mux.HandleFunc("/device", a.devicePage)
	mux.HandleFunc("/fragments", a.fragments)
	mux.HandleFunc("/basic", a.basicPage)
	mux.HandleFunc("/hourly", a.hourlyPage)
	mux.HandleFunc("/preview", func(w http.ResponseWriter, r *http.Request) {
		if !a.demo {
			http.NotFound(w, r)
			return
		}
		htmlResponse(w, "preview.html", nil)
	})
	mux.Handle("/assets/", http.FileServer(http.FS(webFiles)))
	mux.HandleFunc("/", a.dashboardPage)
	return a.lan(mux)
}
