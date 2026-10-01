package main

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"
)

//go:embed assets templates
var webFiles embed.FS
var templates = template.Must(template.New("").Funcs(template.FuncMap{
	"temp":    func(p *float64) string { return optional(p, "°") },
	"num":     func(p *float64) string { return optional(p, "") },
	"weather": weatherText,
	"metric":  formatMetric,
	"bytes":   formatBytes,
	"status":  statusText,
	"stamp":   func(t time.Time) string { return t.Format("01-02 15:04 MST") },
}).ParseFS(webFiles, "templates/*.html"))

type Block struct {
	Source    template.HTML
	ID, Title string
	Rows      []template.HTML
	Content   template.HTML
}
type Group struct {
	ID, Name string
	Blocks   []Block
}
type PageData struct {
	Display           Display
	Groups            []Group
	Blocks            []Block
	Title, Main, Date string
	Page, Pages       int
	Prev, Next        string
	Basic, History    bool
}

func optional(p *float64, unit string) string {
	if p == nil {
		return "未提供"
	}
	return strconv.FormatFloat(*p, 'f', 1, 64) + unit
}
func formatBytes(v uint64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	x := float64(v)
	i := 0
	for x >= 1024 && i < len(units)-1 {
		x /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", x, units[i])
}

func formatMetric(m Metric) string {
	if m.Value == nil {
		return "未提供"
	}
	if m.Unit == "B" || m.Unit == "B/s" {
		v := formatBytes(uint64(*m.Value))
		if m.Unit == "B/s" {
			v += "/s"
		}
		return v
	}
	if m.Unit == "s" {
		d := time.Duration(*m.Value) * time.Second
		return fmt.Sprintf("%d 天 %d 时", int(d.Hours())/24, int(d.Hours())%24)
	}
	return fmt.Sprintf("%.1f %s", *m.Value, m.Unit)
}
func statusText(s string) string {
	switch s {
	case "online":
		return "在线"
	case "offline":
		return "已关机 / 离线"
	case "auth":
		return "凭据失效"
	case "error":
		return "采集异常"
	case "stale":
		return "采集异常"
	case "connecting":
		return "正在重连"
	default:
		return "等待连接"
	}
}
func renderPart(name string, v any) template.HTML {
	var b bytes.Buffer
	if e := templates.ExecuteTemplate(&b, name, v); e != nil {
		return template.HTML("内容暂不可用")
	}
	return template.HTML(b.String())
}
func (a *App) groups(d Display, history bool) []Group {
	groups := []Group{}
	if d.Role == "hub" {
		g := Group{ID: "weather", Name: "天气"}
		for start := 0; start < len(d.Clocks); start += 3 {
			end := start + 3
			if end > len(d.Clocks) {
				end = len(d.Clocks)
			}
			g.Blocks = append(g.Blocks, Block{ID: fmt.Sprintf("clocks-%d", start/3), Title: "城市时间", Content: renderPart("clocks", d.Clocks[start:end])})
		}
		if d.Weather == nil {
			g.Blocks = append(g.Blocks, Block{ID: "welcome", Title: "天气", Content: renderPart("welcome", d)})
		} else {
			w := d.Weather
			today := d.Clocks[0].Date
			var currentDay Day
			for _, day := range w.Days {
				if day.Date == today {
					currentDay = day
					break
				}
			}
			hero := struct {
				Weather *Weather
				Day     Day
				Demo    bool
			}{w, currentDay, d.Demo}
			g.Blocks = append(g.Blocks, Block{ID: "current", Title: "现在 · " + w.City.Name, Content: renderPart("current", hero)})
			hours := dayHours(w, today)
			rows := []template.HTML{}
			loc, _ := time.LoadLocation(w.City.Timezone)
			for _, h := range hours {
				if h.Time.In(loc).Hour()%3 == 0 {
					rows = append(rows, renderPart("hour", struct {
						Conditions Conditions
						Hour       string
					}{h, h.Time.In(loc).Format("15:04")}))
				}
			}
			g.Blocks = append(g.Blocks, Block{ID: "today", Title: "今日分时 · 每三小时摘要", Rows: rows})
			rows = []template.HTML{}
			yesterday := d.Now.In(loc).AddDate(0, 0, -1).Format("2006-01-02")
			for _, day := range w.Days {
				label := day.Date[5:]
				if day.Date == yesterday {
					label = "昨天 · " + label
				}
				if day.Date == today {
					label = "今天 · " + label
				}
				rows = append(rows, renderPart("day", struct {
					Day       Day
					Label     string
					Yesterday bool
				}{day, label, day.Date == yesterday}))
			}
			g.Blocks = append(g.Blocks, Block{ID: "forecast", Title: "17 日预报 · 点击日期看逐小时", Rows: rows})
		}
		if d.Weather != nil {
			for i := range g.Blocks {
				if !strings.HasPrefix(g.Blocks[i].ID, "clocks-") {
					g.Blocks[i].Source = renderPart("weather-source", d.Weather)
				}
			}
		}
		groups = append(groups, g)
	}
	for _, n := range d.NAS {
		g := Group{ID: n.ID, Name: n.Name}
		g.Blocks = append(g.Blocks, Block{ID: n.ID + "-state", Title: n.Name, Content: renderPart("nas-state", n)})
		s := n.Snapshot
		if n.Status != "online" && !history {
			g.Blocks = append(g.Blocks, Block{ID: n.ID + "-offline", Title: "实时指标", Content: renderPart("offline", n)})
			groups = append(groups, g)
			continue
		}
		if s == nil {
			groups = append(groups, g)
			continue
		}
		g.Blocks = append(g.Blocks, Block{ID: n.ID + "-system", Title: "CPU · 内存 · 运行时间", Content: renderPart("system", s)})
		b := Block{ID: n.ID + "-gpu", Title: "GPU"}
		for _, v := range s.GPUs {
			b.Rows = append(b.Rows, renderPart("gpu", v))
		}
		if len(b.Rows) == 0 {
			b.Content = renderPart("missing", "未检测到支持的 GPU 驱动")
		}
		g.Blocks = append(g.Blocks, b)
		b = Block{ID: n.ID + "-volumes", Title: fmt.Sprintf("存储空间 · %d 个", len(s.Volumes))}
		for _, v := range s.Volumes {
			b.Rows = append(b.Rows, renderPart("volume", v))
		}
		if len(b.Rows) == 0 {
			b.Content = renderPart("missing", "未提供：没有可读取的 /volN 存储卷")
		}
		g.Blocks = append(g.Blocks, b)
		external := 0
		for _, disk := range s.Disks {
			if disk.External {
				external++
			}
		}
		b = Block{ID: n.ID + "-disks", Title: fmt.Sprintf("物理磁盘 · %d 块 / 内置 %d / 外接 %d", len(s.Disks), len(s.Disks)-external, external)}
		for _, v := range s.Disks {
			b.Rows = append(b.Rows, renderPart("disk", v))
		}
		if len(b.Rows) == 0 {
			b.Content = renderPart("missing", "未提供：物理块设备不可见")
		}
		g.Blocks = append(g.Blocks, b)
		for _, kind := range []string{"network", "io"} {
			b = Block{ID: n.ID + "-" + kind, Title: "网络 · 收 / 发"}
			items := s.Network
			if kind == "io" {
				b.Title = "磁盘读写"
				items = s.DiskIO
			}
			for _, v := range items {
				b.Rows = append(b.Rows, renderPart("traffic", v))
			}
			if len(b.Rows) == 0 {
				b.Content = renderPart("missing", "未提供：等待设备或采样")
			}
			g.Blocks = append(g.Blocks, b)
		}
		b = Block{ID: n.ID + "-sensors", Title: "温度与风扇"}
		for _, v := range s.Sensors {
			b.Rows = append(b.Rows, renderPart("sensor", v))
		}
		if len(b.Rows) == 0 {
			b.Content = renderPart("missing", "未提供：传感器不可见或权限不足")
		}
		g.Blocks = append(g.Blocks, b)
		groups = append(groups, g)
	}
	return groups
}
func (a *App) dashboardPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	d := a.display()
	p := PageData{Display: d, Groups: a.groups(d, false), Title: "InkBoard · 天气与 NAS"}
	htmlResponse(w, "dashboard.html", p)
}
func (a *App) fragments(w http.ResponseWriter, r *http.Request) {
	d := a.display()
	htmlResponse(w, "groups", PageData{Display: d, Groups: a.groups(d, false)})
}
func htmlResponse(w http.ResponseWriter, name string, v any) {
	var b bytes.Buffer
	if e := templates.ExecuteTemplate(&b, name, v); e != nil {
		http.Error(w, "页面生成失败", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(b.Bytes())
}
func basicPages(blocks []Block, rows int) [][]Block {
	pages := [][]Block{}
	for _, b := range blocks {
		if len(b.Rows) == 0 {
			if rows >= 4 && b.ID == "current" && len(pages) == 1 && len(pages[0]) == 1 && pages[0][0].ID == "clocks-0" {
				pages[0] = append(pages[0], b)
				continue
			}
			pages = append(pages, []Block{b})
			continue
		}
		limit := rows
		if strings.HasSuffix(b.ID, "-disks") || strings.HasSuffix(b.ID, "-volumes") {
			if limit > 2 {
				limit = 2
			}
		}
		if strings.HasSuffix(b.ID, "-gpu") {
			limit = 1
		}
		for i := 0; i < len(b.Rows); i += limit {
			end := i + limit
			if end > len(b.Rows) {
				end = len(b.Rows)
			}
			part := b
			part.Rows = b.Rows[i:end]
			pages = append(pages, []Block{part})
		}
	}
	if len(pages) == 0 {
		pages = append(pages, []Block{})
	}
	return pages
}
func (a *App) basicPage(w http.ResponseWriter, r *http.Request) {
	d := a.display()
	history := r.URL.Query().Get("history") == "1"
	g := a.groups(d, history)
	if len(g) == 0 {
		http.Error(w, "暂无内容", 503)
		return
	}
	main := r.URL.Query().Get("main")
	selected := g[0]
	for _, v := range g {
		if v.ID == main {
			selected = v
			break
		}
	}
	rows, _ := strconv.Atoi(r.URL.Query().Get("rows"))
	if rows < 2 || rows > 10 {
		rows = 4
	}
	pages := basicPages(selected.Blocks, rows)
	i, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if i < 0 || i >= len(pages) {
		i = 0
	}
	suffix := ""
	if history {
		suffix = "&history=1"
	}
	prev, next := (i+len(pages)-1)%len(pages), (i+1)%len(pages)
	p := PageData{Display: d, Groups: g, Blocks: pages[i], Title: selected.Name, Main: selected.ID, Page: i + 1, Pages: len(pages), Basic: true, History: history, Prev: fmt.Sprintf("/basic?main=%s&page=%d&rows=%d%s", selected.ID, prev, rows, suffix), Next: fmt.Sprintf("/basic?main=%s&page=%d&rows=%d%s", selected.ID, next, rows, suffix)}
	htmlResponse(w, "basic.html", p)
}
func (a *App) hourlyPage(w http.ResponseWriter, r *http.Request) {
	d := a.display()
	date := r.URL.Query().Get("date")
	h := dayHours(d.Weather, date)
	if len(h) == 0 {
		http.Error(w, "该日期没有逐小时数据", 404)
		return
	}
	loc, _ := time.LoadLocation(d.Weather.City.Timezone)
	b := Block{ID: "detail", Title: date + " · " + d.Weather.City.Name}
	b.Source = renderPart("weather-source", d.Weather)
	for _, v := range h {
		b.Rows = append(b.Rows, renderPart("hour-detail", struct {
			Conditions Conditions
			Hour       string
		}{v, v.Time.In(loc).Format("15:04 MST")}))
	}
	i, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pages := basicPages([]Block{b}, 2)
	if i < 0 || i >= len(pages) {
		i = 0
	}
	p := PageData{Display: d, Blocks: pages[i], Title: date + " · 逐小时", Date: date, Page: i + 1, Pages: len(pages), Basic: true, Prev: fmt.Sprintf("/hourly?date=%s&page=%d", date, (i+len(pages)-1)%len(pages)), Next: fmt.Sprintf("/hourly?date=%s&page=%d", date, (i+1)%len(pages))}
	htmlResponse(w, "basic.html", p)
}
func (a *App) adminPage(w http.ResponseWriter, r *http.Request) {
	htmlResponse(w, "admin.html", struct {
		Role            string
		Bootstrap, Demo bool
	}{a.runtime.Role, a.config().PasswordHash == "", a.demo})
}
func (a *App) devicePage(w http.ResponseWriter, r *http.Request) { htmlResponse(w, "device.html", nil) }
func cleanText(s string) string                                  { return strings.TrimSpace(s) }
