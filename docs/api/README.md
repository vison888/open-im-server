# OpenIM Server API 接口文档

## 概述

OpenIM Server 提供了完整的即时通讯 API 接口，支持用户管理、好友关系、群组管理、消息收发等功能。所有接口均采用 RESTful 风格设计，使用 JSON 格式进行数据交换。

## 接口文档目录

### 核心业务模块

- **[认证接口 (Auth)](./auth.md)** - 用户登录认证、Token 管理
- **[用户接口 (User)](./user.md)** - 用户注册、信息管理、在线状态
- **[好友接口 (Friend)](./friend.md)** - 好友添加、删除、黑名单管理
- **[群组接口 (Group)](./group.md)** - 群组创建、成员管理、权限控制
- **[消息接口 (Message)](./message.md)** - 消息发送、接收、历史记录
- **[会话接口 (Conversation)](./conversation.md)** - 会话列表、未读消息管理

### 扩展功能模块

- **[第三方服务接口 (Third)](./third.md)** - 文件上传、推送服务、日志管理
- **[JS SDK 接口 (JSSDK)](./jssdk.md)** - Web端专用接口
- **[管理接口 (Admin)](./admin.md)** - 系统监控、服务发现

## 通用说明

### 请求格式

所有POST接口均使用JSON格式：

```http
POST /api/endpoint
Content-Type: application/json

{
  "param1": "value1",
  "param2": "value2"
}
```

### 响应格式

统一的响应格式：

```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    // 具体数据
  }
}
```

### 认证机制

除白名单接口外，所有接口都需要在请求头中携带Token：

```http
Token: your_access_token_here
```

**白名单接口**：
- `POST /auth/get_admin_token`
- `POST /auth/parse_token`

### 错误码说明

| 错误码 | 错误信息 | 说明 |
|--------|----------|------|
| 0 | success | 成功 |
| 1001 | 参数错误 | 请求参数不符合要求 |
| 1002 | 权限不足 | 没有执行该操作的权限 |
| 1003 | 用户不存在 | 指定的用户不存在 |
| 1004 | 群组不存在 | 指定的群组不存在 |
| 1005 | Token无效 | Token已过期或无效 |

### 平台类型常量

| 值 | 平台 | 说明 |
|----|------|------|
| 1 | iOS | iOS平台 |
| 2 | Android | Android平台 |
| 3 | Windows | Windows平台 |
| 4 | MacOS | MacOS平台 |
| 5 | Web | Web平台 |
| 6 | Linux | Linux平台 |
| 7 | iPad | iPad平台 |
| 8 | AndroidPad | Android平板 |
| 999 | Admin | 管理员平台 |

### 消息类型常量

| 值 | 类型 | 说明 |
|----|------|------|
| 101 | Text | 文本消息 |
| 102 | Picture | 图片消息 |
| 103 | Voice | 语音消息 |
| 104 | Video | 视频消息 |
| 105 | File | 文件消息 |
| 106 | AtText | @消息 |
| 107 | Merger | 合并消息 |
| 108 | Card | 名片消息 |
| 109 | Location | 位置消息 |
| 110 | Custom | 自定义消息 |
| 111 | Revoke | 撤回消息 |
| 112 | Quote | 引用回复消息 |
| 113 | Typing | 正在输入 |
| 1201 | OANotification | OA通知 |
| 1400 | BusinessNotification | 业务通知 |

### 会话类型常量

| 值 | 类型 | 说明 |
|----|------|------|
| 1 | SingleChatType | 单聊 |
| 2 | WriteGroupChatType | 群聊 |
| 3 | ReadGroupChatType | 只读群聊 |
| 4 | NotificationChatType | 通知聊天 |

## 开发指南

### 集成步骤

1. **获取管理员Token** - 使用配置的密钥获取管理员权限
2. **用户注册** - 批量注册用户账号
3. **获取用户Token** - 为用户获取访问令牌
4. **业务操作** - 进行用户管理、消息收发等操作

### 最佳实践

1. **Token管理**: 合理设置Token过期时间，及时刷新
2. **错误处理**: 根据错误码进行相应的业务处理
3. **批量操作**: 优先使用批量接口提高效率
4. **消息类型**: 根据业务需求选择合适的消息类型
5. **权限控制**: 注意接口的权限要求，避免越权操作

### 注意事项

1. 所有时间戳均为毫秒级时间戳
2. 文件上传需要先通过第三方服务接口获取上传凭证
3. 群聊消息需要指定groupID，单聊消息需要指定recvID
4. 批量操作接口通常有数量限制，具体限制请参考各接口说明
5. 管理员权限接口需要配置的管理员用户ID才能调用

## 联系支持

如有问题，请参考：
- [OpenIM GitHub](https://github.com/OpenIMSDK/Open-IM-Server)
- [官方文档](https://docs.openim.io/) 