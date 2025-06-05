# 会话相关接口 (Conversation)

## 概述

会话接口提供完整的会话管理功能，包括获取会话列表、设置会话免打扰、置顶会话、获取会话详情等。会话是用户与其他用户或群组的聊天记录的抽象概念。

## 会话状态

| 值 | 状态 | 说明 |
|----|------|------|
| 0 | 正常 | 正常会话状态 |
| 1 | 免打扰 | 消息免打扰 |
| 2 | 置顶 | 会话置顶 |

## 接口列表

### 1. 获取会话列表
**接口地址**: `POST /conversation/get_conversations`

**功能描述**: 分页获取用户的会话列表

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
    "total": 15,
    "conversations": [
      {
        "ownerUserID": "user_001",
        "conversationID": "single_user_001_user_002",
        "conversationType": 1,
        "userID": "user_002",
        "groupID": "",
        "showName": "李四",
        "faceURL": "https://example.com/avatar/002.jpg",
        "recvMsgOpt": 0,
        "unreadCount": 5,
        "groupAtType": 0,
        "latestMsg": {
          "serverMsgID": "server_msg_150",
          "clientMsgID": "client_msg_150",
          "sendID": "user_002",
          "recvID": "user_001",
          "senderNickname": "李四",
          "senderFaceURL": "https://example.com/avatar/002.jpg",
          "sessionType": 1,
          "msgFrom": 100,
          "contentType": 101,
          "content": "{\"content\":\"最近怎么样？\"}",
          "seq": 150,
          "sendTime": 1640995200000,
          "createTime": 1640995200000,
          "status": 1,
          "ex": ""
        },
        "latestMsgSendTime": 1640995200000,
        "draftTextTime": 0,
        "draftText": "",
        "isPinned": false,
        "isPrivateChat": false,
        "burnDuration": 0,
        "isNotInGroup": false,
        "updateUnreadCountTime": 1640995200000,
        "attachedInfo": "",
        "ex": ""
      },
      {
        "ownerUserID": "user_001",
        "conversationID": "group_group_001",
        "conversationType": 2,
        "userID": "",
        "groupID": "group_001",
        "showName": "技术讨论群",
        "faceURL": "https://example.com/group_avatar.jpg",
        "recvMsgOpt": 0,
        "unreadCount": 12,
        "groupAtType": 1,
        "latestMsg": {
          "serverMsgID": "server_msg_89",
          "clientMsgID": "client_msg_89",
          "sendID": "user_003",
          "groupID": "group_001",
          "senderNickname": "王五",
          "senderFaceURL": "https://example.com/avatar/003.jpg",
          "sessionType": 2,
          "msgFrom": 100,
          "contentType": 106,
          "content": "{\"text\":\"@user_001 看到这个问题了吗？\",\"atUserList\":[\"user_001\"]}",
          "seq": 89,
          "sendTime": 1640995100000,
          "createTime": 1640995100000,
          "status": 1,
          "ex": ""
        },
        "latestMsgSendTime": 1640995100000,
        "draftTextTime": 0,
        "draftText": "",
        "isPinned": true,
        "isPrivateChat": false,
        "burnDuration": 0,
        "isNotInGroup": false,
        "updateUnreadCountTime": 1640995100000,
        "attachedInfo": "",
        "ex": ""
      }
    ]
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| total | int32 | 会话总数 |
| conversations | array | 会话列表 |
| conversations[].ownerUserID | string | 会话拥有者用户ID |
| conversations[].conversationID | string | 会话ID，格式：单聊为single_发送者ID_接收者ID，群聊为group_群组ID |
| conversations[].conversationType | int32 | 会话类型：1-单聊，2-群聊，4-通知 |
| conversations[].userID | string | 对方用户ID（单聊时有值） |
| conversations[].groupID | string | 群组ID（群聊时有值） |
| conversations[].showName | string | 显示名称（单聊为对方昵称，群聊为群名） |
| conversations[].faceURL | string | 显示头像URL |
| conversations[].recvMsgOpt | int32 | 消息接收选项：0-正常，1-不接收消息，2-仅接收在线消息 |
| conversations[].unreadCount | int32 | 未读消息数量 |
| conversations[].groupAtType | int32 | 群聊@类型：0-无@，1-@我，2-@所有人 |
| conversations[].latestMsg | object | 最新消息对象，字段说明见消息接口 |
| conversations[].latestMsgSendTime | int64 | 最新消息发送时间（毫秒时间戳） |
| conversations[].draftTextTime | int64 | 草稿时间（毫秒时间戳） |
| conversations[].draftText | string | 草稿内容 |
| conversations[].isPinned | bool | 是否置顶 |
| conversations[].isPrivateChat | bool | 是否私聊模式 |
| conversations[].burnDuration | int32 | 阅后即焚时长（秒），0表示不开启 |
| conversations[].isNotInGroup | bool | 是否不在群组中（针对群聊） |
| conversations[].updateUnreadCountTime | int64 | 更新未读数时间（毫秒时间戳） |
| conversations[].attachedInfo | string | 附加信息 |
| conversations[].ex | string | 扩展字段 |

