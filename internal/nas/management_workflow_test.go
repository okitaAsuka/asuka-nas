package nas

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// 测试数据仅存在临时SQLite；不让工作台读取接口承担调度或写入。
func addWorkflowJobs(t *testing.T, s *Store, count int) {
	t.Helper()
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < count; i++ {
		j := Job{ID: fmt.Sprintf("wf-%05d", i), Hash: fmt.Sprintf("%040x", i+500), Version: 1, Download: "completed", Import: "pending", Discovery: "pending", Refresh: "pending", Seed: "seeding", Sources: []string{fmt.Sprintf("Fixture Show.S02E%03d.mkv", i+1)}}
		switch i % 5 {
		case 0:
			j.Download = "requested"
		case 1:
			j.Import = "needs_confirmation"
			j.BlockReason = "episode_unresolved"
		case 2:
			j.Import = "imported"
		case 3:
			j.Import = "planned"
			j.ErrorCode = "permission_denied"
		case 4:
			j.Import = "imported"
			j.Discovery = "confirmed"
		}
		if i == 0 {
			j.Sources = nil
		} // 真正未知的空来源
		if i == 1 {
			j.Sources = []string{"<img src=x onerror=window.PWNED=1> Fixture S02E02.mkv"}
		}
		b, _ := json.Marshal(j)
		if _, err = tx.Exec("INSERT INTO jobs VALUES(?,?,?,?)", j.ID, j.Hash, j.Version, string(b)); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
func workflowGet(t *testing.T, server *httptest.Server, path string, out any) string {
	t.Helper()
	res, err := server.Client().Get(server.URL + "/api/" + path)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil || res.StatusCode != 200 {
		t.Fatal(res.StatusCode, err, string(b))
	}
	if out != nil {
		if err = json.Unmarshal(b, out); err != nil {
			t.Fatal(err)
		}
	}
	return string(b)
}

type workflowPage struct {
	Items   []workflowItem `json:"items"`
	Next    string         `json:"next"`
	Matched int            `json:"matched"`
	Scope   struct {
		Total, Scanned, Limit int
		Truncated             bool
	} `json:"scope"`
}

func TestManagementWorkflowScopeSearchAndPrivacy(t *testing.T) {
	f := newFixture(t, "Example [02].mkv", 7)
	f.tick(1000)
	originalID := f.job().ID
	_, server := managementFixture(t, f)
	addWorkflowJobs(t, f.store, 105)
	var events int
	f.store.db.QueryRow("SELECT count(*) FROM events").Scan(&events)
	var first, second, filtered workflowPage
	raw := workflowGet(t, server, "jobs", &first)
	foundOriginal := false
	for _, item := range first.Items {
		if item.Job.ID == originalID {
			foundOriginal = true
			if item.Job.CompletedAt != 1000 || item.Job.SeedDays != 7 {
				t.Fatal("existing public timing fields lost", item.Job)
			}
		}
	}
	if !foundOriginal {
		t.Fatal("original task absent from first page")
	}
	if len(first.Items) != 100 || first.Next == "" || first.Matched != 106 || first.Scope.Truncated {
		t.Fatal(first)
	}
	workflowGet(t, server, "jobs?after="+url.QueryEscape(first.Next), &second)
	if len(second.Items) != 6 || second.Next != "" || second.Items[0].Job.ID <= first.Next {
		t.Fatal(second)
	}
	workflowGet(t, server, "jobs?filter=emby&q="+url.QueryEscape("fixture SHOW"), &filtered)
	if filtered.Matched != 21 || len(filtered.Items) != 21 || filtered.Items[0].Job.Import != "imported" {
		t.Fatal(filtered)
	}
	workflowGet(t, server, "jobs?q="+url.QueryEscape("%_"), &filtered)
	if len(filtered.Items) != 0 {
		t.Fatal("search treated SQL wildcard as pattern")
	}
	for _, secret := range []string{"hidden-secret", "magnet:", "guid", "fingerprint", "hash\"", "/downloads", "/library"} {
		if strings.Contains(raw, secret) {
			t.Fatal("privacy", secret)
		}
	}
	if got := safeWorkflowText("Example https://feed.invalid/rss?token=RSS_SECRET token=SECRET password=SECRET"); strings.Contains(got, "SECRET") || strings.Contains(got, "feed.invalid") {
		t.Fatal(got)
	}
	var after int
	f.store.db.QueryRow("SELECT count(*) FROM events").Scan(&after)
	if after != events {
		t.Fatal("read emitted events")
	}
	for _, query := range []string{"filter=invalid", "filter=all%7Cdownloading", "q=" + strings.Repeat("a", 257), "after=" + strings.Repeat("a", 129)} {
		res, err := server.Client().Get(server.URL + "/api/jobs?" + query)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 400 {
			t.Fatal(query, res.StatusCode)
		}
	}
}
func TestManagementWorkflowBoundedCounts(t *testing.T) {
	f := newFixture(t, "Example [02].mkv", -1)
	_, server := managementFixture(t, f)
	addWorkflowJobs(t, f.store, workflowLimit+1)
	var page workflowPage
	workflowGet(t, server, "jobs?q=wf-10000", &page)
	if !page.Scope.Truncated || page.Scope.Total != 10001 || page.Scope.Scanned != 10000 || page.Matched != 0 {
		t.Fatal(page)
	}
	var data struct {
		Counts map[string]int
		Scope  struct {
			Total, Scanned int
			Truncated      bool
		}
	}
	workflowGet(t, server, "workflow", &data)
	if data.Counts["all"] != 10000 || data.Counts["downloading"] != 2000 || !data.Scope.Truncated {
		t.Fatal(data)
	}
}
func TestManagementWorkflowPendingRulesAreReadOnly(t *testing.T) {
	f, _, input := learningFixture(t)
	p, err := f.store.SubmitProposal(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	_, server := managementFixture(t, f)
	var data struct{ Counts map[string]int }
	workflowGet(t, server, "workflow", &data)
	if data.Counts["rules"] != 1 || data.Counts["ready"] != 1 {
		t.Fatal(data)
	}
	current, err := f.store.Proposal(context.Background(), p.ID, p.Revision)
	if err != nil || current.Version != p.Version || current.State != p.State {
		t.Fatal("overview changed review state", current, err)
	}
}

func TestFrontendWorkflowBrowserLoop(t *testing.T) {
	if os.Getenv("NAS_BROWSER_TEST") != "1" {
		t.Skip("run make verify-ui")
	}
	f := newFixture(t, "Example [02].mkv", 7)
	f.tick(1000)
	configured, web := managementFixture(t, f)
	configured.Daemon.Config.QBURL = f.server.URL
	configured.Daemon.Config.EmbyURL = f.server.URL
	addWorkflowJobs(t, f.store, 105)
	empty := newFixture(t, "Empty.mkv", -1)
	emptyWeb := httptest.NewUnstartedServer(nil)
	d := &Daemon{Engine: empty.engine}
	m, err := NewManagement(context.Background(), d, emptyWeb.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	emptyWeb.Config.Handler = m
	emptyWeb.Start()
	defer emptyWeb.Close()
	command := exec.Command("python3", "../../tests/frontend_workflow_browser.py")
	command.Env = append(os.Environ(), "NAS_WORKFLOW_URL="+web.URL, "NAS_WORKFLOW_EMPTY_URL="+emptyWeb.URL)
	out, err := command.CombinedOutput()
	t.Log(string(out))
	if err != nil {
		t.Fatal(err)
	}
}
