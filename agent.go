package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type PairCode struct {
	Version     int    `json:"version"`
	Endpoint    string `json:"endpoint"`
	Fingerprint string `json:"fingerprint"`
	Token       string `json:"token"`
}

func validateAgentURL(address string) error {
	u, e := url.Parse(address)
	if e != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return errors.New("采集器地址须为 HTTPS IP 地址")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsPrivate() && !ip.IsLoopback() {
		return errors.New("采集器仅支持局域网 IP 地址")
	}
	p, e := strconv.Atoi(u.Port())
	if e != nil || p < 1024 || p > 65535 {
		return errors.New("请明确指定采集器端口")
	}
	return nil
}
func agentCertificate(dir string) (tls.Certificate, string, error) {
	certPath, keyPath := filepath.Join(dir, "agent.crt"), filepath.Join(dir, "agent.key")
	if _, e := os.Stat(certPath); os.IsNotExist(e) {
		key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if e != nil {
			return tls.Certificate{}, "", e
		}
		n, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
		now := time.Now()
		template := x509.Certificate{SerialNumber: n, Subject: pkix.Name{CommonName: "InkBoard LAN Collector"}, NotBefore: now.Add(-time.Hour), NotAfter: now.AddDate(5, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
		der, e := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
		if e != nil {
			return tls.Certificate{}, "", e
		}
		kb, e := x509.MarshalPKCS8PrivateKey(key)
		if e != nil {
			return tls.Certificate{}, "", e
		}
		if e = atomicBytes(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kb}), 0600); e != nil {
			return tls.Certificate{}, "", e
		}
		if e = atomicBytes(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); e != nil {
			return tls.Certificate{}, "", e
		}
	}
	c, e := tls.LoadX509KeyPair(certPath, keyPath)
	if e != nil {
		return c, "", e
	}
	h := sha256.Sum256(c.Certificate[0])
	return c, hex.EncodeToString(h[:]), nil
}
func pinnedClient(pin string) *http.Client {
	// VerifyConnection replaces CA/hostname verification with the administrator-paired
	// certificate fingerprint. Both the fingerprint and validity period are checked.
	t := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true, VerifyConnection: func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) == 0 {
			return errors.New("采集器缺少证书")
		}
		c := cs.PeerCertificates[0]
		h := sha256.Sum256(c.Raw)
		if !constantEqual(hex.EncodeToString(h[:]), pin) {
			return fmt.Errorf("%w：证书不匹配，请重新配对", errAgentCert)
		}
		if time.Now().Before(c.NotBefore) || time.Now().After(c.NotAfter) {
			return fmt.Errorf("%w：证书有效期异常", errAgentCert)
		}
		return nil
	}}}
	return &http.Client{Transport: t, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

var errAgentAuth = errors.New("采集器凭据失效")
var errAgentCert = errors.New("采集器证书异常")
var errAgentProtocol = errors.New("采集器返回异常")

func fetchSnapshot(ctx context.Context, n NASConfig, token string) (*Snapshot, error) {
	if e := validateAgentURL(n.Endpoint); e != nil {
		return nil, e
	}
	req, e := http.NewRequestWithContext(ctx, "GET", strings.TrimSuffix(n.Endpoint, "/")+"/api/v1/metrics", nil)
	if e != nil {
		return nil, e
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := pinnedClient(n.Fingerprint)
	defer client.CloseIdleConnections()
	r, e := client.Do(req)
	if e != nil {
		return nil, e
	}
	defer r.Body.Close()
	if r.StatusCode == 401 || r.StatusCode == 403 {
		return nil, errAgentAuth
	}
	if r.StatusCode != 200 {
		return nil, fmt.Errorf("%w：HTTP %d", errAgentProtocol, r.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if e != nil {
		return nil, e
	}
	if len(b) == 1<<20 {
		return nil, errors.New("采集器响应过大")
	}
	var s Snapshot
	if e = json.Unmarshal(b, &s); e != nil {
		return nil, fmt.Errorf("%w：响应格式错误", errAgentProtocol)
	}
	if s.SchemaVersion != 1 || s.ObservedAt.IsZero() {
		return nil, fmt.Errorf("%w：协议版本或时间无效", errAgentProtocol)
	}
	if s.ObservedAt.After(time.Now().Add(5 * time.Minute)) {
		return nil, fmt.Errorf("%w：时钟异常，请核对系统时间", errAgentProtocol)
	}
	return &s, nil
}
func decodePairCode(code string) (PairCode, error) {
	var p PairCode
	b, e := base64.RawURLEncoding.DecodeString(strings.TrimSpace(code))
	if e != nil {
		return p, errors.New("配对码格式错误")
	}
	if len(b) > 4096 || json.Unmarshal(b, &p) != nil || p.Version != 1 || len(p.Token) < 32 || len(p.Token) > 128 || len(p.Fingerprint) != 64 {
		return p, errors.New("配对码无效")
	}
	if _, e = hex.DecodeString(p.Fingerprint); e != nil {
		return p, errors.New("证书指纹无效")
	}
	if e = validateAgentURL(p.Endpoint); e != nil {
		return p, e
	}
	return p, nil
}
