# 拾流 RSS · Shiliu

为开发者本人设计的自托管 RSS 阅读器，运行于现有 NAS。以简洁阅读、可靠自动更新、按源保存和 Agent 接入为核心。项目标识为 `shiliu-rss`，本机目录为 `<checkout>`。

**当前阶段：首版产品基线 v1.0 及本轮 UI 增补已明确，技术选型已完成（2026-10-15）。** 采用 Go + SQLite，前端为 Svelte 5 + TypeScript + SvelteKit 静态构建 + Tailwind CSS 4。NAS 单进程提供静态页面、Agent API 和可靠后台调度。已完成本地 SQLite 搜索实验及 Linux AMD64 交叉编译验证；尚未实现应用或替换 Miniflux。

图标采用用户指定的**抽象、简化石榴**：顶部冠形、圆润果体和五粒留白籽，单色石榴红、透明背景。源文件为 [icon.svg](assets/icon.svg)，用于后续网页图标和应用标识；缩小时优先保持轮廓清晰，不叠加文字或复杂装饰。

| 文档 | 用途 |
|---|---|
| [产品模块与需求](docs/product.md) | 首版功能、阅读流程、数据生命周期、Agent 能力和验收标准 |
| [技术选型](docs/technology.md) | Go/Rust 比较、选定组件、中文搜索、验证证据和待实测预算 |
| [架构设计](docs/architecture.md) | 模块边界、调度恢复、数据生命周期、API、迁移和运行维护 |
| [决策与待确认项](docs/decisions.md) | 用户已确认要求、工程选择、已撤回方案与实施待办 |
| [开源产品参考](docs/research.md) | Miniflux、FreshRSS、Stringer 的可借鉴设计和 NAS 实际约束 |
| [SQLite 搜索实验](spikes/sqlite-search/README.md) | 可重现的独立验证；不是应用脚手架 |
| [AGENTS.md](AGENTS.md) | 面向 Agent 的项目协作与开发约定 |

本项目独立于 [NAS 维护项目](docs/production.md)。NAS 项目负责机器与部署台账；这里负责阅读器产品及未来代码。**开发、测试、构建都在本机；NAS 只部署成品。** 本机开发端口使用本机 APR；NAS 没有 APR，部署端口按实际占用检查及 NAS 台账管理，不需要为本项目安装 APR。

下一阶段从数据模型、API 契约及后台抓取/清理的纵向实现开始。根目录目前没有应用启动命令；实验命令见其 README。NAS 内存、CPU、功耗与恢复目标仍需实际应用验证。

更新日期：2026-10-15。
