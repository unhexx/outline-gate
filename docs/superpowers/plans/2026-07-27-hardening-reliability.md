# Hardening & Reliability (v0.3.0) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Закрыть критические и высокие риски надёжности/безопасности outline-gate и выпустить v0.3.0 с русскими коммитами, merge в main/master, GitHub release и синхронизацией origin + github.

**Architecture:** Точечные правки в lifecycle (`main`), config/proxy/gateway, Docker entrypoint и docs. Без смены публичного API, кроме новых опциональных env (`SOCKS_ALLOW_CIDRS`). IPv6 L3 — документируем как известный gap (не реализуем полный dual-stack в этом релизе).

**Tech Stack:** Go 1.26, nftables, Alpine 3.21 Docker, GitHub Actions CI.

---

## Уже закрыто (не трогать без нужды)

| # | Рекомендация | Статус |
|---|--------------|--------|
| 7 (частично) | `SetDeadline` reset после SOCKS handshake | Уже есть в `socks5.go:237` |
| 11 | `golang:1.26-alpine` | Валидно: `go.mod` / CI `1.26.x`, образ есть |
| 12 (частично) | `apk --no-cache` | Уже в Dockerfile |
| 14 | Валидация `ROUTING_MODE` / `DIRECT_POLICY` | Уже в `config.Validate()` + `TestLoadFromEnv_InvalidMode` |

## Карта файлов

| Файл | Ответственность |
|------|-----------------|
| `cmd/outline-gate/main.go` | shutdown timeout, cfg under mu, apply retry, goto refactor, MaintainReady recover |
| `internal/config/config.go` | `SOCKSAllowCIDRs` / `SOCKS_ALLOW_CIDRS` |
| `internal/config/config_test.go` | тесты allowlist + invalid DIRECT_POLICY |
| `internal/proxy/socks5.go` | source CIDR allowlist, reject + Warn |
| `internal/proxy/socks5_test.go` | allow/deny tests |
| `internal/proxy/transparent.go` | Warn на SO_ORIGINAL_DST fail; clear deadline if set |
| `internal/gateway/gateway.go` | (при необходимости) injectable runner для тестов |
| `deploy/docker/entrypoint.sh` | prefix `ss://` \| `ssconf://` |
| `deploy/docker/Dockerfile` | комментарий port 12345, soft pin packages |
| `README.md`, `docs/routing.md`, `docs/OPERATIONS.ru.md` | IPv6 gap, SOCKS allowlist |
| `CHANGELOG.md` | секция 0.3.0 |

---

### Task 1: Таймаут shutdown на `wg.Wait()`

**Files:**
- Modify: `cmd/outline-gate/main.go` (блок после Shutdown ~319–331)

- [ ] **Step 1: Заменить бесконечный `wg.Wait()`**

```go
done := make(chan struct{})
go func() {
	wg.Wait()
	close(done)
}()
select {
case <-done:
case <-time.After(10 * time.Second):
	log.Warn("goroutines did not exit in time; continuing shutdown")
}
```

- [ ] **Step 2: `go build ./cmd/outline-gate`**
- [ ] **Step 3: Commit** `fix: таймаут ожидания горутин при shutdown`

---

### Task 2: Refactor `goto apply` + recover для MaintainReady

**Files:**
- Modify: `cmd/outline-gate/main.go` (gateway-apply горутина, MaintainReady горутина)

- [ ] **Step 1: Заменить goto на цикл ожидания**

```go
waitReady := func(maxWait time.Duration) {
	deadline := time.Now().Add(maxWait)
	for !client.Ready() {
		if ctx.Err() != nil {
			return
		}
		if time.Now().After(deadline) {
			log.Warn("applying gateway rules without tunnel ready")
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
}
// ...
waitReady(60 * time.Second)
if ctx.Err() != nil {
	return
}
// rebuildPush + Apply with retry (Task 4)
```

- [ ] **Step 2: MaintainReady с recover → errCh**

```go
go func() {
	defer wg.Done()
	defer func() {
		if r := recover(); r != nil {
			errCh <- fmt.Errorf("outline maintain: panic: %v", r)
		}
	}()
	client.MaintainReady(ctx, func(ready bool) { ... })
}()
```

