(function () {
  const TOKEN_KEY = "outline_gate_ui_token";
  const MAX_UI_EVENTS = 500;

  const el = {
    token: document.getElementById("token"),
    saveToken: document.getElementById("btn-save-token"),
    clearToken: document.getElementById("btn-clear-token"),
    form: document.getElementById("add-form"),
    ruleInput: document.getElementById("rule-input"),
    formError: document.getElementById("form-error"),
    rulesBody: document.getElementById("rules-body"),
    status: document.getElementById("status"),
    effective: document.getElementById("effective"),
    refresh: document.getElementById("btn-refresh"),
    apply: document.getElementById("btn-apply"),
    outlineStatus: document.getElementById("outline-status"),
    keyForm: document.getElementById("key-form"),
    accessKey: document.getElementById("access-key"),
    keyError: document.getElementById("key-error"),
    keyOk: document.getElementById("key-ok"),
    readyPill: document.getElementById("ready-pill"),
    authPill: document.getElementById("auth-pill"),
    stReady: document.getElementById("st-ready"),
    stServer: document.getElementById("st-server"),
    stKey: document.getElementById("st-key"),
    stSocks: document.getElementById("st-socks"),
    stGw: document.getElementById("st-gw"),
    stRates: document.getElementById("st-rates"),
    statusError: document.getElementById("status-error"),
    logList: document.getElementById("log-list"),
    logEmpty: document.getElementById("log-empty"),
    logSearch: document.getElementById("log-search"),
    liveDot: document.getElementById("live-dot"),
    liveLabel: document.getElementById("live-label"),
    btnPause: document.getElementById("btn-pause"),
    btnClearLog: document.getElementById("btn-clear-log"),
    appVer: document.getElementById("app-ver"),
    appVerFoot: document.getElementById("app-ver-foot"),
  };

  function setAppVersion(v) {
    if (!v) return;
    if (el.appVer) el.appVer.textContent = v;
    if (el.appVerFoot) el.appVerFoot.textContent = v;
    document.title = "outline-gate " + v;
  }

  // Public endpoint — no token required; always reflects process build.
  async function loadVersion() {
    try {
      const res = await fetch("/api/v1/version");
      if (!res.ok) return;
      const data = await res.json();
      if (data && data.version) setAppVersion(data.version);
    } catch (_) {
      /* keep placeholder */
    }
  }

  let events = [];
  let filter = "all";
  let search = "";
  let paused = false;
  let es = null;
  let stickBottom = true;

  function getToken() {
    return sessionStorage.getItem(TOKEN_KEY) || "";
  }

  function setToken(t) {
    if (t) sessionStorage.setItem(TOKEN_KEY, t);
    else sessionStorage.removeItem(TOKEN_KEY);
  }

  async function api(path, opts = {}) {
    const headers = Object.assign({}, opts.headers || {});
    const token = getToken();
    if (token) headers["Authorization"] = "Bearer " + token;
    if (opts.body && !headers["Content-Type"]) {
      headers["Content-Type"] = "application/json";
    }
    const res = await fetch(path, Object.assign({}, opts, { headers }));
    const text = await res.text();
    let data = null;
    try {
      data = text ? JSON.parse(text) : null;
    } catch (_) {
      data = { raw: text };
    }
    if (!res.ok) {
      const msg = (data && (data.error || data.message)) || res.statusText || "error";
      const err = new Error(msg);
      err.status = res.status;
      err.data = data;
      throw err;
    }
    return data;
  }

  function kindLabel(k) {
    switch (k) {
      case "ip": return "IP";
      case "cidr": return "CIDR";
      case "domain": return "домен";
      case "suffix": return "маска";
      default: return k || "—";
    }
  }

  function showError(msg) {
    el.formError.hidden = !msg;
    el.formError.textContent = msg || "";
  }

  function showKeyMsg(err, ok) {
    el.keyError.hidden = !err;
    el.keyError.textContent = err || "";
    el.keyOk.hidden = !ok;
    el.keyOk.textContent = ok || "";
  }

  function escapeHtml(s) {
    return String(s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  function escapeAttr(s) {
    return escapeHtml(s).replace(/'/g, "&#39;");
  }

  function setAuthPill(ok) {
    if (!getToken()) {
      el.authPill.textContent = "нет токена";
      el.authPill.className = "pill pill-muted";
    } else if (ok) {
      el.authPill.textContent = "авторизован";
      el.authPill.className = "pill pill-ok";
    } else {
      el.authPill.textContent = "401";
      el.authPill.className = "pill pill-bad";
    }
  }

  function setReadyPill(ready) {
    if (ready === true) {
      el.readyPill.textContent = "VPN ready";
      el.readyPill.className = "pill pill-ok";
    } else if (ready === false) {
      el.readyPill.textContent = "VPN down";
      el.readyPill.className = "pill pill-bad";
    } else {
      el.readyPill.textContent = "VPN —";
      el.readyPill.className = "pill pill-muted";
    }
  }

  /* Tabs */
  document.querySelectorAll(".tab").forEach((tab) => {
    tab.addEventListener("click", () => {
      const name = tab.getAttribute("data-tab");
      document.querySelectorAll(".tab").forEach((t) => {
        const on = t === tab;
        t.classList.toggle("active", on);
        t.setAttribute("aria-selected", on ? "true" : "false");
      });
      document.querySelectorAll(".panel").forEach((p) => {
        const on = p.id === "panel-" + name;
        p.classList.toggle("active", on);
        p.hidden = !on;
      });
      if (name === "log") {
        connectStream();
        renderLog();
      }
    });
  });

  function renderOutline(st) {
    if (!st) {
      el.outlineStatus.textContent = "—";
      return;
    }
    const parts = [
      "ключ: " + (st.access_key_redacted || "—"),
      "ready: " + (st.ready ? "да" : "нет"),
    ];
    if (st.server_ip) parts.push("server: " + st.server_ip);
    if (st.persist_file) parts.push("persist: " + st.persist_file);
    el.outlineStatus.textContent = parts.join(" · ");
  }

  function renderStatus(data) {
    el.statusError.hidden = true;
    const o = (data && data.outline) || {};
    setReadyPill(typeof o.ready === "boolean" ? o.ready : null);
    el.stReady.textContent = o.ready ? "ready" : "not ready";
    el.stServer.textContent = o.server_ip || "—";
    el.stKey.textContent = o.access_key_redacted || "—";
    el.stSocks.textContent = (data && data.socks) || "—";
    el.stGw.textContent = data && data.gateway ? "включён" : "выкл";
    const c = (data && data.connlog) || {};
    if (c.total_1m != null) {
      el.stRates.textContent =
        "VPN " + (c.vpn_1m || 0) + " · Direct " + (c.direct_1m || 0) +
        (c.fail_1m ? " · fail " + c.fail_1m : "");
    } else {
      el.stRates.textContent = "—";
    }
    renderOutline(o);
  }

  function renderRules(rules) {
    if (!rules || !rules.length) {
      el.rulesBody.innerHTML = '<tr><td colspan="3" class="muted">Список пуст</td></tr>';
      return;
    }
    el.rulesBody.innerHTML = rules
      .map(
        (r) =>
          `<tr>
            <td><code>${escapeHtml(r.rule)}</code></td>
            <td><span class="kind-badge">${escapeHtml(kindLabel(r.kind))}</span></td>
            <td><button type="button" class="danger" data-rule="${escapeAttr(r.rule)}">Удалить</button></td>
          </tr>`
      )
      .join("");
    el.rulesBody.querySelectorAll("button[data-rule]").forEach((btn) => {
      btn.addEventListener("click", () => removeRule(btn.getAttribute("data-rule")));
    });
  }

  function viaLabel(via) {
    if (via === "direct") return "Direct";
    if (via === "drop") return "Drop";
    return "VPN";
  }

  function viaIcon(via) {
    if (via === "direct") return "icons/direct.svg";
    if (via === "drop") return "icons/drop.svg";
    return "icons/vpn.svg";
  }

  function protoLabel(p) {
    return p === "l3" ? "L3" : "SOCKS";
  }

  function protoIcon(p) {
    return p === "l3" ? "icons/l3.svg" : "icons/socks.svg";
  }

  function formatTime(iso) {
    try {
      const d = new Date(iso);
      return d.toLocaleTimeString(undefined, { hour12: false });
    } catch (_) {
      return "—";
    }
  }

  function eventMatches(e) {
    if (filter === "vpn" && e.via === "direct") return false;
    if (filter === "direct" && e.via !== "direct") return false;
    if (filter === "socks" && e.proto !== "socks") return false;
    if (filter === "l3" && e.proto !== "l3") return false;
    if (search) {
      const q = search.toLowerCase();
      const hay = ((e.host || "") + " " + (e.target || "") + " " + (e.client_ip || "") + " " + (e.rule || "")).toLowerCase();
      if (!hay.includes(q)) return false;
    }
    return true;
  }

  function chainHTML(e) {
    let via = "tunnel";
    if (e.via === "direct") via = "direct";
    else if (e.via === "drop") via = "drop";
    const cls = ["chain", "via-" + via];
    if (!e.ok) cls.push("fail");
    const dest = escapeHtml(e.target || e.host || "—");
    const client = escapeHtml(e.client_ip || "?");
    const rule = e.rule ? `<span class="chain-meta">rule: ${escapeHtml(e.rule)}</span>` : "";
    const dur = e.duration_ms != null ? `<span class="chain-meta">${e.duration_ms}ms</span>` : "";
    const ok = e.ok
      ? `<span class="chain-ok" title="ok">✓</span>`
      : `<span class="chain-fail" title="${escapeAttr(e.error || "fail")}">✗</span>`;
    const err = !e.ok && e.error ? `<span class="chain-meta">${escapeHtml(e.error)}</span>` : "";
    let badgeClass = "vpn";
    if (via === "direct") badgeClass = "direct";
    else if (via === "drop") badgeClass = "drop";
    return `<div class="${cls.join(" ")}" data-id="${e.id}">
      <span class="chain-time">${formatTime(e.time)}</span>
      <span class="chain-node" title="клиент">${client}</span>
      <span class="chain-arrow">→</span>
      <span class="chain-node" title="${protoLabel(e.proto)}"><img class="ico" src="${protoIcon(e.proto)}" width="11" height="11" alt="" /> ${protoLabel(e.proto)}</span>
      <span class="chain-arrow">→</span>
      <span class="badge-via ${badgeClass}"><img class="ico" src="${viaIcon(via)}" width="11" height="11" alt="" /> ${viaLabel(via)}</span>
      <span class="chain-arrow">→</span>
      <span class="chain-dest">${dest}</span>
      ${ok}
      ${dur}
      ${rule}
      ${err}
    </div>`;
  }

  function renderLog() {
    const filtered = events.filter(eventMatches);
    const empty = el.logEmpty;
    if (!filtered.length) {
      el.logList.innerHTML = "";
      el.logList.appendChild(empty);
      empty.hidden = false;
      empty.textContent = events.length
        ? "Нет записей по текущему фильтру."
        : "Пока нет подключений — сделайте запрос через SOCKS или L3.";
      return;
    }
    empty.hidden = true;
    const wasBottom = stickBottom;
    el.logList.innerHTML = filtered.map(chainHTML).join("");
    if (wasBottom && !paused) {
      el.logList.scrollTop = el.logList.scrollHeight;
    }
  }

  function pushEvent(e) {
    if (!e || e.id == null) return;
    if (events.some((x) => x.id === e.id)) return;
    events.push(e);
    if (events.length > MAX_UI_EVENTS) {
      events = events.slice(-MAX_UI_EVENTS);
    }
    if (!paused) renderLog();
  }

  function setLive(state) {
    el.liveDot.classList.remove("on", "paused");
    if (state === "on") {
      el.liveDot.classList.add("on");
      el.liveLabel.textContent = "live";
    } else if (state === "paused") {
      el.liveDot.classList.add("paused");
      el.liveLabel.textContent = "пауза";
    } else {
      el.liveLabel.textContent = "offline";
    }
  }

  function disconnectStream() {
    if (es) {
      es.close();
      es = null;
    }
    setLive("off");
  }

  function connectStream() {
    const token = getToken();
    if (!token) {
      disconnectStream();
      return;
    }
    if (es) return;
    const url = "/api/v1/connections/stream?token=" + encodeURIComponent(token);
    es = new EventSource(url);
    es.addEventListener("snapshot", (ev) => {
      try {
        const arr = JSON.parse(ev.data);
        events = Array.isArray(arr) ? arr.slice() : [];
        renderLog();
      } catch (_) {}
    });
    es.addEventListener("conn", (ev) => {
      try {
        pushEvent(JSON.parse(ev.data));
      } catch (_) {}
    });
    es.onopen = () => setLive(paused ? "paused" : "on");
    es.onerror = () => {
      setLive("off");
      // browser will retry EventSource
    };
  }

  el.logList.addEventListener("scroll", () => {
    const near =
      el.logList.scrollHeight - el.logList.scrollTop - el.logList.clientHeight < 40;
    stickBottom = near;
  });

  document.querySelectorAll("#log-filters .chip").forEach((chip) => {
    chip.addEventListener("click", () => {
      document.querySelectorAll("#log-filters .chip").forEach((c) => c.classList.remove("active"));
      chip.classList.add("active");
      filter = chip.getAttribute("data-filter") || "all";
      renderLog();
    });
  });

  el.logSearch.addEventListener("input", () => {
    search = el.logSearch.value.trim();
    renderLog();
  });

  el.btnPause.addEventListener("click", () => {
    paused = !paused;
    el.btnPause.textContent = paused ? "Продолжить" : "Пауза";
    setLive(es ? (paused ? "paused" : "on") : "off");
    if (!paused) renderLog();
  });

  el.btnClearLog.addEventListener("click", () => {
    events = [];
    renderLog();
  });

  async function loadOutline() {
    try {
      const st = await api("/api/v1/outline");
      renderOutline(st);
      setReadyPill(st.ready);
      setAuthPill(true);
    } catch (e) {
      el.outlineStatus.textContent =
        "Ошибка: " + e.message + (e.status === 401 ? " (проверьте токен)" : "");
      if (e.status === 401) setAuthPill(false);
    }
  }

  async function loadStatus() {
    try {
      const data = await api("/api/v1/status");
      if (data && data.version) setAppVersion(data.version);
      renderStatus(data);
      setAuthPill(true);
    } catch (e) {
      el.statusError.hidden = false;
      el.statusError.textContent =
        "Ошибка: " + e.message + (e.status === 401 ? " (проверьте токен)" : "");
      if (e.status === 401) {
        setAuthPill(false);
        setReadyPill(null);
      }
    }
  }

  async function loadBypass() {
    showError("");
    el.status.textContent = "Загрузка…";
    try {
      const list = await api("/api/v1/bypass");
      renderRules(list.rules || []);
      const eff = await api("/api/v1/bypass/effective");
      const lines = [];
      if (eff.dns_error) lines.push("# DNS: " + eff.dns_error);
      (eff.nets || []).forEach((n) => lines.push(n));
      el.effective.textContent = lines.length ? lines.join("\n") : "(пусто)";
      el.status.textContent =
        "Правил: " + (list.rules || []).length +
        (eff.dns_error ? " · DNS: " + eff.dns_error : " · OK");
      setAuthPill(true);
    } catch (e) {
      el.status.textContent = "Ошибка: " + e.message + (e.status === 401 ? " (проверьте токен)" : "");
      if (e.status === 401) {
        setAuthPill(false);
        el.rulesBody.innerHTML = '<tr><td colspan="3" class="muted">Требуется токен</td></tr>';
      }
    }
  }

  async function loadAll() {
    await loadStatus();
    await loadOutline();
    await loadBypass();
    connectStream();
  }

  async function removeRule(rule) {
    if (!confirm("Удалить " + rule + "?")) return;
    try {
      await api("/api/v1/bypass?rule=" + encodeURIComponent(rule), { method: "DELETE" });
      await loadBypass();
    } catch (e) {
      showError(e.message);
    }
  }

  el.saveToken.addEventListener("click", () => {
    setToken(el.token.value.trim());
    disconnectStream();
    loadAll();
  });
  el.clearToken.addEventListener("click", () => {
    el.token.value = "";
    setToken("");
    disconnectStream();
    events = [];
    renderLog();
    loadAll();
  });
  el.refresh.addEventListener("click", loadBypass);
  el.apply.addEventListener("click", async () => {
    try {
      await api("/api/v1/bypass/apply", { method: "POST" });
      await loadBypass();
    } catch (e) {
      showError(e.message);
    }
  });
  el.form.addEventListener("submit", async (ev) => {
    ev.preventDefault();
    showError("");
    const rule = el.ruleInput.value.trim();
    if (!rule) return;
    try {
      await api("/api/v1/bypass", {
        method: "POST",
        body: JSON.stringify({ rule }),
      });
      el.ruleInput.value = "";
      await loadBypass();
    } catch (e) {
      showError(e.message);
    }
  });
  el.keyForm.addEventListener("submit", async (ev) => {
    ev.preventDefault();
    showKeyMsg("", "");
    const access_key = el.accessKey.value.trim();
    if (!access_key) {
      showKeyMsg("Введите ключ", "");
      return;
    }
    if (!confirm("Заменить ключ Outline и переподключиться?")) return;
    try {
      const res = await api("/api/v1/outline", {
        method: "PUT",
        body: JSON.stringify({ access_key }),
      });
      el.accessKey.value = "";
      renderOutline(res.status || res);
      showKeyMsg("", res.warning || "Ключ применён");
      await loadStatus();
    } catch (e) {
      showKeyMsg(e.message, "");
    }
  });

  el.token.value = getToken();
  setAuthPill(!!getToken());
  loadVersion();
  loadAll();
  // refresh status periodically
  setInterval(() => {
    if (getToken()) loadStatus();
  }, 15000);
})();
