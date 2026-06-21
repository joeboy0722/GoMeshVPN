package qos

import (
	"sync"
	"time"
)

// RateLimiter Token Bucket 流量限制器
type RateLimiter struct {
	maxTokens    float64   // 最大 token 數量（bytes）
	tokens       float64   // 當前 token 數量
	refillRate   float64   // 每秒補充速率（bytes per second）
	lastRefill   time.Time // 上次補充時間
	mu           sync.Mutex
	minBurstSize int // 最小突發大小（允許小封包優先通過）
}

// NewRateLimiter 建立流量限制器
// bandwidthMbps: 頻寬限制（Mbps）
// burstSeconds: 允許的突發時間（秒），決定 bucket 大小
func NewRateLimiter(bandwidthMbps float64, burstSeconds float64) *RateLimiter {
	bytesPerSecond := bandwidthMbps * 1024 * 1024 / 8 // Mbps -> Bytes/s
	maxTokens := bytesPerSecond * burstSeconds

	return &RateLimiter{
		maxTokens:    maxTokens,
		tokens:       maxTokens, // 初始滿載
		refillRate:   bytesPerSecond,
		lastRefill:   time.Now(),
		minBurstSize: 2000, // 允許 2KB 小封包優先通過
	}
}

// Allow 檢查是否允許發送指定大小的封包
// 返回 true 表示允許，false 表示需要等待
func (rl *RateLimiter) Allow(packetSize int) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// 補充 tokens
	rl.refill()

	// 小封包優先放行（避免餓死小封包）
	if packetSize <= rl.minBurstSize && rl.tokens > 0 {
		rl.tokens -= float64(packetSize)
		return true
	}

	// 檢查是否有足夠 tokens
	if rl.tokens >= float64(packetSize) {
		rl.tokens -= float64(packetSize)
		return true
	}

	return false
}

// Wait 等待直到允許發送（阻塞式）
// 返回等待時間
func (rl *RateLimiter) Wait(packetSize int) time.Duration {
	for {
		if rl.Allow(packetSize) {
			return 0
		}

		// 計算需要等待的時間
		rl.mu.Lock()
		needed := float64(packetSize) - rl.tokens
		waitTime := time.Duration(needed / rl.refillRate * float64(time.Second))
		rl.mu.Unlock()

		// 最少等待 1ms，避免忙等
		if waitTime < time.Millisecond {
			waitTime = time.Millisecond
		}

		time.Sleep(waitTime)
	}
}

// refill 補充 tokens（內部函數，調用前必須持有鎖）
func (rl *RateLimiter) refill() {
	now := time.Now()
	elapsed := now.Sub(rl.lastRefill).Seconds()

	if elapsed > 0 {
		// 計算補充的 tokens
		tokensToAdd := elapsed * rl.refillRate
		rl.tokens += tokensToAdd

		// 不超過最大值
		if rl.tokens > rl.maxTokens {
			rl.tokens = rl.maxTokens
		}

		rl.lastRefill = now
	}
}

// GetAvailableTokens 取得當前可用 tokens（用於監控）
func (rl *RateLimiter) GetAvailableTokens() float64 {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.refill()
	return rl.tokens
}

// UpdateRate 動態調整速率（用於自適應）
func (rl *RateLimiter) UpdateRate(newBandwidthMbps float64) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	bytesPerSecond := newBandwidthMbps * 1024 * 1024 / 8
	rl.refillRate = bytesPerSecond

	// 調整 maxTokens 以維持相同的突發時間
	burstSeconds := rl.maxTokens / (rl.refillRate + 1) // 避免除以 0
	rl.maxTokens = bytesPerSecond * burstSeconds
}
