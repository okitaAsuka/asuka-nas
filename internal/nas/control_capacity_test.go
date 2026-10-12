package nas

import (
	"context"
	"fmt"
	"testing"
)

func TestControlPaginationCapacityAndHistory(t *testing.T) {
	f := newFixture(t, "Example.S01E01.mkv", -1)
	ctx := context.Background()
	for round := 0; round < 2; round++ {
		profiles := []Subscription{}
		for i := 0; i < 100; i++ {
			profiles = append(profiles, Subscription{ID: fmt.Sprintf("p%d-%03d", round, i)})
		}
		if err := f.store.InitControl(ctx, profiles); err != nil {
			t.Fatal(err)
		}
	}
	// 页边界独立于调度：完整读出200条历史、当前100条启用项。
	page, next, total, err := f.store.SubscriptionPage(ctx, "")
	if err != nil || len(page) != 100 || next != "p0-099" || total != 200 {
		t.Fatal(len(page), next, total, err)
	}
	page, next, total, err = f.store.SubscriptionPage(ctx, next)
	if err != nil || len(page) != 100 || next != "" || page[99].ID != "p1-099" || total != 200 {
		t.Fatal(page, next, total, err)
	}
	active, err := f.store.ScheduledSubscriptions(ctx)
	if err != nil || len(active) != 100 {
		t.Fatal(len(active), err)
	}
	added := ManagedSubscription{ID: "extra", Profile: "p1-000", Name: "extra", Enabled: true}
	if err = f.store.PutSubscription(ctx, added); err == nil || err.Error() != "subscription_limit" {
		t.Fatal("active capacity not enforced", err)
	}
	all, _ := f.store.ManagedSubscriptions(ctx)
	if len(all) != 200 {
		t.Fatal("rejected creation persisted")
	}
	// 停用项不占活跃容量；超限启用必须连版本、审计一起回滚。
	added.Enabled = false
	if err = f.store.PutSubscription(ctx, added); err != nil {
		t.Fatal(err)
	}
	added.Version = 1
	added.Enabled = true
	if err = f.store.PutSubscription(ctx, added); err == nil || err.Error() != "subscription_limit" {
		t.Fatal(err)
	}
	old := ManagedSubscription{ID: "p0-000", Profile: "p0-000", Name: "historical", Enabled: false, Version: 1}
	if err = f.store.PutSubscription(ctx, old); err != nil {
		t.Fatal("cannot disable unavailable history", err)
	}
	old.Version = 2
	old.Enabled = true
	if err = f.store.PutSubscription(ctx, old); err == nil || err.Error() != "profile_unavailable" {
		t.Fatal("missing profile enabled", err)
	}
	current := ManagedSubscription{ID: "p1-000", Profile: "p1-000", Name: "current", Enabled: false, Version: 1}
	if err = f.store.PutSubscription(ctx, current); err != nil {
		t.Fatal(err)
	}
	if err = f.store.PutSubscription(ctx, added); err != nil {
		t.Fatal("disabled item consumed active capacity", err)
	}
	// 配置初始化同样遵守容量：现有自建映射+100个新档案会超过100。
	profiles := []Subscription{{ID: "p1-000"}}
	for i := 0; i < 99; i++ {
		profiles = append(profiles, Subscription{ID: fmt.Sprintf("p2-%03d", i)})
	}
	// p1-000 已停用，因此 extra+99 =100合法，再启用p1-000应拒绝。
	if err = f.store.InitControl(ctx, profiles); err != nil {
		t.Fatal(err)
	}
	current.Version = 2
	current.Enabled = true
	if err = f.store.PutSubscription(ctx, current); err == nil || err.Error() != "subscription_limit" {
		t.Fatal(err)
	}
	// 新配置替换失败必须原子恢复上一配置档案集。
	freed := ManagedSubscription{ID: "p2-000", Profile: "p2-000", Name: "freed", Enabled: false, Version: 1}
	if err = f.store.PutSubscription(ctx, freed); err != nil {
		t.Fatal(err)
	}
	if err = f.store.PutSubscription(ctx, current); err != nil {
		t.Fatal(err)
	}
	before, _ := f.store.ManagedSubscriptions(ctx)
	profiles = []Subscription{{ID: "p1-000"}}
	for i := 0; i < 99; i++ {
		profiles = append(profiles, Subscription{ID: fmt.Sprintf("p3-%03d", i)})
	}
	if err = f.store.InitControl(ctx, profiles); err == nil || err.Error() != "subscription_limit" {
		t.Fatal("configuration bypassed active cap", err)
	}
	after, _ := f.store.ManagedSubscriptions(ctx)
	if len(before) != len(after) {
		t.Fatal("failed configuration inserted history")
	}
	var kept, newRows int
	f.store.db.QueryRow("SELECT count(*) FROM control_profiles WHERE id='p2-000'").Scan(&kept)
	f.store.db.QueryRow("SELECT count(*) FROM subscriptions WHERE id LIKE 'p3-%'").Scan(&newRows)
	if kept != 1 || newRows != 0 {
		t.Fatal("failed configuration did not roll back", kept, newRows)
	}
	active, err = f.store.ScheduledSubscriptions(ctx)
	if err != nil || len(active) != 100 {
		t.Fatal(len(active), err)
	}
}
func TestControlHistoryCapacityExplicitRollback(t *testing.T) {
	f := newFixture(t, "Example.S01E01.mkv", -1)
	ctx := context.Background()
	if err := f.store.InitControl(ctx, []Subscription{{ID: "fixture"}}); err != nil {
		t.Fatal(err)
	}
	_, err := f.store.db.Exec(`WITH RECURSIVE n(v) AS (SELECT 1 UNION ALL SELECT v+1 FROM n WHERE v<9999) INSERT INTO subscriptions SELECT 'old-'||v FROM n;
 INSERT INTO managed_subscriptions SELECT id,'removed',id,0,1 FROM subscriptions WHERE id LIKE 'old-%';`)
	if err != nil {
		t.Fatal(err)
	}
	v := ManagedSubscription{ID: "overflow", Profile: "fixture", Name: "overflow", Enabled: false}
	if err = f.store.PutSubscription(ctx, v); err == nil || err.Error() != "subscription_history_limit" {
		t.Fatal(err)
	}
	if err = f.store.InitControl(ctx, []Subscription{{ID: "new-profile"}}); err == nil || err.Error() != "subscription_history_limit" {
		t.Fatal(err)
	}
	var total, overflow int
	f.store.db.QueryRow("SELECT count(*) FROM managed_subscriptions").Scan(&total)
	f.store.db.QueryRow("SELECT count(*) FROM subscriptions WHERE id IN ('overflow','new-profile')").Scan(&overflow)
	if total != 10000 || overflow != 0 {
		t.Fatal(total, overflow)
	}
}
