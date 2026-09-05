package middleware

import (
	"bufio"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"time"

	"nautilus/internal/errors"
	"nautilus/internal/log"
)

const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ0123456789"

func randomString(n int) string {
	b := make([]byte, n)
	l := len(alphabet)
	for i := range n {
		b[i] = alphabet[rand.N(l)]
	}
	return string(b)
}

func AccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		logger := log.FromContext(ctx)

		requestID := randomString(8)
		w.Header().Set("Request-Id", requestID)

		logger = logger.With(
			"request_id", requestID,
			"method", r.Method,
			"url", r.URL.String(),
		)
		ctx = log.WithContext(ctx, logger)
		r = r.WithContext(ctx)

		logger.Info("request started")

		ww, err := WrapWriter(w)
		if err != nil {
			panic(err)
		}

		now := time.Now()
		next.ServeHTTP(ww, r)
		elapsed := time.Since(now)

		logger.With(
			"duration", elapsed.Milliseconds(),
			"status", ww.Status(),
			"size", ww.BytesWritten(),
		).Info("request completed")
	})
}

type WriterProxy interface {
	http.ResponseWriter
	http.Hijacker
	Status() int
	BytesWritten() int
	Tee(io.Writer)
	Unwrap() http.ResponseWriter
	Hijacked() bool
}

func WrapWriter(w http.ResponseWriter) (WriterProxy, error) {
	_, fl := w.(http.Flusher)
	_, hj := w.(http.Hijacker)

	if !fl {
		return nil, errors.New("ResponseWriter is not a Flusher")
	}
	if !hj {
		return nil, errors.New("ResponseWriter is not a Hijacker")
	}

	return &writerProxy{ResponseWriter: w}, nil
}

// writerProxy wraps a http.ResponseWriter that implements the minimal
// http.ResponseWriter interface.
type writerProxy struct {
	http.ResponseWriter
	wroteHeader bool
	code        int
	bytes       int
	tee         io.Writer
	hijacked    bool
}

func (wp *writerProxy) WriteHeader(code int) {
	if !wp.wroteHeader {
		wp.code = code
		wp.wroteHeader = true
		wp.ResponseWriter.WriteHeader(code)
	}
}

func (wp *writerProxy) Write(buf []byte) (int, error) {
	wp.WriteHeader(http.StatusOK)
	n, err := wp.ResponseWriter.Write(buf)
	if wp.tee != nil {
		_, e := wp.tee.Write(buf[:n])
		if err == nil {
			err = e
		}
	}
	wp.bytes += n
	return n, errors.Wrap(err, "error writing response")
}

func (wp *writerProxy) Status() int {
	return wp.code
}

func (wp *writerProxy) BytesWritten() int {
	return wp.bytes
}

func (wp *writerProxy) Tee(w io.Writer) {
	wp.tee = w
}

func (wp *writerProxy) Unwrap() http.ResponseWriter {
	return wp.ResponseWriter
}

func (wp *writerProxy) Flush() {
	fl := wp.ResponseWriter.(http.Flusher)
	fl.Flush()
}

func (wp *writerProxy) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj := wp.ResponseWriter.(http.Hijacker)
	wp.hijacked = true

	conn, rw, err := hj.Hijack()
	if err != nil {
		return nil, nil, errors.Wrap(err, "error hijacking connection")
	}
	return conn, rw, nil
}

func (wp *writerProxy) Hijacked() bool {
	return wp.hijacked
}
