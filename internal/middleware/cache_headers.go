package middleware

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
)

// maxETagBuffer caps how much of the response body the ETag middleware
// will buffer before giving up and letting bytes stream through without
// an ETag. Set to 4 MiB — comfortably above any realistic JSON payload
// from this API but small enough to bound memory under load.
const maxETagBuffer = 4 << 20

// CacheHeaders writes Cache-Control and a weak ETag for safe (GET/HEAD)
// requests. When the client supplies a matching If-None-Match it is
// served a 304 with no body. The default policy is conservative
// (max-age=300, s-maxage=900) so the API is CDN-friendly without risking
// stale content for too long; per-route policies should override via
// PerPath. Write methods always receive Cache-Control: no-store so PUT/
// PATCH/POST/DELETE responses are never cached.
//
// PerPath keys are matched as path prefixes and the longest-matching
// prefix wins.
type CacheConfig struct {
	Default  string
	PerPath  map[string]string
	NoStore  string
}

func DefaultCacheConfig() CacheConfig {
	return CacheConfig{
		Default: "public, max-age=300, s-maxage=900, stale-while-revalidate=86400",
		PerPath: map[string]string{
			"/api/health":     "no-store",
			"/api/stats":      "public, max-age=60, s-maxage=300",
			"/api/languages":  "public, max-age=3600, s-maxage=86400",
			"/api/donate":     "public, max-age=3600, s-maxage=86400",
			"/api/volumes":    "public, max-age=300, s-maxage=86400, stale-while-revalidate=86400",
			"/api/app-volume-list": "public, max-age=300, s-maxage=86400",
			"/api/sermons":    "public, max-age=300, s-maxage=86400",
		},
		NoStore: "no-store",
	}
}

// CacheHeaders is the middleware. It only buffers the body for safe
// methods because computing an ETag for non-idempotent responses is
// pointless and would hide write errors behind an extra layer of
// indirection.
func CacheHeaders(cfg CacheConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.Header().Set("Cache-Control", cfg.NoStore)
				next.ServeHTTP(w, r)
				return
			}

			policy := cfg.Default
			best := 0
			for prefix, p := range cfg.PerPath {
				if len(prefix) > best && hasPrefix(r.URL.Path, prefix) {
					policy = p
					best = len(prefix)
				}
			}
			w.Header().Set("Cache-Control", policy)

			rec := &etagRecorder{ResponseWriter: w, buf: &bytes.Buffer{}}
			next.ServeHTTP(rec, r)

			if rec.tooLarge || rec.status >= 300 {
				rec.flush()
				return
			}

			etag := computeETag(rec.buf.Bytes())
			w.Header().Set("ETag", etag)

			if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}

			w.Header().Set("Content-Length", strconv.Itoa(rec.buf.Len()))
			rec.flush()
		})
	}
}

// hasPrefix is a tiny helper so the middleware does not pull in strings
// just for one call site.
func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func computeETag(body []byte) string {
	sum := sha256.Sum256(body)
	return `W/"` + hex.EncodeToString(sum[:8]) + `"`
}

// etagRecorder buffers the inner handler's response so we can hash it
// before deciding whether to send a 304 or the original body. If the
// response grows past maxETagBuffer we stop buffering and stream the
// remaining bytes directly — a safety valve against large list
// responses pinning memory.
type etagRecorder struct {
	http.ResponseWriter
	buf      *bytes.Buffer
	status   int
	tooLarge bool
}

func (e *etagRecorder) WriteHeader(code int) {
	e.status = code
}

func (e *etagRecorder) Write(b []byte) (int, error) {
	if e.tooLarge {
		return e.ResponseWriter.Write(b)
	}
	if e.buf.Len()+len(b) > maxETagBuffer {
		e.tooLarge = true
		if e.status == 0 {
			e.status = http.StatusOK
		}
		e.ResponseWriter.WriteHeader(e.status)
		if e.buf.Len() > 0 {
			if _, err := e.ResponseWriter.Write(e.buf.Bytes()); err != nil {
				return 0, err
			}
			e.buf.Reset()
		}
		return e.ResponseWriter.Write(b)
	}
	return e.buf.Write(b)
}

func (e *etagRecorder) flush() {
	if e.tooLarge {
		return
	}
	if e.status == 0 {
		e.status = http.StatusOK
	}
	e.ResponseWriter.WriteHeader(e.status)
	if e.buf.Len() > 0 {
		_, _ = e.ResponseWriter.Write(e.buf.Bytes())
	}
}
