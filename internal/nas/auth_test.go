package nas

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestQBSessionRecoveryAndReplayLimit(t *testing.T) {
	for _, status := range []int{401, 403} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var mu sync.Mutex
			logins, calls := 0, 0
			session := "valid"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if r.URL.Path == "/api/v2/auth/login" {
					logins++
					r.ParseForm()
					if r.Form.Get("username") != "fixture-user" || r.Form.Get("password") != "fixture-password" {
						t.Error("credential source not used")
					}
					http.SetCookie(w, &http.Cookie{Name: "SID", Value: session, Path: "/"})
					fmt.Fprint(w, "Ok.")
					return
				}
				calls++
				cookie, err := r.Cookie("SID")
				if err != nil || cookie.Value != session {
					w.WriteHeader(status)
					fmt.Fprint(w, "secret-error-body")
					return
				}
				fmt.Fprint(w, "Ok.")
			}))
			defer server.Close()
			q := &QB{HTTPBoundary: NewHTTPBoundary(server.URL, ""), Downloads: "/downloads"}
			q.SetCredentialSource(func() (string, string, error) { return "fixture-user", "fixture-password", nil })
			if err := q.Add(context.Background(), "magnet:?xt=urn:btih:"+fixtureHash); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			if logins != 1 || calls != 2 {
				t.Fatal(logins, calls)
			}
			session = "restarted"
			mu.Unlock()
			// 模拟已过冷却期后的 qB 重启，必须重新取凭据并登录。
			q.authRetryAt = time.Time{}
			if err := q.Stop(context.Background(), fixtureHash); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if logins != 2 || calls != 4 {
				t.Fatal(logins, calls)
			}
		})
	}
}

func TestQBAuthFailuresDoNotLoopOrLeak(t *testing.T) {
	for _, tc := range []struct {
		name                string
		status, loginStatus int
		loginBody           string
		want                string
		calls               int
	}{
		{"invalid_credentials", 403, 200, "Fails.", "qb_invalid_credentials", 1},
		{"login_forbidden", 403, 403, "fixture-password", "qb_forbidden", 1},
		{"still_unauthorized", 401, 200, "Ok.", "qb_unauthorized", 2},
		{"still_forbidden", 403, 200, "Ok.", "qb_forbidden", 2},
		{"server_failure", 500, 200, "Ok.", "fallback", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, logins := 0, 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v2/auth/login" {
					logins++
					w.WriteHeader(tc.loginStatus)
					fmt.Fprint(w, tc.loginBody)
					return
				}
				calls++
				w.WriteHeader(tc.status)
				fmt.Fprint(w, "secret-response")
			}))
			defer s.Close()
			q := &QB{HTTPBoundary: NewHTTPBoundary(s.URL, ""), Downloads: "/downloads"}
			q.SetCredentialSource(func() (string, string, error) { return "fixture-user", "fixture-password", nil })
			err := q.Stop(context.Background(), fixtureHash)
			if err == nil || qbFailureCode(err, "fallback") != tc.want || strings.Contains(err.Error(), "fixture") || strings.Contains(err.Error(), s.URL) {
				t.Fatal(err)
			}
			if calls != tc.calls {
				t.Fatal("unbounded replay", calls)
			}
			expected := 1
			if tc.status == 500 {
				expected = 0
			}
			if logins != expected {
				t.Fatal("wrong login count", logins)
			}
			if expected == 1 {
				if err = q.Stop(context.Background(), fixtureHash); !errors.Is(err, errQBAuthCooldown) {
					t.Fatal("no auth cooldown", err)
				}
				if logins != 1 {
					t.Fatal("login loop")
				}
			}
		})
	}
}

func TestQBMultipartAuthReplayPreservesExactBody(t *testing.T) {
	var bodies [][]byte
	logins := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/auth/login" {
			logins++
			fmt.Fprint(w, "Ok.")
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		bodies = append(bodies, body)
		if len(bodies) == 1 {
			w.WriteHeader(403)
			return
		}
		fmt.Fprint(w, "Ok.")
	}))
	defer server.Close()
	q := &QB{HTTPBoundary: NewHTTPBoundary(server.URL, ""), Downloads: "/downloads"}
	q.SetCredentialSource(func() (string, string, error) { return "fixture-user", "fixture-password", nil })
	if err := q.AddTorrent(context.Background(), []byte(validTorrent)); err != nil {
		t.Fatal(err)
	}
	if logins != 1 || len(bodies) != 2 || !bytes.Equal(bodies[0], bodies[1]) || !bytes.Contains(bodies[1], []byte(validTorrent)) {
		t.Fatal("multipart replay changed payload")
	}
}
