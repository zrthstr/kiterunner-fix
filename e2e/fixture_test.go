package e2e

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// exchange is a request received by a target
type exchange struct {
	Method, Path, Query string
	RequestURI          string // as sent, before decoding
	Header              http.Header
	Body                string
}

type target struct {
	*httptest.Server
	mu   sync.Mutex
	reqs []exchange
}

func (tg *target) requests() []exchange {
	tg.mu.Lock()
	defer tg.mu.Unlock()
	return append([]exchange(nil), tg.reqs...)
}

// requestsTo returns the requests for an exact method and path
func (tg *target) requestsTo(method, path string) []exchange {
	var ret []exchange
	for _, r := range tg.requests() {
		if r.Method == method && r.Path == path {
			ret = append(ret, r)
		}
	}
	return ret
}

func newTarget(t *testing.T, useTLS bool, h http.HandlerFunc) *target {
	t.Helper()
	tg := &target{}
	record := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		tg.mu.Lock()
		tg.reqs = append(tg.reqs, exchange{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, RequestURI: r.RequestURI, Header: r.Header.Clone(), Body: string(body)})
		tg.mu.Unlock()
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		h(w, r)
	}
	if useTLS {
		tg.Server = httptest.NewTLSServer(http.HandlerFunc(record))
	} else {
		tg.Server = httptest.NewServer(http.HandlerFunc(record))
	}
	t.Cleanup(tg.Close)
	return tg
}

func reply(w http.ResponseWriter, status int, contentType, body string) {
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	io.WriteString(w, body)
}

// app serves the routes of the test schema that really exist. It returns false if the request is not one of them,
// so a fixture can answer it with its own "not found" behaviour.
//
// response bodies are deliberately unlike every fixture's not found response (length and word count), so a
// miss is a real false negative and not a coincidence of sizes
func app(w http.ResponseWriter, r *http.Request) bool {
	p := r.URL.Path
	switch {
	case r.Method == "GET" && p == "/api/v1/users":
		reply(w, 200, "application/json", `{"users":[{"id":1},{"id":2}]}`)
	case r.Method == "PUT" && p == "/api/v1/users":
		reply(w, 200, "application/json", `{"updated":true}`)
	case r.Method == "GET" && strings.HasPrefix(p, "/api/v1/users/") && !strings.Contains(p[len("/api/v1/users/"):], "/") && len(p) > len("/api/v1/users/"):
		reply(w, 200, "application/json", `{"id":1,"name":"alice"}`)
	case r.Method == "GET" && p == "/admin":
		reply(w, 403, "text/html", "<h1>Forbidden</h1><p>admins only</p>")
	case r.Method == "POST" && p == "/onigokko/player":
		body, _ := io.ReadAll(r.Body)
		var v struct {
			Player struct {
				ID   *float64 `json:"id"`
				Name string   `json:"name"`
			} `json:"player"`
		}
		ok := r.Header.Get("Token") == "example-token" &&
			r.URL.Query().Get("QueryParam") == "example-query" &&
			strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") &&
			json.Unmarshal(body, &v) == nil && v.Player.ID != nil && v.Player.Name != ""
		if ok {
			reply(w, 201, "application/json", `{"created":true}`)
		} else {
			// kr is expected to send the parameters from the schema, so this must never be what it sees
			reply(w, 422, "application/json", `{"error":"missing token, query or body"}`)
		}
	default:
		return false
	}
	return true
}

// truth is what a full scan of the test schema must report against a target that serves app()
var truth = []string{
	"GET /api/v1/users",
	"PUT /api/v1/users",
	"GET /api/v1/users/*",
	"GET /admin",
	"POST /onigokko/player",
}

func randomHex(min, max int) string {
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(max-min+1)))
	b := make([]byte, (min+int(n.Int64())+1)/2)
	rand.Read(b)
	return hex.EncodeToString(b)[:min+int(n.Int64())]
}

// fixtures describe how a server answers anything that is not a real route
var fixtures = map[string]http.HandlerFunc{
	// a plain 404 with a constant body
	"plain-404": func(w http.ResponseWriter, r *http.Request) {
		reply(w, 404, "text/plain", "not found")
	},
	// express style: the requested method and path are echoed in the body, so the length changes with the path
	"echo-404": func(w http.ResponseWriter, r *http.Request) {
		reply(w, 404, "text/html", "<pre>Cannot "+r.Method+" "+r.URL.Path+"</pre>")
	},
	// soft 404: a 200 page with a random request id, so the length changes on every request
	"soft-404-dynamic": func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, "text/html", "<html><title>Not Found</title><body>Sorry, that page does not exist. ref="+randomHex(8, 40)+"</body></html>")
	},
	// single page app: every GET returns the same index.html with a 200, other methods 404
	"spa-catch-all": func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			reply(w, 200, "text/html", `<!doctype html><html><head><meta charset="utf-8"><title>App</title><script src="/static/js/main.4f2a1b.js"></script></head><body><div id="root"></div></body></html>`)
			return
		}
		reply(w, 404, "text/plain", "not found")
	},
	// everything unknown redirects to a login page that carries the requested path
	"redirect-to-login": func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/login?next="+r.URL.Path)
		w.WriteHeader(302)
	},
	// framework that knows the route but not the method answers 405, unknown paths 404
	"method-405": func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/users" {
			reply(w, 405, "text/plain", "method not allowed")
			return
		}
		reply(w, 404, "text/plain", "not found")
	},
}

