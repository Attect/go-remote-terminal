package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

// TokenAuth Token认证器
type TokenAuth struct {
	token string // 配置的访问令牌
}

// NewTokenAuth 创建Token认证器
func NewTokenAuth(token string) *TokenAuth {
	return &TokenAuth{token: token}
}

// Validate 验证Token是否匹配
func (a *TokenAuth) Validate(provided string) bool {
	if a.token == "" {
		return false
	}
	return a.token == provided
}

// IsConfigured 检查Token是否已配置
func (a *TokenAuth) IsConfigured() bool {
	return a.token != ""
}

// GinMiddleware 返回Gin中间件，用于HTTP API认证
// 从Header的Authorization: Bearer {token} 或 query参数token中提取
func (a *TokenAuth) GinMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !a.IsConfigured() {
			c.JSON(http.StatusUnauthorized, APIResponse{
				Code:    40100,
				Message: "token not configured",
			})
			c.Abort()
			return
		}

		// 优先从Header提取
		token := a.extractFromHeader(c)
		if token == "" {
			// 其次从query参数提取
			token = c.Query("token")
		}

		if !a.Validate(token) {
			c.JSON(http.StatusUnauthorized, APIResponse{
				Code:    40100,
				Message: "invalid token",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// WebSocketAuthFunc 返回WebSocket升级时的认证函数
// 从query参数token中提取并验证
func (a *TokenAuth) WebSocketAuthFunc() func(r *http.Request) bool {
	return func(r *http.Request) bool {
		token := r.URL.Query().Get("token")
		return a.Validate(token)
	}
}

// extractFromHeader 从Authorization Header提取Bearer Token
func (a *TokenAuth) extractFromHeader(c *gin.Context) string {
	auth := c.GetHeader("Authorization")
	if auth == "" {
		return ""
	}

	// 期望格式: Bearer {token}
	const bearerPrefix = "Bearer "
	if len(auth) > len(bearerPrefix) && auth[:len(bearerPrefix)] == bearerPrefix {
		return auth[len(bearerPrefix):]
	}

	return auth
}

// ValidateOrAbort 验证Token，失败时终止请求（用于WebSocket handler）
func (a *TokenAuth) ValidateOrAbort(c *gin.Context) bool {
	if !a.IsConfigured() {
		c.JSON(http.StatusUnauthorized, APIResponse{
			Code:    40100,
			Message: "token not configured",
		})
		return false
	}

	token := c.Query("token")
	if !a.Validate(token) {
		c.JSON(http.StatusUnauthorized, APIResponse{
			Code:    40100,
			Message: "invalid token",
		})
		return false
	}

	return true
}

// EnsureTokenConfigured 确保Token已配置，否则打印提示并退出
func (a *TokenAuth) EnsureTokenConfigured() {
	if !a.IsConfigured() {
		fmt.Fprintln(os.Stderr, "Error: access token is required. Use --token or -t to set it.")
		fmt.Fprintln(os.Stderr, "Example: go-remote-terminal -t mysecret123")
		os.Exit(1)
	}
}
