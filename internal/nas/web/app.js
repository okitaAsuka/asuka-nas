const $ = (s) => document.querySelector(s),
  view = $("#view"),
  message = $("#message");
let csrf = "",
  tab = "jobs",
  generation = 0,
  busy = false,
  detailID = "",
  cursor = "",
  eventCursor = 0,
  subscriptionCursor = "",
  ruleClues = null,
  activeLibraryScan = null;
const names = {
  jobs: "任务与入库",
  subscriptions: "订阅管理",
  rules: "命名规则",
  events: "事件记录",
  health: "健康与接入",
};
const states = {
  pending: "等待处理",
  requested: "已请求",
  requesting: "请求中",
  completed: "下载完成",
  imported: "已硬链接",
  planned: "计划已批准",
  needs_confirmation: "待人工确认",
  confirmed: "已发现",
  stopped: "已停止",
  seeding: "做种中",
  active: "做种中",
  linked: "已链接",
  intended: "已写执行意图",
  not_connected: "未接入",
};
const errors = {
  subscription_limit: "当前可用且启用的订阅已达 100 条；请先停用部分订阅。",
  subscription_history_limit:
    "订阅历史已达 10000 条；操作未保存，请先离线备份并安排历史治理，程序不会自动删除证据。",
  profile_unavailable:
    "来源档案已移除；可保留或停用历史订阅，恢复档案后才能启用。",
  operation_busy: "另一项操作仍在执行，请稍后重试。",
  stale_version: "状态已变化，请刷新后重试。",
  plan_frozen: "计划已执行，目标保持冻结。临时文件或服务问题修正后可恢复；规则不能移动历史文件。",
  fingerprint_changed: "源文件指纹变化，请核对文件。",
  conflict: "目标已存在不同文件；请人工检查，不会覆盖。",
  target_reserved: "目标被另一任务预约，请检查冲突任务。",
  cross_device: "下载和媒体目录不在同一文件系统。",
  permission_denied: "缺少文件权限，请检查部署挂载和运行用户。",
  subscription_frozen: "已有来源记录，不能改变此订阅的来源引用。",
  rule_revision_exists: "此版本已存在，请创建更高的新版本。",
  operation_failed: "操作失败。请检查任务错误码、配置或本地安全日志后重试。",
  csrf_rejected: "页面会话失效，请重新加载页面。",
  invalid_request: "请求格式不正确。",
  library_limit: "目录/NFO 超过安全限额，请选择更小的系列目录。",
  library_unsafe_path: "目录包含符号链接、非普通文件或不安全路径；请人工检查。",
  library_unavailable: "配置的媒体根不可用，请检查挂载与权限。",
  library_read_failed: "无法读取所选目录，请检查本地权限。",
  library_cancelled: "目录读取已取消或超时。",
  library_changed: "读取时目录发生变化，请重新读取候选。",
  candidate_stale: "候选或目标目录已变化，请重新读取、选择和预览。",
  stale_plan: "未开始计划已过期：返回任务重新规划；重试不会改动旧计划。",
};
function el(tag, text, cls) {
  const n = document.createElement(tag);
  if (text !== undefined) n.textContent = String(text);
  if (cls) n.className = cls;
  return n;
}
function panel(title) {
  const n = el("section", undefined, "panel");
  if (title) n.append(el("h2", title));
  return n;
}
function button(text, fn, primary = false) {
  const n = el("button", text, primary ? "primary" : "");
  n.type = "button";
  n.addEventListener("click", fn);
  return n;
}
function badge(s, kind = "") {
  return el("span", states[s] || s, "badge " + kind);
}
function time(v) {
  return v
    ? new Date(v * 1000).toLocaleString("zh-CN", { hour12: false })
    : "尚未记录";
}
function fail(err) {
  message.className = "error";
  message.textContent = errors[err.message] || `操作未完成：${err.message}`;
}
async function api(path, data, signal) {
  let r;
  try {
    r = await fetch("/api/" + path, {
      method: data === undefined ? "GET" : "POST",
      headers:
        data === undefined
          ? {}
          : { "Content-Type": "application/json", "X-CSRF-Nonce": csrf },
      body: data === undefined ? undefined : JSON.stringify(data),
      credentials: "same-origin",
      signal,
    });
  } catch (e) {
    if (e.name === "AbortError") throw new Error("library_cancelled");
    throw new Error("本地服务暂时不可达，请刷新重试。");
  }
  let v;
  try {
    v = await r.json();
  } catch {
    throw new Error("服务返回了无法读取的响应。");
  }
  if (!r.ok) throw new Error(v.error || "operation_failed");
  return v;
}
function setBusy(v) {
  busy = v;
  document.querySelectorAll("[data-write]").forEach((b) => {
    b.disabled = v || b.dataset.unavailable === "true";
  });
}
function writeButton(text, fn, primary = false) {
  const n = button(text, fn, primary);
  n.dataset.write = "true";
  n.disabled = busy;
  return n;
}
async function mutate(fn) {
  if (busy) return;
  const g = generation;
  setBusy(true);
  message.textContent = "";
  try {
    await fn();
    if (g === generation) {
      message.className = "success";
      message.textContent = "操作已保存。";
      await load(false);
    }
  } catch (e) {
    if (g === generation) fail(e);
  } finally {
    setBusy(false);
  }
}
function field(parent, label, name, value = "", type = "text") {
  const l = el("label", label),
    n = el("input");
  n.name = name;
  n.value = value;
  n.type = type;
  l.append(n);
  parent.append(l);
  return n;
}
function select(parent, label, name, options, value) {
  const l = el("label", label),
    n = el("select");
  n.name = name;
  options.forEach(([v, text]) => {
    const o = el("option", text);
    o.value = v;
    n.append(o);
  });
  if (value !== undefined) n.value = value;
  l.append(n);
  parent.append(l);
  return n;
}
function table(headers) {
  const wrap = el("div", undefined, "scroll"),
    t = el("table"),
    head = el("tr"),
    body = el("tbody");
  headers.forEach((h) => head.append(el("th", h)));
  t.append(head, body);
  wrap.append(t);
  return { wrap, body };
}
function row(body, values) {
  const r = el("tr");
  values.forEach((v) => {
    const c = el("td");
    c.append(v instanceof Node ? v : el("span", v));
    r.append(c);
  });
  body.append(r);
}
function unavailable(b, reason) {
  b.disabled = true;
  b.dataset.unavailable = "true";
  b.title = reason;
}
async function load(clear = true) {
  activeLibraryScan?.abort();
  activeLibraryScan = null;
  const g = ++generation;
  if (clear) {
    message.textContent = "";
    view.replaceChildren(el("p", "正在加载…", "empty"));
  }
  view.setAttribute("aria-busy", "true");
  try {
    const content = await {
      jobs: jobsView,
      subscriptions: subscriptionsView,
      rules: rulesView,
      events: eventsView,
      health: healthView,
    }[tab]();
    if (g !== generation) return;
    view.replaceChildren(content);
    setBusy(busy);
  } catch (e) {
    if (g === generation) {
      view.replaceChildren(
        el("p", "未能加载。请点击右上角刷新状态。", "empty"),
      );
      fail(e);
    }
  } finally {
    if (g === generation) view.setAttribute("aria-busy", "false");
  }
}
function navigate(next) {
  tab = next;
  detailID = "";
  cursor = "";
  subscriptionCursor = "";
  $("#heading").textContent = names[tab];
  document
    .querySelectorAll("[data-tab]")
    .forEach((b) => b.classList.toggle("selected", b.dataset.tab === tab));
  load();
}
document
  .querySelectorAll("[data-tab]")
  .forEach((b) => (b.onclick = () => navigate(b.dataset.tab)));
