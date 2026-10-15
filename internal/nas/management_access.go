package nas

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	managementCookie       = "__Host-asuka_session"
	managementSessionTTL   = 12 * time.Hour
	managementSessionLimit = 16
	managementIPLimit      = 256
)

var managementUsername = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
var managementEnv = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
var managementDNSLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

// ManagementAccessConfig 仅显式启用时提供 HTTPS 人工身份；不含密码或证书正文。
type ManagementAccessConfig struct {
	Enabled         bool   `json:"enabled"`
	Listen          string `json:"listen,omitempty"`
	Origin          string `json:"origin,omitempty"`
	Username        string `json:"username,omitempty"`
	PasswordHashEnv string `json:"password_hash_env,omitempty"`
	TLSCertFile     string `json:"tls_cert_file,omitempty"`
	TLSKeyFile      string `json:"tls_key_file,omitempty"`
}

// Validate 不读秘密、不 DNS 解析、不监听；通配监听仅在独立认证模式下允许。
func (c ManagementAccessConfig) Validate(serviceEnvs ...string) error {
	if !c.Enabled {
		return nil
	}
	host, port, err := net.SplitHostPort(c.Listen)
	ip := net.ParseIP(host)
	n, e := strconv.Atoi(port)
	if err != nil || e != nil || n < 1 || n > 65535 || strconv.Itoa(n) != port || ip == nil || (!ip.IsLoopback() && !ip.IsPrivate() && !ip.IsUnspecified()) {
		return errors.New("management_listen_invalid")
	}
	u, err := url.Parse(c.Origin)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || c.Origin != "https://"+u.Host {
		return errors.New("management_origin_invalid")
	}
	host = u.Hostname()
	if host == "" || strings.ToLower(host) != host || strings.HasSuffix(host, ".") {
		return errors.New("management_origin_invalid")
	}
	if ip := net.ParseIP(host); ip != nil {
		if !ip.IsLoopback() && !ip.IsPrivate() {
			return errors.New("management_origin_invalid")
		}
	} else {
		if len(host) > 253 {
			return errors.New("management_origin_invalid")
		}
		for _, label := range strings.Split(host, ".") {
			if !managementDNSLabel.MatchString(label) {
				return errors.New("management_origin_invalid")
			}
		}
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != p {
			return errors.New("management_origin_invalid")
		}
	}
	// URL.Parse 允许空端口等非规范 host；逐字重建后才信任 Host/Origin。
	expectedHost := host
	if strings.Contains(host, ":") {
		expectedHost = "[" + host + "]"
	}
	if u.Port() != "" {
		expectedHost = net.JoinHostPort(host, u.Port())
	}
	if u.Host != expectedHost {
		return errors.New("management_origin_invalid")
	}
	if !managementUsername.MatchString(c.Username) || !managementEnv.MatchString(c.PasswordHashEnv) {
		return errors.New("management_credentials_invalid")
	}
	for _, name := range serviceEnvs {
		if name != "" && name == c.PasswordHashEnv {
			return errors.New("management_identity_reused")
		}
	}
	for _, p := range []string{c.TLSCertFile, c.TLSKeyFile} {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p {
			return errors.New("management_tls_required")
		}
	}
	return nil
}

type managementVerifier struct {
	salt, key  []byte
	iterations int
}

func parseManagementVerifier(raw string) (managementVerifier, error) {
	v := managementVerifier{}
	parts := strings.Split(raw, "$")
	if len(raw) > 256 || len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return v, errors.New("management_credentials_unavailable")
	}
	n, err := strconv.Atoi(parts[1])
	if err != nil || n < 600000 || n > 1000000 || strconv.Itoa(n) != parts[1] {
		return v, errors.New("management_credentials_unavailable")
	}
	salt, e1 := base64.RawStdEncoding.Strict().DecodeString(parts[2])
	key, e2 := base64.RawStdEncoding.Strict().DecodeString(parts[3])
	if e1 != nil || e2 != nil || len(salt) < 16 || len(salt) > 64 || len(key) != 32 || base64.RawStdEncoding.EncodeToString(salt) != parts[2] || base64.RawStdEncoding.EncodeToString(key) != parts[3] {
		return v, errors.New("management_credentials_unavailable")
	}
	return managementVerifier{salt: salt, key: key, iterations: n}, nil
}

