package redis

import (
	"context"

	"github.com/dtm-labs/rockscache"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/errgroup"
)

const (
	// 默认配置常量
	defaultBatchSize       = 50 // 默认批处理大小：每批处理50个键
	defaultConcurrentLimit = 3  // 默认并发限制：最多3个协程并发执行
)

// RedisShardManager Redis分片管理器
// 用于在Redis集群环境中高效地处理大量键的操作
// 核心功能：根据Redis集群的哈希槽(hash slot)对键进行分组，实现槽位优化的批量操作
//
// 设计目标：
// 1. 在Redis集群中，同一槽位的键位于同一节点，批量操作更高效
// 2. 支持并发处理多个槽位，提高整体性能
// 3. 提供灵活的配置选项（批大小、并发数、错误处理策略）
type RedisShardManager struct {
	redisClient redis.UniversalClient // Redis客户端（支持单机/集群/哨兵模式）
	config      *Config               // 配置参数
}

// Config 分片管理器配置
type Config struct {
	batchSize       int  // 批处理大小：每批处理的键数量
	continueOnError bool // 错误处理策略：遇到错误时是否继续处理其他批次
	concurrentLimit int  // 并发限制：同时处理的协程数量上限
}

// Option 配置选项函数类型
// 使用函数选项模式，提供灵活的配置方式
type Option func(c *Config)

// NewRedisShardManager 创建新的Redis分片管理器实例
// 使用函数选项模式允许灵活配置
// 参数：
// - redisClient：Redis客户端实例
// - opts：可变数量的配置选项
func NewRedisShardManager(redisClient redis.UniversalClient, opts ...Option) *RedisShardManager {
	// 设置默认配置
	config := &Config{
		batchSize:       defaultBatchSize,       // 默认批大小50
		continueOnError: false,                  // 默认遇错停止
		concurrentLimit: defaultConcurrentLimit, // 默认并发数3
	}
	// 应用用户提供的配置选项
	for _, opt := range opts {
		opt(config)
	}
	rsm := &RedisShardManager{
		redisClient: redisClient,
		config:      config,
	}
	return rsm
}

// WithBatchSize 设置批处理大小的配置选项
// 较大的批处理大小可以提高效率，但会增加内存使用
// 建议根据键的大小和网络延迟调整此值
func WithBatchSize(size int) Option {
	return func(c *Config) {
		c.batchSize = size
	}
}

// WithContinueOnError 设置错误处理策略的配置选项
// true：遇到错误时继续处理其他批次（适用于允许部分失败的场景）
// false：遇到错误时立即停止（适用于要求严格一致性的场景）
func WithContinueOnError(continueOnError bool) Option {
	return func(c *Config) {
		c.continueOnError = continueOnError
	}
}

// WithConcurrentLimit 设置并发限制的配置选项
// 过高的并发数可能导致Redis服务器过载
// 建议根据Redis服务器性能和网络条件调整
func WithConcurrentLimit(limit int) Option {
	return func(c *Config) {
		c.concurrentLimit = limit
	}
}

