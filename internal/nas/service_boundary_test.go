package nas

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type serviceContract struct {
	name, host, token, route string
	handler                  func(*Store, string, string) http.Handler
}

func serviceContracts() []serviceContract {
	return []serviceContract{
		{"events", "127.0.0.1:8788", eventTestToken, "/v1/events?after=0", EventHandler},
		{"learning", "127.0.0.1:8789", learningFixtureToken, "/v1/learning/samples?after=0", func(s *Store, token, host string) http.Handler {
			return LearningHandler(s, token, host)
		}},
	}
}

type unreadServiceBody struct{ reads int }

func (b *unreadServiceBody) Read([]byte) (int, error) { b.reads++; return 0, io.EOF }
func (*unreadServiceBody) Close() error               { return nil }

// 在真实两个 Handler 上约束共同身份门，不能用只测试 wrapper 的自证替代接线验证。
func TestServiceRequestIdentityContract(t *testing.T) {
	f := newFixture(t, "fixture.txt", -1)
	for _, service := range serviceContracts() {
		t.Run(service.name, func(t *testing.T) {
			for _, tc := range []struct {
				name, token, authorization, host, origin, fetchSite string
				status                                              int
			}{
				{"valid", service.token, "Bearer " + service.token, service.host, "", "", 200},
				{"minimum token", strings.Repeat("x", 32), "Bearer " + strings.Repeat("x", 32), service.host, "", "", 200},
				{"short token", strings.Repeat("x", 31), "Bearer " + strings.Repeat("x", 31), service.host, "", "", 401},
				{"missing", service.token, "", service.host, "", "", 401},
				{"nonce", service.token, "fixture-browser-nonce", service.host, "", "", 401},
				{"wrong token", service.token, "Bearer fixture-wrong-token-012345678901234", service.host, "", "", 401},
				{"scheme case", service.token, "bearer " + service.token, service.host, "", "", 401},
				{"trailing space", service.token, "Bearer " + service.token + " ", service.host, "", "", 401},
				{"wrong host", service.token, "Bearer " + service.token, "evil.invalid", "", "", 401},
				{"host alias", service.token, "Bearer " + service.token, strings.Replace(service.host, "127.0.0.1", "localhost", 1), "", "", 401},
				{"same origin browser", service.token, "Bearer " + service.token, service.host, "http://" + service.host, "", 401},
				{"cross origin browser", service.token, "Bearer " + service.token, service.host, "http://evil.invalid", "", 401},
				{"fetch metadata", service.token, "Bearer " + service.token, service.host, "", "same-origin", 401},
			} {
				t.Run(tc.name, func(t *testing.T) {
					r := httptest.NewRequest("GET", "http://"+service.host+service.route, nil)
					r.Host = tc.host
					r.Header.Set("Authorization", tc.authorization)
					r.Header.Set("Origin", tc.origin)
					r.Header.Set("Sec-Fetch-Site", tc.fetchSite)
					// 转发头不能把错误的实际 Host 转成受信任服务请求。
					r.Header.Set("X-Forwarded-Host", service.host)
					body := &unreadServiceBody{}
					r.Body = body
					w := httptest.NewRecorder()
					service.handler(f.store, tc.token, service.host).ServeHTTP(w, r)
					checkServiceResponse(t, w, tc.status, map[int]string{401: "service_unauthorized"}[tc.status])
					if body.reads != 0 {
						t.Fatal("identity check or read-only route consumed a body")
					}
				})
			}
		})
	}
}

func checkServiceResponse(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status %d, want %d: %s", w.Code, status, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatal("service response headers changed", w.Header())
	}
	if code != "" {
		var body map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body) != 1 || body["error"] != code {
			t.Fatal("public error contract changed", w.Body.String(), err)
		}
	}
}

// 相同身份门不意味着同一方法策略：事件端口只读405，学习未知方法404，缺命名引擎503。
func TestServiceRoutePoliciesRemainDistinct(t *testing.T) {
	f := newFixture(t, "fixture.txt", -1)
	for _, service := range serviceContracts() {
		t.Run(service.name, func(t *testing.T) {
			handler := service.handler(f.store, service.token, service.host)
			call := func(method, route string, authenticated bool, status int, code string) {
				t.Helper()
				r := httptest.NewRequest(method, "http://"+service.host+route, nil)
				if authenticated {
					r.Header.Set("Authorization", "Bearer "+service.token)
				}
				body := &unreadServiceBody{}
				r.Body = body
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				checkServiceResponse(t, w, status, code)
				if !authenticated && body.reads != 0 {
					t.Fatal("unauthorized request body read")
				}
			}
			for _, route := range []string{"/api/proposal-approve", "/api/naming-confirm", "/unknown", "/v1/naming/requests?after=0", service.route} {
				call("POST", route, false, 401, "service_unauthorized")
			}
			for _, route := range []string{"/api/proposal-approve", "/api/naming-confirm", "/unknown"} {
				call("POST", route, true, 404, "not_found")
			}
			path := strings.Split(service.route, "?")[0]
			for _, query := range []string{"", "?after=-1", "?after=bad", "?after=0&after=1", "?after=0&extra=x"} {
				call("GET", path+query, true, 400, "invalid_cursor")
			}
			if service.name == "events" {
				call("POST", service.route, true, 405, "read_only")
				call("GET", "/v1/learning/samples?after=0", true, 404, "not_found")
				call("GET", "/v1/naming/requests?after=0", true, 404, "not_found")
			} else {
				call("POST", service.route, true, 404, "not_found")
				call("GET", "/v1/events?after=0", true, 404, "not_found")
				call("GET", "/v1/naming/requests?after=0", true, 503, "naming_unavailable")
				handler = LearningHandler(f.store, service.token, service.host, f.engine)
				call("GET", "/v1/naming/requests?after=0", true, 200, "")
			}
		})
	}
}