func (v managementVerifier) matches(password string) bool {
	key, err := pbkdf2.Key(sha256.New, password, v.salt, v.iterations, 32)
	return err == nil && subtle.ConstantTimeCompare(v.key, key) == 1
}

type managementSession struct {
	csrf    string
	expires time.Time
}
type managementLoginBucket struct {
	count int
	until time.Time
}
type managementAccess struct {
	mu       sync.Mutex
	username [32]byte
	verifier managementVerifier
	sessions map[[32]byte]managementSession
	global   managementLoginBucket
	ips      map[string]managementLoginBucket
	verify   chan struct{}
	now      func() time.Time
	random   io.Reader
}

func newManagementAccess(username string, verifier managementVerifier) *managementAccess {
	return &managementAccess{username: sha256.Sum256([]byte(username)), verifier: verifier, sessions: make(map[[32]byte]managementSession), ips: make(map[string]managementLoginBucket), verify: make(chan struct{}, 1), now: time.Now, random: rand.Reader}
}

// 凭据和证书全部验证成功才可创建 listener；只消费部署者已有秘密，不签发或写盘。
func prepareManagementAccess(c ManagementAccessConfig, serviceEnvs ...string) (*managementAccess, *tls.Config, error) {
	if !c.Enabled {
		return nil, nil, errors.New("management_disabled")
	}
	if err := c.Validate(serviceEnvs...); err != nil {
		return nil, nil, err
	}
	v, err := parseManagementVerifier(os.Getenv(c.PasswordHashEnv))
	if err != nil {
		return nil, nil, err
	}
	for _, name := range serviceEnvs {
		if token := os.Getenv(name); name != "" && token != "" && len(token) <= 4096 && v.matches(token) {
			return nil, nil, errors.New("management_identity_reused")
		}
	}
	cert, err := tls.LoadX509KeyPair(c.TLSCertFile, c.TLSKeyFile)
	if err != nil || len(cert.Certificate) == 0 {
		return nil, nil, errors.New("management_tls_unavailable")
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	u, _ := url.Parse(c.Origin)
	now := time.Now()
	if err != nil || now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) || leaf.VerifyHostname(u.Hostname()) != nil {
		return nil, nil, errors.New("management_tls_invalid")
	}
	if len(leaf.ExtKeyUsage) > 0 || len(leaf.UnknownExtKeyUsage) > 0 {
		server := false
		for _, use := range leaf.ExtKeyUsage {
			server = server || use == x509.ExtKeyUsageServerAuth || use == x509.ExtKeyUsageAny
		}
		if !server {
			return nil, nil, errors.New("management_tls_invalid")
		}
	}
	cert.Leaf = leaf
	return newManagementAccess(c.Username, v), &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}, NextProtos: []string{"http/1.1"}}, nil
}

func accessJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}
func accessError(w http.ResponseWriter, status int, code string) {
	accessJSON(w, status, map[string]string{"error": code})
}

// 两种模式共用精确 origin 边界；绝不读取代理头或从请求猜测可信地址。
func managementBoundary(w http.ResponseWriter, r *http.Request, host, origin string) bool {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	if r.Host != host || len(r.Header.Values("Origin")) > 1 || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != origin) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		accessError(w, 403, "origin_rejected")
		return false
	}
	if r.Method != "GET" && r.Method != "POST" {
		accessError(w, 405, "method_rejected")
		return false
	}
	return true
}

