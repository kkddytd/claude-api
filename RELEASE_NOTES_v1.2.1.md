# Claude API v1.2.1 发布说明

发布日期: 2026-03-12

## 🎉 新增功能

### Claude 4.6 模型支持
- ✅ 新增 **Claude Opus 4.6** 模型 - 最新最强推理能力
- ✅ 新增 **Claude Sonnet 4.6** 模型 - 最新平衡性能与速度
- ✅ 支持完整规范名称映射（如 `claude-opus-4-6-20260307`）
- ✅ 前端聊天控制台默认使用 **Claude Opus 4.6 Think** 模型

### 模型名称统一优化
- ✅ 统一所有模型名称为短名称格式（如 `claude-opus-4.6`）
- ✅ Chat 会话、智能压缩、强制模型三个功能使用相同的模型名称
- ✅ 提升用户体验，避免模型名称混淆

## 📦 支持的模型

### Claude 4.6（最新）
- `claude-opus-4.6` / `claude-opus-4-6-20260307` - 最新最强推理能力
- `claude-opus-4.6-think` - 最新最强+深度思考
- `claude-sonnet-4.6` / `claude-sonnet-4-6-20260307` - 最新平衡性能与速度
- `claude-sonnet-4.6-think` - 最新平衡+深度思考

### Claude 4.5
- `claude-opus-4.5` / `claude-opus-4-5-20251101` - 强推理能力
- `claude-opus-4.5-think` - 强推理+深度思考
- `claude-sonnet-4.5` / `claude-sonnet-4-5-20250929` - 平衡性能与速度
- `claude-sonnet-4.5-think` - 平衡性能+深度思考
- `claude-haiku-4.5` / `claude-haiku-4-5-20251001` - 轻量高效

### 其他
- `claude-sonnet-4` / `claude-sonnet-4-20250514`
- `auto` - 自动选择最佳模型

## 📋 升级说明

### 从 v1.2.0 升级

1. **备份数据库**
   ```bash
   cp data.sqlite3 data.sqlite3.backup
   ```

2. **停止服务**
   ```bash
   # systemctl stop claude-api  # 如果使用 systemd
   ```

3. **替换二进制文件**
   - 下载新版本并替换旧文件

4. **启动服务**
   ```bash
   ./claude-server
   # systemctl start claude-api  # 如果使用 systemd
   ```

5. **验证**
   - 访问 http://localhost:62311
   - 检查聊天控制台中的模型列表
   - 确认可以看到 Claude 4.6 模型

## 🔄 API 兼容性

- ✅ 所有现有 API 保持兼容
- ✅ 新增模型可直接使用，无需修改代码
- ✅ 旧的规范名称仍然支持（如 `claude-opus-4-5-20251101`）

## ⚠️ 注意事项

1. **模型名称变化**
   - 智能压缩和强制模型的配置现在使用短名称
   - 如果之前配置了完整名称，建议更新为短名称
   - 旧的完整名称仍然可以正常工作

2. **默认模型变化**
   - 聊天控制台默认模型改为 `claude-opus-4.6-think`
   - 如果需要使用其他模型，可在界面中选择

3. **Haiku 模型特性**
   - 强制模型功能不会影响 Haiku 模型
   - 这是设计特性，保留轻量级模型的选择

## 🐛 已知问题

- 无

## 📝 完整更新日志

查看 [CHANGELOG.md](https://github.com/kkddytd/claude-api/blob/main/CHANGELOG.md) 获取完整的更新日志。

## 🙏 致谢

感谢所有为本项目做出贡献的开发者！

## 📮 反馈

如有问题或建议，请提交 [Issue](https://github.com/kkddytd/claude-api/issues)。