- [ ] **Step 3: Commit** `refactor: убрать goto в gateway-apply, recover для MaintainReady`

---

### Task 3: Race-safe доступ к `cfg` при SIGHUP / Status

**Files:**
- Modify: `cmd/outline-gate/main.go`

- [ ] **Step 1: Все чтения `cfg` после старта — через helper под `mu`**

```go
getCfg := func() *config.Config {
	mu.Lock()
	defer mu.Unlock()
	return cfg
}
// Status:
Status: func() webui.RuntimeStatus {
	c := getCfg()
	return webui.RuntimeStatus{
		SOCKSListen:   c.SOCKSListen,
		GatewayEnable: c.GatewayEnable,
		HealthListen:  c.HealthListen,
	}
},
```

- [ ] **Step 2: `rebuildPush` и SIGHUP уже под `mu` — проверить все path**
- [ ] **Step 3: Commit** `fix: синхронизировать доступ к cfg при reload`

---

### Task 4: Retry `gw.Apply()` с backoff

**Files:**
- Modify: `cmd/outline-gate/main.go` (gateway-apply горутина)

- [ ] **Step 1: После ready — retry Apply**

```go
base := getCfg().ReconnectBase
max := getCfg().ReconnectMax
if base <= 0 {
	base = time.Second
}
if max < base {
	max = 60 * time.Second
}
delay := base
var lastErr error
for attempt := 1; ; attempt++ {
	if ctx.Err() != nil {
		return
	}
	mu.Lock()
	g := gw
	mu.Unlock()
	if g == nil {
		return
	}
	if err := g.Apply(); err == nil {
		return
	} else {
		lastErr = err
		log.Error("gateway apply failed", "err", err, "attempt", attempt)
	}
	select {
	case <-ctx.Done():
		return
	case <-time.After(delay):
	}
	delay *= 2
	if delay > max {
		delay = max
	}
	// cap attempts soft: after ~log scale keep trying until ctx cancel
	// but if never succeeds, process stays up (SOCKS may work)
	_ = lastErr
}
```

Примечание: не слать в `errCh` сразу после первой ошибки — только логировать; fatal не нужен, иначе обрываем SOCKS. Убрать `errCh <- gateway` при transient fail.

- [ ] **Step 2: Commit** `fix: retry с backoff для gateway Apply`

---

### Task 5: Сброс deadline / Warn SO_ORIGINAL_DST

**Files:**
- Modify: `internal/proxy/transparent.go`
- Modify: `internal/proxy/socks5.go` (проверить, что reset остаётся)

- [ ] **Step 1: transparent — Warn при originalDST fail**

```go
if err != nil {
	t.Logger.Warn("SO_ORIGINAL_DST failed", "err", err, "remote", conn.RemoteAddr())
	return
}
```

- [ ] **Step 2: После успешного dial перед relay — explicit clear**

```go
_ = conn.SetDeadline(time.Time{})
relay(conn, remote)
```

- [ ] **Step 3: Commit** `fix: Warn на SO_ORIGINAL_DST и сброс deadline в transparent`

---

### Task 6: Опциональный SOCKS allowlist (`SOCKS_ALLOW_CIDRS`)

**Files:**
- Modify: `internal/config/config.go`, `config_test.go`
- Modify: `internal/proxy/socks5.go`, `socks5_test.go`
- Modify: `cmd/outline-gate/main.go` (передать Allow)
- Modify: README / OPERATIONS env table

- [ ] **Step 1: Config field**

```go
// SOCKSAllowCIDRs limits SOCKS client source IPs. Empty = allow all (trusted LAN).
SOCKSAllowCIDRs []net.IPNet
```

Load via `loadCIDRs(getenv, "SOCKS_ALLOW_CIDRS", "SOCKS_ALLOW_CIDRS_FILE")`.

- [ ] **Step 2: SOCKS5.AllowCIDRs + check в handle до greeting**

