# 更新日志

## v1.2.1 (2026-03-12)

### 新增功能

#### Claude 4.6 模型支持
- ✅ 新增 Claude Opus 4.6 模型支持
- ✅ 新增 Claude Sonnet 4.6 模型支持
- ✅ 支持完整规范名称映射（如 claude-opus-4-6-20260307）
- ✅ 前端聊天控制台默认使用 Claude Opus 4.6 Think 模型

### 系统优化

#### 模型名称统一
- ✅ 统一所有模型名称为短名称格式（如 claude-opus-4.6）
- ✅ 智能压缩模型列表使用短名称
- ✅ 强制模型列表使用短名称
- ✅ 提升用户体验，避免模型名称混淆

#### 配置优化
- ✅ 默认压缩模型更新为 claude-sonnet-4.5
- ✅ 前端默认压缩模型更新为 claude-sonnet-4.5

### 技术细节

#### 后端
- 更新 `internal/claude/converter.go` - 添加 4.6 模型映射
- 更新 `internal/models/settings.go` - 更新支持的模型列表
- 更新 `internal/compressor/config.go` - 更新默认压缩模型
- 更新 `internal/api/handlers.go` - 添加 4.6 模型选项

#### 前端
- 更新 `frontend/js/chat.js` - 默认模型改为 claude-opus-4.6-think
- 更新 `frontend/js/settings.js` - 默认压缩模型改为短名称
- 更新 `frontend/index.html` - 添加 4.6 模型说明

---

## v1.2.0 (2026-03-12)

### 新增功能

#### 用户管理系统
- ✅ 新增完整的用户管理功能
- ✅ 支持创建和管理多个用户，每个用户独立的 API Key
- ✅ 用户级别的配额管理（每日/每月 Token 限制）
- ✅ 请求频率限制（RPM - Requests Per Minute）
- ✅ 临时用户支持，可设置过期时间（1-365天）
- ✅ 用户使用统计（请求次数、Token 消耗、费用统计）
- ✅ 批量创建 VIP 用户功能

#### 前端界面
- ✅ 用户管理标签页，支持表格和卡片两种视图
- ✅ 用户列表显示过期时间和状态（已过期/即将过期/永不过期）
- ✅ 创建用户时支持选择过期日期
- ✅ 快捷按钮设置过期时间（1/3/7/15/30/90天）
- ✅ 用户统计图表和详细数据展示
- ✅ 日志筛选支持按用户和账号过滤

### 系统优化

#### 账号限制
- ✅ 删除100个账号限制，现在支持无限数量的账号

#### 日志功能
- ✅ 修复日志筛选下拉框数据加载问题
- ✅ 优化日志查询性能

#### 前端修复
- ✅ 修复前端白屏问题
- ✅ 完善前端文件结构

### API 变更

#### 新增接口
- `POST /v2/users` - 创建用户（支持过期时间）
- `POST /v2/users/temp` - 创建临时用户（指定天数）
- `POST /v2/users/batch-vip` - 批量创建 VIP 用户
- `GET /v2/users` - 获取用户列表
- `GET /v2/users/:id` - 获取用户详情
- `PATCH /v2/users/:id` - 更新用户信息
- `DELETE /v2/users/:id` - 删除用户
- `POST /v2/users/:id/regenerate-key` - 重新生成 API Key
- `GET /v2/users/:id/stats` - 获取用户统计
- `GET /v2/users/:id/ips` - 获取用户关联的 IP 列表

#### 数据库变更
- 新增 `users` 表
- 新增 `user_token_usage` 表
- User 表新增 `expires_at` 字段（过期时间）

### 技术细节

#### 后端
- 新增 `internal/api/handlers_users.go` - 用户管理处理器
- 更新 `internal/models/user.go` - 用户模型
- 更新 `internal/database/users.go` - 用户数据库操作
- 更新 `internal/config/config.go` - 配置管理

#### 前端
- 更新 `frontend/js/users.js` - 用户管理逻辑
- 更新 `frontend/js/app.js` - 应用主逻辑
- 更新 `frontend/index.html` - 用户界面

### 安全性
- ✅ 用户过期检查在 API 认证阶段自动执行
- ✅ 过期用户自动拒绝访问（返回 401）
- ✅ 支持永不过期的用户（ExpiresAt = nil）

### 文档
- ✅ 更新 README.md，添加用户管理功能说明
- ✅ 更新 CHANGELOG.md

---

## v1.1.0 (之前版本)

### 核心功能
- AWS Kiro 账号池管理
- OpenAI 兼容 API
- Claude Messages API 支持
- Web 管理控制台
- 请求日志和统计
- IP 黑名单管理
- 系统设置管理

---

## 升级说明

### 从 v1.1.0 升级到 v1.2.0

1. **数据库自动迁移**
   - GORM 会自动添加新的表和字段
   - 无需手动执行 SQL

2. **配置兼容**
   - 所有现有配置保持兼容
   - 无需修改配置文件

3. **API 兼容**
   - 所有现有 API 保持兼容
   - 新增的用户管理 API 不影响现有功能

4. **升级步骤**
   ```bash
   # 1. 备份数据库
   cp data.sqlite3 data.sqlite3.backup

   # 2. 停止服务
   # systemctl stop claude-api  # 如果使用 systemd

   # 3. 替换二进制文件
   # 下载新版本并替换

   # 4. 启动服务
   ./claude-api
   # systemctl start claude-api  # 如果使用 systemd

   # 5. 验证
   # 访问 http://localhost:62311 检查新功能
   ```

---

## 已知问题

### v1.2.0
- 无

---

## 贡献者

感谢所有为本项目做出贡献的开发者！

---

## 反馈

如有问题或建议，请提交 [Issue](https://github.com/kkddytd/claude-api/issues)。
