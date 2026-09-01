# 状态持久化

> 没有数据库、没有 ORM、没有迁移工具。状态是 data dir 里若干 JSON 文件，原子写（temp + rename）。

---

## 目录

| 模式 | 默认目录 |
|------|----------|
| `NETWORK_MODE=container` | `/data/conduitvpn` |
| `NETWORK_MODE=host` | `./data` |
| `--demo` | `.conduitvpn-demo`（可被 `CONDUIT_DATA_DIR` 覆盖） |

`--data-dir` 优先于 `CONDUIT_DATA_DIR`。`state.SecureDir` 把目录收成 `0700`、普通文件 `0600`，且不跟随符号链接。

host 模式若目录是空的，`EnsureStartupTemplate` 写入不含密码的 `conduitvpn.env.example`。已有文件时不要覆盖。

---

## 文件

| 文件 | 类型 | 读写方 |
|------|------|--------|
| `nodes.json` | `[]*node.Node` | `SaveNodes` / `LoadNodes`；含 `config_text`（磁盘需要它来重连） |
| `ip_purity.json` | map[string]purity.Record | `SavePurity` / `LoadPurity`；按 IP 缓存来源/属性/国家/邮编，缺文件当空 map |
| `blacklist.json` | `map[string]BlacklistEntry` | host 名 → `{reason, marked_at}` |
| `route.json` | `Route{mode,country,node}` | UI 设置优先于 env |
| `ui_auth.json` | 用户名 + PBKDF2 盐/哈希 + `secret_path` | 启动时 `EnsureAuthConfigured` |
| `hy2/` | sing-box 入站配置与自签证书 | `internal/hy2` |
| 隧道临时 `.ovpn` / auth | manager `prepareFiles` | 按节点写入 data dir |

实现：`internal/state/store.go`。所有 JSON 写走 `writeJSON`：`MarshalIndent`、同目录 temp、`chmod 0600`、`Rename`。不要 `os.WriteFile` 直接覆盖这些状态文件。

---

## 凭据

- 生产：必须同时提供 `UI_USER` 与 ≥16 位 `UI_PASSWORD`。缺失则启动失败，不要生成随机密码再打到日志。
- Demo / 测试：`EnsureAuthConfigured(..., demo=true)` 允许短密码；`EnsureAuth` / `EnsureAuthWithDefaults` 仍给测试用。
- 落盘时清掉 `password` 明文。遗留明文文件在下次 `EnsureAuthConfigured` 时迁移为哈希。
- `secret_path` 一旦生成就保持稳定（24 位 hex）。不要每次启动轮换，否则书签和管理路径会失效。

`VerifyPassword` 校验迭代次数必须等于 `passwordIterations`（600000）。改迭代次数等于让现有部署全部无法登录，需要显式迁移，不要默默改常量。

---

## 路由持久化优先于环境变量

`manager.New` 先用 `cfg.RouteMode/Country/Node`，若 `LoadRoute` 成功且 `Mode != ""` 则覆盖。UI 的 `PUT /api/route` 写 `route.json` 并往 `modeCh` 发信号。不要让 env 在进程活着时盖掉 UI 选择。

---

## 黑名单

- 键是 `HostName`
- 不自动过期
- 固定模式锁定的节点调用 `markBlacklisted` 仍可能写入，但 `connectLoop` 对 `isFixedNode` 会跳过「已拉黑则 continue」——锁定节点一直重试
- 黑名单验证结果是内存态（`BlacklistTestStatus`），故意不落盘；重启必须重测

---

## 反模式

- 引入 SQLite / Bolt / 自己再写一套非原子 JSON
- 把 `nodes.json` 里的 `config_text` 原样发到浏览器（磁盘保留、API 脱敏）
- 用 `ioutil.WriteFile` 写凭据
- 在多个包里复制 `writeJSON`；新持久化文件加在 `state.Store` 上
