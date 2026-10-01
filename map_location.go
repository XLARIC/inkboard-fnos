package main

import (
	"errors"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

type MapLocation struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Source    string  `json:"source"`
	Warning   string  `json:"warning,omitempty"`
}

var coordinatePair = regexp.MustCompile(`^\s*([+-]?\d+(?:\.\d+)?)\s*[,，]\s*([+-]?\d+(?:\.\d+)?)\s*$`)
var googlePlaceCoordinates = regexp.MustCompile(`!3d([+-]?\d+(?:\.\d+)?)!4d([+-]?\d+(?:\.\d+)?)`)
var googleMapCenter = regexp.MustCompile(`@([+-]?\d+(?:\.\d+)?),([+-]?\d+(?:\.\d+)?)(?:,|/|$)`)

func mapCoordinates(lat, lon, source, warning string) (MapLocation, error) {
	a, e1 := strconv.ParseFloat(lat, 64)
	b, e2 := strconv.ParseFloat(lon, 64)
	if e1 != nil || e2 != nil || math.IsNaN(a) || math.IsNaN(b) || math.IsInf(a, 0) || math.IsInf(b, 0) || a < -90 || a > 90 || b < -180 || b > 180 {
		return MapLocation{}, errors.New("坐标超出范围：先填写纬度（-90 到 90），再填写经度（-180 到 180）")
	}
	return MapLocation{Latitude: a, Longitude: b, Source: source, Warning: warning}, nil
}

// This parses only user-supplied text. It never fetches a URL, follows a short
// link, reads a Google Maps page, or sends an address to a geocoding API.
func parseMapLocation(input string) (MapLocation, error) {
	input = strings.TrimSpace(input)
	if len(input) == 0 || len(input) > 8192 {
		return MapLocation{}, errors.New("请粘贴坐标或 Google Maps 完整链接（最多 8192 字节）")
	}
	if pair := coordinatePair.FindStringSubmatch(input); pair != nil {
		return mapCoordinates(pair[1], pair[2], "复制的坐标", "")
	}
	u, err := url.Parse(input)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return MapLocation{}, errors.New("请粘贴十进制坐标，或以 https:// 开头的 Google Maps 完整链接")
	}
	host := strings.ToLower(u.Hostname())
	if host == "maps.app.goo.gl" || host == "goo.gl" {
		return MapLocation{}, errors.New("这是分享短链接。请先在浏览器打开它，再复制地址栏完整链接，或右键目标点复制坐标")
	}
	// An explicit allowlist avoids treating another website's parameters as a map location.
	allowed := false
	for _, domain := range []string{"google.com", "google.co.nz", "google.com.au", "google.co.uk", "google.ca", "google.cn", "google.com.hk", "google.co.jp", "google.de", "google.fr", "google.es", "google.it", "google.co.in", "google.com.sg", "google.com.tw"} {
		if host == domain || host == "www."+domain || host == "maps."+domain {
			allowed = true
			break
		}
	}
	if !allowed || (!strings.HasPrefix(host, "maps.") && u.Path != "/maps" && !strings.HasPrefix(u.Path, "/maps/")) {
		return MapLocation{}, errors.New("只支持 Google Maps 地图链接；也可直接粘贴“纬度, 经度”")
	}
	if strings.Contains(u.Path, "/dir/") || u.Query().Get("map_action") == "pano" {
		return MapLocation{}, errors.New("路线或街景链接不能确定唯一选点，请复制目标点的坐标")
	}
	points := googlePlaceCoordinates.FindAllStringSubmatch(u.Path, -1)
	if len(points) > 0 {
		point, err := mapCoordinates(points[0][1], points[0][2], "Google Maps 地点坐标", "请确认与地图上选中的位置一致。Google Maps 完整链接的格式可能变化。")
		if err != nil {
			return MapLocation{}, err
		}
		// Maps can repeat the same place in search context and selected-place data.
		for _, pair := range points[1:] {
			other, err := mapCoordinates(pair[1], pair[2], "", "")
			if err != nil {
				return MapLocation{}, err
			}
			if other.Latitude != point.Latitude || other.Longitude != point.Longitude {
				return MapLocation{}, errors.New("链接包含多个地点，请右键目标点复制唯一坐标")
			}
		}
		return point, nil
	}
	if u.Query().Get("query_place_id") != "" {
		return MapLocation{}, errors.New("此链接使用地点编号，无法离线读取坐标，请右键目标点复制坐标")
	}
	for _, key := range []string{"query", "q"} {
		if pair := coordinatePair.FindStringSubmatch(u.Query().Get(key)); pair != nil {
			return mapCoordinates(pair[1], pair[2], "Google Maps 坐标", "")
		}
	}
	if pair := googleMapCenter.FindStringSubmatch(u.Path); pair != nil {
		return mapCoordinates(pair[1], pair[2], "Google Maps 视图中心", "这是地图视图中心，可能不是选中的地点。建议右键目标点复制坐标；确认无误后再使用。")
	}
	for _, key := range []string{"center", "ll"} {
		if pair := coordinatePair.FindStringSubmatch(u.Query().Get(key)); pair != nil {
			return mapCoordinates(pair[1], pair[2], "Google Maps 视图中心", "这是地图视图中心，请核对后使用；精确选点建议右键复制坐标。")
		}
	}
	return MapLocation{}, errors.New("链接没有可识别的坐标。请在 Google Maps 右键目标点，点击顶部坐标复制后粘贴")
}
