package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type counter struct{ Read, Write uint64 }
type Collector struct {
	mu                      sync.Mutex
	root                    string
	previousCPU             [2]uint64
	previousNet, previousIO map[string]counter
	previousTime            time.Time
	diskCache               map[string]Disk
	slowAt                  time.Time
	gpuAt                   time.Time
	gpuCache                []GPU
}

func newCollector(root string) *Collector {
	return &Collector{root: root, previousNet: map[string]counter{}, previousIO: map[string]counter{}, diskCache: map[string]Disk{}}
}
func (c *Collector) path(p string) string {
	if c.root == "" {
		return p
	}
	return filepath.Join(c.root, p)
}
func (c *Collector) read(p string) string {
	b, _ := os.ReadFile(c.path(p))
	return strings.TrimSpace(string(b))
}
func parseUint(s string) uint64   { v, _ := strconv.ParseUint(strings.TrimSpace(s), 10, 64); return v }
func parseFloat(s string) float64 { v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64); return v }
func cpuCounters(text string) (total, idle uint64) {
	for _, line := range strings.Split(text, "\n") {
		a := strings.Fields(line)
		if len(a) > 5 && a[0] == "cpu" {
			for i := 1; i < len(a) && i <= 8; i++ {
				total += parseUint(a[i])
			}
			idle = parseUint(a[4]) + parseUint(a[5])
			return
		}
	}
	return
}
func cpuPercent(previous, current [2]uint64) Metric {
	if current[0] <= previous[0] || current[1] < previous[1] {
		return unavailable("%", "等待两个有效采样")
	}
	dt, di := current[0]-previous[0], current[1]-previous[1]
	if di > dt {
		return unavailable("%", "CPU 计数器已重置")
	}
	return measured(100*float64(dt-di)/float64(dt), "%")
}
func parseMemory(text string) Memory {
	m := map[string]uint64{}
	for _, line := range strings.Split(text, "\n") {
		a := strings.Fields(line)
		if len(a) >= 2 {
			m[strings.TrimSuffix(a[0], ":")] = parseUint(a[1]) * 1024
		}
	}
	total := m["MemTotal"]
	available, availableProvided := m["MemAvailable"]
	if !availableProvided {
		available = m["MemFree"] + m["Buffers"] + m["Cached"]
	}
	if available > total {
		available = total
	}
	mem := Memory{Total: total, Used: total - available, Usage: unavailable("%", "内存统计不可用")}
	if total > 0 {
		mem.Usage = measured(float64(mem.Used)*100/float64(total), "%")
	}
	return mem
}
func parseNetwork(text string) map[string]counter {
	out := map[string]counter{}
	for _, line := range strings.Split(text, "\n") {
		a := strings.SplitN(line, ":", 2)
		if len(a) != 2 {
			continue
		}
		f := strings.Fields(a[1])
		if len(f) >= 9 {
			out[strings.TrimSpace(a[0])] = counter{parseUint(f[0]), parseUint(f[8])}
		}
	}
	return out
}
func parseDiskIO(text string) map[string]counter {
	out := map[string]counter{}
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) >= 14 {
			out[f[2]] = counter{parseUint(f[5]) * 512, parseUint(f[9]) * 512}
		}
	}
	return out
}
func rate(old, cur counter, elapsed float64) (Metric, Metric) {
	if elapsed <= 0 || cur.Read < old.Read || cur.Write < old.Write {
		return unavailable("B/s", "等待有效采样"), unavailable("B/s", "等待有效采样")
	}
	return measured(float64(cur.Read-old.Read)/elapsed, "B/s"), measured(float64(cur.Write-old.Write)/elapsed, "B/s")
}
func (c *Collector) Collect(now time.Time) Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	name, _ := os.Hostname()
	s := Snapshot{SchemaVersion: 1, Version: version, ObservedAt: now.UTC(), Hostname: name, Platform: runtime.GOOS + "/" + runtime.GOARCH, CPU: CPU{Cores: runtime.NumCPU(), Usage: unavailable("%", "仅支持飞牛 Linux 主机"), Temperature: unavailable("°C", "未找到 CPU 温度传感器")}, Memory: Memory{Usage: unavailable("%", "内存统计不可用")}, GPUs: []GPU{}, Disks: []Disk{}, Volumes: []Volume{}, DiskIO: []Traffic{}, Network: []Traffic{}, Sensors: []Sensor{}, Uptime: unavailable("s", "运行时间不可用"), Capabilities: map[string]string{}}
	if runtime.GOOS != "linux" && c.root == "" {
		s.Capabilities["system"] = "主机采集仅支持 Linux；其他系统可运行纯天气服务"
		return s
	}
	total, idle := cpuCounters(c.read("/proc/stat"))
	if c.previousCPU[0] == 0 {
		s.CPU.Usage = unavailable("%", "等待两个有效采样")
	} else {
		s.CPU.Usage = cpuPercent(c.previousCPU, [2]uint64{total, idle})
	}
	c.previousCPU = [2]uint64{total, idle}
	for _, line := range strings.Split(c.read("/proc/cpuinfo"), "\n") {
		a := strings.SplitN(line, ":", 2)
		if len(a) == 2 && (strings.TrimSpace(a[0]) == "model name" || strings.TrimSpace(a[0]) == "Hardware") {
			s.CPU.Model = strings.TrimSpace(a[1])
			break
		}
	}
	s.Memory = parseMemory(c.read("/proc/meminfo"))
	u := strings.Fields(c.read("/proc/uptime"))
	if len(u) > 0 {
		s.Uptime = measured(parseFloat(u[0]), "s")
	}
	s.Sensors, s.CPU.Temperature = c.sensors()
	s.Disks = c.disks(now)
	s.Volumes = c.volumes()
	elapsed := 0.0
	if !c.previousTime.IsZero() {
		elapsed = now.Sub(c.previousTime).Seconds()
	}
	netCounters := parseNetwork(c.read("/proc/net/dev"))
	for n, v := range netCounters {
		if !c.physicalNetwork(n) {
			continue
		}
		r, w := unavailable("B/s", "等待有效采样"), unavailable("B/s", "等待有效采样")
		if old, ok := c.previousNet[n]; ok {
			r, w = rate(old, v, elapsed)
		}
		s.Network = append(s.Network, Traffic{ID: n, Name: n, Read: r, Write: w})
	}
	ioCounters := parseDiskIO(c.read("/proc/diskstats"))
	for _, d := range s.Disks {
		v, ok := ioCounters[d.ID]
		if !ok {
			continue
		}
		r, w := unavailable("B/s", "等待有效采样"), unavailable("B/s", "等待有效采样")
		if old, ok := c.previousIO[d.ID]; ok {
			r, w = rate(old, v, elapsed)
		}
		s.DiskIO = append(s.DiskIO, Traffic{ID: d.ID, Name: d.Name, Read: r, Write: w})
	}
	c.previousNet, c.previousIO, c.previousTime = netCounters, ioCounters, now
	if now.Sub(c.gpuAt) >= 15*time.Second || c.gpuAt.IsZero() {
		c.gpuCache = c.gpus()
		c.gpuAt = now
	}
	s.GPUs = append([]GPU{}, c.gpuCache...)
	s.Capabilities["cpu"] = "procfs"
	s.Capabilities["memory"] = "MemTotal - MemAvailable"
	s.Capabilities["volumes"] = "独立 /volN 存储卷；排除子卷重复挂载"
	if len(s.GPUs) == 0 {
		s.Capabilities["gpu"] = "未检测到支持的 GPU 驱动"
	}
	if len(s.Disks) == 0 {
		s.Capabilities["disks"] = "物理块设备不可见"
	}
	sort.Slice(s.Network, func(i, j int) bool { return s.Network[i].ID < s.Network[j].ID })
	return s
}
func (c *Collector) physicalNetwork(n string) bool {
	if n == "lo" {
		return false
	}
	if master, e := filepath.EvalSymlinks(c.path("/sys/class/net/" + n + "/master")); e == nil {
		if _, e := os.Stat(filepath.Join(master, "bonding")); e == nil {
			return false
		}
	}
	if _, e := os.Stat(c.path("/sys/class/net/" + n + "/device")); e == nil {
		return true
	}
	_, e := os.Stat(c.path("/sys/class/net/" + n + "/bonding"))
	return e == nil
}
func (c *Collector) sensors() ([]Sensor, Metric) {
	out := []Sensor{}
	cpu := unavailable("°C", "未找到 CPU 温度传感器")
	dirs, _ := filepath.Glob(c.path("/sys/class/hwmon/hwmon*"))
	for _, dir := range dirs {
		name := strings.TrimSpace(readText(filepath.Join(dir, "name")))
		if name == "nvme" || name == "drivetemp" {
			continue
		}
		files, _ := filepath.Glob(filepath.Join(dir, "temp*_input"))
		for _, p := range files {
			b, e := os.ReadFile(p)
			if e != nil {
				continue
			}
			v, e := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
			if e != nil || v <= -50000 || v >= 150000 {
				continue
			}
			label := strings.TrimSpace(readText(strings.TrimSuffix(p, "_input") + "_label"))
			if label == "" {
				label = filepath.Base(strings.TrimSuffix(p, "_input"))
			}
			m := measured(v/1000, "°C")
			out = append(out, Sensor{Name: name + " · " + label, Value: m})
			if name == "coretemp" || name == "k10temp" || strings.Contains(name, "cpu") {
				if cpu.Value == nil || *m.Value > *cpu.Value {
					cpu = m
				}
			}
		}
		fans, _ := filepath.Glob(filepath.Join(dir, "fan*_input"))
		for _, p := range fans {
			v, e := strconv.ParseFloat(strings.TrimSpace(readText(p)), 64)
			if e == nil {
				out = append(out, Sensor{Name: name + " · " + filepath.Base(p), Value: measured(v, "RPM")})
			}
		}
	}
	return out, cpu
}
func readText(p string) string { b, _ := os.ReadFile(p); return string(b) }
func physicalDiskName(n string) bool {
	return !strings.HasPrefix(n, "loop") && !strings.HasPrefix(n, "ram") && !strings.HasPrefix(n, "zram") && !strings.HasPrefix(n, "dm-") && !strings.HasPrefix(n, "md") && !strings.HasPrefix(n, "sr")
}
func (c *Collector) disks(now time.Time) []Disk {
	out := []Disk{}
	entries, _ := os.ReadDir(c.path("/sys/class/block"))
	slow := now.Sub(c.slowAt) >= 5*time.Minute || c.slowAt.IsZero()
	for _, e := range entries {
		n := e.Name()
		base := "/sys/class/block/" + n
		if !physicalDiskName(n) {
			continue
		}
		if _, err := os.Stat(c.path(base + "/partition")); err == nil {
			continue
		}
		if c.read(base+"/size") == "" {
			continue
		}
		kind := "SSD"
		if c.read(base+"/queue/rotational") == "1" {
			kind = "HDD"
		}
		if strings.HasPrefix(n, "nvme") {
			kind = "NVMe SSD"
		}
		d := Disk{ID: n, Name: n, Model: c.read(base + "/device/model"), Kind: kind, Size: parseUint(c.read(base+"/size")) * 512, Temperature: unavailable("°C", "温度不可用或磁盘休眠"), Health: "未提供", HealthReason: "未读取 SMART"}
		deviceLink, _ := filepath.EvalSymlinks(c.path(base + "/device"))
		d.Interface = "SATA"
		if strings.HasPrefix(n, "nvme") {
			d.Interface = "NVMe"
		}
		if strings.Contains(deviceLink, "/usb") {
			d.External = true
			d.Interface = "USB"
		}
		if cached, ok := c.diskCache[n]; ok && cached.Model == d.Model && cached.Size == d.Size {
			d.Temperature, d.Health, d.HealthReason, d.CheckedAt = cached.Temperature, cached.Health, cached.HealthReason, cached.CheckedAt
		}
		paths, _ := filepath.Glob(c.path(base + "/device/hwmon/hwmon*/temp*_input"))
		for _, p := range paths {
			if !slow || c.read(base+"/device/power/runtime_status") != "active" {
				break
			}
			v, e := strconv.ParseFloat(strings.TrimSpace(readText(p)), 64)
			if e == nil && v > 0 && v < 150000 {
				d.Temperature = measured(v/1000, "°C")
				stamp := now.UTC()
				d.CheckedAt = &stamp
				break
			}
		}
		// Only an explicit ATA type is queried. Unsupported low-power checks stop
		// before SMART reads; USB/SCSI bridges never undergo type autodetection.
		if slow && c.root == "" && strings.HasPrefix(n, "sd") {
			devicePath, _ := filepath.EvalSymlinks(c.path(base + "/device"))
			if strings.Contains(devicePath, "/ata") {
				c.smart(&d, now)
			} else {
				d.HealthReason = "桥接设备采用保守模式，未主动查询 SMART"
			}
		}
		c.diskCache[n] = d
		out = append(out, d)
	}
	if slow {
		c.slowAt = now
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func runTool(name string, args ...string) ([]byte, error) {
	p, e := exec.LookPath(name)
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, p, args...)
	return cmd.Output()
}
func (c *Collector) smart(d *Disk, now time.Time) {
	b, e := runTool("smartctl", "-j", "-n", "standby,3,5", "-d", "ata", "-H", "-A", "/dev/"+d.ID)
	if e != nil && (len(b) == 0 || !strings.Contains(string(b), "smart_status")) {
		d.HealthReason = "SMART 不可用、权限不足或磁盘休眠"
		return
	}
	var r struct {
		Temperature struct{ Current *float64 }
		SmartStatus struct{ Passed *bool } `json:"smart_status"`
	}
	if json.Unmarshal(b, &r) != nil {
		return
	}
	if r.Temperature.Current != nil {
		d.Temperature = measured(*r.Temperature.Current, "°C")
	}
	if r.SmartStatus.Passed != nil {
		if *r.SmartStatus.Passed {
			d.Health = "正常"
		} else {
			d.Health = "告警"
		}
		d.HealthReason = ""
	}
	stamp := now.UTC()
	d.CheckedAt = &stamp
}

var volumePattern = regexp.MustCompile(`^/vol[0-9]+$`)

func (c *Collector) volumes() []Volume {
	out := []Volume{}
	seen := map[string]bool{}
	for _, line := range strings.Split(c.read("/proc/self/mountinfo"), "\n") {
		f := strings.Fields(line)
		if len(f) < 7 {
			continue
		}
		path := strings.ReplaceAll(f[4], `\040`, " ")
		if !volumePattern.MatchString(path) {
			continue
		}
		id := f[2]
		if seen[id] {
			continue
		}
		v, e := volumeStats(c.path(path))
		if e != nil {
			out = append(out, Volume{ID: id, Name: path, Usage: unavailable("%", "存储卷统计权限不足")})
			continue
		}
		seen[id] = true
		v.ID = id
		v.Name = path
		for i, item := range f {
			if item == "-" && i+1 < len(f) {
				v.Filesystem = f[i+1]
				break
			}
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
func (c *Collector) gpus() []GPU {
	out := []GPU{}
	paths, _ := filepath.Glob(c.path("/sys/class/drm/card[0-9]*"))
	for _, p := range paths {
		if !regexp.MustCompile(`^card[0-9]+$`).MatchString(filepath.Base(p)) {
			continue
		}
		vendor := strings.TrimSpace(readText(filepath.Join(p, "device/vendor")))
		kind := ""
		switch vendor {
		case "0x8086":
			kind = "Intel"
		case "0x1002":
			kind = "AMD"
		case "0x10de":
			kind = "NVIDIA"
		default:
			continue
		}
		g := GPU{ID: filepath.Base(p), Name: kind + " GPU", Kind: kind, Usage: unavailable("%", "驱动未提供利用率"), Temperature: unavailable("°C", "GPU 温度未提供"), MemoryUsed: unavailable("B", "显存统计未提供"), MemoryTotal: unavailable("B", "显存统计未提供")}
		if kind == "AMD" {
			if x, e := strconv.ParseFloat(strings.TrimSpace(readText(filepath.Join(p, "device/gpu_busy_percent"))), 64); e == nil {
				g.Usage = measured(x, "%")
			}
			for file, m := range map[string]*Metric{"mem_info_vram_used": &g.MemoryUsed, "mem_info_vram_total": &g.MemoryTotal} {
				if x, e := strconv.ParseFloat(strings.TrimSpace(readText(filepath.Join(p, "device", file))), 64); e == nil {
					*m = measured(x, "B")
				}
			}
		}
		files, _ := filepath.Glob(filepath.Join(p, "device/hwmon/hwmon*/temp1_input"))
		for _, f := range files {
			if x, e := strconv.ParseFloat(strings.TrimSpace(readText(f)), 64); e == nil {
				g.Temperature = measured(x/1000, "°C")
				break
			}
		}
		if kind == "Intel" && c.root == "" {
			g.Usage = intelUsage(p)
		}
		out = append(out, g)
	}
	if c.root == "" {
		if b, e := runTool("nvidia-smi", "--query-gpu=uuid,name,utilization.gpu,temperature.gpu,memory.used,memory.total", "--format=csv,noheader,nounits"); e == nil {
			filtered := []GPU{}
			for _, g := range out {
				if g.Kind != "NVIDIA" {
					filtered = append(filtered, g)
				}
			}
			out = filtered
			for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
				f := strings.Split(line, ",")
				if len(f) != 6 {
					continue
				}
				g := GPU{ID: strings.TrimSpace(f[0]), Name: strings.TrimSpace(f[1]), Kind: "NVIDIA", Usage: nvidiaMetric(f[2], "%", 1), Temperature: nvidiaMetric(f[3], "°C", 1), MemoryUsed: nvidiaMetric(f[4], "B", 1048576), MemoryTotal: nvidiaMetric(f[5], "B", 1048576)}
				out = append(out, g)
			}
		}
	}
	return out
}
func nvidiaMetric(s, unit string, multiplier float64) Metric {
	v, e := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if e != nil {
		return unavailable(unit, "驱动未提供该指标")
	}
	return measured(v*multiplier, unit)
}
func intelUsage(card string) Metric {
	// Sample the i915/xe PMU via a bounded child process. No kernel settings or
	// perf_event_paranoid values are changed.
	p, e := exec.LookPath("intel_gpu_top")
	if e != nil {
		return unavailable("%", "未安装 intel_gpu_top")
	}
	cmd := exec.Command(p, "-J", "-s", "1000", "-d", "drm:/dev/dri/"+filepath.Base(card))
	stdout, e := cmd.StdoutPipe()
	if e != nil {
		return unavailable("%", "Intel GPU 采样失败")
	}
	if e = cmd.Start(); e != nil {
		return unavailable("%", "Intel GPU 采样权限不足")
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	timer := time.AfterFunc(2500*time.Millisecond, func() { _ = cmd.Process.Kill() })
	defer timer.Stop()
	scan := bufio.NewScanner(io.LimitReader(stdout, 128<<10))
	scan.Buffer(make([]byte, 4096), 128<<10)
	var buf strings.Builder
	depth := 0
	started := false
	for scan.Scan() {
		line := scan.Text()
		for _, ch := range line {
			if ch == '{' {
				depth++
				started = true
			}
			if started {
				buf.WriteRune(ch)
			}
			if ch == '}' {
				depth--
			}
			if started && depth == 0 {
				var r struct {
					Engines map[string]struct {
						Busy float64 `json:"busy"`
					} `json:"engines"`
				}
				if json.Unmarshal([]byte(buf.String()), &r) == nil && len(r.Engines) > 0 {
					max := 0.0
					for _, engine := range r.Engines {
						if engine.Busy > max {
							max = engine.Busy
						}
					}
					return measured(max, "%")
				}
				buf.Reset()
				started = false
			}
		}
	}
	return unavailable("%", "Intel PMU 无数据或采样权限不足")
}
func runHelper(path, controlFile string) error {
	c := newCollector("")
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	every(ctx, 5*time.Second, func() {
		if controlFile != "" {
			b, e := os.ReadFile(controlFile)
			var control Config
			if e != nil || json.Unmarshal(b, &control) != nil || (control.Role != "agent" && !control.MonitorLocal) {
				return
			}
		}
		s := c.Collect(time.Now())
		raw, e := json.Marshal(s)
		if e != nil {
			fmt.Fprintln(os.Stderr, "硬件快照编码失败")
			return
		}
		if e := atomicBytes(path, append(raw, '\n'), 0640); e != nil {
			fmt.Fprintln(os.Stderr, "硬件快照保存失败")
		}
	})
	return nil
}
