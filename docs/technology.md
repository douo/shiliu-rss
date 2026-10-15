# 技术选型

日期：2026-10-15。状态：**本轮工程选型已完成，尚未实现应用或部署。** 产品依据为 [v1.0 基线](product.md)。用户要求继续技术选型，并明确语言无偏好，以长期维护和低资源占用为准；下列具体组合由 Agent 据此选定，不冒称用户逐项确认过依赖。

## 结论

采用 **Go 模块化单体 + SQLite + Svelte 5 / TypeScript / SvelteKit 静态前端 + Tailwind CSS 4**。一个常驻应用进程承担静态资源、API 和有界后台任务；数据放 NAS 本地 SSD。开发、测试、构建都在本机；NAS 无需 Node.js 运行时、开发工具链或 APR。

| 层 | 选择 | 目的与约束 |
|---|---|---|
| 语言与 HTTP | Go，标准库 `net/http` / `ServeMux` | 简化并发、测试、构建和排错；业务逻辑不绑定 Web 框架 |
| 存储 | SQLite WAL；`modernc.org/sqlite`；`database/sql` | 嵌入式持久化，纯 Go 驱动便于 macOS ARM 到 Linux AMD64 构建 |
| SQL 与迁移 | 显式参数化 SQL；`pressly/goose/v3` 的嵌入式版本迁移 | 首版不用 ORM 或 sqlc；事务和生命周期逻辑直接可审阅，迁移文件随代码版本管理 |
| 搜索 | SQLite FTS5；汉字拆分 + 短语查询；contentless-delete 索引 | 支持常见两字中文词，正文清理与索引变更处于同一事务 |
| 前端 | Svelte 5 + TypeScript + SvelteKit，`adapter-static`；产物 `go:embed` | 现代组件、类型检查与路由；浏览器渲染，NAS 只服务静态文件和 API |
| 样式与组件 | Tailwind CSS 4，按需 shadcn-svelte，Lucide SVG 图标 | 统一主题/间距/排版，复用菜单与弹窗的交互基础；不整套搬入后台模板 |
| API | HTTP JSON、OpenAPI 3.1.0；`oapi-codegen` + `openapi-typescript` / `openapi-fetch` | 同一契约生成 Go 处理器类型与前端调用类型；浏览器和 Agent 使用同一 API |
| 源解析 | `mmcdole/gofeed` | RSS、Atom、JSON Feed 统一模型；只解析应用已受控取得的字节 |
| 显式全文 | `go-shiori/go-readability` + `microcosm-cc/bluemonday` | 对已经下载的 HTML 提取正文并净化；不启用浏览器或主动补抓 |
| 调度 | 进程内定时器 + SQLite 持久任务/租约 | 工作可恢复，空闲不忙等；不用外部 cron 承担核心调度 |
| 运行 | 单个 Docker 容器、现有 nginx、LAN/VPN | 复用 NAS 运维方式；应用数据独立，不依赖 Miniflux PostgreSQL |
| 验证 | Go 测试、真实 SQLite 集成测试、svelte-check、Vitest、Playwright | 优先验证 A01–A10；全部在本机，NAS 部署后做健康确认和正常负载观察 |

## 为什么选 Go，Rust 怎么样

Rust 是可行的正式备选，功能库没有构成阻碍。这里比较的是本项目的实现与维护取舍，**没有做 Go/Rust 的同负载性能或功耗对测**。

| 维度 | Go | Rust |
|---|---|---|
| 运行资源 | 有 GC，需控制正文缓冲和数据库缓存，实测峰值 | 无 GC，对分配和资源释放有更细控制；仍需限制缓存、任务和线程 |
| RSS 服务开发 | 标准 HTTP、直接的并发模型，解析/正文提取库齐备 | Axum/Tokio、feed-rs、reqwest 等能覆盖相同需求 |
| 维护 | 代码约定较少，测试和交叉编译直接 | 类型和所有权约束有利于正确性；异步与同步 SQLite 的边界需明确设计 |
| Agent 协作 | 显式 SQL、事务、普通函数较易形成小改动与测试闭环 | 编译器提供更强约束；宏、生命周期和 trait 组合也可能加长修改反馈周期 |
| SQLite | 选 pure Go 驱动；与原生 SQLite 的性能差异需测量 | rusqlite 可绑定打包的 SQLite；跨平台构建要处理 C 工具链，或使用其他驱动 |
| 本项目结论 | **首选：优先控制长期维护成本** | 如果实测资源预算持续不达标，或维护者形成 Rust 偏好，重新评估 |

