// Package redis 实现基于Redis的会话序列号缓存
// 提供高性能的消息序列号分配和管理，支持分布式环境下的序列号一致性
// 核心功能：消息序列号分配、最小/最大序列号管理、批量操作优化
package redis

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/dtm-labs/rockscache"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache/cachekey"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/msgprocessor"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/redis/go-redis/v9"
)

// NewSeqConversationCacheRedis 创建基于Redis的会话序列号缓存实例
// rdb: Redis客户端实例，支持单机和集群模式
// mgo: MongoDB数据库接口，用于持久化存储
// 返回: 实现了SeqConversationCache接口的Redis缓存实例
func NewSeqConversationCacheRedis(rdb redis.UniversalClient, mgo database.SeqConversation) cache.SeqConversationCache {
	return &seqConversationCacheRedis{
		rdb:              rdb,                                                // Redis客户端
		mgo:              mgo,                                                // MongoDB接口
		lockTime:         time.Second * 3,                                    // 分布式锁超时时间：3秒
		dataTime:         time.Hour * 24 * 365,                               // 数据缓存过期时间：1年
		minSeqExpireTime: time.Hour,                                          // 最小序列号缓存过期时间：1小时
		rocks:            rockscache.NewClient(rdb, *GetRocksCacheOptions()), // RocksCache客户端，防止缓存击穿
	}
}

// seqConversationCacheRedis 基于Redis的会话序列号缓存实现
type seqConversationCacheRedis struct {
	rdb              redis.UniversalClient    // Redis客户端，用于序列号缓存操作
	mgo              database.SeqConversation // MongoDB接口，用于持久化存储
	rocks            *rockscache.Client       // RocksCache客户端，防止缓存击穿和雪崩
	lockTime         time.Duration            // 分布式锁持有时间，防止并发冲突
	dataTime         time.Duration            // 缓存数据过期时间，平衡性能和数据一致性
	minSeqExpireTime time.Duration            // 最小序列号缓存过期时间，控制缓存更新频率
}

// getMinSeqKey 生成最小序列号的缓存键
// conversationID: 会话ID
// 返回: Redis缓存键，用于存储会话的最小序列号
func (s *seqConversationCacheRedis) getMinSeqKey(conversationID string) string {
	return cachekey.GetMallocMinSeqKey(conversationID)
}

// SetMinSeq 设置单个会话的最小序列号
// ctx: 上下文，用于超时控制和链路追踪
// conversationID: 会话ID
// seq: 要设置的最小序列号
// 返回: 错误信息
func (s *seqConversationCacheRedis) SetMinSeq(ctx context.Context, conversationID string, seq int64) error {
	return s.SetMinSeqs(ctx, map[string]int64{conversationID: seq})
}

// GetMinSeq 获取单个会话的最小序列号
// 使用RocksCache防止缓存击穿，如果缓存不存在则从MongoDB获取
// ctx: 上下文
// conversationID: 会话ID
// 返回: 最小序列号和错误信息
func (s *seqConversationCacheRedis) GetMinSeq(ctx context.Context, conversationID string) (int64, error) {
	return getCache(ctx, s.rocks, s.getMinSeqKey(conversationID), s.minSeqExpireTime, func(ctx context.Context) (int64, error) {
		return s.mgo.GetMinSeq(ctx, conversationID)
	})
}

