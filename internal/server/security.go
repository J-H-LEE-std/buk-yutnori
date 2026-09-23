package server

import (
	"container/list"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultLoginLimit = 10
	defaultJoinLimit  = 10
	maxLimiterEntries = 65536
)

// SecurityConfig contains process-local public HTTP boundary controls.
type SecurityConfig struct {
	TrustedProxyCIDRs []netip.Prefix
	Now               func() time.Time
	LoginLimit        int
	LoginWindow       time.Duration
	JoinLimit         int
	JoinWindow        time.Duration
}

// DefaultSecurityConfig is safe for a single directly exposed process. Proxy
// forwarding headers remain untrusted until CIDRs are explicitly configured.
func DefaultSecurityConfig() SecurityConfig {
	return SecurityConfig{
		Now:         time.Now,
		LoginLimit:  defaultLoginLimit,
		LoginWindow: time.Minute,
		JoinLimit:   defaultJoinLimit,
		JoinWindow:  5 * time.Minute,
	}
}

// ParseTrustedProxyCIDRs strictly parses a comma-separated allowlist.
func ParseTrustedProxyCIDRs(raw string) ([]netip.Prefix, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	prefixes := make([]netip.Prefix, 0, len(parts))
	for _, part := range parts {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(part))
		if err != nil || prefix.Bits() == 0 {
			return nil, errors.New("invalid trusted proxy CIDR")
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

func (config SecurityConfig) validate() error {
	if config.Now == nil || config.LoginLimit <= 0 || config.JoinLimit <= 0 || config.LoginWindow <= 0 || config.JoinWindow <= 0 {
		return errors.New("invalid security configuration")
	}
	return nil
}

type fixedWindowLimiter struct {
	mu          sync.Mutex
	now         func() time.Time
	limit       int
	window      time.Duration
	entries     map[string]*limitEntry
	expirations *list.List
}

type limitEntry struct {
	start time.Time
	count int
}

func newFixedWindowLimiter(now func() time.Time, limit int, window time.Duration) *fixedWindowLimiter {
	return &fixedWindowLimiter{
		now: now, limit: limit, window: window,
		entries: make(map[string]*limitEntry), expirations: list.New(),
	}
}

func (limiter *fixedWindowLimiter) allow(key string) (bool, time.Duration) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	now := limiter.now()
	for front := limiter.expirations.Front(); front != nil; front = limiter.expirations.Front() {
		expiredKey := front.Value.(string)
		entry := limiter.entries[expiredKey]
		if now.Before(entry.start.Add(limiter.window)) {
			break
		}
		delete(limiter.entries, expiredKey)
		limiter.expirations.Remove(front)
	}
	entry, exists := limiter.entries[key]
	if !exists {
		if len(limiter.entries) >= maxLimiterEntries {
			oldest := limiter.entries[limiter.expirations.Front().Value.(string)]
			return false, oldest.start.Add(limiter.window).Sub(now)
		}
		entry = &limitEntry{start: now, count: 1}
		limiter.expirations.PushBack(key)
		limiter.entries[key] = entry
		return true, 0
	}
	if entry.count >= limiter.limit {
		return false, entry.start.Add(limiter.window).Sub(now)
	}
	entry.count++
	return true, 0
}

type boundaryProtection struct {
	next         http.Handler
	trusted      []netip.Prefix
	loginLimiter *fixedWindowLimiter
	joinLimiter  *fixedWindowLimiter
}

func newBoundaryProtection(next http.Handler, config SecurityConfig) http.Handler {
	return &boundaryProtection{
		next:         next,
		trusted:      append([]netip.Prefix(nil), config.TrustedProxyCIDRs...),
		loginLimiter: newFixedWindowLimiter(config.Now, config.LoginLimit, config.LoginWindow),
		joinLimiter:  newFixedWindowLimiter(config.Now, config.JoinLimit, config.JoinWindow),
	}
}

func (protection *boundaryProtection) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	client := protection.clientAddress(request)
	var allowed bool
	var retryAfter time.Duration
	switch {
	case request.Method == http.MethodPost && request.URL.Path == "/api/v1/auth/google":
		allowed, retryAfter = protection.loginLimiter.allow(client)
	case request.Method == http.MethodPost && strings.HasPrefix(request.URL.Path, "/api/v1/rooms/") && strings.HasSuffix(request.URL.Path, "/join"):
		allowed, retryAfter = protection.joinLimiter.allow(client)
	default:
		protection.next.ServeHTTP(response, request)
		return
	}
	if !allowed {
		seconds := int64((retryAfter + time.Second - 1) / time.Second)
		if seconds < 1 {
			seconds = 1
		}
		response.Header().Set("Content-Type", "application/json; charset=utf-8")
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
		response.WriteHeader(http.StatusTooManyRequests)
		_, _ = response.Write([]byte("{\"error\":\"rate_limited\"}\n"))
		return
	}
	protection.next.ServeHTTP(response, request)
}

func (protection *boundaryProtection) clientAddress(request *http.Request) string {
	peer := parseAddress(request.RemoteAddr)
	if !containsPrefix(protection.trusted, peer) {
		return rateLimitAddress(peer)
	}
	forwarded := request.Header.Values("X-Forwarded-For")
	if len(forwarded) == 0 {
		return rateLimitAddress(peer)
	}
	parts := strings.Split(strings.Join(forwarded, ","), ",")
	for index := len(parts) - 1; index >= 0; index-- {
		address, err := netip.ParseAddr(strings.TrimSpace(parts[index]))
		if err != nil {
			return rateLimitAddress(peer)
		}
		address = address.Unmap()
		if !containsPrefix(protection.trusted, address) {
			return rateLimitAddress(address)
		}
	}
	return rateLimitAddress(peer)
}

func rateLimitAddress(address netip.Addr) string {
	address = address.Unmap()
	if address.Is6() {
		return netip.PrefixFrom(address, 64).Masked().String()
	}
	return address.String()
}

func parseAddress(remote string) netip.Addr {
	host, _, err := net.SplitHostPort(remote)
	if err == nil {
		remote = host
	}
	address, err := netip.ParseAddr(remote)
	if err != nil {
		return netip.IPv6Unspecified()
	}
	return address.Unmap()
}

func containsPrefix(prefixes []netip.Prefix, address netip.Addr) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