$("#refresh").onclick = () => load();
async function jobsView() {
  if (detailID) return jobView(detailID);
  const data = await api("jobs?after=" + encodeURIComponent(cursor)),
    items = data.items;
  const root = el("div"),
    metrics = el("div", undefined, "metrics");
  [
    ["本页任务", items.length],
    [
      "待人工确认",
      items.filter(
        (v) => v.job.block_reason || v.job.import === "needs_confirmation",
      ).length,
    ],
    ["Emby 已就绪", items.filter((v) => v.ready).length],
  ].forEach(([name, n]) => {
    const m = el("div", undefined, "metric");
    m.append(el("strong", n), el("span", name));
    metrics.append(m);
  });
  root.append(metrics);
  const p = panel("任务进度"),
    filters = el("div", undefined, "inline"),
    filter = select(filters, "本页筛选", "filter", [
      ["all", "全部"],
      ["blocked", "待确认 / 异常"],
      ["ready", "媒体已就绪"],
      ["active", "处理中"],
    ]);
  p.append(filters);
  const { wrap, body } = table([
    "任务 / 来源",
    "下载",
    "入库",
    "Emby",
    "做种",
    "操作",
  ]);
  function render() {
    body.replaceChildren();
    const selected = items.filter(
      (v) =>
        filter.value === "all" ||
        (filter.value === "blocked" &&
          (v.job.block_reason || v.job.error_code)) ||
        (filter.value === "ready" && v.ready) ||
        (filter.value === "active" && !v.ready),
    );
    for (const v of selected) {
      const j = v.job;
      row(body, [
        el("span", j.id.slice(0, 12), "mono"),
        badge(j.download),
        badge(j.import, j.block_reason ? "warn" : ""),
        badge(v.ready ? "已就绪" : j.discovery, v.ready ? "ok" : ""),
        el(
          "span",
          (states[j.seed] || j.seed) +
            " · " +
            (j.stop_at ? time(j.stop_at) : "待完成计时"),
        ),
        button("查看详情", () => {
          detailID = j.id;
          load();
        }),
      ]);
    }
    if (!selected.length)
      row(body, ["暂无任务。启用订阅后等待轮询。", "", "", "", "", ""]);
  }
  filter.onchange = render;
  render();
  p.append(wrap);
  const actions = el("div", undefined, "actions");
  if (cursor)
    actions.append(
      button("返回首页", () => {
        cursor = "";
        load();
      }),
    );
  if (data.next)
    actions.append(
      button("下一页", () => {
        cursor = data.next;
        load();
      }),
    );
  p.append(actions);
  root.append(p);
  return root;
}
async function jobView(id) {
  const v = await api("jobs/" + encodeURIComponent(id)),
    j = v.job,
    root = el("div");
  root.append(
    button("← 返回任务", () => {
      detailID = "";
      load();
    }),
  );
  const p = panel("任务详情");
  p.append(el("p", j.id, "mono"));
  const summary = el("dl", undefined, "summary");
  [
    ["下载", states[j.download] || j.download],
    ["入库", states[j.import] || j.import],
    ["刷新请求", states[j.refresh] || j.refresh],
    [
      "媒体发现",
      v.ready ? "全部发现 · 已就绪" : states[j.discovery] || j.discovery,
    ],
    ["做种", states[j.seed] || j.seed],
    ["停止期限", time(j.stop_at)],
    ["完成时间", time(j.completed_at)],
    ["做种天数", j.seed_days + " 天"],
    ["下次尝试", time(j.next_attempt)],
  ].forEach(([k, x]) => {
    const cell = el("div");
    cell.append(el("dt", k), el("dd", x));
    summary.append(cell);
  });
  p.append(summary);
  if (j.block_reason || j.error_code) {
    p.append(
      el(
        "p",
        "阻塞 / 错误：" +
          [j.block_reason, j.error_code].filter(Boolean).join(" · "),
        "notice",
      ),
    );
    p.append(
      el(
        "p",
        errors[j.block_reason] ||
          errors[j.error_code] ||
          "核对源文件、显式季集映射和部署配置。低置信度不会自动猜季号。",
        "muted",
      ),
    );
  }
  const action = (a, extra = {}) =>
    mutate(() =>
      api("job-action", { id, version: j.version, action: a, ...extra }),
    );
  const actions = el("div", undefined, "actions");
  const permanent = ["late_mapping_conflict", "legacy_evidence_unresolved", "unsupported_episode_kind", "special_episode", "unsupported_manifest", "unsupported_video", "unsupported_file_set", "invalid_file_manifest", "subtitle_mapping_ambiguous"].includes(j.block_reason);
  const retry = writeButton("安全重试", () => action("retry"));
  if (permanent) unavailable(retry, "此阻塞需要人工诊断，重试不能解除");
  actions.append(retry);
  if (j.import === "needs_confirmation" && !j.plan?.started && !permanent) {
    actions.append(button("补充可复用映射规则", () => {
      ruleClues = { contexts: v.contexts || [], source: j.sources?.find(x => /\.(mkv|mp4)$/i.test(x)) || "" };
      navigate("rules");
    }));
  }
  if (permanent) p.append(el("p", "此阻塞不能通过重复重试或新规则强制通过。请核对完整清单/冻结来源证据，修正源数据或部署问题；本版没有历史迁移或强制批准工具，做种到期停止仍独立执行。", "notice"));
  const replan = writeButton("重新规划", () => action("replan"));
  if (j.plan?.started || j.import === "imported" || permanent)
    unavailable(replan, permanent ? "此阻塞需人工诊断，重新规划不能解除" : "已执行计划保持冻结");
  actions.append(replan);
  p.append(
    actions,
    el(
      "p",
      "安全重试只解除退避；不会重下、改名或删除已归档文件。停订阅不会取消已有任务的做种截止。",
      "muted",
    ),
  );
  root.append(p);
  if (v.contexts?.length) {
    const evidence = panel("来源证据（最多 100 条）");
    v.contexts.forEach((c) => {
      evidence.append(el("h3", c.subscription), el("p", c.title, "mono"));
    });
    root.append(evidence);
  }
  const plan = panel("文件组织方案");
  if (j.plan) {
    plan.append(
      el(
        "p",
        `版本 ${j.plan.version} · ${j.plan.started ? "已冻结执行" : j.plan.approved ? "已批准" : "待确认"} · ${j.plan.collection ? "整包方案" : "单集方案"}`,
        "muted",
      ),
    );
    const t = table(["源文件 → 目标", "指纹", "执行 / 发现"]);
    j.plan.files.forEach((f, i) => {
      const state = v.files?.find((x) => x.Index === i);
      row(t.body, [
        f.source + " → " + f.target,
        el("span", f.fingerprint, "mono"),
        (states[state?.State] || state?.State || "等待") +
          (state?.Video
            ? " / " + (state.Discovered ? "已发现" : "未发现")
            : " / 旁车"),
      ]);
    });
    plan.append(t.wrap);
    if (
      j.import === "needs_confirmation" &&
      !j.plan.started &&
      j.block_reason === "awaiting_plan_confirmation"
    )
      plan.append(
        writeButton(
          "确认此指纹绑定方案",
          () =>
            action("confirm", {
              plan_version: j.plan.version,
              digest: v.digest,
            }),
          true,
        ),
      );
    if (j.plan.rule_refs?.length)
      plan.append(el("p", "冻结规则：" + j.plan.rule_refs.join("，"), "muted"));
  } else
    plan.append(
      el("p", "尚未生成可执行方案。完整合集需先建立明确的有界规则。", "muted"),
    );
  root.append(plan);
  if (
    j.download === "completed" &&
    j.import !== "imported" &&
    !j.plan?.started &&
    !permanent
  ) {
    const form = panel("人工单集预览");
    form.append(
      el(
        "p",
        "仅完整单视频清单；字幕随主视频绑定。合集请使用有界规则，不支持挑选一集绕过整包检查。",
        "muted",
      ),
    );
    const source = select(
      form,
      "已下载的清单文件",
      "source",
      (j.sources || []).map((x) => [x, x]),
    );
    const target = field(
      form,
      "目标相对路径（必须明确 SxxExx）",
      "target",
      "",
      "text",
    );
    target.placeholder = "作品/Season 01/作品 - S01E01.mkv";
    form.append(
      writeButton(
        "生成文件预览",
        () => action("preview", { source: source.value, target: target.value }),
        true,
      ),
    );
    root.append(form);
  }
  return root;
}
async function subscriptionsView() {
  const data = await api(
      "subscriptions?after=" + encodeURIComponent(subscriptionCursor),
    ),
    root = el("div"),
    p = panel("订阅来源");
  p.append(
    el(
      "p",
      "启停只控制未来抓取。URL 在部署配置中管理，页面只使用来源引用；停用不会删除文件或中止做种期限。",
      "muted",
    ),
  );
  const t = table(["订阅", "来源引用", "轮询状态", "下次轮询", "操作"]);
  data.items.forEach((v) => {
    const buttons = el("div", undefined, "actions");
    buttons.append(
      writeButton(v.enabled ? "停用" : "启用", () =>
        mutate(() =>
          api("subscriptions", {
            id: v.id,
            profile: v.profile,
            name: v.name,
            enabled: !v.enabled,
            version: v.version,
          }),
        ),
      ),
    );
    buttons.append(
      button("编辑", () => {
        id.value = v.id;
        id.readOnly = true;
        if (!Array.from(profile.options).some((o) => o.value === v.profile)) {
          const old = el("option", v.profile + "（配置已移除，仅可停用）");
          old.value = v.profile;
          profile.append(old);
        }
        profile.value = v.profile;
        name.value = v.name;
        enabled.value = String(v.enabled);
        version = v.version;
        form.scrollIntoView({ block: "nearest" });
      }),
    );
    const retry = writeButton("重试抓取", () =>
      mutate(() => api("source-retry", { id: v.id, version: v.version })),
    );
    if (!v.enabled || !v.available)
      unavailable(retry, "来源未启用或配置已移除");
    buttons.append(retry);
    row(t.body, [
      v.name + " · " + v.id,
      v.profile + (v.available ? "" : "（配置缺失）"),
      v.status.ErrorCode || (v.enabled ? "已启用" : "已停用"),
      time(v.status.NextPoll),
      buttons,
    ]);
  });
  if (!data.items.length) row(t.body, ["暂无订阅", "", "", "", ""]);
  p.append(
    t.wrap,
    el(
      "p",
      `共 ${data.total} 条历史订阅，本页 ${data.items.length} 条；配置缺失项保留历史、不抓取。`,
      "muted",
    ),
  );
  const pages = el("div", undefined, "actions");
  if (subscriptionCursor)
    pages.append(
      button("返回订阅首页", () => {
        subscriptionCursor = "";
        load();
      }),
    );
  if (data.next)
    pages.append(
      button("下一页订阅", () => {
        subscriptionCursor = data.next;
        load();
      }),
    );
  p.append(pages);
  root.append(p);
  const form = panel("添加 / 编辑订阅");
  let version = 0;
  const grid = el("div", undefined, "grid"),
    id = field(grid, "订阅 ID", "subscription_id"),
    name = field(grid, "显示名称", "subscription_name"),
    profile = select(
      grid,
      "预授权来源引用",
      "profile",
      data.profiles.map((x) => [x, x]),
    ),
    enabled = select(grid, "抓取开关", "enabled", [
      ["true", "启用"],
      ["false", "停用"],
    ]);
  form.append(grid);
  const actions = el("div", undefined, "actions"),
    save = writeButton(
      "保存订阅",
      () =>
        mutate(() =>
          api("subscriptions", {
            id: id.value,
            name: name.value,
            profile: profile.value,
            enabled: enabled.value === "true",
            version,
          }),
        ),
      true,
    );
  if (!data.profiles.length) {
    unavailable(save, "请先在本地配置添加来源档案");
    form.append(
      el(
        "p",
        "没有预授权来源。请在本地配置中添加 subscriptions 后重启。",
        "notice",
      ),
    );
  }
  actions.append(
    save,
    button("清空 / 新建", () => {
      version = 0;
      id.value = "";
      id.readOnly = false;
      name.value = "";
    }),
  );
  form.append(actions);
  root.append(form);
  return root;
}
async function allSubscriptions() {
  const items = [];
  let after = "";
  do {
    const page = await api("subscriptions?after=" + encodeURIComponent(after));
    items.push(...page.items);
    after = page.next;
  } while (after);
  return { items };
}
async function rulesView() {
  const [rules, subscriptions, epoch, audit] = await Promise.all([
    api("rules"),
    allSubscriptions(),
    api("epoch"),
    api("audit"),
  ]);
  const root = el("div"),
    p = panel("不可变版本规则");
  p.append(
    el(
      "p",
      "确认一次文件方案不会启用自动规则。明确建立并启用映射后，后续集数才使用该规则；可以从明确选择的已有目录/NFO 获取候选；这不是模型学习，也不能推断新篇章季号。",
      "muted",
    ),
  );
  const t = table(["规则 / 版本", "映射", "状态", "操作"]);
  rules.forEach((v) => {
    const r = v.rule,
      actions = el("div", undefined, "actions");
    actions.append(
      button("载入为新版本", () =>
        fill({
          ...r,
          revision:
            Math.max(
              ...rules
                .filter((x) => x.rule.id === r.id)
                .map((x) => x.rule.revision),
            ) + 1,
        }),
      ),
    );
    actions.append(
      writeButton(v.enabled ? "停用" : "启用此版本", () =>
        mutate(() =>
          api("rule-state", {
            id: r.id,
            revision: r.revision,
            enabled: !v.enabled,
            epoch: epoch.epoch,
          }),
        ),
      ),
    );
    row(t.body, [
      r.id + " / v" + r.revision,
      r.display_name + " · 第 " + r.season + " 季",
      v.enabled ? "已启用" : v.active ? "已停用" : "草稿 / 历史",
      actions,
    ]);
  });
  if (!rules.length)
    row(t.body, ["尚无规则。请填写下面的显式映射。", "", "", ""]);
  p.append(t.wrap);
  root.append(p);
  const form = panel("创建新版本 · 先预览再启用"),
    grid = el("div", undefined, "grid"),
    fields = {};
  const defs = [
    ["规则 ID", "id", ""],
    ["版本", "revision", 1, "number"],
    ["系列 ID（稳定）", "series_id", ""],
    ["集序标识（稳定）", "order", "custom"],
    ["显示名称", "display_name", ""],
    ["目标媒体相对目录", "directory", ""],
    ["季号（必须确认）", "season", 1, "number"],
    ["字幕组（可空）", "group", ""],
    ["相对起始集（可空）", "start", "", "number"],
    ["相对结束集（可空）", "end", "", "number"],
    ["总集起始（可空）", "absolute_start", "", "number"],
    ["总集结束（可空）", "absolute_end", "", "number"],
    ["总集映射到相对首集", "relative_start", 1, "number"],
  ];
  defs.forEach(
    ([label, n, v, type]) => (fields[n] = field(grid, label, n, v, type)),
  );
  fields.subscription = select(
    grid,
    "订阅作用域",
    "rule_subscription",
    subscriptions.items.map((v) => [v.id, v.name + " · " + v.id]),
  );
  form.append(grid);
  const label = el("label", "标题别名（每行一个，完整作品名）"),
    aliases = el("textarea");
  aliases.name = "aliases";
  label.append(aliases);
  form.append(label);
  function rule() {
    const r = {
      provider: "manual",
      aliases: aliases.value
        .split("\n")
        .map((x) => x.trim())
        .filter(Boolean),
    };
    [
      "id",
      "series_id",
      "order",
      "display_name",
      "directory",
      "group",
      "subscription",
    ].forEach((k) => (r[k] = fields[k].value));
    ["revision", "season"].forEach((k) => (r[k] = Number(fields[k].value)));
    if (fields.start.value || fields.end.value)
      r.episodes = {
        start: Number(fields.start.value),
        end: Number(fields.end.value),
      };
    if (fields.absolute_start.value || fields.absolute_end.value)
      r.absolute = {
        start: Number(fields.absolute_start.value),
        end: Number(fields.absolute_end.value),
        relative_start: Number(fields.relative_start.value),
      };
    return r;
  }
  function fill(r) {
    selectedCandidate = null;
    invalidatePreview();
    for (const [k, f] of Object.entries(fields)) f.value = r[k] ?? "";
    fields.start.value = r.episodes?.start ?? "";
    fields.end.value = r.episodes?.end ?? "";
    fields.absolute_start.value = r.absolute?.start ?? "";
    fields.absolute_end.value = r.absolute?.end ?? "";
    fields.relative_start.value = r.absolute?.relative_start ?? 1;
    aliases.value = r.aliases.join("\n");
    preview.textContent = "";
    form.scrollIntoView({ block: "start" });
  }
  const samples = el("div", undefined, "grid"),
    title = field(samples, "示例 RSS 标题", "sample_title"),
    source = field(samples, "示例源文件名", "sample_source");
  form.append(samples);
  const preview = el("pre", undefined, "notice");
  preview.hidden = true;
  let selectedCandidate = null;
  let previewGeneration = 0,
    acceptedPreview = null;
  function previewRequest() {
    return { rule: rule(), title: title.value, source: source.value, ...(selectedCandidate ? { ...selectedCandidate, directory: libraryDirectory.value } : {}) };
  }
  // 程序化载入也必须失效；完整快照再校验，防止没有 input 事件的替换。
  function invalidatePreview() {
    previewGeneration++;
    acceptedPreview = null;
    preview.hidden = true;
    preview.textContent = "";
    saveDraft.dataset.unavailable = "true";
    saveDraft.disabled = true;
    message.textContent = "";
  }
  for (const event of ["input", "change", "reset"])
    form.addEventListener(event, invalidatePreview);
  const saveDraft = writeButton(
    "保存草稿版本",
    () => {
      if (
        !acceptedPreview ||
        acceptedPreview.snapshot !== JSON.stringify(previewRequest())
      ) {
        invalidatePreview();
        fail(new Error("请先预览当前规则及示例，再保存草稿。"));
        return;
      }
      // 保存已审核快照，不能重新读取可能被程序替换的表单。
      const reviewedRule = acceptedPreview.rule;
      mutate(() => acceptedPreview.candidate
        ? api("candidate-rule", { rule: reviewedRule, ...acceptedPreview.candidate })
        : api("rules", reviewedRule));
    },
    true,
  );
  unavailable(saveDraft, "先预览当前规则与示例");
  const actions = el("div", undefined, "actions");
  actions.append(
    writeButton("预览命名", async () => {
      if (busy) return;
      invalidatePreview();
      const g = generation,
        p = previewGeneration,
        request = previewRequest(),
        snapshot = JSON.stringify(request);
      const ownsResponse = () =>
        g === generation &&
        p === previewGeneration &&
        snapshot === JSON.stringify(previewRequest());
      setBusy(true);
      try {
        const v = await api(selectedCandidate ? "candidate-rule-preview" : "rule-preview", request);
        if (ownsResponse()) {
          acceptedPreview = {
            snapshot,
            rule: request.rule,
            revision: request.rule.revision,
            candidate: selectedCandidate ? { directory: request.directory, digest: request.digest } : null,
          };
          saveDraft.dataset.unavailable = "false";
          preview.hidden = false;
          preview.textContent =
            v.explanation + "\n" + JSON.stringify(v.decision, null, 2);
        }
      } catch (e) {
        if (ownsResponse()) fail(e);
      } finally {
        setBusy(false);
      }
    }),
    saveDraft,
  );
  form.append(actions, preview);
  root.append(form);
  const discovery = panel("从已有媒体目录提取候选"),
    libraryDirectory = field(discovery, "媒体根内的相对系列目录（明确选择）", "library_directory"),
    candidateOutput = el("div");
  discovery.append(el("p", "只读取所选目录下的文件名与有界 NFO；最多 512 项、3 层子目录、每份 NFO 64KiB/合计 1MiB、5 秒。拒绝符号链接。已有季仅作观察，不能据此认定新篇章属于下一季；选择候选不授权入库。", "muted"));
  let scanGeneration = 0, lastLibraryDirectory = libraryDirectory.value;
  function invalidateCandidate() {
    lastLibraryDirectory = libraryDirectory.value;
    scanGeneration++;
    activeLibraryScan?.abort();
    activeLibraryScan = null;
    selectedCandidate = null;
    candidateOutput.replaceChildren();
    invalidatePreview();
  }
  const directoryChanged = () => {
    if (libraryDirectory.value !== lastLibraryDirectory) invalidateCandidate();
  };
  libraryDirectory.addEventListener("input", directoryChanged);
  libraryDirectory.addEventListener("change", directoryChanged);
  discovery.append(writeButton("读取目录候选", async () => {
    if (busy) return;
    invalidateCandidate();
    const g = generation, scan = scanGeneration, directory = libraryDirectory.value;
    const controller = new AbortController();
    activeLibraryScan = controller;
    const owns = () => g === generation && scan === scanGeneration && directory === libraryDirectory.value && !controller.signal.aborted;
    setBusy(true);
    try {
      const result = await api("library-candidates", { directory }, controller.signal);
      if (!owns()) return;
      result.candidates.forEach(c => {
        const item = panel(c.season ? `已有证据：第 ${c.season} 季` : "季号未知"),
          info = el("pre", JSON.stringify({
            "名称": c.display_name || "未知，请填写",
            "已有相对集": c.episodes,
            "来源": c.sources.map(e => ({ "路径": e.source, "类型": ({filename:"已有文件名", episode_nfo:"集级 NFO", series_nfo:"系列 NFO"})[e.kind], "季": e.season || "未知", "集": e.episode || "未知", "总集": e.absolute || "未知" })),
            "冲突": c.conflicts.map(x => x.replace("invalid_nfo", "NFO 无效或不支持，需人工检查").replace("display_name_conflict", "来源名称互相矛盾，需明确名称").replace("absolute_episode_conflict", "总集与相对集配对互相矛盾").replace("episode_conflict", "文件名与 NFO 季集互相矛盾")),
            "待补": c.unknown.map(x => ({subscription_and_alias_require_selection:"请选择订阅并填写本次完整 RSS 别名", arc_season_requires_confirmation:"确认本次篇章季号；不能据前季外推", episode_order_requires_confirmation:"确认本地集序标识", season_unknown:"季号未知，请明确填写", display_name_unknown:"显示名称未知，请明确填写", absolute_mapping_unknown:"总集映射未知；有需要时明确有限范围"})[x] || x),
          }, null, 2), "notice");
        item.append(info, button("选入规则草稿", () => {
          if (!owns()) return;
          invalidatePreview();
          selectedCandidate = { directory: result.directory, digest: result.digest };
          fields.directory.value = result.directory;
          fields.display_name.value = c.display_name;
          fields.season.value = c.conflicts.length ? "" : (c.season || "");
          fields.series_id.value = c.series_id;
          fields.order.value = "custom";
          // 仅精确目录/名称的当前明确规则能提供已声明身份，绝不做相似度匹配。
          const identities = rules.filter(v => v.active && v.rule.directory === result.directory && v.rule.display_name === c.display_name);
          if (identities.length && new Set(identities.map(v => v.rule.series_id + "/" + v.rule.order)).size === 1) {
            fields.series_id.value = identities[0].rule.series_id;
            fields.order.value = identities[0].rule.order;
          }
          for (const k of ["start", "end", "absolute_start", "absolute_end"]) fields[k].value = "";
          preview.hidden = false;
          preview.textContent = "已载入局部候选。请确认本次篇章季号、订阅与完整别名；观察到的集数不会自动限制后续集数，总集映射请按明确证据填写。然后预览、保存草稿并单独启用。";
          form.scrollIntoView({ block: "start" });
        }));
        candidateOutput.append(item);
      });
    } catch (e) { if (owns()) fail(e); }
    finally {
      if (activeLibraryScan === controller) activeLibraryScan = null;
      setBusy(false);
    }
  }), button("取消目录读取", () => {
    invalidateCandidate();
    candidateOutput.append(el("p", "目录读取已取消；旧响应不会载入草稿。", "muted"));
  }), candidateOutput);
  root.insertBefore(discovery, form);
  if (ruleClues) {
    const clues = ruleClues;
    ruleClues = null;
    source.value = clues.source;
    fields.season.value = "";
    const choose = select(form, "任务来源线索（请选择并核对）", "mapping_context", [
      ["", "选择来源，仅带入订阅和原始标题"],
      ...clues.contexts.map((c, i) => [String(i), c.subscription + " · " + c.title]),
    ]);
    choose.addEventListener("change", () => {
      invalidatePreview();
      const context = clues.contexts[Number(choose.value)];
      if (choose.value !== "" && context) { fields.subscription.value = context.subscription; title.value = context.title; }
    });
    form.append(el("p", "启用后，无计划的待映射任务将在下次循环自动重检；已有未开始的过期计划需返回任务重新规划，已开始计划保持冻结。RSS 别名需本次人工填写，不从目录相似度自动启用。", "notice"));
  }
  const logs = panel("规则审计（最近 100 条）");
  const a = table(["序号", "规则 / 版本", "操作", "时间"]);
  audit.forEach((v) =>
    row(a.body, [
      v.ID,
      v.Subject + " / " + v.Revision,
      v.Kind,
      time(v.CreatedAt),
    ]),
  );
  if (!audit.length) row(a.body, ["暂无审计记录", "", "", ""]);
  logs.append(a.wrap);
  root.append(logs);
  return root;
}
async function eventsView() {
  const [events, sources] = await Promise.all([
    api("events?after=" + eventCursor),
    api("source-events?after=0"),
  ]);
  const root = el("div"),
    p = panel("任务事件 · 可重放游标");
  p.append(
    el(
      "p",
      "NAS 专用认证事件服务与 agent 通知端已实现，尚未真实连接；事件可供 gateway 消费；出现 discovered 仅表示 NAS 已确认媒体，不表示 Discord 已送达。两类事件使用各自游标。",
      "muted",
    ),
  );
  const t = table(["事件 ID", "任务", "类型", "计划 / 文件"]);
  (events || []).forEach((v) =>
    row(t.body, [
      v.ID,
      el("span", v.JobID.slice(0, 12), "mono"),
      v.Kind,
      (v.PlanVersion ?? "—") + " / " + (v.FileIndex ?? "—"),
    ]),
  );
  if (!events?.length) row(t.body, ["暂无新事件", "", "", ""]);
  p.append(t.wrap);
  const actions = el("div", undefined, "actions");
  if (eventCursor)
    actions.append(
      button("从头重放", () => {
        eventCursor = 0;
        load();
      }),
    );
  if (events?.length)
    actions.append(
      button("读取后续事件", () => {
        eventCursor = events.at(-1).ID;
        load();
      }),
    );
  p.append(actions);
  root.append(p);
  const s = panel("来源轮询事件（首 100 条）");
  const st = table(["ID", "订阅", "结果", "时间"]);
  (sources || []).forEach((v) =>
    row(st.body, [v.ID, v.Subscription, v.Kind, time(v.CreatedAt)]),
  );
  if (!sources?.length) row(st.body, ["暂无来源事件", "", "", ""]);
  s.append(st.wrap);
  root.append(s);
  return root;
}
async function healthView() {
  const health = await api("health"),
    p = panel("本地服务与接入边界");
  p.append(
    badge("数据库 " + health.database, "ok"),
    el(
      "p",
      "当前模式：仅本机单用户。禁止将此端口直接暴露到 NAS 局域网或反向代理；网络访问需要独立认证、TLS 和授权门。",
      "notice",
    ),
  );
  const t = table(["能力", "状态"]);
  [
    ["RSS → qB → 安全入库 → Emby", "已实现；本轮仅 fake 服务验收"],
    ["SQLite 可重放事件", "已实现，只读游标 API"],
    ["NAS 认证只读事件 / agent 固定通知", "配套实现；尚未真实连接或外发"],
    ["目录 / NFO 局部证据候选", "已实现；人工选择后保存明确规则，无模型学习"],
    ["元数据服务 / 模型建议", "未接入"],
    ["真实 NAS / Docker / 上游兼容", "待部署验收"],
  ].forEach((v) => row(t.body, v));
  p.append(
    t.wrap,
    el(
      "p",
      "程序离线、时钟错误或长文件 I/O 可能延后停止做种；停止不删除源文件。",
      "muted",
    ),
  );
  return p;
}
try {
  csrf = (await api("session")).csrf;
  await load();
} catch (e) {
  fail(e);
  view.replaceChildren(el("p", "无法建立本地会话，请重新加载页面。", "empty"));
}