---

### 2. 获取指定会话
**接口地址**: `POST /conversation/get_conversation`

**功能描述**: 获取指定的会话详细信息

**请求参数**:
```json
{
  "ownerUserID": "user_001",
  "conversationID": "single_user_001_user_002"
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| ownerUserID | string | 是 | 会话拥有者用户ID |
| conversationID | string | 是 | 会话ID |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "conversation": {
      "ownerUserID": "user_001",
      "conversationID": "single_user_001_user_002",
      "conversationType": 1,
      "userID": "user_002",
      "groupID": "",
      "showName": "李四",
      "faceURL": "https://example.com/avatar/002.jpg",
      "recvMsgOpt": 0,
      "unreadCount": 5,
      "groupAtType": 0,
      "latestMsgSendTime": 1640995200000,
      "draftTextTime": 0,
      "draftText": "",
      "isPinned": false,
      "isPrivateChat": false,
      "burnDuration": 0,
      "isNotInGroup": false,
      "updateUnreadCountTime": 1640995200000,
      "attachedInfo": "",
      "ex": ""
    }
  }
}
```

**返回字段说明**: 同获取会话列表接口中的conversation对象

---

### 3. 获取多个会话
**接口地址**: `POST /conversation/get_conversations`

**功能描述**: 批量获取指定的会话信息

**请求参数**:
```json
{
  "ownerUserID": "user_001",
  "conversationIDs": ["single_user_001_user_002", "group_group_001"]
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| ownerUserID | string | 是 | 会话拥有者用户ID |
| conversationIDs | array | 是 | 会话ID列表，最大100个 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "conversations": [
      {
        "ownerUserID": "user_001",
        "conversationID": "single_user_001_user_002",
        "conversationType": 1,
        "userID": "user_002",
        "showName": "李四",
        "faceURL": "https://example.com/avatar/002.jpg",
        "recvMsgOpt": 0,
        "unreadCount": 5
      }
    ]
  }
}
```

---

### 4. 设置会话
**接口地址**: `POST /conversation/set_conversation`

**功能描述**: 设置会话的各种属性

