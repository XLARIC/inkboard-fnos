package main

import (
	"net/http"

	"github.com/skip2/go-qrcode"
)

func dashboardURL(r *http.Request, view string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if view == "kindle" {
		return scheme + "://" + r.Host + "/basic"
	}
	return scheme + "://" + r.Host + "/?mode=mobile"
}

func (a *App) qrcodePage(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "方法不允许", 405)
		return
	}
	view := r.URL.Query().Get("view")
	if view != "modern" && view != "kindle" {
		http.Error(w, "请选择彩色版或 Kindle 版", 400)
		return
	}
	// The LAN middleware validates Host. Never encode arbitrary supplied URLs or credentials.
	code, err := qrcode.Encode(dashboardURL(r, view), qrcode.Medium, 256)
	if err != nil {
		http.Error(w, "二维码生成失败", 500)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(code)
}
