package api

import (
	"log"
	"net/http"
	"time"

	"soapweb/internal/applog"
)

// statusRecorder remembers the status code so withLogging can report it.
// HasWritten keeps http.ResponseWriter's informal "WriteHeader implies Write"
// behaviour explicit.
type statusRecorder struct {
	http.ResponseWriter
	status     int
	hasWritten bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if !r.hasWritten {
		r.status = status
		r.hasWritten = true
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	if !r.hasWritten {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(p)
}

// withLogging writes one access line per API request and logs failures with
// their status, so the log file alone explains a "the page broke" report.
func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		elapsed := time.Since(start)
		switch {
		case rec.status >= http.StatusInternalServerError:
			log.Printf("%s %s -> %d（耗时 %s）", r.Method, r.URL.RequestURI(), rec.status, elapsed.Round(time.Millisecond))
		case rec.status >= http.StatusBadRequest:
			// 4xx are routine (a stale tab's 409, a bad upload); they go to the
			// file only, so the console stays readable.
			applog.Debugf("%s %s -> %d", r.Method, r.URL.RequestURI(), rec.status)
		default:
			// Successful reads would drown the log; only note slow ones.
			if elapsed > time.Second {
				log.Printf("%s %s -> %d（慢请求，耗时 %s）", r.Method, r.URL.RequestURI(), rec.status, elapsed.Round(time.Millisecond))
			}
		}
	})
}
