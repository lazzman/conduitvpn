# 设计：节点 IP 纯净度

## 边界

- 新建 `internal/purity`：请求 ipinfo、解析、判定机房。无 IO 之外的选路决策。
- `internal/state`：新增 `ip_purity.json` 的原子读写。
- `internal/manager`：fetchAndBench 之后异步富集；selectCandidates / connectLoop 优先非机房。
- `internal/webui`：节点 API 合并纯净度并脱敏；前端加列和筛选。
- 不改 OpenVPN、egress、netfix、hy2。

## 数据流

1. 更新节点：拉取 CSV → 测速 → 写入 nodes.json。
2. manager 对当前池里尚未缓存的 IP 启动限速查询（demo 跳过）。
3. 每查出一个 IP，原子更新 ip_purity.json。
4. GET /api/nodes 读 nodes.json，去掉 config_text，按 IP 合并纯净度。
5. 前端轮询节点列表直到 pending 为 0。
6. auto/country 选路时读取缓存：已确认非机房排在前面，其余保持原分数顺序。

## 缓存合同

`ip_purity.json` 为 map[ip]Record：

- source: isp / hosting / business / education / 其他原始 type
- hosting: bool
- attrs: 为真的标记列表（vpn、proxy、tor、relay、hosting、mobile、anycast、anonymous、satellite）
- country、postal、city、region、org
- checked_at、error

命中且 error 为空则不再请求。失败记录可在下次更新节点时重试。

## 查询

- URL：https://ipinfo.io/widget/demo/{ip}
- 标准库 net/http；并发 2；429 则退避。
- 只传节点公网 IPv4，不传 profile。
- 不阻塞 connectLoop。container 下可能走 tun，失败重试即可。

## 选路

selectCandidates 先按现有 auto/country/fixed 过滤。
fixed 原样返回。
auto/country：稳定重排为「已确认 hosting=false」+「其余（未知/失败/机房）」。
connectLoop 仍按这个顺序试。当前已连接节点不因结果到达而主动断开。

机房判定：privacy.hosting、is_hosting、asn.type=hosting、company.type=hosting 任一为真。

## API

节点对象增加 `purity`：

- status: pending / ok / error
- source、hosting、attrs、country、postal、city、region、org

GET /api/nodes 必须 sanitizeNode。
GET /api/state 可带 purity_pending 计数，供前端决定是否继续拉列表。

## UI

- 国家列：purity.country 优先，否则 country_short；title 可附带 VPNGate 国家。
- 来源列：i18n 映射 isp=家宽、hosting=机房、business=企业、education=教育。
- 属性列：标签。
- 邮编列：postal，空则城市或破折号。
- 筛选：国家、来源、属性，加上原文本搜索。
- 禁止 innerHTML。三种语言键齐全。

## 兼容

- 无 ip_purity.json 时行为与现在相同（全部视为未知，按原顺序连）。
- 国家路由仍用 VPNGate CountryShort。
- 缓存坏文件当空 map，不要让启动失败。

## 风险

- widget 接口改版或封禁：失败行可见，选路降级。
- 429：限速 + 缓存。
- 表格变宽：来源/属性/邮编三列，国家不新增列。
