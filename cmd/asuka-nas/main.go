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

// 仅公开本轮容量拒绝码，不输出配置URL或原始驱动错误。
func startupMessage(err error) string {
	switch err.Error() {
	case "subscription_limit", "subscription_history_limit":
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
	if !*serve {
		return daemon.Run(ctx)
	}
	listener, err := nas.ListenManagement(*listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	management, err := nas.NewManagement(ctx, daemon, listener.Addr().String())
	if err != nil {
		return err
	}
	// HTTP 与轮询共享取消；等待所有写操作结束后才释放数据库。
	results := make(chan error, 2)
	go func() { results <- daemon.Run(ctx) }()
	go func() { results <- nas.ServeManagement(ctx, listener, management) }()
	err = <-results
	cancel()
	other := <-results
	return errors.Join(err, other)
}