**请求参数**:
```json
{
  "conversation": {
    "ownerUserID": "user_001",
    "conversationID": "single_user_001_user_002",
    "recvMsgOpt": 1,
    "isPinned": true,
    "isPrivateChat": false,
    "burnDuration": 300,
    "ex": "{\"customField\":\"value\"}"
  }
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| conversation | object | 是 | 会话设置对象 |
| conversation.ownerUserID | string | 是 | 会话拥有者用户ID |
| conversation.conversationID | string | 是 | 会话ID |
| conversation.recvMsgOpt | int32 | 否 | 消息接收选项：0-正常，1-不接收消息，2-仅接收在线消息 |
| conversation.isPinned | bool | 否 | 是否置顶 |
| conversation.isPrivateChat | bool | 否 | 是否私聊模式 |
| conversation.burnDuration | int32 | 否 | 阅后即焚时长（秒），0表示关闭 |
| conversation.ex | string | 否 | 扩展字段 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

---

### 5. 批量设置会话
**接口地址**: `POST /conversation/batch_set_conversation`

**功能描述**: 批量设置多个会话的属性

**请求参数**:
```json
{
  "conversations": [
    {
      "ownerUserID": "user_001",
      "conversationID": "single_user_001_user_002",
      "recvMsgOpt": 1,
      "isPinned": false
    },
    {
      "ownerUserID": "user_001", 
      "conversationID": "group_group_001",
      "recvMsgOpt": 0,
      "isPinned": true
    }
  ]
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| conversations | array | 是 | 会话设置对象列表，最大100个 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "success": ["single_user_001_user_002"],
    "failed": ["group_group_001"]
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| success | array | 设置成功的会话ID列表 |
| failed | array | 设置失败的会话ID列表 |

---

### 6. 设置消息接收选项
**接口地址**: `POST /conversation/set_receive_message_opt`

**功能描述**: 设置会话的消息接收选项

**请求参数**:
```json
{
  "ownerUserID": "user_001",
  "conversationIDs": ["single_user_001_user_002", "group_group_001"],
  "opt": 1
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| ownerUserID | string | 是 | 会话拥有者用户ID |
| conversationIDs | array | 是 | 会话ID列表，最大100个 |
| opt | int32 | 是 | 消息接收选项：0-正常接收，1-不接收消息，2-仅接收在线消息 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

---

### 7. 获取所有会话ID
**接口地址**: `POST /conversation/get_all_conversation_message_opt`

**功能描述**: 获取用户所有会话的消息接收选项

**请求参数**:
```json
{
  "userID": "user_001"
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| userID | string | 是 | 用户ID |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "conversationMsgOpts": [
      {
        "ownerUserID": "user_001",
        "conversationID": "single_user_001_user_002",
        "opt": 0
      },
      {
        "ownerUserID": "user_001",
        "conversationID": "group_group_001", 
        "opt": 1
      }
    ]
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| conversationMsgOpts | array | 会话消息选项列表 |
| conversationMsgOpts[].ownerUserID | string | 会话拥有者用户ID |
| conversationMsgOpts[].conversationID | string | 会话ID |
| conversationMsgOpts[].opt | int32 | 消息接收选项 |

---

### 8. 获取会话接收消息选项
**接口地址**: `POST /conversation/get_receive_message_opt`

**功能描述**: 获取指定会话的消息接收选项

**请求参数**:
```json
{
  "conversationIDs": ["single_user_001_user_002", "group_group_001"]
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| conversationIDs | array | 是 | 会话ID列表，最大100个 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "conversationMsgOpts": [
      {
        "conversationID": "single_user_001_user_002",
        "opt": 0
      },
      {
        "conversationID": "group_group_001",
        "opt": 1
      }
    ]
  }
}
```

---

### 9. 设置会话为私聊
**接口地址**: `POST /conversation/set_conversation_is_private_chat`

**功能描述**: 设置会话为私聊模式

**请求参数**:
```json
{
  "sendUserID": "user_001",
  "recvUserID": "user_002", 
  "isPrivate": true
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| sendUserID | string | 是 | 发起设置的用户ID |
| recvUserID | string | 是 | 对方用户ID |
| isPrivate | bool | 是 | 是否设置为私聊：true-开启私聊，false-关闭私聊 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

---

### 10. 设置会话阅后即焚
**接口地址**: `POST /conversation/set_conversation_burn_duration`

**功能描述**: 设置会话的阅后即焚时长

**请求参数**:
```json
{
  "sendUserID": "user_001",
  "recvUserID": "user_002",
  "burnDuration": 300
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| sendUserID | string | 是 | 发起设置的用户ID |
| recvUserID | string | 是 | 对方用户ID |
| burnDuration | int32 | 是 | 阅后即焚时长（秒），0表示关闭该功能 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

---

### 11. 获取会话未读总数
**接口地址**: `POST /conversation/get_total_unread_msg_count`

**功能描述**: 获取用户所有会话的未读消息总数

**请求参数**:
```json
{
  "userID": "user_001"
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| userID | string | 是 | 用户ID |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "totalUnreadCount": 25
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| totalUnreadCount | int32 | 总未读消息数量 |

## 使用示例

### 会话管理完整流程

```bash
# 1. 获取会话列表
curl -X POST "http://localhost:10002/conversation/get_conversations" \
  -H "Content-Type: application/json" \
  -H "token: user_token_here" \
  -d '{
    "userID": "user_001",
    "pagination": {
      "pageNumber": 1,
      "showNumber": 20
    }
  }'

# 2. 设置会话免打扰
curl -X POST "http://localhost:10002/conversation/set_receive_message_opt" \
  -H "Content-Type: application/json" \
  -H "token: user_token_here" \
  -d '{
    "ownerUserID": "user_001",
    "conversationIDs": ["single_user_001_user_002"],
    "opt": 1
  }'

# 3. 设置会话置顶
curl -X POST "http://localhost:10002/conversation/set_conversation" \
  -H "Content-Type: application/json" \
  -H "token: user_token_here" \
  -d '{
    "conversation": {
      "ownerUserID": "user_001",
      "conversationID": "single_user_001_user_002",
      "isPinned": true
    }
  }'

# 4. 设置私聊模式
curl -X POST "http://localhost:10002/conversation/set_conversation_is_private_chat" \
  -H "Content-Type: application/json" \
  -H "token: user_token_here" \
  -d '{
    "sendUserID": "user_001",
    "recvUserID": "user_002",
    "isPrivate": true
  }'

# 5. 获取总未读数
curl -X POST "http://localhost:10002/conversation/get_total_unread_msg_count" \
  -H "Content-Type: application/json" \
  -H "token: user_token_here" \
  -d '{
    "userID": "user_001"
  }'
```

## 会话ID格式说明

### 单聊会话ID格式
```
single_{用户ID1}_{用户ID2}
```
其中用户ID按字典序排列，确保同一对用户的会话ID唯一。

### 群聊会话ID格式  
```
group_{群组ID}
```

### 通知会话ID格式
```
notification_{通知类型}_{用户ID}
```

## 注意事项

1. **会话排序**: 会话列表按最新消息时间倒序排列，置顶会话优先显示
2. **未读数统计**: 免打扰的会话不计入总未读数，但会话本身仍会显示未读数
3. **私聊模式**: 开启后消息端到端加密，仅双方可见
4. **阅后即焚**: 设置后消息在指定时间后自动删除
5. **会话同步**: 会话设置会在多端同步
6. **权限控制**: 只能操作自己的会话，不能修改其他用户的会话设置
7. **批量操作**: 批量接口有数量限制，避免一次处理过多数据
8. **群聊@类型**: 群聊中的@消息会特殊标记，便于用户快速定位 