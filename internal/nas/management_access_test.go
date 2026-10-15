package nas

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// 全部口令、CA、证书仅属于测试 fixture；不读取或创建部署访问凭据。
const accessTestPassword = "synthetic-only-password-0123456789"
const accessTestOrigin = "https://nas.example.test:8787"

func accessTestVerifier(t *testing.T) (string, managementVerifier) {
	t.Helper()
	salt := []byte("synthetic-salt-0123456789")
	key, err := pbkdf2.Key(sha256.New, accessTestPassword, salt, 600000, 32)
	if err != nil {
		t.Fatal(err)
	}
	raw := "pbkdf2-sha256$600000$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(key)
	v, err := parseManagementVerifier(raw)
	if err != nil {
		t.Fatal(err)
	}
	return raw, v
}

func accessTestConfig(t *testing.T) ManagementAccessConfig {
	t.Helper()
	root := t.TempDir()
	return ManagementAccessConfig{Enabled: true, Listen: "127.0.0.1:8787", Origin: accessTestOrigin, Username: "admin", PasswordHashEnv: "NAS_ACCESS_SYNTHETIC_HASH", TLSCertFile: filepath.Join(root, "server.crt"), TLSKeyFile: filepath.Join(root, "server.key")}
}

func accessTestRequest(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, accessTestOrigin+path, strings.NewReader(body))
	r.RemoteAddr = "192.168.20.4:12000"
	if method == "POST" {
		r.Header.Set("Origin", accessTestOrigin)
		r.Header.Set("Content-Type", "application/json")
	}
	return r
}

// 生产 Management 使用同一 boundary/authorize；sentinel 证明非法请求无法到达业务处理器。
func accessTestHandle(a *managementAccess, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	if !managementBoundary(w, r, "nas.example.test:8787", accessTestOrigin) {
		return w
	}
	if _, handled := a.authorize(w, r, accessTestOrigin); !handled {
		w.WriteHeader(204)
	}
	return w
}

func accessTestLogin(t *testing.T, a *managementAccess, previous *http.Cookie) (*http.Cookie, managementSessionDTO) {
	t.Helper()
	r := accessTestRequest("POST", "/api/login", `{"username":"admin","password":"`+accessTestPassword+`"}`)
	if previous != nil {
		r.AddCookie(previous)
	}
	w := accessTestHandle(a, r)
	var v managementSessionDTO
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil || !v.Authenticated || v.Mode != "authenticated_https" || len(v.CSRF) != 64 {
		t.Fatal("login failed", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing cookie")
	}
	c := cookies[0]
	if c.Name != managementCookie || len(c.Value) != 64 || !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" || c.Domain != "" || c.MaxAge != 43200 || c.Expires.Unix() != v.ExpiresAt {
		t.Fatal("unsafe cookie attributes")
	}
	if strings.Contains(w.Body.String(), c.Value) || strings.Contains(w.Body.String(), accessTestPassword) {
		t.Fatal("session or password exposed in JSON")
	}
	return c, v
}

