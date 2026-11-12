package e2e

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// paramCase is one route of the parameter schema together with the check a strict API would apply to it.
// The shapes mirror what occurs in Assetnote's routes dataset (enums, arrays with collectionFormat, formData,
// xml bodies, odd content types, stray whitespace and casing).
type paramCase struct {
	path, method string
	op           map[string]interface{}
	check        func(r *http.Request, body []byte) error
}

func p(in, name, typ string, extra ...interface{}) map[string]interface{} {
	m := map[string]interface{}{"in": in, "name": name, "required": true}
	if typ != "" {
		m["type"] = typ
	}
	for i := 0; i+1 < len(extra); i += 2 {
		m[extra[i].(string)] = extra[i+1]
	}
	return m
}

func operation(consumes []string, params ...map[string]interface{}) map[string]interface{} {
	ps := make([]interface{}, len(params))
	for i := range params {
		ps[i] = params[i]
	}
	op := map[string]interface{}{"description": "d", "operationId": "o", "parameters": ps}
	if consumes != nil {
		op["consumes"] = consumes
	}
	return op
}

var (
	uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	intRe  = regexp.MustCompile(`^-?\d+$`)
	// the random paths of kr's wildcard preflight checks
	preflightRe = regexp.MustCompile(`^((auth)?[0-9a-f]{16}(/[0-9a-f]{16})?|A+|)$`)
)

func oneOf(v string, allowed ...string) error {
	for _, a := range allowed {
		if v == a {
			return nil
		}
	}
	return fmt.Errorf("%q is not one of %v", v, allowed)
}

func intIn(v string, min, max int) error {
	n, err := strconv.Atoi(v)
	if err != nil {
		return fmt.Errorf("%q is not an integer", v)
	}
	if n < min || n > max {
		return fmt.Errorf("%d is outside [%d, %d]", n, min, max)
	}
	return nil
}

func mediaType(r *http.Request) string {
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return mt
}

// jsonBody decodes a json object body, requiring a json content type
func jsonBody(r *http.Request, body []byte) (map[string]interface{}, error) {
	if !strings.Contains(mediaType(r), "json") {
		return nil, fmt.Errorf("content type %q is not json", r.Header.Get("Content-Type"))
	}
	var v map[string]interface{}
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, fmt.Errorf("body %q is not a json object: %v", body, err)
	}
	return v, nil
}

