# 节点列表 IP 纯净度检测

## Goal

管理员更新节点后，能在列表里看到每个节点 IP 的来源、属性、国家和邮编，按这些字段筛选，并且自动选路优先连接非机房节点。

## Background

现有节点表只有 VPNGate 自报国家，过滤是单一文本搜索。自动选路按分数顺序连接，无法避开机房 IP。ipinfo widget 可返回 ASN 类型、隐私标记、国家和邮编；真实 VPNGate 出口几乎都带 vpn=true，其中既有家宽也有机房。

## Requirements

- R1. 点击「更新节点」完成拉取和测速后，后台自动查询当前节点池 IP 的纯净度，不得阻塞测速和建立隧道。
- R2. 列表新增三列：IP来源、IP属性、邮编；原国家列在有检测结果时改显示 ipinfo 国家，否则仍显示 VPNGate 国家。未查出时显示占位，查出后无需整页刷新。
- R3. IP来源显示为家宽 / 机房 / 企业 / 教育（由 asn.type 或 company.type 映射）。IP属性显示 vpn/proxy/tor/relay/hosting/mobile/anycast 等标签。不做单独的「纯净」徽章。
- R4. 列表可按展示国家、IP来源、IP属性筛选，并保留现有文本搜索。
- R5. 按 IP 缓存查询结果到 data dir；限速并发；遇到 429 退避。同一 IP 后续更新节点时走缓存。
- R6. auto / country 模式优先连接已确认的非机房节点。没有非机房、尚未查出或查询失败时，仍按现有顺序选，避免中断。
- R7. 固定 IP 模式不受纯净度排除影响。用户锁定机房节点仍可连接。
- R8. 「固定国家地区」仍按 VPNGate CountryShort 选路，不改成 ipinfo 国家。
- R9. 查询失败不得让节点从列表消失，也不得让网关停止。
- R10. `GET /api/nodes` 返回纯净度字段时必须去掉 OpenVPN `config_text`。
- R11. `--demo` 不访问 ipinfo。

## Out of Scope

- 不引入 ipinfo 官方 token，不新增 Go 第三方依赖。
- 不把 OpenVPN 配置或证书发到 ipinfo。
- 不按 vpn=true 排除自动候选。
- 纯净度结果返回时，不强制断开当前已连接节点。
- 不把国家路由改成 ipinfo 国家。
- 不做单独的「纯净」综合徽章。

## Acceptance Criteria

- AC1. 更新节点后，列表自动出现来源、属性、邮编；国家列在有结果时显示 ipinfo 国家。对应 R1 R2 R3。
- AC2. 家宽样本能看出家宽且无 hosting；机房样本能看出机房。对应 R3。
- AC3. 可按国家、来源、属性把列表筛到符合条件的节点，文本搜索仍可用。对应 R4。
- AC4. 同时存在已确认非机房和机房时，自动模式先连非机房。对应 R6。
- AC5. 全是机房或尚未查出时，网关仍能连上。对应 R6 R9。
- AC6. 锁定机房节点时仍可连接。对应 R7。
- AC7. 固定国家模式仍按 VPNGate 国家筛选候选。对应 R8。
- AC8. 查询 429 或失败时节点仍在列表中，该行可看出失败/未检出，网关不退出。对应 R5 R9。
- AC9. `/api/nodes` 不含 `config_text`。对应 R10。
- AC10. `--demo` 不发起 ipinfo 请求。对应 R11。

## Technical Notes

- 数据源：GET https://ipinfo.io/widget/demo/{ip}，非官方接口，已实测可返回完整 JSON，连续请求会 429。
- 机房判定：privacy.hosting、is_hosting、asn.type=hosting、company.type=hosting 任一为真。
- 缓存与节点列表分开存，避免每次覆盖 nodes.json 丢掉检测结果。
- container 模式连上隧道后默认路由走 tun；查询应尽早在 FETCHING 之后启动，失败则重试，不阻塞选路。
