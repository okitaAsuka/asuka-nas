# 本轮review evidence / 2026-10-12

请求：先连通通知、下载与人工映射。单一跨仓owner；没有递归委派或另开reviewer。请求配置gpt-6.1-sol/high/Standard；运行时实际配置无法核实，标未知。

基线：NAS 2f211868332f8ac20068eae9a4fa15b8e7da477d（Library v7），agent a1ff5c4bb06ab10889f217815fa78827afc3c695（远端已核对，无更新差异）。两仓均新分支codex/nas-notifications-20261012；恢复前agent工作区clean，无覆盖已有修改。本轮允许本地提交与跨仓修改，覆盖仓库默认不commit/不跨仓约定。

## 最小diff与接口

- NAS新增internal/nas/event_service.go，Config.Events和CLI多服务取消/等待。新回环事件端口GET /v1/events?after=N，最多100条；独立Bearer env引用、精确Host、拒绝浏览器Origin/Sec-Fetch-Site，返回安全白名单。管理/api nonce不变。
- agent新增internal/apps/gateway/nasnotify/{config,store,worker}.go；gateway Config配置、Service.Start生命周期与shutdown等待、CLI本机status/resume。包不import Runtime，不直接访问NAS数据库。通用Discord聊天adapter仍未实现。
- inbox/outbox/cursor单事务；owner长期BEGIN EXCLUSIVE锁持有独立连接、退出自动释放。ACK后提交sent；失败沿用相同nonce。暂停/dead/quarantine持久可见；429目标级等待与普通事件级退避独立。
- nas_notification_state投影到现有/events/Operator事件页，白名单nas_event_id/state/error_code。该旧日志best-effort，恢复以本队列为准；UI源码未修改，所以本轮无新增浏览器验收要求。NAS管理health不探测gateway/Discord连接，不作为本通知状态依据。
- NAS现有下载、规则、合集、文件操作代码未重写。新增合成例子：东京篇章人工Season09；Re:Zero人工Season04，bounded67→1..77→11。实际规则保存/预览/启用/重规划API测试，下一集自动规则；合集相对/总集两类测试。

## 实际验证结果

工具链：go version go1.26.9 linux/amd64，真实Go二进制；模块缓存/workspace/asuka-cloud/cache/go-mod，构建缓存/workspace/asuka-cloud/cache/go-build。

- NAS：PATH=<go1.26.9>/bin:$PATH GOMODCACHE=<cache> GOCACHE=<cache> make verify — exit0；gofmt/vet/go test -race -count=1 ./.../build，领域包56.709s；Python预检5/5通过。
- agent：同工具链与缓存，GO_CACHE_DIR=<cache> make verify — exit0；gofmt/go test ./.../go vet ./...完整一次通过。另nasnotify定向race测试通过，包括source授权暂停、HTTP超时/重定向、发送上限与目标级429等待。
- NAS默认配置：go run ./cmd/asuka-nas -config config.example.json — exit0，configuration_valid，无网络/文件执行。
- agent默认配置：go run . --config configs gateway --dry-run — exit0。
- agent go build — exit0；另GOOS=windows GOARCH=amd64 nasnotify测试包交叉编译通过（-exec true，仅编译，未声称Windows运行验收）。
- 跨仓：python3 scripts/nas-notifications-smoke.py --nas-repo /workspace/asuka-nas --go /workspace/toolchains/go1.26.9/bin/go — exit0。真实NAS事件handler→真实Worker→fake Discord；ready=2，mapping-blocked=1，cursor=30；503后重启持久重试，消息禁mentions与隐私断言通过。
- 两仓git diff --check通过。没有UI源码修改；既有完整Go测试中的browser opt-in仍按默认skip，未将其描述为实际Chromium验收。

定向核心测试：TestAtomicCursorInboxOutboxAndOwnership（SQLite触发器回滚、重复页、reopen、双owner/重绑定拒绝）；TestProgressCoalescingCriticalRetentionAndPoison；TestPoisonPayloadWithTrustedID；TestRestartRetryRateLimitAndAcknowledgementLoss（429重启、ACK后事务失败、same nonce）；TestPermanentErrorsAndLocalResume；TestTimeoutAndBoundedRetries；TestSourceAuthorizationPauseStillSendsInbox；TestRedirectRefusedAndHTTPTimeoutRetainsIntent；TestRetryLimitKeepsGlobalRateWait；NAS TestEventServiceAuthAndReplay/TestNotificationDownloadMappingVertical。

测试开发时发现并修正：慢fake服务不读request导致测试close等待，改为有限100ms响应；source授权测试重新绑定endpoint需新store符合身份护栏；跨仓失败行退避期间其它关键通知可先发送，因此nonce复用断言按同nonce重现而非相邻消息。实现自测中修正第12次429后dead仍须保留目标级Retry-After等待。未绕过既有安全断言。

## 剩余边界

