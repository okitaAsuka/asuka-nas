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
		fmt.Fprintln(os.Stderr, "startup_failed")
		os.Exit(1)
	}
}

// 默认只校验；只有显式 -run 才访问部署配置中的服务。
func run() error {
	if len(os.Args) > 1 && os.Args[1] == "rules" {
		return rulesCLI(os.Args[2:])
	}
	path := flag.String("config", "config.json", "deployment configuration")
	execute := flag.Bool("run", false, "run the local polling daemon (explicit opt-in)")
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
	if !*execute {
		fmt.Println("configuration_valid (no network or file operations executed)")
		return nil
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	store, err := nas.OpenStore(cfg.Database)
	if err != nil {
		return err
	}
	defer store.Close()
	lock, err := nas.AcquireInstance(cfg.Database)
	if err != nil {
		return err
	}
	defer lock.Close()
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
	return daemon.Run(ctx)
}
