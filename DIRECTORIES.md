# 目录详细说明

本文档详细说明项目中每个目录的作用和包含的文件。

## `/internal` - 核心业务逻辑

### `/internal/api` - HTTP API 服务器
**作用**: 提供 HTTP 服务器核心功能，处理所有 API 请求

**文件说明**:
- `server.go` - HTTP 服务器初始化、中间件配置（CORS、认证、日志）、缓存管理
- `routes.go` - 路由注册，定义所有 API 端点（OpenAI 兼容端点、管理端点）
- `handlers.go` - 请求处理器，实现核心业务逻辑（账号选择、格式转换、流式响应）
- `cache.go` - AWS Kiro 账号池缓存、系统设置缓存、内存管理

### `/internal/amazonq` - Amazon Q Developer 客户端
**作用**: 与 Amazon Q Developer (Kiro) API 通信

**文件说明**:
- `client.go` - HTTP 客户端封装，发送请求到 Amazon Q API
- `parser.go` - 解析 Amazon Q 返回的 JSON/SSE 响应
- `errors.go` - Amazon Q 错误类型定义和错误处理

### `/internal/auth` - 认证和授权
**作用**: 处理 AWS OIDC 认证、API Key 验证

**文件说明**:
- `oidc.go` - AWS OIDC 设备授权流程实现（获取设备码、轮询令牌、刷新令牌）
- `kiro.go` - Kiro 社交登录集成（GitHub、Google 等）
- `apikey.go` - API Key 验证中间件，保护 API 端点

### `/internal/claude` - API 格式转换
**作用**: 在 OpenAI、Claude、Amazon Q 格式之间转换

**文件说明**:
- `converter.go` - 格式转换器（OpenAI ↔ Amazon Q，Claude ↔ Amazon Q）

### `/internal/stream` - 流式响应处理
**作用**: 处理 SSE 流式响应，支持 OpenAI 和 Claude 格式

**文件说明**:
- `parser.go` - SSE 流解析器，解析 Server-Sent Events
- `openai_sse.go` - 生成 OpenAI 格式的流式响应
- `claude_sse.go` - 生成 Claude 格式的流式响应
- `unified_sse.go` - 统一流处理接口，自动选择格式

### `/internal/database` - 数据持久化层
**作用**: 数据库操作，支持 SQLite 和 MySQL

**文件说明**:
- `database.go` - 数据库连接初始化、表结构创建、迁移
- `accounts.go` - AWS Kiro 账号 CRUD 操作、令牌刷新、状态更新
- `users.go` - 用户管理（多用户支持）
- `settings.go` - 系统设置存储（API Key、限流规则、日志策略）
- `logs.go` - API 请求日志记录、查询、统计
- `proxy.go` - HTTP 代理配置管理
- `blocked_ips.go` - IP 黑名单管理（封禁、解封）

### `/internal/models` - 数据模型定义
**作用**: 定义所有数据结构和 API 格式

**文件说明**:
- `account.go` - AWS Kiro 账号模型（ID、令牌、过期时间、状态）
- `user.go` - 用户模型
- `settings.go` - 系统设置模型
- `openai.go` - OpenAI API 请求/响应格式定义
- `claude.go` - Claude API 请求/响应格式定义
- `amazonq.go` - Amazon Q API 请求/响应格式定义

### `/internal/config` - 配置管理
**作用**: 加载和解析 config.yaml 配置文件

**文件说明**:
- 配置文件读取、环境变量覆盖、默认值设置

### `/internal/logger` - 日志系统
**作用**: 统一日志输出和格式化

**文件说明**:
- 日志级别控制、彩色输出、结构化日志

### `/internal/tokenizer` - Token 计数
**作用**: 计算消息的 Token 数量（用于统计和限流）

**文件说明**:
- 使用 anthropic-tokenizer-go 进行精确的 Token 计算
- `claude_vocab.json` - Claude 分词器词汇表

### `/internal/compressor` - 上下文压缩
**作用**: 压缩对话历史，优化 Token 使用

**文件说明**:
- 智能压缩算法，保留重要上下文

### `/internal/proxy` - 代理池管理
**作用**: 管理 HTTP 代理配置，支持代理轮询

**文件说明**:
- 代理健康检查、自动切换、负载均衡

### `/internal/ratelimit` - 限流器
**作用**: 防止 API 滥用，保护服务稳定性

**文件说明**:
- IP 限流、API Key 限流、滑动窗口算法

### `/internal/sync` - 同步客户端
**作用**: 多实例同步（未来功能）

**文件说明**:
- 分布式部署时的数据同步

### `/internal/utils` - 工具函数
**作用**: 通用辅助函数

**文件说明**:
- 字符串处理、时间格式化、加密解密等

## `/frontend` - Web 管理控制台

### 主页面
- `index.html` - 管理控制台主页面（Vue.js 应用容器）
- `login.html` - 登录页面（密码认证）

### `/frontend/js` - JavaScript 模块
**作用**: 前端业务逻辑

**文件说明**:
- `app.js` - Vue.js 应用入口，路由配置，全局状态管理
- `accounts.js` - AWS Kiro 账号管理模块（添加、删除、刷新、批量导入）
- `users.js` - 用户管理模块（创建、编辑、删除用户）
- `settings.js` - 系统设置模块（API Key、限流、日志保留）
- `chat.js` - 聊天测试界面（支持流式对话、模型选择）
- `logs.js` - 请求日志查看（分页、筛选、统计图表）
- `ips.js` - IP 黑名单管理（封禁、解封、查看历史）
- `serverLogs.js` - 服务器日志实时查看
- `api.js` - API 请求封装（统一错误处理、认证）
- `common.js` - 公共函数（日期格式化、文件下载）
- `utils.js` - 工具函数（验证、转换）
- `ui.js` - UI 组件（模态框、提示、加载动画）
- `devtools.js` - 开发者工具（调试、性能监控）

