# 开源参考与运行约束

调研日期：2026-10-15。使用官方文档与项目仓库；这里借鉴产品取舍，不将上游实现直接视为本项目架构决定。

## 参考产品

| 产品 | 官方资料中与本项目相关的设计 | 本项目采用的启发 |
|---|---|---|
| Miniflux | 简洁响应式阅读、快捷键、分类/收藏、搜索、独立 API；后台调度可配置频率、批次和错误限制 | 保持聚焦阅读的界面；把后台调度是否正常变成可观察的产品能力；API 从首版设计 |
| FreshRSS | 提供紧凑列表、阅读视图、源状态、标签、移动客户端 API；有独立的自动更新机制和扩展能力 | 借鉴订阅状态与筛选设计；适度分类和标签；更新不依赖用户打开页面 |
| Stringer | 明确聚焦个人阅读与快捷键；提供 Fever API，并有清理旧已读文章的机制 | 控制产品体积；保存策略必须有明确例外，不照搬按天直接删除历史的行为 |

资料：

- [Miniflux 功能](https://miniflux.app/features.html)
- [Miniflux 配置](https://miniflux.app/docs/configuration.html)
- [Miniflux API](https://miniflux.app/docs/api.html)
- [FreshRSS 官方仓库](https://github.com/FreshRSS/FreshRSS)
- [FreshRSS 阅读视图](https://freshrss.github.io/FreshRSS/en/users/03_Main_view.html)
- [FreshRSS 自动更新](https://freshrss.github.io/FreshRSS/en/admins/08_FeedUpdates.html)
- [Stringer 官方仓库](https://github.com/stringer-rss/stringer)

Miniflux 当前官方文档描述：解析错误达到配置阈值后可停止轮询，等待手动刷新；调度也受批次和周期影响。因此新产品需要有限频率的自动恢复、错误可见性，以及“最近尝试”和“最近成功”的区分。这是由文档得到的设计启发，不能单凭它断言旧实例的具体故障原因。

FreshRSS 的多用户、扩展和多客户端能力体现可扩展性；本项目只取需要的个人阅读能力。Stringer 当前仓库说明其技术栈包含 Rails、PostgreSQL、Hotwire 和 GoodJob，产品简洁不等于部署成本必然最低。没有用产品名替代实机性能比较。

## 资源约束与迁移启发

设计面向低功耗四核 x86 NAS、8 GB 内存和已有媒体/下载服务的环境。后台并发与请求有上限，空闲不忙轮询，避免为个人阅读引入重型常驻依赖。技术、备份和恢复选择见[技术选型](technology.md)与[架构](architecture.md)。

曾对实际 Miniflux 做只读观察，确认需区分错误计数、最近检查和实际恢复，不单凭统计断言故障原因。真实源/文章数量与运行配置留在私有记录。迁移只能处理旧系统仍存在的数据；OPML 适合订阅交换，不能替代文章、已读和收藏的一致快照。当前完整迁移实现及验证边界见[实施记录](implementation.md)。
