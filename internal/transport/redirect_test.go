package transport

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
)

func request(scheme, host string) *http.Request {
	return &http.Request{URL: &url.URL{Scheme: scheme, Host: host, Path: "/x"}}
}

func TestCheckRedirectRules(t *testing.T) {
	origin := request("https", "official.invalid")
	tests := []struct {
		why    string
		policy redirectPolicy
		next   *http.Request
		code   string
	}{
		{"same host, digest", stayOnHost, request("https", "official.invalid"), ""},
		{"cross host, digest", stayOnHost, request("https", "mirror.invalid"), errs.CodeDigestRedirected},
		{"cross host, archive", followAnywhere, request("https", "mirror.invalid"), ""},
		{"downgrade to http", followAnywhere, request("http", "mirror.invalid"), errs.CodeSchemeDowngrade},
		{"downgrade to ftp", followAnywhere, request("ftp", "mirror.invalid"), errs.CodeSchemeDowngrade},
	}
	for _, tt := range tests {
		err := checkRedirect(tt.policy, tt.next, []*http.Request{origin})
		if tt.code == "" {
			if err != nil {
				t.Errorf("%s: %v", tt.why, err)
			}
			continue
		}
		assertFailure(t, err, exitcode.Integrity, tt.code)
	}
}

func TestCheckRedirectStopsAtTheHopLimit(t *testing.T) {
	mk := func(n int) []*http.Request {
		chain := make([]*http.Request, n)
		for i := range chain {
			chain[i] = request("https", "h.invalid")
		}
		return chain
	}
	atLimit := mk(hopLimit)
	if err := checkRedirect(followAnywhere, atLimit[hopLimit-1], atLimit); err != nil {
		t.Errorf("at the limit: %v", err)
	}
	overLimit := mk(hopLimit + 1)
	assertFailure(t, checkRedirect(followAnywhere, overLimit[hopLimit], overLimit),
		exitcode.Network, errs.CodeRedirectLimit)
}

func FuzzCheckRedirect(f *testing.F) {
	f.Add("https://a/x", "https://b/y", uint8(0))
	f.Add("https://a/x", "http://a/y", uint8(1))
	f.Fuzz(func(t *testing.T, origin, next string, policy uint8) {
		o, err := url.Parse(origin)
		if err != nil {
			t.Skip()
		}
		n, err := url.Parse(next)
		if err != nil {
			t.Skip()
		}
		p := followAnywhere
		if policy%2 == 1 {
			p = stayOnHost
		}
		_ = checkRedirect(p, &http.Request{URL: n}, []*http.Request{{URL: o}})
	})
}
