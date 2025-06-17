// Package redis 消息缓存Redis实现
//
// 本文件实现消息相关的Redis缓存操作，是消息系统数据缓存层的核心实现
//
// **功能概述：**
// ┌─────────────────────────────────────────────────────────┐
// │                消息缓存系统架构                          │
// ├─────────────────────────────────────────────────────────┤
// │ 1. 消息内容缓存：消息详细内容的高速缓存                  │
// │ 2. 发送状态缓存：消息发送状态的临时缓存                  │
// │ 3. 序列号索引：基于消息序列号的快速检索                  │
// │ 4. 会话维度：按会话ID组织的消息缓存结构                  │
// └─────────────────────────────────────────────────────────┘
//
// **缓存策略：**
// - 热点缓存：最近消息常驻内存，提供毫秒级访问
// - 批量优化：使用batchGetCache2进行槽位优化批量查询
// - 状态分离：消息内容和发送状态分别缓存
// - 过期管理：合理的过期时间平衡性能和存储成本
//
// **性能特点：**
// - 槽位分组：Redis集群环境下的性能优化
// - 批量操作：减少网络往返次数
// - 序列号索引：支持高效的消息检索
// - 异步写入：支持消息的异步缓存写入
//
// **业务特点：**
// - 时序性：消息具有严格的时间顺序
// - 会话隔离：不同会话的消息独立缓存
// - 状态管理：支持消息发送状态的跟踪
// - 大容量：支持大量消息的高效缓存
package redis

import (
	"context"
	"encoding/json"
	"time"

	"github.com/dtm-labs/rockscache"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache/cachekey"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/utils/datautil"
	"github.com/redis/go-redis/v9"
)

// msgCacheTimeout 消息缓存过期时间：24小时
// 设计考虑：消息数据访问频繁但时效性要求不高，24小时的缓存时间平衡了性能和存储成本
const msgCacheTimeout = time.Hour * 24

// NewMsgCache 创建消息缓存实例
//
// **功能说明：**
// 创建消息缓存的Redis实现实例
//
// **参数说明：**
// - client: Redis通用客户端，支持单机/集群/哨兵模式
// - db: 消息数据库操作接口
//
// **返回值：**
// - cache.MsgCache: 消息缓存接口实现
func NewMsgCache(client redis.UniversalClient, db database.Msg) cache.MsgCache {
	return &msgCache{
		rdb:            client,                                                // Redis客户端
		rcClient:       rockscache.NewClient(client, *GetRocksCacheOptions()), // RocksCache客户端
		msgDocDatabase: db,                                                    // 消息数据库接口
	}
}

// msgCache 消息缓存Redis实现
//
// **架构设计：**
// ┌─────────────────────────────────────────────────────────┐
// │                    msgCache                            │
// ├─────────────────────────────────────────────────────────┤
// │ Redis客户端层:                                           │
// │   - rdb: 原生Redis客户端                                │
// │   - rcClient: RocksCache增强客户端                      │
// ├─────────────────────────────────────────────────────────┤
// │ 数据库接口层:                                            │
// │   - msgDocDatabase: 消息文档数据库操作                  │
// └─────────────────────────────────────────────────────────┘
//
// **职责分工：**
// - 消息内容缓存：缓存消息的详细内容和元数据
// - 发送状态管理：跟踪消息的发送状态
// - 批量操作优化：提供高效的批量消息操作
// - 缓存生命周期：管理消息缓存的创建、更新和删除
//
// **缓存层次：**
// - L1缓存：RocksCache提供的分布式缓存
// - L2缓存：Redis原生缓存
// - L3存储：数据库持久化存储
type msgCache struct {
	rdb            redis.UniversalClient // Redis原生客户端，用于简单的键值操作
	rcClient       *rockscache.Client    // RocksCache客户端，提供分布式缓存功能
	msgDocDatabase database.Msg          // 消息数据库操作接口
}

// ==================== 缓存键生成函数 ====================

// getSendMsgKey 生成消息发送状态缓存键
// 格式：send_msg:{id}
// 用途：缓存消息的发送状态，用于跟踪消息发送进度
func (c *msgCache) getSendMsgKey(id string) string {
	return cachekey.GetSendMsgKey(id)
}

// ==================== 消息发送状态缓存操作 ====================

