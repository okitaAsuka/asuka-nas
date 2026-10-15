const $ = (s) => document.querySelector(s),
  view = $("#view"),
  message = $("#message");
let csrf = "",
  tab = "overview",
  jobFilter = "all",
  jobQuery = "",
  detailSection = "",
  detailReturn = "jobs",
  generation = 0,
  busy = false,
  detailID = "",
  cursor = "",
  eventCursor = 0,
  subscriptionCursor = "",
  ruleClues = null,
  activeLibraryScan = null,
  focusAfterLoad = null,
  sessionMode = "local_single_user",
  authEpoch = 0,
  sessionLocked = false,
  sessionDraft = null,
  loadingDraft = null;
const pendingRequests = new Set();
const names = {
  overview: "日常总览",
  jobs: "任务与入库",
  subscriptions: "订阅管理",
  rules: "命名规则",
  events: "事件记录",
  health: "健康与接入",
};
const descriptions = {
  overview: "下载、命名、入库。今天的待办，一眼看清。",
  jobs: "追踪每一集的去向，核对依据后再继续。",
  subscriptions: "管理追番来源，让下一集按约而来。",
  rules: "把人工确认过的经验，变成有边界的规则。",
  events: "每一步都有记录，每一条状态都有出处。",
  health: "从离线镜像到服务接入，逐项核对真实状态。",
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
  authentication_failed: "用户名或密码不正确，请重新核对。",
  authentication_required: "会话已失效，请重新登录。未完成请求不会自动重发。",
  authentication_unavailable: "认证服务暂不可用，请稍后手动重试。",
  login_throttled: "登录尝试过于频繁。",
  session_limit: "有效会话已达上限，请从其他已登录浏览器退出，或等待会话到期。",
  origin_rejected: "页面地址与配置的访问地址不一致，请使用部署者提供的完整地址。",
  https_required: "此入口需要可信 HTTPS，请核对访问地址与证书配置。",
  store_failed: "本地数据暂时无法读取，请稍后重新加载。",
  database_unavailable: "本地数据库暂时不可用，请核对部署后重新加载。",
  invalid_cursor: "分页位置不正确，请返回列表首页。",
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
  invalid_job_state: "任务阶段已变化，请刷新任务查看当前方案。",
  episode_unresolved: "源文件编号未明确或存在双编号；请使用手动集号或明确的映射规则。",
  invalid_file_manifest: "完整源清单未通过验证，请核对全部下载文件。",
  unsupported_video: "此视频格式暂不支持整体整理。",
  unsupported_file_set: "文件集合不支持整体整理，请核对完整清单。",
  invalid_batch: "请填写安全的相对目录、显示名、季号，并核对编号方式。",
  order_confirmation_required: "请核对整表的顺序和连续编号，再明确勾选排序确认。",
  duplicate_episode: "集号重复；请逐行核对，完整任务不能重复目标。",
  collection_incomplete: "提交必须覆盖此下载任务的全部视频。",
  comparison_limit: "人工记录或来源证据达到限额；请先安排离线历史治理。",
  episode_out_of_bounds: "季号为1–99、集号为1–999，偏移为-998至998。",
  season_conflict: "源文件已有季号与本次季号不一致；混季任务不支持整体改写。",
  target_conflict: "目标已存在不同文件；不会覆盖，请检查并修改目标。",
  collection_preview_only: "多集单文件不支持；不能强制改成单集。",
  unsupported_episode_kind: "特殊篇或非整数编号暂不支持，请人工诊断。",
  subtitle_mapping_ambiguous: "字幕须同目录同视频stem，支持srt/ass/ssa/vtt及有限语言后缀。",
  naming_stale: "命名依据已变化；草稿保留。可独立人工整理，或明确请求新的建议快照。",
  naming_invalid: "命名事实或文件建议不完整，请核对目录、显示名、季号与全部文件。",
  naming_not_found: "此命名请求不存在，请重新读取任务。",
  naming_retry_required: "已有命名请求；新请求必须明确重试，且旧项已拒绝、过期或未解。",
  naming_limit: "命名辅助达到限额：最多32文件、16来源标题，每任务最多3份请求。仍可核对人工整理。",
  naming_rules_available: "已有明确规则可解，请刷新任务或核对命名规则。",
  naming_unresolved: "缺少明确命名依据；请补充人工事实或填写完整整理表。",
  naming_preview_required: "请使用当前通道重新完整预览后批准；建议审核与独立人工整理的摘要不能混用。",
  naming_replay_conflict: "已有不同结果，请重新读取建议，不会覆盖原始证据。",
  stale_plan: "未开始计划已过期：返回任务重新规划；重试不会改动旧计划。",
};
function el(tag, text, cls) {
  const n = document.createElement(tag);
  if (text !== undefined) n.textContent = String(text);
  if (cls) n.className = cls;
  return n;
}
// 共享表现组件只创建安全 DOM；不持有业务版本、批准状态或请求摘要。
function sectionLead(kicker, title, description) {
  const n = el("div", undefined, "section-lead");
  n.append(el("span", kicker, "eyebrow"), el("h2", title));
  if (description) n.append(el("p", description, "muted"));
  return n;
}
function loadingState() {
  const n = el("div", undefined, "loading-state");
  n.append(el("p", "正在读取本页状态…", "loading-label"));
  const shapes = el("div", undefined, "loading-shapes");
  shapes.setAttribute("aria-hidden", "true");
  for (let i = 0; i < 4; i++) shapes.append(el("span"));
  n.append(shapes);
  return n;
}
function setReadStatus(text, state) {
  $("#read-status").textContent = text;
  $("#read-status").dataset.state = state;
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
  message.setAttribute("role", "alert");
  message.textContent = errors[err.message] || `操作未完成：${err.message}`;
  message.focus({ preventScroll: true });
  message.scrollIntoView({ block: "nearest", behavior: "instant" });
}
async function api(path, data, signal) {
  if (sessionLocked && !["session", "login"].includes(path)) throw new Error("authentication_required");
  const epoch = authEpoch, controller = new AbortController();
  const abort = () => controller.abort();
  if (signal?.aborted) controller.abort();
  signal?.addEventListener("abort", abort, { once: true });
  pendingRequests.add(controller);
  try {
    let r;
    try {
      r = await fetch("/api/" + path, {
        method: data === undefined ? "GET" : "POST",
        headers: data === undefined ? {} : {
          "Content-Type": "application/json",
          ...(path === "login" ? {} : { "X-CSRF-Nonce": csrf }),
        },
        body: data === undefined ? undefined : JSON.stringify(data),
        credentials: "same-origin", signal: controller.signal,
      });
    } catch (e) {
      if (e.name === "AbortError") throw new Error("library_cancelled");
      throw new Error("本地服务暂时不可达，请刷新重试。");
    }
    let v;
    try { v = await r.json(); } catch { throw new Error("服务返回了无法读取的响应。"); }
    // 旧会话的迟到响应不能覆盖新登录，也不能恢复任何预览授权。
    if (epoch !== authEpoch) throw new Error("authentication_required");
    if (!r.ok) {
      if (r.status === 401 && v.error === "authentication_required") lockSession(path !== "logout");
      const error = new Error(v.error || "operation_failed");
      const retry = r.headers.get("Retry-After");
      if (r.status === 429 && /^\d+$/.test(retry || "")) error.retryAfter = Math.max(1, Number(retry));
      throw error;
    }
    return v;
  } finally {
    pendingRequests.delete(controller);
    signal?.removeEventListener("abort", abort);
  }
}
// 仅在当前页面内存保留显式注册的非秘密编辑器。DOM/预览摘要/勾选授权绝不持久化。
function draftable(node, key, capture, restore) {
  node.dataset.draftKey = key;
  node.draftAccess = { capture, restore };
}
function draftIdentity() { return JSON.stringify([tab, detailID, proposalSelection, subscriptionCursor]); }
function captureDraft() {
  const entries = [...view.querySelectorAll("[data-draft-key]")].map(n => [n.dataset.draftKey, n.draftAccess.capture()]);
  return entries.length ? { identity: draftIdentity(), entries } : null;
}
function draftFields(node) {
  return [...node.querySelectorAll("input[name], select[name], textarea[name]")]
    .filter(n => !["checkbox", "password", "hidden"].includes(n.type) && !n.disabled)
    .map(n => [n.name, n.value]);
}
function restoreFields(node, fields) {
  const available = [...node.querySelectorAll("[name]")];
  if (fields.some(([name, value]) => {
    const input = available.find(n => n.name === name && !n.disabled);
    return !input || ["checkbox", "password"].includes(input.type) || input.tagName === "SELECT" && ![...input.options].some(o => o.value === value);
  })) return false;
  for (const [name, value] of fields) {
    const input = [...node.querySelectorAll("[name]")].find(n => n.name === name && !n.disabled);
    if (!input || input.type === "checkbox" || input.type === "password") return false;
    if (input.tagName === "SELECT" && ![...input.options].some(o => o.value === value)) return false;
    input.value = value;
    input.dispatchEvent(new Event("input", { bubbles: true }));
  }
}
function restoreSessionDraft() {
  const saved = sessionDraft; sessionDraft = null;
  if (!saved) return;
  const unrestored = [];
  for (const [key, data] of saved.entries) {
    const target = saved.identity === draftIdentity() && [...view.querySelectorAll("[data-draft-key]")].find(n => n.dataset.draftKey === key);
    if (!target || target.draftAccess.restore(data) === false) unrestored.push([key, data]);
  }
  if (unrestored.length) {
    const fallback = panel("未自动恢复的草稿");
    fallback.append(el("p", "任务、来源或编辑通道已变化。以下仅保留原输入供人工核对，未载入当前方案。", "notice"), el("pre", JSON.stringify(unrestored, null, 2)));
    draftable(fallback, "unrestored", () => unrestored, () => false);
    view.prepend(fallback);
  }
  message.className = "notice";
  message.setAttribute("role", "status");
  message.textContent = "已重新读取当前状态。非秘密草稿已恢复或保留在下方；原预览、批准、范围声明与排序勾选已清除。请核对后重新预览，未自动重发任何操作。";
}
function acceptSession(value) {
  sessionMode = value.mode || "local_single_user";
  if (!["local_single_user", "authenticated_https"].includes(sessionMode)) throw new Error("未知的管理访问模式，请核对服务版本。");
  if (sessionMode === "authenticated_https" && value.authenticated === false) { lockSession(false); return false; }
  if (!value.csrf || value.authenticated === false || sessionMode === "authenticated_https" && value.authenticated !== true) throw new Error("会话响应不完整，请重新读取。");
  csrf = value.csrf; sessionLocked = false;
  $("#session-gate").hidden = true; $("#session-gate").replaceChildren();
  $("#workspace-shell").hidden = false; $(".skip-link").hidden = false;
  $("#logout").hidden = sessionMode !== "authenticated_https";
  // 标识访问模式，不把认证成功等同于任何上游连接成功。
  $(".local").replaceChildren(el("span", "", "dot"), document.createTextNode(sessionMode === "authenticated_https" ? "已认证管理会话" : "本机管理工作台"), el("small", "源文件保留 · 人工始终可继续"));
  return true;
}
function lockSession(preserve = true) {
  if (sessionLocked) return;
  sessionDraft = preserve ? captureDraft() || loadingDraft : null;
  loadingDraft = null;
  csrf = ""; sessionLocked = true; sessionMode = "authenticated_https";
  authEpoch++; generation++;
  activeLibraryScan?.abort(); activeLibraryScan = null;
  for (const request of pendingRequests) request.abort();
  // 清掉旧组件持有的授权，不只隐藏 DOM；即使保留旧节点引用也不能沿用批准。
  view.querySelectorAll('input[type="checkbox"]').forEach(n => { n.checked = false; n.dispatchEvent(new Event("change", { bubbles: true })); });
  view.querySelectorAll("[data-draft-key]").forEach(n => n.dispatchEvent(new Event("reset", { bubbles: true })));
  view.querySelectorAll("button, input, select, textarea").forEach(n => { n.disabled = true; });
  view.replaceChildren(); view.setAttribute("aria-busy", "false");
  setBusy(false);
  $("#workspace-shell").hidden = true; $(".skip-link").hidden = true;
  renderLogin(preserve);
}
function renderLogin(expired) {
  const gate = $("#session-gate"); gate.hidden = false; gate.replaceChildren();
  document.title = "登录 · asuka 番剧工作台";
  const surface = el("div", undefined, "login-surface"), story = el("div", undefined, "login-story");
  story.append(el("div", "ASUKA / YOUR LOCAL MEDIA ARCHIVE", "eyebrow"), el("h2", "热爱的故事，\n安心收藏。"), $("#landscape-art").content.cloneNode(true), el("p", "下载与整理继续由 NAS 处理。\n登录后，回到你的番剧工作台。"));
  const form = el("form", undefined, "login-form"), heading = el("h1", expired ? "会话已结束" : "欢迎回到 asuka");
  heading.id = "session-title"; heading.tabIndex = -1;
  form.append(el("div", "SECURE WORKSPACE", "eyebrow"), heading,
    el("p", expired ? "请重新登录。当前页面的非秘密草稿只保留在内存，关闭或刷新页面会丢失。" : "使用部署者为此工作台配置的独立账号。", "muted"));
  const user = field(form, "用户名", "login_username"); user.autocomplete = "username"; user.required = true; user.maxLength = 64;
  const password = field(form, "密码", "login_password", "", "password"); password.autocomplete = "current-password"; password.required = true;
  const status = el("p", undefined, "login-status"); status.id = "login-status"; status.setAttribute("role", "status");
  password.setAttribute("aria-describedby", status.id);
  const submit = button("登录工作台", () => {}, true); submit.type = "submit";
  form.append(status, submit, el("p", "请使用部署者提供的可信 HTTPS 地址。此页面不提供注册或重设密码。", "login-help"));
  if (expired) form.append(el("p", "已发出的操作可能已被服务器接收。登录后先核对持久状态；旧预览和批准均需重新确认。", "notice"));
  let submitting = false, retryAt = 0;
  form.onsubmit = async e => {
    e.preventDefault();
    if (submitting || Date.now() < retryAt) return;
    submitting = true; submit.disabled = true; user.disabled = true; password.disabled = true;
    password.removeAttribute("aria-invalid"); status.className = "login-status muted"; status.setAttribute("role", "status"); status.textContent = "正在验证会话…";
    const epoch = authEpoch;
    try {
      const value = await api("login", { username: user.value, password: password.value });
      password.value = "";
      if (epoch !== authEpoch || !gate.isConnected) return;
      if (!acceptSession(value)) return;
      authEpoch++;
      syncNavigation(); focusAfterLoad = "#heading";
      await load();
    } catch (error) {
      if (epoch !== authEpoch) return;
      status.className = "login-status error"; status.setAttribute("role", "alert");
      status.textContent = errors[error.message] || "登录未完成，请核对服务后手动重试。";
      if (error.retryAfter) {
        retryAt = Date.now() + error.retryAfter * 1000;
        status.textContent += ` 请至少等待 ${error.retryAfter} 秒后手动重试。`;
        setTimeout(() => { if (epoch === authEpoch && form.isConnected) submit.disabled = false; }, Math.min(error.retryAfter * 1000, 2147483647));
      }
      if (error.message === "authentication_failed") password.setAttribute("aria-invalid", "true");
      password.disabled = false; password.focus();
    } finally {
      password.value = ""; submitting = false; user.disabled = false; password.disabled = false;
      submit.disabled = Date.now() < retryAt;
    }
  };
  surface.append(story, form); gate.append(surface);
  if (expired) heading.focus(); else user.focus();
}