func TestManagementAccessConfiguration(t *testing.T) {
	c := accessTestConfig(t)
	if err := c.Validate("EVENT_TOKEN", "LEARNING_TOKEN"); err != nil {
		t.Fatal(err)
	}
	for _, listen := range []string{"0.0.0.0:8787", "[::]:8787", "192.168.1.9:8443", "[fd00::9]:8443", "[::1]:8443"} {
		v := c
		v.Listen = listen
		if err := v.Validate(); err != nil {
			t.Fatal("explicit private/container listen rejected", listen, err)
		}
	}
	for _, origin := range []string{"https://localhost", "https://nas.example.test", "https://127.0.0.1:8787", "https://[::1]:8787", "https://[fd00::9]"} {
		v := c
		v.Origin = origin
		if err := v.Validate(); err != nil {
			t.Fatal("valid explicit origin rejected", origin, err)
		}
	}
	for _, listen := range []string{"", ":8787", "localhost:8787", "8.8.8.8:8787", "[2001:4860:4860::8888]:8787", "169.254.1.2:8787", "127.0.0.1:0", "127.0.0.1:65536", "127.0.0.1:08787"} {
		v := c
		v.Listen = listen
		if err := v.Validate(); err == nil {
			t.Fatal("unsafe listen accepted", listen)
		}
	}
	for _, origin := range []string{"", "http://nas.example.test", "https://nas.example.test/", "https://nas.example.test?", "https://nas.example.test?token=secret", "https://nas.example.test#fragment", "https://u:p@nas.example.test", "https://NAS.example.test", "https://nas.example.test.", "https://nas.example.test:", "https://nas.example.test:0", "https://nas.example.test:08787", "https://nas.example.test:65536", "https://nas.example.test:443", "https://0.0.0.0", "https://[::]", "https://[0:0:0:0:0:0:0:1]", "https://127.1", "https://0x7f000001", "https://nas.123", "https://8.8.8.8", "https://bad_name", "https://-bad", "https://a..b", "https://[fe80::1%25eth0]"} {
		v := c
		v.Origin = origin
		if err := v.Validate(); err == nil {
			t.Fatal("unsafe origin accepted", origin)
		}
	}
	for _, mutate := range []func(*ManagementAccessConfig){
		func(v *ManagementAccessConfig) { v.Username = "" },
		func(v *ManagementAccessConfig) { v.Username = "admin\n" },
		func(v *ManagementAccessConfig) { v.Username = strings.Repeat("a", 65) },
		func(v *ManagementAccessConfig) { v.PasswordHashEnv = "bad-name" },
		func(v *ManagementAccessConfig) { v.TLSCertFile = "relative.crt" },
		func(v *ManagementAccessConfig) { v.TLSKeyFile = "" },
	} {
		v := c
		mutate(&v)
		if v.Validate() == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
	if err := c.Validate(c.PasswordHashEnv); err == nil || err.Error() != "management_identity_reused" {
		t.Fatal("same credential reference accepted", err)
	}
	// 禁用配置不读文件/凭据；默认回环模式由原 listener 继续限制。
	if err := (ManagementAccessConfig{}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestManagementAccessVerifierAndIdentityIsolation(t *testing.T) {
	raw, v := accessTestVerifier(t)
	if !v.matches(accessTestPassword) || v.matches(accessTestPassword+"x") {
		t.Fatal("verifier mismatch")
	}
	parts := strings.Split(raw, "$")
	for _, bad := range []string{"", "secret", raw + " ", strings.Replace(raw, "600000", "599999", 1), strings.Replace(raw, "600000", "1000001", 1), strings.Replace(raw, "600000", "0600000", 1), strings.Replace(raw, "sha256", "sha1", 1), "pbkdf2-sha256$600000$YQ$" + parts[3], "pbkdf2-sha256$600000$" + parts[2] + "$YQ", raw + "=", strings.Repeat("x", 300)} {
		if _, err := parseManagementVerifier(bad); err == nil {
			t.Fatal("malformed verifier accepted")
		}
	}
	c := accessTestConfig(t)
	t.Setenv(c.PasswordHashEnv, raw)
	t.Setenv("NAS_ACCESS_SYNTHETIC_EVENT", accessTestPassword)
	if _, _, err := prepareManagementAccess(c, "NAS_ACCESS_SYNTHETIC_EVENT"); err == nil || err.Error() != "management_identity_reused" {
		t.Fatal("same actual service password accepted", err)
	}
	t.Setenv(c.PasswordHashEnv, "invalid synthetic verifier")
	if _, _, err := prepareManagementAccess(c); err == nil || strings.Contains(err.Error(), "synthetic verifier") {
		t.Fatal("missing verifier accepted or leaked", err)
	}
}

func TestManagementAccessBootstrapAndSecurityMatrix(t *testing.T) {
	_, v := accessTestVerifier(t)
	a := newManagementAccess("admin", v)
	for _, tc := range []struct {
		name, method, path string
		edit               func(*http.Request)
		want               int
		code               string
	}{
		{"bootstrap", "GET", "/api/session", nil, 200, ""},
		{"static index", "GET", "/", nil, 204, ""},
		{"static script", "GET", "/app.js", nil, 204, ""},
		{"static css", "GET", "/style.css", nil, 204, ""},
		{"private file", "GET", "/config.json", nil, 404, "not_found"},
		{"private jobs", "GET", "/api/jobs", nil, 401, "authentication_required"},
		{"private health", "GET", "/api/health", nil, 401, "authentication_required"},
		{"private write", "POST", "/api/batch-confirm", nil, 401, "authentication_required"},
		{"anonymous logout", "POST", "/api/logout", nil, 401, "authentication_required"},
		{"host", "GET", "/api/session", func(r *http.Request) { r.Host = "evil.example.test" }, 403, "origin_rejected"},
		{"origin", "GET", "/api/session", func(r *http.Request) { r.Header.Set("Origin", "https://evil.example.test") }, 403, "origin_rejected"},
		{"null origin", "GET", "/api/session", func(r *http.Request) { r.Header.Set("Origin", "null") }, 403, "origin_rejected"},
		{"duplicate origin", "POST", "/api/login", func(r *http.Request) { r.Header.Add("Origin", accessTestOrigin) }, 403, "origin_rejected"},
		{"cross site", "GET", "/api/session", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }, 403, "origin_rejected"},
		{"missing origin", "POST", "/api/login", func(r *http.Request) { r.Header.Del("Origin") }, 403, "csrf_rejected"},
		{"form login", "POST", "/api/login", func(r *http.Request) { r.Header.Set("Content-Type", "application/x-www-form-urlencoded") }, 403, "csrf_rejected"},
		{"duplicate content type", "POST", "/api/login", func(r *http.Request) { r.Header.Add("Content-Type", "application/json") }, 403, "csrf_rejected"},
		{"service identity", "GET", "/api/session", func(r *http.Request) { r.Header.Set("Authorization", "Bearer synthetic-service-token") }, 403, "csrf_rejected"},
		{"basic identity", "POST", "/api/login", func(r *http.Request) { r.SetBasicAuth("admin", accessTestPassword) }, 403, "csrf_rejected"},
		{"query credential", "POST", "/api/login?password=synthetic", nil, 400, "invalid_request"},
		{"session query", "GET", "/api/session?", nil, 400, "invalid_request"},
		{"login method", "GET", "/api/login", nil, 405, "method_rejected"},
		{"logout method", "GET", "/api/logout", nil, 405, "method_rejected"},
		{"session method", "POST", "/api/session", nil, 405, "method_rejected"},
		{"options", "OPTIONS", "/api/login", nil, 405, "method_rejected"},
		{"forwarded TLS", "GET", "/api/session", func(r *http.Request) {
			r.TLS = nil
			r.Header.Set("X-Forwarded-Proto", "https")
			r.Header.Set("Forwarded", "proto=https")
		}, 403, "https_required"},
		{"forwarded host", "GET", "/api/session", func(r *http.Request) { r.Host = "evil"; r.Header.Set("X-Forwarded-Host", "nas.example.test:8787") }, 403, "origin_rejected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := accessTestRequest(tc.method, tc.path, `{}`)
			if tc.edit != nil {
				tc.edit(r)
			}
			w := accessTestHandle(a, r)
			if w.Code != tc.want || tc.code != "" && !strings.Contains(w.Body.String(), `"error":"`+tc.code+`"`) {
				t.Fatal(w.Code, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Access-Control-Allow-Origin") != "" || w.Header().Get("Content-Security-Policy") == "" {
				t.Fatal("response boundary missing")
			}
			if strings.Contains(w.Body.String(), "csrf\"") || strings.Contains(w.Body.String(), "synthetic-service-token") {
				t.Fatal("anonymous data exposure")
			}
		})
	}
}

func TestManagementAccessLoginSessionCSRFAndLogout(t *testing.T) {
	_, v := accessTestVerifier(t)
	a := newManagementAccess("admin", v)
	now := time.Unix(1790000000, 0)
	a.now = func() time.Time { return now }
	for _, username := range []string{"other-user", "admin"} {
		body := `{"username":"` + username + `","password":"incorrect-synthetic-password"}`
		w := accessTestHandle(a, accessTestRequest("POST", "/api/login", body))
		if w.Code != 401 || w.Body.String() != "{\"error\":\"authentication_failed\"}\n" || len(w.Result().Cookies()) != 0 {
			t.Fatal("credential mismatch exposed or accepted")
		}
	}
	c1, s1 := accessTestLogin(t, a, nil)
	c2, s2 := accessTestLogin(t, a, nil)
	if c1.Value == c2.Value || s1.CSRF == s2.CSRF || s1.ExpiresAt != now.Add(managementSessionTTL).Unix() {
		t.Fatal("session isolation or expiry")
	}
	for _, nonce := range []string{"", "process-nonce", s2.CSRF} {
		r := accessTestRequest("POST", "/api/batch-confirm", `{}`)
		r.AddCookie(c1)
		r.Header.Set("X-CSRF-Nonce", nonce)
		if w := accessTestHandle(a, r); w.Code != 403 {
			t.Fatal("cross-session/absent nonce accepted", w.Code)
		}
	}
	r := accessTestRequest("POST", "/api/batch-confirm", `{}`)
	r.AddCookie(c1)
	r.Header.Set("X-CSRF-Nonce", s1.CSRF)
	if w := accessTestHandle(a, r); w.Code != 204 {
		t.Fatal("valid session blocked", w.Code)
	}
	r.Header.Add("X-CSRF-Nonce", s1.CSRF)
	if w := accessTestHandle(a, r); w.Code != 403 {
		t.Fatal("duplicate CSRF accepted")
	}
	r = accessTestRequest("GET", "/api/jobs", "")
	r.AddCookie(c1)
	r.AddCookie(c1)
	if w := accessTestHandle(a, r); w.Code != 401 {
		t.Fatal("duplicate cookie accepted")
	}
	now = now.Add(2 * time.Hour)
	r = accessTestRequest("GET", "/api/session", "")
	r.AddCookie(c1)
	w := accessTestHandle(a, r)
	var fetched managementSessionDTO
	json.Unmarshal(w.Body.Bytes(), &fetched)
	if fetched != s1 {
		t.Fatal("read extended or changed session")
	}
	// 重新登录撤销旧 cookie/nonce；其他浏览器不受影响。
	c3, s3 := accessTestLogin(t, a, c1)
	if c3.Value == c1.Value || s3.CSRF == s1.CSRF {
		t.Fatal("session fixation")
	}
	for _, c := range []*http.Cookie{c1, c2, c3} {
		r := accessTestRequest("GET", "/api/jobs", "")
		r.AddCookie(c)
		want := 204
		if c == c1 {
			want = 401
		}
		if w := accessTestHandle(a, r); w.Code != want {
			t.Fatal("replacement invalidation", w.Code)
		}
	}
	r = accessTestRequest("POST", "/api/logout", `{}`)
	r.AddCookie(c3)
	r.Header.Set("X-CSRF-Nonce", s3.CSRF)
	w = accessTestHandle(a, r)
	cleared := w.Result().Cookies()
	if w.Code != 200 || len(cleared) != 1 || cleared[0].MaxAge != -1 || !cleared[0].Secure || !cleared[0].HttpOnly || cleared[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("logout failed")
	}
	if w := accessTestHandle(a, r); w.Code != 401 {
		t.Fatal("logout replay retained authority")
	}
	r = accessTestRequest("GET", "/api/jobs", "")
	r.AddCookie(c2)
	if w := accessTestHandle(a, r); w.Code != 204 {
		t.Fatal("logout revoked another browser")
	}
	// 重启相同 verifier 也不恢复旧会话；期限边界没有滑动续期。
	if w := accessTestHandle(newManagementAccess("admin", v), r); w.Code != 401 {
		t.Fatal("restart retained authority")
	}
	now = time.Unix(s2.ExpiresAt, 0)
	if w := accessTestHandle(a, r); w.Code != 401 {
		t.Fatal("expired session accepted")
	}
}

func TestManagementAccessLimitsAndFailurePaths(t *testing.T) {
	_, v := accessTestVerifier(t)
	newAccess := func() *managementAccess { return newManagementAccess("admin", v) }
	for _, body := range []string{`{}`, `null`, `[]`, "{\"username\":\"admin\",\"password\":\"synthetic-invalid-utf8-\xff\"}", `{"username":"admin","password":"short"}`, `{"username":"admin","password":"` + strings.Repeat("x", 1025) + `"}`, `{"username":"admin","password":"` + accessTestPassword + `","unknown":true}`, `{"username":"admin","password":"` + accessTestPassword + `"}{}`, strings.Repeat(" ", 4097)} {
		w := accessTestHandle(newAccess(), accessTestRequest("POST", "/api/login", body))
		if w.Code != 400 || len(w.Result().Cookies()) != 0 {
			t.Fatal("invalid login accepted", w.Code)
		}
	}
	a := newAccess()
	a.random = strings.NewReader("")
	w := accessTestHandle(a, accessTestRequest("POST", "/api/login", `{"username":"admin","password":"`+accessTestPassword+`"}`))
	if w.Code != 503 || len(a.sessions) != 0 || len(w.Result().Cookies()) != 0 {
		t.Fatal("random failure granted authority")
	}
	a = newAccess()
	a.verify <- struct{}{}
	w = accessTestHandle(a, accessTestRequest("POST", "/api/login", `{}`))
	if w.Code != 429 || w.Header().Get("Retry-After") != "1" {
		t.Fatal("KDF concurrency was not bounded")
	}
	<-a.verify
	now := time.Unix(1790000000, 0)
	a.now = func() time.Time { return now }
	// 有界 fixture 填满 session，避免用大量昂贵 KDF 来测试单一容量不变量。
	for i := 0; i < managementSessionLimit; i++ {
		a.sessions[sha256.Sum256([]byte(fmt.Sprintf("fixture-%d", i)))] = managementSession{csrf: "synthetic", expires: now.Add(time.Hour)}
	}
	w = accessTestHandle(a, accessTestRequest("POST", "/api/login", `{"username":"admin","password":"`+accessTestPassword+`"}`))
	if w.Code != 429 || w.Header().Get("Retry-After") != "3600" || !strings.Contains(w.Body.String(), "session_limit") || len(a.sessions) != managementSessionLimit || len(w.Result().Cookies()) != 0 {
		t.Fatal("full capacity admitted or evicted", w.Code, w.Body.String())
	}
	old := &http.Cookie{Name: managementCookie, Value: strings.Repeat("a", 64)}
	delete(a.sessions, sha256.Sum256([]byte("fixture-0")))
	a.sessions[sha256.Sum256([]byte(old.Value))] = managementSession{csrf: "old", expires: now.Add(time.Hour)}
	c, _ := accessTestLogin(t, a, old)
	if len(a.sessions) != managementSessionLimit {
		t.Fatal("replacement changed capacity")
	}
	if _, found := a.sessions[sha256.Sum256([]byte(old.Value))]; found || c.Value == old.Value {
		t.Fatal("replacement did not revoke old")
	}
	now = now.Add(managementSessionTTL)
	accessTestLogin(t, a, nil)
	if len(a.sessions) != 1 {
		t.Fatal("expired entries not reclaimed")
	}
}

func TestManagementAccessThrottlingBoundedAndConcurrent(t *testing.T) {
	_, v := accessTestVerifier(t)
	a := newManagementAccess("admin", v)
	now := time.Unix(1790000000, 0)
	a.now = func() time.Time { return now }
	for i := 0; i < 5; i++ {
		r := accessTestRequest("POST", "/api/login", `{}`)
		r.Header.Set("X-Forwarded-For", fmt.Sprintf("10.1.1.%d", i))
		r.Header.Set("Forwarded", fmt.Sprintf("for=10.2.2.%d", i))
		if w := accessTestHandle(a, r); w.Code != 400 {
			t.Fatal("unexpected first attempts", w.Code)
		}
	}
	w := accessTestHandle(a, accessTestRequest("POST", "/api/login", `{}`))
	if w.Code != 429 || w.Header().Get("Retry-After") != "60" {
		t.Fatal("IP throttle bypassed", w.Code)
	}
	now = now.Add(time.Minute)
	// 并发 IP 不能突破同一个全局预算；在 -race 下运行。
	var wg sync.WaitGroup
	var mu sync.Mutex
	accepted := 0
	for i := 0; i < 80; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if a.claimLogin(fmt.Sprintf("10.20.0.%d:4321", i+1)) == 0 {
				mu.Lock()
				accepted++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if accepted != 20 || len(a.ips) != 20 {
		t.Fatal("global reservation not bounded", accepted, len(a.ips))
	}
	// 直接填充容量边界，证明新 IP 不会通过驱逐未到期 bucket 绕过节流。
	a.ips = map[string]managementLoginBucket{}
	a.global = managementLoginBucket{}
	for i := 0; i < managementIPLimit; i++ {
		a.ips[fmt.Sprintf("fixture-%d", i)] = managementLoginBucket{count: 1, until: now.Add(time.Minute)}
	}
	if a.claimLogin("10.30.0.1:4321") <= 0 || len(a.ips) != managementIPLimit {
		t.Fatal("IP map capacity bypassed")
	}
	now = now.Add(time.Minute)
	if a.claimLogin("10.30.0.1:4321") != 0 || len(a.ips) != 1 {
		t.Fatal("IP expiry failed")
	}
	if a.claimLogin("not-an-ip") <= 0 {
		t.Fatal("untrusted remote address accepted")
	}
}

// 仅为 synthetic TLS 测试创建临时 CA，客户端显式信任它，不使用 InsecureSkipVerify。
func accessTestCertificate(t *testing.T, c ManagementAccessConfig, host string, notBefore, notAfter time.Time, usage x509.ExtKeyUsage) *x509.CertPool {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Synthetic NAS test CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(48 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ca, err = x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), NotBefore: notBefore, NotAfter: notAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage}}
	if ip := net.ParseIP(host); ip != nil {
		leaf.IPAddresses = []net.IP{ip}
	} else {
		leaf.DNSNames = []string{host}
	}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	priv, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(c.TLSCertFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(c.TLSKeyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: priv}), 0600); err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	return roots
}

