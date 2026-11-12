package scan

import (
	"testing"
)

// target parsing must never panic, and any target it returns must be usable
func FuzzParseDomain(f *testing.F) {
	for _, s := range []string{"example.com", "http://example.com", "https://example.com:8443/base/", "127.0.0.1:80",
		"[::1]:8080", "http://user:pw@host/", "ftp://x", "http://:80", "host:0", "host:99999", "https://h:443/a?b=c#d", ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		targets, err := ParseDomain(in)
		if err != nil {
			return
		}
		for _, tg := range targets {
			if tg.Hostname == "" {
				t.Errorf("ParseDomain(%q) returned a target without a host", in)
			}
			if tg.Port < 1 || tg.Port > 65535 {
				t.Errorf("ParseDomain(%q) returned port %d", in, tg.Port)
			}
		}
	})
}

func FuzzParseFileWithLen(f *testing.F) {
	for _, s := range []string{"apiroutes-260227", "apiroutes-260227:20000", "a:b:3", ":5", "x:", "x:-1", "x:99999999999999999999"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		fl, err := ParseFileWithLen(in)
		if err == nil && fl.MaxLength < 0 {
			t.Errorf("ParseFileWithLen(%q) accepted a negative length %d", in, fl.MaxLength)
		}
	})
}
