# 管理接口 (Admin)

## 概述

管理接口提供系统监控、服务发现、Prometheus指标监控等管理功能，主要用于系统维护、性能监控和运维管理。

## 接口列表

### 1. 检查健康状态
**接口地址**: `GET /healthz`

**功能描述**: 检查服务健康状态

**请求参数**: 无

**返回参数**:
```json
{
  "status": "ok",
  "timestamp": 1640995200000,
  "version": "v3.5.0",
  "services": {
    "database": "healthy",
    "redis": "healthy", 
    "messageQueue": "healthy",
    "objectStorage": "healthy"
  },
  "uptime": 86400
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| status | string | 整体状态：ok-正常，degraded-降级，error-错误 |
| timestamp | int64 | 检查时间戳（毫秒） |
| version | string | 服务版本号 |
| services | object | 各服务状态 |
| services.database | string | 数据库状态 |
| services.redis | string | Redis状态 |
| services.messageQueue | string | 消息队列状态 |
| services.objectStorage | string | 对象存储状态 |
| uptime | int64 | 服务运行时间（秒） |

---

### 2. Prometheus指标
**接口地址**: `GET /metrics`

**功能描述**: 获取Prometheus格式的监控指标

**请求参数**: 无

**返回参数**: Prometheus文本格式
```text
# HELP openim_api_requests_total Total number of API requests
# TYPE openim_api_requests_total counter
openim_api_requests_total{method="POST",endpoint="/user/get_users_info",status="200"} 12345

# HELP openim_api_request_duration_seconds API request duration in seconds
# TYPE openim_api_request_duration_seconds histogram
openim_api_request_duration_seconds_bucket{method="POST",endpoint="/user/get_users_info",le="0.1"} 8000
openim_api_request_duration_seconds_bucket{method="POST",endpoint="/user/get_users_info",le="0.5"} 10000
openim_api_request_duration_seconds_bucket{method="POST",endpoint="/user/get_users_info",le="1.0"} 12000
openim_api_request_duration_seconds_bucket{method="POST",endpoint="/user/get_users_info",le="+Inf"} 12345

# HELP openim_websocket_connections Current number of WebSocket connections
# TYPE openim_websocket_connections gauge
openim_websocket_connections 1500

# HELP openim_messages_sent_total Total number of messages sent
# TYPE openim_messages_sent_total counter
openim_messages_sent_total{type="text"} 1000000
openim_messages_sent_total{type="image"} 50000
openim_messages_sent_total{type="voice"} 30000

# HELP openim_users_online Current number of online users
# TYPE openim_users_online gauge
openim_users_online 5000

# HELP openim_database_connections Current database connections
# TYPE openim_database_connections gauge
openim_database_connections{db="mysql"} 50
openim_database_connections{db="redis"} 20