func TestManagementAccessTLSValidationAndTrustedClient(t *testing.T) {
	raw, _ := accessTestVerifier(t)
	c := accessTestConfig(t)
	t.Setenv(c.PasswordHashEnv, raw)
	if _, _, err := prepareManagementAccess(c); err == nil || err.Error() != "management_tls_unavailable" {
		t.Fatal("missing certificate accepted", err)
	}
	now := time.Now()
	for _, tc := range []struct {
		name, host    string
		before, after time.Time
		usage         x509.ExtKeyUsage
	}{
		{"expired", "nas.example.test", now.Add(-2 * time.Hour), now.Add(-time.Hour), x509.ExtKeyUsageServerAuth},
		{"future", "nas.example.test", now.Add(time.Hour), now.Add(2 * time.Hour), x509.ExtKeyUsageServerAuth},
		{"wrong host", "other.example.test", now.Add(-time.Hour), now.Add(time.Hour), x509.ExtKeyUsageServerAuth},
		{"client only", "nas.example.test", now.Add(-time.Hour), now.Add(time.Hour), x509.ExtKeyUsageClientAuth},
	} {
		t.Run(tc.name, func(t *testing.T) {
			accessTestCertificate(t, c, tc.host, tc.before, tc.after, tc.usage)
			if _, _, err := prepareManagementAccess(c); err == nil || err.Error() != "management_tls_invalid" {
				t.Fatal("invalid certificate accepted", err)
			}
		})
	}
	roots := accessTestCertificate(t, c, "nas.example.test", now.Add(-time.Hour), now.Add(time.Hour), x509.ExtKeyUsageServerAuth)
	a, conf, err := prepareManagementAccess(c)
	if err != nil || conf.MinVersion != tls.VersionTLS12 {
		t.Fatal("valid TLS rejected", err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !managementBoundary(w, r, "nas.example.test:8787", c.Origin) {
			return
		}
		if _, handled := a.authorize(w, r, c.Origin); !handled {
			w.WriteHeader(204)
		}
	}))
	server.TLS = conf
	server.StartTLS()
	defer server.Close()
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: "nas.example.test", MinVersion: tls.VersionTLS12}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	r, _ := http.NewRequest("GET", server.URL+"/api/session", nil)
	r.Host = "nas.example.test:8787"
	response, err := client.Do(r)
	if err != nil {
		t.Fatal("trusted TLS client failed", err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || bytes.Contains(body, []byte("csrf")) || response.TLS.Version < tls.VersionTLS12 {
		t.Fatal("TLS bootstrap boundary failed")
	}
	// 不受信任 CA 必须失败，不以接受警告代替部署步骤。
	untrustedTransport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: x509.NewCertPool(), ServerName: "nas.example.test"}}
	defer untrustedTransport.CloseIdleConnections()
	badClient := &http.Client{Transport: untrustedTransport, Timeout: 3 * time.Second}
	if res, err := badClient.Do(r); err == nil {
		res.Body.Close()
		t.Fatal("untrusted certificate accepted")
	}
}