134 个源每 4–6 小时检查，平均每天约 536–804 次常规检查。主要工作是等待网络和短时解析；条件请求、错峰、有界并发及 SSD 上的批量写入比语言排名更能指导当前设计。这个估算不含失败重试和手动获取，也不能换算为实际瓦数。Go 的选择不意味着它比 Rust 更省电。

Rust 对应后端可以是 Axum/Tokio + reqwest + rusqlite + feed-rs + dom_smoothie + ammonia，API 可用 utoipa；同样可以服务选定的 Svelte 静态前端。已核对这些项目的能力；未构建 Rust 应用或验证提取效果。全文提取对具体网站的质量，两种语言都要用同一组样本验证。参考：[Axum](https://github.com/tokio-rs/axum)、[rusqlite](https://github.com/rusqlite/rusqlite)、[feed-rs](https://docs.rs/feed-rs/latest/feed_rs/)、[dom_smoothie](https://github.com/niklak/dom_smoothie)、[ammonia](https://docs.rs/ammonia/latest/ammonia/)、[utoipa](https://docs.rs/utoipa/latest/utoipa/)。

Python/FastAPI 也能满足当前吞吐，未选它是为了统一构建产物、减少解释器与部署环境维护，并给低内存目标留余量；不是因为这点订阅量必须使用编译语言。无需通过多语言拆分获得这些能力。

## 其余关键取舍

| 问题 | 决定 | 没有选另一方案的原因 |
|---|---|---|
| SQLite 或现有 PostgreSQL | SQLite | 单人、少量并发写可用短事务处理；独立于旧系统生命周期。PostgreSQL 已存在但仍有数据库角色、升级、备份和常驻服务维护成本 |
| 纯 Go 或 C SQLite 驱动 | 先 pure Go | 本机 ARM 开发、J1900 AMD64 部署更方便；如果分析证明驱动是瓶颈，先替换适配器，不重写产品 |
| FTS5 或独立搜索 | FTS5 | 事务一致、部署简单；当前不需要向量检索、语义排名或搜索集群 |
| SSR、htmx 或现代静态前端 | SvelteKit 静态 SPA | 用户要求现代 Web 技术；有状态阅读交互由组件承担，同时避免在 NAS 再运行 JS 服务 |
| 定时器或 cron | 定时器 + 持久状态 | 需要按源下一次时间、手动合并、失败恢复、重启接续；单纯 cron 不涵盖这些状态 |
| REST 或 GraphQL/MCP | REST + OpenAPI | Agent 可直接用 HTTP；以后 MCP 适配器复用 API，不先维护多套入口 |

SQLite 的单写者约束和 WAL 的同机文件要求须认真遵守；它不是任意负载下都替代 PostgreSQL。依据：[适用场景](https://sqlite.org/whentouse.html)、[WAL](https://sqlite.org/wal.html)。

## 现代前端与简洁界面

Svelte 5 提供组件和响应式状态，TypeScript 开启 strict；SvelteKit 提供路由、导航和阅读位置恢复，Vite 完成本机构建。选择 Svelte 而非 React/Next，是为了较直接的组件实现及静态部署；React 同样可行，当前没有需要 React 专属生态的功能。框架不会自动保证体积，最终以构建报告与手机端测试为准。

使用 `adapter-static` 生成客户端应用及 fallback 页面，由 Go 嵌入并提供；动态文章数据通过同源 API 取得。不开 SvelteKit 服务端运行时，不使用 server actions 或依赖服务器的 load。私人阅读器不需要搜索引擎收录，接受首次加载需要 JS 和 API 请求的取舍；以资源缓存、按路由拆包和一次列表请求控制启动开销。[SvelteKit 静态构建](https://svelte.dev/docs/kit/adapter-static) 和 [SPA 模式](https://svelte.dev/docs/kit/single-page-apps) 明确说明了这种方式及启动代价，不能把静态前端宣传成无需 JS。

Tailwind 4 通过 `@tailwindcss/vite` 编译，仅生成需要的 CSS，使用 CSS 主题变量统一颜色、留白、圆角、文字和明暗主题。文章正文用 Typography 插件的 prose 样式再调整中文行高、代码块和图片宽度；应用导航与正文样式隔离。动态状态映射到完整静态类名，不拼接无法被构建扫描的 class，也不把私人文章内容作为 Tailwind 构建输入。依据：[Tailwind Vite 集成](https://tailwindcss.com/docs/installation/using-vite)。

shadcn-svelte 只按需引入 Dialog、Dropdown、Tooltip 等组件及依赖；代码随项目维护，校验焦点、键盘、屏幕阅读器及升级差异。Lucide 图标按个导入，图标按钮仍有可读标签。系统字体、紧凑列表、单一强调色、清楚的已读/未读状态是默认方向，组件库不决定页面必须有大量卡片或动画。参考：[shadcn-svelte 集成](https://www.shadcn-svelte.com/docs/installation/sveltekit)。

从同一 OpenAPI 文件生成 TypeScript 类型并用轻量 `openapi-fetch` 调用；服务端负责所有权限和生命周期规则，前端只负责呈现与交互。首版用 Svelte 自身状态管理，不先引入全局状态框架；乐观更新必须有失败回滚。归档与正文不写浏览器持久缓存，登出清内存状态，避免本地过期正文继续可见。参考：[openapi-fetch](https://openapi-ts.dev/openapi-fetch/)。

## 中文搜索的具体选择

原始正文不改写。索引前将净化后文本中的汉字拆成单字 token，保留英文单词；查询“音乐”转为相邻短语 `"音 乐"`。首版把用户空白分隔的检索项按 AND 组合，各项均作为转义后的字面短语，不开放原始 FTS 操作符。最多 8 项、总长 128 字符，结果分页；单字搜索可用但可能较宽泛。

语义是规范化后的 token 匹配，不承诺逐字节子串：标点作为分隔，英文不区分大小写和常见重音；英文 `SQL` 不等于 `SQLite`。简繁自动互转、拼音、词干、同义词、拼写纠错与日语专用分词不进入首版。中文顺序和相邻性需保留，不能把分散的“音”和“乐”误当成“音乐”。

FTS5 使用 `content=''` 和 `contentless_delete=1`，不另存一份完整可读正文。权威数据在普通表，搜索预览从仍保留的字段生成。更新时提供全部索引列；正文清理后重建该行的保留字段索引，全文索引可由权威数据重建。词项/位置仍占空间；删除后的旧页与段合并另属维护问题，不宣称即时物理擦除。

已实证默认 unicode61 不能命中连续中文内部的两字词，而 trigram 的 MATCH 无法处理不足三字符的词，所以不采用“默认分词即可”或短词全库 LIKE 扫描。上述索引方式先验证语义，再用规模测试检验体积与性能。机制参考：[SQLite FTS5](https://sqlite.org/fts5.html)。

## 版本与已经完成的验证

| 项目 | 2026-10-15 验证结果 | 使用方式 |
|---|---|---|
| Go | 官方当前稳定补丁 1.27.2；本机原有 1.26.4 | 实验使用自动下载的 1.27.2；未更改全局 Homebrew 安装。开发基线 1.27.2 |
| modernc SQLite | `go list -m ...@latest` 得到 v1.60.1；实际运行 SQLite 3.53.4 | 已锁在实验 go.mod/go.sum；保留其要求的 libc v1.77.1，不独立漂移升级 |
| oapi-codegen | 查询得到 v2.8.0；发布说明已支持 OpenAPI 3.1 | 首次实现固定该版本；本轮未生成产品接口。严格类型不等于自动完成权限、业务或全部 schema 校验 |
| 前端版本候选 | Svelte 5.57.2、SvelteKit 3.1.0、adapter-static 4.0.0、Vite 8.3.4、vite-plugin-svelte 7.3.1、TypeScript 6.0.3 | npm 版本及主要 peer 范围已核对；Kit 要求 TS ^6，不使用查询到的最新 TS 7.0.2。尚未做完整组件组合构建 |
| Tailwind | tailwindcss 与 @tailwindcss/vite 当前同为 4.3.3 | 首次实现使用兼容配套版本，Typography 与按需组件一并验证并锁定 |
| 中文搜索与清理 | 本机实际驱动测试通过 | 覆盖两字/单字、中英混排、顺序、字面查询转义、正文清理、保留整理成果、事务回滚 |
| 目标平台 | Linux/AMD64、`GOAMD64=v1`、`CGO_ENABLED=0` 交叉编译验证 | 仅验证实验构建兼容性；不代表已经在 NAS 运行，也不是功耗测试 |

可重现证据见 [SQLite 搜索实验](../spikes/sqlite-search/README.md)。Go 版本依据 [官方发布记录](https://go.dev/doc/devel/release)，驱动依据 [modernc 文档](https://pkg.go.dev/modernc.org/sqlite)，API 工具依据 [v2.8.0 发布说明](https://github.com/oapi-codegen/oapi-codegen/releases/tag/v2.8.0)。

其余已选择的库在首次应用实现时核对兼容补丁并固定到应用的 go.mod/go.sum、pnpm-lock.yaml；目前没有根模块或前端应用，不能把实验依赖当成产品依赖已经全部锁好。前端采用本机 Node 24 LTS + pnpm，具体补丁与 packageManager 固定在项目配置；不静默替换本机已有 Node 26。生成器、构建基础镜像及最终镜像也要固定版本或 digest，升级通过测试，不使用浮动 `latest` 部署。

实现资料：[gofeed](https://github.com/mmcdole/gofeed)、[go-readability](https://github.com/go-shiori/go-readability)、[bluemonday](https://github.com/microcosm-cc/bluemonday)、[goose](https://github.com/pressly/goose)、[OpenAPI 3.1.0](https://spec.openapis.org/oas/v3.1.0.html)。

## 资源预算与实施关口

以下是**待验证的工程预算**，不是用户承诺，也不是已测结果。初始测试规模定为 200 个源、10 万条文章元数据、2 万条仍有正文的文章；测试数据包含中英文、长正文、异常 HTML，正文总量约 500 MiB。先用合成数据，真实私人内容不进入仓库。

| 指标 | 初始预算/判据 |
|---|---|
| 应用内存 | 稳定空闲 RSS ≤ 96 MiB；正常抓取与搜索混合峰值 ≤ 256 MiB；容器暂定硬上限 512 MiB |
| 空闲 CPU | 无请求和到期任务的 30 分钟窗口，进程平均 < 单核 1%；不靠短周期心跳写盘维持状态 |
| 页面/API | NAS 上预热后列表 50 条 p95 ≤ 200 ms；常见中英文搜索 p95 ≤ 500 ms，单次查询超时 2 s；单字宽查询单独记录 |
| 后台 | 全局最多 2 个网络任务，同站点 1 个；正文提取同时 1 个；正常到期任务调度迟延 ≤ 5 分钟，异常有明确状态 |
| 可靠性 | 断网、429/503、无效源、进程终止/重启下通过 A01–A09；数据库断电耐久性与存储设备能力有关 |
| 前端 | 首屏必需 CSS+JS 压缩传输合计预算 ≤ 150 KiB，不计文章及图片；设置/复杂弹窗按需加载，无外部字体/框架 CDN；以构建报告验证 |

规模、性能、故障和恢复测试均在本机，包括本机 Linux 容器/虚拟机；本机结果不能直接换算成 J1900 指标。NAS 部署后仅做健康确认和正常使用下的内存/CPU/请求延迟观察，记录既有媒体/下载负载，不在 NAS 放测试数据、运行压测或故障注入。CPU 与内存是代理指标；真正判断省电需另行安排整机功耗测量，本轮没有这项数据。

不达标先分析请求量、缓冲复制、索引、锁等待与 SQLite 驱动；若仍无法达到资源预算，再决定驱动或语言调整。不能仅用 hello-world 内存或合成搜索耗时证明整套阅读器的性能。

下一阶段顺序：① 本机工具链、数据模型与 API 契约；② 调度、去重、搜索、清理的纵向实现；③ 现代简洁阅读界面与 Agent 写回；④ 本机 Miniflux 导入/恢复演练与 Linux 成品验证；⑤ 后续部署时确认健康及实际资源使用。模块和运行约束见 [架构设计](architecture.md)。