// getSingleMaxSeq 获取单个会话的最大序列号（内部辅助方法）
// 用于批量操作中的单个会话场景优化
// ctx: 上下文
// conversationID: 会话ID
// 返回: 包含单个会话ID和序列号的映射
func (s *seqConversationCacheRedis) getSingleMaxSeq(ctx context.Context, conversationID string) (map[string]int64, error) {
	seq, err := s.GetMaxSeq(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	return map[string]int64{conversationID: seq}, nil
}

// getSingleMaxSeqWithTime 获取单个会话的最大序列号和时间戳（内部辅助方法）
// 用于批量操作中的单个会话场景优化
// ctx: 上下文
// conversationID: 会话ID
// 返回: 包含单个会话ID和序列号时间信息的映射
func (s *seqConversationCacheRedis) getSingleMaxSeqWithTime(ctx context.Context, conversationID string) (map[string]database.SeqTime, error) {
	seq, err := s.GetMaxSeqWithTime(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	return map[string]database.SeqTime{conversationID: seq}, nil
}

// batchGetMaxSeq 批量获取最大序列号（内部方法）
// 使用Redis Pipeline优化批量查询性能，对缓存未命中的键进行回源
// ctx: 上下文
// keys: Redis缓存键列表
// keyConversationID: 缓存键到会话ID的映射关系
// seqs: 存储结果的映射，会话ID -> 序列号
// 返回: 错误信息
func (s *seqConversationCacheRedis) batchGetMaxSeq(ctx context.Context, keys []string, keyConversationID map[string]string, seqs map[string]int64) error {
	// 使用Pipeline批量查询Redis，减少网络往返次数
	result := make([]*redis.StringCmd, len(keys))
	pipe := s.rdb.Pipeline()
	for i, key := range keys {
		result[i] = pipe.HGet(ctx, key, "CURR") // 获取当前序列号
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return errs.Wrap(err)
	}

	// 处理Pipeline结果，收集缓存未命中的键
	var notFoundKey []string
	for i, r := range result {
		req, err := r.Int64()
		if err == nil {
			seqs[keyConversationID[keys[i]]] = req // 缓存命中，直接使用结果
		} else if errors.Is(err, redis.Nil) {
			notFoundKey = append(notFoundKey, keys[i]) // 缓存未命中，记录键
		} else {
			return errs.Wrap(err)
		}
	}

	// 对缓存未命中的键进行回源查询
	for _, key := range notFoundKey {
		conversationID := keyConversationID[key]
		seq, err := s.GetMaxSeq(ctx, conversationID) // 触发缓存回源
		if err != nil {
			return err
		}
		seqs[conversationID] = seq
	}
	return nil
}

// batchGetMaxSeqWithTime 批量获取最大序列号和时间戳（内部方法）
// 使用Redis Pipeline优化批量查询性能，同时获取序列号和时间戳
// ctx: 上下文
// keys: Redis缓存键列表
// keyConversationID: 缓存键到会话ID的映射关系
// seqs: 存储结果的映射，会话ID -> SeqTime结构体
// 返回: 错误信息
func (s *seqConversationCacheRedis) batchGetMaxSeqWithTime(ctx context.Context, keys []string, keyConversationID map[string]string, seqs map[string]database.SeqTime) error {
	// 使用Pipeline批量查询序列号和时间戳
	result := make([]*redis.SliceCmd, len(keys))
	pipe := s.rdb.Pipeline()
	for i, key := range keys {
		result[i] = pipe.HMGet(ctx, key, "CURR", "TIME") // 同时获取当前序列号和时间戳
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return errs.Wrap(err)
	}

	// 处理Pipeline结果
	var notFoundKey []string
	for i, r := range result {
		val, err := r.Result()
		if len(val) != 2 {
			return errs.WrapMsg(err, "batchGetMaxSeqWithTime invalid result", "key", keys[i], "res", val)
		}
		if val[0] == nil {
			notFoundKey = append(notFoundKey, keys[i]) // 缓存未命中
			continue
		}

		// 解析序列号和时间戳
		seq, err := s.parseInt64(val[0])
		if err != nil {
			return err
		}
		mill, err := s.parseInt64(val[1])
		if err != nil {
			return err
		}
		seqs[keyConversationID[keys[i]]] = database.SeqTime{Seq: seq, Time: mill}
	}

	// 对缓存未命中的键进行回源查询
	for _, key := range notFoundKey {
		conversationID := keyConversationID[key]
		seq, err := s.GetMaxSeqWithTime(ctx, conversationID) // 触发缓存回源
		if err != nil {
			return err
		}
		seqs[conversationID] = seq
	}
	return nil
}

// GetMaxSeqs 批量获取多个会话的最大序列号
// 针对不同数量的请求使用不同的优化策略：单个使用直接查询，多个使用批量查询
// ctx: 上下文
// conversationIDs: 会话ID列表
// 返回: 会话ID到最大序列号的映射
func (s *seqConversationCacheRedis) GetMaxSeqs(ctx context.Context, conversationIDs []string) (map[string]int64, error) {
	switch len(conversationIDs) {
	case 0:
		return map[string]int64{}, nil // 空请求直接返回空结果
	case 1:
		return s.getSingleMaxSeq(ctx, conversationIDs[0]) // 单个请求使用优化路径
	}

	// 构建键映射，去除重复的会话ID
	keys := make([]string, 0, len(conversationIDs))
	keyConversationID := make(map[string]string, len(conversationIDs))
	for _, conversationID := range conversationIDs {
		key := s.getSeqMallocKey(conversationID)
		if _, ok := keyConversationID[key]; ok {
			continue // 跳过重复的会话ID
		}
		keys = append(keys, key)
		keyConversationID[key] = conversationID
	}

	if len(keys) == 1 {
		return s.getSingleMaxSeq(ctx, conversationIDs[0]) // 去重后只有一个，使用单个查询
	}

	// 按Redis集群槽位分组，优化集群性能
	slotKeys, err := groupKeysBySlot(ctx, s.rdb, keys)
	if err != nil {
		return nil, err
	}

	// 批量查询每个槽位的键
	seqs := make(map[string]int64, len(conversationIDs))
	for _, keys := range slotKeys {
		if err := s.batchGetMaxSeq(ctx, keys, keyConversationID, seqs); err != nil {
			return nil, err
		}
	}
	return seqs, nil
}

// GetMaxSeqsWithTime 批量获取多个会话的最大序列号和时间戳
// 类似GetMaxSeqs，但同时返回时间戳信息，用于数据一致性检查
// ctx: 上下文
// conversationIDs: 会话ID列表
// 返回: 会话ID到序列号时间信息的映射
func (s *seqConversationCacheRedis) GetMaxSeqsWithTime(ctx context.Context, conversationIDs []string) (map[string]database.SeqTime, error) {
	switch len(conversationIDs) {
	case 0:
		return map[string]database.SeqTime{}, nil // 空请求直接返回空结果
	case 1:
		return s.getSingleMaxSeqWithTime(ctx, conversationIDs[0]) // 单个请求使用优化路径
	}

	// 构建键映射，去除重复的会话ID
	keys := make([]string, 0, len(conversationIDs))
	keyConversationID := make(map[string]string, len(conversationIDs))
	for _, conversationID := range conversationIDs {
		key := s.getSeqMallocKey(conversationID)
		if _, ok := keyConversationID[key]; ok {
			continue // 跳过重复的会话ID
		}
		keys = append(keys, key)
		keyConversationID[key] = conversationID
	}

	if len(keys) == 1 {
		return s.getSingleMaxSeqWithTime(ctx, conversationIDs[0]) // 去重后只有一个，使用单个查询
	}

	// 按Redis集群槽位分组，优化集群性能
	slotKeys, err := groupKeysBySlot(ctx, s.rdb, keys)
	if err != nil {
		return nil, err
	}

	// 批量查询每个槽位的键
	seqs := make(map[string]database.SeqTime, len(conversationIDs))
	for _, keys := range slotKeys {
		if err := s.batchGetMaxSeqWithTime(ctx, keys, keyConversationID, seqs); err != nil {
			return nil, err
		}
	}
	return seqs, nil
}

// getSeqMallocKey 生成序列号分配的缓存键
// conversationID: 会话ID
// 返回: Redis缓存键，用于存储会话的序列号分配信息（CURR、LAST、TIME、LOCK字段）
func (s *seqConversationCacheRedis) getSeqMallocKey(conversationID string) string {
	return cachekey.GetMallocSeqKey(conversationID)
}

// setSeq 设置序列号缓存，通过Redis Lua脚本保证原子性
// 用于在序列号分配完成后更新缓存，支持分布式锁验证
// ctx: 上下文
// key: Redis缓存键
// owner: 锁的拥有者ID，用于验证锁的归属
// currSeq: 当前序列号
// lastSeq: 最大可用序列号
// mill: 时间戳（毫秒）
// 返回: 操作状态码和错误信息
// 状态码说明：
// 0: 成功设置
// 1: 设置成功，但锁已过期（无其他人持锁）
// 2: 锁被其他人持有，设置失败
func (s *seqConversationCacheRedis) setSeq(ctx context.Context, key string, owner int64, currSeq int64, lastSeq int64, mill int64) (int64, error) {
	if lastSeq < currSeq {
		return 0, errs.New("lastSeq must be greater than currSeq")
	}
	// Lua脚本保证操作的原子性，避免并发问题
	script := `
local key = KEYS[1]
local lockValue = ARGV[1]
local dataSecond = ARGV[2]
local curr_seq = tonumber(ARGV[3])
local last_seq = tonumber(ARGV[4])
local mallocTime = ARGV[5]
if redis.call("EXISTS", key) == 0 then
	redis.call("HSET", key, "CURR", curr_seq, "LAST", last_seq, "TIME", mallocTime)
	redis.call("EXPIRE", key, dataSecond)
	return 1
end
if redis.call("HGET", key, "LOCK") ~= lockValue then
	return 2
end
redis.call("HDEL", key, "LOCK")
redis.call("HSET", key, "CURR", curr_seq, "LAST", last_seq, "TIME", mallocTime)
redis.call("EXPIRE", key, dataSecond)
return 0
`
	result, err := s.rdb.Eval(ctx, script, []string{key}, owner, int64(s.dataTime/time.Second), currSeq, lastSeq, mill).Int64()
	if err != nil {
		return 0, errs.Wrap(err)
	}
	return result, nil
}

// malloc 序列号分配核心方法，通过Redis Lua脚本实现分布式序列号分配
// 支持获取当前序列号（size=0）和分配新序列号（size>0）两种模式
// ctx: 上下文
// key: Redis缓存键
// size: 分配大小，0表示仅获取当前序列号，>0表示分配指定数量的序列号
// 返回: 操作结果数组和错误信息
// 结果数组格式根据状态码不同而不同：
// 状态码 0: [状态码, 起始序列号, 最大序列号, 时间戳]
// 状态码 1: [状态码, 锁值, 时间戳] - 需要从数据库获取并加锁
// 状态码 2: [状态码] - 已被锁定
// 状态码 3: [状态码, 当前序列号, 最大序列号, 锁值, 时间戳] - 超出最大值需要重新分配
func (s *seqConversationCacheRedis) malloc(ctx context.Context, key string, size int64) ([]int64, error) {
	// 使用Lua脚本保证分配操作的原子性，避免并发分配导致的序列号重复
	script := `
local key = KEYS[1]
local size = tonumber(ARGV[1])
local lockSecond = ARGV[2]
local dataSecond = ARGV[3]
local mallocTime = ARGV[4]
local result = {}
if redis.call("EXISTS", key) == 0 then
	local lockValue = math.random(0, 999999999)
	redis.call("HSET", key, "LOCK", lockValue)
	redis.call("EXPIRE", key, lockSecond)
	table.insert(result, 1)
	table.insert(result, lockValue)
	table.insert(result, mallocTime)
	return result
end
if redis.call("HEXISTS", key, "LOCK") == 1 then
	table.insert(result, 2)
	return result
end
local curr_seq = tonumber(redis.call("HGET", key, "CURR"))
local last_seq = tonumber(redis.call("HGET", key, "LAST"))
if size == 0 then
	redis.call("EXPIRE", key, dataSecond)
	table.insert(result, 0)
	table.insert(result, curr_seq)
	table.insert(result, last_seq)
	local setTime = redis.call("HGET", key, "TIME")
	if setTime then
		table.insert(result, setTime)	
	else
		table.insert(result, 0)
	end
	return result
end
local max_seq = curr_seq + size
if max_seq > last_seq then
	local lockValue = math.random(0, 999999999)
	redis.call("HSET", key, "LOCK", lockValue)
	redis.call("HSET", key, "CURR", last_seq)
	redis.call("HSET", key, "TIME", mallocTime)
	redis.call("EXPIRE", key, lockSecond)
	table.insert(result, 3)
	table.insert(result, curr_seq)
	table.insert(result, last_seq)
	table.insert(result, lockValue)
	table.insert(result, mallocTime)
	return result
end
redis.call("HSET", key, "CURR", max_seq)
redis.call("HSET", key, "TIME", ARGV[4])
redis.call("EXPIRE", key, dataSecond)
table.insert(result, 0)
table.insert(result, curr_seq)
table.insert(result, last_seq)
table.insert(result, mallocTime)
return result
`
	result, err := s.rdb.Eval(ctx, script, []string{key}, size, int64(s.lockTime/time.Second), int64(s.dataTime/time.Second), time.Now().UnixMilli()).Int64Slice()
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return result, nil
}

// wait 等待方法，用于在锁竞争时进行短暂等待
// 避免忙等待，减少CPU消耗和Redis压力
// ctx: 上下文，支持超时控制
// 返回: 上下文错误（如超时或取消）
func (s *seqConversationCacheRedis) wait(ctx context.Context) error {
	timer := time.NewTimer(time.Second / 4) // 等待250毫秒
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil // 等待完成
	case <-ctx.Done():
		return ctx.Err() // 上下文取消或超时
	}
}

// setSeqRetry 带重试机制的序列号缓存设置
// 在网络抖动或锁竞争场景下提供重试保障，最大重试10次
// ctx: 上下文
// key: Redis缓存键
// owner: 锁拥有者ID
// currSeq: 当前序列号
// lastSeq: 最大序列号
// mill: 时间戳
func (s *seqConversationCacheRedis) setSeqRetry(ctx context.Context, key string, owner int64, currSeq int64, lastSeq int64, mill int64) {
	for i := 0; i < 10; i++ {
		state, err := s.setSeq(ctx, key, owner, currSeq, lastSeq, mill)
		if err != nil {
			log.ZError(ctx, "set seq cache failed", err, "key", key, "owner", owner, "currSeq", currSeq, "lastSeq", lastSeq, "count", i+1)
			if err := s.wait(ctx); err != nil {
				return // 上下文取消或超时，停止重试
			}
			continue
		}

		// 根据状态码处理不同情况
		switch state {
		case 0: // 理想状态：成功设置
		case 1:
			log.ZWarn(ctx, "set seq cache lock not found", nil, "key", key, "owner", owner, "currSeq", currSeq, "lastSeq", lastSeq)
		case 2:
			log.ZWarn(ctx, "set seq cache lock to be held by someone else", nil, "key", key, "owner", owner, "currSeq", currSeq, "lastSeq", lastSeq)
		default:
			log.ZError(ctx, "set seq cache lock unknown state", nil, "key", key, "owner", owner, "currSeq", currSeq, "lastSeq", lastSeq)
		}
		return // 无论成功失败都退出重试
	}
	log.ZError(ctx, "set seq cache retrying still failed", nil, "key", key, "owner", owner, "currSeq", currSeq, "lastSeq", lastSeq)
}

// getMallocSize 计算序列号分配大小
// 根据会话类型（群聊/单聊）动态调整分配策略，减少数据库访问频率
// conversationID: 会话ID
// size: 请求分配的大小
// 返回: 实际从数据库分配的大小
func (s *seqConversationCacheRedis) getMallocSize(conversationID string, size int64) int64 {
	if size == 0 {
		return 0 // 仅查询，不需要分配
	}
	var basicSize int64
	if msgprocessor.IsGroupConversationID(conversationID) {
		basicSize = 100 // 群聊基础分配100个序列号（消息频率高）
	} else {
		basicSize = 50 // 单聊基础分配50个序列号（消息频率相对较低）
	}
	basicSize += size // 基础分配 + 当前请求数量
	return basicSize
}

// Malloc 分配序列号（公共接口）
// 为指定会话分配指定数量的序列号，返回起始序列号
// ctx: 上下文
// conversationID: 会话ID
// size: 分配数量
// 返回: 起始序列号和错误信息
func (s *seqConversationCacheRedis) Malloc(ctx context.Context, conversationID string, size int64) (int64, error) {
	seq, _, err := s.mallocTime(ctx, conversationID, size)
	return seq, err
}

// mallocTime 带时间戳的序列号分配（内部方法）
// 提供序列号分配的完整实现，包含时间戳信息用于数据一致性检查
// ctx: 上下文
// conversationID: 会话ID
// size: 分配数量，0表示仅获取当前最大序列号
// 返回: 起始序列号、时间戳和错误信息
func (s *seqConversationCacheRedis) mallocTime(ctx context.Context, conversationID string, size int64) (int64, int64, error) {
	if size < 0 {
		return 0, 0, errs.New("size must be greater than 0")
	}
	key := s.getSeqMallocKey(conversationID)

	// 最多重试10次，处理锁竞争和网络异常
	for i := 0; i < 10; i++ {
		states, err := s.malloc(ctx, key, size)
		if err != nil {
			return 0, 0, err
		}

		// 根据malloc返回的状态码处理不同情况
		switch states[0] {
		case 0: // 成功：缓存命中且有足够的序列号可分配
			return states[1], states[3], nil
		case 1: // 缓存不存在：需要从数据库获取初始序列号
			mallocSize := s.getMallocSize(conversationID, size)
			seq, err := s.mgo.Malloc(ctx, conversationID, mallocSize)
			if err != nil {
				return 0, 0, err
			}
			// 异步更新缓存，设置当前序列号和最大可用序列号
			s.setSeqRetry(ctx, key, states[1], seq+size, seq+mallocSize, states[2])
			return seq, 0, nil
		case 2: // 已被锁定：等待其他协程完成操作后重试
			if err := s.wait(ctx); err != nil {
				return 0, 0, err
			}
			continue
		case 3: // 序列号耗尽：需要从数据库重新分配
			currSeq := states[1]
			lastSeq := states[2]
			mill := states[4]
			mallocSize := s.getMallocSize(conversationID, size)
			seq, err := s.mgo.Malloc(ctx, conversationID, mallocSize)
			if err != nil {
				return 0, 0, err
			}

			// 检查数据库序列号是否与缓存一致
			if lastSeq == seq {
				// 一致：从当前序列号继续分配
				s.setSeqRetry(ctx, key, states[3], currSeq+size, seq+mallocSize, mill)
				return currSeq, states[4], nil
			} else {
				// 不一致：可能有其他实例分配了序列号，从数据库序列号开始
				log.ZWarn(ctx, "malloc seq not equal cache last seq", nil, "conversationID", conversationID, "currSeq", currSeq, "lastSeq", lastSeq, "mallocSeq", seq)
				s.setSeqRetry(ctx, key, states[3], seq+size, seq+mallocSize, mill)
				return seq, mill, nil
			}
		default:
			log.ZError(ctx, "malloc seq unknown state", nil, "state", states[0], "conversationID", conversationID, "size", size)
			return 0, 0, errs.New(fmt.Sprintf("unknown state: %d", states[0]))
		}
	}
	log.ZError(ctx, "malloc seq retrying still failed", nil, "conversationID", conversationID, "size", size)
	return 0, 0, errs.New("malloc seq waiting for lock timeout", "conversationID", conversationID, "size", size)
}

// GetMaxSeq 获取会话的当前最大序列号
// 通过调用Malloc(size=0)实现，不分配新序列号
// ctx: 上下文
// conversationID: 会话ID
// 返回: 当前最大序列号和错误信息
func (s *seqConversationCacheRedis) GetMaxSeq(ctx context.Context, conversationID string) (int64, error) {
	return s.Malloc(ctx, conversationID, 0)
}

// GetMaxSeqWithTime 获取会话的当前最大序列号和时间戳
// 提供序列号和对应的时间戳，用于数据一致性检查和监控
// ctx: 上下文
// conversationID: 会话ID
// 返回: 包含序列号和时间戳的结构体以及错误信息
func (s *seqConversationCacheRedis) GetMaxSeqWithTime(ctx context.Context, conversationID string) (database.SeqTime, error) {
	seq, mill, err := s.mallocTime(ctx, conversationID, 0)
	if err != nil {
		return database.SeqTime{}, err
	}
	return database.SeqTime{Seq: seq, Time: mill}, nil
}

// SetMinSeqs 批量设置多个会话的最小序列号
// 先更新数据库，然后删除相关缓存，确保数据一致性
// ctx: 上下文
// seqs: 会话ID到最小序列号的映射
// 返回: 错误信息
func (s *seqConversationCacheRedis) SetMinSeqs(ctx context.Context, seqs map[string]int64) error {
	keys := make([]string, 0, len(seqs))
	for conversationID, seq := range seqs {
		keys = append(keys, s.getMinSeqKey(conversationID))
		// 先更新数据库，保证持久化
		if err := s.mgo.SetMinSeq(ctx, conversationID, seq); err != nil {
			return err
		}
	}
	// 删除缓存，下次查询时会从数据库重新加载
	return DeleteCacheBySlot(ctx, s.rocks, keys)
}

// GetCacheMaxSeqWithTime 仅获取现有缓存中的最大序列号和时间戳
// 如果缓存不存在，不会触发数据库查询，用于避免缓存穿透
// ctx: 上下文
// conversationIDs: 会话ID列表
// 返回: 存在于缓存中的会话序列号时间信息映射
func (s *seqConversationCacheRedis) GetCacheMaxSeqWithTime(ctx context.Context, conversationIDs []string) (map[string]database.SeqTime, error) {
	if len(conversationIDs) == 0 {
		return map[string]database.SeqTime{}, nil
	}
	key2conversationID := make(map[string]string)
	keys := make([]string, 0, len(conversationIDs))
	for _, conversationID := range conversationIDs {
		key := s.getSeqMallocKey(conversationID)
		if _, ok := key2conversationID[key]; ok {
			continue
		}
		key2conversationID[key] = conversationID
		keys = append(keys, key)
	}
	slotKeys, err := groupKeysBySlot(ctx, s.rdb, keys)
	if err != nil {
		return nil, err
	}
	res := make(map[string]database.SeqTime)
	for _, keys := range slotKeys {
		if len(keys) == 0 {
			continue
		}
		pipe := s.rdb.Pipeline()
		cmds := make([]*redis.SliceCmd, 0, len(keys))
		for _, key := range keys {
			cmds = append(cmds, pipe.HMGet(ctx, key, "CURR", "TIME"))
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return nil, errs.Wrap(err)
		}
		for i, cmd := range cmds {
			val, err := cmd.Result()
			if err != nil {
				return nil, err
			}
			if len(val) != 2 {
				return nil, errs.WrapMsg(err, "GetCacheMaxSeqWithTime invalid result", "key", keys[i], "res", val)
			}
			if val[0] == nil {
				continue
			}
			seq, err := s.parseInt64(val[0])
			if err != nil {
				return nil, err
			}
			mill, err := s.parseInt64(val[1])
			if err != nil {
				return nil, err
			}
			conversationID := key2conversationID[keys[i]]
			res[conversationID] = database.SeqTime{Seq: seq, Time: mill}
		}
	}
	return res, nil
}

// parseInt64 将Redis返回的任意类型值转换为int64
// 支持多种数据类型的转换，提高系统的健壮性
// val: Redis返回的值，可能是nil、int、int64、string等类型
// 返回: 转换后的int64值和错误信息
func (s *seqConversationCacheRedis) parseInt64(val any) (int64, error) {
	switch v := val.(type) {
	case nil:
		return 0, nil // nil值返回0
	case int:
		return int64(v), nil // int类型直接转换
	case int64:
		return v, nil // int64类型直接返回
	case string:
		// 字符串类型尝试解析为数字
		res, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, errs.WrapMsg(err, "invalid string not int64", "value", v)
		}
		return res, nil
	default:
		// 不支持的类型返回错误
		return 0, errs.New("invalid result not int64", "resType", fmt.Sprintf("%T", v), "value", v)
	}
}