# HELP openim_storage_usage_bytes Storage usage in bytes
# TYPE openim_storage_usage_bytes gauge
openim_storage_usage_bytes{type="database"} 21474836480
openim_storage_usage_bytes{type="files"} 107374182400
```

---

### 3. 获取服务信息
**接口地址**: `POST /manager/get_client_config`

**功能描述**: 获取客户端配置信息

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
    "discoverRegisters": [
      {
        "rpcRegisterName": "openim-rpc-user",
        "rpcRegisterAddr": "127.0.0.1:10110"
      },
      {
        "rpcRegisterName": "openim-rpc-friend",
        "rpcRegisterAddr": "127.0.0.1:10120"
      },
      {
        "rpcRegisterName": "openim-rpc-group",
        "rpcRegisterAddr": "127.0.0.1:10150"
      },
      {
        "rpcRegisterName": "openim-rpc-msg",
        "rpcRegisterAddr": "127.0.0.1:10130"
      },
      {
        "rpcRegisterName": "openim-rpc-conversation",
        "rpcRegisterAddr": "127.0.0.1:10180"
      },
      {
        "rpcRegisterName": "openim-rpc-third",
        "rpcRegisterAddr": "127.0.0.1:10190"
      },
      {
        "rpcRegisterName": "openim-rpc-auth",
        "rpcRegisterAddr": "127.0.0.1:10160"
      },
      {
        "rpcRegisterName": "openim-push",
        "rpcRegisterAddr": "127.0.0.1:10170"
      },
      {
        "rpcRegisterName": "openim-msgtransfer",
        "rpcRegisterAddr": "127.0.0.1:10140"
      }
    ]
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| discoverRegisters | array | 服务注册列表 |
| discoverRegisters[].rpcRegisterName | string | RPC服务名称 |
| discoverRegisters[].rpcRegisterAddr | string | RPC服务地址 |

---

### 4. 获取用户在线Token
**接口地址**: `POST /manager/get_users_online_token_detail`

**功能描述**: 获取在线用户的Token详细信息（管理员权限）

**请求参数**:
```json
{
  "userIDs": ["user_001", "user_002"]
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| userIDs | array | 是 | 用户ID列表，最大100个 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "tokensMap": {
      "user_001": [
        {
          "userID": "user_001",
          "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
          "connID": "conn_12345",
          "platformID": 1,
          "platform": "iOS",
          "loginTime": 1640995200000,
          "expireTime": 1641081600000,
          "isOnline": true
        },
        {
          "userID": "user_001",
          "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
          "connID": "conn_67890",
          "platformID": 5,
          "platform": "Web",
          "loginTime": 1640995000000,
          "expireTime": 1641081400000,
          "isOnline": true
        }
      ],
      "user_002": [
        {
          "userID": "user_002",
          "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
          "connID": "conn_54321",
          "platformID": 2,
          "platform": "Android",
          "loginTime": 1640995100000,
          "expireTime": 1641081500000,
          "isOnline": false
        }
      ]
    }
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| tokensMap | object | 用户Token映射表 |
| tokensMap.{userID} | array | 用户的Token列表 |
| tokensMap.{userID}[].userID | string | 用户ID |
| tokensMap.{userID}[].token | string | JWT Token |
| tokensMap.{userID}[].connID | string | 连接ID |
| tokensMap.{userID}[].platformID | int32 | 平台ID |
| tokensMap.{userID}[].platform | string | 平台名称 |
| tokensMap.{userID}[].loginTime | int64 | 登录时间（毫秒时间戳） |
| tokensMap.{userID}[].expireTime | int64 | 过期时间（毫秒时间戳） |
| tokensMap.{userID}[].isOnline | bool | 是否在线 |

---

### 5. 用户强制登出
**接口地址**: `POST /manager/force_logout`

**功能描述**: 强制用户下线（管理员权限）

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
| userID | string | 是 | 要强制下线的用户ID |
| platformID | int32 | 否 | 平台ID，不指定则所有平台下线 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

---

### 6. 系统配置查询
**接口地址**: `POST /manager/get_config`

**功能描述**: 获取系统配置信息（管理员权限）

**请求参数**:
```json
{
  "configKeys": ["database", "redis", "objectStorage", "push"]
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| configKeys | array | 否 | 配置键列表，为空则返回所有配置 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "configs": {
      "database": {
        "type": "mysql",
        "host": "127.0.0.1",
        "port": 3306,
        "database": "openim_v3",
        "maxOpenConns": 100,
        "maxIdleConns": 10,
        "maxLifeTime": 3600
      },
      "redis": {
        "addr": ["127.0.0.1:6379"],
        "username": "",
        "password": "",
        "db": 0,
        "poolSize": 100
      },
      "objectStorage": {
        "enable": true,
        "type": "minio",
        "endpoint": "http://127.0.0.1:9000",
        "bucket": "openim",
        "accessKeyID": "minioadmin",
        "region": "us-east-1"
      },
      "push": {
        "fcm": {
          "enable": true,
          "serviceAccount": "/path/to/fcm-service-account.json"
        },
        "jpush": {
          "enable": false,
          "appKey": "",
          "masterSecret": ""
        }
      }
    }
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| configs | object | 配置信息对象 |
| configs.database | object | 数据库配置 |
| configs.redis | object | Redis配置 |
| configs.objectStorage | object | 对象存储配置 |
| configs.push | object | 推送服务配置 |

---

### 7. 服务状态查询
**接口地址**: `POST /manager/get_service_status`

**功能描述**: 获取各个微服务的运行状态

**请求参数**:
```json
{
  "services": ["openim-api", "openim-rpc-user", "openim-rpc-msg", "openim-push"]
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| services | array | 否 | 服务名称列表，为空则查询所有服务 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "services": [
      {
        "name": "openim-api",
        "status": "running",
        "address": "127.0.0.1:10002",
        "startTime": 1640995200000,
        "uptime": 86400,
        "version": "v3.5.0",
        "health": "healthy",
        "metrics": {
          "cpuUsage": 15.6,
          "memoryUsage": 524288000,
          "goroutines": 150,
          "connections": 1000
        }
      },
      {
        "name": "openim-rpc-user",
        "status": "running",
        "address": "127.0.0.1:10110",
        "startTime": 1640995200000,
        "uptime": 86400,
        "version": "v3.5.0",
        "health": "healthy",
        "metrics": {
          "cpuUsage": 8.2,
          "memoryUsage": 268435456,
          "goroutines": 80,
          "connections": 50
        }
      }
    ]
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| services | array | 服务状态列表 |
| services[].name | string | 服务名称 |
| services[].status | string | 运行状态：running-运行中，stopped-已停止，error-错误 |
| services[].address | string | 服务地址 |
| services[].startTime | int64 | 启动时间（毫秒时间戳） |
| services[].uptime | int64 | 运行时长（秒） |
| services[].version | string | 服务版本 |
| services[].health | string | 健康状态：healthy-健康，unhealthy-不健康 |
| services[].metrics | object | 性能指标 |
| services[].metrics.cpuUsage | float | CPU使用率（百分比） |
| services[].metrics.memoryUsage | int64 | 内存使用量（字节） |
| services[].metrics.goroutines | int32 | Goroutine数量 |
| services[].metrics.connections | int32 | 连接数 |

---

### 8. 获取系统统计信息
**接口地址**: `POST /manager/get_system_stats`

**功能描述**: 获取系统整体统计信息

**请求参数**:
```json
{
  "timeRange": "24h"
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| timeRange | string | 否 | 时间范围：1h、24h、7d、30d，默认24h |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "users": {
      "total": 100000,
      "online": 5000,
      "newRegistrations": 150
    },
    "messages": {
      "total": 50000000,
      "sent": 125000,
      "textMessages": 100000,
      "imageMessages": 15000,
      "voiceMessages": 8000,
      "videoMessages": 2000
    },
    "groups": {
      "total": 10000,
      "active": 2500,
      "newGroups": 25
    },
    "api": {
      "totalRequests": 1000000,
      "successfulRequests": 990000,
      "errorRequests": 10000,
      "averageResponseTime": 85
    },
    "storage": {
      "totalFiles": 500000,
      "totalSize": 214748364800,
      "newFiles": 1500,
      "newSize": 1073741824
    },
    "performance": {
      "averageCpuUsage": 25.5,
      "averageMemoryUsage": 68.8,
      "maxConcurrentConnections": 8000,
      "averageConnections": 5500
    }
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| users | object | 用户统计 |
| users.total | int64 | 总用户数 |
| users.online | int64 | 在线用户数 |
| users.newRegistrations | int64 | 时间范围内新注册用户数 |
| messages | object | 消息统计 |
| messages.total | int64 | 总消息数 |
| messages.sent | int64 | 时间范围内发送消息数 |
| messages.textMessages | int64 | 文本消息数 |
| messages.imageMessages | int64 | 图片消息数 |
| messages.voiceMessages | int64 | 语音消息数 |
| messages.videoMessages | int64 | 视频消息数 |
| groups | object | 群组统计 |
| groups.total | int64 | 总群组数 |
| groups.active | int64 | 活跃群组数 |
| groups.newGroups | int64 | 时间范围内新建群组数 |
| api | object | API统计 |
| api.totalRequests | int64 | 总请求数 |
| api.successfulRequests | int64 | 成功请求数 |
| api.errorRequests | int64 | 错误请求数 |
| api.averageResponseTime | int32 | 平均响应时间（毫秒） |
| storage | object | 存储统计 |
| storage.totalFiles | int64 | 总文件数 |
| storage.totalSize | int64 | 总存储大小（字节） |
| storage.newFiles | int64 | 时间范围内新增文件数 |
| storage.newSize | int64 | 时间范围内新增存储大小（字节） |
| performance | object | 性能统计 |
| performance.averageCpuUsage | float | 平均CPU使用率（百分比） |
| performance.averageMemoryUsage | float | 平均内存使用率（百分比） |
| performance.maxConcurrentConnections | int64 | 最大并发连接数 |
| performance.averageConnections | int64 | 平均连接数 |

---

### 9. 清理缓存
**接口地址**: `POST /manager/clear_cache`

**功能描述**: 清理系统缓存（管理员权限）

**请求参数**:
```json
{
  "cacheTypes": ["user", "group", "conversation", "all"]
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| cacheTypes | array | 是 | 缓存类型：user-用户缓存，group-群组缓存，conversation-会话缓存，all-所有缓存 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "clearedCaches": ["user", "group"],
    "totalKeys": 15000,
    "clearedKeys": 15000,
    "operationTime": 250
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| clearedCaches | array | 已清理的缓存类型 |
| totalKeys | int64 | 总缓存键数 |
| clearedKeys | int64 | 已清理键数 |
| operationTime | int64 | 操作耗时（毫秒） |

---

### 10. 数据库维护
**接口地址**: `POST /manager/database_maintenance`

**功能描述**: 执行数据库维护操作（管理员权限）

**请求参数**:
```json
{
  "operation": "optimize",
  "tables": ["users", "messages", "groups"]
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| operation | string | 是 | 操作类型：optimize-优化，analyze-分析，repair-修复 |
| tables | array | 否 | 表名列表，为空则操作所有表 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "operation": "optimize",
    "processedTables": ["users", "messages", "groups"],
    "results": [
      {
        "table": "users",
        "status": "OK",
        "message": "Table optimized successfully"
      },
      {
        "table": "messages",
        "status": "OK", 
        "message": "Table optimized successfully"
      },
      {
        "table": "groups",
        "status": "OK",
        "message": "Table optimized successfully"
      }
    ],
    "totalTime": 5200
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| operation | string | 执行的操作 |
| processedTables | array | 已处理的表列表 |
| results | array | 处理结果列表 |
| results[].table | string | 表名 |
| results[].status | string | 处理状态：OK-成功，ERROR-失败 |
| results[].message | string | 处理消息 |
| totalTime | int64 | 总耗时（毫秒） |

---

### 11. 系统备份
**接口地址**: `POST /manager/system_backup`

**功能描述**: 创建系统数据备份（管理员权限）

**请求参数**:
```json
{
  "backupType": "incremental",
  "includeFiles": true,
  "compression": true,
  "description": "定期备份 - 2023年1月1日"
}
```

**请求字段说明**:
| 字段名 | 类型 | 是否必填 | 说明 |
|--------|------|----------|------|
| backupType | string | 是 | 备份类型：full-全量备份，incremental-增量备份 |
| includeFiles | bool | 否 | 是否包含文件数据，默认false |
| compression | bool | 否 | 是否压缩，默认true |
| description | string | 否 | 备份描述 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {
    "backupID": "backup_20230101_001",
    "status": "in_progress",
    "startTime": 1640995200000,
    "estimatedTime": 1800000,
    "backupPath": "/backups/backup_20230101_001.tar.gz",
    "backupSize": 0
  }
}
```

**返回字段说明**:
| 字段名 | 类型 | 说明 |
|--------|------|------|
| backupID | string | 备份任务ID |
| status | string | 备份状态：in_progress-进行中，completed-完成，failed-失败 |
| startTime | int64 | 开始时间（毫秒时间戳） |
| estimatedTime | int64 | 预计耗时（毫秒） |
| backupPath | string | 备份文件路径 |
| backupSize | int64 | 备份文件大小（字节） |

## 使用示例

### 监控系统健康状态

```bash
# 1. 检查整体健康状态
curl -X GET "http://localhost:10002/healthz"

# 2. 获取Prometheus指标
curl -X GET "http://localhost:10002/metrics"

# 3. 获取详细服务状态
curl -X POST "http://localhost:10002/manager/get_service_status" \
  -H "Content-Type: application/json" \
  -H "token: admin_token_here" \
  -d '{
    "services": ["openim-api", "openim-rpc-user", "openim-rpc-msg"]
  }'

# 4. 获取系统统计信息
curl -X POST "http://localhost:10002/manager/get_system_stats" \
  -H "Content-Type: application/json" \
  -H "token: admin_token_here" \
  -d '{
    "timeRange": "24h"
  }'
```

### 用户管理操作

```bash
# 1. 查看用户在线Token详情
curl -X POST "http://localhost:10002/manager/get_users_online_token_detail" \
  -H "Content-Type: application/json" \
  -H "token: admin_token_here" \
  -d '{
    "userIDs": ["user_001", "user_002"]
  }'

# 2. 强制用户下线
curl -X POST "http://localhost:10002/manager/force_logout" \
  -H "Content-Type: application/json" \
  -H "token: admin_token_here" \
  -d '{
    "userID": "user_001",
    "platformID": 1
  }'
```

### 系统维护操作

```bash
# 1. 清理缓存
curl -X POST "http://localhost:10002/manager/clear_cache" \
  -H "Content-Type: application/json" \
  -H "token: admin_token_here" \
  -d '{
    "cacheTypes": ["user", "group"]
  }'

# 2. 数据库优化
curl -X POST "http://localhost:10002/manager/database_maintenance" \
  -H "Content-Type: application/json" \
  -H "token: admin_token_here" \
  -d '{
    "operation": "optimize",
    "tables": ["messages", "users"]
  }'

# 3. 创建系统备份
curl -X POST "http://localhost:10002/manager/system_backup" \
  -H "Content-Type: application/json" \
  -H "token: admin_token_here" \
  -d '{
    "backupType": "full",
    "includeFiles": true,
    "compression": true,
    "description": "月度全量备份"
  }'
```

## Prometheus监控指标说明

### 核心业务指标

| 指标名称 | 类型 | 说明 |
|----------|------|------|
| openim_users_total | Gauge | 总用户数 |
| openim_users_online | Gauge | 在线用户数 |
| openim_messages_sent_total | Counter | 发送消息总数 |
| openim_groups_total | Gauge | 群组总数 |
| openim_conversations_total | Gauge | 会话总数 |

### 系统性能指标

| 指标名称 | 类型 | 说明 |
|----------|------|------|
| openim_api_requests_total | Counter | API请求总数 |
| openim_api_request_duration_seconds | Histogram | API请求耗时 |
| openim_websocket_connections | Gauge | WebSocket连接数 |
| openim_database_connections | Gauge | 数据库连接数 |
| openim_storage_usage_bytes | Gauge | 存储使用量 |

### 错误监控指标

| 指标名称 | 类型 | 说明 |
|----------|------|------|
| openim_api_errors_total | Counter | API错误总数 |
| openim_database_errors_total | Counter | 数据库错误总数 |
| openim_redis_errors_total | Counter | Redis错误总数 |
| openim_push_failures_total | Counter | 推送失败总数 |

## 权限级别

| 接口 | 权限要求 | 说明 |
|------|----------|------|
| /healthz | 无 | 公开接口 |
| /metrics | 无 | 公开接口，建议内网访问 |
| /manager/* | 管理员 | 需要管理员Token |
| 强制下线 | 超级管理员 | 需要最高权限 |
| 系统备份 | 超级管理员 | 需要最高权限 |

## 注意事项

1. **权限控制**: 管理接口需要严格的权限验证，避免未授权访问
2. **监控告警**: 配置Prometheus告警规则，及时发现系统异常
3. **性能影响**: 部分操作可能影响系统性能，建议在低峰期执行
4. **备份策略**: 定期备份重要数据，制定灾难恢复计划
5. **日志审计**: 记录所有管理操作，便于追踪和审计
6. **网络安全**: 管理接口建议仅限内网访问
7. **资源监控**: 密切关注CPU、内存、磁盘等资源使用情况
8. **版本兼容**: 升级时注意API兼容性问题 