# OpenIM Server API 网关接口文档

## 概览

本文档详细描述了 OpenIM Server 网关接口的所有 API 端点。所有接口采用 HTTP POST 方式调用（除特别说明外），请求和响应格式为 JSON。

### 基础信息

- **基础 URL**: `http://your-server:10002`
- **认证方式**: Bearer Token（通过 `token` 请求头传递）
- **内容类型**: `application/json`
- **操作 ID**: 通过 `operationID` 请求头传递

### 通用响应格式

```json
{
  "errCode": 0,           // 错误码，0 表示成功
  "errMsg": "",           // 错误信息
  "errDlt": "",           // 错误详情
  "data": {}              // 响应数据
}
```

## 1. 用户管理模块 (User)

### 1.1 用户注册

**接口**: `POST /user/user_register`

**描述**: 批量注册用户账户

**权限**: 需要管理员权限

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| users | array | 是 | 用户信息数组 |

**users 数组元素结构**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| userID | string | 是 | 用户唯一标识，不能包含冒号 |
| nickname | string | 否 | 用户昵称 |
| faceURL | string | 否 | 用户头像 URL |
| ex | string | 否 | 扩展字段 |
| appMangerLevel | int32 | 否 | 应用管理员等级 |
| globalRecvMsgOpt | int32 | 否 | 全局消息接收选项 |

**响应**: 成功时返回空数据

### 1.2 更新用户信息

**接口**: `POST /user/update_user_info`

**描述**: 更新用户基础信息（已废弃，建议使用 update_user_info_ex）

**权限**: 用户本人或管理员

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| userInfo | object | 是 | 用户信息对象 |

**userInfo 对象结构**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| userID | string | 是 | 用户 ID |
| nickname | string | 否 | 新昵称 |
| faceURL | string | 否 | 新头像 URL |
| ex | string | 否 | 扩展信息 |

### 1.3 更新用户信息（扩展版）

**接口**: `POST /user/update_user_info_ex`

**描述**: 更新用户信息的扩展版本，支持 null 值处理

**权限**: 用户本人或管理员

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| userInfo | object | 是 | 用户信息对象 |

**userInfo 对象结构**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| userID | string | 是 | 用户 ID |
| nickname | object | 否 | 昵称包装对象 {value: string} |
| faceURL | object | 否 | 头像包装对象 {value: string} |
| ex | object | 否 | 扩展信息包装对象 {value: string} |

### 1.4 设置全局消息接收选项

**接口**: `POST /user/set_global_msg_recv_opt`

**描述**: 设置用户的全局消息接收选项

**权限**: 用户本人或管理员

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| userID | string | 是 | 用户 ID |
| globalRecvMsgOpt | int32 | 是 | 全局消息接收选项（0:正常接收，1:不接收，2:在线时接收） |

### 1.5 获取用户信息

**接口**: `POST /user/get_users_info`

**描述**: 批量获取用户公开信息

**权限**: 已认证用户

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| userIDs | array | 是 | 用户 ID 数组 |

**响应数据**:

| 参数名 | 类型 | 描述 |
|-------|------|------|
| usersInfo | array | 用户信息数组 |

### 1.6 获取所有用户 ID

**接口**: `POST /user/get_all_users_uid`

**描述**: 获取所有用户的 ID 列表

**权限**: 管理员

**请求参数**: 无

**响应数据**:

| 参数名 | 类型 | 描述 |
|-------|------|------|
| userIDs | array | 所有用户 ID 数组 |

### 1.7 账户检查

**接口**: `POST /user/account_check`

**描述**: 检查用户账户是否存在

**权限**: 管理员

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| checkUserIDs | array | 是 | 要检查的用户 ID 数组 |

**响应数据**:

| 参数名 | 类型 | 描述 |
|-------|------|------|
| results | array | 检查结果数组 |

### 1.8 分页获取用户

**接口**: `POST /user/get_users`

**描述**: 分页获取用户列表

**权限**: 管理员

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| pagination | object | 是 | 分页参数 |
| userID | string | 否 | 用户 ID 关键词 |
| nickName | string | 否 | 昵称关键词 |

### 1.9 获取用户在线状态

**接口**: `POST /user/get_users_online_status`

**描述**: 获取用户在线状态

**权限**: 已认证用户

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| userIDs | array | 是 | 用户 ID 数组 |

**响应数据**:

| 参数名 | 类型 | 描述 |
|-------|------|------|
| successResult | array | 在线状态结果数组 |

