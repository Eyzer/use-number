package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// 前端资源全部编译进二进制，单文件分发，不依赖外部目录
//
//go:embed web
var webAssets embed.FS

// 程序所在目录，用于存放 data/use-number.db
var appDir string

func main() {
	port := flag.Int("port", 80, "监听端口，默认 80")
	flag.Parse()

	exePath, err := os.Executable()
	if err != nil {
		log.Fatalf("无法定位程序路径: %v", err)
	}
	appDir = filepath.Dir(exePath)

	if err := initDB(appDir); err != nil {
		log.Fatalf("数据库初始化失败: %v", err)
	}

	mux := http.NewServeMux()

	staticFS, err := fs.Sub(webAssets, "web/static")
	if err != nil {
		log.Fatalf("静态资源加载失败: %v", err)
	}
	mux.Handle("/static/", http.StripPrefix("/static/", cacheStatic(http.FileServer(http.FS(staticFS)))))

	mux.HandleFunc("/favicon.svg", func(w http.ResponseWriter, r *http.Request) {
		serveEmbedded(w, "web/static/favicon.svg", "image/svg+xml")
	})
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		serveEmbedded(w, "web/static/favicon.svg", "image/svg+xml")
	})

	// 后台登录页
	mux.HandleFunc("/admin/login", func(w http.ResponseWriter, r *http.Request) {
		serveEmbedded(w, "web/admin_login.html", "text/html; charset=utf-8")
	})

	// 后台主页面，未登录跳登录页
	mux.HandleFunc("/admin", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin" && r.URL.Path != "/admin/" {
			http.NotFound(w, r)
			return
		}
		if getSessionUser(r) == nil {
			http.Redirect(w, r, "/admin/login", http.StatusFound)
			return
		}
		serveEmbedded(w, "web/admin.html", "text/html; charset=utf-8")
	})

	// 前台首页，不需要登录
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		serveEmbedded(w, "web/index.html", "text/html; charset=utf-8")
	})

	mux.HandleFunc("/api/", apiHandler)

	addr := fmt.Sprintf(":%d", *port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      accessLog(mux),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	log.Printf("发文字号取号系统启动，访问地址 http://localhost:%d", *port)
	log.Printf("后台入口 http://localhost:%d/admin", *port)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}

func serveEmbedded(w http.ResponseWriter, name, contentType string) {
	data, err := webAssets.ReadFile(name)
	if err != nil {
		http.Error(w, "资源不存在", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}

func cacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		next.ServeHTTP(w, r)
	})
}

func accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.URL.Path) >= 4 && r.URL.Path[:4] == "/api" {
			log.Printf("%s %s", r.Method, r.URL.Path)
		}
		next.ServeHTTP(w, r)
	})
}