// authorize 在任何领域读取/写入之前执行；仅三份公开静态资源和 session bootstrap 可匿名。
func (a *managementAccess) authorize(w http.ResponseWriter, r *http.Request, origin string) (csrf string, handled bool) {
	if r.TLS == nil {
		accessError(w, 403, "https_required")
		return "", true
	}
	if len(r.Header.Values("Authorization")) != 0 {
		accessError(w, 403, "csrf_rejected")
		return "", true
	}
	path := r.URL.Path
	if path == "/api/session" || path == "/api/login" || path == "/api/logout" {
		if r.URL.RawQuery != "" || r.URL.ForceQuery {
			accessError(w, 400, "invalid_request")
			return "", true
		}
		if (path == "/api/session") != (r.Method == "GET") {
			accessError(w, 405, "method_rejected")
			return "", true
		}
	}
	if r.Method == "POST" && (r.Header.Get("Origin") != origin || len(r.Header.Values("Content-Type")) != 1 || r.Header.Get("Content-Type") != "application/json") {
		accessError(w, 403, "csrf_rejected")
		return "", true
	}
	if path == "/api/session" {
		accessJSON(w, 200, a.sessionDTO(r))
		return "", true
	}
	if path == "/api/login" {
		a.login(w, r)
		return "", true
	}
	if r.Method == "GET" && !strings.HasPrefix(path, "/api/") {
		if path == "/" || path == "/app.js" || path == "/style.css" {
			return "", false
		}
		accessError(w, 404, "not_found")
		return "", true
	}
	s, ok := a.session(r)
	if !ok {
		accessError(w, 401, "authentication_required")
		return "", true
	}
	if r.Method == "POST" && (len(r.Header.Values("X-CSRF-Nonce")) != 1 || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Nonce")), []byte(s.csrf)) != 1) {
		accessError(w, 403, "csrf_rejected")
		return "", true
	}
	if path == "/api/logout" {
		a.logout(w, r)
		return "", true
	}
	return s.csrf, false
}

func accessCookie(r *http.Request) (string, bool) {
	var value string
	count := 0
	for _, cookie := range r.Cookies() {
		if cookie.Name == managementCookie {
			count++
			value = cookie.Value
		}
	}
	if count != 1 || len(value) != 64 {
		return "", false
	}
	b, err := hex.DecodeString(value)
	return value, err == nil && hex.EncodeToString(b) == value
}

func (a *managementAccess) session(r *http.Request) (managementSession, bool) {
	value, ok := accessCookie(r)
	if !ok {
		return managementSession{}, false
	}
	key := sha256.Sum256([]byte(value))
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.sessions[key]
	if ok && !a.now().Before(s.expires) {
		delete(a.sessions, key)
		ok = false
	}
	return s, ok
}

type managementSessionDTO struct {
	Mode          string `json:"mode"`
	Authenticated bool   `json:"authenticated"`
	CSRF          string `json:"csrf,omitempty"`
	ExpiresAt     int64  `json:"expires_at,omitempty"`
}

func (a *managementAccess) sessionDTO(r *http.Request) managementSessionDTO {
	v := managementSessionDTO{Mode: "authenticated_https"}
	if s, ok := a.session(r); ok {
		v.Authenticated, v.CSRF, v.ExpiresAt = true, s.csrf, s.expires.Unix()
	}
	return v
}

func retryAccess(w http.ResponseWriter, code string, delay time.Duration) {
	seconds := max(1, int((delay+time.Second-1)/time.Second))
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	accessError(w, http.StatusTooManyRequests, code)
}