通知at-least-once；Discord官方nonce+enforce_nonce只去重过去几分钟，ACK丢失或窗口外重试可能重复。未知/坏事件有可信id时隔离，不保存原payload；无可信id或坏cursor不能安全跳过，需修复来源协议（固定错误审计+有界退避）。dead/鉴权暂停由停机本机CLI resume显式恢复，隔离记录保留供开发者升级后可信重放。队列没有保留期/清理策略，首版长期运行需关注容量；不自动删恢复证据。

没有真实NAS/qB/Emby/Discord凭据、真实外发、部署、隧道、远端创建/推送/合并、模型调用、联网元数据或自动学习。真实服务版本、Bot权限、跨机拓扑与真实NAS文件/挂载验收仍需部署阶段完成。

## 父任务复核顺序

先读本文，再读下列截取的关键新增代码（本文件内），即可审查接口、事务边界与发送恢复；必要时才展开整个源文件。启动配置及失败处理在NOTIFICATIONS文档。最终HEAD和Library版本以交付回复为准。

### NAS只读handler / internal/nas/event_service.go

```go
func EventHandler(store *Store, token, host string) http.Handler {
	expected := sha256.Sum256([]byte("Bearer " + token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		actual := sha256.Sum256([]byte(r.Header.Get("Authorization")))
		if len(token) < 32 || r.Host != host || r.Header.Get("Origin") != "" || r.Header.Get("Sec-Fetch-Site") != "" || subtle.ConstantTimeCompare(actual[:], expected[:]) != 1 {
			apiError(w, 401, "service_unauthorized")
			return
		}
		if r.URL.Path != "/v1/events" {
			apiError(w, 404, "not_found")
			return
		}
		if r.Method != "GET" {
			apiError(w, 405, "read_only")
			return
		}
		q, e := r.URL.Query()["after"]
		if !e || len(q) != 1 || len(r.URL.Query()) != 1 {
			apiError(w, 400, "invalid_cursor")
			return
		}
		after, err := strconv.ParseInt(q[0], 10, 64)
		if err != nil || after < 0 {
			apiError(w, 400, "invalid_cursor")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5e9)
		defer cancel()
		events, err := store.Events(ctx, after)
		if err != nil {
			apiError(w, 503, "events_unavailable")
			return
		}
		page := eventPage{Events: make([]wireEvent, 0, len(events)), Next: after}
		for _, v := range events {
			page.Events = append(page.Events, wireEvent{v.ID, v.SchemaVersion, v.JobID, v.Kind, v.PlanVersion, v.FileIndex})
			page.Next = v.ID
		}
		apiJSON(w, page)
	})
}

```

### cursor事务 / internal/apps/gateway/nasnotify/store.go

```go
func (q *Queue) Accept(ctx context.Context, p page, now time.Time) error {
	if len(p.Events) > 100 {
		return errors.New("nas_page_invalid")
	}
	tx, e := q.db.BeginTx(ctx, nil)
	if e != nil {
		return errors.New("notification_persist_failed")
	}
	defer tx.Rollback()
	var cursor int64
	if e = tx.QueryRowContext(ctx, `SELECT cursor FROM meta WHERE id=1`).Scan(&cursor); e != nil {
		return errors.New("notification_persist_failed")
	}
	last := cursor
	for _, v := range p.Events {
		if v.ID <= 0 {
			return errors.New("nas_page_invalid")
		}
		if v.ID <= cursor {
			continue
		}
		if v.ID <= last {
			return errors.New("nas_page_invalid")
		}
		last = v.ID
		content, progress, state := render(v)
		job, kind := v.Job, v.Kind
		if state == "quarantined" {
			job = ""
			kind = "invalid_event"
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO inbox(id,job,kind,state) VALUES(?,?,?,?)`, v.ID, job, kind, state); e != nil {
			return errors.New("notification_persist_failed")
		}
		if content == "" {
			continue
		}
		due := now.UnixMilli()
		if progress {
			// 仅合并尚未尝试的进度；已发送/在途的nonce与内容永久固定。
			if _, e = tx.ExecContext(ctx, `UPDATE outbox SET state='superseded' WHERE job=? AND progress=1 AND state='pending' AND attempts=0`, job); e != nil {
				return errors.New("notification_persist_failed")
			}
			due += int64(q.cfg.ProgressSeconds) * 1000
		} else {
			if _, e = tx.ExecContext(ctx, `UPDATE outbox SET state='superseded' WHERE job=? AND progress=1 AND state='pending' AND attempts=0`, job); e != nil {
				return errors.New("notification_persist_failed")
			}
		}
		sum := sha256.Sum256([]byte(q.cfg.SourceID + ":" + q.cfg.NASURL + ":" + q.cfg.ChannelID + ":" + fmt.Sprint(v.ID)))
		nonce := hex.EncodeToString(sum[:12])
		if _, e = tx.ExecContext(ctx, `INSERT INTO outbox(id,job,content,nonce,state,progress,due) VALUES(?,?,?,?,'pending',?,?)`, v.ID, job, content, nonce, progress, due); e != nil {
			return errors.New("notification_persist_failed")
		}
	}
	if p.Next != last {
		return errors.New("nas_page_invalid")
	}
	if _, e = tx.ExecContext(ctx, `UPDATE meta SET cursor=?,poll_attempt=0,poll_next=?,poll_block=0 WHERE id=1`, last, now.Add(time.Duration(q.cfg.PollSeconds)*time.Second).UnixMilli()); e != nil {
		return errors.New("notification_persist_failed")
	}
	if tx.Commit() != nil {
		return errors.New("notification_persist_failed")
	}
	return nil
}

