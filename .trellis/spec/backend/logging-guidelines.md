# 日志规范

> 全进程只走 `internal/logx`。它同时写 stdout JSON 行，并喂给 Web UI 的环形缓冲 / SSE。

---

## API

```go
logx.Init(cfg.LogLevel) // debug | info | warn | error，其它值视为 info
logx.Debug(msg, kv...)
logx.Info(msg, kv...)
logx.Warn(msg, kv...)
logx.Error(msg, kv...)
```

`kv` 必须是成对的 `key, value`。`error` 值会被收成 `err.Error()` 字符串（`error` 没有导出字段可序列化）。

不要引入 `log/slog` 新入口，不要 `fmt.Println` 打运行日志。配置校验失败可以在 `main` 里写 stderr，因为那时 `logx.Init` 还没跑。

---

## 级别

| 级别 | 用在 |
|------|------|
| `debug` | 高频热路径：测速命中、代理拨号失败细节 |
| `info` | 状态迁移、监听地址、选路结果、hy2/sing-box 就绪 |
| `warn` | 可恢复失败：拉取失败回落到缓存、netfix 跳过、节点失败并拉黑 |
| `error` | 无法继续的监督循环问题、解析失败、路由模式下没有候选 |

状态机每次 `setState` 打一条 `logx.Info("state", "state", ..., "detail", ...)`。前端状态药丸依赖这些可观察迁移，不要改成只改内存。

---

## 字段约定

常见键名沿用现有代码：`err`、`host`、`dir`、`addr`、`state`、`detail`、`mode`、`country`、`node`、`port`、`count`、`ms`、`type`。

消息用短英文短语，与仓库现状一致（`fetch failed; falling back to cached nodes`、`tunnel healthy`）。不要改成一套全新的中文日志格式；UI 文案走 i18n，日志保持机器可读。

环形缓冲最多 1000 条；SSE 订阅通道容量 256，满则丢（`select default`）。新增订阅必须保存 `Subscribe()` 返回的 unsubscribe，并在客户端断开时调用——`apiLogStream` 已这样做。

---

## 禁止记录

- OpenVPN `config_text`、证书、私钥、`auth-user-pass` 文件内容
- `UI_PASSWORD` / `LOCAL_PROXY_PASS` / `HY2_PASSWORD` 明文（demo 启动日志可以打 demo 用户名和密码，见 `main.go` 的 demo 分支；生产不要学）
- 完整订阅 URI（可打 `tag` / `server` / `type` / `index`）
- 原始 CSV 行

`logx` 不会自动红线这些字段。调用方负责不传。

---

## SSE 形态

`GET /api/logs/stream` 发送：

```text
data: {"type":"log"|"state","payload":...}
```

`state` 快照里的 `current_node` / `target_node` 必须先 `sanitizeNode`。不要把 `logx` 的 ring map 原样扩展成带二进制或超大字段的条目。
