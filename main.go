package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
)

//go:embed static
var staticFS embed.FS

func main() {
	// 解析配置
	cfg := ParseConfig()

	// 验证Token
	tokenAuth := NewTokenAuth(cfg.Token, cfg.ReadOnlyToken)
	tokenAuth.EnsureTokenConfigured()

	// 验证配置
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// 设置gin为release模式
	gin.SetMode(gin.ReleaseMode)

	// 创建Gin引擎
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(gin.LoggerWithConfig(gin.LoggerConfig{
		SkipPaths: []string{"/ws"}, // WebSocket连接不记录访问日志
	}))

	// 创建会话池
	pool := NewSessionPool()

	// 启动退出会话自动清理定时器（每5分钟清理一次已退出的会话）
	cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-cleanupCtx.Done():
				return
			case <-ticker.C:
				pool.Cleanup()
			}
		}
	}()

	// 创建处理器并注册路由
	handler := NewHandler(pool, tokenAuth)
	handler.RegisterRoutes(r)

	// 注册静态资源路由
	setupStaticRoutes(r)

	// 启动HTTP服务器
	srv := &http.Server{
		Addr:    cfg.Addr(),
		Handler: r,
	}

	// 在goroutine中启动服务器
	go func() {
		log.Printf("Server starting on %s", cfg.Addr())
		log.Printf("Access the terminal at http://%s", cfg.Addr())
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// 等待中断信号进行优雅关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	log.Printf("Received signal %v, shutting down...", sig)

	// 停止清理定时器
	cleanupCancel()

	// 给服务器5秒时间完成正在处理的请求
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	// 清理所有会话
	sessions := pool.List()
	for _, s := range sessions {
		if err := pool.Close(s.ID); err != nil {
			log.Printf("Error closing session %s: %v", s.ID, err)
		}
	}

	log.Println("Server exited")
}

// setupStaticRoutes 设置静态资源路由
func setupStaticRoutes(r *gin.Engine) {
	// 创建子文件系统，去掉 "static" 前缀
	subFS, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatalf("Failed to create sub filesystem: %v", err)
	}
	fileServer := http.FileServer(http.FS(subFS))

	// 静态资源路由: /static/*
	r.GET("/static/*filepath", func(c *gin.Context) {
		c.Request.URL.Path = c.Param("filepath")
		fileServer.ServeHTTP(c.Writer, c.Request)
	})

	// 根路径返回index.html
	r.GET("/", func(c *gin.Context) {
		data, err := staticFS.ReadFile("static/index.html")
		if err != nil {
			c.String(http.StatusInternalServerError, "index.html not found")
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", data)
	})
}
