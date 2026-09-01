# Bootstrap Task: Fill Project Development Guidelines

**You (the AI) are running this task. The developer does not read this file.**

`.trellis/spec/` 已按 ConduitVPN 真实代码填写，不再使用空模板。后续 `trellis-implement` / `trellis-check` 按层加载这些规范。

---

## Status (update the checkboxes as you complete each item)

- [x] Fill backend guidelines
- [x] Add code examples
- [x] Add frontend layer (embed 静态资源)
- [x] Replace database template with JSON state persistence
- [x] Rewrite thinking guides for this repo
- [x] Verify no template placeholders

---

## Spec files (current)

### Backend（`.trellis/spec/backend/`）

| File | What it documents |
|------|------------------|
| `index.md` | Pre-Development Checklist + Quality Check |
| `directory-structure.md` | `cmd/` + `internal/*` 包职责与装配顺序 |
| `error-handling.md` | `fmt.Errorf`、`writeAPIError`、fail-closed |
| `logging-guidelines.md` | `logx` JSON、ring/SSE、禁止记密钥 |
| `quality-guidelines.md` | 零依赖、脱敏、测试、已知边界 |
| `state-persistence.md` | JSON 原子写、凭据、route 优先于 env |
| `network-modes.md` | container/netfix vs host/egress |
| `subprocess.md` | openvpn / sing-box 生命周期 |

已删除：`database-guidelines.md`（本仓库无 ORM / SQL）。

### Frontend（`.trellis/spec/frontend/`）

| File | What it documents |
|------|------------------|
| `index.md` | Pre-Development Checklist + Quality Check |
| `directory-structure.md` | `internal/webui/static/` + `versionedAssets` |
| `ui-conventions.md` | DOM/XSS、i18n 三语、相对 API、状态药丸 |

### Thinking guides

| File | What it documents |
|------|------------------|
| `guides/index.md` | 何时读哪份指南 |
| `guides/code-reuse-thinking-guide.md` | 拨号/日志/子进程/脱敏复用 |
| `guides/cross-layer-thinking-guide.md` | env → 文件 → API → UI 边界 |

---

## Source of truth used

- `AGENTS.md` / `CLAUDE.md`
- `cmd/conduitvpn/main.go`、`internal/*` 与对应 `*_test.go`
- `internal/webui/static/{app,i18n,theme,login}.js`

已知代码与 README 差异已写入规范（状态字符串以代码为准；`GET /api/nodes` 目前未脱敏）。

---

## Completion

规范已写入。归档需开发者确认后再执行：

```bash
python3 ./.trellis/scripts/task.py finish
python3 ./.trellis/scripts/task.py archive 00-bootstrap-guidelines
```