### 1.10 获取用户在线令牌详情

**接口**: `POST /user/get_users_online_token_detail`

**描述**: 获取用户在线令牌详细信息

**权限**: 管理员

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| userIDs | array | 是 | 用户 ID 数组 |

## 2. 好友管理模块 (Friend)

### 2.1 添加好友

**接口**: `POST /friend/add_friend`

**描述**: 发送好友申请

**权限**: 已认证用户

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| toUserID | string | 是 | 被添加用户 ID |
| reqMsg | string | 否 | 申请消息 |
| ex | string | 否 | 扩展字段 |

### 2.2 响应好友申请

**接口**: `POST /friend/add_friend_response`

**描述**: 处理好友申请

**权限**: 已认证用户

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| toUserID | string | 是 | 申请人用户 ID |
| handleResult | int32 | 是 | 处理结果（1:同意，-1:拒绝） |
| handleMsg | string | 否 | 处理消息 |

### 2.3 删除好友

**接口**: `POST /friend/delete_friend`

**描述**: 删除好友关系

**权限**: 已认证用户

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| friendUserID | string | 是 | 好友用户 ID |

### 2.4 获取好友列表

**接口**: `POST /friend/get_friend_list`

**描述**: 分页获取好友列表

**权限**: 已认证用户

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| pagination | object | 是 | 分页参数 |

### 2.5 设置好友备注

**接口**: `POST /friend/set_friend_remark`

**描述**: 设置好友备注名

**权限**: 已认证用户

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| toUserID | string | 是 | 好友用户 ID |
| remark | string | 是 | 备注名 |

### 2.6 添加黑名单

**接口**: `POST /friend/add_black`

**描述**: 将用户添加到黑名单

**权限**: 已认证用户

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| toUserID | string | 是 | 目标用户 ID |
| ex | string | 否 | 扩展字段 |

### 2.7 获取黑名单

**接口**: `POST /friend/get_black_list`

**描述**: 分页获取黑名单列表

**权限**: 已认证用户

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| pagination | object | 是 | 分页参数 |

### 2.8 移除黑名单

**接口**: `POST /friend/remove_black`

**描述**: 从黑名单中移除用户

**权限**: 已认证用户

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| toUserID | string | 是 | 目标用户 ID |

## 3. 群组管理模块 (Group)

### 3.1 创建群组

**接口**: `POST /group/create_group`

**描述**: 创建新群组

**权限**: 已认证用户

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| groupInfo | object | 是 | 群组信息 |
| memberUserIDs | array | 否 | 初始成员用户 ID 数组 |
| adminUserIDs | array | 否 | 初始管理员用户 ID 数组 |

**groupInfo 对象结构**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| groupName | string | 是 | 群组名称 |
| introduction | string | 否 | 群组介绍 |
| notification | string | 否 | 群组公告 |
| faceURL | string | 否 | 群组头像 URL |
| ex | string | 否 | 扩展字段 |
| groupType | int32 | 否 | 群组类型 |
| needVerification | int32 | 否 | 入群验证选项 |

### 3.2 设置群组信息

**接口**: `POST /group/set_group_info`

**描述**: 更新群组基础信息

**权限**: 群主或管理员

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| groupInfo | object | 是 | 群组信息对象 |

### 3.3 加入群组

**接口**: `POST /group/join_group`

**描述**: 申请加入群组

**权限**: 已认证用户

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| groupID | string | 是 | 群组 ID |
| reqMsg | string | 否 | 申请消息 |
| joinSource | int32 | 否 | 加入来源 |
| ex | string | 否 | 扩展字段 |

### 3.4 退出群组

**接口**: `POST /group/quit_group`

**描述**: 用户主动退出群组

**权限**: 群成员

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| groupID | string | 是 | 群组 ID |

### 3.5 踢出群成员

**接口**: `POST /group/kick_group`

**描述**: 踢出群成员

**权限**: 群主或管理员

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| groupID | string | 是 | 群组 ID |
| kickedUserIDs | array | 是 | 被踢出的用户 ID 数组 |
| reason | string | 否 | 踢出原因 |

### 3.6 邀请用户入群

**接口**: `POST /group/invite_user_to_group`

**描述**: 邀请用户加入群组

**权限**: 群成员（根据群设置）

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| groupID | string | 是 | 群组 ID |
| invitedUserIDs | array | 是 | 被邀请用户 ID 数组 |
| reason | string | 否 | 邀请理由 |

### 3.7 转让群组

**接口**: `POST /group/transfer_group`

