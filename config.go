package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Store struct {
	mu           sync.RWMutex
	dir, dataDir string
	cfg          Config
	key          []byte
}

func randomToken() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func atomicJSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	return atomicBytes(path, append(b, '\n'), 0600)
}
func atomicBytes(path string, b []byte, mode os.FileMode) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".inkboard-*")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if e = f.Chmod(mode); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if e = os.Rename(tmp, path); e != nil {
		return e
	}
	if d, e := os.Open(filepath.Dir(path)); e == nil {
		defer d.Close()
		return d.Sync()
	}
	return nil
}
func openStore(dir, dataDir string) (*Store, error) {
	for _, p := range []string{dir, dataDir} {
		if e := os.MkdirAll(p, 0700); e != nil {
			return nil, e
		}
	}
	kp := filepath.Join(dir, "master.key")
	key, e := os.ReadFile(kp)
	if os.IsNotExist(e) {
		key = make([]byte, 32)
		_, e = rand.Read(key)
		if e == nil {
			e = atomicBytes(kp, key, 0600)
		}
	}
	if e != nil || len(key) != 32 {
		return nil, errors.New("无法读取有效的本机密钥")
	}
	s := &Store{dir: dir, dataDir: dataDir, key: key}
	b, e := os.ReadFile(filepath.Join(dir, "config.json"))
	if os.IsNotExist(e) {
		s.cfg = Config{SchemaVersion: 1, Role: "hub", Listen: "127.0.0.1:18888", AgentListen: "127.0.0.1:18889", AllowedCIDRs: []string{"127.0.0.0/8", "::1/128", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7"}, LocalName: "本机 NAS", NAS: []NASConfig{}}
		if e = s.saveLocked(); e != nil {
			return nil, e
		}
	} else if e != nil {
		return nil, e
	} else if e = json.Unmarshal(b, &s.cfg); e != nil {
		return nil, fmt.Errorf("配置文件损坏：%w", e)
	}
	if s.cfg.ClockCities == nil {
		s.cfg.ClockCities = defaultClockCities()
	}
	if s.cfg.SchemaVersion != 1 {
		return nil, errors.New("配置版本不受支持，未覆盖原配置")
	}
	if e = validateConfig(s.cfg); e != nil {
		return nil, e
	}
	return s, nil
}
func (s *Store) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := s.cfg
	c.NAS = append([]NASConfig{}, c.NAS...)
	c.ClockCities = append([]ClockCity{}, c.ClockCities...)
	c.AllowedCIDRs = append([]string{}, c.AllowedCIDRs...)
	if c.City != nil {
		x := *c.City
		c.City = &x
	}
	return c
}
func (s *Store) Update(fn func(*Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	before := s.cfg
	b, _ := json.Marshal(before)
	var c Config
	_ = json.Unmarshal(b, &c)
	if e := fn(&c); e != nil {
		return e
	}
	if e := validateConfig(c); e != nil {
		return e
	}
	s.cfg = c
	if e := s.saveLocked(); e != nil {
		s.cfg = before
		return e
	}
	return nil
}
func (s *Store) saveLocked() error { return atomicJSON(filepath.Join(s.dir, "config.json"), s.cfg) }
func (s *Store) Seal(text string) (string, error) {
	block, e := aes.NewCipher(s.key)
	if e != nil {
		return "", e
	}
	a, e := cipher.NewGCM(block)
	if e != nil {
		return "", e
	}
	n := make([]byte, a.NonceSize())
	if _, e = rand.Read(n); e != nil {
		return "", e
	}
	return base64.RawURLEncoding.EncodeToString(a.Seal(n, n, []byte(text), []byte("inkboard-v1"))), nil
}
func (s *Store) Open(text string) (string, error) {
	b, e := base64.RawURLEncoding.DecodeString(text)
	if e != nil {
		return "", e
	}
	block, _ := aes.NewCipher(s.key)
	a, _ := cipher.NewGCM(block)
	if len(b) < a.NonceSize() {
		return "", errors.New("凭据格式错误")
	}
	p, e := a.Open(nil, b[:a.NonceSize()], b[a.NonceSize():], []byte("inkboard-v1"))
	return string(p), e
}
func hashPassword(password string) (string, error) {
	if len(password) < 12 || len(password) > 256 {
		return "", errors.New("管理密码须为 12–256 字节")
	}
	salt := make([]byte, 16)
	if _, e := rand.Read(salt); e != nil {
		return "", e
	}
	key, e := pbkdf2.Key(sha256.New, password, salt, 600000, 32)
	if e != nil {
		return "", e
	}
	return fmt.Sprintf("pbkdf2$600000$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}
func checkPassword(password, encoded string) bool {
	a := strings.Split(encoded, "$")
	if len(a) != 4 || a[0] != "pbkdf2" || len(password) > 256 {
		return false
	}
	n, e := strconv.Atoi(a[1])
	if e != nil || n < 600000 || n > 1000000 {
		return false
	}
	salt, e := base64.RawStdEncoding.DecodeString(a[2])
	if e != nil {
		return false
	}
	expected, e := base64.RawStdEncoding.DecodeString(a[3])
	if e != nil || len(expected) != 32 {
		return false
	}
	k, e := pbkdf2.Key(sha256.New, password, salt, n, 32)
	return e == nil && hmac.Equal(k, expected)
}
func constantEqual(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func validateConfig(c Config) error {
	if c.GeocoderURL != "" {
		u, e := url.Parse(c.GeocoderURL)
		if e != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && isPrivateIP(u.Hostname()))) {
			return errors.New("地址服务需 HTTPS，或局域网 HTTP；不能在地址中包含凭据或查询参数")
		}
	}
	if c.Role != "hub" && c.Role != "agent" {
		return errors.New("运行角色须为 hub 或 agent")
	}
	for _, a := range []string{c.Listen, c.AgentListen} {
		host, port, e := net.SplitHostPort(a)
		if e != nil || net.ParseIP(host) == nil {
			return errors.New("监听地址须使用明确的 IP 和端口")
		}
		p, e := strconv.Atoi(port)
		if e != nil || p < 1024 || p > 65535 {
			return errors.New("服务端口须为 1024–65535")
		}
	}
	if len(c.AllowedCIDRs) == 0 {
		return errors.New("至少需要一个允许的局域网段")
	}
	for _, v := range c.AllowedCIDRs {
		p, e := netip.ParsePrefix(v)
		if e != nil {
			return errors.New("网段格式错误")
		}
		allowed := false
		for _, base := range []string{"127.0.0.0/8", "::1/128", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7"} {
			n := netip.MustParsePrefix(base)
			if n.Addr().BitLen() == p.Addr().BitLen() && p.Bits() >= n.Bits() && n.Contains(p.Masked().Addr()) {
				allowed = true
				break
			}
		}
		if !allowed {
			return errors.New("允许网段必须完整位于局域网或回环范围")
		}
	}
	if c.Listen == c.AgentListen {
		return errors.New("页面和采集接口不能占用同一地址")
	}
	if c.HTTPSListen != "" {
		host, port, e := net.SplitHostPort(c.HTTPSListen)
		p, _ := strconv.Atoi(port)
		if e != nil || net.ParseIP(host) == nil || p < 1024 || p > 65535 || c.HTTPSCert == "" || c.HTTPSKey == "" {
			return errors.New("HTTPS 监听与证书配置无效")
		}
	}
	if c.City != nil {
		if e := validateCity(*c.City); e != nil {
			return e
		}
	}
	if len(c.ClockCities) > 8 {
		return errors.New("最多添加 8 个时钟城市")
	}
	for _, city := range c.ClockCities {
		if strings.TrimSpace(city.Name) == "" || len(city.Name) > 120 {
			return errors.New("时钟城市名无效")
		}
		if _, e := time.LoadLocation(city.Timezone); e != nil {
			return errors.New("时钟城市时区无效")
		}
	}
	seen := map[string]bool{}
	for _, n := range c.NAS {
		if n.ID == "" || seen[n.ID] || len(n.Name) == 0 || len(n.Name) > 120 {
			return errors.New("NAS 名称或标识无效")
		}
		seen[n.ID] = true
		if e := validateAgentURL(n.Endpoint); e != nil {
			return e
		}
	}
	return nil
}
