# 群组相关接口 (Group)

## 概述

群组接口提供完整的群组管理功能，包括群组创建、成员管理、权限控制、申请处理等。支持多种群组类型和角色权限管理。

## 群组角色权限

| 角色 | 值 | 权限说明 |
|------|----|---------| 
| 普通成员 | 1 | 发送消息、查看群信息 |
| 管理员 | 2 | 踢人、禁言、审批入群申请 |
| 群主 | 3 | 所有权限，包括解散群组、转让群主 |

## 群组类型

| 类型 | 值 | 说明 |
|------|----|----- |
| 普通群 | 0 | 任何人都可以申请加入 |
| 私有群 | 1 | 需要管理员审批才能加入 |
| 工作群 | 2 | 仅通过邀请加入 |

## 接口列表

### 1. 创建群组
**接口地址**: `POST /group/create_group`

**功能描述**: 创建新的群组

**请求参数**:
```json
{
  "memberUserIDs": ["user_001", "user_002", "user_003"],
  "groupInfo": {
    "groupName": "技术讨论群",
    "introduction": "这是一个技术交流群组",
    "notification": "欢迎大家积极发言",
    "faceURL": "https://example.com/group_avatar.jpg",
    "ex": "{\"category\":\"tech\"}",
    "groupType": 0,
    "needVerification": 0
  },
  "adminUserIDs": ["user_002"]
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| memberUserIDs | array | 是 | 初始成员用户ID列表，不包括创建者，最大500个 |
| groupInfo | object | 是 | 群组基本信息 |
| groupInfo.groupName | string | 是 | 群组名称，最大64字符 |
| groupInfo.introduction | string | 否 | 群组介绍，最大255字符 |
| groupInfo.notification | string | 否 | 群组公告，最大255字符 |
| groupInfo.faceURL | string | 否 | 群组头像URL |
| groupInfo.ex | string | 否 | 扩展字段 |
| groupInfo.groupType | int32 | 否 | 群组类型，见群组类型常量，默认0 |
| groupInfo.needVerification | int32 | 否 | 加群验证：0-直接加入，1-需要验证，2-禁止加群 |
| adminUserIDs | array | 否 | 管理员用户ID列表，必须在memberUserIDs中 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "groupInfo": {
      "groupID": "group_001",
      "groupName": "技术讨论群",
      "notification": "欢迎大家积极发言",
      "introduction": "这是一个技术交流群组",
      "faceURL": "https://example.com/group_avatar.jpg",
      "ownerUserID": "user_001",
      "createTime": 1640995200000,
      "memberCount": 4,
      "ex": "{\"category\":\"tech\"}",
      "status": 0,
      "creatorUserID": "user_001",
      "groupType": 0,
      "needVerification": 0
    }
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| groupInfo.groupID | string | 系统生成的群组ID |
| groupInfo.groupName | string | 群组名称 |
| groupInfo.notification | string | 群组公告 |
| groupInfo.introduction | string | 群组介绍 |
| groupInfo.faceURL | string | 群组头像URL |
| groupInfo.ownerUserID | string | 群主用户ID |
| groupInfo.createTime | int64 | 创建时间（毫秒时间戳） |
| groupInfo.memberCount | int32 | 群成员数量 |
| groupInfo.ex | string | 扩展字段 |
| groupInfo.status | int32 | 群组状态：0-正常，1-被封禁，2-已解散 |
| groupInfo.creatorUserID | string | 创建者用户ID |
| groupInfo.groupType | int32 | 群组类型 |
| groupInfo.needVerification | int32 | 加群验证设置 |

---

### 2. 获取群组信息
**接口地址**: `POST /group/get_groups_info`

**功能描述**: 获取指定群组的详细信息

**请求参数**:
```json
{
  "groupIDs": ["group_001", "group_002"]
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| groupIDs | array | 是 | 群组ID列表，最大100个 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "groupInfos": [
      {
        "groupID": "group_001",
        "groupName": "技术讨论群",
        "notification": "欢迎大家积极发言",
        "introduction": "这是一个技术交流群组",
        "faceURL": "https://example.com/group_avatar.jpg",
        "ownerUserID": "user_001",
        "createTime": 1640995200000,
        "memberCount": 15,
        "ex": "{\"category\":\"tech\"}",
        "status": 0,
        "creatorUserID": "user_001",
        "groupType": 0,
        "needVerification": 0
      }
    ]
  }
}
```

---

### 3. 申请加入群组
**接口地址**: `POST /group/join_group`

**功能描述**: 申请加入群组

**请求参数**:
```json
{
  "groupID": "group_001",
  "reqMsg": "我想加入这个群组",
  "joinSource": 1,
  "inviterUserID": ""
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| groupID | string | 是 | 群组ID |
| reqMsg | string | 否 | 申请消息，最大255字符 |
| joinSource | int32 | 否 | 加入来源：1-搜索加入，2-邀请加入，3-二维码 |
| inviterUserID | string | 否 | 邀请人用户ID（如果是邀请加入） |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

**错误码**:
- `1301`: 群组不存在 - 指定的群组ID不存在
- `1302`: 已是群成员 - 用户已经在群组中
- `1303`: 群组已满 - 群组成员数已达上限
- `1304`: 禁止加群 - 群组设置为禁止加入

---

### 4. 邀请用户入群
**接口地址**: `POST /group/invite_user_to_group`

**功能描述**: 邀请用户加入群组（需要管理员权限）

**请求参数**:
```json
{
  "groupID": "group_001",
  "reason": "邀请加入我们的技术讨论群",
  "invitedUserIDs": ["user_004", "user_005"]
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| groupID | string | 是 | 群组ID |
| reason | string | 否 | 邀请原因 |
| invitedUserIDs | array | 是 | 被邀请用户ID列表，最大100个 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

---

### 5. 处理群组申请
**接口地址**: `POST /group/group_application_response`

**功能描述**: 处理群组加入申请（同意或拒绝）

**请求参数**:
```json
{
  "groupID": "group_001",
  "fromUserID": "user_006",
  "handleResult": 1,
  "handleMsg": "欢迎加入"
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| groupID | string | 是 | 群组ID |
| fromUserID | string | 是 | 申请人用户ID |
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

---

### 6. 获取群成员列表
**接口地址**: `POST /group/get_group_member_list`

**功能描述**: 分页获取群成员列表

**请求参数**:
```json
{
  "groupID": "group_001",
  "pagination": {
    "pageNumber": 1,
    "showNumber": 50
  },
  "filter": 0
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| groupID | string | 是 | 群组ID |
| pagination | object | 是 | 分页参数 |
| pagination.pageNumber | int32 | 是 | 页码，从1开始 |
| pagination.showNumber | int32 | 是 | 每页显示数量，最大100 |
| filter | int32 | 否 | 过滤条件：0-所有成员，1-群主，2-管理员，3-普通成员 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "total": 15,
    "members": [
      {
        "groupID": "group_001",
        "userID": "user_001",
        "roleLevel": 3,
        "joinTime": 1640995200000,
        "nickname": "张三",
        "faceURL": "https://example.com/avatar/001.jpg",
        "appMangerLevel": 1,
        "joinSource": 1,
        "operatorUserID": "user_001",
        "ex": "",
        "muteEndTime": 0,
        "inviterUserID": ""
      }
    ]
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| total | int32 | 群成员总数 |
| members | array | 群成员列表 |
| members[].groupID | string | 群组ID |
| members[].userID | string | 用户ID |
| members[].roleLevel | int32 | 角色等级：1-普通成员，2-管理员，3-群主 |
| members[].joinTime | int64 | 加入时间（毫秒时间戳） |
| members[].nickname | string | 用户昵称 |
| members[].faceURL | string | 用户头像 |
| members[].appMangerLevel | int32 | 应用管理等级 |
| members[].joinSource | int32 | 加入来源 |
| members[].operatorUserID | string | 操作人用户ID |
| members[].ex | string | 扩展字段 |
| members[].muteEndTime | int64 | 禁言结束时间（毫秒时间戳），0表示未禁言 |
| members[].inviterUserID | string | 邀请人用户ID |

---

### 7. 踢出群成员
**接口地址**: `POST /group/kick_group`

**功能描述**: 将成员踢出群组（需要管理员权限）

**请求参数**:
```json
{
  "groupID": "group_001",
  "kickedUserIDs": ["user_007", "user_008"],
  "reason": "违反群规"
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| groupID | string | 是 | 群组ID |
| kickedUserIDs | array | 是 | 被踢用户ID列表，最大100个 |
| reason | string | 否 | 踢出原因 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

---

### 8. 退出群组
**接口地址**: `POST /group/quit_group`

**功能描述**: 主动退出群组

**请求参数**:
```json
{
  "groupID": "group_001",
  "userID": "user_003"
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| groupID | string | 是 | 群组ID |
| userID | string | 是 | 退出的用户ID |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

**注意**: 群主不能直接退出，需要先转让群主权限

---

### 9. 转让群主
**接口地址**: `POST /group/transfer_group`

**功能描述**: 转让群主权限

**请求参数**:
```json
{
  "groupID": "group_001",
  "oldOwnerUserID": "user_001",
  "newOwnerUserID": "user_002"
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| groupID | string | 是 | 群组ID |
| oldOwnerUserID | string | 是 | 当前群主用户ID |
| newOwnerUserID | string | 是 | 新群主用户ID，必须是群成员 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

---

### 10. 解散群组
**接口地址**: `POST /group/dismiss_group`

**功能描述**: 解散群组（仅群主可操作）

**请求参数**:
```json
{
  "groupID": "group_001"
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| groupID | string | 是 | 群组ID |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

---

### 11. 禁言群成员
**接口地址**: `POST /group/mute_group_member`

**功能描述**: 禁言指定的群成员

**请求参数**:
```json
{
  "groupID": "group_001",
  "userID": "user_009",
  "mutedSeconds": 3600
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| groupID | string | 是 | 群组ID |
| userID | string | 是 | 被禁言用户ID |
| mutedSeconds | int32 | 是 | 禁言时长（秒），0表示取消禁言 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

---

### 12. 禁言群组
**接口地址**: `POST /group/mute_group`

**功能描述**: 禁言整个群组（全员禁言）

**请求参数**:
```json
{
  "groupID": "group_001"
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| groupID | string | 是 | 群组ID |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

---

### 13. 取消禁言群组
**接口地址**: `POST /group/cancel_mute_group`

**功能描述**: 取消群组的禁言状态

**请求参数**:
```json
{
  "groupID": "group_001"
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| groupID | string | 是 | 群组ID |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

---

### 14. 设置群组信息
**接口地址**: `POST /group/set_group_info`

**功能描述**: 设置群组基本信息

**请求参数**:
```json
{
  "groupInfo": {
    "groupID": "group_001",
    "groupName": "新的群组名称",
    "notification": "更新的群组公告",
    "introduction": "更新的群组介绍",
    "faceURL": "https://example.com/new_group_avatar.jpg",
    "ex": "{\"updated\":true}",
    "needVerification": 1
  }
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| groupInfo | object | 是 | 群组信息，只更新传入的字段 |
| groupInfo.groupID | string | 是 | 群组ID |
| groupInfo.groupName | string | 否 | 新的群组名称 |
| groupInfo.notification | string | 否 | 新的群组公告 |
| groupInfo.introduction | string | 否 | 新的群组介绍 |
| groupInfo.faceURL | string | 否 | 新的群组头像URL |
| groupInfo.ex | string | 否 | 新的扩展字段 |
| groupInfo.needVerification | int32 | 否 | 新的加群验证设置 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

---

### 15. 设置群成员信息
**接口地址**: `POST /group/set_group_member_info`

**功能描述**: 设置群成员的信息（如角色等级）

**请求参数**:
```json
{
  "groupID": "group_001",
  "userID": "user_002",
  "nickname": "新昵称",
  "faceURL": "https://example.com/new_avatar.jpg",
  "roleLevel": 2,
  "ex": ""
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| groupID | string | 是 | 群组ID |
| userID | string | 是 | 用户ID |
| nickname | string | 否 | 群内昵称 |
| faceURL | string | 否 | 群内头像URL |
| roleLevel | int32 | 否 | 角色等级：1-普通成员，2-管理员 |
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

### 16. 获取已加入群组列表
**接口地址**: `POST /group/get_joined_group_list`

**功能描述**: 获取用户已加入的群组列表

**请求参数**:
```json
{
  "fromUserID": "user_001",
  "pagination": {
    "pageNumber": 1,
    "showNumber": 50
  }
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| fromUserID | string | 是 | 用户ID |
| pagination | object | 是 | 分页参数 |
| pagination.pageNumber | int32 | 是 | 页码，从1开始 |
| pagination.showNumber | int32 | 是 | 每页显示数量，最大100 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "total": 8,
    "groups": [
      {
        "groupID": "group_001",
        "groupName": "技术讨论群",
        "notification": "欢迎大家积极发言",
        "introduction": "这是一个技术交流群组",
        "faceURL": "https://example.com/group_avatar.jpg",
        "ownerUserID": "user_001",
        "createTime": 1640995200000,
        "memberCount": 15,
        "ex": "{\"category\":\"tech\"}",
        "status": 0,
        "creatorUserID": "user_001",
        "groupType": 0,
        "needVerification": 0
      }
    ]
  }
}
```

**返回字段说明**: 同获取群组信息接口

---

### 17. 获取群组申请列表
**接口地址**: `POST /group/get_recv_group_applicationList`

**功能描述**: 获取收到的群组加入申请列表（管理员查看）

**请求参数**:
```json
{
  "pagination": {
    "pageNumber": 1,
    "showNumber": 20
  }
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
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
    "groupRequests": [
      {
        "groupID": "group_001",
        "groupName": "技术讨论群",
        "groupFaceURL": "https://example.com/group_avatar.jpg",
        "userID": "user_010",
        "nickname": "新用户",
        "userFaceURL": "https://example.com/avatar/010.jpg",
        "handleResult": 0,
        "reqMsg": "我想加入这个群组",
        "handleMsg": "",
        "reqTime": 1640995200000,
        "handleUserID": "",
        "handleTime": 0,
        "ex": "",
        "joinSource": 1,
        "inviterUserID": ""
      }
    ]
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| total | int32 | 申请总数 |
| groupRequests | array | 群组申请列表 |
| groupRequests[].groupID | string | 群组ID |
| groupRequests[].groupName | string | 群组名称 |
| groupRequests[].groupFaceURL | string | 群组头像 |
| groupRequests[].userID | string | 申请人用户ID |
| groupRequests[].nickname | string | 申请人昵称 |
| groupRequests[].userFaceURL | string | 申请人头像 |
| groupRequests[].handleResult | int32 | 处理结果：0-未处理，1-同意，-1-拒绝 |
| groupRequests[].reqMsg | string | 申请消息 |
| groupRequests[].handleMsg | string | 处理消息 |
| groupRequests[].reqTime | int64 | 申请时间（毫秒时间戳） |
| groupRequests[].handleUserID | string | 处理人用户ID |
| groupRequests[].handleTime | int64 | 处理时间（毫秒时间戳） |
| groupRequests[].ex | string | 扩展字段 |
| groupRequests[].joinSource | int32 | 加入来源 |
| groupRequests[].inviterUserID | string | 邀请人用户ID |

## 使用示例

### 创建群组完整流程

```bash
# 1. 创建群组
curl -X POST "http://localhost:10002/group/create_group" \
  -H "Content-Type: application/json" \
  -H "token: user_token_here" \
  -d '{
    "memberUserIDs": ["user_002", "user_003"],
    "groupInfo": {
      "groupName": "我的群组",
      "introduction": "这是我创建的群组",
      "groupType": 0,
      "needVerification": 0
    },
    "adminUserIDs": ["user_002"]
  }'

# 2. 邀请用户入群
curl -X POST "http://localhost:10002/group/invite_user_to_group" \
  -H "Content-Type: application/json" \
  -H "token: user_token_here" \
  -d '{
    "groupID": "group_001",
    "reason": "邀请您加入我们的群组",
    "invitedUserIDs": ["user_004", "user_005"]
  }'

# 3. 获取群成员列表
curl -X POST "http://localhost:10002/group/get_group_member_list" \
  -H "Content-Type: application/json" \
  -H "token: user_token_here" \
  -d '{
    "groupID": "group_001",
    "pagination": {"pageNumber": 1, "showNumber": 50}
  }'
```

## 注意事项

1. **权限控制**: 不同操作需要不同的角色权限，普通成员只能查看信息，管理员可以踢人禁言，群主拥有所有权限
2. **成员数量限制**: 群组成员有上限，默认500人
3. **角色限制**: 群主不能被踢出或禁言，需要先转让群主权限
4. **申请处理**: 群组申请需要管理员或群主处理
5. **群组状态**: 被封禁或已解散的群组无法进行操作
6. **通知机制**: 群组操作会触发相应的系统通知 