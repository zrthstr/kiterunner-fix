package http

import (
	"testing"

	"github.com/valyala/fasthttp"
)

func FuzzRangeFromString(f *testing.F) {
	for _, s := range []string{"5", "100-105", "0-0", "10-5", "-5", "5-", "a-b", "1-2-3", "", " 1-2"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		r, err := RangeFromString(in)
		if err != nil {
			return
		}
		if r.Min > r.Max {
			t.Errorf("RangeFromString(%q) = %v, min above max", in, r)
		}
		back, err := RangeFromString(r.String())
		if err != nil || back != r {
			t.Errorf("RangeFromString(%q) = %v does not round trip through %q: %v %v", in, r, r.String(), back, err)
		}
	})
}

// a Location header is attacker controlled; following it must never panic
func FuzzUpdateRedirectURL(f *testing.F) {
	for _, s := range []string{"/next", "next", "http://other.example/x", "//other/x", "https://h:99999/", "/a/../b", "?q=1", "", "\x00", "http://[::1"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, location string) {
		base := fasthttp.AcquireURI()
		defer fasthttp.ReleaseURI(base)
		base.Parse(nil, []byte("http://example.com/start/path?x=1"))
		updateRedirectURL(base, nil, []byte(location))
	})
}
