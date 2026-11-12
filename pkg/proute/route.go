package proute

import (
	"fmt"
	"io"
	"net/url"
	"strings"
	"unicode"

	"github.com/valyala/fasttemplate"
)

// Route is a request to be made
type Route struct {
	TemplatePath string  // the raw path with template locations. This should begin with a '/'
	PathCrumbs   []Crumb // Pieces of the path
	path         []byte  // cached rendered path
	query        []byte  // cached rendered query

	Method       string
	HeaderCrumbs []Crumb // can be static or various types
	headers      []KV

	QueryCrumbs []Crumb
	queryParams []KV

	BodyCrumbs  []Crumb
	body        []byte // rendered body
	ContentType []ContentType
}

func (r Route) QueryParams(generate bool) []KV {
	if generate || len(r.queryParams) == 0 {
		r.queryParams = r.queryParams[:0]
		for _, v := range r.QueryCrumbs {
			r.queryParams = append(r.queryParams, KV{Key: v.Key(), Value: scalarValue(v)})
		}
	}
	return r.queryParams
}

type ContentType string

var (
	ContentTypeAny         ContentType = "any"
	ContentTypeJSON        ContentType = "application/json"
	ContentTypeFormData    ContentType = "multipart/form-data"
	ContentTypeXML         ContentType = "text/xml"
	ContentTypePlain       ContentType = "text/plain"
	ContentTypeFormEncoded ContentType = "application/x-www-form-urlencoded"
)

func DefaultValTagFunc(defaultv []byte, m map[string][]byte) func(w io.Writer, tag string) (int, error) {
	return func(w io.Writer, tag string) (int, error) {
		v, ok := m[tag]
		if !ok {
			return w.Write(defaultv)
		}
		if v == nil {
			return 0, nil
		}
		return w.Write(v)
	}
}

// Path returns a rendered path (with all the elements populated as a string)
// calling with generate=true will generate a new path using the crumbs provided. otherwise a cached
// version will be returned.
// we generate the path by substituting the values into the handlebars-esque template string provided
func (r Route) Path(generate bool) (string, error) {
	var err error
	if generate || len(r.path) == 0 {
		r.path = append(r.path[:0], r.TemplatePath...)
		t, err := fasttemplate.NewTemplate(r.TemplatePath, "{", "}")
		if err != nil {
			return r.TemplatePath, fmt.Errorf("failed to compile template: %w", err)
		}

		// generate our template string. by default we use 42 as a default value as its both a string and a numeric
		vals := make(map[string][]byte)
		for _, v := range r.PathCrumbs {
			vals[v.Key()] = []byte(v.Value())
		}
		r.path = append(r.path[:0], t.ExecuteFuncString(DefaultValTagFunc([]byte("42"), vals))...)

		if strings.ContainsAny(string(r.path), "{}") {
			err = fmt.Errorf("path still contains template tokens")
			// log.Trace().Str("base", r.TemplatePath).Str("result", string(r.path)).Msg("rendered path still contains template tokens")
		}
	}
	return string(r.path), err
}

// Query will generate the full query string not including the ? e.g. foo=bar&baz=boo
// Query Parameters will be pulled from the route QueryCrumbs
// We can add extra query params. These will be written first, and the route query params will override the provided
func (r Route) Query(generate bool, extraParams ...KV) (string, error) {
	if generate || len(r.query) == 0 {

		// only generate if there are params to generate
		if len(extraParams) == 0 && len(r.QueryParams(generate)) == 0 {
			return "", nil
		}

		params := make([]KV, 0)
		params = append(params, extraParams...)
		params = append(params, r.QueryParams(generate)...)

		qp := make(url.Values)
		for _, v := range params {
			qp.Add(v.Key, v.Value)
		}
		r.query = append(r.query, qp.Encode()...)
	}
	return string(r.query), nil
}

// Path returns a rendered body (with all the elements populated as a string)
// calling with generate=true will generate a new path using the crumbs provided. otherwise a cached
// version will be returned
// ContentType can be a specified contentType, or ContentTypeAny. If Any, this will attempt to deduce
// the content type based on the route data
func (r Route) Body(generate bool, contentType ContentType) []byte {
	if generate || len(r.body) == 0 {
		r.body = r.body[:0]
		enc := EncodingFor(contentType)
		// need to handle the case of 1 xml BodyCrumb thats the entire object
		// we don't want to wrap the object with pointless tags
		if len(r.BodyCrumbs) == 1 && enc == ContentTypeXML {
			r.body = []byte(MarshalXMLCrumb(r.BodyCrumbs[0], CrumbOptContentType(enc)))
		} else {
			r.body = []byte(ObjectCrumb{Name: "root", Elements: r.BodyCrumbs}.Value(CrumbOptContentType(enc)))
		}
	}
	return r.body
}

// EncodingFor maps a content type to the body encoding kr knows how to produce. Content types were matched
// exactly before, so e.g. application/xml, text/json or application/json;charset=UTF-8 got a form encoded body
func EncodingFor(ct ContentType) ContentType {
	v := strings.ToLower(string(ct))
	switch {
	case strings.Contains(v, "json"):
		return ContentTypeJSON
	case strings.Contains(v, "xml"):
		return ContentTypeXML
	case strings.Contains(v, "multipart/form-data"):
		return ContentTypeFormData
	}
	return ContentTypeFormEncoded
}

// PickContentType chooses the content type to send from an operation's consumes list: the first concrete
// media type. Wildcards like */* are not valid Content-Type values
func PickContentType(consumes []ContentType) ContentType {
	for _, c := range consumes {
		c = ContentType(strings.TrimSpace(string(c)))
		if c != "" && !strings.Contains(string(c), "*") {
			return c
		}
	}
	return ContentTypeFormEncoded
}

// scalarValue is the value of a crumb used as a query parameter or header. An array is sent as one of its
// elements, which is valid for every collectionFormat (csv, multi, pipes, ...)
func scalarValue(c Crumb) string {
	if a, ok := c.(ArrayCrumb); ok && a.Element != nil {
		return scalarValue(a.Element)
	}
	return c.Value()
}

func (r Route) Headers(generate bool) []KV {
	if generate || len(r.headers) == 0 {
		r.headers = r.headers[:0]
		for _, v := range r.HeaderCrumbs {
			// strip not spaces because that makes it an invalid header
			k := stripNonSpaceWhitespace(v.Key())
			val := stripNonSpaceWhitespace(scalarValue(v))
			r.headers = append(r.headers, KV{Key: k, Value: val})
		}
	}
	return r.headers
}

func stripNonSpaceWhitespace(v string) string {
	buf := make([]byte, 0, len(v))
	for _, c := range v {
		if unicode.IsSpace(c) && c != ' ' {
			continue
		}
		buf = append(buf, string(c)...)
	}
	return string(buf)
}

type KV struct {
	Key   string
	Value string
}