// ProcessKeysBySlot 按槽位处理键的核心方法（类实例方法）
// 功能：将键按Redis集群哈希槽分组，然后并发地批量处理每个槽位的键
//
// 工作流程：
// 1. 调用groupKeysBySlot将键按槽位分组
// 2. 对每个槽位的键按batchSize分批
// 3. 使用errgroup并发处理各个批次
// 4. 根据continueOnError配置决定错误处理策略
//
// 参数：
// - ctx：上下文对象，用于取消和超时控制
// - keys：要处理的键列表
// - processFunc：处理函数，接收槽位号和键列表，执行具体的业务逻辑
func (rsm *RedisShardManager) ProcessKeysBySlot(
	ctx context.Context,
	keys []string,
	processFunc func(ctx context.Context, slot int64, keys []string) error,
) error {

	// **第一步：按槽位对键进行分组**
	// 这是性能优化的关键步骤
	slots, err := groupKeysBySlot(ctx, rsm.redisClient, keys)
	if err != nil {
		return err
	}

	// **第二步：设置并发控制**
	// 使用errgroup进行并发处理和错误收集
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(rsm.config.concurrentLimit) // 限制并发协程数量

	// **第三步：并发处理各个槽位**
	for slot, singleSlotKeys := range slots {
		// 将单个槽位的键按批大小分割
		batches := splitIntoBatches(singleSlotKeys, rsm.config.batchSize)
		for _, batch := range batches {
			slot, batch := slot, batch // 避免闭包捕获问题（go语言经典陷阱）
			// 启动协程处理单个批次
			g.Go(func() error {
				err := processFunc(ctx, slot, batch)
				if err != nil {
					log.ZWarn(ctx, "Batch processFunc failed", err, "slot", slot, "keys", batch)
					// 根据配置决定是否继续处理
					if !rsm.config.continueOnError {
						return err // 立即返回错误，停止所有处理
					}
				}
				return nil // 继续处理模式下总是返回nil
			})
		}
	}

	// **第四步：等待所有协程完成**
	if err := g.Wait(); err != nil {
		return err
	}
	return nil
}

// groupKeysBySlot **核心函数**：根据Redis集群哈希槽对键进行分组
//
// **作用和重要性：**
// 1. **性能优化**：Redis集群将键空间分为16384个槽位，同一槽位的键存储在同一节点
// 2. **网络效率**：批量操作同一槽位的键只需要一次网络请求到对应节点
// 3. **避免跨节点操作**：减少多节点间的协调开销
// 4. **支持单机模式**：非集群模式下将所有键归到槽位0
//
// **实现原理：**
// - 使用Redis的CLUSTER KEYSLOT命令获取每个键的槽位号
// - 利用Pipeline批量执行KEYSLOT命令，减少网络往返
// - 将相同槽位的键分组到一起
//
// **参数说明：**
// - ctx：上下文对象
// - redisClient：Redis客户端
// - keys：要分组的键列表
//
// **返回值：**
// - map[int64][]string：槽位号到键列表的映射
func groupKeysBySlot(ctx context.Context, redisClient redis.UniversalClient, keys []string) (map[int64][]string, error) {
	slots := make(map[int64][]string)

	// **关键判断：是否为Redis集群模式**
	clusterClient, isCluster := redisClient.(*redis.ClusterClient)

	if isCluster && len(keys) > 1 {
		// **Redis集群模式的处理逻辑**

		// 创建Pipeline以提高批量查询效率
		// Pipeline允许将多个命令打包发送，减少网络往返次数
		pipe := clusterClient.Pipeline()
		cmds := make([]*redis.IntCmd, len(keys))

		// 批量添加CLUSTER KEYSLOT命令到Pipeline
		for i, key := range keys {
			// CLUSTER KEYSLOT命令返回指定键所属的槽位号
			cmds[i] = pipe.ClusterKeySlot(ctx, key)
		}

		// 执行Pipeline中的所有命令
		_, err := pipe.Exec(ctx)
		if err != nil {
			return nil, errs.WrapMsg(err, "get slot err")
		}

		// 处理Pipeline执行结果
		for i, cmd := range cmds {
			// 获取键的槽位号
			slot, err := cmd.Result()
			if err != nil {
				log.ZWarn(ctx, "some key get slot err", err, "key", keys[i])
				return nil, errs.WrapMsg(err, "get slot err", "key", keys[i])
			}
			// 将键添加到对应槽位的分组中
			slots[slot] = append(slots[slot], keys[i])
		}
	} else {
		// **非集群模式的处理逻辑**
		// 单机Redis或哨兵模式下，所有键都放在槽位0
		// 这样保持了API的一致性，上层调用代码无需关心部署模式
		slots[0] = keys
	}

	return slots, nil
}

