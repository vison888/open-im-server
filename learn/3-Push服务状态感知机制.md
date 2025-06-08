# Push服务状态感知机制分析

## 概述

Push服务状态感知机制是OpenIM推送系统的核心组件，负责感知用户设备的在线状态变化，并基于这些状态信息做出智能的推送决策。该机制通过全量状态加载和实时Redis队列订阅相结合的方式，确保Push服务能够准确掌握所有用户的在线状态。

## 核心架构组件

### 1. RPC在线状态缓存 (rpccache/online.go)
提供用户在线状态的本地缓存能力，支持全量缓存和LRU缓存两种模式。

### 2. Redis状态订阅
实时监听Redis发布的状态变更消息，保持本地缓存与全局状态的一致性。

### 3. 智能推送决策引擎
基于用户在线状态信息，决定是否需要进行离线推送。

## 详细流程分析

### 第一阶段：Push服务初始化

#### 1.1 缓存策略选择

```go
// rpccache/online.go: NewOnlineCache方法
func NewOnlineCache(client *rpcli.UserClient, group *GroupLocalCache, rdb redis.UniversalClient, fullUserCache bool, fn func(ctx context.Context, userID string, platformIDs []int32)) (*OnlineCache, error) {
    switch x.fullUserCache {
    case true:
        // 全量缓存模式：缓存所有用户状态，查询快但内存消耗大
        log.ZDebug(ctx, "fullUserCache is true")
        x.mapCache = cacheutil.NewCache[string, []int32]()
        // 异步初始化全量用户在线状态
        go func() {
            if err := x.initUsersOnlineStatus(ctx); err != nil {
                log.ZError(ctx, "initUsersOnlineStatus failed", err)
            }
        }()
    case false:
        // LRU缓存模式：只缓存热点用户，内存友好但可能有缓存miss
        log.ZDebug(ctx, "fullUserCache is false")
        // 创建1024个分片的LRU缓存，每个分片容量2048
        x.lruCache = lru.NewSlotLRU(1024, localcache.LRUStringHash, func() lru.LRU[string, []int32] {
            return lru.NewLayLRU[string, []int32](2048, cachekey.OnlineExpire/2, time.Second*3, localcache.EmptyTarget{}, func(key string, value []int32) {})
        })
        // LRU模式下直接进入订阅阶段，无需全量初始化
        x.CurrentPhase.Store(DoSubscribeOver)
        x.Cond.Broadcast()
    }
}
```

**缓存策略对比：**

| 特性 | 全量缓存模式 | LRU缓存模式 |
|------|-------------|-------------|
| 内存使用 | 线性增长，消耗大 | 固定上限，可控 |
| 查询性能 | 恒定O(1) | 缓存命中O(1)，miss需RPC |
| 启动时间 | 需全量加载，较慢 | 立即可用 |
| 适用场景 | 小规模用户，查询频繁 | 大规模用户，推送服务 |

#### 1.2 三阶段初始化机制

```go
// 初始化阶段常量定义
const (
    Begin              uint32 = iota // 开始阶段
    DoOnlineStatusOver               // 在线状态初始化完成
    DoSubscribeOver                  // 订阅初始化完成
)

type OnlineCache struct {
    Lock         *sync.Mutex    // 保护条件变量的互斥锁
    Cond         *sync.Cond     // 用于阶段同步的条件变量
    CurrentPhase atomic.Uint32  // 当前初始化阶段，使用原子操作确保线程安全
}
```

**阶段同步机制：**
1. **Begin阶段**：服务刚启动，开始初始化
2. **DoOnlineStatusOver阶段**：全量状态加载完成（仅全量缓存模式）
3. **DoSubscribeOver阶段**：Redis订阅建立完成，可以正常提供服务

### 第二阶段：全量状态加载（全量缓存模式）

#### 2.1 分页获取所有在线用户

```go
// rpccache/online.go: initUsersOnlineStatus方法
func (o *OnlineCache) initUsersOnlineStatus(ctx context.Context) (err error) {
    var (
        totalSet      atomic.Int64         // 原子计数器，统计处理的用户总数
        maxTries      = 5                  // 最大重试次数
        retryInterval = time.Second * 5    // 重试间隔
    )

    // 重试机制封装，提高网络调用的可靠性
    retryOperation := func(operation func() error, operationName string) error {
        for i := 0; i < maxTries; i++ {
            if err = operation(); err != nil {
                log.ZWarn(ctx, fmt.Sprintf("initUsersOnlineStatus: %s failed", operationName), err)
                time.Sleep(retryInterval)
            } else {
                return nil
            }
        }
        return err
    }

    // 分页获取所有在线用户，cursor为分页游标
    cursor := uint64(0)
    for resp == nil || resp.NextCursor != 0 {
        if err = retryOperation(func() error {
            // 调用RPC获取一页在线用户数据
            resp, err = o.client.GetAllOnlineUsers(ctx, cursor)
            if err != nil {
                return err
            }

            // 处理当前页的用户状态
            for _, u := range resp.StatusList {
                if u.Status == constant.Online {
                    // 只缓存在线用户，离线用户不占用内存
                    o.setUserOnline(u.UserID, u.PlatformIDs)
                }
                totalSet.Add(1) // 统计处理数量
            }
            cursor = resp.NextCursor // 更新游标到下一页
            return nil
        }, "getAllOnlineUsers"); err != nil {
            return err
        }
    }
}
```

