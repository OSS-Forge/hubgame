package api

import (
	"bufio"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	debugOnce    sync.Once
	debugEnabled bool
)

func isDebugEnabled() bool {
	debugOnce.Do(func() {
		v := strings.TrimSpace(strings.ToLower(os.Getenv("HUBGAME_DEBUG")))
		debugEnabled = v == "1" || v == "true" || v == "yes" || v == "on"
	})
	return debugEnabled
}

func debugf(format string, args ...any) {
	if !isDebugEnabled() {
		return
	}
	log.Printf("[debug] "+format, args...)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return h.Hijack()
}

func (r *statusRecorder) Push(target string, opts *http.PushOptions) error {
	p, ok := r.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return p.Push(target, opts)
}

func withRequestDebug(service string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		debugf("%s %s %s status=%d dur_ms=%d remote=%s", service, r.Method, r.URL.RequestURI(), rec.status, time.Since(start).Milliseconds(), r.RemoteAddr)
	})
}
