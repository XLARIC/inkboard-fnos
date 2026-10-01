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
	"unix":    func(t time.Time) int64 { return t.Unix() },
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
	Display        Display
	Groups         []Group
	Title, Main    string
	Basic, History bool
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
	return a.groupsForDate(d, history, "")
}
func (a *App) groupsForDate(d Display, history bool, selectedDate string) []Group {
	return a.groupsForSelection(d, history, selectedDate, "")
}
func (a *App) groupsForSelection(d Display, history bool, selectedDate, selectedHour string) []Group {
	groups := []Group{}
	if d.Role == "hub" {
		g := Group{ID: "weather", Name: "天气"}
		g.Blocks = append(g.Blocks, Block{ID: "clocks-0", Title: "城市时间", Content: renderPart("clocks", d.Clocks)})
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
			heroWeather := *w
			currentTitle := "现在 · " + w.City.Name
			if w.Error != "" {
				currentTitle = "缓存 · " + currentTitle
			}
			hourUnix, _ := strconv.ParseInt(selectedHour, 10, 64)
			loc, _ := time.LoadLocation(w.City.Timezone)
			for _, h := range w.Hourly {
				if h.Time.Unix() == hourUnix {
					heroWeather.Current = h
					currentTitle = h.Time.In(loc).Format("01-02 15:04 MST") + " · 分时详情"
					for _, day := range w.Days {
						if day.Date == h.Time.In(loc).Format("2006-01-02") {
							currentDay = day
							break
						}
					}
					break
				}
			}
			hero := struct {
				Weather *Weather
				Day     Day
				Demo    bool
			}{&heroWeather, currentDay, d.Demo}
			g.Blocks = append(g.Blocks, Block{ID: "current", Title: currentTitle, Content: renderPart("current", hero)})
			date := today
			if len(dayHours(w, selectedDate)) > 0 {
				date = selectedDate
			}
			hours := dayHours(w, date)
			rows := []template.HTML{}
			for _, h := range hours {
				rows = append(rows, renderPart("hour", struct {
					Conditions Conditions
					Hour       string
				}{h, h.Time.In(loc).Format("15:04 MST")}))
			}
			title := "今日分时"
			if date != today {
				title = date + " · 分时天气"
			}
			g.Blocks = append(g.Blocks, Block{ID: "today", Title: title, Rows: rows})
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
			g.Blocks = append(g.Blocks, Block{ID: "forecast", Title: "昨天 · 今天 · 未来 15 天", Rows: rows})
		}
		if d.Weather != nil {
			for i := range g.Blocks {
				if g.Blocks[i].ID == "current" {
					g.Blocks[i].Source = renderPart("weather-source", d.Weather)
				}
			}
		}
		g.Blocks = append(g.Blocks, Block{ID: "controls", Title: "INKBOARD", Content: renderPart("controls", d)})
		groups = append(groups, g)
	}
	for _, n := range d.NAS {
		g := Group{ID: n.ID, Name: n.Name}
		if history && n.Status != "online" {
			n.Reason = "历史快照，以下指标不是实时状态。" + n.Reason
		}
		stateTitle := n.Name
		if history && n.Status != "online" {
			stateTitle = "历史快照 · " + n.Name
		}
		g.Blocks = append(g.Blocks, Block{ID: n.ID + "-state", Title: stateTitle, Content: renderPart("nas-state", n)})
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
	p := PageData{Display: d, Groups: a.groupsForSelection(d, false, r.URL.Query().Get("date"), r.URL.Query().Get("hour")), Main: r.URL.Query().Get("main"), Title: "InkBoard · 天气与 NAS"}
	htmlResponse(w, "dashboard.html", p)
}
func (a *App) fragments(w http.ResponseWriter, r *http.Request) {
	d := a.display()
	history := r.URL.Query().Get("history") == "1"
	htmlResponse(w, "groups", PageData{Display: d, Groups: a.groupsForSelection(d, history, r.URL.Query().Get("date"), r.URL.Query().Get("hour"))})
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
func (a *App) basicPage(w http.ResponseWriter, r *http.Request) {
	d := a.display()
	history := r.URL.Query().Get("history") == "1"
	g := a.groupsForSelection(d, history, r.URL.Query().Get("date"), r.URL.Query().Get("hour"))
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
	p := PageData{Display: d, Groups: g, Title: selected.Name + " · InkBoard", Main: selected.ID, Basic: true, History: history}
	htmlResponse(w, "dashboard.html", p)
}
func (a *App) hourlyPage(w http.ResponseWriter, r *http.Request) {
	date := r.URL.Query().Get("date")
	h := dayHours(a.display().Weather, date)
	if len(h) == 0 {
		http.Error(w, "该日期没有逐小时数据", 404)
		return
	}
	// Old bookmarks retain a useful destination; details stay in the weather screen.
	d := a.display()
	htmlResponse(w, "dashboard.html", PageData{Display: d, Groups: a.groupsForSelection(d, false, date, r.URL.Query().Get("hour")), Title: date + " · 分时天气", Main: "weather"})
}
func (a *App) adminPage(w http.ResponseWriter, r *http.Request) {
	htmlResponse(w, "admin.html", struct {
		Role            string
		Bootstrap, Demo bool
	}{a.runtime.Role, a.config().PasswordHash == "", a.demo})
}
func (a *App) devicePage(w http.ResponseWriter, r *http.Request) { htmlResponse(w, "device.html", nil) }
func cleanText(s string) string                                  { return strings.TrimSpace(s) }
