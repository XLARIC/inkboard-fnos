package main

import (
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestOverviewSelectionAndOfflineHistory(t *testing.T) {
	d := demoDisplay(time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC))
	a := &App{}
	g := a.groups(d, false)
	if len(g) != len(d.NAS)+1 {
		t.Fatal("one screen per NAS plus weather")
	}
	for _, b := range g[0].Blocks {
		if b.ID == "today" && len(b.Rows) != len(dayHours(d.Weather, d.Clocks[0].Date)) {
			t.Fatal("hourly rail lost civil-day hours")
		}
		if b.ID == "forecast" && len(b.Rows) != 17 {
			t.Fatal("forecast rail lost dates")
		}
	}
	h := d.Weather.Hourly[27]
	before := d.Weather.Current.Time
	selected := a.groupsForSelection(d, false, h.Time.In(mustLocation(d.City.Timezone)).Format("2006-01-02"), strconv.FormatInt(h.Time.Unix(), 10))
	if !strings.Contains(selected[0].Blocks[1].Title, "分时详情") || d.Weather.Current.Time != before {
		t.Fatal("hour selection must not mutate current weather cache")
	}
	historical := a.groups(d, true)
	if !strings.Contains(historical[2].Blocks[0].Title, "历史快照") || !strings.Contains(string(g[2].Blocks[1].Content), "CPU —") {
		t.Fatal("offline history cannot be labeled live")
	}
}
func TestBoardRoutesDoNotReloadOrSplitWeather(t *testing.T) {
	s := testStore(t)
	a, e := newApp(s, s.Get(), true, "")
	if e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"/", "/basic", "/fragments"} {
		r := httptest.NewRequest("GET", path, nil)
		r.RemoteAddr = "127.0.0.1:9000"
		r.Host = "127.0.0.1:18888"
		w := httptest.NewRecorder()
		a.webHandler().ServeHTTP(w, r)
		body := w.Body.String()
		if w.Code != 200 || strings.Count(body, `data-main=`) != 3 {
			t.Fatal(path, "screen count", w.Code)
		}
		if strings.Contains(body, `http-equiv="refresh"`) || strings.Contains(body, `class="topbar"`) || strings.Contains(body, `id="pager"`) {
			t.Fatal(path, "reload or global navigation bar remains")
		}
		if !strings.Contains(body, `data-block="clocks-0"`) || !strings.Contains(body, `data-block="forecast"`) {
			t.Fatal(path, "weather overview incomplete")
		}
	}
}
