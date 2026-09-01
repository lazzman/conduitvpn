# 实现清单

## 顺序

1. 在 `internal/purity` 实现客户端、JSON 解析、机房判定和来源/属性提取；用家宽/机房/VPNGate 抽样做 fixture 测试。
2. 在 `internal/state` 增加 `ip_purity.json` 的 Load/Save，走现有 writeJSON；补坏文件/空文件测试。
3. 在 `internal/manager`：fetchAndBench 成功后异步 Enrich；demo 跳过；selectCandidates 对 auto/country 做非机房优先重排；补选路测试。
4. 在 `internal/webui`：GET /api/nodes 脱敏并合并 purity；state 增加 pending 计数；补 API 测试确认无 config_text。
5. 改 `internal/webui/static`：表头、渲染、筛选、轮询、i18n（zh-CN/zh-TW/en）、colSpan。
6. 运行 `make vet` 与相关包测试。

## 校验

- go test ./internal/purity ./internal/state ./internal/manager ./internal/webui ./internal/node
- make vet
- 手工：更新节点后列逐渐填满；按机房筛选；自动模式优先非机房；锁定机房仍可连。

## 风险点

- `internal/webui/webui.go` 的 apiNodes：改时必须脱敏。
- `internal/manager/manager.go` 的 selectCandidates：fixed 模式不能被重排丢掉。
- `internal/webui/static/app.js`：禁止 innerHTML。
- 不要改 go.mod 增加依赖。

## 回滚

删除 purity 包、ip_purity.json 读写、选路重排和 UI 列即可回到原行为。缓存文件可留在 data dir 不影响旧二进制。

## 开始前

实现阶段先读 trellis-before-dev，再改代码。用户批准本规划后才运行 task.py start。
