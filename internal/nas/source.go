package nas

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Subscription 的 URL 可含用户 token，只保存于受保护配置，不写入事件或日志。
type Subscription struct {
	ID               string   `json:"id"`
	URL              string   `json:"url"`
	EnclosureOrigins []string `json:"enclosure_origins,omitempty"`
}

// SourceClient 使用有界无重定向 HTTP；RSS 和 torrent 不复用 downloader cookie。
type SourceClient struct {
	Gap                    time.Duration
	mu                     sync.Mutex
	next                   time.Time
	client                 *http.Client
	RSSLimit, TorrentLimit int64
	MaxItems               int
}

// NewSourceClient 配置来自校验过的部署配置。
func NewSourceClient(timeout time.Duration, rssLimit, torrentLimit int64, maxItems int) *SourceClient {
	return &SourceClient{Gap: 250 * time.Millisecond, client: &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect_blocked") }}, RSSLimit: rssLimit, TorrentLimit: torrentLimit, MaxItems: maxItems}
}

func sourceURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" {
		return nil, errors.New("source_url_invalid")
	}
	return u, nil
}
func origin(u *url.URL) string { return strings.ToLower(u.Scheme + "://" + u.Host) }

func (s *SourceClient) fetch(ctx context.Context, raw string, limit int64) ([]byte, error) {
	s.mu.Lock()
	at := time.Now()
	if s.next.After(at) {
		at = s.next
	}
	s.next = at.Add(s.Gap)
	s.mu.Unlock()
	if wait := time.Until(at); wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, errors.New("source_cancelled")
		case <-timer.C:
		}
	}

	if _, err := sourceURL(raw); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if err != nil {
		return nil, errors.New("source_request_failed")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, errors.New("source_unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("source_rejected")
	}
	if resp.ContentLength > limit {
		return nil, errors.New("source_too_large")
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, errors.New("source_read_failed")
	}
	if int64(len(b)) > limit {
		return nil, errors.New("source_too_large")
	}
	return b, nil
}

// feedEntry 保留有界不可信标题；GUID 缺失时以 enclosure 字符串的哈希作稳定标识。
type feedEntry struct {
	Title     string `xml:"title"`
	GUID      string `xml:"guid"`
	Link      string `xml:"link"`
	Enclosure struct {
		URL string `xml:"url,attr"`
	} `xml:"enclosure"`
}

func parseFeed(b []byte, max int) ([]feedEntry, error) {
	var feed struct {
		XMLName xml.Name `xml:"rss"`
		Channel struct {
			Items []feedEntry `xml:"item"`
		} `xml:"channel"`
	}
	if xml.Unmarshal(b, &feed) != nil || len(feed.Channel.Items) > max {
		return nil, errors.New("rss_invalid")
	}
	return feed.Channel.Items, nil
}

// Poll 逐项解析。已持久化项跳过 torrent 下载；单项错误不饿死其它条目。
func (s *SourceClient) Poll(ctx context.Context, sub Subscription, store *Store, days int) error {
	base, err := sourceURL(sub.URL)
	if err != nil {
		return err
	}
	b, err := s.fetch(ctx, sub.URL, s.RSSLimit)
	if err != nil {
		return err
	}
	entries, err := parseFeed(b, s.MaxItems)
	if err != nil {
		return err
	}
	var failures []error
	for _, entry := range entries {
		if ctx.Err() != nil {
			return errors.New("source_cancelled")
		}
		raw := entry.Enclosure.URL
		if raw == "" {
			raw = entry.Link
		}
		guid := entry.GUID
		if guid == "" {
			guid = digestString(raw)
		} else {
			guid = digestString(guid)
		}
		seen, err := store.ResourceSeen(ctx, sub.ID, guid)
		if err != nil {
			return errors.New("source_store_failed")
		}
		if seen {
			if err = store.AttachResourceTitle(ctx, sub.ID, guid, entry.Title); err != nil {
				failures = append(failures, errors.New("source_store_failed"))
			}
			continue
		}
		if err = s.ingestEntry(ctx, base, sub, store, days, guid, raw, entry.Title); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (s *SourceClient) ingestEntry(ctx context.Context, base *url.URL, sub Subscription, store *Store, days int, guid, raw, title string) error {
	if len(title) > TitleLimit {
		return errors.New("title_too_large")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("rss_resource_invalid")
	}
	if u.Scheme == "magnet" {
		// 复用固定的 magnet 校验，标题和 tracker 参数不会传入下载器。
		xt := u.Query().Get("xt")
		hash := strings.TrimPrefix(xt, "urn:btih:")
		if !strings.HasPrefix(xt, "urn:btih:") || !btih.MatchString(hash) {
			return errors.New("rss_invalid_infohash")
		}
		hash = strings.ToLower(hash)
		_, err = store.IngestRelease(ctx, sub.ID, guid, hash, "magnet:?xt=urn:btih:"+hash, nil, days, title)
		if err != nil {
			return errors.New("source_store_failed")
		}
		return nil
	}
	u = base.ResolveReference(u)
	if _, err = sourceURL(u.String()); err != nil {
		return err
	}
	allowed := origin(u) == origin(base)
	for _, o := range sub.EnclosureOrigins {
		if origin(u) == strings.ToLower(o) {
			allowed = true
		}
	}
	if !allowed {
		return errors.New("enclosure_origin_denied")
	}
	b, err := s.fetch(ctx, u.String(), s.TorrentLimit)
	if err != nil {
		return err
	}
	hash, err := TorrentHash(b)
	if err != nil {
		return err
	}
	_, err = store.IngestRelease(ctx, sub.ID, guid, hash, "", b, days, title)
	if err != nil {
		return errors.New("source_store_failed")
	}
	return nil
}
