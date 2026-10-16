# 文档索引

文档按使用、开发和维护场景组织。首次使用从[项目 README](../README.md)开始。

| 阅读目标 | 文档 | 维护边界 |
|---|---|---|
| 启动与功能概览 | [根 README](../README.md) | 可直接复现的本机入口、当前功能 |
| 需求与验收场景 | [product.md](product.md) | R-01–R-29、A01–A19；需求不等于全部质量目标已达成 |
| 完成情况与实际证据 | [implementation.md](implementation.md) | 最新检查、已实现机制、未完成的质量/性能验证 |
| 技术与版本 | [technology.md](technology.md) | 选择理由、固定工具链、工程预算 |
| 模块和数据规则 | [architecture.md](architecture.md) | 单进程职责、SQL、生命周期、事务、API |
| 重要决定 | [decisions.md](decisions.md) | 稳定决定编号及旧提案被替代的原因 |
| Agent 实现 | [agent-design.md](agent-design.md) | 晨报、对话、RAG、预算、恢复与引用 |
| 提示词及格式 | [agent-prompts.md](agent-prompts.md)、[schema](agent-schemas/) | 设计说明与样本；实际执行模板在 internal/agent/prompts |
| 外部接口 | [agent-api.md](agent-api.md)、[OpenAPI](../api/openapi.yaml) | 网页和外部 Agent 共用业务规则，生成代码检查漂移 |
| 本机运维与迁移 | [operations.md](operations.md) | 快照、导入、备份恢复、本机 Linux 验证 |
| 正式部署 | [production.md](production.md) | 配置、初始化、反向代理、升级与回退 |
| 前端开发 | [web/README.md](../web/README.md) | 快捷键、布局、浏览器测试、安全与构建 |
| 安全与敏感数据 | [security.md](security.md) | 凭据、配置、备份与仓库检查 |
| 调研背景 | [research.md](research.md)、[agent-research.md](agent-research.md) | 有日期的参考资料，不作为现有功能证明 |
| 规模实验 | [scale.json](validation/scale.json)、[搜索实验](../spikes/sqlite-search/README.md) | 合成工作负载及独立实验，不能外推生产性能 |

功能规则见产品需求，设计理由见决策记录，测试结果与已知限制见实施记录。