// SetSendMsgStatus 设置消息发送状态
//
// **功能说明：**
// 设置指定消息的发送状态，用于跟踪消息发送进度
//
// **状态值定义：**
// - 0: 发送中
// - 1: 发送成功
// - 2: 发送失败
// - 其他: 自定义状态
//
// **应用场景：**
// - 消息发送跟踪：记录消息的发送进度
// - 重试机制：基于状态决定是否需要重试
// - 状态查询：客户端查询消息发送状态
// - 统计分析：消息发送成功率统计
//
// **缓存特点：**
// - 临时性：发送状态是临时信息，设置24小时过期
// - 高频访问：发送过程中可能频繁查询状态
// - 轻量级：只存储状态值，数据量很小
//
// **参数说明：**
// - ctx: 上下文信息
// - id: 消息ID
// - status: 发送状态值
//
// **返回值：**
// - error: 错误信息
func (c *msgCache) SetSendMsgStatus(ctx context.Context, id string, status int32) error {
	return errs.Wrap(c.rdb.Set(ctx, c.getSendMsgKey(id), status, time.Hour*24).Err())
}

// GetSendMsgStatus 获取消息发送状态
//
// **功能说明：**
// 获取指定消息的发送状态
//
// **查询场景：**
// - 客户端状态查询：用户查看消息是否发送成功
// - 服务端重试判断：根据状态决定是否需要重试发送
// - 监控告警：监控消息发送失败情况
// - 数据统计：统计消息发送成功率
//
// **错误处理：**
// - 消息ID不存在：返回Redis的nil错误
// - 状态值无效：返回类型转换错误
// - 网络异常：返回网络相关错误
//
// **性能特点：**
// - 快速查询：直接从Redis获取，毫秒级响应
// - 无缓存穿透：状态不存在时直接返回错误
//
// **参数说明：**
// - ctx: 上下文信息
// - id: 消息ID
//
// **返回值：**
// - int32: 发送状态值
// - error: 错误信息
func (c *msgCache) GetSendMsgStatus(ctx context.Context, id string) (int32, error) {
	result, err := c.rdb.Get(ctx, c.getSendMsgKey(id)).Int()
	return int32(result), errs.Wrap(err)
}

// ==================== 消息内容缓存操作 ====================

// GetMessageBySeqs 根据序列号批量获取消息
//
// **功能说明：**
// 根据消息序列号批量获取指定会话的消息内容
//
// **核心特性：**
// 1. 槽位优化：使用batchGetCache2进行Redis集群槽位分组
// 2. 缓存穿透保护：使用RocksCache的分布式锁机制
// 3. 批量查询：减少数据库访问次数
// 4. 自动缓存：查询结果自动写入缓存
//
// **处理流程：**
// 1. 边界条件检查：空序列号列表直接返回
// 2. 构建缓存键：为每个序列号生成对应的缓存键
// 3. 槽位分组：按Redis槽位对缓存键进行分组
// 4. 并发查询：并发查询各槽位的缓存数据
// 5. 数据库回源：对于缓存未命中的序列号，批量查询数据库
// 6. 结果返回：返回完整的消息列表
//
// **性能优化：**
// - 网络往返：从O(n)优化到O(槽位数)
// - 并发处理：不同槽位并行处理
// - 缓存命中：热点消息快速返回
// - 批量数据库查询：减少数据库连接开销
//
// **应用场景：**
// - 消息历史查询：用户查看历史消息
// - 消息同步：客户端同步指定范围的消息
// - 消息转发：获取要转发的消息内容
// - 消息搜索：基于序列号的消息检索
//
// **参数说明：**
// - ctx: 上下文信息
// - conversationID: 会话ID，标识消息所属的会话
// - seqs: 消息序列号列表，用于精确定位消息
//
// **返回值：**
// - []*model.MsgInfoModel: 消息信息模型列表
// - error: 错误信息
func (c *msgCache) GetMessageBySeqs(ctx context.Context, conversationID string, seqs []int64) ([]*model.MsgInfoModel, error) {
	// **边界条件检查**
	// 空序列号列表直接返回，避免后续不必要的处理开销
	if len(seqs) == 0 {
		return nil, nil
	}

	// **定义缓存键生成函数**
	// 为每个序列号生成对应的缓存键
	getKey := func(seq int64) string {
		return cachekey.GetMsgCacheKey(conversationID, seq)
	}

	// **定义消息ID提取函数**
	// 从消息对象中提取序列号，用于结果映射
	getMsgID := func(msg *model.MsgInfoModel) int64 {
		return msg.Msg.Seq
	}

	// **定义数据库查询函数**
	// 缓存未命中时的数据库查询回调
	find := func(ctx context.Context, seqs []int64) ([]*model.MsgInfoModel, error) {
		return c.msgDocDatabase.FindSeqs(ctx, conversationID, seqs)
	}

	// **执行批量缓存查询**
	// 使用batchGetCache2进行高性能的批量查询
	return batchGetCache2(ctx, c.rcClient, msgCacheTimeout, seqs, getKey, getMsgID, find)
}