// fixtureTarget serves app() and answers everything else with the named fixture
func fixtureTarget(t *testing.T, name string, useTLS bool) *target {
	t.Helper()
	fallback := fixtures[name]
	if fallback == nil {
		t.Fatalf("unknown fixture %s", name)
	}
	return newTarget(t, useTLS, func(w http.ResponseWriter, r *http.Request) {
		if !app(w, r) {
			fallback(w, r)
		}
	})
}

// slowTarget never answers within any reasonable timeout
func slowTarget(t *testing.T) *target {
	return newTarget(t, false, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	})
}

// wildcardTarget answers every request with a 200 that echoes the path
func wildcardTarget(t *testing.T) *target {
	return newTarget(t, false, func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, "text/plain", "ok "+r.URL.Path)
	})
}

// schema is the kitebuilder test schema: one API of real routes, one with parameters, and decoys that never exist
func schema() []map[string]interface{} {
	op := func() map[string]interface{} { return map[string]interface{}{"description": "d", "operationId": "o"} }
	idParam := map[string]interface{}{"get": map[string]interface{}{
		"description": "d", "operationId": "o",
		"parameters": []interface{}{map[string]interface{}{"in": "path", "name": "id", "required": true, "type": "integer"}},
	}}
	player := map[string]interface{}{"post": map[string]interface{}{
		"description": "create a player", "operationId": "createPlayer",
		"consumes": []string{"application/json"},
		"parameters": []interface{}{
			map[string]interface{}{"in": "header", "name": "Token", "required": true, "type": "string", "default": "example-token"},
			map[string]interface{}{"in": "query", "name": "QueryParam", "required": true, "type": "string", "default": "example-query"},
			map[string]interface{}{"in": "body", "name": "player", "required": true, "schema": map[string]interface{}{
				"type": "object", "required": []string{"id", "name"},
				"properties": map[string]interface{}{
					"id":   map[string]interface{}{"type": "integer", "default": 5},
					"name": map[string]interface{}{"type": "string", "default": "Nathan Reline"},
				},
			}},
		},
	}}
	return []map[string]interface{}{
		{"ksuid": strings.Repeat("a", 40), "url": "users.example", "securityDefinitions": map[string]interface{}{}, "paths": map[string]interface{}{
			"/api/v1/users":      map[string]interface{}{"get": op(), "put": op(), "delete": op()},
			"/api/v1/users/{id}": idParam,
			"/admin":             map[string]interface{}{"get": op()},
		}},
		{"ksuid": strings.Repeat("b", 40), "url": "game.example", "securityDefinitions": map[string]interface{}{}, "paths": map[string]interface{}{
			"/onigokko/player": player,
			"/onigokko/scores": map[string]interface{}{"get": op()},
		}},
		{"ksuid": strings.Repeat("c", 40), "url": "decoy.example", "securityDefinitions": map[string]interface{}{}, "paths": map[string]interface{}{
			"/v1/orders":               map[string]interface{}{"get": op(), "post": op()},
			"/v1/orders/{id}":          idParam,
			"/v1/settings":             map[string]interface{}{"put": op(), "patch": op()},
			"/v1/sessions":             map[string]interface{}{"delete": op()},
			"/graphql":                 map[string]interface{}{"get": op(), "post": op()},
			"/internal/health/details": map[string]interface{}{"get": op()},
			"/static/app.js":           map[string]interface{}{"get": op()},
		}},
	}
}

var (
	kiteOnce sync.Once
	kiteFile string
	jsonFile string
	kiteErr  string
)

// testKite compiles the schema with `kr kb compile` once and returns the .kite and .json paths
func testKite(t *testing.T) (kite, schemaJSON string) {
	t.Helper()
	kiteOnce.Do(func() {
		dir, err := os.MkdirTemp("", "kr-e2e-kite-")
		if err != nil {
			kiteErr = err.Error()
			return
		}
		jsonFile = filepath.Join(dir, "apis.json")
		kiteFile = filepath.Join(dir, "apis.kite")
		b, _ := json.Marshal(schema())
		if err := os.WriteFile(jsonFile, b, 0o644); err != nil {
			kiteErr = err.Error()
			return
		}
		r := kr(t, "", "-q", "kb", "compile", jsonFile, kiteFile)
		if r.ExitCode != 0 {
			kiteErr = r.Stderr
		}
	})
	if kiteErr != "" {
		t.Fatalf("failed to compile test schema: %s", kiteErr)
	}
	return kiteFile, jsonFile
}

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}
