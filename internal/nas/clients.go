package nas

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// HTTPBoundary 限制响应、超时和重定向；原始 URL 与远端响应不进入错误。
type HTTPBoundary struct {
	client *http.Client
	base   string
	key    string
}

// NewHTTPBoundary 只保存客户端配置；此处不连接服务。
func NewHTTPBoundary(base, key string) *HTTPBoundary {
	jar, _ := cookiejar.New(nil)
	return &HTTPBoundary{client: &http.Client{Timeout: 10 * time.Second, Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect_blocked") }}, base: strings.TrimRight(base, "/"), key: key}
}

func (h *HTTPBoundary) call(ctx context.Context, method, endpoint string, form url.Values) ([]byte, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, e := http.NewRequestWithContext(ctx, method, h.base+endpoint, body)
	if e != nil {
		return nil, errors.New("upstream_request_failed")
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Referer", h.base+"/")
	}
	if h.key != "" {
		req.Header.Set("X-Emby-Token", h.key)
	}
	resp, e := h.client.Do(req)
	if e != nil {
		return nil, errors.New("upstream_unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New("upstream_rejected")
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024+1))
	if e != nil || len(b) > 2*1024*1024 {
		return nil, errors.New("upstream_response_invalid")
	}
	return b, nil
}

// TorrentStatus 的完成时间来自下载器，不能用本服务启动时间替代。
type TorrentStatus struct {
	Complete    bool
	CompletedAt int64
	Files       []string
}

// Downloader 是 qBittorrent 边界，不提供删除接口。
type Downloader interface {
	Add(context.Context, string) error
	Status(context.Context, string) (TorrentStatus, error)
	Files(context.Context, string) ([]string, error)
	Stop(context.Context, string) error
}

// QB 对应 qBittorrent 5.x API；4.x pause 兼容尚未实现。
type QB struct {
	*HTTPBoundary
	Downloads string
}

// Login 使用用户部署时提供的账号，不保存/输出密码。
func (q *QB) Login(ctx context.Context, user, password string) error {
	b, e := q.call(ctx, "POST", "/api/v2/auth/login", url.Values{"username": {user}, "password": {password}})
	if e != nil {
		return e
	}
	if string(b) != "Ok." {
		return errors.New("qb_login_failed")
	}
	return nil
}

// Add 的 torrent infohash 自带去重；失败后可按相同 hash 重试。
func (q *QB) Add(ctx context.Context, magnet string) error {
	hash := strings.TrimPrefix(magnet, "magnet:?xt=urn:btih:")
	if !strings.HasPrefix(magnet, "magnet:?xt=urn:btih:") || !btih.MatchString(hash) {
		return errors.New("invalid_magnet")
	}
	if !filepath.IsAbs(q.Downloads) || filepath.Clean(q.Downloads) != q.Downloads {
		return errors.New("invalid_download_root")
	}

	b, e := q.call(ctx, "POST", "/api/v2/torrents/add", url.Values{"urls": {magnet}, "savepath": {q.Downloads}})
	if e != nil {
		return e
	}
	if strings.TrimSpace(string(b)) != "Ok." {
		return errors.New("qb_add_failed")
	}
	return nil
}

// Status 只在确实完成时读取下载器文件清单。
func (q *QB) Status(ctx context.Context, hash string) (TorrentStatus, error) {
	var s TorrentStatus
	if !btih.MatchString(hash) {
		return s, errors.New("invalid_infohash")
	}
	b, e := q.call(ctx, "GET", "/api/v2/torrents/info?"+url.Values{"hashes": {hash}}.Encode(), nil)
	if e != nil {
		return s, e
	}
	var items []struct {
		Hash       string  `json:"hash"`
		Progress   float64 `json:"progress"`
		Completion int64   `json:"completion_on"`
		SavePath   string  `json:"save_path"`
	}
	if json.Unmarshal(b, &items) != nil {
		return s, errors.New("qb_response_invalid")
	}
	if len(items) == 0 {
		return s, nil
	}
	if len(items) != 1 || items[0].Hash != hash {
		return s, errors.New("qb_response_invalid")
	}
	s.Complete = items[0].Progress == 1 && items[0].Completion > 0
	s.CompletedAt = items[0].Completion
	if !s.Complete {
		return s, nil
	}
	return s, nil
}

// Files 独立于完成状态查询，文件 API 故障不能阻塞停止做种。
func (q *QB) Files(ctx context.Context, hash string) ([]string, error) {
	if !btih.MatchString(hash) {
		return nil, errors.New("invalid_infohash")
	}
	// 路径映射属于导入校验，不能阻塞完成时间和停止做种。
	info, err := q.call(ctx, "GET", "/api/v2/torrents/info?"+url.Values{"hashes": {hash}}.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var paths []struct {
		Hash     string `json:"hash"`
		SavePath string `json:"save_path"`
	}
	if json.Unmarshal(info, &paths) != nil || len(paths) != 1 || paths[0].Hash != hash {
		return nil, errors.New("qb_response_invalid")
	}
	if paths[0].SavePath != q.Downloads {
		return nil, errors.New("qb_download_root_mismatch")
	}

	b, e := q.call(ctx, "GET", "/api/v2/torrents/files?"+url.Values{"hash": {hash}}.Encode(), nil)
	if e != nil {
		return nil, e
	}
	var files []struct {
		Name     string  `json:"name"`
		Progress float64 `json:"progress"`
	}
	if json.Unmarshal(b, &files) != nil {
		return nil, errors.New("qb_response_invalid")
	}
	var result []string
	for _, f := range files {
		if f.Progress != 1 {
			return nil, errors.New("incomplete_files")
		}
		result = append(result, f.Name)
	}
	return result, nil
}

// Stop 只停止做种，绝不删除种子或源文件。
func (q *QB) Stop(ctx context.Context, hash string) error {
	if !btih.MatchString(hash) {
		return errors.New("invalid_infohash")
	}
	_, e := q.call(ctx, "POST", "/api/v2/torrents/stop", url.Values{"hashes": {hash}})
	return e
}

// Media 将刷新请求和媒体发现拆成两个事实。
type Media interface {
	Refresh(context.Context) error
	Discover(context.Context, string) (bool, error)
}

// Emby 使用部署时 token header，禁止把 token 放入 URL。
type Emby struct{ *HTTPBoundary }

// Refresh 仅表示已接受扫描请求。
func (e *Emby) Refresh(ctx context.Context) error {
	_, err := e.call(ctx, "POST", "/Library/Refresh", nil)
	return err
}

// Discover 用精确媒体路径核对，不把任意返回项当作命中。
func (e *Emby) Discover(ctx context.Context, p string) (bool, error) {
	b, err := e.call(ctx, "GET", "/Items?"+url.Values{"Recursive": {"true"}, "Fields": {"Path"}, "Path": {p}}.Encode(), nil)
	if err != nil {
		return false, err
	}
	var result struct {
		Items []struct {
			Path string `json:"Path"`
		}
	}
	if json.Unmarshal(b, &result) != nil {
		return false, errors.New("emby_response_invalid")
	}
	for _, item := range result.Items {
		if item.Path == p {
			return true, nil
		}
	}
	return false, nil
}

var btih = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

// Resource 忽略 RSS 标题；来源和 GUID 只参与本地去重。
type Resource struct{ GUID, Hash, Magnet string }

// ParseRSS 首版支持含 40 位 BTIH magnet；不访问 enclosure 地址。
func ParseRSS(r io.Reader) ([]Resource, error) {
	b, e := io.ReadAll(io.LimitReader(r, 2*1024*1024+1))
	if e != nil || len(b) > 2*1024*1024 {
		return nil, errors.New("rss_invalid")
	}
	var feed struct {
		Channel struct {
			Items []struct {
				GUID      string `xml:"guid"`
				Link      string `xml:"link"`
				Enclosure struct {
					URL string `xml:"url,attr"`
				} `xml:"enclosure"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if xml.Unmarshal(b, &feed) != nil {
		return nil, errors.New("rss_invalid")
	}
	var result []Resource
	for _, i := range feed.Channel.Items {
		raw := i.Enclosure.URL
		if raw == "" {
			raw = i.Link
		}
		u, e := url.Parse(raw)
		if e != nil || u.Scheme != "magnet" || i.GUID == "" {
			return nil, errors.New("rss_unsupported_resource")
		}
		xt := u.Query().Get("xt")
		hash := strings.TrimPrefix(xt, "urn:btih:")
		if !strings.HasPrefix(xt, "urn:btih:") || !btih.MatchString(hash) {
			return nil, errors.New("rss_invalid_infohash")
		}
		// 丢弃显示标题和任意 tracker 参数，避免 RSS 向下载器注入未知 URL。
		hash = strings.ToLower(hash)
		result = append(result, Resource{i.GUID, hash, "magnet:?xt=urn:btih:" + hash})
	}
	return result, nil
}
