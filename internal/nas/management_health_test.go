package nas

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// 健康信息只说明支持与配置，不把启用开关或已有结果当作实时连接证明。
func TestManagementHealthCapabilities(t *testing.T) {
	f := namingFixture(t)
	m, server := managementFixture(t, f)
	for _, enabled := range []bool{false, true} {
		m.Daemon.Config.Events.Enabled = enabled
		m.Daemon.Config.Learning.Enabled = enabled
		m.Daemon.Config.Learning.AutomaticNaming = enabled
		response := httptest.NewRecorder()
		request := httptest.NewRequest("GET", server.URL+"/api/health", nil)
		m.ServeHTTP(response, request)
		var body struct {
			Gateway      string `json:"gateway"`
			Discord      string `json:"discord"`
			Model        string `json:"model"`
			Capabilities map[string]struct {
				Supported         bool   `json:"supported"`
				ConfiguredEnabled bool   `json:"configured_enabled"`
				Runtime           string `json:"runtime"`
			} `json:"capabilities"`
		}
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &body) != nil {
			t.Fatal("health response", response.Code)
		}
		if body.Gateway != "unknown" || body.Discord != "unknown" || body.Model != "unknown" {
			t.Fatal("configuration claimed connection", body)
		}
		for _, key := range []string{"events", "learning", "automatic_naming"} {
			v := body.Capabilities[key]
			if !v.Supported || v.ConfiguredEnabled != enabled || v.Runtime != "unknown" {
				t.Fatal("wrong capability", key, v)
			}
		}
		v := body.Capabilities["naming"]
		if !v.Supported || v.Runtime != "unknown" {
			t.Fatal("naming support", v)
		}
	}
	// 健康读取不创建建议、任务意图或模型调用。
	items, err := f.engine.NamingReviews(context.Background(), f.job().ID)
	if err != nil || len(items) != 0 || f.job().Plan != nil {
		t.Fatal("health mutated naming", err)
	}
}
