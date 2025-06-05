# 消息相关接口 (Message)

## 概述

消息接口提供完整的消息管理功能，包括各种类型消息的发送、接收、撤回、已读状态管理等。支持文本、图片、语音、视频、文件、@消息、自定义消息等多种消息类型。

## 消息内容类型

| 值 | 类型 | 说明 |
|----|------|------|
| 101 | Text | 文本消息 |
| 102 | Picture | 图片消息 |
| 103 | Voice | 语音消息 |
| 104 | Video | 视频消息 |
| 105 | File | 文件消息 |
| 106 | AtText | @消息 |
| 110 | Custom | 自定义消息 |
| 111 | Revoke | 撤回消息 |
| 1400 | BusinessNotification | 业务通知 |

## 接口列表

### 1. 发送消息
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
| recvID | string | 否 | 接收者用户ID（单聊时必填，群聊时为空） |
| sendMsg | object | 是 | 消息发送对象 |
| sendMsg.sendID | string | 是 | 发送者用户ID |
| sendMsg.groupID | string | 否 | 群组ID（群聊时必填，单聊时为空） |
| sendMsg.senderNickname | string | 否 | 发送者昵称 |
| sendMsg.senderFaceURL | string | 否 | 发送者头像URL |
| sendMsg.senderPlatformID | int32 | 是 | 发送者平台ID |
| sendMsg.content | object | 是 | 消息内容，根据contentType不同而不同 |
| sendMsg.contentType | int32 | 是 | 消息类型，见消息内容类型常量表 |
| sendMsg.sessionType | int32 | 是 | 会话类型：1-单聊，2-群聊，4-通知 |
| sendMsg.isOnlineOnly | bool | 否 | 是否仅在线发送，默认false |
| sendMsg.notOfflinePush | bool | 否 | 是否禁用离线推送，默认false |
| sendMsg.sendTime | int64 | 否 | 发送时间戳（毫秒），0表示使用服务器时间 |
| sendMsg.offlinePushInfo | object | 否 | 离线推送信息 |
| sendMsg.ex | string | 否 | 消息扩展字段 |

#### 文本消息 (contentType: 101)
```json
{
  "content": {
    "content": "这是一条文本消息"
  }
}
```

**字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| content | string | 是 | 文本内容，最大4096字符 |

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

**字段说明**:
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

**字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| uuid | string | 否 | 语音唯一标识 |
| soundPath | string | 否 | 本地文件路径 |
| sourceUrl | string | 是 | 语音文件URL |
| dataSize | int64 | 否 | 文件大小（字节） |
| duration | int64 | 是 | 语音时长（秒），最小1秒，最大120秒 |

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

**字段说明**:
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

**字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| filePath | string | 否 | 本地文件路径 |
| uuid | string | 否 | 文件唯一标识 |
| sourceUrl | string | 是 | 文件下载URL |
| fileName | string | 是 | 文件名称 |
| fileSize | int64 | 是 | 文件大小（字节），最大100MB |

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

**字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| text | string | 否 | @消息文本内容 |
| atUserList | array | 是 | @的用户ID列表，最大1000个，"all"表示@所有人 |
| isAtSelf | bool | 否 | 是否@自己 |

#### 自定义消息 (contentType: 110)
```json
{
  "content": {
    "data": "{\"type\":\"location\",\"lat\":39.9042,\"lng\":116.4074}",
    "description": "位置信息",
    "extension": "{\"address\":\"北京市朝阳区\"}"
  }
}
```

**字段说明**:
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

---

### 2. 批量发送消息
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
| recvIDs | array | 是 | 接收者用户ID列表，isSendAll为false时必填，最大10000个 |

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

---

### 3. 发送业务通知
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

---

### 4. 获取最新序列号
**接口地址**: `POST /msg/newest_seq`

**功能描述**: 获取会话的最新消息序列号

