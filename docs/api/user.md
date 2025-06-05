# 用户相关接口 (User)

## 概述

用户接口提供完整的用户管理功能，包括用户注册、信息管理、在线状态查询、通知设置等。支持批量操作和搜索功能。

## 接口列表

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
    },
    {
      "userID": "user_002", 
      "nickname": "李四",
      "faceURL": "https://example.com/avatar/002.jpg",
      "ex": "",
      "appMangerLevel": 1,
      "globalRecvMsgOpt": 0
    }
  ]
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| users | array | 是 | 用户信息数组，最大1000个，不能为空 |
| users[].userID | string | 是 | 用户唯一标识，不能包含冒号:，不能重复，最大64字符 |
| users[].nickname | string | 否 | 用户昵称，最大255字符 |
| users[].faceURL | string | 否 | 用户头像URL |
| users[].ex | string | 否 | 扩展字段，可存储JSON格式的额外信息 |
| users[].appMangerLevel | int32 | 否 | 应用管理等级：1-普通用户，2-通知管理员，默认1 |
| users[].globalRecvMsgOpt | int32 | 否 | 全局消息接收选项：0-正常接收，1-不接收消息，2-仅接收在线消息，默认0 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

**错误码**:
- `1001`: 参数错误 - users数组为空或超过限制
- `1101`: 用户ID重复 - 传入的用户ID已存在

---

### 2. 更新用户信息扩展版
**接口地址**: `POST /user/update_user_info_ex`

**功能描述**: 更新用户基本信息，支持部分字段更新

