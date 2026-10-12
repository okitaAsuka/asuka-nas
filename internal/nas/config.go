package nas

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"path/filepath"
	"regexp"
)

// Config 的秘密只由部署时提供，不序列化进日志。
type Config struct {
	Events                EventServiceConfig `json:"events,omitempty"`
	FetchIntervalMillis   int                `json:"fetch_interval_ms,omitempty"`
	Subscriptions         []Subscription     `json:"subscriptions,omitempty"`
	PollSeconds           int                `json:"poll_seconds,omitempty"`
	TickSeconds           int                `json:"tick_seconds,omitempty"`
	RequestTimeoutSeconds int                `json:"request_timeout_seconds,omitempty"`
	RSSMaxBytes           int64              `json:"rss_max_bytes,omitempty"`
	TorrentMaxBytes       int64              `json:"torrent_max_bytes,omitempty"`
	MaxItems              int                `json:"max_items,omitempty"`

	Database  string `json:"database"`
	Downloads string `json:"downloads"`
	Library   string `json:"library"`
	SeedDays  int    `json:"seed_days"`
	QBURL     string `json:"qb_url"`
	EmbyURL   string `json:"emby_url"`
}

// LoadConfig 严格解码，不回传含秘密的解析错误。
func LoadConfig(r io.Reader) (Config, error) {
	var c Config
	b, err := io.ReadAll(io.LimitReader(r, 65537))
	if err != nil || len(b) > 65536 {
		return c, errors.New("invalid_config")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil {
		return c, errors.New("invalid_config")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return c, errors.New("invalid_config")
	}
	c.defaults()
	return c, c.Validate()
}

// Validate 不执行网络探测，避免读取配置触发真实服务调用。
func (c Config) Validate() error {
	c.defaults()
	if err := c.Events.Validate(); err != nil {
		return err
	}
	if c.FetchIntervalMillis < 100 || c.FetchIntervalMillis > 60000 {
		return errors.New("invalid_fetch_interval")
	}
	if c.PollSeconds < 60 || c.PollSeconds > 86400 || c.TickSeconds < 1 || c.TickSeconds > 300 || c.RequestTimeoutSeconds < 1 || c.RequestTimeoutSeconds > 60 || c.RSSMaxBytes < 1024 || c.RSSMaxBytes > 8*1024*1024 || c.TorrentMaxBytes < 1024 || c.TorrentMaxBytes > 8*1024*1024 || c.MaxItems < 1 || c.MaxItems > 1000 || len(c.Subscriptions) > 100 {
		return errors.New("invalid_poll_limits")
	}
	seen := map[string]bool{}
	for _, sub := range c.Subscriptions {
		if !subscriptionID.MatchString(sub.ID) || seen[sub.ID] {
			return errors.New("invalid_subscription_id")
		}
		seen[sub.ID] = true
		if _, err := sourceURL(sub.URL); err != nil {
			return errors.New("invalid_subscription_url")
		}
		for _, raw := range sub.EnclosureOrigins {
			u, err := sourceURL(raw)
			if err != nil || raw != origin(u) {
				return errors.New("invalid_enclosure_origin")
			}
		}
	}

	if c.SeedDays < 0 || c.SeedDays > 7 {
		return errors.New("invalid_seed_days")
	}
	for _, p := range []string{c.Database, c.Downloads, c.Library} {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p {
			return errors.New("invalid_path")
		}
	}
	if c.Downloads == c.Library {
		return errors.New("overlapping_roots")
	}
	for _, pair := range [][2]string{{c.Downloads, c.Library}, {c.Library, c.Downloads}} {
		r, _ := filepath.Rel(pair[0], pair[1])
		if filepath.IsLocal(r) {
			return errors.New("overlapping_roots")
		}
	}
	for _, s := range []string{c.QBURL, c.EmbyURL} {
		u, e := url.Parse(s)
		if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return errors.New("invalid_service_url")
		}
	}
	return nil
}

// LogValue 禁止 slog 意外展开部署路径或 URL。
func (c Config) LogValue() slog.Value { return slog.StringValue("[config omitted]") }

var subscriptionID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func (c *Config) defaults() {
	if c.FetchIntervalMillis == 0 {
		c.FetchIntervalMillis = 250
	}
	if c.PollSeconds == 0 {
		c.PollSeconds = 600
	}
	if c.TickSeconds == 0 {
		c.TickSeconds = 15
	}
	if c.RequestTimeoutSeconds == 0 {
		c.RequestTimeoutSeconds = 10
	}
	if c.RSSMaxBytes == 0 {
		c.RSSMaxBytes = 2 * 1024 * 1024
	}
	if c.TorrentMaxBytes == 0 {
		c.TorrentMaxBytes = 8 * 1024 * 1024
	}
	if c.MaxItems == 0 {
		c.MaxItems = 200
	}
}
