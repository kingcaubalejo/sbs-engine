package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"
)

// gzipMinSize sets the threshold below which compression is skipped.
// Below ~1 KiB the headers and CPU dominate and gzip can actually
// inflate the response.
const gzipMinSize = 1024

var gzipWriterPool = sync.Pool{
	New: func() any { return gzip.NewWriter(io.Discard) },
}

// Gzip compresses responses for clients that advertise gzip support.
// Compression is deferred until enough bytes are buffered (gzipMinSize)
// to avoid penalising tiny responses.
func Gzip(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		gw := &gzipResponseWriter{ResponseWriter: w}
		defer gw.Close()
		next.ServeHTTP(gw, r)
	})
}

type gzipResponseWriter struct {
	http.ResponseWriter
	writer    *gzip.Writer
	buf       []byte
	headerWrt bool
	status    int
	threshold bool
}

func (g *gzipResponseWriter) WriteHeader(code int) {
	g.status = code
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	if g.threshold {
		return g.writer.Write(b)
	}
	g.buf = append(g.buf, b...)
	if len(g.buf) < gzipMinSize {
		return len(b), nil
	}
	g.startCompression()
	return len(b), nil
}

func (g *gzipResponseWriter) startCompression() {
	g.threshold = true
	g.writer = gzipWriterPool.Get().(*gzip.Writer)
	g.writer.Reset(g.ResponseWriter)
	g.ResponseWriter.Header().Set("Content-Encoding", "gzip")
	g.ResponseWriter.Header().Del("Content-Length")
	g.ResponseWriter.Header().Add("Vary", "Accept-Encoding")
	if !g.headerWrt {
		if g.status == 0 {
			g.status = http.StatusOK
		}
		g.ResponseWriter.WriteHeader(g.status)
		g.headerWrt = true
	}
	if len(g.buf) > 0 {
		_, _ = g.writer.Write(g.buf)
		g.buf = nil
	}
}

func (g *gzipResponseWriter) Close() {
	if g.threshold {
		_ = g.writer.Close()
		gzipWriterPool.Put(g.writer)
		return
	}
	// Below threshold — flush as-is, no compression.
	if !g.headerWrt {
		if g.status == 0 {
			g.status = http.StatusOK
		}
		g.ResponseWriter.WriteHeader(g.status)
		g.headerWrt = true
	}
	if len(g.buf) > 0 {
		_, _ = g.ResponseWriter.Write(g.buf)
	}
}
