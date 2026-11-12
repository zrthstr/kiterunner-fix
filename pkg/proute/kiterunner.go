package proute

import (
	"net/url"
	"strings"

	"github.com/hashicorp/go-multierror"
	"github.com/zrthstr/kiterunner-fix/pkg/http"
)

func APIsToKiterunnerRoutes(api []API) ([]*http.Route, error) {
	var merr *multierror.Error
	ret := make([]*http.Route, 0)
	for _, v := range api {
		tmp, err := ToKiterunnerRoutes(v)
		if err != nil {
			multierror.Append(merr, err)
		}
		ret = append(ret, tmp...)
	}
	return ret, merr.ErrorOrNil()
}

func ToKiterunnerRoutes(api API) ([]*http.Route, error) {
	var merr *multierror.Error

	ret := make([]*http.Route, 0)
	for _, v := range api.Routes {
		// Skip these options since we don't actually care about the content here
		r, err := v.ToKiterunner(api.Headers(true)...)
		if err != nil {
			multierror.Append(merr, err)
		}
		switch string(r.Method) {
		case "HEAD", "OPTIONS", "CONNECT", "TRACE":
			// we're biased. skip these since they're noisy
			continue
		case "GET", "POST", "PUT", "DELETE", "PATCH":
			// these guys are alright
		}

		r.Source = api.ID
		ret = append(ret, r)
	}
	return ret, merr.ErrorOrNil()
}

func (r Route) ToKiterunner(extraHeaders ...KV) (*http.Route, error) {
	var err error

	method := strings.TrimSpace(strings.ToUpper(r.Method))
	switch method {
	case "HEAD", "OPTIONS", "CONNECT", "TRACE":
		// we're biased. skip these since they're noisy
	case "GET", "POST", "PUT", "DELETE", "PATCH":
		// these guys are alright
	default:
		method = "GET"
	}

	ret := &http.Route{
		Path:   []byte(r.path),
		Method: http.Method(method),
	}
	// header names that are not valid HTTP tokens (e.g. an unrendered "${auth.header}" from a swagger file)
	// make servers reject the whole request with a 400, which then looks like a finding
	for _, h := range r.Headers(true) {
		if validHeaderName(h.Key) {
			ret.Headers = append(ret.Headers, http.Header{Key: h.Key, Value: h.Value})
		}
	}

	for _, h := range extraHeaders {
		if validHeaderName(h.Key) {
			ret.Headers = append(ret.Headers, http.Header{Key: h.Key, Value: h.Value})
		}
	}

	ct := PickContentType(r.ContentType)
	// Overwrite the form-data format with a proper boundary header
	if EncodingFor(ct) == ContentTypeFormData {
		ct = "multipart/form-data; boundary=" + DefaultFormDataBoundary
	}
	ret.Body = r.Body(true, ct)

	// only add content type if there is a body specified
	// or if its a non-get type
	if len(ret.Body) > 0 || (string(ret.Method) != string(http.GET)) {
		ret.Headers = append(ret.Headers, http.Header{Key: "Content-Type", Value: string(ct)})
	}

	path, err := r.Path(false)
	if err != nil {
		return ret, err
	}
	query, err := r.Query(false)

	// some route templates carry a query string ("/operators?page=1&size=3"). it belongs in the query, the
	// "?" was percent encoded into the path before
	if i := strings.IndexByte(path, '?'); i >= 0 {
		templateQuery := path[i+1:]
		path = path[:i]
		if v, perr := url.ParseQuery(templateQuery); perr == nil {
			templateQuery = v.Encode()
		} else {
			templateQuery = url.QueryEscape(templateQuery)
		}
		switch {
		case query == "":
			query = templateQuery
		case templateQuery != "":
			query = templateQuery + "&" + query
		}
	}
	ret.Path = []byte(path)
	ret.Query = []byte(query)
	return ret, err
}

// validHeaderName reports whether s is a valid HTTP header field name (an RFC 7230 token)
func validHeaderName(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range []byte(s) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0:
		default:
			return false
		}
	}
	return true
}
