package main

import (
	"flag"
	"fmt"
	"os"
)

// Config 应用配置
type Config struct {
	Host  string // 监听地址，默认 "0.0.0.0"
	Port  int    // 监听端口，默认 8080
	Token string // 访问令牌，必需
}

// ParseConfig 解析配置
// 优先级: 命令行参数 > 环境变量 > 默认值
func ParseConfig() *Config {
	cfg := &Config{}

	// 定义命令行参数
	flag.StringVar(&cfg.Host, "host", "", "listen address (default 0.0.0.0)")
	flag.IntVar(&cfg.Port, "port", 0, "listen port (default 8080)")
	flag.StringVar(&cfg.Token, "token", "", "access token (required)")
	flag.StringVar(&cfg.Token, "t", "", "access token (shorthand)")

	// 自定义usage信息
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "A lightweight cross-platform web terminal remote control system.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nEnvironment Variables:\n")
		fmt.Fprintf(os.Stderr, "  GRT_HOST  listen address\n")
		fmt.Fprintf(os.Stderr, "  GRT_PORT  listen port\n")
		fmt.Fprintf(os.Stderr, "  GRT_TOKEN access token\n")
	}

	flag.Parse()

	// 环境变量覆盖默认值（命令行参数优先级更高）
	if cfg.Host == "" {
		cfg.Host = os.Getenv("GRT_HOST")
	}
	if cfg.Port == 0 {
		if envPort := os.Getenv("GRT_PORT"); envPort != "" {
			var err error
			cfg.Port, err = parsePort(envPort)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: invalid GRT_PORT value %q: %v\n", envPort, err)
				os.Exit(1)
			}
		}
	}
	if cfg.Token == "" {
		cfg.Token = os.Getenv("GRT_TOKEN")
	}

	// 设置默认值
	if cfg.Host == "" {
		cfg.Host = "0.0.0.0"
	}
	if cfg.Port == 0 {
		cfg.Port = 8080
	}

	return cfg
}

// Validate 校验配置合法性
func (c *Config) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("invalid port number: %d (must be 1-65535)", c.Port)
	}

	if c.Token == "" {
		return fmt.Errorf("access token is required")
	}

	if len(c.Token) < 8 {
		return fmt.Errorf("token too short (minimum 8 characters)")
	}

	return nil
}

// Addr 返回监听地址字符串
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// parsePort 解析端口号
func parsePort(s string) (int, error) {
	var port int
	_, err := fmt.Sscanf(s, "%d", &port)
	if err != nil {
		return 0, fmt.Errorf("invalid port: %w", err)
	}
	return port, nil
}
