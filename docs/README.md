# 文档索引

更新：2026-10-16。文档描述当前源码，历史提案明确标注为已替代；真实实例地址、订阅及运维凭据不在仓库维护。

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
| 正式部署 | [production.md](production.md) | 通用部署与升级流程；实例台账单独私有保存 |
| 前端开发 | [web/README.md](../web/README.md) | 快捷键、布局、浏览器测试、安全与构建 |
| 安全与仓库脱敏 | [security.md](security.md) | 私密文件边界、历史检查和凭据处理 |
| 调研背景 | [research.md](research.md)、[agent-research.md](agent-research.md) | 有日期的参考资料，不作为现有功能证明 |
| 规模实验 | [scale.json](validation/scale.json)、[搜索实验](../spikes/sqlite-search/README.md) | 合成工作负载及独立实验，不能外推生产性能 |

更新需求时同步产品、决策和实际实现状态。更新检查结果时注明版本、环境和未覆盖项；不要将旧截图、历史构建数字或小样本当成当前完整验收。模型效果、程序覆盖校验与正式实例正常访问分别记录。
