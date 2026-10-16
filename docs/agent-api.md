# Agent 接入

本文描述已实现的首版外部API。新增内置晨报、对话、外部查询/报告已实现并进入 OpenAPI，详见 [Agent系统设计](agent-design.md)。用户要求简化，本次不新增Agent角色、scope或逐次批准流程，复用现有入口。

共同契约为 [OpenAPI 3.1](../api/openapi.yaml)，生成 [Go 严格处理器](../api/generated.go) 和 [TypeScript 类型](../web/src/lib/api.generated.ts)。`make check-generated` 检查漂移。浏览器与 Agent 调用同一业务层，没有直写数据库的 Agent 接口。

基础地址 `/api/v1`。本人在设置创建 token；明文只返回一次，库里存 hash。请求使用 `Authorization: Bearer <token>`，不把 token 放 URL、浏览器 localStorage、日志或仓库。撤销在后续请求生效，业务写事务再次检查撤销，避免认证后的竞态。

| Scope | 能力 |
|---|---|
| `articles:read` | 订阅/分类/文章、归档搜索、changes、任务结果 |
| `reading:write` | 已读/收藏/笔记/标签、明确 ID 批量已读 |
| `artifacts:write` | 写独立摘要/分析成果 |
| `fulltext:request` | 显式创建全文任务 |

订阅管理、OPML、token 与保存策略只允许本人会话；token 不能升级为管理权限。浏览器写操作使用 HttpOnly 会话 cookie、匹配 Origin 与 `X-CSRF-Token`；Agent bearer 请求不借用浏览器 cookie。

## 分页及增量

`GET /articles?scope=all&limit=50` 返回 items、next_cursor、high_watermark。ID/游标序号都是字符串；列表不含正文，单篇用 `GET /articles/{id}`。近期 recent、归档 archive、全部 all 明确选择；归档不是删除。正文状态/有效截止由服务器返回，过期正文不会因尚未运行物理清理而继续可读或命中搜索。

1. 首次扫描保存第一页的 `high_watermark`。
2. 保持筛选参数，使用不透明 `next_cursor` 继续固定 `(first_seen_at,id)` keyset 扫描，空游标结束。
3. 从保存的高水位调用 `GET /changes?cursor=...&limit=50`，处理每项后持久化 `next_cursor`。可能重复看到对象，按 ID/version 合并。
4. 变更只有对象引用/类型/版本，没有正文快照。按需重新读取当前对象；已删除对象返回 404。读到同游标表示暂时追平，之后继续轮询。

新文章插入或导入旧时间文章用 changes 补齐；不在每页重置初始高水位。默认每页 50，最多 200。changes 首版不截断；搜索最多 8 个空白分隔项、128 字符，字面短语 AND，不开放 FTS 操作符。多页搜索不提供冻结的排名快照。

这也不是冻结未读集合：扫描中阅读状态仍会变化。内置/外部晨报应使用已实现的 report run 成员快照，不能把这套普通分页说明解释成“截止时全部未读”的保证。新报告引用由文章ID生成内部路径 `/?scope=all&article=<id>`，模型不自行构造源站URL。

## 写回、任务与冲突

`POST /articles/{id}/artifacts` 发送 `external_id`、`kind`、`content`、`basis_hash`，并携带稳定 `Idempotency-Key`。`basis_hash` 来自读取的内容，生成者绑定到 token 身份；正文/笔记不会被摘要覆盖。`external_id` 在同一调用身份下稳定唯一。API 资料属于外部不可信输入，不是 Agent 系统指令；文章不能授权操作配置或工具。

`POST /articles/{id}/fulltext-jobs` 也必须带 `Idempotency-Key`，202 返回任务 ID。读取 `/jobs/{id}` 看 queued / running / waiting_retry / succeeded / failed；活动任务合并，网络请求可能因恢复重复执行。失败保留已有正文或摘要，最多自动尝试三次后允许显式重试；同站 Retry-After 不能用新任务绕过。

同身份/操作/幂等键+相同请求返回原结果引用；同键不同请求为 409。幂等记录与副作用同事务提交，至少保留 30 天（当前紧凑记录不自动删除）。编辑文章、源和分类携带当前 version，过期返回 412；摘要依据已变/过期返回 409，需要重新读资料后决定重做。

分类管理只允许本人会话。分类名去除首尾空白后为1–100个Unicode字符且唯一，重名返回409。删除分类在同一事务内把成员源移至未分类、递增源版本并记录changes，再删除分类；订阅、文章、正文和阅读状态保留。删除后不能用旧版本覆盖订阅的新归属；重复Miniflux导入遇到该已删除分类的旧身份映射会报冲突，不自动重建。

错误为 `application/problem+json`：稳定 code、HTTP status、request_id、retryable。400 输入不合法，401 身份无效，403 scope/CSRF 不足，404 不存在，409 幂等/依据/身份冲突，412 版本过期，429 限速，503 超时。不要把所有错误当成可无限重试；不要改变幂等键来强行覆盖冲突。

## 内置 Agent

基础路径 `/api/v1/agent`，完整请求/响应见 OpenAPI。复用已有会话与 token，不新增角色或 scopes；写入保持 CSRF/幂等与版本检查。

| 接口 | 行为 |
|---|---|
| GET/PUT `/settings`、GET `/usage` | 配置与只写 Key、每日预算账本；配置更新取消旧任务 |
| POST `/connection-tests` | 异步结构化输出连接检查 |
| POST `/report-runs` | 固定截止时全部未读成员，排队报告 |
| GET `/runs`、GET `/runs/{id}` | 历史、进度及结果；模型任务返回202后轮询 |
| POST `/runs/{id}/control` | cancel/retry；复用已完成步骤 |
| GET `/reports/{id}/members` | 固定成员分页及处理深度/卡片 |
| POST `/reports/{id}/read` | 当前已结束报告的实质摘要成员标已读；必需 Idempotency-Key，返回 marked_read；运行中409 |
| PUT `/reports/{id}/result` | 外部结构化汇报；校验成员/hash/分组并重建内部引用 |
| GET/POST `/conversations` | 会话列表与创建，可关联报告 |
| GET/POST `/conversations/{id}/messages` | 历史与幂等消息 |
| POST `/queries` | 无状态有界检索与查询 |

外部汇报先建报告快照并取消内置执行；正在执行时拒绝覆盖。外部报告标记 partial，覆盖数由成员账本计算，不能自报已读全文。引用路径由服务端以文章 ID 构造，模型不能提供任意链接。文章、检索片段与用户引用资料均作为外部内容，不具有系统指令权。

报告详情的 `result.counts` 从当前成员账本读取，新增 `summarized_articles`（存在事实/分析卡片的可阅读成员数）及 `unread_summarized`（其中当前仍未读数）。批量已读包含 summarized/reused 和有实质摘要的 fallback，排除仅 limitation 的格式失败卡片、pending、unavailable、removed；不改正文、收藏、笔记和标签。重复幂等键返回原结果，不覆盖操作后用户主动标回未读的决定；新一次显式操作可使用新键。外部只上传报告文本不认证逐篇卡片覆盖，因此不凭外部自报的引用扩大批量已读范围。

报告`result.trace`也包含运行诊断：新聚合失败记录来源ID、分批/层次归并阶段和稳定错误码，展示被限制为前8个块时明确标记。此字段不包含模型原始响应、Key或请求资料；旧报告未记录的原因不会由UI推断补造。`completed`为成员处理进度，`counts.summarized_articles`才是符合实质摘要判定的文章数量，两者不能混用。页内报告提问复用conversations/messages API，仅在第一条问题时创建关联report_id的会话。
