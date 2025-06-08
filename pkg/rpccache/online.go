package rpccache

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/rpcli"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/user"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache/cachekey"
	"github.com/openimsdk/open-im-server/v3/pkg/localcache"
	"github.com/openimsdk/open-im-server/v3/pkg/localcache/lru"
	"github.com/openimsdk/open-im-server/v3/pkg/util/useronline"
	"github.com/openimsdk/tools/db/cacheutil"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
	"github.com/redis/go-redis/v9"
)

// NewOnlineCache 创建在线状态缓存实例
// 设计思路：
// 1. 在RPC调用之上增加本地缓存层，减少网络开销
// 2. 支持两种缓存策略：全量缓存(mapCache)和LRU缓存(lruCache)
// 3. 通过Redis发布订阅机制实现分布式缓存同步
// 4. 采用三阶段初始化确保数据一致性
//
// 缓存策略对比：
// - fullUserCache=true: 缓存所有用户状态，查询快但内存消耗大
// - fullUserCache=false: 只缓存热点用户，内存友好但可能有缓存miss
//
// 参数：
//   - client: 用户服务RPC客户端
//   - group: 群组本地缓存
//   - rdb: Redis客户端
//   - fullUserCache: 是否启用全量用户缓存
//   - fn: 状态变更回调函数
//
// 返回值：
//   - *OnlineCache: 在线状态缓存实例
//   - error: 错误信息
func NewOnlineCache(client *rpcli.UserClient, group *GroupLocalCache, rdb redis.UniversalClient, fullUserCache bool, fn func(ctx context.Context, userID string, platformIDs []int32)) (*OnlineCache, error) {
	l := &sync.Mutex{}
	x := &OnlineCache{
		client:        client,
		group:         group,
		fullUserCache: fullUserCache,
		Lock:          l,
		Cond:          sync.NewCond(l), // 用于阶段同步的条件变量
	}

	// 生成唯一的操作ID用于日志追踪
	ctx := mcontext.SetOperationID(context.TODO(), strconv.FormatInt(time.Now().UnixNano()+int64(rand.Uint32()), 10))

	// 根据缓存策略初始化不同的缓存实现
	switch x.fullUserCache {
	case true:
		// 全量缓存模式：使用并发安全的Map缓存所有用户状态
		log.ZDebug(ctx, "fullUserCache is true")
		x.mapCache = cacheutil.NewCache[string, []int32]()
		// 异步初始化全量用户在线状态
		go func() {
			if err := x.initUsersOnlineStatus(ctx); err != nil {
				log.ZError(ctx, "initUsersOnlineStatus failed", err)
			}
		}()
	case false:
		// LRU缓存模式：使用分片LRU缓存热点用户状态
		log.ZDebug(ctx, "fullUserCache is false")
		// 创建1024个分片的LRU缓存，每个分片容量2048
		// 缓存有效期为OnlineExpire/2，清理间隔3秒
		x.lruCache = lru.NewSlotLRU(1024, localcache.LRUStringHash, func() lru.LRU[string, []int32] {
			return lru.NewLayLRU[string, []int32](2048, cachekey.OnlineExpire/2, time.Second*3, localcache.EmptyTarget{}, func(key string, value []int32) {})
		})
		// LRU模式下直接进入订阅阶段，无需全量初始化
		x.CurrentPhase.Store(DoSubscribeOver)
		x.Cond.Broadcast()
	}

	// 启动Redis订阅协程，监听状态变更消息
	go func() {
		x.doSubscribe(ctx, rdb, fn)
	}()
	return x, nil
}

// 初始化阶段常量定义
// 设计思路：使用原子操作确保阶段切换的线程安全性
const (
	Begin              uint32 = iota // 开始阶段
	DoOnlineStatusOver               // 在线状态初始化完成
	DoSubscribeOver                  // 订阅初始化完成
)