func TestManagementAccessCookieAndSessionLookupConcurrency(t *testing.T) {
	_, v := accessTestVerifier(t)
	a := newManagementAccess("admin", v)
	c, session := accessTestLogin(t, a, nil)
	for _, value := range []string{"", "short", strings.Repeat("z", 64), strings.Repeat("A", 64)} {
		r := accessTestRequest("GET", "/api/jobs", "")
		r.AddCookie(&http.Cookie{Name: managementCookie, Value: value})
		if w := accessTestHandle(a, r); w.Code != 401 {
			t.Fatal("invalid cookie accepted")
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				r := accessTestRequest("GET", "/api/session", "")
				r.AddCookie(c)
				accessTestHandle(a, r)
			}
		}()
	}
	r := accessTestRequest("POST", "/api/logout", `{}`)
	r.AddCookie(c)
	r.Header.Set("X-CSRF-Nonce", session.CSRF)
	accessTestHandle(a, r)
	wg.Wait()
	if _, ok := a.session(r); ok {
		t.Fatal("concurrent reads restored logged-out session")
	}
	// 确认 cookie 确实为 32 字节随机量，且没有存储其原文作为服务器索引。
	decoded, err := hex.DecodeString(c.Value)
	if err != nil || len(decoded) != 32 {
		t.Fatal("cookie encoding")
	}
}