// splitIntoBatches 将键列表按指定大小分割成多个批次
// 使用切片的reslice技巧高效地分割数组，避免额外的内存分配
//
// 参数：
// - keys：要分割的键列表
// - batchSize：每批的大小
//
// 实现技巧：
// - 使用keys[0:batchSize:batchSize]确保新切片不会意外增长
// - 通过循环逐步移动切片窗口完成分割
func splitIntoBatches(keys []string, batchSize int) [][]string {
	var batches [][]string
	// 当剩余键数量大于批大小时，继续分割
	for batchSize < len(keys) {
		// reslice技巧：[start:end:capacity]
		// keys[0:batchSize:batchSize] 创建一个容量受限的切片
		keys, batches = keys[batchSize:], append(batches, keys[0:batchSize:batchSize])
	}
	// 添加最后一批（可能不足batchSize个）
	return append(batches, keys)
}

// ProcessKeysBySlot 全局函数版本的按槽位处理键方法
// 提供更简单的API，无需创建RedisShardManager实例
// 内部逻辑与类方法版本完全相同
//
// 使用场景：一次性操作，不需要重复使用配置的情况
func ProcessKeysBySlot(
	ctx context.Context,
	redisClient redis.UniversalClient,
	keys []string,
	processFunc func(ctx context.Context, slot int64, keys []string) error,
	opts ...Option,
) error {

	// 创建默认配置并应用选项
	config := &Config{
		batchSize:       defaultBatchSize,
		continueOnError: false,
		concurrentLimit: defaultConcurrentLimit,
	}
	for _, opt := range opts {
		opt(config)
	}

	// 按槽位分组键
	slots, err := groupKeysBySlot(ctx, redisClient, keys)
	if err != nil {
		return err
	}

	// 并发处理逻辑（与类方法相同）
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(config.concurrentLimit)

	for slot, singleSlotKeys := range slots {
		batches := splitIntoBatches(singleSlotKeys, config.batchSize)
		for _, batch := range batches {
			slot, batch := slot, batch // 避免闭包捕获问题
			g.Go(func() error {
				err := processFunc(ctx, slot, batch)
				if err != nil {
					log.ZWarn(ctx, "Batch processFunc failed", err, "slot", slot, "keys", batch)
					if !config.continueOnError {
						return err
					}
				}
				return nil
			})
		}
	}

	if err := g.Wait(); err != nil {
		return err
	}
	return nil
}

// DeleteCacheBySlot 基于槽位优化的缓存删除函数
// 专门用于RocksCache的批量删除操作
//
// **优化策略：**
// - 0个键：直接返回，无操作
// - 1个键：直接调用TagAsDeletedBatch2，避免不必要的分槽开销
// - 多个键：使用槽位分组优化批量删除
//
// **工作原理：**
// 1. 获取RocksCache内部的Redis客户端
// 2. 按槽位对键进行分组
// 3. 并发处理各个槽位的删除操作
// 4. 使用TagAsDeletedBatch2进行标记删除（软删除）
//
// 参数：
// - ctx：上下文对象
// - rcClient：RocksCache客户端
// - keys：要删除的缓存键列表
func DeleteCacheBySlot(ctx context.Context, rcClient *rockscache.Client, keys []string) error {
	switch len(keys) {
	case 0:
		return nil // 无键需要删除
	case 1:
		// 单键直接删除，避免分槽开销
		return rcClient.TagAsDeletedBatch2(ctx, keys)
	default:
		// 多键使用槽位优化删除
		return ProcessKeysBySlot(ctx, getRocksCacheRedisClient(rcClient), keys, func(ctx context.Context, slot int64, keys []string) error {
			// TagAsDeletedBatch2是RocksCache的软删除方法
			// 优势：延迟删除，避免缓存穿透，支持原子操作
			return rcClient.TagAsDeletedBatch2(ctx, keys)
		})
	}
}
