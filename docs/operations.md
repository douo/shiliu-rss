# 本机开发、迁移与维护

本文提供开发检查、Miniflux 迁移、备份恢复和 Linux 镜像验证命令。正式服务的配置和升级见[部署指南](production.md)，凭据与数据保护见[安全说明](security.md)。

## 日常入口

```sh
make setup
make init
make dev
```

`make init` 只初始化新账号。固定开发配置在 `config/dev.env`，密码与 SQLite 在忽略的 `.local/`。`make dev` 与 `make run` 共用 API 端口，二选一；`make test-web` 前先停开发服务，测试脚本不会抢占已监听端口。

```sh
make check-generated check test
make test-web
pnpm --dir web check:bundle
```

PostgreSQL 合成快照测试需要 `initdb`、`pg_ctl` 和 `psql`。默认探测本机安装，也可设 `SHILIU_TEST_POSTGRES_BIN` 为其 bin 目录。测试用临时 Unix socket，不注册常驻服务；缺工具时明确 skip。

## Miniflux 一致快照

当前导出器适配 Miniflux 2.3.0，部署数据库 schema 需要独立核实；支持定义参见上游 [migrations.go](https://github.com/miniflux/v2/blob/2.3.0/internal/database/migrations.go)。导出在 `REPEATABLE READ READ ONLY` 事务中完成，NDJSON 带最终计数；失败或截断快照不得导入。

```sh
go build -o dist/miniflux-export ./cmd/miniflux-export
make build
```

数据库连接串通过受保护环境提供，不拼进命令行或 shell 历史。以下路径和实例名均是占位值；目标使用独立本机库：

```sh
umask 077
# 先通过自己的秘密管理方式设置 MINIFLUX_DATABASE_URL。
dist/miniflux-export -instance source-instance -source-version 2.3.0 -user-id 1 \
  > .local/snapshot.ndjson.partial
# 仅在导出成功后发布完整文件。
mv .local/snapshot.ndjson.partial .local/snapshot.ndjson
unset MINIFLUX_DATABASE_URL

dist/reader preview .local/snapshot.ndjson
SHILIU_DB=.local/import-preview.db dist/reader import .local/snapshot.ndjson
```

运行导入前停止目标阅读器并备份。先审阅分类、源、文章、状态和不兼容项，再核对关联、首次时间、收藏、标签、removed、正文及保护期。重复导入保持稳定 `source_instance`；不更换标签绕过去重。正式切换前安排稳定窗口重新导出和对账，单次旧快照不等于持续双向同步。

导入规则：

- `disabled` 表示人工暂停；累计错误不映射为暂停。首次源默认普通保存。
- 保留源实例/原 ID/旧 hash 及阅读、收藏、标签、发布时间和正文；HTML 重新净化。原 GUID 未存时用 URL/旧 hash 建立别名，不伪造 GUID。
- 优先 `created_at` 作为首次收录；缺失时固定首次导入时间并标 estimated。不可靠 read_at 用首次导入时间计时并标未知。
- 首次导入正文有 30 天保护，重导不延长。普通隐藏规则仍生效；旧系统已删除的文章不可恢复。
- URL/身份冲突停止报告；用户已删除分类的旧映射不被重复导入悄悄重建。
- 认证、代理、改写/过滤、隐藏等专有设置报告字段名而非秘密值；默认拒绝有差异的导入。审阅后可显式 `import FILE --allow-unsupported`，这不表示配置已兼容，也不扩大出站权限。
- 迁入标签使用独立有界限制以兼容旧数据，日常手工编辑限制保持不变。

快照本身可能含私密订阅 token 和真实正文，应整体保密。跨设备导出用受控标准输入/输出传输，不在运行设备留下临时导出或脚本。

## 一致备份与恢复

停止相关本机服务后：

```sh
SHILIU_DB=.local/reader.db dist/reader backup .local/backups
# 将 BACKUP.db 替换为命令实际返回的成功备份；恢复目标必须是新路径。
dist/reader restore .local/backups/BACKUP.db .local/restored.db
```

程序对数据库持有进程锁。备份采用 SQLite `VACUUM INTO`，经过完整性、外键、文件同步及原子发布后才轮换，保留最多 7 个成功版本；失败不删已有成功副本。不能用单独复制正在运行的 `.db` 替代一致备份。

恢复拒绝覆盖现有目标或未知未来 schema。发布恢复库前迁移并清理已到期正文/索引，避免从旧备份重新暴露过期内容。恢复后核对计数、关联、整理成果和有效期，再切换路径；损坏备份拒绝开放读取。

## Linux 构建与本机运行检查

```sh
make build-linux
docker build --platform linux/amd64 -f deploy/Dockerfile \
  -t shiliu-rss:LOCAL_RELEASE .
```

Dockerfile 只打包本机生成的 Linux AMD64 v1 二进制，基础镜像固定 digest。需要实际 Linux 容器运行检查，Mac 原生测试不能替代它。

本机 Colima 工作流：

```sh
colima --profile shiliu start
DOCKER_HOST="$(docker context inspect colima-shiliu --format '{{.Endpoints.docker.Host}}')" \
  docker buildx build --platform linux/amd64 -f deploy/Dockerfile \
  -t shiliu-rss:LOCAL_RELEASE --load .
deploy/smoke-linux.sh shiliu-rss:LOCAL_RELEASE
colima --profile shiliu stop
```

smoke 脚本仅接受本机 Colima Unix socket，生成临时合成账号、暂停源及 volume，不发布主机端口，结束清理。覆盖首次启动/重启、登录/CSRF/token 撤销、健康、深链接/404、UID 65532、只读根和 CA。AMD64 模拟运行不等于目标硬件性能或功耗验证。

## 合成规模演练

```sh
go run ./cmd/bench-reader -maintenance -output /tmp/shiliu-scale.json
```

生成 200 源、10 万文章、2 万正文的临时数据库，结束删除。报告含查询、空间、备份恢复与完整性数据。已保存的历史样本及限制见[实施记录](implementation.md)，不要把历史结果写成当前版本复测。
