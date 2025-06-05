# OpenIM Server API 接口文档

## 目录
- [认证相关接口 (Auth)](#认证相关接口-auth)
- [用户相关接口 (User)](#用户相关接口-user)
- [好友相关接口 (Friend)](#好友相关接口-friend)
- [群组相关接口 (Group)](#群组相关接口-group)
- [消息相关接口 (Message)](#消息相关接口-message)
- [会话相关接口 (Conversation)](#会话相关接口-conversation)
- [第三方服务接口 (Third)](#第三方服务接口-third)
- [JS SDK 接口 (JSSDK)](#js-sdk-接口-jssdk)
- [监控发现接口 (Prometheus Discovery)](#监控发现接口-prometheus-discovery)

## 认证相关接口 (Auth)

### 1. 获取管理员Token
**接口地址**: `POST /auth/get_admin_token`

**功能描述**: 获取管理员访问权限的Token，用于管理员级别的API操作

**请求参数**: 
```json
{
  "userID": "admin_user_001",
  "secret": "your_admin_secret_key"
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| userID | string | 是 | 管理员用户ID，必须在系统配置的管理员用户列表中 |
| secret | string | 是 | 管理员密钥，必须与服务器配置的Secret一致 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
    "expireTimeSeconds": 7257600
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| token | string | JWT格式的访问令牌 |
| expireTimeSeconds | int64 | Token过期时间（秒），默认84天 |

### 2. 获取用户Token
**接口地址**: `POST /auth/get_user_token`

**功能描述**: 获取普通用户访问权限的Token（需要管理员权限调用）

**请求参数**: 
```json
{
  "userID": "user_001",
  "platformID": 1
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| userID | string | 是 | 目标用户ID，不能是管理员ID |
| platformID | int32 | 是 | 平台ID，见平台类型常量表，不能是999（管理员平台） |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
    "expireTimeSeconds": 7257600
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| token | string | JWT格式的访问令牌 |
| expireTimeSeconds | int64 | Token过期时间（秒），默认84天 |

### 3. 解析Token
**接口地址**: `POST /auth/parse_token`

**功能描述**: 解析和验证Token的有效性，获取Token中包含的用户信息

**请求参数**: 
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| token | string | 是 | 需要解析的JWT Token |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "userID": "user_001",
    "platformID": 1,
    "expireTimeSeconds": 1640995200
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| userID | string | Token中包含的用户ID |
| platformID | int32 | Token中包含的平台ID |
| expireTimeSeconds | int64 | Token过期时间戳（Unix时间戳） |

### 4. 强制登出
**接口地址**: `POST /auth/force_logout`

**功能描述**: 强制指定用户在指定平台登出，使其Token失效（需要管理员权限）

**请求参数**: 
```json
{
  "userID": "user_001",
  "platformID": 1
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| userID | string | 是 | 要强制登出的用户ID |
| platformID | int32 | 是 | 要登出的平台ID |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

## 用户相关接口 (User)

### 1. 用户注册
**接口地址**: `POST /user/user_register`

**功能描述**: 批量注册新用户（需要管理员权限）

**请求参数**: 
```json
{
  "users": [
    {
      "userID": "user_001",
      "nickname": "张三",
      "faceURL": "https://example.com/avatar/001.jpg",
      "ex": "{\"age\":25,\"city\":\"北京\"}",
      "appMangerLevel": 1,
      "globalRecvMsgOpt": 0
    }
  ]
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| users | array | 是 | 用户信息数组，不能为空 |
| users[].userID | string | 是 | 用户唯一标识，不能包含冒号:，不能重复 |
| users[].nickname | string | 否 | 用户昵称 |
| users[].faceURL | string | 否 | 用户头像URL |
| users[].ex | string | 否 | 扩展字段，可存储JSON格式的额外信息 |
| users[].appMangerLevel | int32 | 否 | 应用管理等级：1-普通用户，2-通知管理员 |
| users[].globalRecvMsgOpt | int32 | 否 | 全局消息接收选项：0-正常接收，1-不接收消息，2-仅接收在线消息 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

### 2. 更新用户信息
**接口地址**: `POST /user/update_user_info`

**功能描述**: 更新用户基本信息（已废弃，建议使用update_user_info_ex）

### 3. 更新用户信息扩展版
**接口地址**: `POST /user/update_user_info_ex`

**功能描述**: 更新用户基本信息，支持部分字段更新

**请求参数**: 
```json
{
  "userInfo": {
    "userID": "user_001",
    "nickname": "李四",
    "faceURL": "https://example.com/avatar/new_001.jpg",
    "ex": "{\"age\":26,\"city\":\"上海\"}"
  }
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| userInfo | object | 是 | 用户信息对象 |
| userInfo.userID | string | 是 | 要更新的用户ID |
| userInfo.nickname | string | 否 | 新的用户昵称，传null表示不更新 |
| userInfo.faceURL | string | 否 | 新的用户头像URL，传null表示不更新 |
| userInfo.ex | string | 否 | 新的扩展字段，传null表示不更新 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

### 4. 设置全局消息接收选项
**接口地址**: `POST /user/set_global_msg_recv_opt`

**功能描述**: 设置用户的全局消息接收选项

**请求参数**: 
```json
{
  "userID": "user_001",
  "globalRecvMsgOpt": 1
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| userID | string | 是 | 要设置的用户ID |
| globalRecvMsgOpt | int32 | 是 | 消息接收选项：0-正常接收，1-不接收消息，2-仅接收在线消息 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

### 5. 获取用户公开信息
**接口地址**: `POST /user/get_users_info`

**功能描述**: 批量获取指定用户的公开信息

**请求参数**: 
```json
{
  "userIDs": ["user_001", "user_002", "user_003"]
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| userIDs | array | 是 | 用户ID列表，最大支持1000个 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "usersInfo": [
      {
        "userID": "user_001",
        "nickname": "张三",
        "faceURL": "https://example.com/avatar/001.jpg",
        "ex": "{\"age\":25,\"city\":\"北京\"}",
        "createTime": 1640995200000,
        "appMangerLevel": 1,
        "globalRecvMsgOpt": 0
      }
    ]
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| usersInfo | array | 用户信息列表 |
| usersInfo[].userID | string | 用户唯一标识 |
| usersInfo[].nickname | string | 用户昵称 |
| usersInfo[].faceURL | string | 用户头像URL |
| usersInfo[].ex | string | 扩展字段 |
| usersInfo[].createTime | int64 | 用户创建时间（毫秒时间戳） |
| usersInfo[].appMangerLevel | int32 | 应用管理等级 |
| usersInfo[].globalRecvMsgOpt | int32 | 全局消息接收选项 |

### 6. 获取所有用户ID
**接口地址**: `POST /user/get_all_users_uid`

**功能描述**: 分页获取系统中所有用户的ID列表

**请求参数**: 
```json
{
  "pagination": {
    "pageNumber": 1,
    "showNumber": 100
  }
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| pagination | object | 是 | 分页参数 |
| pagination.pageNumber | int32 | 是 | 页码，从1开始 |
| pagination.showNumber | int32 | 是 | 每页显示数量，最大1000 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "total": 1500,
    "userIDs": ["user_001", "user_002", "user_003"]
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| total | int64 | 用户总数 |
| userIDs | array | 当前页用户ID列表 |

### 7. 账户检查
**接口地址**: `POST /user/account_check`

**功能描述**: 检查指定用户账户的注册状态（需要管理员权限）

**请求参数**: 
```json
{
  "checkUserIDs": ["user_001", "user_002", "user_003"]
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| checkUserIDs | array | 是 | 要检查的用户ID列表，不能有重复值 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "results": [
      {
        "userID": "user_001",
        "accountStatus": "registered"
      },
      {
        "userID": "user_002",
        "accountStatus": "unregistered"
      }
    ]
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| results | array | 检查结果列表 |
| results[].userID | string | 用户ID |
| results[].accountStatus | string | 账户状态：registered-已注册，unregistered-未注册 |

### 8. 获取用户列表
**接口地址**: `POST /user/get_users`

**功能描述**: 分页获取用户列表，支持按用户ID和昵称搜索

**请求参数**: 
```json
{
  "pagination": {
    "pageNumber": 1,
    "showNumber": 20
  },
  "userID": "",
  "nickName": ""
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| pagination | object | 是 | 分页参数 |
| pagination.pageNumber | int32 | 是 | 页码，从1开始 |
| pagination.showNumber | int32 | 是 | 每页显示数量，最大100 |
| userID | string | 否 | 用户ID关键词搜索，为空则不过滤 |
| nickName | string | 否 | 昵称关键词搜索，为空则不过滤 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "total": 158,
    "users": [
      {
        "userID": "user_001",
        "nickname": "张三",
        "faceURL": "https://example.com/avatar/001.jpg",
        "ex": "{\"age\":25}",
        "createTime": 1640995200000,
        "appMangerLevel": 1,
        "globalRecvMsgOpt": 0
      }
    ]
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| total | int32 | 符合条件的用户总数 |
| users | array | 当前页用户列表，字段说明同获取用户信息接口 |

### 9. 获取用户在线状态
**接口地址**: `POST /user/get_users_online_status`

**功能描述**: 获取指定用户的在线状态

**请求参数**:
```json
{
  "userIDs": ["user_001", "user_002", "user_003"]
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| userIDs | array | 是 | 用户ID列表，最大支持1000个 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "successResult": [
      {
        "userID": "user_001",
        "status": "online",
        "detailPlatformStatus": [
          {
            "platformID": 1,
            "status": "online",
            "connID": "conn_123",
            "isBackground": false,
            "token": "eyJhbGciOiJIUzI1NiIs..."
          },
          {
            "platformID": 2,
            "status": "online",
            "connID": "conn_456",
            "isBackground": true,
            "token": "eyJhbGciOiJIUzI1NiIs..."
          }
        ]
      }
    ]
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| successResult | array | 在线状态结果列表 |
| successResult[].userID | string | 用户ID |
| successResult[].status | string | 用户总体状态：online-在线，offline-离线 |
| successResult[].detailPlatformStatus | array | 各平台详细状态 |
| detailPlatformStatus[].platformID | int32 | 平台ID |
| detailPlatformStatus[].status | string | 该平台状态：online-在线，offline-离线 |
| detailPlatformStatus[].connID | string | 连接ID |
| detailPlatformStatus[].isBackground | bool | 是否为后台状态 |
| detailPlatformStatus[].token | string | 该平台的Token |

### 10. 获取用户在线Token详情
**接口地址**: `POST /user/get_users_online_token_detail`

**功能描述**: 获取用户的在线Token详细信息

### 11. 订阅用户状态
**接口地址**: `POST /user/subscribe_users_status`

**功能描述**: 订阅或取消订阅用户状态变化

### 12. 获取用户状态
**接口地址**: `POST /user/get_users_status`

**功能描述**: 获取用户的当前状态

### 13. 获取订阅的用户状态
**接口地址**: `POST /user/get_subscribe_users_status`

**功能描述**: 获取已订阅用户的状态

### 14. 用户命令处理 - 添加
**接口地址**: `POST /user/process_user_command_add`

**功能描述**: 处理用户相关的添加命令

### 15. 用户命令处理 - 删除
**接口地址**: `POST /user/process_user_command_delete`

**功能描述**: 处理用户相关的删除命令

### 16. 用户命令处理 - 更新
**接口地址**: `POST /user/process_user_command_update`

**功能描述**: 处理用户相关的更新命令

### 17. 用户命令处理 - 获取
**接口地址**: `POST /user/process_user_command_get`

**功能描述**: 处理用户相关的获取命令

### 18. 用户命令处理 - 获取全部
**接口地址**: `POST /user/process_user_command_get_all`

**功能描述**: 处理用户相关的获取全部命令

### 19. 添加通知账户
**接口地址**: `POST /user/add_notification_account`

**功能描述**: 添加通知账户

### 20. 更新通知账户信息
**接口地址**: `POST /user/update_notification_account`

**功能描述**: 更新通知账户信息

### 21. 搜索通知账户
**接口地址**: `POST /user/search_notification_account`

**功能描述**: 搜索通知账户

### 22. 用户注册统计
**接口地址**: `POST /statistics/user/register`

**功能描述**: 获取用户注册统计信息

## 好友相关接口 (Friend)

### 1. 申请添加好友
**接口地址**: `POST /friend/add_friend`

**功能描述**: 发送好友添加申请

### 2. 响应好友申请
**接口地址**: `POST /friend/add_friend_response`

**功能描述**: 同意或拒绝好友申请

### 3. 删除好友
**接口地址**: `POST /friend/delete_friend`

**功能描述**: 删除好友关系

### 4. 获取好友申请列表
**接口地址**: `POST /friend/get_friend_apply_list`

**功能描述**: 获取收到的好友申请列表

### 5. 获取指定好友申请
**接口地址**: `POST /friend/get_designated_friend_apply`

**功能描述**: 获取指定的好友申请信息

### 6. 获取自己的申请列表
**接口地址**: `POST /friend/get_self_friend_apply_list`

**功能描述**: 获取自己发送的好友申请列表

### 7. 获取好友列表
**接口地址**: `POST /friend/get_friend_list`

**功能描述**: 分页获取好友列表

### 8. 获取指定好友
**接口地址**: `POST /friend/get_designated_friends`

**功能描述**: 获取指定的好友信息

### 9. 设置好友备注
**接口地址**: `POST /friend/set_friend_remark`

**功能描述**: 设置好友的备注名

### 10. 添加黑名单
**接口地址**: `POST /friend/add_black`

**功能描述**: 将用户添加到黑名单

### 11. 获取黑名单列表
**接口地址**: `POST /friend/get_black_list`

**功能描述**: 分页获取黑名单列表

### 12. 获取指定黑名单
**接口地址**: `POST /friend/get_specified_blacks`

**功能描述**: 获取指定的黑名单用户信息

### 13. 移除黑名单
**接口地址**: `POST /friend/remove_black`

**功能描述**: 从黑名单中移除用户

### 14. 获取增量黑名单
**接口地址**: `POST /friend/get_incremental_blacks`

**功能描述**: 获取增量的黑名单变化（已废弃）

### 15. 导入好友
**接口地址**: `POST /friend/import_friend`

**功能描述**: 批量导入好友关系

### 16. 检查是否为好友
**接口地址**: `POST /friend/is_friend`

**功能描述**: 检查两个用户是否为好友关系

### 17. 获取好友ID列表
**接口地址**: `POST /friend/get_friend_id`

**功能描述**: 获取用户的所有好友ID列表

### 18. 获取指定好友信息
**接口地址**: `POST /friend/get_specified_friends_info`

**功能描述**: 获取指定好友的详细信息

### 19. 更新好友信息
**接口地址**: `POST /friend/update_friends`

**功能描述**: 批量更新好友信息

### 20. 获取增量好友
**接口地址**: `POST /friend/get_incremental_friends`

**功能描述**: 获取增量的好友变化

### 21. 获取完整好友用户ID列表
**接口地址**: `POST /friend/get_full_friend_user_ids`

**功能描述**: 获取完整的好友用户ID列表

### 22. 获取未处理申请数量
**接口地址**: `POST /friend/get_self_unhandled_apply_count`

**功能描述**: 获取自己未处理的好友申请数量

## 群组相关接口 (Group)

### 1. 创建群组
**接口地址**: `POST /group/create_group`

**功能描述**: 创建新的群组

### 2. 设置群组信息
**接口地址**: `POST /group/set_group_info`

**功能描述**: 设置群组基本信息

### 3. 设置群组信息扩展版
**接口地址**: `POST /group/set_group_info_ex`

**功能描述**: 设置群组信息的扩展版本

### 4. 加入群组
**接口地址**: `POST /group/join_group`

**功能描述**: 申请加入群组

### 5. 退出群组
**接口地址**: `POST /group/quit_group`

**功能描述**: 主动退出群组

### 6. 群组申请响应
**接口地址**: `POST /group/group_application_response`

**功能描述**: 处理群组加入申请（同意或拒绝）

### 7. 转让群组
**接口地址**: `POST /group/transfer_group`

**功能描述**: 转让群主权限

### 8. 获取收到的群组申请列表
**接口地址**: `POST /group/get_recv_group_applicationList`

**功能描述**: 获取收到的群组加入申请列表

### 9. 获取用户发送的群组申请列表
**接口地址**: `POST /group/get_user_req_group_applicationList`

**功能描述**: 获取用户发送的群组申请列表

### 10. 获取群组用户申请列表
**接口地址**: `POST /group/get_group_users_req_application_list`

**功能描述**: 获取群组的用户申请列表

### 11. 获取指定用户群组申请信息
**接口地址**: `POST /group/get_specified_user_group_request_info`

**功能描述**: 获取指定用户的群组申请信息

### 12. 获取群组信息
**接口地址**: `POST /group/get_groups_info`

**功能描述**: 获取指定群组的详细信息

### 13. 踢出群成员
**接口地址**: `POST /group/kick_group`

**功能描述**: 将成员踢出群组

### 14. 获取群成员信息
**接口地址**: `POST /group/get_group_members_info`

**功能描述**: 获取指定群成员的信息

### 15. 获取群成员列表
**接口地址**: `POST /group/get_group_member_list`

**功能描述**: 分页获取群成员列表

### 16. 邀请用户入群
**接口地址**: `POST /group/invite_user_to_group`

**功能描述**: 邀请用户加入群组

### 17. 获取已加入群组列表
**接口地址**: `POST /group/get_joined_group_list`

**功能描述**: 获取用户已加入的群组列表

### 18. 解散群组
**接口地址**: `POST /group/dismiss_group`

**功能描述**: 解散群组

### 19. 禁言群成员
**接口地址**: `POST /group/mute_group_member`

**功能描述**: 禁言指定的群成员

### 20. 取消禁言群成员
**接口地址**: `POST /group/cancel_mute_group_member`

**功能描述**: 取消群成员的禁言状态

### 21. 禁言群组
**接口地址**: `POST /group/mute_group`

**功能描述**: 禁言整个群组

### 22. 取消禁言群组
**接口地址**: `POST /group/cancel_mute_group`

**功能描述**: 取消群组的禁言状态

### 23. 设置群成员信息
**接口地址**: `POST /group/set_group_member_info`

**功能描述**: 设置群成员的信息

### 24. 获取群组摘要信息
**接口地址**: `POST /group/get_group_abstract_info`

**功能描述**: 获取群组的摘要信息

### 25. 获取群组列表
**接口地址**: `POST /group/get_groups`

**功能描述**: 获取群组列表

### 26. 获取群成员用户ID
**接口地址**: `POST /group/get_group_member_user_id`

**功能描述**: 获取群组中所有成员的用户ID

### 27. 获取增量加入群组
**接口地址**: `POST /group/get_incremental_join_groups`

**功能描述**: 获取增量的加入群组信息

### 28. 获取增量群成员
**接口地址**: `POST /group/get_incremental_group_members`

**功能描述**: 获取增量的群成员变化

### 29. 批量获取增量群成员
**接口地址**: `POST /group/get_incremental_group_members_batch`

**功能描述**: 批量获取增量的群成员变化

### 30. 获取完整群成员用户ID
**接口地址**: `POST /group/get_full_group_member_user_ids`

**功能描述**: 获取完整的群成员用户ID列表

### 31. 获取完整加入群组ID
**接口地址**: `POST /group/get_full_join_group_ids`

**功能描述**: 获取完整的加入群组ID列表

### 32. 获取群组申请未处理数量
**接口地址**: `POST /group/get_group_application_unhandled_count`

**功能描述**: 获取群组申请的未处理数量

### 33. 群组创建统计
**接口地址**: `POST /statistics/group/create`

**功能描述**: 获取群组创建统计信息

## 消息相关接口 (Message)

### 1. 获取最新序列号
**接口地址**: `POST /msg/newest_seq`

**功能描述**: 获取会话的最新消息序列号

### 2. 根据序列号拉取消息
**接口地址**: `POST /msg/pull_msg_by_seq`

**功能描述**: 根据指定的序列号列表拉取消息

### 3. 撤回消息
**接口地址**: `POST /msg/revoke_msg`

**功能描述**: 撤回已发送的消息

### 4. 标记消息为已读
**接口地址**: `POST /msg/mark_msgs_as_read`

**功能描述**: 标记指定消息为已读状态

### 5. 标记会话为已读
**接口地址**: `POST /msg/mark_conversation_as_read`

**功能描述**: 标记整个会话为已读状态

### 6. 获取会话已读和最大序列号
**接口地址**: `POST /msg/get_conversations_has_read_and_max_seq`

**功能描述**: 获取会话的已读序列号和最大序列号

### 7. 设置会话已读序列号
**接口地址**: `POST /msg/set_conversation_has_read_seq`

**功能描述**: 设置会话的已读序列号

### 8. 清空会话消息
**接口地址**: `POST /msg/clear_conversation_msg`

**功能描述**: 清空指定会话的所有消息

### 9. 用户清空所有消息
**接口地址**: `POST /msg/user_clear_all_msg`

**功能描述**: 清空用户的所有消息

### 10. 删除消息
**接口地址**: `POST /msg/delete_msgs`

**功能描述**: 删除指定的消息

### 11. 物理删除消息（按序列号）
**接口地址**: `POST /msg/delete_msg_phsical_by_seq`

**功能描述**: 根据序列号物理删除消息

### 12. 物理删除消息
**接口地址**: `POST /msg/delete_msg_physical`

**功能描述**: 物理删除指定的消息

### 13. 发送消息
**接口地址**: `POST /msg/send_msg`

**功能描述**: 发送消息（仅限管理员调用）

**请求参数**:
```json
{
  "recvID": "user_002",
  "sendMsg": {
    "sendID": "user_001",
    "groupID": "",
    "senderNickname": "张三",
    "senderFaceURL": "https://example.com/avatar/001.jpg",
    "senderPlatformID": 1,
    "content": {
      "content": "这是一条文本消息"
    },
    "contentType": 101,
    "sessionType": 1,
    "isOnlineOnly": false,
    "notOfflinePush": false,
    "sendTime": 1640995200000,
    "offlinePushInfo": {
      "title": "新消息",
      "desc": "您收到一条新消息",
      "ex": "{\"badge\":1}",
      "iOSPushSound": "default",
      "iOSBadgeCount": true
    },
    "ex": ""
  }
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| recvID | string | 是 | 接收者用户ID（单聊时必填） |
| sendMsg | object | 是 | 消息发送对象 |
| sendMsg.sendID | string | 是 | 发送者用户ID |
| sendMsg.groupID | string | 否 | 群组ID（群聊时必填） |
| sendMsg.senderNickname | string | 否 | 发送者昵称 |
| sendMsg.senderFaceURL | string | 否 | 发送者头像URL |
| sendMsg.senderPlatformID | int32 | 是 | 发送者平台ID |
| sendMsg.content | object | 是 | 消息内容，根据contentType不同而不同 |
| sendMsg.contentType | int32 | 是 | 消息类型，见内容类型常量表 |
| sendMsg.sessionType | int32 | 是 | 会话类型：1-单聊，2-群聊，4-通知 |
| sendMsg.isOnlineOnly | bool | 否 | 是否仅在线发送，默认false |
| sendMsg.notOfflinePush | bool | 否 | 是否禁用离线推送，默认false |
| sendMsg.sendTime | int64 | 否 | 发送时间戳（毫秒），0表示使用服务器时间 |
| sendMsg.offlinePushInfo | object | 否 | 离线推送信息 |
| sendMsg.offlinePushInfo.title | string | 否 | 推送标题 |
| sendMsg.offlinePushInfo.desc | string | 否 | 推送内容 |
| sendMsg.offlinePushInfo.ex | string | 否 | 推送扩展字段 |
| sendMsg.offlinePushInfo.iOSPushSound | string | 否 | iOS推送声音 |
| sendMsg.offlinePushInfo.iOSBadgeCount | bool | 否 | 是否显示角标 |
| sendMsg.ex | string | 否 | 消息扩展字段 |

**消息内容类型详细说明**:

#### 文本消息 (contentType: 101)
```json
{
  "content": {
    "content": "这是一条文本消息"
  }
}
```

#### 图片消息 (contentType: 102)
```json
{
  "content": {
    "sourcePicture": {
      "uuid": "pic_uuid_001",
      "type": "jpg",
      "size": 204800,
      "width": 1920,
      "height": 1080,
      "url": "https://example.com/images/original.jpg"
    },
    "bigPicture": {
      "uuid": "pic_uuid_002", 
      "type": "jpg",
      "size": 102400,
      "width": 1920,
      "height": 1080,
      "url": "https://example.com/images/big.jpg"
    },
    "snapshotPicture": {
      "uuid": "pic_uuid_003",
      "type": "jpg",
      "size": 10240,
      "width": 200,
      "height": 150,
      "url": "https://example.com/images/thumbnail.jpg"
    }
  }
}
```

**图片消息字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| sourcePicture | object | 是 | 原图信息 |
| sourcePicture.uuid | string | 否 | 图片唯一标识 |
| sourcePicture.type | string | 是 | 图片格式：jpg、png、gif等 |
| sourcePicture.size | int64 | 否 | 图片大小（字节） |
| sourcePicture.width | int32 | 是 | 图片宽度（像素） |
| sourcePicture.height | int32 | 是 | 图片高度（像素） |
| sourcePicture.url | string | 是 | 图片访问URL |
| bigPicture | object | 是 | 大图信息，字段同sourcePicture |
| snapshotPicture | object | 是 | 缩略图信息，字段同sourcePicture |

#### 语音消息 (contentType: 103)
```json
{
  "content": {
    "uuid": "audio_uuid_001",
    "soundPath": "/local/path/audio.mp3",
    "sourceUrl": "https://example.com/audio/voice.mp3",
    "dataSize": 51200,
    "duration": 10
  }
}
```

**语音消息字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| uuid | string | 否 | 语音唯一标识 |
| soundPath | string | 否 | 本地文件路径 |
| sourceUrl | string | 是 | 语音文件URL |
| dataSize | int64 | 否 | 文件大小（字节） |
| duration | int64 | 是 | 语音时长（秒），最小1秒 |

#### 视频消息 (contentType: 104)
```json
{
  "content": {
    "videoPath": "/local/path/video.mp4",
    "videoUUID": "video_uuid_001",
    "videoUrl": "https://example.com/video/movie.mp4",
    "videoType": "mp4",
    "videoSize": 1048576,
    "duration": 30,
    "snapshotPath": "/local/path/snapshot.jpg",
    "snapshotUUID": "snap_uuid_001",
    "snapshotSize": 10240,
    "snapshotUrl": "https://example.com/video/snapshot.jpg",
    "snapshotWidth": 640,
    "snapshotHeight": 480
  }
}
```

**视频消息字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| videoPath | string | 否 | 本地视频路径 |
| videoUUID | string | 否 | 视频唯一标识 |
| videoUrl | string | 是 | 视频文件URL |
| videoType | string | 是 | 视频格式：mp4、avi等 |
| videoSize | int64 | 是 | 视频文件大小（字节） |
| duration | int64 | 是 | 视频时长（秒） |
| snapshotPath | string | 否 | 本地缩略图路径 |
| snapshotUUID | string | 否 | 缩略图唯一标识 |
| snapshotSize | int64 | 否 | 缩略图大小（字节） |
| snapshotUrl | string | 是 | 缩略图URL |
| snapshotWidth | int32 | 是 | 缩略图宽度（像素） |
| snapshotHeight | int32 | 是 | 缩略图高度（像素） |

#### 文件消息 (contentType: 105)
```json
{
  "content": {
    "filePath": "/local/path/document.pdf",
    "uuid": "file_uuid_001",
    "sourceUrl": "https://example.com/files/document.pdf",
    "fileName": "重要文档.pdf",
    "fileSize": 1048576
  }
}
```

**文件消息字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| filePath | string | 否 | 本地文件路径 |
| uuid | string | 否 | 文件唯一标识 |
| sourceUrl | string | 是 | 文件下载URL |
| fileName | string | 是 | 文件名称 |
| fileSize | int64 | 是 | 文件大小（字节） |

#### @消息 (contentType: 106)
```json
{
  "content": {
    "text": "@all 重要通知：今天下午2点开会",
    "atUserList": ["user_002", "user_003"],
    "isAtSelf": false
  }
}
```

**@消息字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| text | string | 否 | @消息文本内容 |
| atUserList | array | 是 | @的用户ID列表，最大1000个，"all"表示@所有人 |
| isAtSelf | bool | 否 | 是否@自己 |

#### 自定义消息 (contentType: 200)
```json
{
  "content": {
    "data": "{\"type\":\"location\",\"lat\":39.9042,\"lng\":116.4074}",
    "description": "位置信息",
    "extension": "{\"address\":\"北京市朝阳区\"}"
  }
}
```

**自定义消息字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| data | string | 是 | 自定义数据，通常为JSON字符串 |
| description | string | 否 | 消息描述 |
| extension | string | 否 | 扩展信息 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "serverMsgID": "server_msg_001",
    "clientMsgID": "client_msg_001",
    "sendTime": 1640995200000
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| serverMsgID | string | 服务器生成的消息ID |
| clientMsgID | string | 客户端消息ID |
| sendTime | int64 | 实际发送时间（毫秒时间戳） |

### 14. 发送业务通知
**接口地址**: `POST /msg/send_business_notification`

**功能描述**: 发送业务通知消息（仅限管理员调用）

**请求参数**:
```json
{
  "key": "order_status_change",
  "data": "{\"orderId\":\"12345\",\"status\":\"shipped\"}",
  "sendUserID": "system_001",
  "recvUserID": "user_002"
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| key | string | 是 | 通知类型键值，用于业务方识别通知类型 |
| data | string | 是 | 通知数据，JSON字符串格式 |
| sendUserID | string | 是 | 发送者用户ID |
| recvUserID | string | 是 | 接收者用户ID |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "serverMsgID": "server_msg_002",
    "clientMsgID": "client_msg_002",
    "sendTime": 1640995300000
  }
}
```

### 15. 批量发送消息
**接口地址**: `POST /msg/batch_send_msg`

**功能描述**: 批量发送消息给多个用户（仅限管理员调用）

**请求参数**:
```json
{
  "sendMsg": {
    "sendID": "system_001",
    "senderNickname": "系统通知",
    "senderFaceURL": "https://example.com/system_avatar.jpg",
    "senderPlatformID": 1,
    "content": {
      "content": "系统维护通知：今晚23:00-24:00进行系统维护"
    },
    "contentType": 101,
    "sessionType": 1,
    "isOnlineOnly": false,
    "notOfflinePush": false,
    "sendTime": 0,
    "offlinePushInfo": {
      "title": "系统通知",
      "desc": "系统维护通知"
    }
  },
  "isSendAll": false,
  "recvIDs": ["user_001", "user_002", "user_003"]
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| sendMsg | object | 是 | 消息对象，字段说明同发送消息接口 |
| isSendAll | bool | 否 | 是否发送给所有用户，true时忽略recvIDs |
| recvIDs | array | 是 | 接收者用户ID列表，isSendAll为false时必填 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success", 
  "data": {
    "results": [
      {
        "serverMsgID": "server_msg_003",
        "clientMsgID": "client_msg_003",
        "sendTime": 1640995400000,
        "recvID": "user_001"
      },
      {
        "serverMsgID": "server_msg_004",
        "clientMsgID": "client_msg_004",
        "sendTime": 1640995400000,
        "recvID": "user_002"
      }
    ],
    "failedUserIDs": ["user_003"]
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| results | array | 成功发送的消息结果列表 |
| results[].serverMsgID | string | 服务器消息ID |
| results[].clientMsgID | string | 客户端消息ID |
| results[].sendTime | int64 | 发送时间戳 |
| results[].recvID | string | 接收者用户ID |
| failedUserIDs | array | 发送失败的用户ID列表 |

### 16. 检查消息发送状态
**接口地址**: `POST /msg/check_msg_is_send_success`

**功能描述**: 检查消息是否发送成功

### 17. 搜索消息
**接口地址**: `POST /msg/search_msg`

**功能描述**: 搜索消息内容

### 18. 获取服务器时间
**接口地址**: `POST /msg/get_server_time`

**功能描述**: 获取服务器当前时间

### 19. 获取活跃用户
**接口地址**: `POST /statistics/user/active`

**功能描述**: 获取活跃用户统计信息

### 20. 获取活跃群组
**接口地址**: `POST /statistics/group/active`

**功能描述**: 获取活跃群组统计信息

## 会话相关接口 (Conversation)

### 1. 获取排序的会话列表
**接口地址**: `POST /conversation/get_sorted_conversation_list`

**功能描述**: 获取按时间排序的会话列表

### 2. 获取所有会话
**接口地址**: `POST /conversation/get_all_conversations`

**功能描述**: 获取用户的所有会话

### 3. 获取会话
**接口地址**: `POST /conversation/get_conversation`

**功能描述**: 获取指定会话信息

### 4. 获取多个会话
**接口地址**: `POST /conversation/get_conversations`

**功能描述**: 批量获取会话信息

### 5. 设置会话
**接口地址**: `POST /conversation/set_conversations`

**功能描述**: 设置会话属性

### 6. 获取会话离线推送用户ID
**接口地址**: `POST /conversation/get_conversation_offline_push_user_ids`

**功能描述**: 获取会话中需要离线推送的用户ID

### 7. 获取完整会话ID
**接口地址**: `POST /conversation/get_full_conversation_ids`

**功能描述**: 获取用户的完整会话ID列表

### 8. 获取增量会话
**接口地址**: `POST /conversation/get_incremental_conversations`

**功能描述**: 获取增量的会话变化

### 9. 获取拥有者会话
**接口地址**: `POST /conversation/get_owner_conversation`

**功能描述**: 获取会话拥有者信息

### 10. 获取非通知会话ID
**接口地址**: `POST /conversation/get_not_notify_conversation_ids`

**功能描述**: 获取不需要通知的会话ID列表

### 11. 获取置顶会话ID
**接口地址**: `POST /conversation/get_pinned_conversation_ids`

**功能描述**: 获取置顶的会话ID列表

## 第三方服务接口 (Third)

### 1. FCM更新Token
**接口地址**: `POST /third/fcm_update_token`

**功能描述**: 更新Firebase Cloud Messaging的推送Token

### 2. 设置应用角标
**接口地址**: `POST /third/set_app_badge`

**功能描述**: 设置应用图标上的角标数字

### 3. 获取Prometheus监控
**接口地址**: `GET /third/prometheus`

**功能描述**: 重定向到Grafana监控页面

### 对象存储相关接口

### 4. 分片限制
**接口地址**: `POST /object/part_limit`

**功能描述**: 获取分片上传的限制信息

### 5. 分片大小
**接口地址**: `POST /object/part_size`

**功能描述**: 获取分片大小配置

### 6. 初始化分片上传
**接口地址**: `POST /object/initiate_multipart_upload`

**功能描述**: 初始化分片上传任务

### 7. 授权签名
**接口地址**: `POST /object/auth_sign`

**功能描述**: 获取上传授权签名

### 8. 完成分片上传
**接口地址**: `POST /object/complete_multipart_upload`

**功能描述**: 完成分片上传任务

### 9. 获取访问URL
**接口地址**: `POST /object/access_url`

**功能描述**: 获取文件访问URL

### 10. 初始化表单数据
**接口地址**: `POST /object/initiate_form_data`

**功能描述**: 初始化表单上传数据

### 11. 完成表单数据
**接口地址**: `POST /object/complete_form_data`

**功能描述**: 完成表单数据上传

### 12. 对象重定向
**接口地址**: `GET /object/*name`

**功能描述**: 对象访问重定向

### 日志相关接口

### 13. 上传日志
**接口地址**: `POST /third/logs/upload`

**功能描述**: 上传系统日志

### 14. 删除日志
**接口地址**: `POST /third/logs/delete`

**功能描述**: 删除指定日志

### 15. 搜索日志
**接口地址**: `POST /third/logs/search`

**功能描述**: 搜索日志内容

## JS SDK 接口 (JSSDK)

### 1. 获取会话列表
**接口地址**: `POST /jssdk/get_conversations`

**功能描述**: 获取指定的会话列表信息

**请求参数**:
```json
{
  "conversationIDs": ["conv1", "conv2", "conv3"]
}
```

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "conversations": [
      {
        "conversation": {
          "conversationID": "会话ID",
          "conversationType": 1,
          "userID": "用户ID",
          "groupID": "群组ID",
          "showName": "显示名称",
          "faceURL": "头像URL",
          "isPrivateChat": false,
          "ex": "扩展字段"
        },
        "lastMsg": {
          "serverMsgID": "服务器消息ID",
          "clientMsgID": "客户端消息ID",
          "sendID": "发送者ID",
          "recvID": "接收者ID",
          "senderNickname": "发送者昵称",
          "senderFaceURL": "发送者头像",
          "sessionType": 1,
          "msgFrom": 100,
          "contentType": 101,
          "content": "消息内容",
          "seq": 123,
          "sendTime": 1640995200000,
          "createTime": 1640995200000
        },
        "maxSeq": 123,
        "readSeq": 120,
        "user": {
          "userID": "用户ID",
          "nickname": "用户昵称",
          "faceURL": "用户头像"
        },
        "friend": {
          "friendUserID": "好友用户ID",
          "remark": "好友备注"
        },
        "group": {
          "groupID": "群组ID",
          "groupName": "群组名称",
          "faceURL": "群组头像"
        }
      }
    ],
    "unreadCount": 10
  }
}
```

### 2. 获取活跃会话列表
**接口地址**: `POST /jssdk/get_active_conversations`

**功能描述**: 获取用户的活跃会话列表，按活跃度和置顶状态排序

**请求参数**:
```json
{
  "count": 20 // 返回数量，默认100，最大500
}
```

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "conversations": [
      {
        "conversation": {
          "conversationID": "会话ID",
          "conversationType": 1,
          "userID": "用户ID", 
          "groupID": "群组ID",
          "showName": "显示名称",
          "faceURL": "头像URL"
        },
        "lastMsg": {
          // 最后一条消息信息
        },
        "maxSeq": 123, // 最大序列号
        "readSeq": 120, // 已读序列号
        "user": {
          // 用户信息（单聊时）
        },
        "friend": {
          // 好友信息（单聊时）
        },
        "group": {
          // 群组信息（群聊时）
        }
      }
    ],
    "unreadCount": 15 // 总未读数
  }
}
```

**功能特点**:
- 自动按置顶状态和最后活跃时间排序
- 包含会话的完整信息（用户信息、好友信息、群组信息）
- 返回总的未读消息数量
- 支持自定义返回数量限制

## 监控发现接口 (Prometheus Discovery)

这些接口用于Prometheus服务发现，返回各服务的监控目标信息。仅在使用ETCD服务发现时可用。

### 1. API服务发现
**接口地址**: `GET /prometheus_discovery/api`

**功能描述**: 获取API服务的Prometheus监控目标

### 2. 用户服务发现
**接口地址**: `GET /prometheus_discovery/user`

**功能描述**: 获取用户服务的Prometheus监控目标

### 3. 群组服务发现
**接口地址**: `GET /prometheus_discovery/group`

**功能描述**: 获取群组服务的Prometheus监控目标

### 4. 消息服务发现
**接口地址**: `GET /prometheus_discovery/msg`

**功能描述**: 获取消息服务的Prometheus监控目标

### 5. 好友服务发现
**接口地址**: `GET /prometheus_discovery/friend`

**功能描述**: 获取好友服务的Prometheus监控目标

### 6. 会话服务发现
**接口地址**: `GET /prometheus_discovery/conversation`

**功能描述**: 获取会话服务的Prometheus监控目标

### 7. 第三方服务发现
**接口地址**: `GET /prometheus_discovery/third`

**功能描述**: 获取第三方服务的Prometheus监控目标

### 8. 认证服务发现
**接口地址**: `GET /prometheus_discovery/auth`

**功能描述**: 获取认证服务的Prometheus监控目标

### 9. 推送服务发现
**接口地址**: `GET /prometheus_discovery/push`

**功能描述**: 获取推送服务的Prometheus监控目标

### 10. 消息网关发现
**接口地址**: `GET /prometheus_discovery/msg_gateway`

**功能描述**: 获取消息网关的Prometheus监控目标

### 11. 消息传输发现
**接口地址**: `GET /prometheus_discovery/msg_transfer`

**功能描述**: 获取消息传输服务的Prometheus监控目标

**返回参数格式**:
```json
[
  {
    "targets": ["ip:port"],
    "labels": {
      "job": "服务名称",
      "instance": "实例标识"
    }
  }
]
```

## 通用错误码说明

| 错误码 | 错误信息 | 说明 |
|--------|----------|------|
| 0 | success | 成功 |
| 1001 | 参数错误 | 请求参数不符合要求 |
| 1002 | 权限不足 | 没有执行该操作的权限 |
| 1003 | 用户不存在 | 指定的用户不存在 |
| 1004 | 群组不存在 | 指定的群组不存在 |
| 1005 | Token无效 | Token已过期或无效 |

## 认证说明

除了以下白名单接口外，所有接口都需要在请求头中携带有效的Token：

**白名单接口**:
- `POST /auth/get_admin_token`
- `POST /auth/parse_token`

**Token使用方式**:
在HTTP请求头中添加：
```
Token: your_access_token_here
```

## 内容类型常量

### 消息类型 (ContentType)
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

### 会话类型 (SessionType)
| 值 | 类型 | 说明 |
|----|------|------|
| 1 | SingleChatType | 单聊 |
| 2 | WriteGroupChatType | 群聊 |
| 3 | ReadGroupChatType | 只读群聊 |
| 4 | NotificationChatType | 通知聊天 |

### 平台类型 (PlatformID)
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

## 注意事项

1. 所有时间戳均为毫秒级时间戳
2. 文件上传需要先通过第三方服务接口获取上传凭证
3. 群聊消息需要指定groupID，单聊消息需要指定recvID
4. 消息内容content字段根据contentType的不同有不同的结构要求
5. 管理员权限接口需要配置的管理员用户ID才能调用
6. 批量操作接口通常有数量限制，具体限制请参考各接口说明
