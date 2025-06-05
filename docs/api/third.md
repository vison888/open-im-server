# 第三方服务接口 (Third)

## 概述

第三方服务接口提供文件上传、推送服务、日志管理等扩展功能，支持OSS存储、FCM推送、webhook回调等第三方服务集成。

## 接口列表

### 1. 应用上传文件
**接口地址**: `POST /third/apply_put`

**功能描述**: 申请文件上传权限，获取上传凭证

**请求参数**:
```json
{
  "name": "document.pdf",
  "size": 1048576,
  "contentType": "application/pdf",
  "group": "documents"
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| name | string | 是 | 文件名称，需包含扩展名 |
| size | int64 | 是 | 文件大小（字节），最大100MB |
| contentType | string | 是 | 文件MIME类型 |
| group | string | 否 | 文件分组，如：images、documents、videos |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "url": "https://openim-1234567890.cos.ap-beijing.myqcloud.com/",
    "method": "PUT",
    "key": "uploads/2023/01/01/uuid-document.pdf",
    "query": {},
    "header": {
      "Content-Type": "application/pdf",
      "Authorization": "q-sign-algorithm=sha1&q-ak=AKIDxxxxxx&q-sign-time=1640995200;1641081600&q-key-time=1640995200;1641081600&q-header-list=&q-url-param-list=&q-signature=xxxxxxxx"
    },
    "formData": {},
    "putID": "put_12345",
    "expireTime": 1641081600000
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| url | string | 上传目标URL |
| method | string | HTTP方法，通常为PUT |
| key | string | 文件在存储服务中的键名 |
| query | object | URL查询参数 |
| header | object | 请求头信息 |
| formData | object | 表单数据（用于POST方式上传） |
| putID | string | 上传任务ID |
| expireTime | int64 | 凭证过期时间（毫秒时间戳） |

---

### 2. 确认上传完成
**接口地址**: `POST /third/confirm_put`

**功能描述**: 确认文件上传完成，获取访问URL

**请求参数**:
```json
{
  "putID": "put_12345"
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| putID | string | 是 | 上传任务ID，来自申请上传接口 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "url": "https://example.com/files/uploads/2023/01/01/uuid-document.pdf",
    "size": 1048576,
    "hash": "d41d8cd98f00b204e9800998ecf8427e"
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| url | string | 文件访问URL |
| size | int64 | 文件实际大小（字节） |
| hash | string | 文件MD5哈希值 |

---

### 3. 获取上传信息
**接口地址**: `POST /third/get_put`

**功能描述**: 根据上传ID获取上传信息

**请求参数**:
```json
{
  "putID": "put_12345"
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| putID | string | 是 | 上传任务ID |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "putID": "put_12345",
    "name": "document.pdf",
    "size": 1048576,
    "contentType": "application/pdf",
    "group": "documents",
    "status": 2,
    "url": "https://example.com/files/uploads/2023/01/01/uuid-document.pdf",
    "createTime": 1640995200000,
    "expireTime": 1641081600000
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| putID | string | 上传任务ID |
| name | string | 文件名称 |
| size | int64 | 文件大小（字节） |
| contentType | string | 文件MIME类型 |
| group | string | 文件分组 |
| status | int32 | 上传状态：1-待上传，2-上传完成，3-上传失败 |
| url | string | 文件访问URL（上传完成后有值） |
| createTime | int64 | 创建时间（毫秒时间戳） |
| expireTime | int64 | 过期时间（毫秒时间戳） |

---

### 4. 获取下载信息
**接口地址**: `POST /third/get_url`

**功能描述**: 获取文件下载信息

**请求参数**:
```json
{
  "name": "uploads/2023/01/01/uuid-document.pdf",
  "expires": 3600
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| name | string | 是 | 文件键名或URL |
| expires | int64 | 否 | 有效期（秒），默认3600秒 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "url": "https://example.com/files/uploads/2023/01/01/uuid-document.pdf?expires=1641001800&signature=xxxxxxxx",
    "expireTime": 1641001800000
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| url | string | 带签名的下载URL |
| expireTime | int64 | URL过期时间（毫秒时间戳） |

---

### 5. 删除文件
**接口地址**: `POST /third/delete_file`

**功能描述**: 删除存储的文件

**请求参数**:
```json
{
  "name": "uploads/2023/01/01/uuid-document.pdf"
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| name | string | 是 | 文件键名 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

---

### 6. FCM推送
**接口地址**: `POST /third/fcm_update_token`

**功能描述**: 更新用户的FCM推送Token

**请求参数**:
```json
{
  "platformID": 1,
  "fcmToken": "dGVzdF90b2tlbl9zdHJpbmcxMjM0NTY3ODkw",
  "account": "user_001",
  "expireTime": 1672531200000
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| platformID | int32 | 是 | 平台ID，见平台类型常量表 |
| fcmToken | string | 是 | FCM推送Token |
| account | string | 是 | 用户账号 |
| expireTime | int64 | 否 | Token过期时间（毫秒时间戳） |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

---

### 7. 设置应用Badge
**接口地址**: `POST /third/set_app_badge`

**功能描述**: 设置应用图标上的未读数角标

**请求参数**:
```json
{
  "userID": "user_001",
  "appUnreadCount": 5
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| userID | string | 是 | 用户ID |
| appUnreadCount | int32 | 是 | 应用未读数量 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

---

### 8. 搜索日志
**接口地址**: `POST /logs/search`

**功能描述**: 搜索系统日志

**请求参数**:
```json
{
  "keyword": "error",
  "start": 1640995200000,
  "end": 1641081600000,
  "pagination": {
    "pageNumber": 1,
    "showNumber": 50
  }
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| keyword | string | 否 | 搜索关键词 |
| start | int64 | 是 | 开始时间（毫秒时间戳） |
| end | int64 | 是 | 结束时间（毫秒时间戳） |
| pagination | object | 是 | 分页参数 |
| pagination.pageNumber | int32 | 是 | 页码，从1开始 |
| pagination.showNumber | int32 | 是 | 每页显示数量，最大100 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "logsNum": 125,
    "logs": [
      {
        "filename": "/opt/openim/logs/api.log",
        "content": "2023-01-01 12:00:00 ERROR [api] user login failed: user not found",
        "linenumber": 1001
      },
      {
        "filename": "/opt/openim/logs/msg.log", 
        "content": "2023-01-01 12:01:00 ERROR [msg] send message failed: invalid token",
        "linenumber": 2055
      }
    ]
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| logsNum | int32 | 日志总条数 |
| logs | array | 日志条目列表 |
| logs[].filename | string | 日志文件名 |
| logs[].content | string | 日志内容 |
| logs[].linenumber | int32 | 行号 |

---

### 9. 发送短信
**接口地址**: `POST /third/send_sms`

**功能描述**: 发送短信验证码或通知

**请求参数**:
```json
{
  "phoneNumber": "+86138****8888",
  "verifyCode": "123456",
  "usedFor": 1,
  "ex": ""
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| phoneNumber | string | 是 | 手机号码，需要国际格式 |
| verifyCode | string | 是 | 验证码 |
| usedFor | int32 | 是 | 使用场景：1-注册，2-登录，3-找回密码，4-修改手机号 |
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

### 10. 邮件发送
**接口地址**: `POST /third/send_mail`

**功能描述**: 发送邮件通知

**请求参数**:
```json
{
  "toMail": "user@example.com",
  "verifyCode": "123456",
  "usedFor": 1,
  "ex": ""
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| toMail | string | 是 | 收件人邮箱地址 |
| verifyCode | string | 是 | 验证码 |
| usedFor | int32 | 是 | 使用场景：1-注册，2-登录，3-找回密码，4-修改邮箱 |
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

### 11. 对象存储信息
**接口地址**: `POST /third/object_info`

**功能描述**: 获取对象存储的配置信息

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
    "type": "cos",
    "bucket": "openim-1234567890",
    "region": "ap-beijing",
    "endpoint": "https://openim-1234567890.cos.ap-beijing.myqcloud.com",
    "publicRead": false
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| type | string | 存储类型：cos、oss、s3、minio |
| bucket | string | 存储桶名称 |
| region | string | 存储区域 |
| endpoint | string | 存储服务端点 |
| publicRead | bool | 是否支持公开读取 |

---

### 12. 签名
**接口地址**: `POST /third/sign_url`

**功能描述**: 为URL生成签名

**请求参数**:
```json
{
  "url": "https://example.com/api/webhook",
  "method": "POST",
  "params": {
    "key1": "value1",
    "key2": "value2"
  },
  "expires": 3600
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| url | string | 是 | 要签名的URL |
| method | string | 是 | HTTP方法 |
| params | object | 否 | 请求参数 |
| expires | int64 | 否 | 签名有效期（秒），默认3600 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "signedUrl": "https://example.com/api/webhook?signature=xxxxxxxx&expires=1641001800",
    "signature": "xxxxxxxx",
    "expireTime": 1641001800000
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| signedUrl | string | 带签名的URL |
| signature | string | 签名值 |
| expireTime | int64 | 签名过期时间（毫秒时间戳） |

---

### 13. Webhook配置
**接口地址**: `POST /third/webhook/config`

**功能描述**: 配置Webhook回调

**请求参数**:
```json
{
  "url": "https://your-domain.com/webhook/openim",
  "events": ["user.register", "message.send", "group.create"],
  "secret": "your_webhook_secret",
  "active": true
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| url | string | 是 | Webhook回调URL |
| events | array | 是 | 订阅的事件类型列表 |
| secret | string | 否 | Webhook密钥，用于验证签名 |
| active | bool | 否 | 是否激活，默认true |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "webhookID": "webhook_001",
    "status": "active"
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| webhookID | string | Webhook配置ID |
| status | string | 配置状态：active-已激活，inactive-未激活 |

---

### 14. Webhook事件测试
**接口地址**: `POST /third/webhook/test`

**功能描述**: 测试Webhook配置

**请求参数**:
```json
{
  "webhookID": "webhook_001",
  "eventType": "test.ping"
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| webhookID | string | 是 | Webhook配置ID |
| eventType | string | 否 | 测试事件类型，默认test.ping |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "success": true,
    "responseCode": 200,
    "responseBody": "ok",
    "responseTime": 150
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| success | bool | 测试是否成功 |
| responseCode | int32 | HTTP响应码 |
| responseBody | string | 响应内容 |
| responseTime | int64 | 响应时间（毫秒） |

---

### 15. 应用版本检查
**接口地址**: `POST /third/app_version`

**功能描述**: 检查应用版本更新

**请求参数**:
```json
{
  "platform": "android",
  "version": "1.0.0",
  "buildNumber": 100
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| platform | string | 是 | 平台：ios、android、web |
| version | string | 是 | 当前版本号 |
| buildNumber | int32 | 否 | 构建号 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "hasUpdate": true,
    "version": "1.1.0",
    "buildNumber": 110,
    "forceUpdate": false,
    "updateUrl": "https://example.com/app/download/v1.1.0",
    "updateLog": "1. 新增消息撤回功能\n2. 优化界面体验\n3. 修复已知问题",
    "minSupportedVersion": "1.0.0"
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| hasUpdate | bool | 是否有更新 |
| version | string | 最新版本号 |
| buildNumber | int32 | 最新构建号 |
| forceUpdate | bool | 是否强制更新 |
| updateUrl | string | 更新下载URL |
| updateLog | string | 更新日志 |
| minSupportedVersion | string | 最低支持版本 |

## 使用示例

### 文件上传完整流程

```bash
# 1. 申请上传
curl -X POST "http://localhost:10002/third/apply_put" \
  -H "Content-Type: application/json" \
  -H "token: user_token_here" \
  -d '{
    "name": "avatar.jpg",
    "size": 204800,
    "contentType": "image/jpeg",
    "group": "avatars"
  }'

# 2. 使用返回的凭证上传文件到OSS
curl -X PUT "https://openim-bucket.cos.region.myqcloud.com/uploads/2023/01/01/uuid-avatar.jpg" \
  -H "Content-Type: image/jpeg" \
  -H "Authorization: 返回的授权头" \
  --data-binary "@avatar.jpg"

# 3. 确认上传完成
curl -X POST "http://localhost:10002/third/confirm_put" \
  -H "Content-Type: application/json" \
  -H "token: user_token_here" \
  -d '{
    "putID": "put_12345"
  }'
```

### Webhook配置示例

```bash
# 1. 配置Webhook
curl -X POST "http://localhost:10002/third/webhook/config" \
  -H "Content-Type: application/json" \
  -H "token: admin_token_here" \
  -d '{
    "url": "https://your-app.com/webhook/openim",
    "events": ["message.send", "user.register"],
    "secret": "your_secret_key",
    "active": true
  }'

# 2. 测试Webhook
curl -X POST "http://localhost:10002/third/webhook/test" \
  -H "Content-Type: application/json" \
  -H "token: admin_token_here" \
  -d '{
    "webhookID": "webhook_001"
  }'
```

## Webhook事件类型

| 事件类型 | 说明 | 触发时机 |
|----------|------|----------|
| user.register | 用户注册 | 新用户注册成功时 |
| user.login | 用户登录 | 用户登录成功时 |
| message.send | 消息发送 | 用户发送消息时 |
| group.create | 群组创建 | 创建新群组时 |
| group.join | 加入群组 | 用户加入群组时 |
| friend.add | 添加好友 | 成功添加好友时 |

## 存储服务支持

| 服务类型 | 说明 | 配置要求 |
|----------|------|----------|
| 腾讯云COS | 对象存储服务 | SecretId、SecretKey、Bucket、Region |
| 阿里云OSS | 对象存储服务 | AccessKeyId、AccessKeySecret、Bucket、Endpoint |
| AWS S3 | 亚马逊S3 | AccessKey、SecretKey、Bucket、Region |
| MinIO | 开源对象存储 | AccessKey、SecretKey、Endpoint、Bucket |

## 注意事项

1. **文件上传**: 大文件建议使用分片上传，单次最大100MB
2. **凭证安全**: 上传凭证具有时效性，注意及时使用
3. **Webhook验证**: 使用HMAC-SHA256验证Webhook签名
4. **推送Token**: FCM Token需要定期更新，避免推送失败
5. **日志查询**: 日志搜索有时间范围限制，避免查询过大时间跨度
6. **权限控制**: 第三方接口通常需要管理员权限
7. **错误重试**: 网络请求失败时建议实现重试机制
8. **资源清理**: 及时清理不需要的文件，避免存储费用增加 