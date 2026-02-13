# 项目结构说明

本文档详细说明了 Claude API 项目中每个文件和目录的作用。

## 根目录文件

| 文件 | 说明 |
|------|------|
| `main.go` | 程序入口文件，负责初始化服务器、数据库、启动 HTTP 服务 |
| `build.sh` | 多平台构建脚本，支持 Linux/macOS/Windows 的服务端和桌面应用编译 |
| `config.yaml` | 配置文件示例（可选），支持数据库、服务器端口等配置 |
| `config-mysql-example.yaml` | MySQL 数据库配置示例 |
| `go.mod` | Go 模块依赖管理文件 |
| `go.sum` | Go 模块依赖校验文件 |
| `README.md` | 项目说明文档，包含功能介绍、使用指南、部署说明 |
| `PROJECT_STRUCTURE.md` | 本文件，项目结构详细说明 |

## 目录结构

### `/internal` - 内部包（核心业务逻辑）

#### `/internal/api` - API 服务器
- `server.go` - HTTP 服务器核心，包含中间件、CORS、认证等
- `routes.go` - 路由配置，定义所有 API 端点
- `handlers.go` - 请求处理器，实现主要业务逻辑
- `cache.go` - 账号池和设置缓存管理

#### `/internal/amazonq` - Amazon Q 客户端
- `client.go` - HTTP 客户端，负责与 Amazon Q API 通信
- `parser.go` - 响应解析器，处理 Amazon Q 返回的数据
- `errors.go` - 错误处理和错误类型定义

#### `/internal/auth` - 认证模块
- `oidc.go` - OIDC 设备授权流程实现
- `kiro.go` - Kiro 社交登录集成
- `apikey.go` - API Key 验证中间件

#### `/internal/claude` - 格式转换
- `converter.go` - OpenAI 格式 ↔ Amazon Q 格式的双向转换器

#### `/internal/stream` - 流处理
- `parser.go` - SSE 流解析器
- `openai_sse.go` - OpenAI 格式流式响应生成
- `claude_sse.go` - Claude 格式流式响应生成
- `unified_sse.go` - 统一流处理接口

#### `/internal/database` - 数据库层
- `database.go` - 数据库初始化，支持 SQLite 和 MySQL
- `accounts.go` - 账号管理（CRUD、刷新令牌）
- `users.go` - 用户管理
- `settings.go` - 系统设置存储和读取
- `logs.go` - 请求日志记录和查询
- `proxy.go` - 代理配置管理
- `blocked_ips.go` - IP 黑名单管理

#### `/internal/models` - 数据模型
- `account.go` - 账号数据模型
- `user.go` - 用户数据模型
- `settings.go` - 设置数据模型
- `openai.go` - OpenAI API 格式定义
- `claude.go` - Claude API 格式定义
- `amazonq.go` - Amazon Q API 格式定义

#### `/internal/config` - 配置管理
- 配置文件加载和解析

#### `/internal/logger` - 日志系统
- 统一日志输出和格式化

#### `/internal/tokenizer` - Token 计数
- 使用 anthropic-tokenizer-go 进行 Token 计算

#### `/internal/compressor` - 上下文压缩器
- 对话历史压缩，优化 Token 使用

#### `/internal/proxy` - 代理池管理
- HTTP 代理配置和轮询

#### `/internal/ratelimit` - 限流器
- IP 和 API Key 双重限流

#### `/internal/utils` - 工具函数
- 通用辅助函数

### `/frontend` - Web 前端

#### 主页面
- `index.html` - 管理控制台主页面
- `login.html` - 登录页面

#### `/frontend/js` - JavaScript 模块
- `app.js` - 主应用入口，Vue 应用初始化
- `accounts.js` - 账号管理模块（添加、删除、刷新）
- `users.js` - 用户管理模块
- `settings.js` - 系统设置模块
- `chat.js` - 聊天测试界面
- `logs.js` - 请求日志查看
- `ips.js` - IP 黑名单管理
- `api.js` - API 请求封装
- `common.js` - 公共函数
- `utils.js` - 工具函数
- `ui.js` - UI 组件
- `devtools.js` - 开发者工具
- `serverLogs.js` - 服务器日志查看

#### `/frontend/css` - 样式文件
- 界面样式定义

#### `/frontend/vendor` - 第三方库
- `vue.global.js` - Vue.js 3 框架
- `marked.min.js` - Markdown 渲染
- `highlight.min.js` - 代码高亮

#### `/frontend/img` - 图片资源
- 界面图标和图片

### `/desktop` - 桌面应用（Wails）
- `wails.json` - Wails 配置文件
- `frontend/` - 桌面应用前端
- `build/` - 桌面应用构建配置
- `embedded/` - 嵌入式资源

### `/scripts` - 辅助脚本
- `start.sh` - 启动脚本
- `stop.sh` - 停止脚本
- `setup.sh` - 环境配置脚本

### `/展示图` - 界面截图
- `dashboard.png` - 控制台主界面
- `accounts.png` - 账号管理界面
- `chat.png` - 聊天测试界面
- `settings.png` - 系统设置界面
- `logs.png` - 请求日志界面

### `/dist` - 编译产物（自动生成）
- `server/` - 各平台服务端程序压缩包
  - `claude-server-linux-amd64.tar.gz`
  - `claude-server-linux-arm64.tar.gz`
  - `claude-server-darwin-amd64.tar.gz`
  - `claude-server-darwin-arm64.tar.gz`
  - `claude-server-windows-amd64.zip`
  - `claude-server-windows-arm64.zip`
- `desktop/` - 桌面应用安装包
  - `Claude-API-Server-macOS.zip`
  - `Claude-API-Server-Windows.zip`

### `/test` - 测试文件
- 单元测试和集成测试

## 数据流程

```
客户端请求
    ↓
Gin Router (routes.go)
    ↓
认证中间件 (auth/apikey.go)
    ↓
请求处理器 (api/handlers.go)
    ↓
账号选择器 (api/cache.go)
    ↓
格式转换器 (claude/converter.go)
    ↓
Amazon Q 客户端 (amazonq/client.go)
    ↓
流解析器 (stream/parser.go)
    ↓
OpenAI 格式流 (stream/openai_sse.go)
    ↓
返回客户端
```

## 构建流程

1. `build.sh` 读取构建参数
2. 编译 Go 后端（支持跨平台）
3. 打包前端静态文件
4. 生成压缩包到 `dist/` 目录
5. （可选）构建桌面应用使用 Wails

## 运行时文件

运行时会生成以下文件（已在 .gitignore 中忽略）：

- `data.sqlite3` - SQLite 数据库文件
- `logs/` - 日志文件目录
- `cache/` - 缓存文件目录
- `config.yaml` - 用户配置文件（可选）

## 开发建议

1. **添加新功能**：
   - 在 `internal/` 对应模块添加业务逻辑
   - 在 `internal/api/handlers.go` 添加处理器
   - 在 `internal/api/routes.go` 注册路由
   - 在 `frontend/js/` 添加前端逻辑

2. **修改数据模型**：
   - 更新 `internal/models/` 中的模型定义
   - 更新 `internal/database/` 中的数据库操作
   - 运行数据库迁移（如需要）

3. **添加新的 API 端点**：
   - 在 `internal/api/routes.go` 注册路由
   - 在 `internal/api/handlers.go` 实现处理器
   - 在 `frontend/js/api.js` 添加前端 API 调用

4. **调试**：
   - 启用 `config.yaml` 中的 `debug: true`
   - 查看控制台日志输出
   - 使用 `/v2/logs` 端点查看请求日志