// 全局/IP/在途三层有界；预约在验证前，伪造 forwarded headers 不能扩容或绕过。
func (a *managementAccess) claimLogin(remote string) time.Duration {
	remote, _, err := net.SplitHostPort(remote)
	ip := net.ParseIP(remote)
	if err != nil || ip == nil {
		return time.Minute
	}
	remote = ip.String()
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	for key, b := range a.ips {
		if !now.Before(b.until) {
			delete(a.ips, key)
		}
	}
	if !now.Before(a.global.until) {
		a.global = managementLoginBucket{until: now.Add(time.Minute)}
	}
	b, found := a.ips[remote]
	if !found && len(a.ips) >= managementIPLimit {
		return time.Minute
	}
	if !found {
		b.until = now.Add(time.Minute)
	}
	if a.global.count >= 20 || b.count >= 5 {
		until := b.until
		if a.global.count >= 20 && a.global.until.After(until) {
			until = a.global.until
		}
		return until.Sub(now)
	}
	a.global.count++
	b.count++
	a.ips[remote] = b
	return 0
}

func decodeAccessRequest(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		return errors.New("invalid_request")
	}
	return nil
}

func (a *managementAccess) login(w http.ResponseWriter, r *http.Request) {
	if delay := a.claimLogin(r.RemoteAddr); delay > 0 {
		retryAccess(w, "login_throttled", delay)
		return
	}
	select {
	case a.verify <- struct{}{}:
		defer func() { <-a.verify }()
	default:
		retryAccess(w, "login_throttled", time.Second)
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if decodeAccessRequest(w, r, &in) != nil || !managementUsername.MatchString(in.Username) || len(in.Password) < 12 || len(in.Password) > 1024 || !utf8.ValidString(in.Password) {
		accessError(w, 400, "invalid_request")
		return
	}
	user := sha256.Sum256([]byte(in.Username))
	// 即使用户名不匹配也执行同一 KDF；比较固定长度摘要，不返回具体失败身份。
	valid := a.verifier.matches(in.Password)
	if subtle.ConstantTimeCompare(user[:], a.username[:]) != 1 || !valid {
		accessError(w, 401, "authentication_failed")
		return
	}
	if r.Context().Err() != nil {
		return
	}
	var random [64]byte
	if _, err := io.ReadFull(a.random, random[:]); err != nil {
		accessError(w, 503, "authentication_unavailable")
		return
	}
	value := hex.EncodeToString(random[:32])
	key := sha256.Sum256([]byte(value))
	a.mu.Lock()
	now := a.now()
	for k, s := range a.sessions {
		if !now.Before(s.expires) {
			delete(a.sessions, k)
		}
	}
	old, hasOld := accessCookie(r)
	oldKey := sha256.Sum256([]byte(old))
	_, replaces := a.sessions[oldKey]
	if len(a.sessions) >= managementSessionLimit && !(hasOld && replaces) {
		earliest := now.Add(managementSessionTTL)
		for _, s := range a.sessions {
			if s.expires.Before(earliest) {
				earliest = s.expires
			}
		}
		a.mu.Unlock()
		retryAccess(w, "session_limit", earliest.Sub(now))
		return
	}
	if _, exists := a.sessions[key]; exists {
		a.mu.Unlock()
		accessError(w, 503, "authentication_unavailable")
		return
	}
	s := managementSession{csrf: hex.EncodeToString(random[32:]), expires: now.Add(managementSessionTTL)}
	if hasOld {
		delete(a.sessions, oldKey)
	}
	a.sessions[key] = s
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: managementCookie, Value: value, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: int(managementSessionTTL / time.Second), Expires: s.expires})
	accessJSON(w, 200, managementSessionDTO{Mode: "authenticated_https", Authenticated: true, CSRF: s.csrf, ExpiresAt: s.expires.Unix()})
}

func (a *managementAccess) logout(w http.ResponseWriter, r *http.Request) {
	var in struct{}
	if decodeAccessRequest(w, r, &in) != nil {
		accessError(w, 400, "invalid_request")
		return
	}
	value, _ := accessCookie(r) // 上层已经检查当前会话和独立 CSRF。
	a.mu.Lock()
	delete(a.sessions, sha256.Sum256([]byte(value)))
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: managementCookie, Value: "", Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1, Expires: time.Unix(1, 0)})
	accessJSON(w, 200, map[string]bool{"ok": true})
}
