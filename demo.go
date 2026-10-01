package main

import (
	"math"
	"time"
)

func number(v float64) *float64 { return &v }
func demoDisplay(now time.Time) Display {
	city := City{Name: "奥克兰", Country: "新西兰", Latitude: -36.85, Longitude: 174.76, Timezone: "Pacific/Auckland"}
	loc, _ := time.LoadLocation(city.Timezone)
	today := now.In(loc)
	midnight := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, loc)
	w := &Weather{SchemaVersion: 1, City: city, FetchedAt: now, Hourly: []Conditions{}, Days: []Day{}}
	for i := -1; i < 16; i++ {
		date := midnight.AddDate(0, 0, i)
		next := date.AddDate(0, 0, 1)
		w.Days = append(w.Days, Day{Date: date.Format("2006-01-02"), Minimum: number(10 + float64(i%4)), Maximum: number(18 + float64(i%5)), Probability: number(float64((i+1)%4) * 20), Precipitation: number(float64((i + 1) % 3)), Wind: number(18), UV: number(5), Sunrise: "06:49", Sunset: "19:28", Code: []int{2, 3, 61, 0}[(i+1)%4]})
		for t := date; t.Before(next); t = t.Add(time.Hour) {
			h := t.Hour()
			w.Hourly = append(w.Hourly, Conditions{Time: t, Temperature: number(15 + 5*math.Sin(float64(h-7)*math.Pi/12)), Apparent: number(14), Humidity: number(70), Wind: number(18), Direction: number(225), Gusts: number(26), Probability: number(20), Precipitation: number(0.2), Pressure: number(1016), Code: 2})
		}
	}
	w.Current = Conditions{Time: now, Temperature: number(18), Apparent: number(17), Humidity: number(68), Wind: number(18), Direction: number(225), Gusts: number(26), Precipitation: number(0), Pressure: number(1016), Code: 2}
	s := &Snapshot{SchemaVersion: 1, Version: version, ObservedAt: now, Hostname: "demo-nas", Platform: "linux/amd64", CPU: CPU{Model: "Synthetic Example CPU @ 3.10 GHz · 4 Cores / 4 Threads", Cores: 4, Usage: measured(12.4, "%"), Temperature: measured(42, "°C")}, Memory: Memory{Total: 8 << 30, Used: 2 << 30, Usage: measured(25, "%")}, Uptime: measured(258600, "s"), Capabilities: map[string]string{"intel_gpu": "演示数据"}}
	s.GPUs = []GPU{{ID: "card0", Name: "演示 Intel 核显", Kind: "Intel", Usage: measured(3, "%"), Temperature: unavailable("°C", "驱动未提供 GPU 温度"), MemoryUsed: unavailable("B", "共享显存"), MemoryTotal: unavailable("B", "共享显存")}}
	for i := 0; i < 5; i++ {
		s.Disks = append(s.Disks, Disk{ID: string(rune('a' + i)), Name: []string{"sda", "nvme0n1", "nvme1n1", "sdb", "sdc"}[i], Model: "Synthetic SSD 512GB NVMe Hardware Example", Interface: []string{"SATA", "NVMe", "NVMe", "USB", "USB"}[i], External: i >= 3, Kind: []string{"SSD", "NVMe SSD", "NVMe SSD", "SSD", "SSD"}[i], Size: uint64(256+i*256) << 30, Temperature: measured(float64(36+i), "°C"), Health: "unknown", HealthReason: "未主动查询 SMART：磁盘可能休眠，桥接协议尚未验证"})
		if i < 3 {
			s.Volumes = append(s.Volumes, Volume{ID: string(rune('a' + i)), Name: []string{"/vol1", "/vol2", "/vol3"}[i], Total: uint64(240+i*240) << 30, Used: uint64(48+i*60) << 30, Usage: measured(float64(20+i*10), "%"), Filesystem: []string{"Btrfs", "Btrfs", "ext4"}[i]})
		}
	}
	s.Network = []Traffic{{ID: "eth0", Name: "eth0", Read: measured(680000, "B/s"), Write: measured(120000, "B/s")}}
	s.Network = append(s.Network, Traffic{ID: "demo-new-interface", Name: "demo-new-interface", Read: unavailable("B/s", "等待首次采样，暂未计算速率（演示设备）"), Write: unavailable("B/s", "等待首次采样，暂未计算速率（演示设备）")})
	s.DiskIO = []Traffic{{ID: "nvme0n1", Name: "nvme0n1", Read: measured(2400000, "B/s"), Write: measured(450000, "B/s")}}
	s.Sensors = []Sensor{{Name: "CPU", Value: measured(42, "°C")}, {Name: "系统风扇", Value: measured(890, "RPM")}}
	last := now.Add(-3 * time.Hour)
	old := *s
	old.ObservedAt = last
	return Display{SchemaVersion: 1, Version: version, Now: now, Role: "hub", Demo: true, City: &city, Clocks: clocksAt(now, &city), Weather: w, NAS: []NASView{{ID: "local", Name: "演示 · 常开 NAS", Status: "online", Snapshot: s, LastReceived: &now}, {ID: "powerful", Name: "演示 · 定时关机 NAS", Status: "offline", Reason: "NAS 已离线；开机后会自动恢复", Snapshot: &old, LastReceived: &last}}}
}