function setBusy(v) {
  busy = v;
  $("#logout").disabled = v;
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
      message.setAttribute("role", "status");
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
    thead = el("thead"),
    body = el("tbody");
  headers.forEach((h) => { const th = el("th", h); th.scope = "col"; head.append(th); });
  thead.append(head);
  t.append(thead, body);
  wrap.append(t);
  return { wrap, body };
}
function row(body, values) {
  const r = el("tr");
  values.forEach((v, i) => {
    const c = el("td");
    c.dataset.label = body.parentElement.querySelectorAll("th")[i]?.textContent || "";
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
  if (sessionLocked) return;
  loadingDraft = captureDraft();
  const g = ++generation, requestedFocus = focusAfterLoad;
  focusAfterLoad = null;
  setReadStatus("正在读取本页…", "loading");
  if (clear) {
    message.textContent = "";
    message.className = "";
    message.setAttribute("role", "status");
    view.replaceChildren(loadingState());
  }
  view.setAttribute("aria-busy", "true");
  try {
    if (!csrf && !acceptSession(await api("session"))) return;
    const content = await {
      overview: overviewView,
      jobs: jobsView,
      subscriptions: subscriptionsView,
      rules: rulesView,
      events: eventsView,
      health: healthView,
    }[tab]();
    if (g !== generation) return;
    view.replaceChildren(content);
    loadingDraft = null;
    restoreSessionDraft();
    setReadStatus("本页读取于 " + new Date().toLocaleTimeString("zh-CN", {hour12: false}), "read");
    view.querySelector("[data-review-heading]")?.focus();
    if (requestedFocus) {
      const target = typeof requestedFocus === "function" ? requestedFocus() : document.querySelector(requestedFocus);
      target?.focus({ preventScroll: true });
      if (target?.id === "heading") window.scrollTo({ top: 0, behavior: "instant" });
      else target?.scrollIntoView({ block: "nearest", behavior: "instant" });
    }
    if (detailSection) jumpTo(detailSection);
    setBusy(busy);
  } catch (e) {
    if (g === generation) {
      setReadStatus("本页读取失败 · 可重试", "error");
      const recovery = panel("暂时无法读取");
      recovery.classList.add("load-error");
      recovery.append(el("p", "数据尚未读取成功。可重试；当前页面入口会保留。", "muted"), button("重新加载", () => load(), true));
      view.replaceChildren(recovery);
      fail(e);
    }
  } finally {
    if (g === generation) view.setAttribute("aria-busy", "false");
  }
}
function syncNavigation() {
  $("#heading").textContent = names[tab];
  $("#page-description").textContent = descriptions[tab];
  document.title = names[tab] + " · asuka 番剧工作台";
  document.querySelectorAll("[data-tab]").forEach((b) => {
    b.classList.toggle("selected", b.dataset.tab === tab);
    if (b.dataset.tab === tab) b.setAttribute("aria-current", "page");
    else b.removeAttribute("aria-current");
  });
}
function saveRoute() {
  const params = new URLSearchParams();
  if (tab === "jobs") {
    if (detailID) params.set("id", detailID);
    if (detailSection) params.set("section", detailSection);
    if (cursor) params.set("after", cursor);
    if (jobFilter !== "all") params.set("filter", jobFilter);
    if (jobQuery) params.set("q", jobQuery);
  }
  const route = "#" + tab + (params.size ? "?" + params : "");
  if (location.hash !== route) history.pushState(null, "", route);
}
function readRoute() {
  const [name, query] = location.hash.slice(1).split("?");
  tab = names[name] ? name : "overview";
  const params = new URLSearchParams(query || "");
  detailID = tab === "jobs" ? params.get("id") || "" : "";
  cursor = params.get("after") || "";
  jobFilter = ["all", "downloading", "naming", "emby", "failed", "ready"].includes(params.get("filter")) ? params.get("filter") : "all";
  jobQuery = params.get("q") || "";
  detailSection = params.get("section") || "";
  detailReturn = "jobs";
  proposalSelection = null;
  syncNavigation();
}
function navigate(next, filter = "all") {
  tab = next; detailID = ""; detailSection = ""; cursor = "";
  jobFilter = filter; jobQuery = "";
  subscriptionCursor = ""; proposalSelection = null;
  focusAfterLoad = "#heading";
  syncNavigation(); saveRoute(); load();
}
function openJob(id, section = "") {
  detailReturn = tab === "overview" ? "overview" : "jobs";
  tab = "jobs"; detailID = id; detailSection = section;
  focusAfterLoad = "#heading";
  syncNavigation(); saveRoute(); load();
}
function jumpTo(id) {
  const target = document.getElementById("job-" + id);
  if (!target) {
    message.className = "notice";
    message.textContent = "此任务在当前阶段暂无该入口的数据，请核对任务详情。";
    return;
  }
  target.tabIndex = -1;
  target.focus({ preventScroll: true });
  target.scrollIntoView({ block: "start", behavior: "instant" });
}
window.addEventListener("popstate", () => { if (sessionLocked) return; readRoute(); focusAfterLoad = "#heading"; load(); });
// 离开页面后只提示快照可能陈旧，不自动刷新并清掉人工草稿，也不触发上游探测。
document.addEventListener("visibilitychange", () => {
  if (!document.hidden && $("#read-status").dataset.state === "read") {
    setReadStatus("离开期间未刷新 · 状态可能已变化", "stale");
  }
});
$(".skip-link").onclick = (e) => { e.preventDefault(); $("#heading").focus(); };
document.querySelectorAll("[data-tab]").forEach((b) => (b.onclick = () => navigate(b.dataset.tab)));
$("#refresh").onclick = () => load();
$("#logout").onclick = async () => {
  if (busy || sessionMode !== "authenticated_https") return;
  setBusy(true);
  try { await api("logout", {}); lockSession(false); }
  catch (error) { if (!sessionLocked) fail(error); }
  finally { setBusy(false); }
};
function scopeText(scope) {
  if (!scope) return "统计范围由接口返回；当前未提供全库计数。";
  return scope.truncated
    ? `全库 ${scope.total} 个任务；工作台仅覆盖按 ID 排序的前 ${scope.scanned} 个（上限 ${scope.limit}），搜索与筛选也限此范围。`
    : `覆盖全库 ${scope.total} 个任务；只读状态快照，刷新后重新读取。`;
}
const workflowFilters = [
  ["all", "全部任务"], ["downloading", "下载中 / 等待下载"],
  ["naming", "待命名 / 待补事实"], ["emby", "已入库 · Emby 未发现"],
  ["failed", "失败 / 待恢复"], ["ready", "媒体已就绪"],
];
async function overviewView() {
  const data = await api("workflow"), root = el("div");
  const intro = el("section", undefined, "workflow-hero");
  const copy = el("div", undefined, "hero-copy");
  copy.append(el("div", "YOUR COLLECTION, YOUR CONSTELLATION", "eyebrow"),
    el("h2", "让每一集，\n都有归处。"),
    el("p", "从追番到收藏，留住热爱的每一帧。\n规则优先，人工把关；每一步都清楚可循。", "hero-line"));
  const heroActions = el("div", undefined, "actions");
  heroActions.append(button("打开全部任务", () => navigate("jobs"), true), button("管理订阅", () => navigate("subscriptions")));
  copy.append(heroActions);
  const art = el("div", undefined, "hero-art");
  art.setAttribute("aria-hidden", "true");
  art.append($("#landscape-art").content.cloneNode(true), el("span", "明日も、好きな物語を。", "art-caption"));
  intro.append(copy, art);
  root.append(intro, sectionLead("01 / TODAY’S WORKSPACE", "把今天要处理的事放在一起"), el("p", scopeText(data.scope), "scope-note"));
  const metrics = el("div", undefined, "workflow-metrics");
  const steps = [
    ["downloading", "下载中 / 等待下载", "查看下载状态；速度和进度未提供"],
    ["naming", "待命名 / 待补事实", "补充明确季号，或直接人工整理"],
    ["rules", "待审规则版本", "核对原始人工依据，再试算与终审"],
    ["emby", "入库后待发现", "硬链接成功后，继续核对 Emby"],
    ["failed", "失败 / 待恢复", "先核对错误，恢复原冻结方案"],
  ];
  steps.forEach(([key, label, hint], i) => {
    const n = button("", () => navigate(key === "rules" ? "rules" : "jobs", key === "rules" ? "all" : key));
    n.className = "workflow-metric"; n.dataset.workflow = key;
    n.append(el("span", ["↓", "✦", "⌘", "◈", "↺"][i], "metric-symbol"), el("span", "0" + (i + 1), "step-index"), el("strong", data.counts[key]), el("span", label, "metric-label"), el("small", hint), el("span", "查看 →", "metric-link"));
    metrics.append(n);
  });
  root.append(metrics, el("p", "任务分类可能重叠：例如待命名任务也可能有错误。待审规则为全库持久 pending 版本数，进入详情后重新核验是否过期。", "muted"));
  const next = panel(data.counts.all ? "继续处理" : "从第一条订阅开始");
  next.append(el("p", data.counts.all ? `已发现 ${data.counts.ready} 个媒体就绪任务。选择上方待办，进入任务核对依据。` : "当前没有任务。先在部署配置中添加来源档案，再在订阅页选择来源和启用抓取。", "muted"));
  const actions = el("div", undefined, "actions");
  actions.append(button("查看就绪任务", () => navigate("jobs", "ready"), true), button("查看接入步骤", () => navigate("health")));
  next.append(actions);
  const journey = panel("从追番，到收藏");
  journey.classList.add("journey-panel");
  journey.append(el("p", "流程说明 · 不代表当前任务的执行进度", "muted"));
  const stages = el("ol", undefined, "journey");
  [["RSS 订阅", "读取来源"], ["qB 下载", "源文件做种"], ["规则 / 人工", "确认命名"], ["硬链接", "不覆盖源文件"], ["Emby 发现", "逐文件核对"]].forEach(([title, hint], i) => {
    const stage = el("li"); stage.append(el("span", String(i + 1).padStart(2, "0"), "step-index"), el("strong", title), el("small", hint)); stages.append(stage);
  });
  journey.append(stages, el("p", "普通状态通知由 gateway 固定发送，零模型调用。命名难题可交给 agent，复用规则仍须人工终审。", "journey-note"));
  root.append(journey, next);
  return root;
}
async function jobsView() {
  if (detailID) return jobView(detailID);
  const data = await api("jobs?" + new URLSearchParams({after: cursor, filter: jobFilter, q: jobQuery}));
  const root = el("div"), p = panel("任务进度");
  p.classList.add("task-workspace");
  p.append(el("p", scopeText(data.scope), "scope-note"));
  const filters = el("form", undefined, "task-filters");
  const search = field(filters, "搜索任务名 / 来源摘要 / ID", "job_search", jobQuery, "search");
  search.maxLength = 256;
  search.placeholder = "例如：作品名、S01E02";
  const filter = select(filters, "处理阶段", "job_filter", workflowFilters, jobFilter);
  const apply = button("应用筛选", () => {} , true); apply.type = "submit";
  filters.append(apply);
  function applyFilters() {
    jobQuery = search.value.trim(); jobFilter = filter.value; cursor = "";
    focusAfterLoad = "[name=job_search]";
    saveRoute(); load();
  }
  filters.onsubmit = (e) => { e.preventDefault(); applyFilters(); };
  filter.onchange = applyFilters;
  const results = el("p", `本页 ${data.items.length} 条${data.matched === undefined ? "" : ` · 范围内匹配 ${data.matched} 条`} · 按任务 ID 排序，每页最多 100 条`, "results-count muted");
  results.id = "task-results"; results.tabIndex = -1;
  results.setAttribute("role", "status");
  p.append(filters, results);
  const list = el("div", undefined, "task-list");
  data.items.forEach((v) => {
    const j = v.job, card = el("article", undefined, "task-card"), title = el("div", undefined, "task-identity");
    card.dataset.jobId = j.id;
    card.dataset.tone = j.error_code ? "error" : workflowTaskNeedsNaming(j) ? "attention" : v.ready ? "ready" : "pending";
    title.append(el("h3", v.summary || "名称未知"), el("p", `${v.summary_kind || "尚无来源摘要"} · ${v.episode || "季集未知 · 需明确依据"}`, "muted"), el("small", j.id, "mono"));
    const state = el("div", undefined, "task-states");
    [["下载", j.download], ["入库", j.import], ["Emby", v.ready ? "已就绪" : j.discovery], ["做种", j.seed]].forEach(([label, value]) => {
      const cell = el("div"); cell.append(el("span", label, "state-label"), badge(value, (label === "Emby" && v.ready) || ["completed", "imported", "stopped"].includes(value) ? "ok" : j.block_reason && label === "入库" ? "warn" : "")); state.append(cell);
    });
    const actions = el("div", undefined, "task-actions");
    actions.append(el("p", v.next_step || "查看证据与当前方案", "next-step"), button("查看详情", () => openJob(j.id), true));
    if (workflowTaskNeedsNaming(j)) actions.append(button("命名 / 人工整理", () => openJob(j.id, "naming")));
    if (j.error_code) actions.append(button("安全恢复入口", () => openJob(j.id, "recovery")));
    card.append(title, state, actions);
    if (j.block_reason || j.error_code) card.append(el("p", [j.block_reason, j.error_code].filter(Boolean).join(" · "), "task-error mono"));
    list.append(card);
  });
  if (!data.items.length) {
    const empty = el("div", undefined, "empty");
    empty.append(el("h3", jobQuery || jobFilter !== "all" || cursor ? "没有符合条件的任务" : "暂无任务"), el("p", jobQuery || jobFilter !== "all" || cursor ? "调整搜索与阶段，或返回第一页重新读取。" : "启用订阅后等待轮询；来源配置与连接状态可在接入页核对。"));
    empty.append(button("清除筛选并返回首页", () => { cursor = ""; jobQuery = ""; jobFilter = "all"; saveRoute(); load(); }), button("管理订阅", () => navigate("subscriptions")));
    list.append(empty);
  }
  p.append(list);
  const actions = el("div", undefined, "actions");
  if (cursor) actions.append(button("返回首页", () => { cursor = ""; saveRoute(); load(); }));
  if (data.next) actions.append(button("下一页", () => { cursor = data.next; focusAfterLoad = "#task-results"; saveRoute(); load(); }));
  p.append(actions); root.append(p); return root;
}
function workflowTaskNeedsNaming(j) {
  return j.download === "completed" && j.import !== "imported" && (j.import === "needs_confirmation" || j.block_reason);
}
async function jobView(id) {
  const v = await api("jobs/" + encodeURIComponent(id)),
    j = v.job,
    root = el("div");
  root.append(
    button("← 返回任务", () => {
      const returningID = detailID;
      detailID = ""; detailSection = ""; tab = detailReturn;
      focusAfterLoad = () => [...view.querySelectorAll(".task-card")].find(card => card.dataset.jobId === returningID)?.querySelector("button") || $("#heading");
      syncNavigation(); saveRoute(); load();
    }),
  );
  const p = panel("任务详情");
  p.id = "job-recovery";
  p.append(el("p", "来源摘要：" + (j.sources?.[0] || v.contexts?.[0]?.title || "名称未知"), "detail-name"), el("p", j.id, "mono"));
  const jumps = el("div", undefined, "actions detail-jumps");
  [["naming", "命名 / 人工整理"], ["evidence", "来源证据"], ["plan", "文件方案"], ["comparison", "对比与审计"], ["recovery", "安全恢复"]].forEach(([id, label]) => { const b = button(label, () => jumpTo(id)); b.dataset.jobSection = id; jumps.append(b); });
  jumps.setAttribute("role", "group"); jumps.setAttribute("aria-label", "任务详情分区");
  root.append(jumps);
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
  const retry = writeButton(j.plan?.started ? "恢复原冻结方案" : "安全重试", () => action("retry"));
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
  if (j.download === "completed" && !permanent) {
    const naming = await namingPanel(j, v); naming.id = "job-naming"; root.append(naming);
  }
  if (v.contexts?.length) {
    const evidence = panel("来源证据（最多 100 条）");
    evidence.id = "job-evidence";
    v.contexts.forEach((c) => {
      evidence.append(el("h3", c.subscription), el("p", c.title, "mono"));
    });
    root.append(evidence);
  }
  const plan = panel("文件组织方案");
  plan.id = "job-plan";
  if (j.plan) {
    plan.append(
      el(
        "p",
        `版本 ${j.plan.version} · ${j.plan.started ? "已冻结执行" : j.plan.approved ? "已批准" : "待确认"} · ${j.plan.collection ? "整包方案" : "单集方案"}`,
        "muted",
      ),
    );
    const t = comparisonTable(["源文件 → 目标", "指纹", "执行 / 发现"]);
    j.plan.files.forEach((f, i) => {
      const state = v.files?.find((x) => x.Index === i);
      fileRow(t.body, [
        filePair(f.source, f.target),
        el("span", f.fingerprint, "mono"),
        (states[state?.State] || state?.State || "等待") +
          (state?.Video
            ? " / " + (state.Discovered ? "已发现" : "未发现")
            : " / 旁车"),
      ], ["源文件 → 目标", "指纹", "执行 / 发现"]);
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
      el("p", "尚未生成可执行方案。可使用下方批量整理明确本次完整清单。", "muted"),
    );
  root.append(plan);
  const comparison = comparisonPanel(id); comparison.id = "job-comparison"; root.append(comparison);
  if (
    j.download === "completed" &&
    j.import !== "imported" &&
    !j.plan?.started &&
    !permanent && (j.sources || []).filter(x => /\.(mkv|mp4)$/i.test(x)).length === 1
  ) {
    const form = panel("人工单集预览");
    form.append(
      el(
        "p",
        "仅完整单视频清单；字幕随主视频绑定。合集使用批量整理明确全部视频，不支持挑选一集绕过整包检查。",
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
    draftable(form, "single-file", () => draftFields(form), saved => restoreFields(form, saved));
    form.append(
      writeButton(
        "生成文件预览",
        () => action("preview", { source: source.value, target: target.value }),
        true,
      ),
    );
    root.append(form);
  }
  jumps.querySelectorAll("[data-job-section]").forEach((b) => {
    if (!root.querySelector("#job-" + b.dataset.jobSection)) unavailable(b, "此任务当前没有此阶段的数据");
  });
  return root;
}
// 同一静态入口内按职责拆分；管理服务的embed/CSP路径保持不变。
const isBatchVideo = (source) => /\.(mkv|mp4)$/i.test(source);
const padEpisode = (number) => String(number).padStart(2, "0");
function batchTarget(source, organization, episode) {
  if (
    !organization.directory ||
    !organization.display_name ||
    !organization.season ||
    !episode
  )
    return "填写目录、显示名和明确季集后显示拟定路径";
  return `${organization.directory}/Season ${padEpisode(organization.season)}/${organization.display_name} - S${padEpisode(organization.season)}E${padEpisode(episode)}${source.slice(source.lastIndexOf("."))}`;
}
function recognitionText(hint) {
  if (hint.problem) return errors[hint.problem] || hint.problem;
  return `季 ${hint.season || "未声明"} · 相对集 ${hint.relative || "未声明"} · 总集 ${hint.absolute || "未声明"}`;
}
function comparisonTable(headers) {
  const result = table(headers);
  result.wrap.classList.add("file-table");
  result.body.parentElement
    .querySelectorAll("th")
    .forEach((h) => (h.scope = "col"));
  return result;
}
function fileRow(body, values, labels) {
  row(body, values);
  [...body.lastElementChild.children].forEach(
    (cell, i) => (cell.dataset.label = labels[i]),
  );
}
function filePair(source, target) {
  const pair = el("div", undefined, "file-pair");
  pair.append(
    el("span", source, "mono"),
    el("span", " → " + target, "mono target-name"),
  );
  return pair;
}
function batchSteps(current) {
  const steps = el("ol", undefined, "batch-steps");
  steps.setAttribute("aria-label", "整理流程");
  ["选择任务", "批量编辑", "完整预览", "确认方案", "执行 / 对比"].forEach(
    (label, i) => {
      const step = el("li", `${i + 1} · ${label}`);
      if (i === current) step.setAttribute("aria-current", "step");
      steps.append(step);
    },
  );
  return steps;
}

// 请求代数与原始DOM快照双重绑定：静默赋值也不能复用旧预览。
class ReviewedRequestGate {
  constructor(snapshot) {
    this.snapshot = snapshot;
    this.revision = 0;
    this.accepted = null;
  }
  invalidate() {
    this.revision++;
    this.accepted = null;
  }
  begin() {
    const version = this.revision,
      g = generation,
      snapshot = this.snapshot();
    return {
      snapshot,
      owns: () =>
        version === this.revision &&
        g === generation &&
        snapshot === this.snapshot(),
    };
  }
  accept(ticket, request, result) {
    if (!ticket.owns()) return false;
    this.accepted = { snapshot: ticket.snapshot, request, ...result };
    return true;
  }
  reviewed() {
    return this.accepted?.snapshot === this.snapshot() ? this.accepted : null;
  }
}

class BatchEditor {
  constructor(job, detail, changed) {
    this.job = job;
    this.hints = new Map((detail.batch_hints || []).map((h) => [h.source, h]));
    // 保留下载器顺序，绝不在前端按字典序隐式分配集数。
    this.rows = (job.sources || [])
      .filter(isBatchVideo)
      .map((source) => ({ source, episode: "" }));
    this.changed = changed;
    this.node = el("div", undefined, "batch-editor");
    const grid = el("div", undefined, "grid batch-fields");
    this.directory = field(grid, "媒体库相对目录", "batch_directory");
    this.directory.placeholder = "例如：Example";
    this.display = field(grid, "媒体库显示名称", "batch_display_name");
    this.season = field(
      grid,
      "本次目标季号 · 1–99",
      "batch_season",
      "",
      "number",
    );
    this.mode = select(grid, "编号方式", "batch_mode", [
      ["manual", "手动 · 逐行填写"],
      ["extracted", "提取编号 + 偏移"],
      ["sequential", "按本表顺序连续编号"],
    ]);
    this.offset = field(
      grid,
      "提取编号偏移 · -998 至 998",
      "batch_offset",
      0,
      "number",
    );
    this.start = field(
      grid,
      "连续编号首集 · 1–999",
      "batch_start",
      1,
      "number",
    );
    [
      [this.season, 1, 99],
      [this.offset, -998, 998],
      [this.start, 1, 999],
    ].forEach(([n, min, max]) => {
      n.min = min;
      n.max = max;
      n.step = 1;
    });
    this.modeHelp = el("p", undefined, "muted");
    this.modeHelp.id = "batch-mode-help";
    this.mode.setAttribute("aria-describedby", this.modeHelp.id);
    this.numbering = button("提取 / 展示连续编号", () => this.assignNumbers());
    this.order = el("input");
    this.order.type = "checkbox";
    this.order.name = "batch_order_confirmed";
    this.orderLabel = el("label", undefined, "order-approval");
    this.orderLabel.append(
      this.order,
      el(
        "span",
        "我已逐行核对本表顺序与连续编号；该顺序代表本次集序。调整顺序或编号后需再次确认。",
      ),
    );
    this.order.addEventListener("change", () => {
      this.orderSnapshot = this.order.checked ? this.orderSignature() : null;
      this.changed();
    });
    this.table = comparisonTable([
      "原名 / 识别信息",
      "目标集号",
      "拟定视频路径",
      "调整行序",
    ]);
    this.node.append(
      grid,
      this.modeHelp,
      this.numbering,
      this.table.wrap,
      this.orderLabel,
    );
    [
      this.directory,
      this.display,
      this.season,
      this.mode,
      this.offset,
      this.start,
    ].forEach((n) => {
      for (const event of ["input", "change"])
        n.addEventListener(event, () => {
          // input 已记录当前值；稍后失焦的同值 change 不抹掉已审核快照。
          if (event === "change" && this.orderSignature() === this.lastInputSignature) return;
          this.clearOrder();
          this.changed();
          this.update();
        });
    });
    this.renderRows();
    this.update();
  }
  clearOrder() {
    this.order.checked = false;
    this.orderSnapshot = null;
  }
  rowValues() {
    return this.rows.map((r) => ({
      source: r.source,
      episode: r.input?.value ?? r.episode,
    }));
  }
  raw() {
    return {
      directory: this.directory.value,
      display: this.display.value,
      season: this.season.value,
      mode: this.mode.value,
      offset: this.offset.value,
      start: this.start.value,
      rows: this.rowValues(),
    };
  }
  orderSignature() {
    return JSON.stringify(this.raw());
  }
  snapshot() {
    return JSON.stringify({
      ...this.raw(),
      checked: this.order.checked,
      orderSnapshot: this.orderSnapshot,
    });
  }
  request() {
    return {
      id: this.job.id,
      version: this.job.version,
      organization: {
        directory: this.directory.value,
        display_name: this.display.value,
        season: Number(this.season.value),
        mode: this.mode.value,
        offset: this.mode.value === "extracted" ? Number(this.offset.value) : 0,
        start: this.mode.value === "sequential" ? Number(this.start.value) : 0,
        order_confirmed:
          this.mode.value === "sequential" &&
          this.order.checked &&
          this.orderSnapshot === this.orderSignature(),
        rows: this.rowValues().map((r) => ({
          source: r.source,
          episode: Number(r.episode),
        })),
      },
    };
  }
  assignNumbers() {
    if (this.mode.value === "manual") return;
    this.rows.forEach((r, i) => {
      const h = this.hints.get(r.source) || {};
      r.input.value =
        this.mode.value === "sequential"
          ? Number(this.start.value) + i
          : !h.problem &&
              !(h.relative && h.absolute) &&
              (h.relative || h.absolute)
            ? Number(h.relative || h.absolute) + Number(this.offset.value)
            : "";
    });
    this.clearOrder();
    this.changed();
    this.update();
  }
  move(index, delta) {
    const next = index + delta;
    if (next < 0 || next >= this.rows.length) return;
    this.rows.forEach((r) => (r.episode = r.input.value));
    const moved = this.rows[index];
    [this.rows[index], this.rows[next]] = [this.rows[next], this.rows[index]];
    this.clearOrder();
    this.changed();
    this.renderRows();
    this.update();
    moved.actions
      .querySelector(delta > 0 ? "button:first-child" : "button:last-child")
      .focus();
  }
  renderRows() {
    this.table.body.replaceChildren();
    this.rows.forEach((r, i) => {
      const info = el("div");
      info.append(
        el("p", `${i + 1}. ${r.source}`, "mono source-name"),
        el("p", recognitionText(this.hints.get(r.source) || {}), "muted"),
      );
      r.input = el("input");
      r.input.type = "number";
      r.input.name = "batch_episode";
      r.input.min = 1;
      r.input.max = 999;
      r.input.step = 1;
      r.input.value = r.episode;
      r.input.setAttribute("aria-label", r.source + " 目标集号");
      r.target = el("span", undefined, "mono target-name");
      r.actions = el("div", undefined, "actions row-actions");
      for (const [label, delta] of [
        ["上移", -1],
        ["下移", 1],
      ]) {
        const b = button(label, () => this.move(i, delta));
        b.setAttribute("aria-label", `${label} ${r.source}`);
        b.disabled = i + delta < 0 || i + delta >= this.rows.length;
        r.actions.append(b);
      }
      for (const event of ["input", "change"])
        r.input.addEventListener(event, () => {
          if (event === "change" && this.orderSignature() === this.lastInputSignature) return;
          this.clearOrder();
          this.changed();
          this.update();
        });
      fileRow(
        this.table.body,
        [info, r.input, r.target, r.actions],
        ["原名 / 识别信息", "目标集号", "拟定视频路径", "调整行序"],
      );
    });
  }
  update() {
    // 输入修改后移除上一轮字段错误；新预览仍由服务器完整校验。
    [this.directory, this.display, this.season, this.offset, this.start, this.order].forEach(n => n.removeAttribute("aria-invalid"));
    const mode = this.mode.value;
    this.offset.closest("label").hidden = mode !== "extracted";
    this.start.closest("label").hidden = mode !== "sequential";
    this.orderLabel.hidden = mode !== "sequential";
    this.numbering.hidden = mode === "manual";
    this.modeHelp.textContent =
      mode === "manual"
        ? "逐行填写本次集号；识别信息只作参考。"
        : mode === "extracted"
          ? "只使用源文件的明确编号。双编号或未知编号不能自动选择；修改偏移后请重新提取。"
          : "先用上移 / 下移确定集序，再展示连续编号，最后逐行核对并勾选排序确认。初始顺序不代表集序。";
    const organization = this.request().organization;
    this.rows.forEach((r) => {
      r.input.readOnly = mode !== "manual";
      r.target.textContent = batchTarget(
        r.source,
        organization,
        Number(r.input.value),
      );
      r.input.removeAttribute("aria-invalid");
    });
    this.lastInputSignature = this.orderSignature();
  }
  markError(code) {
    let focus;
    if (code === "order_confirmation_required") {
      this.clearOrder();
      focus = this.order;
    }
    if (code === "episode_out_of_bounds")
      focus = [
        this.season,
        this.offset,
        this.start,
        ...this.rows.map((r) => r.input),
      ].find(
        (n) => !n.closest("label")?.hidden && (!n.value || !n.validity.valid),
      );
    if (code === "duplicate_episode") {
      const seen = new Set();
      this.rows.forEach((r) => {
        const value = Number(r.input.value);
        if (seen.has(value)) {
          r.input.setAttribute("aria-invalid", "true");
          focus ||= r.input;
        }
        seen.add(value);
      });
    }
    if (code === "invalid_batch")
      focus = [this.directory, this.display, this.season].find(
        (n) => !n.value || !n.validity.valid,
      );
    if (focus) {
      focus.setAttribute("aria-invalid", "true");
      focus.focus();
    }
  }
}

function renderBatchPreview(result) {
  const root = el("div"),
    plan = result.plan;
  root.append(
    el("h3", "完整预览 · 请逐文件核对"),
    el(
      "p",
      `方案 v${plan.version} · 共 ${plan.manifest.length} 个源文件指纹 / ${plan.files.length} 个入库文件。预览不落盘；确认仅保存人工意图，执行及全部 Emby 发现仍待完成。`,
      "notice",
    ),
  );
  const t = comparisonTable(["下载原文件 → 媒体库硬链接", "类型 / 预览状态"]);
  plan.files.forEach((f) =>
    fileRow(
      t.body,
      [
        filePair(f.source, f.target),
        el(
          "span",
          `${isBatchVideo(f.source) ? "视频" : "关联字幕"} · 待确认`,
          "badge",
        ),
      ],
      ["原名 → 目标", "类型 / 预览状态"],
    ),
  );
  root.append(t.wrap);
  const auxiliary = plan.manifest.filter(
    (f) => !plan.files.some((p) => p.source === f.source),
  );
  if (auxiliary.length) {
    const details = el("details"),
      list = el("ul");
    details.append(
      el("summary", `${auxiliary.length} 个辅助文件 · 绑定指纹，不入库`),
    );
    auxiliary.forEach((f) => list.append(el("li", f.source, "mono")));
    details.append(list);
    root.append(details);
  }
  const details = el("details"),
    list = el("ul");
  details.append(el("summary", "查看完整源清单与指纹"));
  plan.manifest.forEach((f) =>
    list.append(el("li", `${f.source} · ${f.fingerprint}`, "mono")),
  );
  details.append(list);
  root.append(details);
  return root;
}

class BatchWorkbench {
  constructor(job, detail, assistance = null) {
    this.assistance = assistance;
    this.node = panel(assistance ? "编辑命名建议 · 完整任务" : "批量整理并入库");
    this.node.dataset.batch = "true";
    this.node.classList.add("batch-workbench");
    this.steps = batchSteps(1);
    this.node.append(
      this.steps,
      el(
        "p",
        "只整理本次下载任务。媒体库创建正确命名的硬链接，下载原文件保留做种；缺少命名不影响下载和做种。",
        "muted",
      ),
    );
    this.status = el("p", undefined, "batch-status muted");
    this.status.dataset.batchStatus = "true";
    this.status.id = "batch-status";
    this.status.setAttribute("role", "status");
    this.status.setAttribute("aria-live", "polite");
    this.output = el("div");
    this.output.dataset.batchPreview = "true";
    this.editor = new BatchEditor(job, detail, () => this.invalidate());
    this.gate = new ReviewedRequestGate(() => JSON.stringify([this.assistance?.id, this.assistance?.version, this.editor.snapshot()]));
    draftable(this.node, "batch", () => ({ channel: this.assistance?.id || "manual", values: this.editor.raw() }), saved => {
      if (saved.channel !== (this.assistance?.id || "manual")) return false;
      const raw = saved.values, editor = this.editor;
      if (raw.rows.length !== editor.rows.length || raw.rows.some(row => !editor.rows.some(r => r.source === row.source))) return false;
      editor.directory.value = raw.directory; editor.display.value = raw.display; editor.season.value = raw.season;
      editor.mode.value = raw.mode; editor.offset.value = raw.offset; editor.start.value = raw.start;
      editor.rows.sort((a, b) => raw.rows.findIndex(r => r.source === a.source) - raw.rows.findIndex(r => r.source === b.source));
      editor.rows.forEach(row => { row.episode = raw.rows.find(r => r.source === row.source).episode; });
      editor.clearOrder(); editor.renderRows(); editor.update(); this.invalidate();
    });
    this.preview = writeButton(
      "预览完整整理表",
      () => this.readPreview(),
      true,
    );
    this.confirm = writeButton(assistance ? "人工批准本次命名方案" : "确认整表方案", () => this.confirmPlan(), true);
    this.confirm.setAttribute("aria-describedby", "batch-status");
    unavailable(this.confirm, "先预览当前整表");
    this.cancel = button("取消本次预览", () => {
      this.invalidate("本次预览已取消；草稿保留，未确认方案不会执行。");
      this.editor.clearOrder();
      this.controller?.abort();
    });
    const actions = el("div", undefined, "actions batch-toolbar");
    actions.append(this.preview, this.cancel);
    this.confirmArea = el("div", undefined, "actions batch-confirm");
    this.confirmArea.append(
      this.confirm,
      el(
        "span",
        "确认后保存不可变人工意图；全部视频被 Emby 发现后才完成。",
        "muted",
      ),
    );

    this.node.append(
      this.editor.node,
      actions,
      this.status,
      this.output,
      this.confirmArea,
    );
    const scope = el("details", undefined, "batch-scope");
    scope.append(
      el("summary", `${job.sources.length} 个下载文件 · 范围与支持限制`),
      el(
        "p",
        "完整任务全部视频及关联字幕一起核对；mkv/mp4，字幕 srt/ass/ssa/vtt。同目录同 stem，最多两个安全语言后缀。辅助文件只绑定指纹。特殊篇、多集单文件、混季及不明确字幕关联暂不支持。",
        "muted",
      ),
    );
    this.node.append(scope, this.candidatePanel());
    if (assistance) {
      this.approval = el("input");
      this.approval.type = "checkbox";
      this.approval.name = "naming_approval";
      const label = el("label", undefined, "order-approval");
      label.append(this.approval, el("span", "我已核对完整文件、季集、依据与不确定项，明确批准当前预览。只批准本次任务，不启用未来规则。"));
      this.confirmArea.prepend(label);
      this.approval.addEventListener("change", () => {
        this.confirm.dataset.unavailable = String(!this.approval.checked || !this.gate.reviewed());
        setBusy(busy);
      });
      this.node.append(el("p", "来源：命名辅助历史 + 人工终审。资料预检可能没有模型调用；修改、拒绝或人工整理仍保留来源，不进入纯人工学习样本。", "notice"));
    }
    this.node.addEventListener("reset", () => {
      this.editor.clearOrder();
      this.invalidate();
    });
  }
  feedback(text, error = false) {
    this.status.textContent = text;
    this.status.className = error ? "batch-status error" : "batch-status muted";
    this.status.setAttribute("role", error ? "alert" : "status");
    if (error) { this.status.tabIndex = -1; this.status.focus(); }
  }
  invalidate(text = "草稿变更后，请重新预览完整整理表。") {
    this.gate.invalidate();
    if (this.approval) this.approval.checked = false;
    this.output.replaceChildren();
    this.candidateOutput?.replaceChildren();
    unavailable(this.confirm, "先预览当前整表");
    this.steps.replaceWith((this.steps = batchSteps(1)));
    this.feedback(text);
  }
  async readPreview() {
    if (busy) return;
    this.invalidate();
    this.editor.update();
    const ticket = this.gate.begin(),
      request = this.assistance ? { id: this.assistance.id, version: this.assistance.version, organization: this.editor.request().organization } : this.editor.request();
    this.controller = new AbortController();
    setBusy(true);
    this.node.setAttribute("aria-busy", "true");
    this.feedback("正在校验全部源文件、字幕与目标冲突… 可取消，草稿会保留。");
    try {
      const result = await api(
        this.assistance ? "naming-preview" : "batch-preview",
        request,
        this.controller.signal,
      );
      if (
        (!this.assistance && result.job_version !== request.version) ||
        !this.gate.accept(ticket, request, result)
      )
        return;
      this.output.replaceChildren(renderBatchPreview(result));
      this.confirm.dataset.unavailable = String(!!this.approval);
      this.confirm.title = "";
      this.confirmArea.hidden = false;
      this.steps.replaceWith((this.steps = batchSteps(3)));
      this.feedback(
        "完整预览已就绪，尚未确认、未入库。请核对全部视频和字幕，再确认方案。",
      );
    } catch (e) {
      if (ticket.owns()) {
        this.feedback(errors[e.message] || `预览未完成：${e.message}`, true);
        this.editor.markError(e.message);
        if (["naming_stale", "stale_version", "stale_plan", "naming_preview_required"].includes(e.message)) await this.onConflict?.();
      }
    } finally {
      if (
        !ticket.owns() &&
        this.gate.accepted === null &&
        this.status.textContent.startsWith("正在校验")
      )
        this.invalidate("输入已变化，旧响应已丢弃；请重新预览。");
      this.node.setAttribute("aria-busy", "false");
      setBusy(false);
      this.controller = null;
    }
  }
  async confirmPlan() {
    if (busy) return;
    const reviewed = this.gate.reviewed();
    if (!reviewed || (this.approval && !this.approval.checked)) {
      this.editor.clearOrder();
      this.editor.update();
      this.invalidate("输入已变化；请先预览当前整表，再确认。");
      return;
    }
    const ticket = this.gate.begin();
    setBusy(true);
    this.cancel.disabled = true;
    this.node.setAttribute("aria-busy", "true");
    this.feedback(
      "正在保存已核对的完整方案… 确认请求发出后无法撤销，请等待状态。",
    );
    try {
      await api(this.assistance ? "naming-confirm" : "batch-confirm", this.assistance ? {
        id: reviewed.request.id, version: reviewed.request.version, digest: reviewed.digest,
      } : { ...reviewed.request, plan_version: reviewed.plan.version, digest: reviewed.digest });
      if (!ticket.owns()) return;
      message.className = "notice";
      message.textContent =
        "人工意图已确认，等待执行与全部 Emby 发现；可在对比记录查看实际结果。";
      await load(false);
    } catch (e) {
      if (ticket.owns()) {
        this.invalidate();
        this.feedback(errors[e.message] || `确认未完成：${e.message}`, true);
        this.editor.markError(e.message);
        if (["naming_stale", "stale_version", "stale_plan", "naming_preview_required"].includes(e.message)) await this.onConflict?.();
      }
    } finally {
      if (!ticket.owns() && this.status.textContent.startsWith("正在保存"))
        this.invalidate(
          "输入已变化；旧确认响应不载入草稿。请刷新任务核对已保存状态。",
        );
      this.node.setAttribute("aria-busy", "false");
      this.cancel.disabled = false;
      setBusy(false);
    }
  }
  candidatePanel() {
    const details = el("details", undefined, "batch-candidates"),
      output = el("div");
    this.candidateOutput = output;
    details.append(
      el("summary", "可选 · 从所填目录 / NFO 读取名称与季号候选"),
      el(
        "p",
        "候选只帮助填表，不赋予执行授权；仍需完整预览及人工确认。",
        "muted",
      ),
    );
    const gate = new ReviewedRequestGate(() =>
      JSON.stringify([this.editor.snapshot(), this.gate.revision]),
    );
    details.append(
      writeButton("读取所填目录 / NFO 候选", async () => {
        if (busy) return;
        gate.invalidate();
        const ticket = gate.begin();
        output.replaceChildren();
        setBusy(true);
        try {
          const result = await api("library-candidates", {
            directory: this.editor.directory.value,
          });
          if (!ticket.owns()) return;
          result.candidates.forEach((c) =>
            output.append(
              button(
                `填入候选：${c.display_name || "名称未知"} / 季${c.season || "未知"}`,
                () => {
                  if (!ticket.owns() || busy) return;
                  this.editor.display.value = c.display_name || "";
                  this.editor.season.value = c.conflicts.length
                    ? ""
                    : c.season || "";
                  this.editor.clearOrder();
                  this.invalidate();
                  this.editor.update();
                  output.replaceChildren();
                },
              ),
            ),
          );
          output.append(
            el(
              "p",
              result.candidates.length
                ? "请核对本次篇章季号与集号。"
                : "没有可用候选，可继续手动填写本次整理。",
              "muted",
            ),
          );
        } catch (e) {
          if (ticket.owns())
            output.replaceChildren(
              el("p", errors[e.message] || e.message, "error"),
            );
        } finally {
          setBusy(false);
        }
      }),
      output,
    );
    return details;
  }
}
function batchPanel(job, detail) {
  return new BatchWorkbench(job, detail).node;
}

const namingStates = {
  requested: "已请求 · 等待结果", suggested: "建议待审核", unresolved: "依据不足 · 待补充",
  rejected: "已人工拒绝", approved: "已人工批准", stale: "依据已过期",
};

// 辅助状态独立轮询；不以requested推断agent在线、调用中或进度。
class NamingAssistance {
  constructor(job, detail, items, readError = "") {
    this.job = job;
    this.detail = detail;
    this.items = items || [];
    this.generation = generation;
    this.node = el("div", undefined, "naming-assistance");
    this.panel = panel("agent 命名辅助 · 本次任务");
    this.panel.dataset.naming = "true";
    this.state = el("p", undefined, "naming-state");
    this.state.dataset.namingState = "true";
    this.state.setAttribute("role", "status");
    this.panel.append(this.state, el("p", "建议帮助整理，人工决定是否入库。agent 未开启或离线时，下载、做种和人工填表仍可继续。", "muted"));
    const hints = el("div", undefined, "grid naming-hints");
    this.directory = field(hints, "人工明确的媒体目录（可空）", "naming_directory");
    this.display = field(hints, "人工明确的显示名（可空）", "naming_display_name");
    this.season = field(hints, "人工明确的季号（可空，不设默认）", "naming_season", "", "number");
    this.season.min = 1; this.season.max = 99; this.season.step = 1;
    this.hintGate = new ReviewedRequestGate(() => JSON.stringify([this.job.id, this.job.version, this.hints(), this.latest?.id, this.latest?.version]));
    draftable(hints, "naming-hints", () => draftFields(hints), saved => { if (restoreFields(hints, saved) === false) return false; this.hintGate.invalidate(); this.sync(); });
    for (const event of ["input", "change", "reset"]) hints.addEventListener(event, () => { this.hintGate.invalidate(); this.sync(); });
    this.status = el("p", undefined, "batch-status muted");
    this.status.dataset.namingStatus = "true";
    this.status.setAttribute("role", "status");
    this.status.tabIndex = -1;
    this.request = writeButton("请求命名建议", () => this.requestNaming(), true);
    this.rejectCheck = el("input"); this.rejectCheck.type = "checkbox"; this.rejectCheck.name = "naming_rejection";
    this.rejectLabel = el("label", undefined, "order-approval");
    this.rejectLabel.append(this.rejectCheck, el("span", "我明确拒绝此请求 / 建议；不批准文件，不自动重新请求。"));
    this.reject = writeButton("人工拒绝本次建议", () => this.rejectNaming());
    this.rejectCheck.addEventListener("change", () => this.sync());
    this.refresh = writeButton("重新读取建议状态", () => this.poll(true));
    const actions = el("div", undefined, "actions");
    actions.append(this.request, this.refresh);
    this.evidence = el("div", undefined, "naming-evidence");
    this.editorSlot = el("div");
    this.panel.append(hints, actions, this.status, this.evidence, this.rejectLabel, this.reject);
    this.node.append(this.panel, this.editorSlot);
    this.render();
    if (readError) this.feedback(readError, true);
    this.schedule();
  }
  get latest() { return this.items.at(-1); }
  hints() { return { directory: this.directory.value, display_name: this.display.value, season: Number(this.season.value) }; }
  current() { return this.generation === generation && this.node.isConnected; }
  feedback(text, error = false) {
    this.status.textContent = text;
    this.status.className = "batch-status " + (error ? "error" : "muted");
    this.status.setAttribute("role", error ? "alert" : "status");
    if (error && this.current()) this.status.focus();
  }
  sync() {
    const v = this.latest, editable = this.job.import === "needs_confirmation" && !this.job.plan?.approved && !this.job.plan?.started;
    const supplemented = v && JSON.stringify(this.hints()) !== JSON.stringify(v.snapshot.hints);
    this.request.textContent = v ? "明确重新请求当前快照" : "请求命名建议";
    this.request.dataset.unavailable = String(!editable || !!v && !supplemented && !["unresolved", "rejected", "stale"].includes(v.state));
    this.reject.dataset.unavailable = String(!v || ["approved", "rejected"].includes(v.state) || !this.rejectCheck.checked);
    this.rejectLabel.hidden = this.reject.hidden = !v || ["approved", "rejected"].includes(v.state);
    setBusy(busy);
  }
  render(draft = null, hints = null) {
    // 状态切换替换表格时，旧组件的在途响应也必须失效。
    this.workbench?.gate.invalidate();
    this.workbench?.controller?.abort();
    const v = this.latest;
    this.state.replaceChildren(badge(v ? namingStates[v.state] || v.state : "尚未请求", v?.state === "unresolved" || v?.state === "stale" ? "warn" : ""));
    if (v?.state === "requested") this.state.append(el("span", " 请求已保存；NAS 没有agent在线或调用进度信息。", "muted"));
    this.evidence.replaceChildren();
    if (v) {
      const facts = hints || v.snapshot.hints;
      this.directory.value = facts.directory || ""; this.display.value = facts.display_name || ""; this.season.value = facts.season || "";
      this.evidence.append(el("p", `请求版本 ${v.version} · ${v.snapshot.files.length} 个完整源文件 · ${v.snapshot.trigger === "automatic" ? "自动入队（已配置启用）" : v.snapshot.trigger === "human" ? "人工请求" : "旧请求（入口未记录）"} · ${v.result ? "命名辅助结果 + 人工审核" : "尚无辅助结果"}`, "muted"));
      if (v.result) this.evidence.append(this.resultTable(v));
      if (v.state === "unresolved") this.evidence.append(el("p", "可补充上方明确事实，再主动申请一次新建议；也可直接在下方人工填写，完整预览后批准。每任务最多3份请求，不自动重试。", "notice"));
      if (["rejected", "stale"].includes(v.state) && v.result) this.evidence.append(el("p", "原结果与辅助来源保留。可在下方独立人工整理，完整预览后直接确认；无需再次请求模型。", "notice"));
      if (v.state === "approved" && v.preview?.plan?.batch) this.evidence.append(button("复用已确认事实为规则草稿", () => {
        if (busy || !this.current()) return;
        ruleClues = { contexts: this.detail.contexts || [], source: v.preview.plan.batch.rows[0]?.source || "", organization: v.preview.plan.batch };
        navigate("rules");
      }));
      if (this.items.length > 1) {
        const history = el("details"); history.append(el("summary", `查看 ${this.items.length} 份请求历史`));
        this.items.forEach(item => {
          history.append(el("p", `${item.id.slice(0, 12)} · v${item.version} · ${namingStates[item.state] || item.state}`, "mono"));
          if (item.result) history.append(this.resultTable(item));
        });
        this.evidence.append(history);
      }
    }
    this.rejectCheck.checked = false;
    this.editorSlot.replaceChildren();
    if (this.job.import !== "imported" && !this.job.plan?.started && !this.job.plan?.approved) {
      const active = !this.manual && v?.result && ["suggested", "unresolved"].includes(v.state) ? v
        : !this.manual && v?.state === "requested" ? [...this.items].reverse().find(item => item.result && ["suggested", "unresolved"].includes(item.state)) || null
        : null;
      if (active && active !== v) this.evidence.append(el("p", "新请求仍待结果；下方人工表绑定此前未解 / 建议的模型来源，须完整预览与终审。", "notice"), this.resultTable(active));
      this.workbench = new BatchWorkbench(this.job, this.detail, active);
      this.workbench.onConflict = () => this.recoverContext();
      this.editorSlot.append(this.workbench.node);
      if (v && !active) this.workbench.node.append(el("p", "独立人工整理：直接预览并确认当前完整表，不需要模型结果或重试。辅助请求历史永久保留，不进入纯人工学习；原建议状态不改写。", "notice"));
      if (active) this.evidence.append(button("改用独立人工整理", () => {
        if (busy || !this.current()) return;
        this.manual = true;
        this.workbench.gate.invalidate();
        this.render(this.workbench.editor.raw(), this.hints());
      }));
      if (draft) {
        const editor = this.workbench.editor;
        editor.directory.value = draft.directory; editor.display.value = draft.display;
        editor.season.value = draft.season; editor.mode.value = draft.mode;
        editor.offset.value = draft.offset; editor.start.value = draft.start;
        editor.rows.sort((a, b) => draft.rows.findIndex(r => r.source === a.source) - draft.rows.findIndex(r => r.source === b.source));
        editor.rows.forEach(row => { row.episode = draft.rows.find(r => r.source === row.source)?.episode || ""; });
        editor.clearOrder(); editor.renderRows(); editor.update();
      } else if (active) {
        const editor = this.workbench.editor;
        editor.directory.value = active.snapshot.hints.directory || "";
        editor.display.value = active.snapshot.hints.display_name || "";
        editor.season.value = active.snapshot.hints.season || "";
        editor.update();
      }
      if (active?.state === "suggested") {
        // 默认建议通过服务端绑定私有source；不在前端猜测同basename对应哪个文件。
        this.workbench.node.querySelector("h2").after(writeButton("载入完整建议到编辑表", () => this.loadSuggestion(active), true));
        this.workbench.feedback("先载入服务端完整建议，核对逐文件映射；编辑后重新预览再批准。也可直接人工填写完整表。" );
      }
    }
    this.sync();
  }
  resultTable(v) {
    const root = el("div");
    root.append(el("h3", v.state === "unresolved" ? "缺少的依据" : "完整逐文件建议"), el("p", v.result.explanation));
    const uncertain = el("ul", undefined, "naming-uncertainties");
    (v.result.uncertainties || []).forEach(text => uncertain.append(el("li", text)));
    if (uncertain.childNodes.length) root.append(uncertain);
    const t = comparisonTable(["原名 → 建议目标", "季 / 集", "依据", "不确定项"]);
    v.snapshot.files.forEach(f => {
      const r = v.result.files.find(x => x.file_id === f.id);
      fileRow(t.body, [filePair(f.name, r?.target || (f.kind === "auxiliary" ? "辅助文件 · 不入库" : "未确定")),
        `${r?.season || "未声明"} / ${r?.episode || "未声明"}`,
        r?.basis || "未提供依据", (r?.uncertainties || []).join("；") || "无已列出项 · 仍须人工核对"], ["原名 → 目标", "季 / 集", "依据", "不确定项"]);
    });
    root.append(t.wrap); return root;
  }
  async recoverContext() {
    const g = generation;
    try {
      const [detail, data] = await Promise.all([
        api("jobs/" + encodeURIComponent(this.job.id)),
        api("naming?job_id=" + encodeURIComponent(this.job.id)),
      ]);
      if (g !== generation || !this.current()) return;
      const draft = this.workbench?.editor.raw(), hints = this.hints();
      this.workbench?.gate.invalidate();
      this.job = detail.job; this.detail = detail;
      this.items = data.items || []; this.hintGate.invalidate();
      this.render(draft, hints);
      this.feedback(this.latest?.state === "stale"
        ? "命名依据已过期；草稿保留，可直接独立人工整理，或明确请求新的建议。"
        : "任务状态已更新；草稿与建议保留。请重新完整预览，无需再次请求模型。");
    } catch (e) {
      if (g === generation && this.current()) this.feedback("状态重读未完成；草稿保留，请手动重新读取。", true);
    }
  }
  async loadSuggestion(v) {
    if (busy) return;
    const wb = this.workbench;
    wb.invalidate();
    const ticket = wb.gate.begin(); setBusy(true);
    try {
      const result = await api("naming-preview", { id: v.id, version: v.version });
      if (!ticket.owns() || !this.current()) return;
      const b = result.plan.batch, editor = wb.editor;
      editor.directory.value = b.directory; editor.display.value = b.display_name; editor.season.value = b.season; editor.mode.value = "manual";
      editor.rows.forEach(row => { row.input.value = b.rows.find(r => r.source === row.source)?.episode || ""; });
      editor.clearOrder(); editor.update();
      // 载入只填表，持久preview绝不自动转为本地批准态。
      wb.invalidate("已载入完整建议；请核对 / 修改，再重新预览当前整表。" );
    } catch (e) { if (ticket.owns() && this.current()) wb.feedback(errors[e.message] || e.message, true); }
    finally { setBusy(false); }
  }
  async requestNaming() {
    if (busy) return;
    this.hintGate.invalidate(); const ticket = this.hintGate.begin();
    const request = { job_id: this.job.id, job_version: this.job.version, hints: this.hints(), retry: !!this.latest };
    setBusy(true);
    try {
      await api("naming-request", request);
      if (!ticket.owns() || !this.current()) return;
      await this.poll(true);
    } catch (e) {
      if (ticket.owns() && this.current()) {
        this.feedback(errors[e.message] || e.message, true);
        if (["naming_stale", "stale_version"].includes(e.message)) await this.recoverContext();
      }
    }
    finally { setBusy(false); }
  }
  async rejectNaming() {
    if (busy || !this.rejectCheck.checked || !this.latest) return;
    const v = this.latest, ticket = this.hintGate.begin();
    this.workbench?.invalidate("已取消审核；等待拒绝结果。" ); setBusy(true);
    try {
      await api("naming-reject", { id: v.id, version: v.version });
      if (!ticket.owns() || !this.current()) return;
      await this.poll(true);
      this.feedback("已人工拒绝；不会自动重新请求。原始依据与来源保留。" );
    } catch (e) {
      if (ticket.owns() && this.current()) {
        this.feedback(errors[e.message] || e.message, true);
        if (["naming_stale", "stale_version"].includes(e.message)) await this.recoverContext();
      }
    }
    finally { setBusy(false); }
  }
  schedule() {
    setTimeout(async () => {
      if (!this.current()) return;
      if (this.latest?.state === "requested" && !busy) await this.poll(false);
      this.schedule();
    }, 5000);
  }
  async poll(explicit) {
    if (!this.current() || this.polling) return;
    this.polling = true;
    const g = generation, ticket = this.hintGate.begin();
    try {
      const data = await api("naming?job_id=" + encodeURIComponent(this.job.id));
      if (g !== generation || !this.current() || !ticket.owns()) return;
      const changed = JSON.stringify(data.items) !== JSON.stringify(this.items);
      if (changed) {
        const detail = await api("jobs/" + encodeURIComponent(this.job.id));
        if (g !== generation || !this.current() || !ticket.owns()) return;
        this.job = detail.job; this.detail = detail;
        // 新结果切换审核通道时保留人工草稿；在途输入变动仍丢弃旧响应。
        const raw = this.workbench?.editor.raw();
        const dirty = raw && (raw.directory || raw.display || raw.season || raw.rows.some(row => row.episode));
        const draft = dirty ? this.workbench.editor.raw() : null;
        const hints = this.hints();
        this.items = data.items || []; this.render(draft, hints);
        if (draft) this.feedback("建议状态已更新；人工草稿保留，请重新预览当前完整表。" );
      } else if (explicit) this.feedback("状态已重新读取；未自动批准或重新请求。" );
    } catch (e) { if (g === generation && this.current() && ticket.owns() && explicit) this.feedback(errors[e.message] || e.message, true); }
    finally { this.polling = false; }
  }
}
async function namingPanel(job, detail) {
  let items = [], error = "";
  try { items = (await api("naming?job_id=" + encodeURIComponent(job.id))).items || []; }
  catch (e) { error = "命名状态暂时不可读取；可继续人工填表。"; }
  const assistance = new NamingAssistance(job, detail, items, error);
  return assistance.node;
}

const comparisonStatuses = {
  confirmed_intent: "人工意图已确认，尚未执行",
  partial: "部分执行，未完整成功",
  awaiting_discovery: "全部硬链接，等待全部 Emby 确认",
  complete: "整包完成",
  superseded: "原方案已被替换，未计完整成功",
};
const changedFieldNames = {
  target_relative_name: "目标文件名",
  directory: "目录",
  display_name: "显示名",
  season: "季号",
  episode: "集号",
  inherited_video_mapping: "沿用视频映射",
};
function executionText(f) {
  if (!f) return "尚无执行结果";
  return `${states[f.State] || f.State} · ${f.Video ? (f.Discovered ? "Emby 已发现" : "Emby 未发现") : "关联字幕"}`;
}
function renderComparison(record) {
  const intent = record.intent,
    status = comparisonStatuses[record.status] || record.status;
  const section = panel(`人工记录 · 方案 v${intent.plan_version}`);
  section.classList.add("comparison-record");
  section.append(
    badge(
      status,
      record.status === "complete" && !record.error_code ? "ok" : "warn",
    ),
    el(
      "p",
      `${intent.origin === "model_assisted_human" ? "model_assisted_human · 命名辅助历史 + 人工终审（不进入纯人工学习）" : intent.origin || "human"} · ${time(intent.created_at)} · 任务版本 ${intent.job_version} · 计划版本 ${intent.plan_version}`,
      "muted",
    ),
    el(
      "p",
      "人工意图（不可变） → 原方案实际结果（持续更新）",
      "comparison-key",
    ),
  );
  const t = comparisonTable([
    "原名 → 目标",
    "人工季集意图 / 改动",
    "实际执行 / 发现",
  ]);
  intent.changes.forEach((change, i) => {
    const meaning = el("div");
    meaning.append(
      el(
        "p",
        change.semantics_resolved
          ? `S${padEpisode(change.season)}E${padEpisode(change.episode)}`
          : "季集未声明",
      ),
      el(
        "p",
        `原识别：季 ${change.before_season || "未声明"} · 相对集 ${change.before_episode || "未声明"} · 总集 ${change.before_absolute || "未声明"}`,
        "muted",
      ),
      el(
        "p",
        "人工改动：" +
          change.changed_fields
            .map((k) => changedFieldNames[k] || k)
            .join("、"),
        "muted",
      ),
    );
    fileRow(
      t.body,
      [
        filePair(change.source, change.target),
        meaning,
        executionText(record.files.find((f) => f.Index === i)),
      ],
      ["原名 → 目标", "人工季集意图 / 改动", "实际执行 / 发现"],
    );
  });
  if (record.error_code)
    section.append(
      el("p", errors[record.error_code] || record.error_code, "error"),
    );
  section.append(t.wrap);
  const evidence = el("details");
  evidence.append(
    el("summary", "记录身份与完整清单"),
    el("p", intent.sample_id, "mono"),
    el(
      "p",
      `摘要 ${intent.digest} · 解析版本 ${intent.parser_version}`,
      "mono",
    ),
  );
  const list = el("ul");
  intent.manifest.forEach((f) => list.append(el("li", f.source, "mono")));
  evidence.append(list);
  section.append(evidence);
  return section;
}
function comparisonPanel(id) {
  const p = panel("人工对比记录"),
    output = el("div"),
    feedback = el("p", undefined, "muted");
  feedback.setAttribute("role", "status");
  let after = 0,
    readRevision = 0,
    reading = false;
  const read = button("查看对比记录", async () => {
    if (reading) return;
    const g = generation,
      revision = ++readRevision;
    reading = true;
    read.disabled = true;
    feedback.className = "muted";
    feedback.textContent = "正在读取人工意图与原计划实际结果…";
    try {
      const data = await api(
        `comparisons?id=${encodeURIComponent(id)}&after=${after}`,
      );
      if (g !== generation || revision !== readRevision) return;
      if (!after) output.replaceChildren();
      data.items.forEach((record) => output.append(renderComparison(record)));
      if (!data.items.length && !after)
        output.append(
          el("p", "没有已确认的人工记录；预览或取消不会生成样本。", "muted"),
        );
      after = data.next;
      read.textContent = after ? "读取后续对比记录" : "重新读取对比记录";
      feedback.textContent = "";
    } catch (e) {
      if (g === generation && revision === readRevision) {
        feedback.className = "error";
        feedback.textContent = errors[e.message] || e.message;
      }
    } finally {
      reading = false;
      read.disabled = false;
    }
  });
  p.append(
    el(
      "p",
      "不可变记录保存 human 人工意图；实际执行与发现独立显示。部分执行和待发现不会标成完成。agent 归纳建议可在命名规则页审核，启用前必须人工终审。每页最多 20 份。",
      "muted",
    ),
    read,
    feedback,
    output,
    el(
      "p",
      "本次确认不会自动创建或启用命名规则。重复使用的映射可另行人工维护。",
      "muted",
    ),
    button("打开命名规则（人工维护）", () => navigate("rules")),
  );
  return p;
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
        name.focus({ preventScroll: true });
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
  form.classList.add("subscription-editor");
  draftable(form, "subscription", () => ({ editing: id.readOnly, values: draftFields(form) }), saved => {
    const savedID = saved.values.find(([key]) => key === "subscription_id")?.[1];
    if (saved.editing) {
      const current = data.items.find(item => item.id === savedID);
      if (!current) return false;
      version = current.version; id.readOnly = true;
      if (![...profile.options].some(o => o.value === current.profile)) { const option = el("option", current.profile + "（配置已移除，仅可停用）"); option.value = current.profile; profile.append(option); }
    }
    return restoreFields(form, saved.values);
  });
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
      id.focus();
    }),
  );
  form.append(actions);
  root.append(form, setupGuide(data.profiles.length));
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
  if (proposalSelection) return proposalDetail();
  const [rules, subscriptions, epoch, audit, suggestions] = await Promise.all([
    api("rules"),
    allSubscriptions(),
    api("epoch"),
    api("audit"),
    proposalList(),
  ]);
  const root = el("div"),
    p = panel("不可变版本规则");
  root.append(suggestions);
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
  const identity = el("fieldset", undefined, "mapping-group"), relative = el("fieldset", undefined, "mapping-group"), absolute = el("fieldset", undefined, "mapping-group");
  identity.append(el("legend", "01 · 媒体身份与版本"));
  relative.append(el("legend", "02 · 相对集数范围"), el("p", "只填写有依据的有限范围。", "muted"));
  absolute.append(el("legend", "03 · 总集 → 相对集（可选）"), el("p", "例如总集 67–78 对应本季 1–12；不能据此前推新篇章。", "muted"));
  const identityGrid = el("div", undefined, "grid"), relativeGrid = el("div", undefined, "grid"), absoluteGrid = el("div", undefined, "grid");
  identity.append(identityGrid); relative.append(relativeGrid); absolute.append(absoluteGrid);
  defs.forEach(([label, n, v, type]) => {
    const group = ["start", "end"].includes(n) ? relativeGrid : ["absolute_start", "absolute_end", "relative_start"].includes(n) ? absoluteGrid : identityGrid;
    fields[n] = field(group, label, n, v, type);
  });
  grid.classList.add("mapping-groups");
  grid.append(identity, relative, absolute);
  fields.subscription = select(
    identityGrid,
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
  let selectedCandidate = null, reusedNamingFacts = false;
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
      const finiteRange = request.rule.episodes || request.rule.absolute;
      if (reusedNamingFacts && (!request.rule.subscription || !request.rule.aliases.length || !finiteRange ||
        !Number.isInteger(finiteRange.start) || !Number.isInteger(finiteRange.end) || finiteRange.start < 1 ||
        finiteRange.end > 999 || finiteRange.end < finiteRange.start)) {
        fail(new Error("请为复用规则核对订阅来源、完整标题别名及明确有限集数范围（1–999）。"));
        return;
      }
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
  draftable(form, "rule-draft", () => draftFields(form).filter(([name]) => name !== "mapping_context"), saved => { if (restoreFields(form, saved) === false) return false; invalidatePreview(); });
  root.append(form);
  const discovery = panel("从已有媒体目录提取候选"),
    libraryDirectory = field(discovery, "媒体根内的相对系列目录（明确选择）", "library_directory"),
    candidateOutput = el("div");
  draftable(discovery, "library-directory", () => libraryDirectory.value, saved => { libraryDirectory.value = saved; });
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
    fields.season.value = clues.organization?.season ?? "";
    fields.directory.value = clues.organization?.directory ?? "";
    fields.display_name.value = clues.organization?.display_name ?? "";
    // 只复用已批准事实；别名与未来有限范围不能由模型结果静默推广。
    if (clues.organization) { reusedNamingFacts = true; fields.subscription.value = ""; }
    if (clues.organization) form.prepend(el("p", "从已批准任务带入目录、显示名、季号。请填写并核对稳定系列身份、标题别名及有限集数范围；预览后保存草稿，再单独启用。", "notice"));
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
function setupGuide(profileCount) {
  const p = panel("接入步骤 · 配置文件管理");
  p.classList.add("setup-guide");
  p.append(el("p", `${profileCount} 个来源档案已配置。${profileCount ? "页面可选择来源引用、启停与重试抓取。" : "缺少 RSS 来源档案，当前不能保存新的启用订阅。"} RSS URL、服务地址与凭据需由部署者在本地配置；本页没有这些字段的保存接口。`, "notice"));
  const steps = el("ol", undefined, "setup-steps");
  [
    ["准备离线包", "先解压离线包；极空间 GUI 导入 image.tar。config.template.json 是公开模板，复制为实际挂载的 config.json；GUI 的启动参数、挂载与访问入口仍需在 NAS 上核验。"],
    ["填写来源与服务", "在实际 config.json 的 subscriptions 中填写唯一 id 和 RSS url；qb_url、emby_url 填部署者确认的服务地址。RSS token 只留在本地配置，不能粘贴到显示名称、规则或来源 ID。"],
    ["提供秘密环境变量", "qB 登录使用 ASUKA_NAS_QB_USERNAME / ASUKA_NAS_QB_PASSWORD；Emby 使用 ASUKA_NAS_EMBY_TOKEN。credentials.env.template 提供变量名，实际值由运行环境注入，不在此页面读取或保存。"],
    ["核对挂载再重启", "核对 downloads 与 library 不重叠且支持同文件系统硬链接；qB 下载路径与服务一致，Emby 媒体路径一致。配置校验只验证格式；重启后回到订阅页选择来源并启用，核对真实轮询与任务证据。"],
  ].forEach(([title, text]) => { const li = el("li"); li.append(el("strong", title), el("p", text, "muted")); steps.append(li); });
  p.append(steps);
  const template = el("details"); template.append(el("summary", "查看无秘密的配置片段（需合入完整模板）"));
  template.append(el("pre", JSON.stringify({subscriptions:[{id:"my_feed",url:"https://example.invalid/rss"}],qb_url:"http://127.0.0.1:8080",emby_url:"http://127.0.0.1:8096"}, null, 2)), el("p", "示例地址为占位内容；不要直接启动。页面不访问这些地址，也不回显已有 URL。", "muted"));
  p.append(template);
  const checks = el("details"); checks.append(el("summary", "部署检查清单"));
  const list = el("ul");
  ["关闭数据库后备份 SQLite；本版开发迁移未授权升级真实生产库。", sessionMode === "authenticated_https" ? "当前使用独立 HTTPS 会话。证书信任、监听与端口映射由部署者核验；现有 Caddy 保持不变。" : "当前是回环单用户入口，不可直接公开。网络访问需先完成独立身份与可信 HTTPS 配置；现有 Caddy 保持不变。", "核对 Linux openat2、非 root 用户权限、挂载路径与实际硬链接；不通过复制绕过跨设备。", "从一条测试订阅开始，逐项核对下载完成、命名预览、人工确认、硬链接及 Emby 真实 Path 发现。", "命名/学习/事件服务默认关闭；启用配置不证明 agent、模型或通知消费者在线。"].forEach((text) => list.append(el("li", text)));
  checks.append(list); p.append(checks); return p;
}
async function healthView() {
  const health = await api("health"),
    p = panel("本地服务与接入边界");
  p.append(
    badge("数据库 " + (health.database === "ok" ? "可读" : health.database || "未知"), health.database === "ok" ? "ok" : ""),
    el(
      "p",
      sessionMode === "authenticated_https" ? "当前模式：已认证 HTTPS 管理。会话认证只证明当前访问身份，不代表 qB、Emby、agent 或 Discord 已连接。" : "当前模式：仅本机单用户、回环监听。导入NAS镜像不会开放LAN访问；用户已有授权SSH回环隧道可作为访问候选，真实部署仍需核验。",
      "notice",
    ),
  );
  const t = table(["能力", "配置", "运行 / 验收状态"]);
  const configured = (key) => health.capabilities?.[key]?.configured_enabled === true ? "已配置启用"
    : health.capabilities?.[key]?.configured_enabled === false ? "配置关闭" : "配置未知";
  [
    ["RSS → qB → 安全入库 → Emby", "已实现", "fake服务验证；真实上游兼容待验"],
    ["SQLite 可重放事件", "已实现", "数据库可读不证明挂载或外部服务健康"],
    ["认证只读事件 / agent 固定通知", configured("events"), "支持已实现；消费者 / Discord当前状态未知"],
    ["映射学习 / agent规则候选", configured("learning"), "支持已实现；agent / 模型当前状态未知"],
    ["单任务命名辅助 / 人工终审", "已实现；人工入口始终可用", "agent运行状态未知；以任务建议列表核对结果"],
    ["命名自动入队", configured("automatic_naming"), "默认关闭；启用不等于agent在线或模型已调用"],
    ["目录 / NFO 局部证据候选", "已实现", "用户明确选目录；候选不授予执行权限"],
    ["TVDB / 在线元数据搜索", "未实现", "无外部元数据接入"],
    ["管理访问身份", sessionMode === "authenticated_https" ? "已启用独立账号 / HTTPS 会话" : "本机单用户 / 无登录", "以当前 session 返回模式为准；真实NAS与客户端信任仍需部署者核验"],
    ["Docker / 真实 NAS", "支持离线镜像包", "CI可验构建与配置；真实NAS部署待验"],
  ].forEach((v) => row(t.body, v));
  p.append(
    t.wrap,
    el(
      "p",
      "程序离线、时钟错误或长文件 I/O 可能延后停止做种；停止不删除源文件。",
      "muted",
    ),
  );
  const config = health.integration_config || {};
  const integration = table(["接入项", "配置已提供", "连接状态"]);
  [["RSS 来源档案", config.rss_profiles === undefined ? "未知" : `${config.rss_profiles} 个（URL 隐藏）`], ["qB 服务地址", config.qb_url === true ? "已提供（地址隐藏）" : config.qb_url === false ? "缺失" : "未知"], ["Emby 服务地址", config.emby_url === true ? "已提供（地址隐藏）" : config.emby_url === false ? "缺失" : "未知"]].forEach(([name, value]) => row(integration.body, [name, value, "未知 · 未发起连接探测"]));
  p.append(el("h3", "当前配置概况"), integration.wrap, el("p", "凭据是否有效只能由真实服务运行验证；数据库正常只证明本地数据库可读。", "muted"));
  const root = el("div"); root.append(p, setupGuide(config.rss_profiles || 0)); return root;
}
// 候选审核复用安全DOM、单写门与快照门；浏览器只有人工管理身份。
const proposalStates = {
  pending: "待人工终审",
  approved: "已批准 · 规则已启用",
  rejected: "已拒绝 · 未启用",
  stale: "已过期 · 不可批准",
};
const proposalErrors = {
  proposal_invalid: "字段不合法。请核对标记字段、名称与说明；草稿已保留。",
  proposal_unbounded:
    "范围必须有限且有依据；不能自行外推未来篇章。草稿已保留。",
  proposal_evidence_conflict:
    "规则与人工依据矛盾，请对照原始记录修正。草稿已保留。",
  proposal_stale: "候选已过期。请重新读取详情，旧试算不能批准。",
  sample_stale: "人工依据已变化。请重新读取详情，旧试算不能批准。",
  proposal_trial_stale:
    "规则环境或试算摘要已变化。请重新历史试算，不能直接重试批准。",
  proposal_trial_limit:
    "试算超过安全限额。请安排依据或规则治理，不能省略必要依据后批准。",
  proposal_conflict: "试算存在冲突。请核对冲突结果、修正规则并保存新版本。",
  proposal_revision_exists: "新版本已存在。请重新读取候选与历史。草稿已保留。",
  proposal_replay_conflict: "同一依据已有不同候选。请重新读取候选与历史。",
  proposal_not_found: "候选不存在。请返回清单重新读取。",
  proposal_limit: "候选版本已达限额。请安排离线历史治理，不会删除证据。",
};
Object.assign(errors, proposalErrors);
let proposalSelection = null,
  proposalCursor = 0;
function proposalStatus(p) {
  return badge(
    proposalStates[p.state] || p.state,
    p.state === "approved" ? "ok" : "warn",
  );
}
function openProposal(id, revision) {
  proposalSelection = { id, revision };
  load();
}
function proposalScope(rule) {
  const bound = rule.episodes;
  return `第 ${rule.season} 季 · 相对集 ${bound ? bound.start + "–" + bound.end : "未声明"} · 订阅 ${rule.subscription} · 字幕组 ${rule.group || "未限定"}${rule.absolute ? ` · 总集 ${rule.absolute.start}–${rule.absolute.end} → 相对首集 ${rule.absolute.relative_start}` : ""}`;
}
async function proposalList() {
  const data = await api("proposals?after=" + proposalCursor);
  const p = panel("agent 待审建议");
  p.classList.add("proposal-list");
  p.append(
    el(
      "p",
      "人工改名记录 → agent 归纳候选 → 人工终审。提交候选不会启用规则；批准规则不会立即执行媒体任务。",
      "muted",
    ),
  );
  const t = comparisonTable([
    "候选 / 版本",
    "有限适用范围",
    "审核状态",
    "操作",
  ]);
  data.items.forEach((v) => {
    const title = el("div");
    title.append(
      el("strong", v.input.rule.display_name),
      el(
        "p",
        `${v.id} · revision ${v.revision} / version ${v.version}`,
        "mono",
      ),
      el(
        "p",
        `${v.input.samples.length} 份人工依据 · ${time(v.created_at)}`,
        "muted",
      ),
    );
    fileRow(
      t.body,
      [
        title,
        proposalScope(v.input.rule),
        proposalStatus(v),
        button("审核建议", () => openProposal(v.id, v.revision)),
      ],
      ["候选 / 版本", "有限适用范围", "审核状态", "操作"],
    );
  });
  if (!data.items.length)
    p.append(
      el(
        "p",
        "暂无待审建议。只有成功的人工对比才会成为学习依据；agent 离线不影响 NAS。",
        "empty",
      ),
    );
  else p.append(t.wrap);
  const actions = el("div", undefined, "actions");
  if (proposalCursor)
    actions.append(
      button("建议首页", () => {
        proposalCursor = 0;
        load();
      }),
    );
  if (data.next)
    actions.append(
      button("下一页建议", () => {
        proposalCursor = data.next;
        load();
      }),
    );
  p.append(actions);
  return p;
}
function textArea(parent, title, name, value) {
  const label = el("label", title),
    input = el("textarea");
  input.name = name;
  input.value = value;
  label.append(input);
  parent.append(label);
  return input;
}
function referenceDetails(ref, title = "样本身份与摘要") {
  const d = el("details");
  d.append(el("summary", title));
  for (const [label, value] of [
    ["样本 ID", ref.sample_id],
    ["计划摘要", ref.digest],
    ["导出摘要", ref.export_digest],
  ])
    d.append(el("p", `${label}：${value}`, "mono"));
  return d;
}
function proposalEvidence(samples, state) {
  const p = panel("1 · 人工原始依据与来源");
  p.append(
    el(
      "p",
      state === "stale"
        ? "以下是历史冻结投影；候选已过期，这些记录不能作为当前批准授权。"
        : "仅从下列成功人工改动归纳。未观察的季号、篇章或来源不能据此推断。",
      "notice",
    ),
  );
  samples.forEach((sample) => {
    const card = el("article", undefined, "proposal-sample");
    card.append(
      el("h3", `任务 ${sample.job_id} · 计划 v${sample.plan_version}`),
    );
    (sample.contexts || []).forEach((c) =>
      card.append(
        el(
          "p",
          `订阅：${c.subscription} · 来源标题：${c.title}`,
          "source-name",
        ),
      ),
    );
    const t = comparisonTable([
      "原名 → 人工目标",
      "原识别 → 人工意图",
      "人工修改字段",
    ]);
    sample.changes.forEach((c) =>
      fileRow(
        t.body,
        [
          filePair(c.source, c.target),
          `原季 ${c.before_season || "未知"} / 相对集 ${c.before_episode || "未知"} / 总集 ${c.before_absolute || "未知"} → 季 ${c.season} / 集 ${c.episode}`,
          c.changed_fields.map((x) => changedFieldNames[x] || x).join("、"),
        ],
        ["原名 → 人工目标", "原识别 → 人工意图", "人工修改字段"],
      ),
    );
    const original = el("details");
    original.append(
      el("summary", "查看完整人工对比与实际执行"),
      comparisonPanel(sample.job_id),
    );
    card.append(
      t.wrap,
      referenceDetails(sample.ref),
      el(
        "p",
        `任务版本 ${sample.job_version} · 来源版本 ${sample.context_version} · 解析版本 ${sample.parser_version}`,
        "muted",
      ),
      original,
    );
    p.append(card);
  });
  return p;
}
class ProposalEditor {
  constructor(proposal, changed) {
    this.proposal = proposal;
    this.node = panel("2 · 有限范围与新版本草稿");
    this.node.dataset.proposalEditor = "true";
    draftable(this.node, "proposal-draft", () => ({ fields: draftFields(this.node), scopeWasDeclared: this.scopeCheck.checked }), saved => {
      if (this.proposal.state !== "pending") return false;
      if (restoreFields(this.node, saved.fields) === false) return false;
      this.scopeCheck.checked = false; this.scopeCheck.dispatchEvent(new Event("change", { bubbles: true }));
      changed();
    });
    const r = proposal.input.rule,
      grid = el("div", undefined, "grid"),
      f = (this.fields = {});
    this.node.append(
      el(
        "p",
        "候选 ID、依据集合和旧版本保持不变。保存产生新 revision；保存后须重新历史试算。",
        "muted",
      ),
      el("p", proposalScope(r), "notice"),
    );
    const defs = [
      ["系列 ID", "series_id", r.series_id],
      ["集序标识", "order", r.order],
      ["显示名称", "display_name", r.display_name],
      ["媒体库相对目录", "directory", r.directory],
      ["目标季号 · 1–99", "season", r.season, "number"],
      ["订阅作用域", "subscription", r.subscription],
      ["字幕组 · 可空", "group", r.group || ""],
      ["相对起始集", "start", r.episodes?.start || "", "number"],
      ["相对结束集 · 连续最多32集", "end", r.episodes?.end || "", "number"],
      ["总集起始 · 可空", "absolute_start", r.absolute?.start || "", "number"],
      ["总集结束 · 可空", "absolute_end", r.absolute?.end || "", "number"],
      [
        "总集对应相对首集 · 可空",
        "relative_start",
        r.absolute?.relative_start || "",
        "number",
      ],
    ];
    defs.forEach(([label, name, value, type]) => {
      f[name] = field(grid, label, "proposal_" + name, value, type);
      if (type === "number") {
        f[name].min = 1;
        f[name].max = name === "season" ? 99 : 999;
        f[name].step = 1;
      }
    });
    this.node.append(grid);
    for (const [label, name, value] of [
      ["标题别名 · 每行一个，最多8个", "aliases", r.aliases.join("\n")],
      [
        "归纳理由 · 应说明哪些人工改动支持此规则",
        "explanation",
        proposal.input.explanation,
      ],
      [
        "不能外推 / 不确定性 · 每行一条",
        "uncertainties",
        (proposal.input.uncertainties || []).join("\n"),
      ],
      [
        "例外 · 每行一条",
        "exceptions",
        (proposal.input.exceptions || []).join("\n"),
      ],
    ])
      f[name] = textArea(this.node, label, "proposal_" + name, value);
    const scope = el("div", undefined, "proposal-scope-declaration");
    this.scopeCheck = el("input");
    this.scopeCheck.type = "checkbox";
    this.scopeCheck.name = "proposal_scope_declaration";
    this.scopeCheck.checked = !!proposal.scope_declaration;
    const scopeLabel = el("label", undefined, "order-approval");
    scopeLabel.append(
      this.scopeCheck,
      el(
        "span",
        "人工声明有限适用范围 · 包含尚未验证的集数。仅我明确声明，agent 不能自行扩大范围。",
      ),
    );
    f.scope_reason = textArea(
      scope,
      "人工范围声明理由 · 最多512字节，说明为何适用未验证集数",
      "proposal_scope_reason",
      proposal.scope_declaration?.reason || "",
    );
    this.node.append(
      scopeLabel,
      el(
        "p",
        "开关启用时，上方相对集数 / 总集映射就是人工声明范围；连续最多32集、季号仍需人工依据。保存后必须重新试算与确认。",
        "muted",
      ),
      scope,
    );
    this.scopeCheck.addEventListener("change", () => {
      scope.hidden = !this.scopeCheck.checked;
    });
    scope.hidden = !this.scopeCheck.checked;
    this.initial = this.snapshot();
    this.last = this.initial;
    for (const event of ["input", "change"])
      this.node.addEventListener(event, () => {
        const next = this.snapshot();
        if (next === this.last) return;
        this.last = next;
        changed();
      });
    this.node.addEventListener("reset", changed);
    if (proposal.state !== "pending")
      [...Object.values(f), this.scopeCheck].forEach(
        (n) => (n.disabled = true),
      );
  }
  snapshot() {
    return JSON.stringify({
      fields: Object.fromEntries(
        Object.entries(this.fields).map(([k, v]) => [k, v.value]),
      ),
      scope: this.scopeCheck.checked,
    });
  }
  dirty() {
    return this.snapshot() !== this.initial;
  }
  input() {
    const f = this.fields,
      original = this.proposal.input;
    const r = { ...original.rule };
    for (const k of [
      "series_id",
      "order",
      "display_name",
      "directory",
      "subscription",
      "group",
    ])
      r[k] = f[k].value;
    r.season = Number(f.season.value);
    r.aliases = this.lines("aliases");
    r.episodes = { start: Number(f.start.value), end: Number(f.end.value) };
    delete r.absolute;
    if (
      ["absolute_start", "absolute_end", "relative_start"].some(
        (k) => f[k].value,
      )
    )
      r.absolute = {
        start: Number(f.absolute_start.value),
        end: Number(f.absolute_end.value),
        relative_start: Number(f.relative_start.value),
      };
    return {
      ...original,
      rule: r,
      explanation: f.explanation.value,
      uncertainties: this.lines("uncertainties"),
      exceptions: this.lines("exceptions"),
    };
  }
  declaration() {
    if (!this.scopeCheck.checked) return undefined;
    const r = this.input().rule;
    return {
      episodes: r.episodes,
      reason: this.fields.scope_reason.value,
      ...(r.absolute ? { absolute: r.absolute } : {}),
    };
  }
  lines(k) {
    return this.fields[k].value
      .split("\n")
      .map((x) => x.trim())
      .filter(Boolean);
  }
  validate() {
    const f = this.fields,
      input = this.input(),
      invalid = [];
    Object.values(f).forEach((n) => n.removeAttribute("aria-invalid"));
    const bytes = (value) => new TextEncoder().encode(value).length;
    for (const k of [
      "series_id",
      "order",
      "display_name",
      "directory",
      "subscription",
      "explanation",
    ])
      if (!f[k].value.trim()) invalid.push(k);
    for (const k of ["season", "start", "end"])
      if (!f[k].value || !f[k].checkValidity()) invalid.push(k);
    if (
      input.rule.episodes.end < input.rule.episodes.start ||
      input.rule.episodes.end - input.rule.episodes.start > 31
    )
      invalid.push("end");
    if (input.rule.absolute)
      for (const k of ["absolute_start", "absolute_end", "relative_start"])
        if (!f[k].value || !f[k].checkValidity()) invalid.push(k);
    for (const [k, max, count] of [
      ["aliases", 512, 8],
      ["uncertainties", 512, 8],
      ["exceptions", 512, 8],
    ])
      if (
        this.lines(k).length > count ||
        (k === "aliases" && !this.lines(k).length) ||
        this.lines(k).some((x) => bytes(x) > max)
      )
        invalid.push(k);
    for (const [k, max] of [
      ["explanation", 2000],
      ["display_name", 200],
      ["directory", 512],
    ])
      if (bytes(f[k].value) > max) invalid.push(k);
    if (
      this.scopeCheck.checked &&
      (!f.scope_reason.value.trim() || bytes(f.scope_reason.value) > 512)
    )
      invalid.push("scope_reason");
    if (!invalid.length) return true;
    invalid.forEach((k) => f[k].setAttribute("aria-invalid", "true"));
    f[invalid[0]].focus();
    return false;
  }
}
// 大集合逐页呈现，全部依据/结果保留可达；不以JSON替代审核界面。
function reviewPages(items, headers, render, title) {
  const node = el("div", undefined, "review-pages"),
    t = comparisonTable(headers),
    actions = el("div", undefined, "actions"),
    status = el("span", undefined, "muted");
  let page = 0;
  const prev = button("上一页" + title, () => {
      page--;
      update();
      (prev.disabled ? status : prev).focus();
    }),
    next = button("下一页" + title, () => {
      page++;
      update();
      (next.disabled ? status : next).focus();
    });
  function update() {
    t.body.replaceChildren();
    items
      .slice(page * 20, (page + 1) * 20)
      .forEach((item) => fileRow(t.body, render(item), headers));
    status.textContent = `${items.length} 条 · ${items.length ? page * 20 + 1 : 0}–${Math.min((page + 1) * 20, items.length)}`;
    prev.disabled = page === 0;
    next.disabled = (page + 1) * 20 >= items.length;
  }
  status.setAttribute("role", "status");
  status.tabIndex = -1;
  actions.append(prev, status, next);
  node.append(t.wrap, actions);
  update();
  return node;
}
function renderProposalTrial(trial) {
  const node = el("div", undefined, "proposal-trial");
  node.append(
    el(
      "h3",
      `试算：${trial.results.length} 个结果 · ${trial.conflicts} 个冲突`,
    ),
    el(
      "p",
      `revision ${trial.revision} / version ${trial.version} · 命名环境 epoch ${trial.epoch} · 历史样本上限 ${trial.limit}`,
      "muted",
    ),
    el(
      "p",
      trial.truncated
        ? "覆盖有限：历史集合已截断，只验证下列覆盖记录，不代表全部历史或未来资源。"
        : "本次仅验证下列覆盖集合；不代表全数据库、真实 NAS 或未来来源。",
      "notice",
    ),
  );
  if (trial.scope_review) {
    const scope = trial.scope_review,
      p = panel("人工声明范围 · 与已验证依据分开");
    p.append(
      el(
        "p",
        `已验证集数 · ${(scope.verified_episodes || []).length} 集：${(scope.verified_episodes || []).join("、") || "无"}`,
        "scope-verified",
      ),
      el(
        "p",
        `人工声明 · 相对集 ${scope.declared_episodes.start}–${scope.declared_episodes.end}`,
        "muted",
      ),
      el(
        "p",
        `人工声明但未验证 · ${(scope.unverified_episodes || []).length} 集：${(scope.unverified_episodes || []).join("、") || "无"}`,
        "notice",
      ),
      el("p", "声明理由：" + scope.reason),
    );
    if (scope.absolute)
      p.append(
        el(
          "p",
          `声明总集 ${scope.absolute.start}–${scope.absolute.end} → 相对首集 ${scope.absolute.relative_start}`,
        ),
      );
    node.append(p);
  }
  const outcomes = {
    matched: "匹配",
    not_matched: "未匹配",
    out_of_scope: "范围外",
    conflict: "冲突 · 不可批准",
  };
  node.append(
    reviewPages(
      trial.results,
      ["源文件 → 试算目标", "试算结果", "样本 ID"],
      (r) => [
        filePair(r.source, r.target || "无目标"),
        badge(
          outcomes[r.outcome] || r.outcome,
          r.outcome === "conflict" ? "warn" : "",
        ),
        el("span", r.sample_id, "mono"),
      ],
      "结果",
    ),
  );
  const coverage = el("details");
  coverage.open = true;
  coverage.append(
    el("summary", `完整试算覆盖集合 · ${trial.coverage.length} 份样本`),
    reviewPages(
      trial.coverage,
      ["样本 ID", "计划 / 导出摘要"],
      (r) => [
        el("span", r.sample_id, "mono"),
        referenceDetails(r, "查看计划与导出摘要"),
      ],
      "覆盖",
    ),
  );
  node.append(coverage, el("p", "试算摘要：" + trial.digest, "mono"));
  return node;
}
class ProposalReview {
  constructor(proposal) {
    this.proposal = proposal;
    this.node = el("div");
    this.editor = new ProposalEditor(proposal, () => this.invalidate());
    this.gate = new ReviewedRequestGate(() =>
      JSON.stringify([
        proposal.id,
        proposal.revision,
        proposal.version,
        this.editor.snapshot(),
      ]),
    );
    this.status = el("p", undefined, "batch-status");
    this.status.dataset.proposalStatus = "true";
    this.status.setAttribute("role", "status");
    this.status.tabIndex = -1;
    this.output = el("div");
    this.output.dataset.proposalTrial = "true";
    this.preview = writeButton("历史试算当前版本", () => this.run("preview"));
    this.save = writeButton("保存为新 revision", () => this.run("revise"));
    this.approve = writeButton(
      "人工批准并启用规则",
      () => this.run("approve"),
      true,
    );
    this.reject = writeButton("人工拒绝此建议", () => this.run("reject"));
    this.approvalCheck = el("input");
    this.approvalCheck.type = "checkbox";
    this.approvalCheck.name = "proposal_approval";
    this.scopeApproval = el("input");
    this.scopeApproval.type = "checkbox";
    this.scopeApproval.name = "proposal_scope_approval";
    this.scopeApprovalLabel = el("label", undefined, "order-approval");
    this.scopeApprovalLabel.hidden = true;
    this.scopeApprovalLabel.append(
      this.scopeApproval,
      el(
        "span",
        "我再次确认：人工声明范围包含未验证集数。历史试算没有证明这些集数；我仍明确批准这一有限范围。",
      ),
    );
    this.rejectionCheck = el("input");
    this.rejectionCheck.type = "checkbox";
    this.rejectionCheck.name = "proposal_rejection";
    const approveLabel = el("label", undefined, "order-approval"),
      rejectLabel = el("label", undefined, "order-approval");
    approveLabel.append(
      this.approvalCheck,
      el(
        "span",
        "我已核对人工依据、有限范围及完整试算覆盖；明确批准此版本并启用规则。批准不会执行或迁移媒体文件。",
      ),
    );
    rejectLabel.append(
      this.rejectionCheck,
      el("span", "我明确拒绝此候选；拒绝不会创建或启用规则。"),
    );
    [this.approvalCheck, this.rejectionCheck, this.scopeApproval].forEach((n) =>
      n.addEventListener("change", () => this.sync()),
    );
    const toolbar = el("div", undefined, "actions");
    toolbar.append(
      this.save,
      this.preview,
      button("取消本次审核 / 试算", () =>
        this.invalidate(
          "审核已取消；草稿保留。操作若已送达服务，请重新读取实际状态。",
        ),
      ),
    );
    const decision = panel("4 · 人工终审"),
      trial = panel("3 · 历史试算");
    trial.append(
      el(
        "p",
        "须先保存当前草稿。冲突非零不能批准；修改、导航、取消和新版本都会使旧试算失效。",
        "muted",
      ),
      this.output,
    );
    decision.append(
      approveLabel,
      this.scopeApprovalLabel,
      this.approve,
      rejectLabel,
      this.reject,
    );
    this.editor.node.append(
      toolbar,
      this.status,
      button("重新读取候选状态", () => load()),
    );
    this.node.append(this.editor.node, trial, decision);
    this.sync();
  }
  request() {
    const p = this.proposal;
    return { id: p.id, revision: p.revision, version: p.version };
  }
  sync() {
    // 静默赋值在请求边界被观察后也更新去重基线；恢复原值必须同步按钮状态。
    this.editor.last = this.editor.snapshot();
    const pending = this.proposal.state === "pending",
      dirty = this.editor.dirty(),
      trial = this.gate.reviewed();
    for (const [b, allowed] of [
      [this.save, pending && dirty],
      [this.preview, pending && !dirty],
      [
        this.approve,
        pending &&
          !dirty &&
          trial &&
          trial.conflicts === 0 &&
          this.approvalCheck.checked &&
          (!trial.scope_review?.unverified_episodes?.length ||
            this.scopeApproval.checked),
      ],
      [this.reject, pending && !dirty && this.rejectionCheck.checked],
    ]) {
      b.dataset.unavailable = String(!allowed);
      b.disabled = busy || !allowed;
    }
  }
  invalidate(text = "草稿已变化；旧试算失效。请保存新 revision 后重新试算。") {
    this.gate.invalidate();
    this.output.replaceChildren();
    this.approvalCheck.checked = false;
    this.rejectionCheck.checked = false;
    this.scopeApproval.checked = false;
    this.scopeApprovalLabel.hidden = true;
    this.status.className = "muted batch-status";
    this.status.textContent = text;
    this.sync();
  }
  async run(action) {
    if (busy || this.proposal.state !== "pending") return;
    if (action === "revise" && !this.editor.validate()) {
      this.status.className = "error batch-status";
      this.status.textContent = errors.proposal_invalid;
      return;
    }
    if (action !== "revise" && this.editor.dirty()) {
      this.invalidate("草稿尚未保存。请先保存新 revision，再重新历史试算。");
      return;
    }
    const reviewed = this.gate.reviewed();
    if (
      action === "approve" &&
      (!reviewed ||
        reviewed.conflicts ||
        !this.approvalCheck.checked ||
        (reviewed.scope_review?.unverified_episodes?.length &&
          !this.scopeApproval.checked))
    ) {
      this.invalidate(
        "当前版本没有可批准的成功试算。请重新试算并明确人工确认。",
      );
      return;
    }
    if (action === "reject" && !this.rejectionCheck.checked) return;
    if (action === "preview") this.invalidate("正在试算当前已保存版本…");
    const ticket = this.gate.begin(),
      request = this.request();
    if (action === "revise") {
      request.input = this.editor.input();
      request.scope_declaration = this.editor.declaration();
    }
    if (action === "approve") {
      request.epoch = reviewed.epoch;
      request.digest = reviewed.digest;
    }
    setBusy(true);
    this.sync();
    try {
      const result = await api("proposal-" + action, request);
      if (!ticket.owns()) return;
      if (action === "preview") {
        if (
          result.id !== request.id ||
          result.revision !== request.revision ||
          result.version !== request.version ||
          !result.digest
        )
          throw new Error("proposal_trial_stale");
        if (this.proposal.scope_declaration && !result.scope_review)
          throw new Error("proposal_trial_stale");
        this.gate.accept(ticket, request, result);
        this.output.replaceChildren(renderProposalTrial(result));
        this.scopeApprovalLabel.hidden =
          !result.scope_review?.unverified_episodes?.length;
        this.status.className = "muted batch-status";
        this.status.textContent = result.conflicts
          ? "试算存在冲突；批准已锁定。请核对并修正。"
          : "当前版本试算成功。请核对完整覆盖，再明确选择人工终审。";
      } else {
        // 写响应仅表示请求完成；读取持久状态再呈现批准/拒绝。
        const revision =
          action === "revise" ? result.revision : request.revision;
        const actual = await api(
          `proposal?id=${encodeURIComponent(request.id)}&revision=${revision}`,
        );
        if (!ticket.owns()) return;
        if (
          (action === "approve" && actual.state !== "approved") ||
          (action === "reject" && actual.state !== "rejected") ||
          (action === "revise" && actual.revision <= request.revision)
        )
          throw new Error("proposal_stale");
        proposalSelection = { id: actual.id, revision: actual.revision };
        await load();
      }
    } catch (e) {
      if (!ticket.owns()) return;
      this.invalidate();
      this.status.className = "error batch-status";
      this.status.textContent = errors[e.message] || e.message;
      this.status.focus();
    } finally {
      setBusy(false);
      this.sync();
    }
  }
}
async function proposalDetail() {
  const { id, revision } = proposalSelection,
    query = `id=${encodeURIComponent(id)}&revision=${revision}`;
  const [proposal, samples, audit] = await Promise.all([
    api("proposal?" + query),
    api("proposal-samples?" + query),
    api("proposal-audit?id=" + encodeURIComponent(id)),
  ]);
  const root = el("div", undefined, "proposal-review"),
    p = panel("审核 agent 规则建议");
  root.append(
    button("← 返回建议清单", () => {
      proposalSelection = null;
      load();
    }),
  );
  p.append(
    el("h3", proposal.input.rule.display_name),
    proposalStatus(proposal),
    el(
      "p",
      `${id} · revision ${proposal.revision} / version ${proposal.version} · 来源 ${proposal.origin === "agent" ? "agent 归纳" : "人工修订"}`,
      "mono",
    ),
    el(
      "p",
      "候选提交 ≠ 启用规则。人工批准 ≠ 执行媒体。既有冻结文件不会迁移。",
      "notice",
    ),
  );
  p.querySelector("h2").tabIndex = -1;
  p.querySelector("h2").dataset.reviewHeading = "true";
  const history = el("details");
  history.append(
    el("summary", `候选审计与版本历史 · 最近 ${audit.items.length} 条`),
  );
  const kinds = {
    proposal_agent: "agent 提交候选",
    proposal_human: "人工新版本",
    proposal_submitted: "agent 提交候选",
    proposal_revised: "人工保存新版本",
    proposal_scope_declared: "人工声明有限范围",
    proposal_approved: "人工批准并启用",
    proposal_rejected: "人工拒绝",
    proposal_stale: "依据或版本过期",
  };
  const revisions = [...new Set(audit.items.map((x) => x.revision))].sort(
    (a, b) => b - a,
  );
  const links = el("div", undefined, "actions");
  revisions.forEach((r) =>
    links.append(button("查看 revision " + r, () => openProposal(id, r))),
  );
  history.append(links);
  audit.items.forEach((a) =>
    history.append(
      el(
        "p",
        `#${a.seq} · revision ${a.revision} · ${kinds[a.kind] || a.kind} · ${time(a.created_at)}`,
        "muted",
      ),
    ),
  );
  p.append(history);
  root.append(
    p,
    proposalEvidence(samples.items, proposal.state),
    new ProposalReview(proposal).node,
  );
  return root;
}

readRoute();
await load();
