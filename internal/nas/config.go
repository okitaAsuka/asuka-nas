package nas

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"path/filepath"
)

// Config 的秘密只由部署时提供，不序列化进日志。
type Config struct {
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
	return c, c.Validate()
}

// Validate 不执行网络探测，避免读取配置触发真实服务调用。
func (c Config) Validate() error {
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