// OnlineCache 在线状态RPC缓存结构
// 架构设计：
// 1. 在RPC调用层之上增加本地缓存，减少网络延迟
// 2. 通过Redis发布订阅实现多实例间的缓存同步
// 3. 支持灵活的缓存策略切换，适应不同规模的业务场景
type OnlineCache struct {
	client *rpcli.UserClient // 用户服务RPC客户端
	group  *GroupLocalCache  // 群组本地缓存引用

	// fullUserCache 缓存策略标志
	// true: 使用mapCache缓存所有用户的在线状态
	// false: 使用lruCache只缓存部分用户的在线状态（无论是否在线）
	fullUserCache bool

	lruCache lru.LRU[string, []int32]          // LRU缓存实现，用于热点数据缓存
	mapCache *cacheutil.Cache[string, []int32] // 全量缓存实现，用于缓存所有用户状态

	Lock         *sync.Mutex   // 保护条件变量的互斥锁
	Cond         *sync.Cond    // 用于阶段同步的条件变量
	CurrentPhase atomic.Uint32 // 当前初始化阶段，使用原子操作确保线程安全
}

// initUsersOnlineStatus 初始化所有用户的在线状态（仅全量缓存模式）
// 设计思路：
// 1. 分页获取所有在线用户，避免一次性加载过多数据
// 2. 使用重试机制应对网络异常，提高初始化成功率
// 3. 原子计数器统计处理数量，便于监控和调试
// 4. 完成后切换阶段并通知等待的协程
//
// 潜在问题：
// - 初始化时间可能很长，影响服务启动速度
// - 网络异常时可能导致数据不完整
// - 内存消耗随用户数量线性增长
func (o *OnlineCache) initUsersOnlineStatus(ctx context.Context) (err error) {
	log.ZDebug(ctx, "init users online status begin")

	var (
		totalSet      atomic.Int64      // 原子计数器，统计处理的用户总数
		maxTries      = 5               // 最大重试次数
		retryInterval = time.Second * 5 // 重试间隔

		resp *user.GetAllOnlineUsersResp // RPC响应对象
	)

	// 使用defer确保阶段切换和性能统计
	defer func(t time.Time) {
		log.ZInfo(ctx, "init users online status end", "cost", time.Since(t), "totalSet", totalSet.Load())
		o.CurrentPhase.Store(DoOnlineStatusOver) // 切换到下一阶段
		o.Cond.Broadcast()                       // 通知等待的协程
	}(time.Now())

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

	return nil
}

// doSubscribe 执行Redis订阅，监听在线状态变更消息
// 设计思路：
// 1. 等待初始化阶段完成后再开始处理订阅消息，确保数据一致性
// 2. 先处理积压的消息，再进入正常的消息处理循环
// 3. 根据缓存策略选择不同的消息处理逻辑
// 4. 支持外部回调函数，便于扩展和监控
//
// 同步流程：
// 1. 订阅Redis频道
// 2. 等待初始化完成
// 3. 处理积压消息
// 4. 进入正常消息循环
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

	// 消息处理函数，根据缓存策略选择不同的处理逻辑
	doMessage := func(message *redis.Message) {
		// 解析Redis消息，提取用户ID和平台ID列表
		userID, platformIDs, err := useronline.ParseUserOnlineStatus(message.Payload)
		if err != nil {
			log.ZError(ctx, "OnlineCache setHasUserOnline redis subscribe parseUserOnlineStatus", err, "payload", message.Payload, "channel", message.Channel)
			return
		}
		log.ZDebug(ctx, fmt.Sprintf("get subscribe %s message", cachekey.OnlineChannel), "useID", userID, "platformIDs", platformIDs)

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
			log.ZDebug(ctx, "OnlineCache setHasUserOnline", "userID", userID, "platformIDs", platformIDs, "payload", message.Payload, "storageCache", storageCache)
			if fn != nil {
				fn(ctx, userID, platformIDs) // 执行外部回调
			}
		}
	}

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
}