```go
if len(s.AllowCIDRs) > 0 {
	ip := net.ParseIP(clientIPOf(conn))
	if !ipInAny(ip, s.AllowCIDRs) {
		s.Logger.Warn("SOCKS connection rejected: source not in allowlist", "client", clientIPOf(conn))
		return
	}
}
```

- [ ] **Step 3: Tests allow/deny**
- [ ] **Step 4: Commit** `feat: опциональный allowlist source CIDR для SOCKS5`

---

### Task 7: entrypoint + Dockerfile

**Files:**
- Modify: `deploy/docker/entrypoint.sh`
- Modify: `deploy/docker/Dockerfile`

- [ ] **Step 1: entrypoint**

```sh
case "$OUTLINE_ACCESS_KEY" in
  ss://*|ssconf://*) ;;
  *)
    echo "outline-gate: key must start with ss:// or ssconf://" >&2
    exit 1
    ;;
esac
```

- [ ] **Step 2: Dockerfile**

```dockerfile
# TRANSPROXY_LISTEN defaults to 127.0.0.1:12345 (loopback only) — do not EXPOSE.
RUN apk add --no-cache ca-certificates nftables=~1.1 iproute2 wget
```

Проверить доступный пакет на alpine:3.21 (`apk search nftables` в build, или soft pin `nftables` без жёсткой версии если ~1.1 нет — тогда `nftables` + comment).

- [ ] **Step 3: Commit** `fix(docker): валидация формата ключа и комментарии Dockerfile`

---

### Task 8: Документация IPv6 gap + SOCKS allowlist

**Files:**
- Modify: `README.md`, `docs/routing.md`, `docs/OPERATIONS.ru.md`

- [ ] **Step 1: Явно:** IPv6 не редиректится nft; dual-stack → IPv6 bypass туннеля
- [ ] **Step 2: Документировать `SOCKS_ALLOW_CIDRS`**
- [ ] **Step 3: Commit** `docs: IPv6 gap и SOCKS_ALLOW_CIDRS`

---

### Task 9: Тесты gateway reload / mock apply + invalid DIRECT_POLICY

**Files:**
- Modify: `internal/config/config_test.go`
- Create/Modify: `internal/gateway/gateway_test.go` (script stability under UpdateEngine)
- Optional: unit test для helper wait/retry если вынесен

- [ ] **Step 1: `TestLoadFromEnv_InvalidDirectPolicy`**
- [ ] **Step 2: `TestUpdateEngineReapplies`** с mock `runNFT` если сделан inject; иначе DryRun после UpdateEngine
- [ ] **Step 3: `go test ./...` && `go vet ./...`**
- [ ] **Step 4: Commit** `test: DIRECT_POLICY и UpdateEngine`

---

### Task 10: CHANGELOG + version badges → v0.3.0

**Files:**
- Modify: `CHANGELOG.md`, `README.md` (badge version)

- [ ] **Step 1: Секция 0.3.0 Fixed/Added/Security**
- [ ] **Step 2: Commit** `docs: CHANGELOG и бейджи v0.3.0`

---

### Task 11: Интеграция, release, remotes

- [ ] **Step 1:** `go test ./...` && `go vet ./...` && `go build`
- [ ] **Step 2:** Merge `fix/hardening-reliability-v0.3.0` → `master` и `main` (локально)
- [ ] **Step 3:** Push `origin` и `github` (master + main)
- [ ] **Step 4:** Tag `v0.3.0`, push tags
- [ ] **Step 5:** `gh release create v0.3.0` с notes из CHANGELOG; binary linux/amd64 если принят в проекте
- [ ] **Step 6:** Убедиться, что оба remote на одном SHA

---

## Политика коммитов

- Сообщения на **русском**, conventional prefix: `fix:`, `feat:`, `docs:`, `refactor:`, `test:`, `release:`
- Не коммитить: `deploy/compose/config/bypass.rules.txt` (локальные правила), `.superpowers/`

## Self-review checklist

1. Spec coverage: все 🔴🟠🟡 из анализа закрыты или явно N/A
2. Нет placeholder-шагов
3. Go 1.26 не «фиксим» на 1.23
4. SetDeadline SOCKS не дублируем ломающе
5. Apply retry не валит процесс на первой ошибке nft
