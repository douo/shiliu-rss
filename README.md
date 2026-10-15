# 拾流 RSS · Shiliu

一个自托管的个人 RSS 阅读器：可靠后台更新，清爽阅读，按源保存，正式 Agent API。Go + SQLite 单进程，Svelte 5 / TypeScript / SvelteKit 静态前端，Tailwind CSS 4。图标使用 [抽象石榴 SVG](assets/icon.svg)。

**首版及内置 Agent 已实现并完成本机验证（2026-10-16）。** 已正式部署到 NAS，并完成 Miniflux 全量迁移与入口切换。入口 **https://reader.example.com**，报告与对话在 **/agent**；详情见 [生产记录](docs/production.md)。已实现的功能及验证边界见 [实施验收记录](docs/implementation.md)。

## 本机启动

需要 Go 自动工具链、pnpm 11.0.9；项目固定 Go 1.27.2、Node 24.22.0，pnpm 自动下载项目 Node，不更改全局 Node。首次准备：

```sh
make setup
make init
make dev
```

打开 **http://127.0.0.1:41002**。初始随机密码在本机 `.local/admin-password`，只有本机用户可读；登录无用户名。`make init` 仅在首次初始化时执行，不覆盖已有账号。本次已完成本机初始化，可以直接 `make dev`。

API `41001`、Vite `41002` 是本机 APR 分配的固定端口，固化在 [config/dev.env](config/dev.env)，启动不再调用 ensure。开发库位于 `.local/reader.db`，所有私人内容、密码、数据库和备份都被 Git 忽略。源正文由后台抓取，普通公网源无需开放内网访问；测试或明确的内网源需在源设置填写允许的具体主机。

运行嵌入静态前端的单进程成品（与 dev 二选一）：

```sh
make run
```

打开 **http://127.0.0.1:41001**。本机 HTTP 使用非 Secure cookie；部署 HTTPS 必须设 `SHILIU_COOKIE_SECURE=true` 和准确的 `SHILIU_ORIGIN`，不信任任意转发头。

## 已实现

- RSS / Atom / JSON Feed、分类、OPML、源设置与健康；默认 5 小时 ±1 小时自动检查，条件请求、退避、持久任务/租约、重启恢复与同站限流。
- 近期/未读/收藏/归档、中文及英文搜索、可折叠侧栏、来源图标与跳转、分类及成员管理、手机布局、↑/↓和避让 Vimium 默认映射的单键、笔记/标签/Agent 摘要。新内容提示后主动更新列表，保留阅读位置。
- 隐藏与清正文分开；普通源 90 天、收藏及长期源豁免、迁入正文 30 天保护；清理同时处理衍生文本和索引，普通轮询不会填回已清正文。
- 显式全文获取及重要源晨报按策略自动补抓；失败保留已有内容。HTML 净化、出站地址检查、体积/并发限制、严格脚本 CSP、gzip 静态资源。
- 本人密码/会话、独立可撤销 scoped token、稳定分页、增量 changes、事务幂等、版本与内容依据冲突检查。
- Miniflux 2.3.0 一致快照导出、预览/对账/可重试导入；一致备份/恢复、升级前备份、Linux AMD64 成品与部署模板。

## Agent 配置

在「设置 → Agent」填写 Base URL、协议、模型名和 API Key，再启用。未配置时停用。支持 OpenAI 兼容 Responses / Chat Completions；Key 只写不回显，留空保留，或通过 `SHILIU_AGENT_API_KEY_FILE` 提供受保护文件。默认每天北京时间 08:00 汇总全部未读（含归档），来源可设重要/普通/次要；重要源完整处理，其他源有界截取。

侧栏「Agent」打开 `/agent`，查看报告、覆盖明细和会话。引用打开系统内文章；全文失效不用于检索。每日 token/调用预算、续作和运行时限可配置；RAG 使用有界全文词法检索，尚无语义向量与重排。

## 可重复验证

```sh
make check-generated  # OpenAPI → Go/TS 生成漂移
make check            # go vet + Svelte/TypeScript
make test             # Go race / SQLite / 抓取 / API / 调度 / 导入恢复
make test-web         # 先停 dev/run；隔离临时库启动 Go 静态成品，42 项浏览器验收
make build-linux      # 本机静态交叉编译 Linux AMD64 v1
```

PostgreSQL 一致快照测试使用本机临时 Unix socket；本机已有 PostgreSQL 18.6 CLI。没有相应工具时该单项明确 skip，不能当作完整迁移验收。详见 [操作说明](docs/operations.md)。

完整合成规模及维护演练：

```sh
go run ./cmd/bench-reader -maintenance -output /tmp/shiliu-scale.json
```

使用 200 个源、10 万文章、2 万正文（约 500 MiB HTML），临时库结束自动删除。已记录的本机结果见 [scale.json](docs/validation/scale.json)：列表 p95 0.657 ms、中文搜索 p95 31.8 ms，备份 10.09 秒、恢复 14.57 秒。它们是本机合成场景结果，**不是 NAS 性能、混合负载峰值内存或功耗结果**。

## 文档与部署边界

| 文档 | 内容 |
|---|---|
| [产品需求](docs/product.md) | R-01–R-18、A01–A10、首版范围 |
| [实施验收](docs/implementation.md) | 完成状态、实际检查、未验证事项 |
| [技术选型](docs/technology.md) / [架构](docs/architecture.md) | 约束、版本、模块与不变量 |
| [决策记录](docs/decisions.md) | 用户决定与实现中的兼容修订 |
| [OpenAPI](api/openapi.yaml) / [Agent 接入](docs/agent-api.md) | 共享契约、scope、分页与幂等 |
| [操作说明](docs/operations.md) | 导入、备份恢复、Linux 检查、部署准备 |
| [前端说明](web/README.md) | 构建、键盘/手机交互、浏览器验证 |

开发、测试、构建和恢复演练全部在本机。NAS 仅接收发布产物；部署时再核对实际占用、配置端口并登记 NAS 台账，不在 NAS 安装 APR。正式迁移采用独立数据库预览与对账，切换后保留 Miniflux 数据及回退依据。生产步骤见 [运行说明](docs/production.md)。