**描述**: 转让群主权限

**权限**: 群主

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| groupID | string | 是 | 群组 ID |
| newOwnerUserID | string | 是 | 新群主用户 ID |

### 3.8 解散群组

**接口**: `POST /group/dismiss_group`

**描述**: 解散群组

**权限**: 群主

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| groupID | string | 是 | 群组 ID |

### 3.9 禁言群成员

**接口**: `POST /group/mute_group_member`

**描述**: 禁言指定群成员

**权限**: 群主或管理员

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| groupID | string | 是 | 群组 ID |
| userID | string | 是 | 被禁言用户 ID |
| mutedSeconds | int32 | 是 | 禁言时长（秒） |

### 3.10 取消禁言群成员

**接口**: `POST /group/cancel_mute_group_member`

**描述**: 取消群成员禁言

**权限**: 群主或管理员

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| groupID | string | 是 | 群组 ID |
| userID | string | 是 | 用户 ID |

### 3.11 全群禁言

**接口**: `POST /group/mute_group`

**描述**: 开启全群禁言

**权限**: 群主或管理员

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| groupID | string | 是 | 群组 ID |

### 3.12 取消全群禁言

**接口**: `POST /group/cancel_mute_group`

**描述**: 取消全群禁言

**权限**: 群主或管理员

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| groupID | string | 是 | 群组 ID |

## 4. 认证模块 (Auth)

### 4.1 获取管理员令牌

**接口**: `POST /auth/get_admin_token`

**描述**: 获取管理员访问令牌

**权限**: 公开接口

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| secret | string | 是 | 管理员密钥 |
| userID | string | 是 | 管理员用户 ID |

**响应数据**:

| 参数名 | 类型 | 描述 |
|-------|------|------|
| token | string | 访问令牌 |
| expireTime | int64 | 过期时间戳 |

### 4.2 获取用户令牌

**接口**: `POST /auth/get_user_token`

**描述**: 获取普通用户访问令牌

**权限**: 公开接口

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| secret | string | 是 | 应用密钥 |
| platformID | int32 | 是 | 平台 ID |
| userID | string | 是 | 用户 ID |

### 4.3 解析令牌

**接口**: `POST /auth/parse_token`

**描述**: 解析并验证访问令牌

**权限**: 公开接口

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| token | string | 是 | 要解析的令牌 |

### 4.4 强制下线

**接口**: `POST /auth/force_logout`

**描述**: 强制用户下线

**权限**: 管理员

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| userID | string | 是 | 用户 ID |
| platformID | int32 | 否 | 平台 ID（可选） |

## 5. 第三方服务模块 (Third)

### 5.1 FCM 令牌更新

**接口**: `POST /third/fcm_update_token`

**描述**: 更新 Firebase Cloud Messaging 令牌

**权限**: 已认证用户

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| platform | int32 | 是 | 平台 ID |
| fcmToken | string | 是 | FCM 令牌 |
| account | string | 是 | 用户账户 |
| expireTime | int64 | 是 | 过期时间 |

### 5.2 设置应用角标

**接口**: `POST /third/set_app_badge`

**描述**: 设置应用程序角标数量

**权限**: 已认证用户

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| userID | string | 是 | 用户 ID |
| appUnreadCount | int32 | 是 | 未读消息数量 |

### 5.3 对象存储相关接口

#### 5.3.1 初始化分片上传

**接口**: `POST /object/initiate_multipart_upload`

**描述**: 初始化分片上传任务

**权限**: 已认证用户

#### 5.3.2 完成分片上传

**接口**: `POST /object/complete_multipart_upload`

**描述**: 完成分片上传任务

**权限**: 已认证用户

#### 5.3.3 获取访问 URL

**接口**: `POST /object/access_url`

**描述**: 获取文件访问 URL

**权限**: 已认证用户

#### 5.3.4 对象重定向

**接口**: `GET /object/*name`

**描述**: 对象访问重定向

**权限**: 已认证用户

### 5.4 日志管理

#### 5.4.1 上传日志

**接口**: `POST /third/logs/upload`

**描述**: 上传应用日志

**权限**: 已认证用户

#### 5.4.2 删除日志

**接口**: `POST /third/logs/delete`

**描述**: 删除指定日志

**权限**: 管理员

#### 5.4.3 搜索日志

**接口**: `POST /third/logs/search`

**描述**: 搜索日志内容

**权限**: 管理员

## 6. 消息模块 (Message)

### 6.1 发送消息

