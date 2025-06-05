# 认证相关接口 (Auth)

## 概述

认证接口用于管理系统的身份验证和授权，包括管理员和普通用户的Token获取、解析和登出等功能。

## 接口列表

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

**错误码**:
- `1002`: 权限不足 - Secret无效或用户ID不在管理员列表中
- `1003`: 用户不存在 - 指定的用户ID在系统中不存在

---

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

**错误码**:
- `1002`: 权限不足 - 非管理员调用或尝试获取管理员Token
- `1003`: 用户不存在 - 指定的用户ID不存在
- `1001`: 参数错误 - platformID为管理员平台ID

---

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

**错误码**:
- `1005`: Token无效 - Token格式错误、已过期或已被踢下线
- `1001`: 参数错误 - Token参数为空

---

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
| platformID | int32 | 是 | 要登出的平台ID，0表示所有平台 |

**返回参数**:
```json
{
  "errCode": 0,
  "errMsg": "success",
  "data": {}
}
```

**错误码**:
- `1002`: 权限不足 - 非管理员调用

## 使用示例

### 获取管理员Token示例

```bash
curl -X POST "http://localhost:10002/auth/get_admin_token" \
  -H "Content-Type: application/json" \
  -d '{
    "userID": "imAdmin",
    "secret": "openIM123"
  }'
```

### 获取用户Token示例

```bash
curl -X POST "http://localhost:10002/auth/get_user_token" \
  -H "Content-Type: application/json" \
  -H "token: admin_token_here" \
  -d '{
    "userID": "user123",
    "platformID": 1
  }'
```

## 多端登录策略

系统支持多种登录策略，通过配置文件中的 `multiLogin` 参数控制：

1. **允许多端同时在线**: 同一用户可在多个平台同时登录
2. **单端登录**: 同一用户同时只能在一个平台登录
3. **同类平台互踢**: 同类平台（如手机端）只能有一个在线

## Token管理

### Token格式
系统使用JWT (JSON Web Token) 格式的Token，包含以下信息：
- `userID`: 用户ID
- `platformID`: 平台ID  
- `exp`: 过期时间
- `iat`: 签发时间

### Token状态
- `NormalToken`: 正常状态
- `KickedToken`: 已被踢下线状态

### 安全注意事项
1. Token应安全存储，避免泄露
2. 管理员Token权限较高，使用时需格外注意
3. 定期检查Token的有效性
4. 生产环境中应使用强密码作为Secret 