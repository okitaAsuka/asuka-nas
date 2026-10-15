package nas

// Temporary UI integration harness only; not a production API or deployment setting.
import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func runAuthenticatedBrowserUI(t *testing.T) {
	t.Helper()
	f := newFixture(t, "Authenticated [02].mkv", 0)
	f.tick(1000)
	c := accessTestConfig(t)
	reserve, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	c.Listen = reserve.Addr().String()
	reserve.Close()
	c.Origin = "https://" + c.Listen
	raw, _ := accessTestVerifier(t)
	t.Setenv(c.PasswordHashEnv, raw)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(101), Subject: pkix.Name{CommonName: "Asuka isolated UI fixture CA"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ca, err = x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(102), Subject: pkix.Name{CommonName: "Asuka UI fixture"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	caFile := filepath.Join(t.TempDir(), "fixture-ca.pem")
	for path, block := range map[string]*pem.Block{caFile: {Type: "CERTIFICATE", Bytes: caDER}, c.TLSCertFile: {Type: "CERTIFICATE", Bytes: leafDER}, c.TLSKeyFile: {Type: "EC PRIVATE KEY", Bytes: private}} {
		if err := os.WriteFile(path, pem.EncodeToMemory(block), 0600); err != nil {
			t.Fatal(err)
		}
	}
	d := &Daemon{Engine: f.engine, Config: Config{Management: c, Subscriptions: []Subscription{{ID: "fixture", URL: f.server.URL + "/rss"}}}}
	listener, m, err := OpenAuthenticatedManagement(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- ServeManagement(ctx, listener, m) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/expire" {
			m.access.mu.Lock()
			for key, session := range m.access.sessions {
				session.expires = time.Now().Add(-time.Second)
				m.access.sessions[key] = session
			}
			m.access.mu.Unlock()
			w.WriteHeader(204)
			return
		}
		if r.Method == "GET" && r.URL.Path == "/state" {
			j := f.job()
			json.NewEncoder(w).Encode(map[string]bool{"approved": j.Plan != nil && j.Plan.Approved})
			return
		}
		w.WriteHeader(404)
	}))
	defer control.Close()
	command := exec.Command("python3", "../../tests/authenticated_browser.py")
	command.Env = append(os.Environ(), "NAS_AUTH_URL="+c.Origin, "NAS_AUTH_CONTROL="+control.URL, "NAS_AUTH_CA="+caFile, "NAS_AUTH_PASSWORD="+accessTestPassword)
	out, err := command.CombinedOutput()
	t.Log(string(out))
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.discovered = true
	f.mu.Unlock()
	f.tick(1001)
	j := f.job()
	if j.Import != "imported" || j.Discovery != "confirmed" || j.Seed != "stopped" {
		t.Fatal("authenticated UI intent did not complete isolated file workflow", j)
	}
	source, err := os.Stat(filepath.Join(f.downloads, f.name))
	if err != nil {
		t.Fatal(err)
	}
	target, err := os.Stat(filepath.Join(f.library, j.Plan.Files[0].Target))
	if err != nil || !os.SameFile(source, target) {
		t.Fatal("source retained hardlink missing", err)
	}
	t.Log("PASS REAL AUTH EXECUTION: browser-approved immutable intent -> actual temporary hardlink -> per-file fake Emby discovery; source retained and seed stop independent")
}