#### 2.2 状态缓存存储

```go
// setUserOnline 设置用户在线状态（内部方法）
func (o *OnlineCache) setUserOnline(userID string, platformIDs []int32) {
    switch o.fullUserCache {
    case true:
        // 全量缓存：直接存储到并发安全的Map中
        o.mapCache.Store(userID, platformIDs)
    case false:
        // LRU缓存：存储到分片LRU缓存中
        o.lruCache.Set(userID, platformIDs)
    }
}
```

**全量加载优势：**
1. **启动后即时可用**：所有查询都能从本地缓存命中
2. **网络开销最小**：启动后无需额外的RPC调用
3. **查询性能恒定**：O(1)的查询时间复杂度

**全量加载问题：**
1. **内存消耗巨大**：用户数量线性增长的内存占用
2. **启动时间长**：需要遍历所有用户才能提供服务
3. **网络依赖强**：启动失败会导致服务不可用

### 第三阶段：Redis状态订阅

#### 3.1 订阅建立与同步

```go
// rpccache/online.go: doSubscribe方法
func (o *OnlineCache) doSubscribe(ctx context.Context, rdb redis.UniversalClient, fn func(ctx context.Context, userID string, platformIDs []int32)) {
    o.Lock.Lock()
    // 订阅Redis在线状态变更频道
    ch := rdb.Subscribe(ctx, cachekey.OnlineChannel).Channel()
    // 等待在线状态初始化完成
    for o.CurrentPhase.Load() < DoOnlineStatusOver {
        o.Cond.Wait()
    }
    o.Lock.Unlock()
    
    log.ZInfo(ctx, "begin doSubscribe")
}
```

#### 3.2 消息处理机制

```go
// 消息处理函数，根据缓存策略选择不同的处理逻辑
doMessage := func(message *redis.Message) {
    // 解析Redis消息，提取用户ID和平台ID列表
    userID, platformIDs, err := useronline.ParseUserOnlineStatus(message.Payload)
    if err != nil {
        log.ZError(ctx, "OnlineCache setHasUserOnline redis subscribe parseUserOnlineStatus", err, "payload", message.Payload, "channel", message.Channel)
        return
    }
    
    switch o.fullUserCache {
    case true:
        // 全量缓存模式：直接更新mapCache
        if len(platformIDs) == 0 {
            // 平台列表为空表示用户离线，删除缓存记录
            o.mapCache.Delete(userID)
        } else {
            // 更新用户在线状态
            o.mapCache.Store(userID, platformIDs)
        }
    case false:
        // LRU缓存模式：更新lruCache并调用回调函数
        storageCache := o.setHasUserOnline(userID, platformIDs)
        if fn != nil {
            fn(ctx, userID, platformIDs) // 执行外部回调，用于推送决策
        }
    }
}
```

#### 3.3 积压消息处理

```go
// 处理初始化完成后的积压消息
if o.CurrentPhase.Load() == DoOnlineStatusOver {
    for done := false; !done; {
        select {
        case message := <-ch:
            doMessage(message)
        default:
            // 没有积压消息，切换到订阅完成阶段
            o.CurrentPhase.Store(DoSubscribeOver)
            o.Cond.Broadcast()
            done = true
        }
    }
}

// 进入正常的消息处理循环
for message := range ch {
    doMessage(message)
}
```

**订阅机制特点：**
1. **阶段化处理**：确保全量加载完成后再处理实时消息
2. **积压消息处理**：防止初始化期间的消息丢失
3. **无阻塞设计**：使用select非阻塞处理积压消息

### 第四阶段：状态查询与推送决策

#### 4.1 单用户状态查询

