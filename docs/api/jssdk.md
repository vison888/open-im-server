# JS SDK接口 (JSSDK)

## 概述

JS SDK接口专为Web端提供，包含SDK初始化、WebRTC通话、浏览器兼容性检测等Web专用功能。适用于浏览器环境的即时通讯功能集成。

## 接口列表

### 1. 获取初始化配置
**接口地址**: `POST /jssdk/get_init_config`

**功能描述**: 获取JS SDK初始化所需的配置信息

**请求参数**:
```json
{
  "userID": "user_001",
  "platform": "web",
  "domain": "https://example.com"
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| userID | string | 是 | 用户ID |
| platform | string | 否 | 平台标识，默认"web" |
| domain | string | 否 | 应用域名，用于CORS验证 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "apiURL": "https://api.openim.com",
    "wsURL": "wss://ws.openim.com/ws",
    "adminURL": "https://admin.openim.com",
    "objectStorage": {
      "enable": true,
      "endpoint": "https://minio.openim.com",
      "bucket": "openim-web",
      "accessKeyID": "web_access_key",
      "secretAccessKey": "web_secret_key",
      "publicRead": true
    },
    "chatConfig": {
      "maxMessageLength": 4096,
      "allowedFileTypes": ["jpg", "png", "gif", "pdf", "doc", "docx"],
      "maxFileSize": 104857600,
      "enableVoiceCall": true,
      "enableVideoCall": true,
      "enableScreenShare": true
    },
    "webRTC": {
      "enable": true,
      "stunServers": [
        "stun:stun.l.google.com:19302",
        "stun:stun1.l.google.com:19302"
      ],
      "turnServers": [
        {
          "urls": "turn:turn.openim.com:3478",
          "username": "turn_user",
          "credential": "turn_pass"
        }
      ]
    },
    "features": {
      "enableGroupCall": true,
      "enableLiveStream": false,
      "enableWhiteboard": false,
      "enableTranslation": true,
      "enableBots": false
    },
    "security": {
      "enableE2EE": true,
      "enableMessageEncryption": false,
      "sessionTimeout": 3600000
    },
    "ui": {
      "theme": "light",
      "language": "zh-CN",
      "customCSS": "",
      "logoURL": "https://example.com/logo.png"
    }
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| apiURL | string | API服务地址 |
| wsURL | string | WebSocket连接地址 |
| adminURL | string | 管理后台地址 |
| objectStorage | object | 对象存储配置 |
| objectStorage.enable | bool | 是否启用对象存储 |
| objectStorage.endpoint | string | 存储服务端点 |
| objectStorage.bucket | string | 存储桶名称 |
| objectStorage.accessKeyID | string | 访问密钥ID |
| objectStorage.secretAccessKey | string | 访问密钥 |
| objectStorage.publicRead | bool | 是否支持公开读取 |
| chatConfig | object | 聊天配置 |
| chatConfig.maxMessageLength | int32 | 最大消息长度 |
| chatConfig.allowedFileTypes | array | 允许的文件类型 |
| chatConfig.maxFileSize | int64 | 最大文件大小（字节） |
| chatConfig.enableVoiceCall | bool | 是否启用语音通话 |
| chatConfig.enableVideoCall | bool | 是否启用视频通话 |
| chatConfig.enableScreenShare | bool | 是否启用屏幕共享 |
| webRTC | object | WebRTC配置 |
| webRTC.enable | bool | 是否启用WebRTC |
| webRTC.stunServers | array | STUN服务器列表 |
| webRTC.turnServers | array | TURN服务器配置 |
| features | object | 功能特性配置 |
| features.enableGroupCall | bool | 是否启用群组通话 |
| features.enableLiveStream | bool | 是否启用直播功能 |
| features.enableWhiteboard | bool | 是否启用白板功能 |
| features.enableTranslation | bool | 是否启用翻译功能 |
| features.enableBots | bool | 是否启用机器人 |
| security | object | 安全配置 |
| security.enableE2EE | bool | 是否启用端到端加密 |
| security.enableMessageEncryption | bool | 是否启用消息加密 |
| security.sessionTimeout | int64 | 会话超时时间（毫秒） |
| ui | object | UI配置 |
| ui.theme | string | 主题：light、dark |
| ui.language | string | 语言代码 |
| ui.customCSS | string | 自定义CSS |
| ui.logoURL | string | Logo URL |

---

### 2. 检查浏览器兼容性
**接口地址**: `POST /jssdk/check_browser_compatibility`

**功能描述**: 检查当前浏览器对WebRTC和其他功能的兼容性

**请求参数**:
```json
{
  "userAgent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
  "features": ["webrtc", "websocket", "indexeddb", "notification"]
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| userAgent | string | 是 | 浏览器User-Agent字符串 |
| features | array | 否 | 需要检查的功能列表 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "browserInfo": {
      "name": "Chrome",
      "version": "91.0.4472.124",
      "platform": "Windows",
      "mobile": false,
      "supported": true
    },
    "compatibility": {
      "webrtc": {
        "supported": true,
        "version": "1.0",
        "features": ["getUserMedia", "RTCPeerConnection", "RTCDataChannel"]
      },
      "websocket": {
        "supported": true,
        "version": "13"
      },
      "indexeddb": {
        "supported": true,
        "version": "3.0"
      },
      "notification": {
        "supported": true,
        "permission": "default"
      },
      "fileapi": {
        "supported": true,
        "features": ["File", "FileReader", "Blob", "FormData"]
      },
      "encryption": {
        "supported": true,
        "features": ["SubtleCrypto", "WebCrypto"]
      }
    },
    "recommendations": [
      {
        "level": "warning",
        "feature": "notification",
        "message": "建议开启浏览器通知权限以获得更好的体验"
      }
    ],
    "unsupportedFeatures": [],
    "minimumRequirements": {
      "chrome": "60+",
      "firefox": "55+",
      "safari": "11+",
      "edge": "79+"
    }
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| browserInfo | object | 浏览器基本信息 |
| browserInfo.name | string | 浏览器名称 |
| browserInfo.version | string | 浏览器版本 |
| browserInfo.platform | string | 操作系统平台 |
| browserInfo.mobile | bool | 是否为移动设备 |
| browserInfo.supported | bool | 是否支持基本功能 |
| compatibility | object | 兼容性检查结果 |
| compatibility.webrtc | object | WebRTC支持情况 |
| compatibility.websocket | object | WebSocket支持情况 |
| compatibility.indexeddb | object | IndexedDB支持情况 |
| compatibility.notification | object | 通知API支持情况 |
| compatibility.fileapi | object | 文件API支持情况 |
| compatibility.encryption | object | 加密API支持情况 |
| recommendations | array | 使用建议列表 |
| recommendations[].level | string | 建议级别：info、warning、error |
| recommendations[].feature | string | 相关功能 |
| recommendations[].message | string | 建议内容 |
| unsupportedFeatures | array | 不支持的功能列表 |
| minimumRequirements | object | 最低浏览器版本要求 |

---

### 3. WebRTC信令服务器配置
**接口地址**: `POST /jssdk/get_webrtc_config`

**功能描述**: 获取WebRTC通话所需的信令服务器配置

**请求参数**:
```json
{
  "userID": "user_001",
  "callType": "video",
  "roomID": "room_12345"
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| userID | string | 是 | 用户ID |
| callType | string | 是 | 通话类型：voice-语音，video-视频，screen-屏幕共享 |
| roomID | string | 否 | 房间ID，群组通话时必填 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "signalingURL": "wss://signaling.openim.com/ws",
    "turnConfig": {
      "iceServers": [
        {
          "urls": ["stun:stun.l.google.com:19302"]
        },
        {
          "urls": ["turn:turn.openim.com:3478"],
          "username": "user_001_1640995200",
          "credential": "temp_credential_12345"
        }
      ],
      "iceTransportPolicy": "all",
      "iceCandidatePoolSize": 10
    },
    "mediaConstraints": {
      "video": {
        "width": {"min": 320, "ideal": 1280, "max": 1920},
        "height": {"min": 240, "ideal": 720, "max": 1080},
        "frameRate": {"min": 15, "ideal": 30, "max": 60},
        "facingMode": "user"
      },
      "audio": {
        "echoCancellation": true,
        "noiseSuppression": true,
        "autoGainControl": true,
        "sampleRate": 48000,
        "channelCount": 2
      }
    },
    "codecPreferences": {
      "video": ["VP8", "VP9", "H264"],
      "audio": ["opus", "PCMU", "PCMA"]
    },
    "bandwidth": {
      "video": {"min": 150, "max": 2000},
      "audio": {"min": 16, "max": 128}
    },
    "recording": {
      "enabled": false,
      "format": "webm",
      "quality": "high"
    },
    "expireTime": 1641001800000
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| signalingURL | string | 信令服务器WebSocket地址 |
| turnConfig | object | ICE服务器配置 |
| turnConfig.iceServers | array | ICE服务器列表 |
| turnConfig.iceTransportPolicy | string | ICE传输策略 |
| turnConfig.iceCandidatePoolSize | int32 | ICE候选池大小 |
| mediaConstraints | object | 媒体约束配置 |
| mediaConstraints.video | object | 视频约束 |
| mediaConstraints.audio | object | 音频约束 |
| codecPreferences | object | 编解码器偏好 |
| codecPreferences.video | array | 视频编解码器列表 |
| codecPreferences.audio | array | 音频编解码器列表 |
| bandwidth | object | 带宽限制 |
| bandwidth.video | object | 视频带宽（kbps） |
| bandwidth.audio | object | 音频带宽（kbps） |
| recording | object | 录制配置 |
| recording.enabled | bool | 是否启用录制 |
| recording.format | string | 录制格式 |
| recording.quality | string | 录制质量 |
| expireTime | int64 | 配置过期时间（毫秒时间戳） |

---

### 4. 上传Web日志
**接口地址**: `POST /jssdk/upload_logs`

**功能描述**: 上传Web端的错误日志和性能数据

**请求参数**:
```json
{
  "userID": "user_001",
  "sessionID": "session_12345",
  "logs": [
    {
      "level": "error",
      "timestamp": 1640995200000,
      "message": "WebRTC connection failed",
      "details": {
        "error": "ICE connection state failed",
        "userAgent": "Chrome/91.0.4472.124",
        "url": "https://app.example.com/chat"
      }
    },
    {
      "level": "performance",
      "timestamp": 1640995300000,
      "message": "Page load time",
      "details": {
        "loadTime": 1250,
        "domReady": 800,
        "firstPaint": 450
      }
    }
  ],
  "performance": {
    "memoryUsage": 52428800,
    "cpuUsage": 15.6,
    "networkLatency": 45,
    "fps": 30
  }
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| userID | string | 是 | 用户ID |
| sessionID | string | 是 | 会话ID |
| logs | array | 是 | 日志条目列表 |
| logs[].level | string | 是 | 日志级别：error、warn、info、debug、performance |
| logs[].timestamp | int64 | 是 | 时间戳（毫秒） |
| logs[].message | string | 是 | 日志消息 |
| logs[].details | object | 否 | 详细信息 |
| performance | object | 否 | 性能数据 |
| performance.memoryUsage | int64 | 否 | 内存使用量（字节） |
| performance.cpuUsage | float | 否 | CPU使用率（百分比） |
| performance.networkLatency | int32 | 否 | 网络延迟（毫秒） |
| performance.fps | int32 | 否 | 帧率 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "logID": "log_67890",
    "uploadTime": 1640995400000,
    "status": "processed"
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| logID | string | 日志记录ID |
| uploadTime | int64 | 上传时间（毫秒时间戳） |
| status | string | 处理状态：pending-待处理，processed-已处理 |

---

### 5. 获取Web推送配置
**接口地址**: `POST /jssdk/get_push_config`

**功能描述**: 获取Web推送（Push API）配置

**请求参数**:
```json
{
  "userID": "user_001",
  "endpoint": "https://fcm.googleapis.com/fcm/send/..."
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| userID | string | 是 | 用户ID |
| endpoint | string | 否 | 推送端点URL |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "vapidPublicKey": "BEl62iUYgUivxIkv69yViEuiBIa-Ib9-SkvMeAtA3LFgDzkrxZJjSgSnfckjBJuBkr3qBUYIHBQFLXYp5Nksh8U",
    "pushConfig": {
      "enabled": true,
      "showNotifications": true,
      "soundEnabled": true,
      "vibrationEnabled": false,
      "iconURL": "https://example.com/icon-192x192.png",
      "badgeURL": "https://example.com/badge-72x72.png"
    },
    "subscription": {
      "endpoint": "https://fcm.googleapis.com/fcm/send/...",
      "keys": {
        "p256dh": "key_value_here",
        "auth": "auth_value_here"
      }
    },
    "permissions": {
      "notification": "granted",
      "push": "granted"
    }
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| vapidPublicKey | string | VAPID公钥 |
| pushConfig | object | 推送配置 |
| pushConfig.enabled | bool | 是否启用推送 |
| pushConfig.showNotifications | bool | 是否显示通知 |
| pushConfig.soundEnabled | bool | 是否启用声音 |
| pushConfig.vibrationEnabled | bool | 是否启用震动 |
| pushConfig.iconURL | string | 通知图标URL |
| pushConfig.badgeURL | string | 角标图标URL |
| subscription | object | 推送订阅信息 |
| subscription.endpoint | string | 推送端点 |
| subscription.keys | object | 加密密钥 |
| permissions | object | 权限状态 |
| permissions.notification | string | 通知权限：granted、denied、default |
| permissions.push | string | 推送权限状态 |

---

### 6. 更新Web推送订阅
**接口地址**: `POST /jssdk/update_push_subscription`

**功能描述**: 更新或注册Web推送订阅

**请求参数**:
```json
{
  "userID": "user_001",
  "subscription": {
    "endpoint": "https://fcm.googleapis.com/fcm/send/cYX8aBqTQJw:APA91bHkSP...",
    "keys": {
      "p256dh": "BLc4xRzKlKORKWlbdgFaBTTcLOzOQbPwvMgBL0VOuFJZWCGbmLz...",
      "auth": "BWgGJWs66BzKgwLQsR5K6g"
    }
  },
  "options": {
    "userVisibleOnly": true,
    "applicationServerKey": "BEl62iUYgUivxIkv69yViEuiBIa-Ib9-SkvMeAtA3LFgDzkrxZJjSgSnfckjBJuBkr3qBUYIHBQFLXYp5Nksh8U"
  }
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| userID | string | 是 | 用户ID |
| subscription | object | 是 | 推送订阅对象 |
| subscription.endpoint | string | 是 | 推送端点URL |
| subscription.keys | object | 是 | 加密密钥对 |
| subscription.keys.p256dh | string | 是 | P256DH公钥 |
| subscription.keys.auth | string | 是 | 认证密钥 |
| options | object | 否 | 订阅选项 |
| options.userVisibleOnly | bool | 否 | 是否仅用户可见 |
| options.applicationServerKey | string | 否 | 应用服务器密钥 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "subscriptionID": "sub_12345",
    "status": "active",
    "createTime": 1640995400000,
    "expireTime": 1672531200000
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| subscriptionID | string | 订阅ID |
| status | string | 订阅状态：active-活跃，expired-已过期 |
| createTime | int64 | 创建时间（毫秒时间戳） |
| expireTime | int64 | 过期时间（毫秒时间戳） |

## 使用示例

### SDK初始化完整流程

```javascript
// 1. 获取初始化配置
const configResponse = await fetch('/jssdk/get_init_config', {
  method: 'POST',
  headers: {
    'Content-Type': 'application/json',
    'token': userToken
  },
  body: JSON.stringify({
    userID: 'user_001',
    platform: 'web',
    domain: window.location.origin
  })
});

const config = await configResponse.json();

// 2. 检查浏览器兼容性
const compatibilityResponse = await fetch('/jssdk/check_browser_compatibility', {
  method: 'POST',
  headers: {
    'Content-Type': 'application/json'
  },
  body: JSON.stringify({
    userAgent: navigator.userAgent,
    features: ['webrtc', 'websocket', 'indexeddb', 'notification']
  })
});

const compatibility = await compatibilityResponse.json();

// 3. 初始化SDK
if (compatibility.data.browserInfo.supported) {
  const sdk = new OpenIMSDK({
    apiURL: config.data.apiURL,
    wsURL: config.data.wsURL,
    userID: 'user_001',
    token: userToken,
    ...config.data.chatConfig
  });
  
  await sdk.init();
}
```

### WebRTC通话示例

```javascript
// 1. 获取WebRTC配置
const webrtcConfigResponse = await fetch('/jssdk/get_webrtc_config', {
  method: 'POST',
  headers: {
    'Content-Type': 'application/json',
    'token': userToken
  },
  body: JSON.stringify({
    userID: 'user_001',
    callType: 'video',
    roomID: 'room_12345'
  })
});

const webrtcConfig = await webrtcConfigResponse.json();

// 2. 创建RTCPeerConnection
const peerConnection = new RTCPeerConnection(webrtcConfig.data.turnConfig);

// 3. 获取用户媒体
const stream = await navigator.mediaDevices.getUserMedia(
  webrtcConfig.data.mediaConstraints
);

// 4. 添加媒体流到连接
stream.getTracks().forEach(track => {
  peerConnection.addTrack(track, stream);
});
```

### Web推送配置示例

```javascript
// 1. 检查推送支持
if ('serviceWorker' in navigator && 'PushManager' in window) {
  // 2. 注册Service Worker
  const registration = await navigator.serviceWorker.register('/sw.js');
  
  // 3. 获取推送配置
  const pushConfigResponse = await fetch('/jssdk/get_push_config', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'token': userToken
    },
    body: JSON.stringify({
      userID: 'user_001'
    })
  });
  
  const pushConfig = await pushConfigResponse.json();
  
  // 4. 订阅推送
  const subscription = await registration.pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey: pushConfig.data.vapidPublicKey
  });
  
  // 5. 更新推送订阅
  await fetch('/jssdk/update_push_subscription', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'token': userToken
    },
    body: JSON.stringify({
      userID: 'user_001',
      subscription: subscription.toJSON()
    })
  });
}
```

## 浏览器支持

| 浏览器 | 最低版本 | WebRTC支持 | Push API支持 | 备注 |
|--------|----------|------------|--------------|------|
| Chrome | 60+ | ✅ | ✅ | 完整支持 |
| Firefox | 55+ | ✅ | ✅ | 完整支持 |
| Safari | 11+ | ✅ | ⚠️ | 推送API支持有限 |
| Edge | 79+ | ✅ | ✅ | 完整支持 |
| Mobile Safari | 11+ | ⚠️ | ❌ | WebRTC支持有限 |
| Chrome Mobile | 60+ | ✅ | ✅ | 完整支持 |

## 注意事项

1. **HTTPS要求**: WebRTC和Push API需要HTTPS环境
2. **权限申请**: 需要用户授权摄像头、麦克风、通知等权限
3. **浏览器兼容**: 不同浏览器对WebRTC的支持程度不同
4. **内存管理**: 及时释放媒体流和关闭连接，避免内存泄漏
5. **错误处理**: WebRTC连接可能失败，需要实现重连机制
6. **性能监控**: 监控通话质量和性能指标
7. **安全考虑**: 使用TURN服务器保护用户IP隐私
8. **跨域限制**: 注意CORS配置，确保API调用正常 