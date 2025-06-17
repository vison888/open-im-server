package redis

import (
	"context"
	"encoding/json"
	"time"
	"unsafe"

	"github.com/dtm-labs/rockscache"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
)

// getRocksCacheRedisClient 通过unsafe指针转换获取rockscache内部的redis客户端
// 这是一个hack方法，用于访问rockscache.Client的私有字段rdb
// 目的：获取底层的redis.UniversalClient，以便进行Redis集群槽位相关操作
func getRocksCacheRedisClient(cli *rockscache.Client) redis.UniversalClient {
	// 定义与rockscache.Client相同内存布局的结构体
	type Client struct {
		rdb redis.UniversalClient // 第一个字段：Redis客户端
		_   rockscache.Options    // 第二个字段：配置选项（占位符）
		_   singleflight.Group    // 第三个字段：单飞组（占位符）
	}
	// 通过unsafe.Pointer进行类型转换，获取内部的redis客户端
	return (*Client)(unsafe.Pointer(cli)).rdb
}

// batchGetCache2 批量获取缓存数据的核心函数
//
// **整体处理流程图：**
//
//	输入: ids []K (业务ID列表)
//	  ↓
//	┌─────────────────────────────────────────────────────────────┐
//	│ 第一阶段：输入验证和预处理                                    │
//	│ • 边界条件检查 (len(ids) == 0)                              │
//	│ • 键去重: ids → findKeys (去重的缓存键)                     │
//	│ • 映射构建: keyId[缓存键] = 业务ID                           │
//	└─────────────────────────────────────────────────────────────┘
//	  ↓
//	┌─────────────────────────────────────────────────────────────┐
//	│ 第二阶段：Redis集群槽位优化                                   │
//	│ • groupKeysBySlot(): 按槽位分组键                           │
//	│ • slotKeys[槽位号] = []缓存键                               │
//	│ • 优化目标: 同槽位键在同节点批量处理                         │
//	└─────────────────────────────────────────────────────────────┘
//	  ↓
//	┌─────────────────────────────────────────────────────────────┐
//	│ 第三阶段：按槽位并发处理 (for each slot)                     │
//	│                                                             │
//	│  ┌─── RocksCache.FetchBatch2 ───┐                          │
//	│  │ 3.1 尝试批量获取缓存          │                          │
//	│  │     ↓                        │                          │
//	│  │ 3.2 缓存未命中处理回调         │                          │
//	│  │     • 构建queryIds            │                          │
//	│  │     • fn(queryIds) 查询数据库  │                          │
//	│  │     • 序列化结果为JSON         │                          │
//	│  │     • 返回cacheIndex映射       │                          │
//	│  │     ↓                        │                          │
//	│  │ 3.3 批量写入缓存              │                          │
//	│  └─────────────────────────────┘                          │
//	└─────────────────────────────────────────────────────────────┘
//	  ↓
//	┌─────────────────────────────────────────────────────────────┐
//	│ 第四阶段：结果处理和对象重建                                  │
//	│ • JSON反序列化: string → 业务对象                           │
//	│ • BatchCacheCallback 扩展调用                              │
//	│ • 添加到result结果集                                         │
//	└─────────────────────────────────────────────────────────────┘
//	  ↓
//	┌─────────────────────────────────────────────────────────────┐
//	│ 第五阶段：返回最终结果                                        │
//	│ • 返回 []*V (指针切片，包含所有查询到的数据)                 │
//	│ • 数据来源: 缓存命中 + 数据库查询                            │
//	└─────────────────────────────────────────────────────────────┘
//
// **关键性能优化点：**
// 1. 键去重：避免重复的缓存查询
// 2. 槽位分组：Redis集群优化，减少跨节点通信
// 3. 批量操作：减少网络往返次数
// 4. 分布式锁：防止缓存击穿
// 5. Pipeline：批量执行Redis命令
//
// **数据一致性保证：**
// 1. RocksCache强一致性模式
// 2. 分布式锁防止并发写入
// 3. 原子性的批量缓存更新
//
// **错误处理策略：**
// 1. 边界条件检查
// 2. 详细的错误日志
// 3. 分阶段错误传播
// 4. 缓存穿透保护
//
// **使用示例：**
//
// ```go
// // 1. 定义业务数据结构
//
//	type User struct {
//	    ID   string `json:"id"`
//	    Name string `json:"name"`
//	}
//
// // 2. 实现BatchCacheCallback接口（可选）
//
//	func (u *User) BatchCache(id string) {
//	    log.Printf("用户 %s 从缓存加载", id)
//	}
//
// // 3. 调用batchGetCache2
// users, err := batchGetCache2(
//
//	ctx,
//	rcClient,
//	time.Hour,                    // 缓存1小时
//	[]string{"user1", "user2"},   // 要查询的用户ID
//	func(id string) string {      // ID到缓存键的转换
//	    return "user:cache:" + id
//	},
//	func(u *User) string {        // 从用户对象提取ID
//	    return u.ID
//	},
//	func(ctx context.Context, ids []string) ([]*User, error) { // 数据库查询
//	    return getUsersFromDB(ctx, ids)
//	},
//
// )
// ```
//
// **最佳实践：**
// 1. 缓存键命名规范：使用前缀和版本号，如 "user:v1:123"
// 2. 过期时间设置：根据数据更新频率和业务需求合理设置
// 3. 批量大小控制：避免单次查询过多数据，建议50-200个键
// 4. 错误处理：区分缓存错误和业务错误，实现降级策略
// 5. 监控指标：记录缓存命中率、查询延迟等关键指标
//
// **性能注意事项：**
// 1. 大对象序列化：考虑使用压缩或分片存储
// 2. 热点数据：使用本地缓存+分布式缓存的多级架构
// 3. 缓存更新：使用事件驱动的主动更新机制
// 4. 集群配置：确保Redis集群的槽位分布均匀
//
// 功能：高效地批量从缓存中获取数据，对于缓存未命中的数据会调用数据库查询函数
//
// **设计目标：**
// 1. 最小化网络往返次数（通过槽位分组和Pipeline）
// 2. 避免缓存穿透和击穿（使用RocksCache的分布式锁机制）
// 3. 支持Redis集群环境的性能优化
// 4. 提供灵活的泛型接口，适用于各种数据类型
// 5. 保证数据一致性和并发安全
//
// **核心优化策略：**
// - 槽位分组：将键按Redis集群槽位分组，同槽位键在同一节点批量处理
// - 批量序列化：减少JSON序列化/反序列化的开销
// - 回调扩展：支持自定义缓存加载后的处理逻辑
//
// **泛型参数说明：**
// - K：键的类型（可比较类型，如string、int等）
// - V：值的类型（任意类型）
//
// **参数说明：**
// - ctx：上下文，用于取消操作和传递请求信息
// - rcClient：rockscache客户端，提供分布式缓存功能
// - expire：缓存过期时间，控制数据的时效性
// - ids：要查询的ID列表，业务层的标识符
// - idKey：ID到缓存键的转换函数，定义键的命名规则
// - vId：从值中提取ID的函数，用于结果映射和验证
// - fn：缓存未命中时的数据库查询函数，返回业务数据
//
// **返回值：**
// - []*V：查询到的数据列表（指针切片，避免大对象拷贝）
// - error：错误信息
func batchGetCache2[K comparable, V any](ctx context.Context, rcClient *rockscache.Client, expire time.Duration, ids []K, idKey func(id K) string, vId func(v *V) K, fn func(ctx context.Context, ids []K) ([]*V, error)) ([]*V, error) {
	// ==================== 第一阶段：输入验证和预处理 ====================

	// **边界条件检查**
	// 空输入直接返回，避免后续不必要的处理开销
	if len(ids) == 0 {
		return nil, nil
	}

	// **键去重和映射构建**
	// 目的1：避免重复的缓存查询，提高效率
	// 目的2：建立缓存键与业务ID的双向映射关系
	// 注意：这里只对缓存键去重，不对业务ID去重，因为业务ID可能合法重复
	findKeys := make([]string, 0, len(ids)) // 存储去重后的缓存键列表
	keyId := make(map[string]K)             // 缓存键 -> 业务ID 的映射表

	for _, id := range ids {
		key := idKey(id) // 通过用户提供的函数将业务ID转换为缓存键

		// **去重逻辑**：如果缓存键已存在，跳过处理
		// 这里的去重是基于缓存键而非业务ID，因为：
		// 1. 不同的业务ID可能映射到相同的缓存键（业务设计）
		// 2. 相同的缓存键只需要查询一次
		if _, ok := keyId[key]; ok {
			continue
		}

		// 建立映射关系并添加到查询列表
		keyId[key] = id
		findKeys = append(findKeys, key)
	}

	// ==================== 第二阶段：Redis集群槽位优化 ====================

	// **核心性能优化：按Redis集群槽位对键进行分组**
	//
	// **为什么需要槽位分组？**
	// 1. Redis集群将16384个槽位分布在不同节点上
	// 2. 同一槽位的键必定在同一个节点上
	// 3. 批量操作同槽位的键可以减少网络往返和节点间通信
	// 4. 避免跨节点的分布式事务开销
	//
	// **性能收益：**
	// - 网络延迟：从O(n)次网络请求优化到O(槽位数)次
	// - 并发处理：不同槽位可以并行处理
	// - 原子性：同槽位操作具有更好的原子性保证
	slotKeys, err := groupKeysBySlot(ctx, getRocksCacheRedisClient(rcClient), findKeys)
	if err != nil {
		return nil, err
	}

	// **结果集预分配**
	// 使用len(findKeys)而不是len(ids)，因为去重后的键数量可能更少
	result := make([]*V, 0, len(findKeys))

	// ==================== 第三阶段：按槽位并发处理缓存查询 ====================

	// **按槽位分组处理的优势：**
	// 1. 每个槽位的键可以在一次批量操作中处理
	// 2. 不同槽位之间可以并发处理（由RocksCache内部优化）
	// 3. 减少Redis集群的跨节点协调开销
	for _, keys := range slotKeys {

		// **RocksCache批量查询核心逻辑**
		// FetchBatch2是RocksCache提供的高级批量查询API
		//
		// **工作流程：**
		// 1. 尝试从缓存中批量获取所有键的数据
		// 2. 对于缓存未命中的键，调用提供的回调函数
		// 3. 将回调函数返回的数据写入缓存
		// 4. 返回完整的结果集（缓存命中+新查询的数据）
		//
		// **FetchBatch2的优势：**
		// - 分布式锁：防止缓存击穿（多个请求同时查询同一个未缓存的键）
		// - 批量操作：减少Redis网络往返
		// - 强一致性：保证缓存和数据源的一致性
		// - 异常处理：提供完善的错误处理机制
		indexCache, err := rcClient.FetchBatch2(ctx, keys, expire, func(idx []int) (map[int]string, error) {
			// **缓存未命中处理回调函数**
			// idx：缓存未命中的键在keys数组中的索引列表

			// -------------------- 子阶段3.1：构建数据库查询参数 --------------------

			// **查询ID列表构建**
			// 从缓存未命中的键索引，反向查找对应的业务ID
			queryIds := make([]K, 0, len(idx)) // 需要从数据库查询的业务ID列表
			idIndex := make(map[K]int)         // 业务ID -> 键索引 的映射表（用于结果回填）

			for _, index := range idx {
				// 通过键索引获取缓存键，再通过映射表获取业务ID
				key := keys[index]
				id := keyId[key]

				// 建立业务ID到键索引的映射，后续用于结果回填
				idIndex[id] = index
				queryIds = append(queryIds, id)
			}

			// -------------------- 子阶段3.2：执行数据库查询 --------------------

			// **调用用户提供的数据库查询函数**
			// 这是缓存未命中时的数据回源逻辑
			values, err := fn(ctx, queryIds)
			if err != nil {
				// **详细的错误日志**：帮助排查缓存穿透和数据库查询问题
				log.ZError(ctx, "batchGetCache query database failed", err, "keys", keys, "queryIds", queryIds)
				return nil, err
			}

			// **空结果处理**
			// 如果数据库查询返回空结果，返回空映射而不是nil
			// 这样RocksCache会将空值缓存起来，避免缓存穿透
			if len(values) == 0 {
				return map[int]string{}, nil
			}

			// -------------------- 子阶段3.3：结果序列化和索引映射 --------------------

			// **构建缓存写入数据**
			cacheIndex := make(map[int]string) // 键索引 -> JSON字符串 的映射

			for _, value := range values {
				// **从查询结果中提取业务ID**
				// 使用用户提供的vId函数从数据对象中提取ID
				id := vId(value)

				// **验证ID映射关系**
				// 确保查询返回的数据确实对应我们请求的ID
				index, ok := idIndex[id]
				if !ok {
					// 如果返回的数据ID不在请求列表中，跳过处理
					// 这可能是数据库查询逻辑的问题，或者数据不一致
					continue
				}

				// **JSON序列化**
				// 将业务对象序列化为JSON字符串，准备写入Redis缓存
				bs, err := json.Marshal(value)
				if err != nil {
					log.ZError(ctx, "marshal failed", err)
					return nil, err
				}

				// **建立索引到缓存值的映射**
				// 这个映射会被RocksCache用于批量写入缓存
				cacheIndex[index] = string(bs)
			}

			return cacheIndex, nil
		})

		// **批量查询错误处理**
		if err != nil {
			return nil, errs.WrapMsg(err, "FetchBatch2 failed")
		}

		// ==================== 第四阶段：结果处理和对象重建 ====================

		// **处理批量查询结果**
		// indexCache：键索引 -> JSON字符串 的映射（包含缓存命中和新查询的数据）
		for index, data := range indexCache {
			// **空数据过滤**
			// 跳过空字符串，这可能是缓存穿透保护的空值标记
			if data == "" {
				continue
			}

			// **JSON反序列化**
			// 将缓存中的JSON字符串反序列化为业务对象
			var value V
			if err := json.Unmarshal([]byte(data), &value); err != nil {
				return nil, errs.WrapMsg(err, "Unmarshal failed")
			}

			// **扩展点：批量缓存回调接口**
			// 如果业务对象实现了BatchCacheCallback接口，调用其回调方法
			//
			// **应用场景：**
			// - 缓存命中统计：记录哪些数据来自缓存
			// - 数据校验：对从缓存加载的数据进行校验
			// - 依赖注入：为缓存对象注入运行时依赖
			// - 审计日志：记录数据访问轨迹
			if cb, ok := any(&value).(BatchCacheCallback[K]); ok {
				// 传入对应的业务ID，让回调函数知道这个对象对应哪个ID
				cb.BatchCache(keyId[keys[index]])
			}

			// **添加到最终结果集**
			result = append(result, &value)
		}
	}

	// ==================== 第五阶段：返回最终结果 ====================

	// **返回完整结果集**
	// 结果包含：缓存命中的数据 + 数据库查询并缓存的数据
	// 注意：结果顺序可能与输入顺序不同，因为按槽位分组处理的原因
	return result, nil
}

// BatchCacheCallback 批量缓存回调接口
// 实现此接口的类型可以在从缓存加载时执行自定义逻辑
// 例如：记录缓存命中统计、执行数据校验等
type BatchCacheCallback[K comparable] interface {
	BatchCache(id K) // 当对象从缓存加载时调用，传入对应的ID
}
