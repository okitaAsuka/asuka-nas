package nas

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPBoundaryRejectsRedirectAndRedacts(t *testing.T) {
	hit := false
	dst := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer dst.Close()
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, dst.URL+"?token=secret", 302) }))
	defer src.Close()
	h := NewHTTPBoundary(src.URL, "fake-secret")
	_, e := h.call(context.Background(), "GET", "/?secret=private", nil)
	if e == nil || hit || strings.Contains(e.Error(), "secret") || strings.Contains(e.Error(), src.URL) {
		t.Fatal("redirect or error leak", e)
	}
}

func TestRSSUntrustedAndUnsupported(t *testing.T) {
	for _, input := range []string{`<broken>`, `<rss><channel><item><guid>x</guid><link>https://mikan.invalid/secret</link></item></channel></rss>`, strings.Repeat("x", 2*1024*1024+1)} {
		if _, e := ParseRSS(strings.NewReader(input)); e == nil {
			t.Fatal("bad RSS accepted")
		}
	}
	input := fmt.Sprintf(`<rss><channel><item><guid>g</guid><title>secret</title><enclosure url="magnet:?xt=urn:btih:%s&amp;dn=secret&amp;tr=http://private.invalid"/></item></channel></rss>`, fixtureHash)
	r, e := ParseRSS(strings.NewReader(input))
	if e != nil || len(r) != 1 {
		t.Fatal(r, e)
	}
	if strings.Contains(r[0].Magnet, "secret") || strings.Contains(r[0].Magnet, "tr=") {
		t.Fatal("untrusted magnet parameters retained")
	}
}

func TestQBLoginAndSemanticFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "Fails.") }))
	defer server.Close()
	q := &QB{HTTPBoundary: NewHTTPBoundary(server.URL, ""), Downloads: "/fixture"}
	if e := q.Login(context.Background(), "fake-user", "fake-password"); e == nil {
		t.Fatal("login false success")
	}
	if e := q.Add(context.Background(), "magnet:?xt=urn:btih:"+fixtureHash); e == nil {
		t.Fatal("add false success")
	}
}

func TestDownloaderRejectsBroadOrInjectedCommands(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer s.Close()
	q := &QB{HTTPBoundary: NewHTTPBoundary(s.URL, ""), Downloads: "/fixture"}
	for _, hash := range []string{"all", fixtureHash + "|" + fixtureHash, "secret\n"} {
		if err := q.Stop(context.Background(), hash); err == nil {
			t.Fatal("unsafe stop accepted")
		}
		if _, err := q.Status(context.Background(), hash); err == nil {
			t.Fatal("unsafe status accepted")
		}
	}
	if err := q.Add(context.Background(), "https://private.invalid?token=secret"); err == nil {
		t.Fatal("arbitrary URL add accepted")
	}
	if calls != 0 {
		t.Fatal("unsafe command reached downloader")
	}
}
