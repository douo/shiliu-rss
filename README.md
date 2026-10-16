# 拾流 RSS · Shiliu

自托管的个人 RSS 阅读器，支持后台更新、按源保存、晨报与对话式 Agent。Go + SQLite 单进程，Svelte 5 / TypeScript / SvelteKit 静态前端，Tailwind CSS 4。图标为[抽象石榴 SVG](assets/icon.svg)。

当前已实现 R-01–R-29，并完成本机功能验证及一次实际部署、Miniflux 全量迁移。最新验收为 60 项桌面/手机浏览器检查；模型语义质量、真实移动浏览器和目标设备性能仍有验证边界，见[实施记录](docs/implementation.md)。仓库只保留通用配置和脱敏文档。

## 本机启动

需要支持自动工具链的 Go、pnpm 11.0.9。项目固定 Go 1.27.2、Node 24.22.0；pnpm 下载项目运行时，不替换全局 Node。

```sh
make setup
make init    # 仅首次初始化，不覆盖已有账号
make dev
```

打开 **http://127.0.0.1:41002**。登录无需用户名；随机初始密码在 `.local/admin-password`，文件仅本机用户可读。开发库为 `.local/reader.db`。

固定开发端口为 API `41001` / Vite `41002`，保存在 [config/dev.env](config/dev.env)。换用端口时同时更新该文件并核对占用；启动不会自动重新分配。部署配置独立于开发配置。

运行嵌入前端的单进程成品（与 dev 二选一）：

```sh
make run
```

打开 **http://127.0.0.1:41001**。本机 HTTP 使用非 Secure cookie；正式服务使用 HTTPS、准确的 `SHILIU_ORIGIN` 和 `SHILIU_COOKIE_SECURE=true`。

## 阅读与整理

- RSS / Atom / JSON Feed、分类、OPML；默认每 5 小时检查并加入抖动，支持条件请求、退避、持久任务和重启续作。
- 首页晨报，近期、收藏、归档与搜索共用侧栏。文章按发布时间倒序；分类可展开源，源显示全量未读及异常详情。源/分类列表顶部可查看详情并直接编辑。
- 列表未读标签按完整筛选范围统计，不受分页或阅读标签影响。“本页标为已读”仅操作已加载条目；晨报末尾可标读该报告已实际总结的文章。
- 正文、笔记、标签和 Agent 摘要分别保留。普通正文 90 天清理，收藏/长期源豁免；迁入正文有 30 天保护。日常隐藏不等于已读或删除。
- 显式全文获取、危险 HTML 清理、出站地址检查、严格脚本 CSP、gzip 静态资源。
- 可撤销 API token、分页与 changes 增量、幂等写回及版本/内容依据冲突检查。

桌面快捷键：`↑/↓` 导航，`c` 侧栏，`a` 已读，`s` 收藏，`e` 原文，`q` 搜索，`Esc` 返回。分配避让 Vimium 默认映射；输入、弹窗和表单内保留原生操作，自定义扩展映射仍可能冲突。

## Agent

在「设置 → Agent 配置」填写 Base URL、协议、模型和 API Key，再启用。未配置时停用。支持 OpenAI 兼容 Responses / Chat Completions；Key 只写不回显，留空保留，也可从 `SHILIU_AGENT_API_KEY_FILE` 指定的受保护文件读取。

默认每天 Asia/Shanghai 08:00 汇总全部未读（含归档）。重要源按全文处理并按策略补抓，普通/次要源有界截取；按分类、来源、重要度和未读量聚合，引用打开系统内部文章。返回正文前的晨报位置，或在右侧/底部围绕报告提问。

日 token/调用预算、运行窗口及续作可配置。缺文、格式失败和预算不足明确显示覆盖缺口。对话使用有界词法 RAG；尚无语义向量、重排或任意工具执行器。外部系统可使用同一[Agent API](docs/agent-api.md)。

## 验证与构建

```sh
make check-generated  # OpenAPI → Go / TypeScript 生成漂移
make check            # go vet + Svelte / TypeScript
make test             # Go race 与集成测试
make test-web         # 先停 dev/run；隔离库运行 60 项浏览器检查
make build-linux      # 本机静态交叉编译 Linux AMD64 v1
./scripts/check-privacy --history
```

PostgreSQL 一致快照测试需要本机 PostgreSQL CLI；缺少时明确跳过该项，不算完整迁移验收。Linux 镜像运行检查和部署步骤见[操作说明](docs/operations.md)。合成规模数据见 [scale.json](docs/validation/scale.json)，不能用来宣称目标设备的性能或功耗达标。

## 文档与部署

从[文档索引](docs/README.md)按角色阅读：

| 内容 | 入口 |
|---|---|
| 产品范围与验收场景 | [产品需求](docs/product.md)、[实施记录](docs/implementation.md) |
| 技术、架构与决策 | [技术选型](docs/technology.md)、[架构](docs/architecture.md)、[决策](docs/decisions.md) |
| Agent 设计与契约 | [系统设计](docs/agent-design.md)、[提示词](docs/agent-prompts.md)、[API](docs/agent-api.md)、[OpenAPI](api/openapi.yaml) |
| 开发、迁移与运维 | [前端说明](web/README.md)、[操作说明](docs/operations.md)、[部署指南](docs/production.md) |
| 敏感信息与发布边界 | [安全与脱敏](docs/security.md) |

开发、测试、构建和恢复演练在本机，NAS 仅接收已验证的发布产物。[部署模板](deploy/compose.yaml)要求显式配置镜像、HTTPS 地址、端口和备份目录。数据库、真实订阅、凭据、截图、日志和私有运维记录均不应提交；实际实例地址和存储布局不属于仓库配置。