```go
// GetUserOnline 判断用户是否在线
func (o *OnlineCache) GetUserOnline(ctx context.Context, userID string) (bool, error) {
    platformIDs, err := o.getUserOnlinePlatform(ctx, userID)
    if err != nil {
        return false, err
    }
    return len(platformIDs) > 0, nil
}

// getUserOnlinePlatform 获取用户在线平台列表（内部方法）
func (o *OnlineCache) getUserOnlinePlatform(ctx context.Context, userID string) ([]int32, error) {
    platformIDs, err := o.lruCache.Get(userID, func() ([]int32, error) {
        // 缓存miss时的回调函数，调用RPC获取数据
        return o.client.GetUserOnlinePlatform(ctx, userID)
    })
    if err != nil {
        log.ZError(ctx, "OnlineCache GetUserOnlinePlatform", err, "userID", userID)
        return nil, err
    }
    return platformIDs, nil
}
```

#### 4.2 批量用户状态查询

```go
// GetUsersOnline 批量获取用户在线状态，返回在线和离线用户列表
func (o *OnlineCache) GetUsersOnline(ctx context.Context, userIDs []string) ([]string, []string, error) {
    var (
        onlineUserIDs  = make([]string, 0, len(userIDs))  // 在线用户列表
        offlineUserIDs = make([]string, 0, len(userIDs)) // 离线用户列表
    )

    switch o.fullUserCache {
    case true:
        // 全量缓存模式：直接查询本地缓存，无网络开销
        for _, userID := range userIDs {
            if _, ok := o.mapCache.Load(userID); ok {
                onlineUserIDs = append(onlineUserIDs, userID)
            } else {
                offlineUserIDs = append(offlineUserIDs, userID)
            }
        }
    case false:
        // LRU缓存模式：可能需要RPC调用
        userOnlineMap, err := o.getUserOnlinePlatformBatch(ctx, userIDs)
        if err != nil {
            return nil, nil, err
        }

        // 根据平台列表判断在线状态
        for key, value := range userOnlineMap {
            if len(value) > 0 {
                onlineUserIDs = append(onlineUserIDs, key)
            } else {
                offlineUserIDs = append(offlineUserIDs, key)
            }
        }
    }

    log.ZInfo(ctx, "get users online", "online users length", len(onlineUserIDs), "offline users length", len(offlineUserIDs), "cost", time.Since(t))
    return onlineUserIDs, offlineUserIDs, nil
}
```

#### 4.3 推送决策逻辑

基于查询到的用户在线状态，Push服务可以做出智能的推送决策：

```go
// 伪代码：推送决策示例
func (p *PushService) decidePushStrategy(userID string, message *Message) error {
    // 查询用户在线状态
    isOnline, err := p.onlineCache.GetUserOnline(ctx, userID)
    if err != nil {
        return err
    }

    if isOnline {
        // 用户在线，通过WebSocket实时推送
        return p.sendRealtimePush(userID, message)
    } else {
        // 用户离线，发送离线推送通知
        return p.sendOfflinePush(userID, message)
    }
}

// 批量推送决策
func (p *PushService) batchPushDecision(userIDs []string, message *Message) error {
    onlineUsers, offlineUsers, err := p.onlineCache.GetUsersOnline(ctx, userIDs)
    if err != nil {
        return err
    }

    // 并发处理在线和离线用户
    var wg sync.WaitGroup
    
    // 处理在线用户
    wg.Add(1)
    go func() {
        defer wg.Done()
        p.batchRealtimePush(onlineUsers, message)
    }()

    // 处理离线用户
    wg.Add(1)
    go func() {
        defer wg.Done()
        p.batchOfflinePush(offlineUsers, message)
    }()

    wg.Wait()
    return nil
}
```

### 第五阶段：缓存优化与管理

#### 5.1 LRU缓存分片设计

```go
// 创建1024个分片的LRU缓存，每个分片容量2048
x.lruCache = lru.NewSlotLRU(1024, localcache.LRUStringHash, func() lru.LRU[string, []int32] {
    return lru.NewLayLRU[string, []int32](
        2048,                    // 每个分片的容量
        cachekey.OnlineExpire/2, // 缓存TTL
        time.Second*3,           // 清理间隔
        localcache.EmptyTarget{}, 
        func(key string, value []int32) {}, // 淘汰回调
    )
})
```

**分片优势：**
1. **减少锁竞争**：每个分片独立加锁，提高并发性能
2. **内存局部性**：相关数据聚集在同一分片中
3. **负载均衡**：哈希分片均匀分布访问压力

#### 5.2 批量查询优化

```go
// getUserOnlinePlatformBatch 批量获取用户在线平台（内部方法）
func (o *OnlineCache) getUserOnlinePlatformBatch(ctx context.Context, userIDs []string) (map[string][]int32, error) {
    platformIDsMap, err := o.lruCache.GetBatch(userIDs, func(missingUsers []string) (map[string][]int32, error) {
        platformIDsMap := make(map[string][]int32)
        // 批量调用RPC获取缺失的用户状态
        usersStatus, err := o.client.GetUsersOnlinePlatform(ctx, missingUsers)
        if err != nil {
            return nil, err
        }

        // 构建返回映射
        for _, u := range usersStatus {
            platformIDsMap[u.UserID] = u.PlatformIDs
        }
        return platformIDsMap, nil
    })
    return platformIDsMap, nil
}
```