**请求参数**:
```json
{
  "userID": "user_001",
  "conversationIDs": ["conv_001", "conv_002"]
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| userID | string | 是 | 用户ID |
| conversationIDs | array | 是 | 会话ID列表，最大100个 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "maxSeqs": [
      {
        "conversationID": "conv_001",
        "maxSeq": 156
      },
      {
        "conversationID": "conv_002",
        "maxSeq": 89
      }
    ]
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| maxSeqs | array | 最新序列号列表 |
| maxSeqs[].conversationID | string | 会话ID |
| maxSeqs[].maxSeq | int64 | 最新消息序列号 |

---

### 5. 根据序列号拉取消息
**接口地址**: `POST /msg/pull_msg_by_seq`

**功能描述**: 根据指定的序列号列表拉取消息

**请求参数**:
```json
{
  "userID": "user_001",
  "conversationID": "conv_001",
  "seqs": [150, 151, 152, 153, 154, 155]
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| userID | string | 是 | 用户ID |
| conversationID | string | 是 | 会话ID |
| seqs | array | 是 | 消息序列号列表，最大1000个 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "msgs": [
      {
        "serverMsgID": "server_msg_150",
        "clientMsgID": "client_msg_150",
        "sendID": "user_002",
        "recvID": "user_001",
        "senderNickname": "李四",
        "senderFaceURL": "https://example.com/avatar/002.jpg",
        "sessionType": 1,
        "msgFrom": 100,
        "contentType": 101,
        "content": "{\"content\":\"Hello World\"}",
        "seq": 150,
        "sendTime": 1640995200000,
        "createTime": 1640995200000,
        "status": 1,
        "ex": ""
      }
    ]
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| msgs | array | 消息列表 |
| msgs[].serverMsgID | string | 服务器消息ID |
| msgs[].clientMsgID | string | 客户端消息ID |
| msgs[].sendID | string | 发送者用户ID |
| msgs[].recvID | string | 接收者用户ID |
| msgs[].senderNickname | string | 发送者昵称 |
| msgs[].senderFaceURL | string | 发送者头像 |
| msgs[].sessionType | int32 | 会话类型 |
| msgs[].msgFrom | int32 | 消息来源：100-用户，200-系统 |
| msgs[].contentType | int32 | 消息内容类型 |
| msgs[].content | string | 消息内容JSON字符串 |
| msgs[].seq | int64 | 消息序列号 |
| msgs[].sendTime | int64 | 发送时间（毫秒时间戳） |
| msgs[].createTime | int64 | 创建时间（毫秒时间戳） |
| msgs[].status | int32 | 消息状态：1-正常，2-已撤回 |
| msgs[].ex | string | 扩展字段 |

---

### 6. 撤回消息
**接口地址**: `POST /msg/revoke_msg`

**功能描述**: 撤回已发送的消息

**请求参数**:
```json
{
  "conversationID": "conv_001",
  "seq": 155,
  "userID": "user_001"
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| conversationID | string | 是 | 会话ID |
| seq | int64 | 是 | 要撤回的消息序列号 |
| userID | string | 是 | 操作用户ID |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

**注意**: 只能撤回2分钟内的消息，且只能撤回自己发送的消息

---

### 7. 标记消息为已读
**接口地址**: `POST /msg/mark_msgs_as_read`

**功能描述**: 标记指定消息为已读状态

**请求参数**:
```json
{
  "userID": "user_001",
  "conversationID": "conv_001",
  "seqs": [150, 151, 152]
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| userID | string | 是 | 用户ID |
| conversationID | string | 是 | 会话ID |
| seqs | array | 是 | 要标记为已读的消息序列号列表 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

---

### 8. 标记会话为已读
**接口地址**: `POST /msg/mark_conversation_as_read`

**功能描述**: 标记整个会话为已读状态

**请求参数**:
```json
{
  "userID": "user_001",
  "conversationIDs": ["conv_001", "conv_002"],
  "hasReadSeq": 155
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| userID | string | 是 | 用户ID |
| conversationIDs | array | 是 | 会话ID列表 |
| hasReadSeq | int64 | 是 | 已读到的序列号 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

---

### 9. 获取会话已读和最大序列号
**接口地址**: `POST /msg/get_conversations_has_read_and_max_seq`

**功能描述**: 获取会话的已读序列号和最大序列号

**请求参数**:
```json
{
  "userID": "user_001",
  "conversationIDs": ["conv_001", "conv_002"]
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| userID | string | 是 | 用户ID |
| conversationIDs | array | 是 | 会话ID列表 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "seqs": [
      {
        "conversationID": "conv_001",
        "hasReadSeq": 150,
        "maxSeq": 155
      },
      {
        "conversationID": "conv_002",
        "hasReadSeq": 85,
        "maxSeq": 89
      }
    ]
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| seqs | array | 序列号信息列表 |
| seqs[].conversationID | string | 会话ID |
| seqs[].hasReadSeq | int64 | 已读序列号 |
| seqs[].maxSeq | int64 | 最大序列号 |

---

### 10. 清空会话消息
**接口地址**: `POST /msg/clear_conversation_msg`

**功能描述**: 清空指定会话的所有消息

**请求参数**:
```json
{
  "userID": "user_001",
  "conversationIDs": ["conv_001"],
  "deleteSyncOpt": 1
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| userID | string | 是 | 用户ID |
| conversationIDs | array | 是 | 要清空的会话ID列表 |
| deleteSyncOpt | int32 | 否 | 删除同步选项：1-本端删除，2-双端删除 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

---

### 11. 删除消息
**接口地址**: `POST /msg/delete_msgs`

**功能描述**: 删除指定的消息

**请求参数**:
```json
{
  "userID": "user_001",
  "conversationID": "conv_001",
  "seqs": [150, 151],
  "deleteSyncOpt": 1
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| userID | string | 是 | 用户ID |
| conversationID | string | 是 | 会话ID |
| seqs | array | 是 | 要删除的消息序列号列表 |
| deleteSyncOpt | int32 | 否 | 删除同步选项：1-本端删除，2-双端删除 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

---

### 12. 获取服务器时间
**接口地址**: `POST /msg/get_server_time`

**功能描述**: 获取服务器当前时间

**请求参数**:
```json
{}
```

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "serverTime": 1640995200000
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| serverTime | int64 | 服务器当前时间（毫秒时间戳） |

## 使用示例

### 发送不同类型消息示例

```bash
# 1. 发送文本消息
curl -X POST "http://localhost:10002/msg/send_msg" \
  -H "Content-Type: application/json" \
  -H "token: admin_token_here" \
  -d '{
    "recvID": "user_002",
    "sendMsg": {
      "sendID": "user_001",
      "senderNickname": "张三",
      "senderPlatformID": 1,
      "content": {
        "content": "你好，最近怎么样？"
      },
      "contentType": 101,
      "sessionType": 1
    }
  }'

# 2. 发送图片消息
curl -X POST "http://localhost:10002/msg/send_msg" \
  -H "Content-Type: application/json" \
  -H "token: admin_token_here" \
  -d '{
    "recvID": "user_002",
    "sendMsg": {
      "sendID": "user_001",
      "senderNickname": "张三",
      "senderPlatformID": 1,
      "content": {
        "sourcePicture": {
          "url": "https://example.com/image.jpg",
          "type": "jpg",
          "width": 1080,
          "height": 720
        },
        "bigPicture": {
          "url": "https://example.com/image.jpg",
          "type": "jpg", 
          "width": 1080,
          "height": 720
        },
        "snapshotPicture": {
          "url": "https://example.com/thumbnail.jpg",
          "type": "jpg",
          "width": 200,
          "height": 150
        }
      },
      "contentType": 102,
      "sessionType": 1
    }
  }'

# 3. 群聊@消息
curl -X POST "http://localhost:10002/msg/send_msg" \
  -H "Content-Type: application/json" \
  -H "token: admin_token_here" \
  -d '{
    "sendMsg": {
      "sendID": "user_001",
      "groupID": "group_001",
      "senderNickname": "张三",
      "senderPlatformID": 1,
      "content": {
        "text": "@all 大家好，明天开会",
        "atUserList": ["all"],
        "isAtSelf": false
      },
      "contentType": 106,
      "sessionType": 2
    }
  }'
```

## 注意事项

1. **消息大小限制**: 不同类型消息有不同的大小限制，文件消息最大100MB
2. **消息撤回**: 只能撤回2分钟内自己发送的消息
3. **离线推送**: 可通过offlinePushInfo自定义推送内容
4. **在线消息**: isOnlineOnly为true时，离线用户收不到消息
5. **序列号**: 消息序列号在会话中是连续递增的
6. **权限控制**: 发送消息接口需要管理员权限
7. **文件上传**: 媒体文件需要先通过第三方服务接口上传获取URL
8. **消息同步**: 支持多端消息同步，注意处理重复消息 