```

### 发送意图与ACK事务 / internal/apps/gateway/nasnotify/worker.go

```go
func (w *Worker) send(ctx context.Context, now time.Time) error {
	var next int64
	var blocked bool
	if e := w.Queue.db.QueryRowContext(ctx, `SELECT send_next,send_block FROM meta WHERE id=1`).Scan(&next, &blocked); e != nil {
		return errors.New("notification_persist_failed")
	}
	if blocked || now.UnixMilli() < next {
		return nil
	}
	var d delivery
	e := w.Queue.db.QueryRowContext(ctx, `SELECT id,content,nonce,attempts FROM outbox WHERE state='pending' AND due<=? ORDER BY progress,id LIMIT 1`, now.UnixMilli()).Scan(&d.id, &d.content, &d.nonce, &d.attempt)
	if e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return nil
		}
		return errors.New("notification_persist_failed")
	}
	if d.attempt >= 12 {
		if _, e = w.Queue.db.ExecContext(ctx, `UPDATE outbox SET state='dead',error_code='discord_retry_exhausted' WHERE id=?`, d.id); e != nil {
			return errors.New("notification_persist_failed")
		}
		w.notice(d.id, "dead", "discord_retry_exhausted")
		return nil
	}
	d.attempt++
	if _, e = w.Queue.db.ExecContext(ctx, `UPDATE outbox SET attempts=?,due=? WHERE id=?`, d.attempt, now.Add(backoff(d.attempt)).UnixMilli(), d.id); e != nil {
		return errors.New("notification_persist_failed")
	}
	body, _ := json.Marshal(map[string]any{"content": d.content, "nonce": d.nonce, "enforce_nonce": true, "allowed_mentions": map[string]any{"parse": []string{}, "replied_user": false}})
	req, _ := http.NewRequestWithContext(ctx, "POST", w.Queue.cfg.DiscordURL+"/channels/"+w.Queue.cfg.ChannelID+"/messages", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bot "+w.discordToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "AsukaNASNotifications/1")
	resp, e := w.client.Do(req)
	state, code, messageID := "pending", "discord_transport_failed", ""
	delay := backoff(d.attempt)
	halt := false
	rateLimited := false
	if e == nil {
		defer resp.Body.Close()
		b, readErr := io.ReadAll(io.LimitReader(resp.Body, 65537))
		switch {
		case resp.StatusCode == 200 || resp.StatusCode == 201:
			var ack struct {
				ID string `json:"id"`
			}
			if readErr == nil && len(b) <= 65536 && json.Unmarshal(b, &ack) == nil && snowflake(ack.ID) {
				state = "sent"
				code = ""
				messageID = ack.ID
			} else {
				code = "discord_ack_invalid"
			}
		case resp.StatusCode == 429:
			rateLimited = true
			code = "discord_rate_limited"
			var parsed bool
			delay, parsed = retryAfter(resp, b, now)
			if !parsed {
				halt = true
				code = "discord_retry_after_invalid"
			}
		case resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 404:
			state = "dead"
			code = "discord_destination_rejected"
			halt = true
		case resp.StatusCode >= 400 && resp.StatusCode < 500:
			state = "dead"
			code = "discord_request_rejected"
		default:
			code = "discord_service_failed"
		}
	}
	if d.attempt >= 12 && state == "pending" {
		state = "dead"
		code = "discord_retry_exhausted"
	}
	// 发送ACK与outbox结果同一事务；ACK丢失仍用相同nonce，超过平台去重窗口可能重复。
	tx, e := w.Queue.db.BeginTx(ctx, nil)
	if e != nil {
		return errors.New("notification_persist_failed")
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `UPDATE outbox SET state=?,due=?,error_code=?,message_id=? WHERE id=?`, state, now.Add(delay).UnixMilli(), code, messageID, d.id); e != nil {
		return errors.New("notification_persist_failed")
	}
	next = now.Add(time.Second).UnixMilli()
	if rateLimited {
		next = now.Add(delay).UnixMilli()
	}
	if _, e = tx.ExecContext(ctx, `UPDATE meta SET send_next=?,send_block=? WHERE id=1`, next, halt); e != nil {
		return errors.New("notification_persist_failed")
	}
	if tx.Commit() != nil {
		return errors.New("notification_persist_failed")
	}
	w.notice(d.id, state, code)
	return nil
}
```
