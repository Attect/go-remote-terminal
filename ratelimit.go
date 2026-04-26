package main

import (
	"sync"
	"time"
)

// TokenBucket 令牌桶速率限制器
type TokenBucket struct {
	rate       float64   // 每秒产生的令牌数
	burst      float64   // 桶容量（突发上限）
	tokens     float64   // 当前令牌数
	lastUpdate time.Time // 上次更新时间
	mu         sync.Mutex
}

// NewTokenBucket 创建令牌桶
// rate: 每秒令牌数, burst: 桶容量
func NewTokenBucket(rate, burst int64) *TokenBucket {
	return &TokenBucket{
		rate:       float64(rate),
		burst:      float64(burst),
		tokens:     float64(burst),
		lastUpdate: time.Now(),
	}
}

// Allow 尝试消费 n 个令牌，返回是否允许
func (tb *TokenBucket) Allow(n int) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastUpdate).Seconds()
	tb.tokens += elapsed * tb.rate
	if tb.tokens > tb.burst {
		tb.tokens = tb.burst
	}
	tb.lastUpdate = now

	if tb.tokens >= float64(n) {
		tb.tokens -= float64(n)
		return true
	}
	return false
}