func paramCases() []paramCase {
	userSchema := map[string]interface{}{
		"type": "object", "required": []string{"name", "age", "tags", "address", "status"},
		"properties": map[string]interface{}{
			"name":    map[string]interface{}{"type": "string"},
			"age":     map[string]interface{}{"type": "integer", "format": "int32"},
			"score":   map[string]interface{}{"type": "number", "format": "double"},
			"admin":   map[string]interface{}{"type": "boolean"},
			"tags":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			"address": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}}},
			"status":  map[string]interface{}{"type": "string", "enum": []string{"active", "inactive"}},
			"labels":  map[string]interface{}{"type": "object", "additionalProperties": map[string]interface{}{"type": "string"}},
		},
	}
	checkUser := func(r *http.Request, body []byte) error {
		v, err := jsonBody(r, body)
		if err != nil {
			return err
		}
		if _, ok := v["user"]; ok { // a body parameter may be wrapped in its parameter name
			if inner, ok := v["user"].(map[string]interface{}); ok {
				v = inner
			}
		}
		if s, ok := v["name"].(string); !ok || s == "" {
			return fmt.Errorf("name must be a non empty string: %v", v["name"])
		}
		if n, ok := v["age"].(float64); !ok || n != float64(int64(n)) {
			return fmt.Errorf("age must be an integer: %v", v["age"])
		}
		if tags, ok := v["tags"].([]interface{}); !ok || len(tags) == 0 {
			return fmt.Errorf("tags must be a non empty array: %v", v["tags"])
		} else if _, ok := tags[0].(string); !ok {
			return fmt.Errorf("tags must contain strings: %v", tags)
		}
		if a, ok := v["address"].(map[string]interface{}); !ok {
			return fmt.Errorf("address must be an object: %v", v["address"])
		} else if _, ok := a["city"].(string); !ok {
			return fmt.Errorf("address.city must be a string: %v", a)
		}
		if err := oneOf(fmt.Sprint(v["status"]), "active", "inactive"); err != nil {
			return fmt.Errorf("status: %v", err)
		}
		if b, ok := v["admin"]; ok {
			if _, ok := b.(bool); !ok {
				return fmt.Errorf("admin must be a boolean: %v", b)
			}
		}
		return nil
	}
	query := func(name string, check func(string) error) func(*http.Request, []byte) error {
		return func(r *http.Request, _ []byte) error {
			vals, ok := r.URL.Query()[name]
			if !ok {
				return fmt.Errorf("query parameter %s missing (query %q)", name, r.URL.RawQuery)
			}
			for _, v := range vals {
				if err := check(v); err != nil {
					return fmt.Errorf("query %s: %v", name, err)
				}
			}
			return nil
		}
	}

	return []paramCase{
		{"/q/enum", "get", operation(nil, p("query", "order", "string", "enum", []string{"asc", "desc"})),
			query("order", func(v string) error { return oneOf(v, "asc", "desc") })},
		{"/q/enum-int", "get", operation(nil, p("query", "level", "integer", "enum", []int{1, 2, 3})),
			query("level", func(v string) error { return oneOf(v, "1", "2", "3") })},
		{"/q/range", "get", operation(nil, p("query", "limit", "integer", "format", "int32", "minimum", 1, "maximum", 100)),
			query("limit", func(v string) error { return intIn(v, 1, 100) })},
		{"/q/bool", "get", operation(nil, p("query", "flag", "boolean")),
			query("flag", func(v string) error { return oneOf(v, "true", "false") })},
		{"/q/default", "get", operation(nil, p("query", "page", "integer", "default", "3")),
			query("page", func(v string) error { return oneOf(v, "3") })},
		{"/q/pattern", "get", operation(nil, p("query", "code", "string", "pattern", "^[A-Z]{3}$")),
			query("code", func(v string) error {
				if !regexp.MustCompile(`^[A-Z]{3}$`).MatchString(v) {
					return fmt.Errorf("%q does not match ^[A-Z]{3}$", v)
				}
				return nil
			})},
		{"/q/array-multi", "get", operation(nil, p("query", "tags", "array", "items", map[string]interface{}{"type": "string"}, "collectionFormat", "multi")),
			query("tags", func(v string) error {
				if v == "" {
					return fmt.Errorf("empty")
				}
				return nil
			})},
		{"/q/array-csv", "get", operation(nil, p("query", "ids", "array", "items", map[string]interface{}{"type": "integer"}, "collectionFormat", "csv")),
			query("ids", func(v string) error {
				for _, part := range strings.Split(v, ",") {
					if !intRe.MatchString(part) {
						return fmt.Errorf("%q is not a csv list of integers", v)
					}
				}
				return nil
			})},
		{"/q/datetime", "get", operation(nil, p("query", "since", "string", "format", "date-time")),
			query("since", func(v string) error {
				if _, err := time.Parse(time.RFC3339, v); err != nil {
					return fmt.Errorf("%q is not an RFC 3339 date-time", v)
				}
				return nil
			})},
		{"/q/date", "get", operation(nil, p("query", "day", "string", "format", "date")),
			query("day", func(v string) error {
				if _, err := time.Parse("2006-01-02", v); err != nil {
					return fmt.Errorf("%q is not an RFC 3339 full-date", v)
				}
				return nil
			})},
		{"/path/{id}", "get", operation(nil, p("path", "id", "string", "format", "uuid")),
			func(r *http.Request, _ []byte) error {
				id := strings.TrimPrefix(r.URL.Path, "/path/")
				if !uuidRe.MatchString(id) {
					return fmt.Errorf("path id %q is not a uuid", id)
				}
				return nil
			}},
		{"/header/int", "get", operation(nil, p("header", "X-Page", "integer")),
			func(r *http.Request, _ []byte) error {
				if !intRe.MatchString(r.Header.Get("X-Page")) {
					return fmt.Errorf("header X-Page %q is not an integer", r.Header.Get("X-Page"))
				}
				return nil
			}},
		{"/body/json", "post", operation([]string{"application/json"}, p("body", "user", "", "schema", userSchema)), checkUser},
		{"/body/json-charset", "post", operation([]string{"application/json;charset=UTF-8"}, p("body", "user", "", "schema", userSchema)), checkUser},
		{"/body/text-json", "post", operation([]string{"text/json"}, p("body", "user", "", "schema", userSchema)), checkUser},
		{"/body/json-patch", "post", operation([]string{"application/merge-patch+json"}, p("body", "user", "", "schema", userSchema)), checkUser},
		{"/body/xml", "post", operation([]string{"application/xml"}, p("body", "item", "", "schema", map[string]interface{}{
			"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "integer"}, "name": map[string]interface{}{"type": "string"}},
		})), func(r *http.Request, body []byte) error {
			if !strings.Contains(mediaType(r), "xml") {
				return fmt.Errorf("content type %q is not xml", r.Header.Get("Content-Type"))
			}
			var v struct {
				ID   string `xml:"id"`
				Name string `xml:"name"`
			}
			if err := xml.Unmarshal(body, &v); err != nil {
				return fmt.Errorf("body %q is not xml: %v", body, err)
			}
			if !intRe.MatchString(v.ID) || v.Name == "" {
				return fmt.Errorf("xml body %q is missing id/name", body)
			}
			return nil
		}},
		{"/form/urlencoded", "post", operation([]string{"application/x-www-form-urlencoded"},
			p("formData", "name", "string"), p("formData", "count", "integer")),
			func(r *http.Request, body []byte) error {
				if mediaType(r) != "application/x-www-form-urlencoded" {
					return fmt.Errorf("content type %q is not form encoded", r.Header.Get("Content-Type"))
				}
				v, err := url.ParseQuery(string(body))
				if err != nil {
					return fmt.Errorf("body %q is not form encoded: %v", body, err)
				}
				if v.Get("name") == "" || !intRe.MatchString(v.Get("count")) {
					return fmt.Errorf("form %q is missing name or an integer count", body)
				}
				return nil
			}},
		{"/form/multipart", "post", operation([]string{"multipart/form-data"},
			p("formData", "name", "string"), p("formData", "upload", "file")),
			func(r *http.Request, body []byte) error {
				mt, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
				if err != nil || mt != "multipart/form-data" || params["boundary"] == "" {
					return fmt.Errorf("content type %q is not multipart with a boundary", r.Header.Get("Content-Type"))
				}
				mr := multipart.NewReader(strings.NewReader(string(body)), params["boundary"])
				form, err := mr.ReadForm(1 << 20)
				if err != nil {
					return fmt.Errorf("body is not valid multipart: %v", err)
				}
				if len(form.Value["name"])+len(form.File["name"]) == 0 || len(form.Value["upload"])+len(form.File["upload"]) == 0 {
					return fmt.Errorf("multipart body is missing name or upload: values=%v files=%v", form.Value, form.File)
				}
				return nil
			}},
		{"/form/wildcard", "post", operation([]string{"*/*"}, p("formData", "q", "string")),
			func(r *http.Request, body []byte) error {
				ct := r.Header.Get("Content-Type")
				if strings.Contains(ct, "*") {
					return fmt.Errorf("content type %q is a wildcard, not a real media type", ct)
				}
				if mediaType(r) == "application/x-www-form-urlencoded" {
					if v, err := url.ParseQuery(string(body)); err != nil || v.Get("q") == "" {
						return fmt.Errorf("form body %q is missing q", body)
					}
					return nil
				}
				if v, err := jsonBody(r, body); err != nil || v["q"] == nil {
					return fmt.Errorf("body %q of type %q is missing q", body, ct)
				}
				return nil
			}},
		// stray whitespace and casing as they occur in the dataset: " formData", "String "
		{"/messy", "post", operation([]string{"application/x-www-form-urlencoded"},
			p(" formData", "msg", "String "), p("query", "flag", "boolean ")),
			func(r *http.Request, body []byte) error {
				v, err := url.ParseQuery(string(body))
				if err != nil || v.Get("msg") == "" {
					return fmt.Errorf("form body %q is missing msg", body)
				}
				if err := oneOf(r.URL.Query().Get("flag"), "true", "false"); err != nil {
					return fmt.Errorf("query flag: %v", err)
				}
				return nil
			}},
	}
}