**接口**: `POST /msg/send_msg`

**描述**: 发送单聊或群聊消息

**权限**: 已认证用户

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| sendID | string | 是 | 发送者 ID |
| recvID | string | 否 | 接收者 ID（单聊时必填） |
| groupID | string | 否 | 群组 ID（群聊时必填） |
| senderPlatformID | int32 | 是 | 发送者平台 ID |
| senderNickname | string | 否 | 发送者昵称 |
| senderFaceURL | string | 否 | 发送者头像 |
| sessionType | int32 | 是 | 会话类型（1:单聊，2:群聊） |
| msgFrom | int32 | 是 | 消息来源 |
| contentType | int32 | 是 | 消息内容类型 |
| content | object | 是 | 消息内容 |
| isOnlineOnly | bool | 否 | 是否仅在线接收 |
| notOfflinePush | bool | 否 | 是否不推送离线消息 |
| offlinePushInfo | object | 否 | 离线推送信息 |

### 6.2 批量发送消息

**接口**: `POST /msg/batch_send_msg`

**描述**: 批量发送消息给多个用户

**权限**: 已认证用户

### 6.3 撤回消息

**接口**: `POST /msg/revoke_msg`

**描述**: 撤回已发送的消息

**权限**: 消息发送者或管理员

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| conversationID | string | 是 | 会话 ID |
| seq | int64 | 是 | 消息序列号 |
| userID | string | 是 | 用户 ID |

### 6.4 标记消息为已读

**接口**: `POST /msg/mark_msgs_as_read`

**描述**: 标记指定消息为已读

**权限**: 已认证用户

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| conversationID | string | 是 | 会话 ID |
| seqs | array | 是 | 消息序列号数组 |

### 6.5 清空会话消息

**接口**: `POST /msg/clear_conversation_msg`

**描述**: 清空指定会话的消息

**权限**: 已认证用户

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| conversationIDs | array | 是 | 会话 ID 数组 |
| deleteSyncOpt | bool | 否 | 是否同步删除 |

### 6.6 删除消息

**接口**: `POST /msg/delete_msgs`

**描述**: 删除指定消息

**权限**: 已认证用户

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| conversationID | string | 是 | 会话 ID |
| seqs | array | 是 | 消息序列号数组 |

### 6.7 获取最新序列号

**接口**: `POST /msg/newest_seq`

**描述**: 获取用户最新消息序列号

**权限**: 已认证用户

### 6.8 拉取消息

**接口**: `POST /msg/pull_msg_by_seq`

**描述**: 根据序列号拉取消息

**权限**: 已认证用户

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| conversationID | string | 是 | 会话 ID |
| seqs | array | 是 | 序列号数组 |

### 6.9 搜索消息

**接口**: `POST /msg/search_msg`

**描述**: 搜索消息内容

**权限**: 已认证用户

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| conversationID | string | 否 | 会话 ID |
| keyWords | array | 是 | 搜索关键词 |
| sendTime | int64 | 否 | 发送时间 |
| sessionType | int32 | 否 | 会话类型 |
| pagination | object | 是 | 分页参数 |

## 7. 会话模块 (Conversation)

### 7.1 获取排序会话列表

**接口**: `POST /conversation/get_sorted_conversation_list`

**描述**: 获取排序后的会话列表

**权限**: 已认证用户

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| pagination | object | 是 | 分页参数 |

### 7.2 获取所有会话

**接口**: `POST /conversation/get_all_conversations`

**描述**: 获取用户所有会话

**权限**: 已认证用户

### 7.3 获取单个会话

**接口**: `POST /conversation/get_conversation`

**描述**: 获取指定会话信息

**权限**: 已认证用户

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| conversationID | string | 是 | 会话 ID |

### 7.4 获取多个会话

**接口**: `POST /conversation/get_conversations`

**描述**: 批量获取会话信息

**权限**: 已认证用户

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| conversationIDs | array | 是 | 会话 ID 数组 |

### 7.5 设置会话

**接口**: `POST /conversation/set_conversations`

**描述**: 设置会话属性

**权限**: 已认证用户

**请求参数**:

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| conversation | object | 是 | 会话对象 |

## 8. 统计模块 (Statistics)

### 8.1 用户注册统计

**接口**: `POST /statistics/user/register`

**描述**: 获取用户注册统计数据

**权限**: 管理员

### 8.2 活跃用户统计

**接口**: `POST /statistics/user/active`

**描述**: 获取活跃用户统计数据

**权限**: 管理员

