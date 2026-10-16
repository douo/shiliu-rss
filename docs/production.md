# 部署与升级指南

本文件提供通用部署流程；实际域名、SSH 目标、主机端口、存储布局、镜像摘要、备份名称和实例数据计数在私有运维台账维护。项目已有部署/迁移经验，不在仓库披露个人实例记录。以下地址和路径均是示例，执行前核对目标环境。

## 准备成品

1. 本机完成生成漂移、代码检查、Go race、浏览器验收和 Linux 运行检查，提交代码。
2. 用固定发布标签构建镜像并记录平台、二进制 SHA256、镜像 manifest/config 摘要；OCI 索引和平台 config ID 不等同。
3. 检查目标设备实际端口、Compose 版本、存储权限和备份目标；不在 NAS 安装 Go/Node/测试工具或 APR。
4. 传输固定镜像。开发库、源码工具链、node_modules、测试数据及镜像 tar 不留在运行设备。

参见[本机 Linux 检查](operations.md)。Compose 显式 `pull_policy: never`，需要事先加载本机验证的镜像；普通重启不会偷偷拉新镜像。

## 配置与初始化

复制 `deploy/compose.yaml`、`deploy/.env.example` 和 `deploy/reader.env.example` 到自己的服务目录，将示例文件分别命名为 `.env` 和 `reader.env`，权限设为 0600。`.env` 用于 Compose 替换，`reader.env` 是传给进程的环境，两者作用不同。

| 参数 | 用途 |
|---|---|
| `SHILIU_IMAGE` | 已加载且固定版本的镜像标签或 digest |
| `SHILIU_ORIGIN` | 准确的 HTTPS origin，例如 `https://reader.example.com` |
| `NAS_PORT` | 经检查可用的主机回环端口 |
| `SHILIU_BACKUP_PATH` | 主机上专用备份目录的绝对路径，建议与数据盘分离 |

服务目录中的 `data/` 和备份目录由 UID/GID 65532 可写、目录 0700。SQLite、WAL、SHM 同机同目录，放本地 SSD 文件系统，避免 NFS 和容器可写镜像层。容器根只读、非 root、全部 capabilities drop、no-new-privileges；512 MiB 上限是工程设置，不代表实测峰值。

账户初始化使用受保护密码文件和 `reader init`，没有默认密码。可在已停服务的目标目录用一次性 Compose 容器挂载密码文件：

```sh
# .env / reader.env 已完成配置；initial-password 是自行生成的受保护文件。
docker compose run --rm --no-deps -T \
  -v /PRIVATE/initial-password:/run/secrets/initial-password:ro \
  -e SHILIU_PASSWORD_FILE=/run/secrets/initial-password reader init
```

容器 UID 65532 必须可读取挂载的密码文件，限制其目录权限，初始化后妥善移除该临时挂载文件。不要用真实密码作为命令行值。已有初始化/迁入数据库不重复创建账户。

服务使用 Secure cookie 和 HTTPS。将 `deploy/nginx.conf.example` 合并到自己的 TLS server，替换端口并先运行 `nginx -t`；保留既有访问策略和证书，不把示例直接覆盖生产配置。

`deploy/shiliu-rss.service` 也是模板：按实际服务目录修改 `WorkingDirectory` 和 `RequiresMountsFor`，备份挂载路径必须与 `.env` 一致。该 unit 仅显式启动本地镜像，避免使用可能隐式 pull/down 的通用 Compose 模板。

## 初次切换与迁移

Miniflux 按[一致快照流程](operations.md)导出，在独立本机库预览、导入和逐项对账；验证重复导入、身份、状态、标签与正文保护。切换窗口固定旧系统写入边界并重新取得最终快照。

新服务先完成 HTTPS 登录、源/分类/文章计数、健康、正常阅读和备份确认，再切换入口。旧服务数据与配置保留为明确回退依据，是否停用/下线取决于当前操作授权；不默认并行抓取或双写。

迁入之后 RSS 可能再发现旧快照已经不含的历史文章，它们会按新文章入库；发布时间早不代表已读。核对导入身份、来源 URL 和变更记录来解释差异，不凭数量直接全标已读。

## Agent 配置

在系统「设置 → Agent 配置」中填写自己的兼容接口前缀，例如 `https://api.example.com/v1`、协议、模型名、Key、预算和运行窗口。未配置时停用；Key 只写不回显，也支持受保护的 `SHILIU_AGENT_API_KEY_FILE`。

默认每天 Asia/Shanghai 08:00，覆盖全部未读含归档，重要源按策略补抓。确认系统时钟、时区和预算，按源选择重要度。协议兼容、模型语义质量与费用分别评估，不因为 HTTP 成功就宣称大规模报告质量达标。

## 升级、重启和回退

普通重启在实际服务目录执行 `docker compose restart reader`。升级步骤：

1. 保留上一固定镜像、运行配置和成功备份；先核对当前数据库 schema。
2. 停止 reader，运行 `docker compose run --rm --no-deps -T reader backup /backup`，只接受实际返回的成功备份。
3. 原子更新 `.env` 中的镜像值，执行 `docker compose config --quiet`，再 `docker compose up -d --pull never reader`。
4. 有界等待 healthcheck，核对实际镜像 ID、HTTPS 登录与正常业务；失败切回兼容旧镜像。

通过 SSH stdin 执行部署脚本时，Compose 命令的 stdin 应重定向到 `/dev/null`，避免子进程消费余下脚本。

迁移失败拒绝启动。回退前确认旧程序支持当前 schema；不支持时停服务，用升级前备份恢复到新路径，验证并完成生命周期清理后再替换数据库。不能让旧程序直接打开未知 schema。当前 schema4 的上一轮 schema3 程序需要数据库回退，只有切镜像不足够。

## 正常确认与运维记录

确认 `/healthz`、`/readyz`、HTTPS 登录、来源与列表完整计数、晨报入口及日常资源。故障、恢复、压力和大量模型试验仍只在本机。日志 stdout 由 Docker 按 5 MiB×3 轮换，成功备份最多保留 7 版。

实际镜像、备份和回退关系写入受保护台账。清理本次传输产生的临时 tar/脚本/日志，保留运行必需数据和受控日志；不要把完整 inspect、环境、认证状态或真实内容复制到 GitHub issue/文档。
