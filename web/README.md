# 拾流 RSS Web

Svelte 5 + TypeScript 6 + SvelteKit 3 静态 SPA，Tailwind CSS 4。构建输出 `web/build`，由 Go 嵌入服务，`200.html` 为页面路由回退；生产无需 Node 服务端。图标沿用 [assets/icon.svg](../assets/icon.svg)。

## 开发与构建

在仓库根目录运行 `make setup`、`make init`、`make dev`。单独前端命令：

```sh
pnpm --dir web check
pnpm --dir web generate
pnpm --dir web build
pnpm --dir web check:bundle
```

Node 24.22.0 由 pnpm 项目 runtime 提供，pnpm 固定 11.0.9，不替换全局 Node。Vite 读取 [config/dev.env](../config/dev.env)，固定前端端口并将 `/api` 代理至 Go；不在重启时重新分配端口。

[OpenAPI](../api/openapi.yaml) 是共享契约。生成 `src/lib/api.generated.ts`，`models.ts` 提供类型别名，`api.ts` 用 openapi-fetch 发请求。修改契约后运行根目录 `make generate check-generated`。

Kit 3 配置通过 Vite `sveltekit({...})` 传入，TypeScript 继承 `$app/tsconfig`。Lucide 使用 `@lucide/svelte`；固定依赖的 peer 例外在 `pnpm-workspace.yaml` 记录，不随意放宽版本范围。

## 当前交互

- 首页为晨报；近期文章、收藏、归档、分类和源共用主侧栏。旧 `/agent` 路径重定向至首页 Agent 工作区。
- 晨报内部引用打开正文，返回恢复报告、锚点和滚动位置。每个来源聚合模块末尾提供本组已总结文章的标读按钮、实时可标读数及组内结果反馈；技术说明支持悬停、聚焦和触屏。报告提问在桌面右栏或窄屏底部展开。
- 分类和订阅源分区独立折叠；每个分类可展开其源。两处源行显示服务端全量非零未读，异常橙点可查看实际错误和调度时间。
- 文章按发布时间倒序；无发布时间时用收录时间。未读标签取完整筛选计数，忽略阅读标签及分页；已加载篇数和本页批量操作单独呈现。过期响应不能覆盖新筛选的计数。
- 分类/源列表标题旁的详情按钮显示当前对象，并直接定位编辑目标。关闭设置返回原筛选列表。
- 返回列表保留阅读位置，新内容通过主动更新列表显示。来源图标/名称是独立链接，保留组合点击与新标签行为。
- 桌面侧栏折叠偏好存于 localStorage；手机使用滑出导航。分类和源共享可滚动区域，品牌、主导航和底部工具保留。

| 按键 | 行为 |
|---|---|
| `↑/↓` | 列表焦点移动，或正文上一篇/下一篇 |
| `c` | 展开/收起侧栏 |
| `a` / `s` | 正文已读状态 / 收藏 |
| `e` | 打开原文 |
| `q` | 返回并聚焦搜索 |
| `Enter` / `Tab` | 原生控件启用 / 焦点移动 |
| `Esc` | 关闭当前浮层、导航、正文或设置 |

使用无修饰小写 `KeyboardEvent.key`，避让 Vimium 默认键及序列前缀。输入、组合输入、表单、设置、弹窗不触发后台阅读操作；长按动作键不重复切换。自定义 Vimium 映射仍可能冲突。

## 安全与生命周期

文章 HTML 仅呈现服务端净化后的内容，普通字段由 Svelte 转义，外链只接受 HTTP/HTTPS。favicon 仅请求安全站点 origin 的 `/favicon.ico`，无路径/查询、无 referrer，失败回退 RSS 图标。

正文不写 localStorage、IndexedDB 或 service worker。期限到达移除屏幕源内容，页面恢复时向服务器复验。登出或 401 清空内存；未完成的旧请求不能重新填回私人内容。浏览器使用 cookie + CSRF，token 明文仅在创建时短暂显示。

普通全文按钮显式创建任务；重要源晨报自动补抓由服务端策略控制。取消收藏和修改保存策略显示影响，预览只对当前参数有效。日期筛选将本地日历转换为起点及次日排他终点，服务端按首次收录时间筛选。

构建用 parse5 外置启动脚本，再预压缩 HTML/JS/CSS；严格 CSP 无需 `unsafe-inline` 脚本。`check:bundle` 统计全部 JS/CSS，包括懒加载设置页，以 150 KiB gzip 为保守预算，不能代表设备加载速度。

## 浏览器验证

完整入口是根目录 `make test-web`：先停止本机 dev/run，再以临时账号和隔离数据库运行嵌入 Go 的成品。当前 60 项 Chromium 桌面/手机尺寸检查通过，实际结果与限制见[实施记录](../docs/implementation.md)。

| 用例文件 | 主要覆盖 |
|---|---|
| interactions / audit | 阅读位置、快捷键、输入避让、会话、过期、安全链接、策略、长文本与大量源 |
| categories / sidebar | 分类 CRUD、版本冲突、成员管理、独立展开、源异常和徽标 |
| agent | 模型配置、晨报原位返回、分组批量已读、页内对话与技术说明 |
| list-context | 全量未读、分页、状态刷新、部分失败、延迟响应、详情与准确编辑 |
| real-service | 真实本机 Go API + 合成 RSS/HTML 的订阅、抓取、阅读、全文及笔记闭环 |

mock 不能替代后端；real-service 未提供隔离实例密码时明确跳过。`SHILIU_E2E_URL` 和 `SHILIU_E2E_PASSWORD` 只用于本机隔离服务，不得指向日常库或生产。截图/trace 在忽略的 `web/test-results`。设备尺寸模拟不等于真实 Safari/WebKit 或 Android 验证。
