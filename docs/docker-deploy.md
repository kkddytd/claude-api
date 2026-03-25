# Claude API Docker 部署手册

这份手册基于仓库当前实际启动方式整理，新增了 `Dockerfile` 和 `docker-compose.yml`，可以直接在本仓库里执行。

## 1. 部署方式说明

- 默认方案使用 SQLite，不需要额外数据库，数据持久化在 Docker 卷 `claude_api_data` 中。
- 容器启动命令为 `./claude-server -no-browser -data-dir=/data`。
- 程序会把数据库、日志和可选的 `config.yaml` 都放在 `/data` 下。
- 服务默认端口为 `62311`，健康检查接口为 `/healthz`。

## 2. 前置条件

- Docker 24+ 或兼容版本
- Docker Compose v2
- 服务器放通 `62311` 端口，或由 Nginx/Caddy 做反向代理

检查环境：

```bash
docker --version
docker compose version
```

## 3. 快速部署

首次部署：

```bash
git clone https://github.com/kkddytd/claude-api.git
cd claude-api
docker compose up -d --build
```

如果你所在网络访问 `proxy.golang.org` 不稳定，可以直接切换 Go 模块代理后再构建：

```bash
GOPROXY=https://goproxy.cn,direct docker compose up -d --build
```

查看状态：

```bash
docker compose ps
docker compose logs -f claude-api
```

浏览器访问：

```text
http://localhost:62311
```

首次登录信息：

- 管理后台默认密码：`admin`
- 登录后建议立刻修改管理员密码
- 在“系统设置”中配置你的 API Key
- 在“账号管理”中添加 AWS Kiro 账号

## 4. 常用运维命令

启动或重建：

```bash
docker compose up -d --build
```

停止服务：

```bash
docker compose down
```

重启服务：

```bash
docker compose restart claude-api
```

查看实时日志：

```bash
docker compose logs -f claude-api
```

升级到仓库最新代码：

```bash
git pull
docker compose up -d --build
```

## 5. 数据与备份

容器内关键路径：

- `/data/data.sqlite3`：SQLite 数据库
- `/data/logs/`：服务日志
- `/data/config.yaml`：可选自定义配置文件

备份 SQLite：

```bash
mkdir -p backups
docker cp claude-api:/data/data.sqlite3 ./backups/data.sqlite3.$(date +%Y%m%d_%H%M%S)
```

检查数据目录：

```bash
docker compose exec claude-api sh -lc 'ls -lah /data && ls -lah /data/logs || true'
```

## 6. 自定义配置

如果你要改端口、启用 MySQL、打开调试模式，推荐通过 `config.yaml` 完成。

先复制示例配置：

```bash
cp docker/config.yaml.example docker/config.yaml
```

然后编辑 `docker/config.yaml`，再把 `docker-compose.yml` 里的这行取消注释：

```yaml
- ./docker/config.yaml:/data/config.yaml:ro
```

重要说明：

- 这个项目在传入 `-data-dir=/data` 后，会先切换工作目录到 `/data`，再读取 `config.yaml`。
- 所以配置文件必须挂载到 `/data/config.yaml`，而不是 `/app/config.yaml`。

如果你只想改宿主机映射端口，不改应用内部监听端口，可以直接这样启动：

```bash
CLAUDE_API_PORT=18080 docker compose up -d --build
```

此时访问地址变为：

```text
http://localhost:18080
```

## 7. 切换到 MySQL

项目本身支持 MySQL，`docker/config.yaml.example` 已经给了可直接改的模板。

你有两种做法：

1. 使用外部 MySQL 服务，把 `host` 改成真实地址。
2. 自己额外起一个 MySQL 容器，再把 `host` 改成该容器在同一网络下的服务名。

最少需要改这些字段：

- `database.type: mysql`
- `database.mysql.host`
- `database.mysql.port`
- `database.mysql.user`
- `database.mysql.password`
- `database.mysql.database`

改完后重新启动：

```bash
docker compose up -d --build
```

## 8. 反向代理建议

生产环境建议在外层加 Nginx 或 Caddy：

- 域名统一入口
- HTTPS 证书托管
- 更方便做访问控制
- 对流式响应要关闭代理缓冲

如果你已经有 Nginx，可以把上游指向：

```text
http://127.0.0.1:62311
```

## 9. 故障排查

健康检查：

```bash
curl http://127.0.0.1:62311/healthz
```

如果容器启动失败，优先检查：

- `docker compose logs -f claude-api`
- `docker/config.yaml` 是否是合法 YAML
- MySQL 连接信息是否正确
- 宿主机 `62311` 端口是否被占用
- 构建阶段如果出现 `proxy.golang.org ... unexpected EOF`，改用 `GOPROXY=https://goproxy.cn,direct`

如果只是想恢复默认配置，删除 `/data/config.yaml` 后重启容器即可。