// Every request kr builds from the schema must be one a strict API accepts: the route answers 200 only when
// the parameters, body and content type are valid, and 422 with the reason otherwise.
func TestRequestsMatchSchema(t *testing.T) {
	cases := paramCases()
	paths := map[string]interface{}{}
	for _, c := range cases {
		if _, ok := paths[c.path]; !ok {
			paths[c.path] = map[string]interface{}{}
		}
		paths[c.path].(map[string]interface{})[c.method] = c.op
	}
	b, _ := json.Marshal([]map[string]interface{}{{"ksuid": strings.Repeat("e", 40), "url": "params.example", "securityDefinitions": map[string]interface{}{}, "paths": paths}})
	dir := t.TempDir()
	schemaFile, kite := filepath.Join(dir, "params.json"), filepath.Join(dir, "params.kite")
	if err := os.WriteFile(schemaFile, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if r := kr(t, "", "-q", "kb", "compile", schemaFile, kite); r.ExitCode != 0 {
		t.Fatalf("kb compile failed: %s", tail(r.Stderr))
	}

	match := func(c paramCase, r *http.Request) bool {
		if !strings.EqualFold(c.method, r.Method) {
			return false
		}
		if strings.Contains(c.path, "{") {
			prefix := c.path[:strings.Index(c.path, "{")]
			// kr's wildcard preflight probes random paths below each directory; they are not route requests
			return strings.HasPrefix(r.URL.Path, prefix) && !preflightRe.MatchString(strings.TrimPrefix(r.URL.Path, prefix))
		}
		return r.URL.Path == c.path
	}
	tg := newTarget(t, false, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		for _, c := range cases {
			if match(c, r) {
				if err := c.check(r, body); err != nil {
					reply(w, 422, "text/plain", err.Error())
					return
				}
				reply(w, 200, "text/plain", "accepted "+c.path)
				return
			}
		}
		reply(w, 404, "text/plain", "not found")
	})

	// every route here is valid, so more than 10 consecutive hits would quarantine the host as a wildcard
	// (--quarantine-threshold, by design) and leave routes unrequested
	r := kr(t, "", "-q", "-o", "json", "scan", tg.URL, "-w", kite, "--kitebuilder-full-scan", "-x", "5", "--quarantine-threshold", "0")
	if r.ExitCode != 0 {
		t.Fatalf("scan failed: %s", tail(r.Stderr))
	}
	got := map[string]int{}
	for _, h := range parseHits(t, r.Stdout) {
		if len(h.Responses) > 0 {
			got[strings.ToLower(h.Method)+" "+h.Path] = h.Responses[0].SC
		}
	}
	if len(got) < len(cases)-1 {
		t.Errorf("expected every route to be reported as a hit, got %d of %d: %v", len(got), len(cases), got)
	}

	for _, c := range cases {
		c := c
		t.Run(strings.TrimPrefix(c.path, "/"), func(t *testing.T) {
			var reqs []exchange
			for _, e := range tg.requests() {
				hr := &http.Request{Method: e.Method, URL: &url.URL{Path: e.Path}}
				if match(c, hr) {
					reqs = append(reqs, e)
				}
			}
			if len(reqs) == 0 {
				t.Fatalf("kr never requested %s %s", c.method, c.path)
			}
			e := reqs[len(reqs)-1]
			hr, _ := http.NewRequest(e.Method, "http://x"+e.Path+"?"+e.Query, nil)
			hr.Header = e.Header
			if err := c.check(hr, []byte(e.Body)); err != nil {
				t.Errorf("a strict API would reject this request: %v\n  %s %s?%s\n  content-type: %q\n  body: %q",
					err, e.Method, e.Path, e.Query, e.Header.Get("Content-Type"), e.Body)
			}
		})
	}
}