// getUserOnlinePlatform 获取用户在线平台列表（内部方法）
// 设计思路：
// 1. 优先从LRU缓存获取，缓存miss时调用RPC
// 2. 使用LRU的Get方法，自动处理缓存逻辑
// 3. 记录详细的错误日志便于问题排查
func (o *OnlineCache) getUserOnlinePlatform(ctx context.Context, userID string) ([]int32, error) {
	platformIDs, err := o.lruCache.Get(userID, func() ([]int32, error) {
		// 缓存miss时的回调函数，调用RPC获取数据
		return o.client.GetUserOnlinePlatform(ctx, userID)
	})
	if err != nil {
		log.ZError(ctx, "OnlineCache GetUserOnlinePlatform", err, "userID", userID)
		return nil, err
	}
	//log.ZDebug(ctx, "OnlineCache GetUserOnlinePlatform", "userID", userID, "platformIDs", platformIDs)
	return platformIDs, nil
}

// GetUserOnlinePlatform 获取用户在线平台列表（对外接口）
// 设计思路：
// 1. 返回平台ID的副本，避免调用方修改影响缓存数据
// 2. 统一的错误处理和日志记录
func (o *OnlineCache) GetUserOnlinePlatform(ctx context.Context, userID string) ([]int32, error) {
	platformIDs, err := o.getUserOnlinePlatform(ctx, userID)
	if err != nil {
		return nil, err
	}
	// 创建副本避免外部修改影响缓存
	tmp := make([]int32, len(platformIDs))
	copy(tmp, platformIDs)
	return platformIDs, nil
}

// GetUserOnline 判断用户是否在线
// 设计思路：
// 1. 基于平台列表长度判断在线状态
// 2. 复用getUserOnlinePlatform的缓存逻辑
func (o *OnlineCache) GetUserOnline(ctx context.Context, userID string) (bool, error) {
	platformIDs, err := o.getUserOnlinePlatform(ctx, userID)
	if err != nil {
		return false, err
	}
	return len(platformIDs) > 0, nil
}

// getUserOnlinePlatformBatch 批量获取用户在线平台（内部方法）
// 设计思路：
// 1. 使用LRU的批量接口，自动处理缓存hit/miss
// 2. 批量RPC调用减少网络往返次数
// 3. 返回完整的用户ID到平台列表的映射
func (o *OnlineCache) getUserOnlinePlatformBatch(ctx context.Context, userIDs []string) (map[string][]int32, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}

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
	if err != nil {
		log.ZError(ctx, "OnlineCache GetUserOnlinePlatform", err, "userID", userIDs)
		return nil, err
	}
	return platformIDsMap, nil
}

// GetUsersOnline 批量获取用户在线状态，返回在线和离线用户列表
// 设计思路：
// 1. 根据缓存策略选择不同的查询路径
// 2. 全量缓存模式：直接查询mapCache，性能最优
// 3. LRU缓存模式：使用批量查询，平衡性能和内存
// 4. 返回分离的在线/离线用户列表，便于业务处理
func (o *OnlineCache) GetUsersOnline(ctx context.Context, userIDs []string) ([]string, []string, error) {
	t := time.Now()

	var (
		onlineUserIDs  = make([]string, 0, len(userIDs)) // 在线用户列表
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

// setUserOnline 设置用户在线状态（内部方法）
// 设计思路：根据缓存策略选择对应的存储方式
func (o *OnlineCache) setUserOnline(userID string, platformIDs []int32) {
	switch o.fullUserCache {
	case true:
		o.mapCache.Store(userID, platformIDs)
	case false:
		o.lruCache.Set(userID, platformIDs)
	}
}

// setHasUserOnline 设置用户在线状态并返回是否存储到缓存
// 设计思路：
// 1. 使用LRU的SetHas方法，返回是否实际存储
// 2. 便于调试和监控缓存命中率
func (o *OnlineCache) setHasUserOnline(userID string, platformIDs []int32) bool {
	return o.lruCache.SetHas(userID, platformIDs)
}
