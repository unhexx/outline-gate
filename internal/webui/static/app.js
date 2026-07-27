(function () {
  const TOKEN_KEY = "outline_gate_ui_token";

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
  };

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
            <td>${escapeHtml(kindLabel(r.kind))}</td>
            <td><button type="button" class="danger" data-rule="${escapeAttr(r.rule)}">Удалить</button></td>
          </tr>`
      )
      .join("");
    el.rulesBody.querySelectorAll("button[data-rule]").forEach((btn) => {
      btn.addEventListener("click", () => removeRule(btn.getAttribute("data-rule")));
    });
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

  async function loadOutline() {
    try {
      const st = await api("/api/v1/outline");
      renderOutline(st);
    } catch (e) {
      el.outlineStatus.textContent =
        "Ошибка: " + e.message + (e.status === 401 ? " (проверьте токен)" : "");
    }
  }

  async function loadAll() {
    showError("");
    el.status.textContent = "Загрузка…";
    await loadOutline();
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
    } catch (e) {
      el.status.textContent = "Ошибка: " + e.message + (e.status === 401 ? " (проверьте токен)" : "");
      if (e.status === 401) {
        el.rulesBody.innerHTML = '<tr><td colspan="3" class="muted">Требуется токен</td></tr>';
      }
    }
  }

  async function removeRule(rule) {
    if (!confirm("Удалить " + rule + "?")) return;
    try {
      await api("/api/v1/bypass?rule=" + encodeURIComponent(rule), { method: "DELETE" });
      await loadAll();
    } catch (e) {
      showError(e.message);
    }
  }

  el.saveToken.addEventListener("click", () => {
    setToken(el.token.value.trim());
    loadAll();
  });
  el.clearToken.addEventListener("click", () => {
    el.token.value = "";
    setToken("");
    loadAll();
  });
  el.refresh.addEventListener("click", loadAll);
  el.apply.addEventListener("click", async () => {
    try {
      await api("/api/v1/bypass/apply", { method: "POST" });
      await loadAll();
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
      await loadAll();
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
      await loadOutline();
    } catch (e) {
      showKeyMsg(e.message, "");
    }
  });

  el.token.value = getToken();
  loadAll();
})();