### `/frontend/css` - 样式文件
**作用**: 界面样式定义

**文件说明**:
- 响应式布局、主题配色、动画效果

### `/frontend/img` - 图片资源
**作用**: 界面图标和图片

**文件说明**:
- Logo、图标、占位图

### `/frontend/vendor` - 第三方库
**作用**: 前端依赖库（无需 npm）

**目录说明**:
- `/frontend/vendor/js` - JavaScript 库
  - `vue.global.js` - Vue.js 3 框架（响应式 UI）
  - `marked.min.js` - Markdown 渲染（聊天消息格式化）
  - `highlight.min.js` - 代码高亮（代码块语法高亮）
- `/frontend/vendor/css` - CSS 库
  - 第三方样式文件
- `/frontend/vendor/fonts` - 字体文件
  - 图标字体、自定义字体

## `/desktop` - 桌面应用（Wails）

**作用**: 使用 Wails 框架构建的跨平台桌面应用

### 目录说明
- `wails.json` - Wails 项目配置文件
- `/desktop/frontend` - 桌面应用前端（基于 Web 前端）
  - `/desktop/frontend/src` - 源代码
  - `/desktop/frontend/wailsjs` - Wails 自动生成的 JS 绑定
- `/desktop/build` - 构建配置和资源
  - `/desktop/build/bin` - 构建工具
  - `/desktop/build/darwin` - macOS 应用配置（图标、Info.plist）
  - `/desktop/build/windows` - Windows 应用配置（图标、manifest）
- `/desktop/embedded` - 嵌入式资源（打包到应用中）

## `/scripts` - 辅助脚本

**作用**: 自动化运维脚本

**文件说明**:
- `start.sh` - 启动服务脚本（后台运行、日志重定向）
- `stop.sh` - 停止服务脚本（优雅关闭）
- `setup.sh` - 环境配置脚本（依赖检查、数据库初始化）

## `/test` - 测试文件

**作用**: 单元测试和集成测试

**文件说明**:
- Go 测试文件（`*_test.go`）
- 测试数据和 Mock

## `/展示图` - 界面截图

**作用**: GitHub README 展示图片

**文件说明**:
- `dashboard.png` - 控制台主界面截图
- `accounts.png` - AWS Kiro 账号管理界面
- `chat.png` - 聊天测试界面
- `settings.png` - 系统设置界面
- `logs.png` - 请求日志界面

## `/assets` - 静态资源

**作用**: 项目静态资源（图标、文档等）

## `/dist` - 编译产物（自动生成，不提交到 Git）

**作用**: 构建脚本生成的可执行文件

### 目录说明
- `/dist/server` - 服务端程序（多平台）
  - `claude-server-linux-amd64.tar.gz` - Linux AMD64
  - `claude-server-linux-arm64.tar.gz` - Linux ARM64
  - `claude-server-darwin-amd64.tar.gz` - macOS Intel
  - `claude-server-darwin-arm64.tar.gz` - macOS Apple Silicon
  - `claude-server-windows-amd64.zip` - Windows AMD64
  - `claude-server-windows-arm64.zip` - Windows ARM64
- `/dist/desktop` - 桌面应用安装包
  - `Claude-API-Server-macOS.zip` - macOS 应用
  - `Claude-API-Server-Windows.zip` - Windows 安装程序

## 运行时目录（不提交到 Git）

### `/logs` - 日志文件
**作用**: 运行时日志输出

**文件说明**:
- `server.log` - 服务器日志
- `error.log` - 错误日志

### `/cache` - 缓存文件
**作用**: 临时缓存数据

**文件说明**:
- 账号池缓存、API 响应缓存

### `/.build-cache` - 构建缓存
**作用**: 加速重复构建

## 数据库文件（不提交到 Git）

- `data.sqlite3` - SQLite 数据库文件（存储账号、设置、日志）
- `data.sqlite3-shm` - SQLite 共享内存文件
- `data.sqlite3-wal` - SQLite 预写日志文件

## 配置文件（不提交到 Git）

- `config.yaml` - 用户自定义配置（数据库、端口、调试模式）
- `config.json` - JSON 格式配置（桌面应用使用）

## 目录依赖关系

```
main.go
  ↓
internal/api (HTTP 服务器)
  ↓
├─ internal/auth (认证)
├─ internal/database (数据库)
├─ internal/amazonq (Amazon Q 客户端)
├─ internal/claude (格式转换)
├─ internal/stream (流处理)
├─ internal/ratelimit (限流)
└─ internal/logger (日志)
  ↓
frontend (Web 界面)
```

## 开发工作流

1. **添加新功能**:
   - 在 `internal/` 对应模块添加业务逻辑
   - 在 `internal/api/handlers.go` 添加处理器
   - 在 `internal/api/routes.go` 注册路由
   - 在 `frontend/js/` 添加前端逻辑

2. **修改数据模型**:
   - 更新 `internal/models/` 中的模型定义
   - 更新 `internal/database/` 中的数据库操作
   - 运行数据库迁移

3. **添加新的 API 端点**:
   - 在 `internal/api/routes.go` 注册路由
   - 在 `internal/api/handlers.go` 实现处理器
   - 在 `frontend/js/api.js` 添加前端 API 调用

4. **构建和部署**:
   - 运行 `./build.sh` 构建所有平台
   - 运行 `./build.sh server` 仅构建服务端
   - 运行 `./build.sh desktop` 仅构建桌面应用
   - 产物在 `dist/` 目录
