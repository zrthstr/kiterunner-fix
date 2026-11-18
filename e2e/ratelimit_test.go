package e2e

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

var incompleteWarning = regexp.MustCompile(`no-response"?[=:](\d+)[, ]+"?rate-limited"?[=:](\d+)`)

// assertIncomplete checks the end of scan warning about unanswered requests. "" means no warning
func assertIncomplete(t *testing.T, stderr, noResponse, rateLimited string) {
	t.Helper()
	m := incompleteWarning.FindStringSubmatch(stderr)
	switch {
	case noResponse == "" && m != nil:
		t.Errorf("unexpected warning: %s", m[0])
	case noResponse != "" && m == nil:
		t.Errorf("expected a warning about %s unanswered and %s rate limited requests: %s", noResponse, rateLimited, tail(stderr))
	case m != nil && (m[1] != noResponse || m[2] != rateLimited):
		t.Errorf("warning reports %s unanswered and %s rate limited requests, expected %s and %s", m[1], m[2], noResponse, rateLimited)
	}
}

// a request that times out must only lose that one route. kr used to stop sending the rest of the connection's
// routes after a single failed request, silently dropping up to 1/x of the scan
func TestTimeoutLosesOnlyThatRoute(t *testing.T) {
	tg := newTarget(t, false, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/w02":
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
		case strings.HasPrefix(r.URL.Path, "/w"):
			reply(w, 200, "text/plain", "route "+r.URL.Path+" exists")
		default:
			reply(w, 404, "text/plain", "not found")
		}
	})
	var words, want []string
	for i := 1; i <= 20; i++ {
		words = append(words, fmt.Sprintf("w%02d", i))
		if i != 2 {
			want = append(want, fmt.Sprintf("GET /w%02d", i))
		}
	}
	wl := writeFile(t, "words.txt", strings.Join(words, "\n")+"\n")
	r := kr(t, "", "-o", "json", "brute", tg.URL, "-w", wl, "-x", "1", "-t", "1s", "--progress=false", "--quarantine-threshold", "0")
	if r.ExitCode != 0 {
		t.Fatalf("exit %d: %s", r.ExitCode, tail(r.Stderr))
	}
	assertExactly(t, parseHits(t, r.Stdout), want...)
	assertIncomplete(t, r.Stderr, "1", "0")
}

// rateLimitedTarget serves app() with a plain 404, but answers the first request of every method and path with
// the given rate limit response, preflight requests included
func rateLimitedTarget(t *testing.T, limit func(w http.ResponseWriter)) *target {
	var (
		mu   sync.Mutex
		seen = map[string]bool{}
	)
	return newTarget(t, false, func(w http.ResponseWriter, r *http.Request) {
		k := r.Method + " " + r.URL.Path
		mu.Lock()
		first := !seen[k]
		seen[k] = true
		mu.Unlock()
		if first {
			limit(w)
			return
		}
		if !app(w, r) {
			reply(w, 404, "text/plain", "not found")
		}
	})
}

// rate limited requests are retried after the delay the server asks for, so nothing is lost and nothing is
// reported because of the rate limit
func TestRateLimitRetried(t *testing.T) {
	kite, _ := testKite(t)
	for name, limit := range map[string]func(w http.ResponseWriter){
		"429 retry-after": func(w http.ResponseWriter) {
			w.Header().Set("Retry-After", "1")
			reply(w, 429, "text/plain", "slow down")
		},
		// no Retry-After: kr backs off 1s, 2s, 4s
		"429": func(w http.ResponseWriter) {
			reply(w, 429, "text/plain", "slow down")
		},
		// 503 with Retry-After is how many proxies and CDNs throttle
		"503 retry-after": func(w http.ResponseWriter) {
			w.Header().Set("Retry-After", "1")
			reply(w, 503, "text/html", "<h1>Service Unavailable</h1>")
		},
	} {
		limit := limit
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tg := rateLimitedTarget(t, limit)
			// every request waits once, more connections keep the test short
			r := kr(t, "", "-o", "json", "scan", tg.URL, "-w", kite, "--kitebuilder-full-scan", "--progress=false", "-x", "20")
			if r.ExitCode != 0 {
				t.Fatalf("exit %d: %s", r.ExitCode, tail(r.Stderr))
			}
			assertExactly(t, parseHits(t, r.Stdout), truth...)
			assertIncomplete(t, r.Stderr, "", "")
			counts := map[string]int{}
			for _, e := range tg.requests() {
				counts[e.Method+" "+e.Path]++
			}
			for k, n := range counts {
				if n > 2 {
					t.Errorf("%s was requested %d times, expected one retry", k, n)
				}
			}
		})
	}
}

// a host that keeps rate limiting must never produce results, and the scan must say what it could not check
func TestRateLimitNeverReported(t *testing.T) {
	kite, _ := testKite(t)
	for name, limit := range map[string]func(w http.ResponseWriter){
		"429": func(w http.ResponseWriter) {
			reply(w, 429, "application/json", `{"error":"too many requests"}`)
		},
		"503 retry-after": func(w http.ResponseWriter) {
			w.Header().Set("Retry-After", "120")
			reply(w, 503, "text/html", "<h1>Service Unavailable</h1>")
		},
	} {
		limit := limit
		t.Run(name, func(t *testing.T) {
			// unknown paths (and so the preflight baseline) get a normal 404, but every real route is throttled
			tg := newTarget(t, false, func(w http.ResponseWriter, r *http.Request) {
				if app(httptestDiscard{}, r) {
					limit(w)
					return
				}
				reply(w, 404, "text/plain", "not found")
			})
			r := kr(t, "", "-o", "json", "scan", tg.URL, "-w", kite, "--kitebuilder-full-scan", "--progress=false", "--rate-limit-retries", "0")
			if r.ExitCode != 0 {
				t.Fatalf("exit %d: %s", r.ExitCode, tail(r.Stderr))
			}
			assertExactly(t, parseHits(t, r.Stdout))
			assertIncomplete(t, r.Stderr, "0", fmt.Sprint(len(truth)))
		})
	}
}

// a 503 without Retry-After is an ordinary answer: on a host that answers 503 for every unknown path it is the
// wildcard baseline, and the real routes must still be found without any retrying
func TestServiceUnavailableAsNotFound(t *testing.T) {
	kite, _ := testKite(t)
	tg := newTarget(t, false, func(w http.ResponseWriter, r *http.Request) {
		if !app(w, r) {
			reply(w, 503, "text/html", "<h1>503 Service Unavailable</h1>")
		}
	})
	r := kr(t, "", "-o", "json", "scan", tg.URL, "-w", kite, "--kitebuilder-full-scan", "--progress=false")
	if r.ExitCode != 0 {
		t.Fatalf("exit %d: %s", r.ExitCode, tail(r.Stderr))
	}
	assertExactly(t, parseHits(t, r.Stdout), truth...)
	assertIncomplete(t, r.Stderr, "", "")
	if r.Duration > 15*time.Second {
		t.Errorf("scan took %s, a 503 without Retry-After must not be retried", r.Duration)
	}
}

// httptestDiscard is a ResponseWriter that drops everything, to ask app() whether a request is a real route
type httptestDiscard struct{}

func (httptestDiscard) Header() http.Header         { return http.Header{} }
func (httptestDiscard) Write(b []byte) (int, error) { return len(b), nil }
func (httptestDiscard) WriteHeader(int)             {}
