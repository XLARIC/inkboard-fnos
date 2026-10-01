package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println(version)
		return
	}
	cmd := "serve"
	args := os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd = args[0]
		args = args[1:]
	}
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	configDir := fs.String("config-dir", envOr("INKBOARD_CONFIG_DIR", ".local/config"), "配置目录")
	dataDir := fs.String("data-dir", envOr("INKBOARD_DATA_DIR", ".local/data"), "缓存目录")
	listen := fs.String("listen", os.Getenv("INKBOARD_LISTEN"), "覆盖页面监听地址")
	agentListen := fs.String("agent-listen", os.Getenv("INKBOARD_AGENT_LISTEN"), "覆盖采集 API 监听地址")
	role := fs.String("role", os.Getenv("INKBOARD_ROLE"), "hub 或 agent")
	demo := fs.Bool("demo", false, "仅回环地址的演示模式")
	monitor := fs.Bool("monitor-local", false, "初始化时监控本机")
	passwordFile := fs.String("password-file", "", "初始化密码文件，读取后不输出")
	snapshotFile := fs.String("snapshot-file", os.Getenv("INKBOARD_SNAPSHOT_FILE"), "读取隔离采集助手的快照")
	controlFile := fs.String("control-file", "", "助手仅在配置启用采集时采样")
	_ = fs.Parse(args)
	if cmd == "helper" {
		out := filepath.Join(*dataDir, "snapshot.json")
		if *snapshotFile != "" {
			out = *snapshotFile
		}
		if e := runHelper(out, *controlFile); e != nil {
			log.Fatal(e)
		}
		return
	}
	s, e := openStore(*configDir, *dataDir)
	if e != nil {
		log.Fatal(e)
	}
	if cmd == "init" {
		e = s.Update(func(c *Config) error {
			if *listen != "" {
				c.Listen = *listen
			}
			if *agentListen != "" {
				c.AgentListen = *agentListen
			}
			if *role != "" {
				c.Role = *role
			}
			fs.Visit(func(f *flag.Flag) {
				if f.Name == "monitor-local" {
					c.MonitorLocal = *monitor
				}
			})
			if *passwordFile != "" {
				b, e := os.ReadFile(*passwordFile)
				if e != nil {
					return e
				}
				p, e := hashPassword(strings.TrimSuffix(string(b), "\n"))
				if e != nil {
					return e
				}
				c.PasswordHash = p
			}
			return nil
		})
		if e != nil {
			log.Fatal(e)
		}
		fmt.Println("配置已保存")
		return
	}
	if cmd == "print-entry" {
		_ = json.NewEncoder(os.Stdout).Encode(desktopEntry(s.Get()))
		return
	}
	if cmd == "print-wizard" {
		b, e := configuredWizard(s.Get())
		if e != nil {
			log.Fatal(e)
		}
		_, _ = os.Stdout.Write(b)
		return
	}
	if cmd != "serve" {
		log.Fatal(errors.New("支持的命令：serve、init、helper、version"))
	}
	c := s.Get()
	if *listen != "" {
		c.Listen = *listen
	}
	if *agentListen != "" {
		c.AgentListen = *agentListen
	}
	if *role != "" {
		c.Role = *role
	}
	if e = validateConfig(c); e != nil {
		log.Fatal(e)
	}
	if *demo && !strings.HasPrefix(c.Listen, "127.0.0.1:") {
		log.Fatal("演示模式仅允许 127.0.0.1")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	app, e := newApp(s, c, *demo, *snapshotFile)
	if e != nil {
		log.Fatal(e)
	}
	app.start(ctx)
	log.Printf("InkBoard %s，%s，页面监听 %s", version, c.Role, c.Listen)
	if e = app.serve(ctx); e != nil {
		log.Fatal(e)
	}
}
func envOr(k, d string) string {
	if x := os.Getenv(k); x != "" {
		return x
	}
	return d
}
func every(ctx context.Context, interval time.Duration, fn func()) {
	fn()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			fn()
		}
	}
}
