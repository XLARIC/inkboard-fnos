package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func validateCity(c City) error {
	if len(c.Address) > 600 || (c.AddressSource != "" && c.AddressSource != "osm") {
		return errors.New("详细地址无效")
	}
	if strings.TrimSpace(c.Name) == "" || len(c.Name) > 160 || math.IsNaN(c.Latitude) || math.IsNaN(c.Longitude) || math.IsInf(c.Latitude, 0) || math.IsInf(c.Longitude, 0) || c.Latitude < -90 || c.Latitude > 90 || c.Longitude < -180 || c.Longitude > 180 {
		return errors.New("城市信息无效")
	}
	if _, e := time.LoadLocation(c.Timezone); e != nil {
		return errors.New("城市时区无效")
	}
	return nil
}
func clocksAt(now time.Time, c *City) []Clock {
	return configuredClocks(now, c, nil)
}
func defaultClockCities() []ClockCity {
	return []ClockCity{{"纽约", "America/New_York"}, {"上海", "Asia/Shanghai"}}
}
func configuredClocks(now time.Time, c *City, cities []ClockCity) []Clock {
	if cities == nil {
		cities = defaultClockCities()
	}
	zone, name := "UTC", "所在地未设置"
	if c != nil {
		zone = c.Timezone
		name = c.Name
	}
	local, e := time.LoadLocation(zone)
	if e != nil {
		local = time.UTC
	}
	_, base := now.In(local).Zone()
	week := []string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}
	out := []Clock{}
	for _, v := range append([]ClockCity{{name, zone}}, cities...) {
		l, e := time.LoadLocation(v.Timezone)
		if e != nil {
			l = time.UTC
		}
		t := now.In(l)
		_, offset := t.Zone()
		diff := "所在地"
		if len(out) > 0 {
			diff = formatDifference(offset - base)
		}
		out = append(out, Clock{Name: v.Name, Zone: v.Timezone, Date: t.Format("2006-01-02"), Weekday: week[t.Weekday()], Time: t.Format("15:04"), Difference: diff, Offset: offset})
	}
	return out
}
func formatDifference(seconds int) string {
	if seconds == 0 {
		return "与本地同时间"
	}
	word := "快"
	if seconds < 0 {
		word = "慢"
		seconds = -seconds
	}
	h, m := seconds/3600, seconds%3600/60
	if m == 0 {
		return fmt.Sprintf("比本地%s %d 小时", word, h)
	}
	return fmt.Sprintf("比本地%s %d 小时 %d 分", word, h, m)
}
func locateCity(ctx context.Context, client *http.Client, latitude, longitude float64) (City, error) {
	c := City{Name: "定位点", Latitude: latitude, Longitude: longitude, Timezone: "UTC"}
	if e := validateCity(c); e != nil {
		return City{}, e
	}
	return resolveTimezone(ctx, client, c)
}
func resolveTimezone(ctx context.Context, client *http.Client, c City) (City, error) {
	c.Timezone = "UTC"
	if e := validateCity(c); e != nil {
		return City{}, e
	}
	q := url.Values{"latitude": {strconv.FormatFloat(c.Latitude, 'f', -1, 64)}, "longitude": {strconv.FormatFloat(c.Longitude, 'f', -1, 64)}, "timezone": {"auto"}, "forecast_days": {"1"}, "current": {"temperature_2m"}}
	var v struct {
		Timezone string `json:"timezone"`
	}
	if e := getJSON(ctx, client, "https://api.open-meteo.com/v1/forecast?"+q.Encode(), &v); e != nil {
		return City{}, e
	}
	c.Timezone = v.Timezone
	return c, validateCity(c)
}
func sameCity(a, b City) bool {
	return a.Latitude == b.Latitude && a.Longitude == b.Longitude && a.Timezone == b.Timezone && a.Name == b.Name
}
func getJSON(ctx context.Context, client *http.Client, address string, v any) error {
	req, e := http.NewRequestWithContext(ctx, "GET", address, nil)
	if e != nil {
		return e
	}
	req.Header.Set("User-Agent", "InkBoard/"+version+" (+https://github.com/XLARIC/inkboard-fnos)")
	res, e := client.Do(req)
	if e != nil {
		return errors.New("天气服务暂时无法连接")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("天气服务返回 HTTP %d", res.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if e != nil {
		return e
	}
	if len(b) == 2<<20 {
		return errors.New("天气响应过大")
	}
	return json.Unmarshal(b, v)
}

type omResponse struct {
	Current map[string]json.RawMessage   `json:"current"`
	Hourly  map[string][]json.RawMessage `json:"hourly"`
	Daily   map[string][]json.RawMessage `json:"daily"`
}

func rawFloat(m map[string]json.RawMessage, k string) *float64 {
	r, ok := m[k]
	if !ok || string(r) == "null" {
		return nil
	}
	var v float64
	if json.Unmarshal(r, &v) != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return &v
}
func arrayFloat(m map[string][]json.RawMessage, k string, i int) *float64 {
	a := m[k]
	if i >= len(a) {
		return nil
	}
	return rawFloat(map[string]json.RawMessage{k: a[i]}, k)
}
func unixFrom(v *float64) time.Time {
	if v == nil {
		return time.Time{}
	}
	return time.Unix(int64(*v), 0).UTC()
}
func floatCode(v *float64) int {
	if v == nil {
		return -1
	}
	return int(*v)
}
func fetchWeather(ctx context.Context, client *http.Client, c City, now time.Time) (*Weather, error) {
	q := url.Values{"latitude": {strconv.FormatFloat(c.Latitude, 'f', -1, 64)}, "longitude": {strconv.FormatFloat(c.Longitude, 'f', -1, 64)}, "timezone": {c.Timezone}, "past_days": {"1"}, "forecast_days": {"16"}, "timeformat": {"unixtime"}, "wind_speed_unit": {"kmh"}}
	q.Set("current", "temperature_2m,apparent_temperature,relative_humidity_2m,weather_code,wind_speed_10m,wind_direction_10m,wind_gusts_10m,precipitation,pressure_msl")
	q.Set("hourly", "temperature_2m,apparent_temperature,relative_humidity_2m,weather_code,wind_speed_10m,wind_direction_10m,wind_gusts_10m,precipitation,precipitation_probability,pressure_msl")
	q.Set("daily", "temperature_2m_min,temperature_2m_max,weather_code,precipitation_probability_max,precipitation_sum,wind_speed_10m_max,uv_index_max,sunrise,sunset")
	var raw omResponse
	if e := getJSON(ctx, client, "https://api.open-meteo.com/v1/forecast?"+q.Encode(), &raw); e != nil {
		return nil, e
	}
	return normalizeWeather(raw, c, now)
}
func normalizeWeather(r omResponse, c City, now time.Time) (*Weather, error) {
	if len(r.Daily["time"]) != 17 || len(r.Hourly["time"]) < 17*23 || rawFloat(r.Current, "temperature_2m") == nil {
		return nil, errors.New("天气服务未返回完整的 17 天数据")
	}
	w := &Weather{SchemaVersion: 1, City: c, FetchedAt: now.UTC(), Hourly: []Conditions{}, Days: []Day{}}
	condition := func(m map[string]json.RawMessage) Conditions {
		return Conditions{Time: unixFrom(rawFloat(m, "time")), Temperature: rawFloat(m, "temperature_2m"), Apparent: rawFloat(m, "apparent_temperature"), Humidity: rawFloat(m, "relative_humidity_2m"), Wind: rawFloat(m, "wind_speed_10m"), Direction: rawFloat(m, "wind_direction_10m"), Gusts: rawFloat(m, "wind_gusts_10m"), Precipitation: rawFloat(m, "precipitation"), Probability: rawFloat(m, "precipitation_probability"), Pressure: rawFloat(m, "pressure_msl"), Code: floatCode(rawFloat(m, "weather_code"))}
	}
	w.Current = condition(r.Current)
	for i := range r.Hourly["time"] {
		m := map[string]json.RawMessage{}
		for k, a := range r.Hourly {
			if i < len(a) {
				m[k] = a[i]
			}
		}
		h := condition(m)
		if h.Time.IsZero() {
			return nil, errors.New("天气小时数据缺少时间")
		}
		w.Hourly = append(w.Hourly, h)
	}
	loc, _ := time.LoadLocation(c.Timezone)
	base := now.In(loc)
	base = time.Date(base.Year(), base.Month(), base.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -1)
	for i := range r.Daily["time"] {
		// Daily labels follow civil dates, never a fixed 86400-second step across DST.
		label := base.AddDate(0, 0, i).Format("2006-01-02")
		sr, ss := unixFrom(arrayFloat(r.Daily, "sunrise", i)), unixFrom(arrayFloat(r.Daily, "sunset", i))
		rise, set := "—", "—"
		if !sr.IsZero() {
			rise = sr.In(loc).Format("15:04")
		}
		if !ss.IsZero() {
			set = ss.In(loc).Format("15:04")
		}
		w.Days = append(w.Days, Day{Date: label, Minimum: arrayFloat(r.Daily, "temperature_2m_min", i), Maximum: arrayFloat(r.Daily, "temperature_2m_max", i), Probability: arrayFloat(r.Daily, "precipitation_probability_max", i), Precipitation: arrayFloat(r.Daily, "precipitation_sum", i), Wind: arrayFloat(r.Daily, "wind_speed_10m_max", i), UV: arrayFloat(r.Daily, "uv_index_max", i), Sunrise: rise, Sunset: set, Code: floatCode(arrayFloat(r.Daily, "weather_code", i))})
	}
	return w, nil
}
func searchCities(ctx context.Context, client *http.Client, name string) ([]City, error) {
	if len(strings.TrimSpace(name)) < 2 || len(name) > 100 {
		return nil, errors.New("请输入至少两个字的城市名称")
	}
	var result struct {
		Results []struct {
			Name, Country, Timezone string
			Latitude, Longitude     float64
		} `json:"results"`
	}
	q := url.Values{"name": {name}, "count": {"8"}, "language": {"zh"}, "format": {"json"}}
	if e := getJSON(ctx, client, "https://geocoding-api.open-meteo.com/v1/search?"+q.Encode(), &result); e != nil {
		return nil, e
	}
	cities := []City{}
	for _, x := range result.Results {
		c := City{Name: x.Name, Country: x.Country, Latitude: x.Latitude, Longitude: x.Longitude, Timezone: x.Timezone}
		if validateCity(c) == nil {
			cities = append(cities, c)
		}
	}
	return cities, nil
}
func weatherText(code int) string {
	switch {
	case code == 0:
		return "晴"
	case code <= 3 && code >= 1:
		return "多云"
	case code == 45 || code == 48:
		return "雾"
	case code >= 51 && code <= 57:
		return "毛毛雨"
	case code >= 61 && code <= 67:
		return "雨"
	case code >= 71 && code <= 77:
		return "雪"
	case code >= 80 && code <= 82:
		return "阵雨"
	case code == 85 || code == 86:
		return "阵雪"
	case code >= 95:
		return "雷雨"
	default:
		return "未提供"
	}
}
func dayHours(w *Weather, date string) []Conditions {
	out := []Conditions{}
	if w == nil {
		return out
	}
	loc, e := time.LoadLocation(w.City.Timezone)
	if e != nil {
		return out
	}
	for _, h := range w.Hourly {
		if h.Time.In(loc).Format("2006-01-02") == date {
			out = append(out, h)
		}
	}
	return out
}
