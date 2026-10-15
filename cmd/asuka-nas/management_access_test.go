package main

import (
	"bytes"
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/okitaAsuka/asuka-nas/internal/nas"
)

// 子进程 env 只含 synthetic 配置/凭据；校验和失败启动不能访问上游或留下 listener。
func TestManagementAccessCLIStartupFailClosed(t *testing.T) {
	if os.Getenv("NAS_ACCESS_STARTUP_CHILD") == "1" {
		flag.CommandLine = flag.NewFlagSet("nas", flag.ContinueOnError)
		os.Args = []string{"nas", "-config", os.Getenv("NAS_ACCESS_STARTUP_CONFIG")}
		mode := os.Getenv("NAS_ACCESS_STARTUP_MODE")
		if mode != "validate" {
			os.Args = append(os.Args, "-serve")
		}
		if mode == "conflict" {
			os.Args = append(os.Args, "-listen", "127.0.0.1:8787")
		}
		err := run()
		if mode == "validate" && err == nil {
			os.Exit(0)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, startupMessage(err))
			if err.Error() == os.Getenv("NAS_ACCESS_STARTUP_EXPECT") {
				os.Exit(3)
			}
		}
		os.Exit(4)
	}
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.WriteHeader(503)
	}))
	defer upstream.Close()
	salt := []byte("synthetic-cli-only-salt")
	key, err := pbkdf2.Key(sha256.New, "synthetic-cli-password-0123456789", salt, 600000, 32)
	if err != nil {
		t.Fatal(err)
	}
	verifier := "pbkdf2-sha256$600000$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(key)
	for _, tc := range []struct{ name, mode, verifier, want string }{
		{"validation needs no secrets or files", "validate", "", ""},
		{"explicit listen conflicts", "conflict", "", "management_listen_conflict"},
		{"missing verifier", "serve", "", "management_credentials_unavailable"},
		{"invalid verifier", "serve", "synthetic-private-invalid-verifier", "management_credentials_unavailable"},
		{"missing TLS", "serve", verifier, "management_tls_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			address, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			listen := address.Addr().String()
			address.Close()
			cfg := nas.Config{Database: filepath.Join(root, "state.db"), Downloads: filepath.Join(root, "downloads"), Library: filepath.Join(root, "library"), QBURL: upstream.URL, EmbyURL: upstream.URL, Subscriptions: []nas.Subscription{{ID: "fixture", URL: upstream.URL + "/rss?token=synthetic-private"}}, Management: nas.ManagementAccessConfig{Enabled: true, Listen: listen, Origin: "https://nas.example.test:8787", Username: "admin", PasswordHashEnv: "NAS_ACCESS_CLI_SYNTHETIC_HASH", TLSCertFile: filepath.Join(root, "missing.crt"), TLSKeyFile: filepath.Join(root, "missing.key")}}
			// 校验/冲突分支连媒体目录也不存在，证明未启动文件执行。
			if tc.mode == "serve" {
				for _, p := range []string{cfg.Downloads, cfg.Library} {
					if err := os.Mkdir(p, 0700); err != nil {
						t.Fatal(err)
					}
				}
			}
			b, _ := json.Marshal(cfg)
			path := filepath.Join(root, "synthetic-config.json")
			if err := os.WriteFile(path, b, 0600); err != nil {
				t.Fatal(err)
			}
			child := exec.Command(os.Args[0], "-test.run=^TestManagementAccessCLIStartupFailClosed$")
			child.Env = []string{"NAS_ACCESS_STARTUP_CHILD=1", "NAS_ACCESS_STARTUP_CONFIG=" + path, "NAS_ACCESS_STARTUP_MODE=" + tc.mode, "NAS_ACCESS_STARTUP_EXPECT=" + tc.want, cfg.Management.PasswordHashEnv + "=" + tc.verifier}
			var out bytes.Buffer
			child.Stdout, child.Stderr = &out, &out
			err = child.Run()
			if tc.mode == "validate" {
				if err != nil || !strings.Contains(out.String(), "configuration_valid") {
					t.Fatal("configuration check performed runtime access", err, out.String())
				}
			} else if ex, ok := err.(*exec.ExitError); !ok || ex.ExitCode() != 3 {
				t.Fatal("unexpected startup outcome", err, out.String())
			}
			if upstreamCalls.Load() != 0 {
				t.Fatal("failed startup reached upstream")
			}
			if strings.Contains(out.String(), "synthetic-private") || strings.Contains(out.String(), verifier) || strings.Contains(out.String(), upstream.URL) || strings.Contains(out.String(), root) {
				t.Fatal("startup exposed configuration/credential")
			}
			if tc.mode != "serve" {
				if _, err := os.Stat(cfg.Database); !os.IsNotExist(err) {
					t.Fatal("validation/conflict created database")
				}
			}
			probe, err := net.Listen("tcp", listen)
			if err != nil {
				t.Fatal("failed startup retained listener", err)
			}
			probe.Close()
		})
	}
}