### 8.3 群组创建统计

**接口**: `POST /statistics/group/create`

**描述**: 获取群组创建统计数据

**权限**: 管理员

### 8.4 活跃群组统计

**接口**: `POST /statistics/group/active`

**描述**: 获取活跃群组统计数据

**权限**: 管理员

## 9. JS SDK 模块 (JSSDK)

### 9.1 获取会话列表

**接口**: `POST /jssdk/get_conversations`

**描述**: JS SDK 专用会话列表接口

**权限**: 已认证用户

### 9.2 获取活跃会话

**接口**: `POST /jssdk/get_active_conversations`

**描述**: 获取活跃会话列表

**权限**: 已认证用户

## 10. 监控发现模块 (Prometheus Discovery)

提供各种服务的监控发现接口：

- `GET /prometheus_discovery/api` - API 服务发现
- `GET /prometheus_discovery/user` - 用户服务发现
- `GET /prometheus_discovery/group` - 群组服务发现
- `GET /prometheus_discovery/msg` - 消息服务发现
- `GET /prometheus_discovery/friend` - 好友服务发现
- `GET /prometheus_discovery/conversation` - 会话服务发现
- `GET /prometheus_discovery/third` - 第三方服务发现
- `GET /prometheus_discovery/auth` - 认证服务发现
- `GET /prometheus_discovery/push` - 推送服务发现
- `GET /prometheus_discovery/msg_gateway` - 消息网关发现
- `GET /prometheus_discovery/msg_transfer` - 消息传输服务发现

## 11. 配置管理模块 (Config)

### 11.1 获取配置列表

**接口**: `POST /config/get_config_list`

**描述**: 获取配置项列表

**权限**: 管理员

### 11.2 获取配置

**接口**: `POST /config/get_config`

**描述**: 获取指定配置项

**权限**: 管理员

### 11.3 设置配置

**接口**: `POST /config/set_config`

**描述**: 设置配置项

**权限**: 管理员

### 11.4 重置配置

**接口**: `POST /config/reset_config`

**描述**: 重置配置项到默认值

**权限**: 管理员

### 11.5 重启服务

**接口**: `POST /restart`

**描述**: 重启服务

**权限**: 管理员

## 12. 分页参数说明

分页参数通用结构：

| 参数名 | 类型 | 必填 | 描述 |
|-------|------|------|------|
| pageNumber | int32 | 是 | 页码（从1开始） |
| showNumber | int32 | 是 | 每页显示数量 |

## 13. 错误码说明

| 错误码 | 描述 |
|-------|------|
| 0 | 成功 |
| 1001 | 参数错误 |
| 1002 | 数据库错误 |
| 1003 | 服务器内部错误 |
| 1004 | 网络错误 |
| 1005 | 权限不足 |
| 1006 | 用户不存在 |
| 1007 | 群组不存在 |
| 1008 | 令牌无效 |
| 1009 | 令牌过期 |

## 14. 使用示例

### 用户注册示例

```bash
curl -X POST http://your-server:10002/user/user_register \
  -H "Content-Type: application/json" \
  -H "token: your-admin-token" \
  -H "operationID: 123456789" \
  -d '{
    "users": [
      {
        "userID": "user001",
        "nickname": "张三",
        "faceURL": "http://example.com/avatar.jpg"
      }
    ]
  }'
```

### 发送消息示例

```bash
curl -X POST http://your-server:10002/msg/send_msg \
  -H "Content-Type: application/json" \
  -H "token: your-user-token" \
  -H "operationID: 123456789" \
  -d '{
    "sendID": "user001",
    "recvID": "user002",
    "senderPlatformID": 1,
    "sessionType": 1,
    "msgFrom": 100,
    "contentType": 101,
    "content": {
      "text": "Hello, World!"
    }
  }'
```

## 重要说明

1. **认证**: 除了白名单接口（如获取令牌、解析令牌），所有接口都需要在请求头中携带有效的 `token`。

2. **操作 ID**: 建议在每个请求中包含唯一的 `operationID` 用于日志追踪和问题排查。

3. **错误处理**: 所有接口都遵循统一的错误响应格式，客户端应根据 `errCode` 判断请求是否成功。

4. **版本兼容**: 本文档基于 OpenIM Server v3 版本，不同版本间可能存在接口差异。

5. **速率限制**: 建议客户端实现合理的请求频率控制，避免触发服务端限流。

---

**版本**: v3.0
**更新时间**: 2024年
**维护者**: OpenIM 开发团队 