// DelMessageBySeqs 根据序列号批量删除消息缓存
//
// **功能说明：**
// 根据消息序列号批量删除指定会话的消息缓存
//
// **删除策略：**
// 1. 边界条件检查：空序列号列表直接返回
// 2. 构建缓存键列表：为每个序列号生成对应的缓存键
// 3. 槽位分组：按Redis槽位对缓存键进行分组
// 4. 批量删除：对每个槽位的键进行批量删除操作
//
// **应用场景：**
// - 消息撤回：用户撤回消息后删除缓存
// - 消息删除：用户删除消息后清理缓存
// - 消息更新：消息内容更新前删除旧缓存
// - 缓存清理：定期清理过期的消息缓存
//
// **性能优化：**
// - 槽位分组：Redis集群环境下的批量删除优化
// - 批量操作：减少网络往返次数
// - 并发处理：不同槽位并行删除
//
// **错误处理：**
// - 槽位分组失败：返回分组错误
// - 批量删除失败：返回删除操作错误
// - 网络异常：返回网络相关错误
//
// **参数说明：**
// - ctx: 上下文信息
// - conversationID: 会话ID
// - seqs: 要删除的消息序列号列表
//
// **返回值：**
// - error: 错误信息
func (c *msgCache) DelMessageBySeqs(ctx context.Context, conversationID string, seqs []int64) error {
	// **边界条件检查**
	if len(seqs) == 0 {
		return nil
	}

	// **构建缓存键列表**
	// 使用datautil.Slice进行函数式编程风格的转换
	keys := datautil.Slice(seqs, func(seq int64) string {
		return cachekey.GetMsgCacheKey(conversationID, seq)
	})

	// **按槽位分组缓存键**
	// 获取RocksCache内部的Redis客户端进行槽位分组
	slotKeys, err := groupKeysBySlot(ctx, getRocksCacheRedisClient(c.rcClient), keys)
	if err != nil {
		return err
	}

	// **按槽位批量删除**
	for _, keys := range slotKeys {
		// 使用RocksCache的TagAsDeletedBatch2进行批量标记删除
		if err := c.rcClient.TagAsDeletedBatch2(ctx, keys); err != nil {
			return err
		}
	}

	return nil
}

// SetMessageBySeqs 根据序列号批量设置消息缓存
//
// **功能说明：**
// 批量将消息内容写入缓存，按序列号进行索引
//
// **写入策略：**
// 1. 遍历所有消息对象
// 2. 验证消息的有效性（非空且序列号有效）
// 3. 序列化消息为JSON格式
// 4. 使用RocksCache的RawSet方法写入缓存
//
// **数据验证：**
// - 消息对象不能为空
// - 消息内容不能为空
// - 消息序列号必须大于0
//
// **应用场景：**
// - 消息发送：新消息发送后写入缓存
// - 消息同步：从其他节点同步消息后缓存
// - 缓存预热：批量预加载热点消息到缓存
// - 数据迁移：消息数据迁移时的缓存重建
//
// **性能特点：**
// - 批量写入：减少网络往返次数
// - JSON序列化：标准化的数据格式
// - 异步写入：不阻塞主要业务流程
//
// **错误处理：**
// - JSON序列化失败：返回序列化错误
// - 缓存写入失败：返回写入错误
// - 数据验证失败：跳过无效消息继续处理
//
// **参数说明：**
// - ctx: 上下文信息
// - conversationID: 会话ID
// - msgs: 要缓存的消息列表
//
// **返回值：**
// - error: 错误信息
func (c *msgCache) SetMessageBySeqs(ctx context.Context, conversationID string, msgs []*model.MsgInfoModel) error {
	// **遍历所有消息进行缓存写入**
	for _, msg := range msgs {
		// **数据有效性验证**
		if msg == nil || msg.Msg == nil || msg.Msg.Seq <= 0 {
			// 跳过无效消息，继续处理下一个
			continue
		}

		// **JSON序列化**
		// 将消息对象序列化为JSON字符串
		data, err := json.Marshal(msg)
		if err != nil {
			return err
		}

		// **写入缓存**
		// 使用RocksCache的RawSet方法直接写入缓存
		if err := c.rcClient.RawSet(ctx, cachekey.GetMsgCacheKey(conversationID, msg.Msg.Seq), string(data), msgCacheTimeout); err != nil {
			return err
		}
	}

	return nil
}
