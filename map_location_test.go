package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMapLocationImport(t *testing.T) {
	for _, tt := range []struct {
		input   string
		lat     float64
		lon     float64
		warning bool
	}{
		{" -36.85, 174.76 ", -36.85, 174.76, false},
		{"31.23，121.47", 31.23, 121.47, false},
		{"31.123456789,121.987654321", 31.123456789, 121.987654321, false},
		{"0,0", 0, 0, false},
		{"https://www.google.com/maps/search/?api=1&query=-36.85%2C174.76", -36.85, 174.76, false},
		{"https://maps.google.com/?q=31.23,121.47", 31.23, 121.47, false},
		{"https://www.google.co.nz/maps/place/Test/@-36.9,174.8,17z/data=!4m2!3d-36.85!4d174.76", -36.85, 174.76, true},
		{"https://www.google.com/maps/place/Test/@31.1,121.1,17z/data=!4m15!1m8!3m7!8m2!3d31.123456789!4d121.987654321!3m5!8m2!3d31.1234567890!4d121.9876543210", 31.123456789, 121.987654321, true},
		{"https://www.google.com/maps/@-36.85,174.76,17z", -36.85, 174.76, true},
		{"https://www.google.com/maps/@?api=1&map_action=map&center=-36.85,174.76", -36.85, 174.76, true},
	} {
		p, err := parseMapLocation(tt.input)
		if err != nil || p.Latitude != tt.lat || p.Longitude != tt.lon || (p.Warning != "") != tt.warning {
			t.Errorf("import %q: %#v, %v", tt.input, p, err)
		}
	}
	for _, input := range []string{
		"", "NaN,121", "91,120", "31,181", "174,-36", "https://www.google.com/maps/search/?query=Auckland",
		"https://maps.app.goo.gl/TEST", "https://goo.gl/maps/TEST", "https://www.google.com/maps/dir/a/b/@31,121,10z",
		"https://www.google.com/maps/@?map_action=pano&center=31,121", "https://www.google.com/maps/search/?query=31,121&query_place_id=PLACE",
		"https://www.google.com/maps/data=!3d31!4d121!3d32!4d122", "https://example.com/maps/@31,121,10z",
		"https://www.google.com.attacker.test/maps/@31,121,10z", "https://user:password@www.google.com/maps/@31,121,10z",
		"javascript:alert(1)", "https://www.google.com/notmaps/@31,121,10z", strings.Repeat("1", 8193),
	} {
		if _, err := parseMapLocation(input); err == nil {
			t.Errorf("must reject ambiguous or invalid input: %q", input)
		}
	}
}

type coordinateTransport func(*http.Request) (*http.Response, error)

func (f coordinateTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLocationPreservesCoordinatePrecision(t *testing.T) {
	client := &http.Client{Transport: coordinateTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("latitude") != "31.123456789" || r.URL.Query().Get("longitude") != "121.987654321" {
			t.Error("timezone request rounded coordinates")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"timezone":"Asia/Shanghai"}`)), Header: http.Header{}}, nil
	})}
	c, err := locateCity(context.Background(), client, 31.123456789, 121.987654321)
	if err != nil || c.Latitude != 31.123456789 || c.Longitude != 121.987654321 {
		t.Fatal("device coordinates rounded", c, err)
	}
	s := testStore(t)
	if err := s.Update(func(conf *Config) error { conf.City = &c; return nil }); err != nil {
		t.Fatal(err)
	}
	reopened, err := openStore(s.dir, s.dataDir)
	if err != nil || *reopened.Get().City != c {
		t.Fatal("saved coordinates lost precision", err)
	}
}

func TestMapImportRequiresSessionAndCSRFAndDoesNotSave(t *testing.T) {
	a := testApp(t)
	a.sessions["test-session"] = session{CSRF: "test-csrf", Until: time.Now().Add(time.Hour)}
	for _, tt := range []struct {
		method, cookie, csrf, origin string
		code                         int
	}{
		{"POST", "", "test-csrf", "http://127.0.0.1", 401},
		{"POST", "test-session", "", "http://127.0.0.1", 403},
		{"POST", "test-session", "test-csrf", "http://attacker.test", 403},
		{"GET", "test-session", "test-csrf", "http://127.0.0.1", 404},
		{"POST", "test-session", "test-csrf", "http://127.0.0.1", 200},
	} {
		r := httptest.NewRequest(tt.method, "http://127.0.0.1/api/admin/map-location", strings.NewReader(`{"Input":"31.23,121.47"}`))
		r.RemoteAddr = "127.0.0.1:12345"
		r.Header.Set("Origin", tt.origin)
		r.Header.Set("X-CSRF-Token", tt.csrf)
		if tt.cookie != "" {
			r.AddCookie(&http.Cookie{Name: "inkboard_session", Value: tt.cookie})
		}
		w := httptest.NewRecorder()
		a.webHandler().ServeHTTP(w, r)
		if w.Code != tt.code {
			t.Errorf("%s: got %d, want %d: %s", tt.method, w.Code, tt.code, w.Body.String())
		}
		if tt.code == 200 {
			var p MapLocation
			if json.Unmarshal(w.Body.Bytes(), &p) != nil || p.Latitude != 31.23 || p.Longitude != 121.47 {
				t.Fatal("wrong imported coordinates")
			}
		}
	}
	if a.store.Get().City != nil {
		t.Fatal("import saved a location without user confirmation")
	}
}
