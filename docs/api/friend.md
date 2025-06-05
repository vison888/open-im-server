# 好友相关接口 (Friend)

## 概述

好友接口提供完整的好友关系管理功能，包括好友申请、添加、删除、黑名单管理等。支持双向好友关系和好友备注功能。

## 接口列表

### 1. 申请添加好友
**接口地址**: `POST /friend/add_friend`

**功能描述**: 发送好友添加申请

**请求参数**:
```json
{
  "fromUserID": "user_001", 
  "toUserID": "user_002",
  "reqMsg": "我是张三，希望添加您为好友",
  "ex": ""
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| fromUserID | string | 是 | 申请人用户ID |
| toUserID | string | 是 | 被申请人用户ID |
| reqMsg | string | 否 | 申请消息，最大255字符 |
| ex | string | 否 | 扩展字段 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

**错误码**:
- `1003`: 用户不存在 - 申请人或被申请人不存在
- `1201`: 重复申请 - 已存在未处理的好友申请
- `1202`: 已是好友关系 - 用户已经是好友
- `1203`: 在黑名单中 - 被申请人已将申请人加入黑名单

---

### 2. 响应好友申请
**接口地址**: `POST /friend/add_friend_response`

**功能描述**: 同意或拒绝好友申请

**请求参数**:
```json
{
  "fromUserID": "user_001",
  "toUserID": "user_002", 
  "handleResult": 1,
  "handleMsg": "很高兴认识您"
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| fromUserID | string | 是 | 申请人用户ID |
| toUserID | string | 是 | 处理人用户ID（当前用户） |
| handleResult | int32 | 是 | 处理结果：1-同意，-1-拒绝 |
| handleMsg | string | 否 | 处理消息 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

**错误码**:
- `1204`: 申请不存在 - 没有找到对应的好友申请
- `1205`: 申请已处理 - 该申请已经被处理过

---

### 3. 删除好友
**接口地址**: `POST /friend/delete_friend`

**功能描述**: 删除好友关系

**请求参数**:
```json
{
  "ownerUserID": "user_001",
  "friendUserID": "user_002"
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| ownerUserID | string | 是 | 操作用户ID |
| friendUserID | string | 是 | 要删除的好友用户ID |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

---

### 4. 获取好友申请列表
**接口地址**: `POST /friend/get_friend_apply_list`

**功能描述**: 获取收到的好友申请列表（他人发给我的）

**请求参数**:
```json
{
  "userID": "user_002",
  "pagination": {
    "pageNumber": 1,
    "showNumber": 20
  }
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| userID | string | 是 | 用户ID |
| pagination | object | 是 | 分页参数 |
| pagination.pageNumber | int32 | 是 | 页码，从1开始 |
| pagination.showNumber | int32 | 是 | 每页显示数量，最大100 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "total": 5,
    "friendRequests": [
      {
        "fromUserID": "user_001",
        "fromNickname": "张三",
        "fromFaceURL": "https://example.com/avatar/001.jpg",
        "toUserID": "user_002",
        "toNickname": "李四", 
        "toFaceURL": "https://example.com/avatar/002.jpg",
        "handleResult": 0,
        "reqMsg": "我是张三，希望添加您为好友",
        "createTime": 1640995200000,
        "handlerUserID": "",
        "handleMsg": "",
        "handleTime": 0,
        "ex": ""
      }
    ]
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| total | int32 | 好友申请总数 |
| friendRequests | array | 好友申请列表 |
| friendRequests[].fromUserID | string | 申请人用户ID |
| friendRequests[].fromNickname | string | 申请人昵称 |
| friendRequests[].fromFaceURL | string | 申请人头像 |
| friendRequests[].toUserID | string | 被申请人用户ID |
| friendRequests[].toNickname | string | 被申请人昵称 |
| friendRequests[].toFaceURL | string | 被申请人头像 |
| friendRequests[].handleResult | int32 | 处理结果：0-未处理，1-同意，-1-拒绝 |
| friendRequests[].reqMsg | string | 申请消息 |
| friendRequests[].createTime | int64 | 申请时间（毫秒时间戳） |
| friendRequests[].handlerUserID | string | 处理人用户ID |
| friendRequests[].handleMsg | string | 处理消息 |
| friendRequests[].handleTime | int64 | 处理时间（毫秒时间戳） |
| friendRequests[].ex | string | 扩展字段 |

---

### 5. 获取自己的申请列表
**接口地址**: `POST /friend/get_self_friend_apply_list`

**功能描述**: 获取自己发送的好友申请列表

**请求参数**:
```json
{
  "userID": "user_001",
  "pagination": {
    "pageNumber": 1,
    "showNumber": 20
  }
}
```

**请求字段说明**: 同获取好友申请列表

**返回参数**: 同获取好友申请列表，但friendRequests中的数据是用户主动发送的申请

---

### 6. 获取好友列表
**接口地址**: `POST /friend/get_friend_list`

**功能描述**: 分页获取好友列表

**请求参数**:
```json
{
  "ownerUserID": "user_001",
  "pagination": {
    "pageNumber": 1,
    "showNumber": 50
  }
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| ownerUserID | string | 是 | 用户ID |
| pagination | object | 是 | 分页参数 |
| pagination.pageNumber | int32 | 是 | 页码，从1开始 |
| pagination.showNumber | int32 | 是 | 每页显示数量，最大100 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "total": 25,
    "friendsInfo": [
      {
        "ownerUserID": "user_001",
        "remark": "小李",
        "createTime": 1640995200000,
        "addSource": 1,
        "operatorUserID": "user_001",
        "ex": "",
        "friendUser": {
          "userID": "user_002",
          "nickname": "李四",
          "faceURL": "https://example.com/avatar/002.jpg",
          "ex": ""
        }
      }
    ]
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| total | int32 | 好友总数 |
| friendsInfo | array | 好友信息列表 |
| friendsInfo[].ownerUserID | string | 拥有者用户ID |
| friendsInfo[].remark | string | 好友备注 |
| friendsInfo[].createTime | int64 | 成为好友的时间（毫秒时间戳） |
| friendsInfo[].addSource | int32 | 添加来源：1-搜索添加，2-群聊添加，3-名片分享 |
| friendsInfo[].operatorUserID | string | 操作人用户ID |
| friendsInfo[].ex | string | 扩展字段 |
| friendsInfo[].friendUser | object | 好友用户信息 |
| friendsInfo[].friendUser.userID | string | 好友用户ID |
| friendsInfo[].friendUser.nickname | string | 好友昵称 |
| friendsInfo[].friendUser.faceURL | string | 好友头像 |
| friendsInfo[].friendUser.ex | string | 好友扩展字段 |

---

### 7. 获取指定好友
**接口地址**: `POST /friend/get_designated_friends`

**功能描述**: 获取指定的好友信息

**请求参数**:
```json
{
  "ownerUserID": "user_001",
  "friendUserIDs": ["user_002", "user_003", "user_004"]
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| ownerUserID | string | 是 | 用户ID |
| friendUserIDs | array | 是 | 好友用户ID列表，最大100个 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "friendsInfo": [
      {
        "ownerUserID": "user_001",
        "remark": "小李",
        "createTime": 1640995200000,
        "addSource": 1,
        "operatorUserID": "user_001",
        "ex": "",
        "friendUser": {
          "userID": "user_002",
          "nickname": "李四",
          "faceURL": "https://example.com/avatar/002.jpg",
          "ex": ""
        }
      }
    ]
  }
}
```

**返回字段说明**: 同获取好友列表

---

### 8. 设置好友备注
**接口地址**: `POST /friend/set_friend_remark`

**功能描述**: 设置好友的备注名

**请求参数**:
```json
{
  "ownerUserID": "user_001",
  "friendUserID": "user_002",
  "remark": "我的同学小李"
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| ownerUserID | string | 是 | 用户ID |
| friendUserID | string | 是 | 好友用户ID |
| remark | string | 是 | 好友备注，最大64字符 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

---

### 9. 添加黑名单
**接口地址**: `POST /friend/add_black`

**功能描述**: 将用户添加到黑名单

**请求参数**:
```json
{
  "ownerUserID": "user_001",
  "blackUserID": "user_999",
  "ex": ""
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| ownerUserID | string | 是 | 用户ID |
| blackUserID | string | 是 | 要拉黑的用户ID |
| ex | string | 否 | 扩展字段 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

---

### 10. 获取黑名单列表
**接口地址**: `POST /friend/get_black_list`

**功能描述**: 分页获取黑名单列表

**请求参数**:
```json
{
  "ownerUserID": "user_001",
  "pagination": {
    "pageNumber": 1,
    "showNumber": 20
  }
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| ownerUserID | string | 是 | 用户ID |
| pagination | object | 是 | 分页参数 |
| pagination.pageNumber | int32 | 是 | 页码，从1开始 |
| pagination.showNumber | int32 | 是 | 每页显示数量，最大100 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "total": 3,
    "blacksInfo": [
      {
        "ownerUserID": "user_001",
        "createTime": 1640995200000,
        "blackUserInfo": {
          "userID": "user_999",
          "nickname": "被拉黑的用户",
          "faceURL": "https://example.com/avatar/999.jpg",
          "ex": ""
        },
        "addSource": 1,
        "operatorUserID": "user_001",
        "ex": ""
      }
    ]
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| total | int32 | 黑名单总数 |
| blacksInfo | array | 黑名单信息列表 |
| blacksInfo[].ownerUserID | string | 拥有者用户ID |
| blacksInfo[].createTime | int64 | 拉黑时间（毫秒时间戳） |
| blacksInfo[].blackUserInfo | object | 被拉黑用户信息 |
| blacksInfo[].blackUserInfo.userID | string | 被拉黑用户ID |
| blacksInfo[].blackUserInfo.nickname | string | 被拉黑用户昵称 |
| blacksInfo[].blackUserInfo.faceURL | string | 被拉黑用户头像 |
| blacksInfo[].blackUserInfo.ex | string | 被拉黑用户扩展字段 |
| blacksInfo[].addSource | int32 | 添加来源 |
| blacksInfo[].operatorUserID | string | 操作人用户ID |
| blacksInfo[].ex | string | 扩展字段 |

---

### 11. 移除黑名单
**接口地址**: `POST /friend/remove_black`

**功能描述**: 从黑名单中移除用户

**请求参数**:
```json
{
  "ownerUserID": "user_001",
  "blackUserID": "user_999"
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| ownerUserID | string | 是 | 用户ID |
| blackUserID | string | 是 | 要移除的黑名单用户ID |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

---

### 12. 检查是否为好友
**接口地址**: `POST /friend/is_friend`

**功能描述**: 检查两个用户是否为好友关系

**请求参数**:
```json
{
  "userID1": "user_001",
  "userID2": "user_002"
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| userID1 | string | 是 | 用户ID1 |
| userID2 | string | 是 | 用户ID2 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "inUser1Friends": true,
    "inUser2Friends": true
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| inUser1Friends | bool | userID2是否在userID1的好友列表中 |
| inUser2Friends | bool | userID1是否在userID2的好友列表中 |

---

### 13. 批量导入好友
**接口地址**: `POST /friend/import_friend`

**功能描述**: 批量导入好友关系（需要管理员权限）

**请求参数**:
```json
{
  "ownerUserID": "user_001",
  "friendUserIDs": ["user_002", "user_003"],
  "isCheck": true
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| ownerUserID | string | 是 | 拥有者用户ID |
| friendUserIDs | array | 是 | 好友用户ID列表，最大1000个 |
| isCheck | bool | 否 | 是否检查用户存在性，默认true |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

## 使用示例

### 添加好友完整流程

```bash
# 1. 发送好友申请
curl -X POST "http://localhost:10002/friend/add_friend" \
  -H "Content-Type: application/json" \
  -H "token: user_token_here" \
  -d '{
    "fromUserID": "user_001",
    "toUserID": "user_002",
    "reqMsg": "你好，我想加你为好友"
  }'

# 2. 查看好友申请（user_002查看）
curl -X POST "http://localhost:10002/friend/get_friend_apply_list" \
  -H "Content-Type: application/json" \
  -H "token: user_002_token_here" \
  -d '{
    "userID": "user_002",
    "pagination": {"pageNumber": 1, "showNumber": 10}
  }'

# 3. 同意好友申请
curl -X POST "http://localhost:10002/friend/add_friend_response" \
  -H "Content-Type: application/json" \
  -H "token: user_002_token_here" \
  -d '{
    "fromUserID": "user_001",
    "toUserID": "user_002",
    "handleResult": 1,
    "handleMsg": "同意添加"
  }'
```

## 注意事项

1. **双向好友关系**: 成为好友后，双方都能在各自的好友列表中看到对方
2. **黑名单优先级**: 被拉黑的用户无法发送好友申请
3. **申请去重**: 系统会自动去重，避免重复申请
4. **权限控制**: 用户只能操作自己的好友关系
5. **数量限制**: 好友申请和黑名单都有数量限制
6. **状态同步**: 好友关系变更会触发相应的通知推送 