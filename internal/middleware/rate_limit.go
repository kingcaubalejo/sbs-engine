package middleware

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"sbs-engine/internal/response"
)

// RateLimitConfig controls per-IP rate limiting. Default is the limit
// applied to any path not present in PerRoute. PerRoute lets specific
// path prefixes use a stricter (or looser) limit — for example
// /api/sermons/search has a much smaller bucket than /api/volumes.
// Bypass paths (e.g. /health, /swagger/) skip rate limiting entirely so
// they cannot exhaust an attacker's budget against a load-balancer probe.
type RateLimitConfig struct {
	Default  Bucket
	PerRoute map[string]Bucket
	Bypass   []string
}

// Bucket captures one limiter's tokens-per-second and burst.
type Bucket struct {
	RPS   rate.Limit
	Burst int
}

// ipLimiter is one entry in the per-IP+route limiter map. The limiter
// itself is the long-lived state; lastSeen is updated on every Allow call
// and used by the GC goroutine to evict idle IPs.
type ipLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type limiterStore struct {
	mu       sync.Mutex
	limiters map[string]*ipLimiter
}

func newLimiterStore() *limiterStore {
	s := &limiterStore{limiters: map[string]*ipLimiter{}}
	go s.gc()
	return s
}

// gc evicts any limiter whose owner hasn't sent a request in 10 minutes,
// preventing the map from growing unboundedly under attack.
func (s *limiterStore) gc() {
	for range time.Tick(time.Minute) {
		s.mu.Lock()
		cutoff := time.Now().Add(-10 * time.Minute)
		for k, v := range s.limiters {
			if v.lastSeen.Before(cutoff) {
				delete(s.limiters, k)
			}
		}
		s.mu.Unlock()
	}
}

func (s *limiterStore) get(key string, cfg Bucket) *rate.Limiter {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry, ok := s.limiters[key]; ok {
		entry.lastSeen = time.Now()
		return entry.limiter
	}
	l := rate.NewLimiter(cfg.RPS, cfg.Burst)
	s.limiters[key] = &ipLimiter{limiter: l, lastSeen: time.Now()}
	return l
}

// RateLimit returns a middleware that enforces a per-IP token-bucket
// limit. The bucket is keyed by (clientIP, routePrefix) so that an
// attacker hammering /sermons/search does not also lock out their own
// access to /volumes (and vice versa).
func RateLimit(cfg RateLimitConfig) func(http.Handler) http.Handler {
	store := newLimiterStore()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path
			for _, b := range cfg.Bypass {
				if path == b || strings.HasPrefix(path, b) {
					next.ServeHTTP(w, r)
					return
				}
			}

			ip := clientIP(r)
			if ip == "" {
				response.Error(w, http.StatusBadRequest, "could not determine client address")
				return
			}

			limit := cfg.Default
			route := "default"
			for prefix, l := range cfg.PerRoute {
				if strings.HasPrefix(path, prefix) {
					limit = l
					route = prefix
					break
				}
			}

			key := ip + "|" + route
			limiter := store.get(key, limit)
			if !limiter.Allow() {
				retryAfter := time.Second
				if r := limiter.Reserve(); r.OK() {
					if d := r.Delay(); d > 0 {
						retryAfter = d
					}
					r.Cancel()
				}
				w.Header().Set("Retry-After", fmt.Sprintf("%.0f", retryAfter.Seconds()))
				response.Error(w, http.StatusTooManyRequests, "too many requests")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