**批量优化效果：**
1. **减少网络往返**：一次RPC调用获取多个用户状态
2. **提高缓存效率**：批量填充缓存，提升命中率
3. **降低延迟**：减少总的查询时间

### 第六阶段：状态变更回调处理

#### 6.1 回调函数设计

```go
// Push服务可以注册回调函数，实时感知状态变更
func newOnlineCacheWithCallback() *OnlineCache {
    return NewOnlineCache(
        userClient,
        groupCache,
        redisClient,
        false, // 使用LRU缓存模式
        func(ctx context.Context, userID string, platformIDs []int32) {
            // 状态变更回调处理
            log.ZDebug(ctx, "user online status changed", "userID", userID, "platforms", platformIDs)
            
            // 可以在这里实现：
            // 1. 推送策略调整
            // 2. 离线消息处理
            // 3. 用户活跃度统计
            // 4. 实时通知其他服务
        },
    )
}
```

#### 6.2 状态变更事件处理

```go
// 状态变更处理示例
func (p *PushService) handleUserStatusChange(ctx context.Context, userID string, platformIDs []int32) {
    if len(platformIDs) > 0 {
        // 用户上线处理
        p.handleUserOnline(ctx, userID, platformIDs)
    } else {
        // 用户下线处理
        p.handleUserOffline(ctx, userID)
    }
}

func (p *PushService) handleUserOnline(ctx context.Context, userID string, platformIDs []int32) {
    // 1. 检查是否有离线消息需要推送
    offlineMessages, err := p.getOfflineMessages(userID)
    if err != nil {
        log.ZError(ctx, "get offline messages failed", err)
        return
    }

    // 2. 推送离线消息
    for _, msg := range offlineMessages {
        if err := p.sendRealtimePush(userID, msg); err != nil {
            log.ZError(ctx, "send offline message failed", err)
        }
    }

    // 3. 清理已推送的离线消息
    p.clearOfflineMessages(userID)
}

func (p *PushService) handleUserOffline(ctx context.Context, userID string) {
    // 1. 记录用户离线时间
    p.recordUserOfflineTime(userID, time.Now())

    // 2. 可以预处理一些离线推送策略
    p.prepareOfflinePushStrategy(userID)
}
```

## 关键设计特点

### 1. 双模式缓存策略
- **全量缓存**：适合小规模、高频查询场景
- **LRU缓存**：适合大规模、推送服务场景
- **灵活切换**：配置驱动的策略选择

### 2. 三阶段初始化机制
- **有序启动**：确保数据完整性
- **同步控制**：条件变量协调多个协程
- **状态可见**：原子操作保证状态一致性

### 3. 实时状态同步
- **Redis订阅**：实时感知状态变更
- **消息处理**：支持批量和增量更新
- **回调机制**：支持业务逻辑扩展

### 4. 性能优化设计
- **分片缓存**：减少锁竞争，提高并发
- **批量操作**：减少网络开销
- **内存管理**：TTL和容量控制，防止内存泄漏

### 5. 容错与监控
- **重试机制**：网络异常的自动恢复
- **降级策略**：缓存失效时的RPC兜底
- **详细日志**：便于问题排查和性能分析

## 推送决策优化

### 1. 智能推送策略
```go
type PushStrategy struct {
    RealtimePush bool // 是否实时推送
    OfflinePush  bool // 是否离线推送
    Delay        time.Duration // 延迟推送时间
    Priority     int  // 推送优先级
}

func (p *PushService) getOptimalStrategy(userID string, messageType int) *PushStrategy {
    isOnline, _ := p.onlineCache.GetUserOnline(ctx, userID)
    
    switch messageType {
    case MessageTypeUrgent:
        return &PushStrategy{
            RealtimePush: isOnline,
            OfflinePush:  !isOnline,
            Delay:        0,
            Priority:     10,
        }
    case MessageTypeNormal:
        return &PushStrategy{
            RealtimePush: isOnline,
            OfflinePush:  !isOnline,
            Delay:        time.Minute * 5,
            Priority:     5,
        }
    }
}
```

### 2. 批量推送优化
- **状态预过滤**：先过滤在线用户，减少无效推送
- **分组处理**：按在线状态分组，并行处理
- **资源控制**：限制并发推送数量，避免资源耗尽

这个Push服务状态感知机制通过精心设计的缓存策略、状态同步和推送决策，实现了高效、可靠的用户状态感知能力，为OpenIM的推送系统提供了坚实的技术基础。 