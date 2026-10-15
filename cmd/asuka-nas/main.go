package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/okitaAsuka/asuka-nas/internal/nas"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, startupMessage(err))
		os.Exit(1)
	}
}

// 只公开显式允许的固定启动码，不输出配置路径/URL、凭据或原始驱动错误。
func startupMessage(err error) string {
	switch err.Error() {
	case "subscription_limit", "subscription_history_limit":
		return "startup_failed: " + err.Error()
	case "management_listen_invalid", "management_origin_invalid", "management_credentials_invalid", "management_identity_reused", "management_tls_required", "management_credentials_unavailable", "management_tls_unavailable", "management_tls_invalid", "management_listen_unavailable", "management_authenticated_required", "management_listen_conflict":
		return "startup_failed: " + err.Error()
	}
	return "startup_failed"
}

// 默认只校验；只有显式 -run 才访问部署配置中的服务。
func run() error {
	if len(os.Args) > 1 && os.Args[1] == "rules" {
		return rulesCLI(os.Args[2:])
	}
	path := flag.String("config", "config.json", "deployment configuration")
	execute := flag.Bool("run", false, "run the local polling daemon (explicit opt-in)")
	serve := flag.Bool("serve", false, "serve local management UI and daemon (explicit opt-in)")
	listen := flag.String("listen", "127.0.0.1:8787", "loopback address only")
	flag.Parse()
	file, err := os.Open(*path)
	if err != nil {
		return err
	}
	defer file.Close()
	cfg, err := nas.LoadConfig(file)
	if err != nil {
		return err
	}
	// 认证模式的地址只来自显式配置，拒绝另一 CLI 地址来源而不是静默忽略。
	if cfg.Management.Enabled && *serve {
		conflict := false
		flag.Visit(func(f *flag.Flag) { conflict = conflict || f.Name == "listen" })
		if conflict {
			return errors.New("management_listen_conflict")
		}
	}
	if !*execute && !*serve {
		fmt.Println("configuration_valid (no network or file operations executed)")
		return nil
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	store, lock, err := nas.OpenLockedStore(cfg.Database)
	if err != nil {
		return err
	}
	defer lock.Close()
	defer store.Close()
	files, err := nas.OpenFiles(cfg.Downloads, cfg.Library)
	if err != nil {
		return err
	}
	defer files.Close()
	qb := &nas.QB{HTTPBoundary: nas.NewHTTPBoundary(cfg.QBURL, ""), Downloads: cfg.Downloads}
	qb.SetTimeout(time.Duration(cfg.RequestTimeoutSeconds) * time.Second)
	// 只消费部署者已有凭据，不生成、写盘或回显。
	qb.SetCredentialSource(func() (string, string, error) {
		user, password := os.Getenv("ASUKA_NAS_QB_USERNAME"), os.Getenv("ASUKA_NAS_QB_PASSWORD")
		if user == "" || password == "" {
			return "", "", errors.New("credentials_unavailable")
		}
		return user, password, nil
	})
	emby := &nas.Emby{HTTPBoundary: nas.NewHTTPBoundary(cfg.EmbyURL, os.Getenv("ASUKA_NAS_EMBY_TOKEN"))}
	emby.SetTimeout(time.Duration(cfg.RequestTimeoutSeconds) * time.Second)
	engine := &nas.Engine{Store: store, Files: files, QB: qb, Emby: emby, Library: cfg.Library}
	daemon := &nas.Daemon{Config: cfg, Engine: engine, Source: nas.NewSourceClient(time.Duration(cfg.RequestTimeoutSeconds)*time.Second, cfg.RSSMaxBytes, cfg.TorrentMaxBytes, cfg.MaxItems), Logger: slog.New(slog.NewJSONHandler(os.Stderr, nil))}
	daemon.Source.Gap = time.Duration(cfg.FetchIntervalMillis) * time.Millisecond
	// 各服务先完成初始化，再启动循环；任一失败取消并等待全部退出。
	workers := []func() error{func() error { return daemon.Run(ctx) }}
	if cfg.Learning.Enabled {
		l, h, err := nas.OpenLearningService(cfg.Learning, cfg.Events, store, engine)
		if err != nil {
			return err
		}
		defer l.Close()
		workers = append(workers, func() error { return nas.ServeManagement(ctx, l, h) })
	}
	if cfg.Events.Enabled {
		l, h, err := nas.OpenEventService(cfg.Events, store)
		if err != nil {
			return err
		}
		defer l.Close()
		workers = append(workers, func() error { return nas.ServeManagement(ctx, l, h) })
	}
	if *serve {
		if cfg.Management.Enabled {
			listener, management, err := nas.OpenAuthenticatedManagement(ctx, daemon)
			if err != nil {
				return err
			}
			defer listener.Close()
			workers = append(workers, func() error { return nas.ServeManagement(ctx, listener, management) })
		} else {
			listener, err := nas.ListenManagement(*listen)
			if err != nil {
				return err
			}
			defer listener.Close()
			management, err := nas.NewManagement(ctx, daemon, listener.Addr().String())
			if err != nil {
				return err
			}
			workers = append(workers, func() error { return nas.ServeManagement(ctx, listener, management) })
		}
	}
	results := make(chan error, len(workers))
	for _, worker := range workers {
		go func() { results <- worker() }()
	}
	err = <-results
	cancel()
	for i := 1; i < len(workers); i++ {
		err = errors.Join(err, <-results)
	}
	return err
}
