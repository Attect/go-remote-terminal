package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

// TokenAuth Token认证器
type TokenAuth struct {
	token         string // 管理令牌（完整权限）
	readOnlyToken string // 只读令牌（仅接收输出）
}

// NewTokenAuth 创建Token认证器
func NewTokenAuth(token, readOnlyToken string) *TokenAuth {
	return &TokenAuth{token: token, readOnlyToken: readOnlyToken}
}

// ValidateResult 验证结果
type ValidateResult int

const (
	ValidateInvalid  ValidateResult = iota // 无效Token
	ValidateReadOnly                        // 只读Token
	ValidateAdmin                           // 管理Token
)

// Validate 验证Token，返回验证结果
func (a *TokenAuth) Validate(provided string) ValidateResult {
	if a.token != "" && a.token == provided {
		return ValidateAdmin
	}
	if a.readOnlyToken != "" && a.readOnlyToken == provided {
		return ValidateReadOnly
	}
	return ValidateInvalid
}

// IsConfigured 检查是否有任何Token已配置
func (a *TokenAuth) IsConfigured() bool {
	return a.token != "" || a.readOnlyToken != ""
}

// IsAdminConfigured 检查管理Token是否已配置
func (a *TokenAuth) IsAdminConfigured() bool {
	return a.token != ""
}

// GinMiddleware 返回Gin中间件，用于HTTP API认证
// HTTP API只允许管理Token访问
func (a *TokenAuth) GinMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !a.IsAdminConfigured() {
			c.JSON(http.StatusUnauthorized, APIResponse{
				Code:    40100,
				Message: "admin token not configured",
			})
			c.Abort()
			return
		}

		token := a.extractFromHeader(c)
		if token == "" {
			token = c.Query("token")
		}

		if a.Validate(token) != ValidateAdmin {
			c.JSON(http.StatusUnauthorized, APIResponse{
				Code:    40100,
				Message: "invalid admin token",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// extractFromHeader 从Authorization Header提取Bearer Token
func (a *TokenAuth) extractFromHeader(c *gin.Context) string {
	auth := c.GetHeader("Authorization")
	if auth == "" {
		return ""
	}

	const bearerPrefix = "Bearer "
	if len(auth) > len(bearerPrefix) && auth[:len(bearerPrefix)] == bearerPrefix {
		return auth[len(bearerPrefix):]
	}

	return auth
}

// EnsureTokenConfigured 确保至少有一个Token已配置，否则打印提示并退出
func (a *TokenAuth) EnsureTokenConfigured() {
	if !a.IsConfigured() {
		fmt.Fprintln(os.Stderr, "Error: access token is required. Use --token or -t to set it.")
		fmt.Fprintln(os.Stderr, "Example: go-remote-terminal -t mysecret123")
		os.Exit(1)
	}
}