**请求参数**: 
```json
{
  "userInfo": {
    "userID": "user_001",
    "nickname": "新昵称",
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

**错误码**:
- `1003`: 用户不存在 - 指定的用户ID不存在

---

### 3. 设置全局消息接收选项
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

---

### 4. 获取用户公开信息
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
      },
      {
        "userID": "user_002",
        "nickname": "李四", 
        "faceURL": "https://example.com/avatar/002.jpg",
        "ex": "",
        "createTime": 1640995300000,
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

---

### 5. 获取所有用户ID
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

---

### 6. 账户检查
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
| checkUserIDs | array | 是 | 要检查的用户ID列表，不能有重复值，最大1000个 |

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

---

### 7. 获取用户列表
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

---

### 8. 获取用户在线状态
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
            "connID": "conn_123456",
            "isBackground": false,
            "token": "eyJhbGciOiJIUzI1NiIs..."
          },
          {
            "platformID": 2,
            "status": "online",
            "connID": "conn_789012",
            "isBackground": true,
            "token": "eyJhbGciOiJIUzI1NiIs..."
          }
        ]
      },
      {
        "userID": "user_002",
        "status": "offline",
        "detailPlatformStatus": []
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

---

### 9. 订阅用户状态
**接口地址**: `POST /user/subscribe_users_status`

**功能描述**: 订阅或取消订阅用户状态变化

**请求参数**:
```json
{
  "userID": "user_001",
  "userIDs": ["user_002", "user_003", "user_004"],
  "genre": 1
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| userID | string | 是 | 订阅者用户ID |
| userIDs | array | 是 | 被订阅用户ID列表，最大1000个 |
| genre | int32 | 是 | 操作类型：1-订阅，2-取消订阅 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

---

### 10. 获取用户状态
**接口地址**: `POST /user/get_users_status`

**功能描述**: 获取用户的当前状态

**请求参数**:
```json
{
  "userID": "user_001",
  "userIDs": ["user_002", "user_003"]
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| userID | string | 是 | 查询者用户ID |
| userIDs | array | 是 | 被查询用户ID列表 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "statusList": [
      {
        "userID": "user_002",
        "status": 1,
        "platformIDs": [1, 2]
      },
      {
        "userID": "user_003",
        "status": 0,
        "platformIDs": []
      }
    ]
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| statusList | array | 状态列表 |
| statusList[].userID | string | 用户ID |
| statusList[].status | int32 | 用户状态：1-在线，0-离线 |
| statusList[].platformIDs | array | 在线平台ID列表 |

---

### 11. 获取订阅的用户状态
**接口地址**: `POST /user/get_subscribe_users_status`

**功能描述**: 获取已订阅用户的状态

**请求参数**:
```json
{
  "userID": "user_001",
  "pagination": {
    "pageNumber": 1,
    "showNumber": 100
  }
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| userID | string | 是 | 订阅者用户ID |
| pagination | object | 是 | 分页参数 |
| pagination.pageNumber | int32 | 是 | 页码，从1开始 |
| pagination.showNumber | int32 | 是 | 每页显示数量 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "total": 50,
    "statusList": [
      {
        "userID": "user_002",
        "status": 1,
        "platformIDs": [1, 5]
      }
    ]
  }
}
```

---

### 12. 添加通知账户
**接口地址**: `POST /user/add_notification_account`

**功能描述**: 添加通知账户

**请求参数**:
```json
{
  "userID": "notification_001",
  "nickname": "系统通知",
  "faceURL": "https://example.com/notification_avatar.jpg",
  "operationID": "op_123456"
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| userID | string | 是 | 通知账户用户ID |
| nickname | string | 是 | 通知账户昵称 |
| faceURL | string | 否 | 通知账户头像URL |
| operationID | string | 否 | 操作ID |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

---

### 13. 更新通知账户信息
**接口地址**: `POST /user/update_notification_account`

**功能描述**: 更新通知账户信息

**请求参数**:
```json
{
  "userID": "notification_001",
  "nickname": "新系统通知",
  "faceURL": "https://example.com/new_notification_avatar.jpg",
  "operationID": "op_789012"
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| userID | string | 是 | 通知账户用户ID |
| nickname | string | 否 | 新的昵称 |
| faceURL | string | 否 | 新的头像URL |
| operationID | string | 否 | 操作ID |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

---

### 14. 搜索通知账户
**接口地址**: `POST /user/search_notification_account`

**功能描述**: 搜索通知账户

**请求参数**:
```json
{
  "keyword": "系统",
  "pagination": {
    "pageNumber": 1,
    "showNumber": 20
  }
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| keyword | string | 是 | 搜索关键词 |
| pagination | object | 是 | 分页参数 |
| pagination.pageNumber | int32 | 是 | 页码，从1开始 |
| pagination.showNumber | int32 | 是 | 每页显示数量 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "total": 2,
    "notificationAccounts": [
      {
        "userID": "notification_001",
        "nickname": "系统通知",
        "faceURL": "https://example.com/notification_avatar.jpg"
      }
    ]
  }
}
```

---

### 15. 用户注册统计
**接口地址**: `POST /statistics/user/register`

**功能描述**: 获取用户注册统计信息

**请求参数**:
```json
{
  "start": 1640995200000,
  "end": 1641081600000
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| start | int64 | 是 | 开始时间（毫秒时间戳） |
| end | int64 | 是 | 结束时间（毫秒时间戳） |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "total": 156,
    "before": 120,
    "count": [
      {
        "date": "2022-01-01",
        "count": 25
      },
      {
        "date": "2022-01-02", 
        "count": 31
      }
    ]
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| total | int64 | 时间段内注册总数 |
| before | int64 | 开始时间之前的注册总数 |
| count | array | 每日注册数量统计 |
| count[].date | string | 日期 |
| count[].count | int64 | 当日注册数量 |

## 使用示例

### 用户注册和管理完整流程

```bash
# 1. 批量注册用户
curl -X POST "http://localhost:10002/user/user_register" \
  -H "Content-Type: application/json" \
  -H "token: admin_token_here" \
  -d '{
    "users": [
      {
        "userID": "user_001",
        "nickname": "张三",
        "faceURL": "https://example.com/avatar/001.jpg",
        "appMangerLevel": 1,
        "globalRecvMsgOpt": 0
      },
      {
        "userID": "user_002",
        "nickname": "李四", 
        "faceURL": "https://example.com/avatar/002.jpg",
        "appMangerLevel": 1,
        "globalRecvMsgOpt": 0
      }
    ]
  }'

# 2. 获取用户信息
curl -X POST "http://localhost:10002/user/get_users_info" \
  -H "Content-Type: application/json" \
  -H "token: user_token_here" \
  -d '{
    "userIDs": ["user_001", "user_002"]
  }'

# 3. 更新用户信息
curl -X POST "http://localhost:10002/user/update_user_info_ex" \
  -H "Content-Type: application/json" \
  -H "token: user_token_here" \
  -d '{
    "userInfo": {
      "userID": "user_001",
      "nickname": "新昵称",
      "faceURL": "https://example.com/new_avatar.jpg"
    }
  }'

# 4. 查看用户在线状态
curl -X POST "http://localhost:10002/user/get_users_online_status" \
  -H "Content-Type: application/json" \
  -H "token: user_token_here" \
  -d '{
    "userIDs": ["user_001", "user_002"]
  }'
```

## 注意事项

1. **用户ID唯一性**: 用户ID在系统中必须唯一，一旦创建不能修改
2. **批量操作限制**: 批量接口有数量限制，避免一次处理过多数据
3. **权限控制**: 部分接口需要管理员权限，注意权限验证
4. **状态订阅**: 用户状态订阅需要建立长连接，适用于需要实时状态的场景
5. **搜索功能**: 支持模糊搜索，但要注意性能影响
6. **统计数据**: 统计接口可能有缓存，数据可能有延迟
7. **在线状态**: 用户可能在多个平台同时在线，需要处理多端状态 