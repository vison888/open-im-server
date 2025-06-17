// Copyright © 2023 OpenIM. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dtm-labs/rockscache"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache"
	"github.com/openimsdk/open-im-server/v3/pkg/localcache"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/utils/datautil"
	"github.com/redis/go-redis/v9"
)

const (
	// rocksCacheTimeout RocksCache操作的超时时间
	// 用于锁过期和副本等待超时，确保在分布式环境下的一致性
	rocksCacheTimeout = 11 * time.Second
)

// BatchDeleterRedis Redis批量删除器的具体实现
// 基于Redis和RocksCache实现，提供高效的批量缓存删除功能
// 主要特性：
// 1. 支持Redis集群环境下的槽位优化批量删除
// 2. 支持本地缓存失效通知（通过Redis发布订阅）
// 3. 支持链式调用和内存安全的克隆操作
type BatchDeleterRedis struct {
	redisClient    redis.UniversalClient // Redis客户端（支持单机/集群/哨兵模式）
	keys           []string              // 待删除的缓存键列表
	rocksClient    *rockscache.Client    // RocksCache客户端，提供分布式缓存一致性
	redisPubTopics []string              // Redis发布主题列表，用于通知其他节点更新本地缓存
}

// NewBatchDeleterRedis 创建新的BatchDeleterRedis实例
// 参数：
// - redisClient：Redis客户端实例
// - options：RocksCache配置选项
// - redisPubTopics：Redis发布主题列表，用于缓存失效通知
func NewBatchDeleterRedis(redisClient redis.UniversalClient, options *rockscache.Options, redisPubTopics []string) *BatchDeleterRedis {
	return &BatchDeleterRedis{
		redisClient:    redisClient,
		rocksClient:    rockscache.NewClient(redisClient, *options),
		redisPubTopics: redisPubTopics,
	}
}

// ExecDelWithKeys 直接使用提供的键进行批量删除
// 功能：对指定的缓存键进行批量删除操作，并发布删除通知
// 参数：
// - ctx：上下文对象
// - keys：要删除的缓存键列表
// 返回：删除操作的错误信息
func (c *BatchDeleterRedis) ExecDelWithKeys(ctx context.Context, keys []string) error {
	// 对键进行去重处理，避免重复删除
	distinctKeys := datautil.Distinct(keys)
	return c.execDel(ctx, distinctKeys)
}

// ChainExecDel 用于链式调用的批量删除方法
// 注意：必须调用Clone()方法防止内存污染
// 该方法会删除通过AddKeys()方法添加的所有键
func (c *BatchDeleterRedis) ChainExecDel(ctx context.Context) error {
	// 对内部keys字段进行去重处理
	distinctKeys := datautil.Distinct(c.keys)
	return c.execDel(ctx, distinctKeys)
}

// execDel 执行批量删除的核心方法
// 工作流程：
// 1. 按Redis集群槽位对键进行分组批量删除
// 2. 通过Redis发布订阅通知其他节点更新本地缓存
// 参数：
// - ctx：上下文对象
// - keys：去重后的缓存键列表
func (c *BatchDeleterRedis) execDel(ctx context.Context, keys []string) error {
	if len(keys) > 0 {
		log.ZDebug(ctx, "delete cache", "topic", c.redisPubTopics, "keys", keys)

		// **第一步：批量删除缓存键**
		// 使用ProcessKeysBySlot按槽位分组处理，提高Redis集群环境下的删除效率
		err := ProcessKeysBySlot(ctx, c.redisClient, keys, func(ctx context.Context, slot int64, keys []string) error {
			// 使用RocksCache的TagAsDeletedBatch2方法标记键为已删除
			// 这种方法比直接删除更高效，支持延迟删除和一致性保证
			return c.rocksClient.TagAsDeletedBatch2(ctx, keys)
		})
		if err != nil {
			return err
		}

		// **第二步：发布缓存失效通知**
		// 通过Redis发布订阅机制通知其他节点更新本地缓存
		if len(c.redisPubTopics) > 0 && len(keys) > 0 {
			// 根据主题对键进行分组
			keysByTopic := localcache.GetPublishKeysByTopic(c.redisPubTopics, keys)
			for topic, keys := range keysByTopic {
				if len(keys) > 0 {
					// 将键列表序列化为JSON
					data, err := json.Marshal(keys)
					if err != nil {
						log.ZWarn(ctx, "keys json marshal failed", err, "topic", topic, "keys", keys)
					} else {
						// 发布到Redis频道，通知其他节点
						if err := c.redisClient.Publish(ctx, topic, string(data)).Err(); err != nil {
							log.ZWarn(ctx, "redis publish cache delete error", err, "topic", topic, "keys", keys)
						}
					}
				}
			}
		}
	}
	return nil
}

// Clone 创建BatchDeleterRedis的副本
// 用于链式调用时防止内存污染和并发安全问题
// 返回一个新的BatchDeleter实例，共享相同的客户端但拥有独立的keys列表
func (c *BatchDeleterRedis) Clone() cache.BatchDeleter {
	return &BatchDeleterRedis{
		redisClient:    c.redisClient,
		keys:           c.keys, // 注意：这里是浅拷贝，实际使用中需要注意并发安全
		rocksClient:    c.rocksClient,
		redisPubTopics: c.redisPubTopics,
	}
}

// AddKeys 添加需要删除的缓存键
// 用于构建待删除键列表，支持多次调用累积添加
// 参数：keys - 要添加的缓存键列表（可变参数）
func (c *BatchDeleterRedis) AddKeys(keys ...string) {
	c.keys = append(c.keys, keys...)
}

// GetRocksCacheOptions 返回RocksCache的默认配置选项
// RocksCache是一个提供强一致性的分布式缓存库
// 配置说明：
// - LockExpire：分布式锁的过期时间
// - WaitReplicasTimeout：等待副本同步的超时时间
// - StrongConsistency：启用强一致性模式
// - RandomExpireAdjustment：随机过期调整因子，防止缓存雪崩
func GetRocksCacheOptions() *rockscache.Options {
	opts := rockscache.NewDefaultOptions()
	opts.LockExpire = rocksCacheTimeout          // 锁过期时间
	opts.WaitReplicasTimeout = rocksCacheTimeout // 副本等待超时
	opts.StrongConsistency = true                // 强一致性模式
	opts.RandomExpireAdjustment = 0.2            // 20%的随机过期调整

	return &opts
}

// getCache 单个缓存获取的通用方法
// 功能：从缓存中获取单个数据，缓存未命中时调用提供的函数从数据源获取
//
// 泛型参数：
// - T：缓存值的类型
//
// 参数：
// - ctx：上下文对象
// - rcClient：RocksCache客户端
// - key：缓存键
// - expire：缓存过期时间
// - fn：缓存未命中时的回调函数
//
// 工作原理：
// 1. 尝试从缓存读取数据
// 2. 如果缓存未命中，调用fn函数获取数据
// 3. 将获取的数据序列化后写入缓存
// 4. 返回数据
func getCache[T any](ctx context.Context, rcClient *rockscache.Client, key string, expire time.Duration, fn func(ctx context.Context) (T, error)) (T, error) {
	var t T
	var write bool // 标记是否进行了写操作

	// 使用RocksCache的Fetch2方法进行缓存查询
	v, err := rcClient.Fetch2(ctx, key, expire, func() (s string, err error) {
		// 缓存未命中时的回调函数
		t, err = fn(ctx)
		if err != nil {
			//log.ZError(ctx, "getCache query database failed", err, "key", key)
			return "", err
		}
		// 序列化数据准备写入缓存
		bs, err := json.Marshal(t)
		if err != nil {
			return "", errs.WrapMsg(err, "marshal failed")
		}
		write = true // 标记进行了写操作

		return string(bs), nil
	})
	if err != nil {
		return t, errs.Wrap(err)
	}

	// 如果进行了写操作，直接返回从数据源获取的数据
	if write {
		return t, nil
	}

	// 如果缓存值为空，表示记录不存在
	if v == "" {
		return t, errs.ErrRecordNotFound.WrapMsg("cache is not found")
	}

	// 反序列化缓存数据
	err = json.Unmarshal([]byte(v), &t)
	if err != nil {
		errInfo := fmt.Sprintf("cache json.Unmarshal failed, key:%s, value:%s, expire:%s", key, v, expire)
		return t, errs.WrapMsg(err, errInfo)
	}

	return t, nil
}

// 注释掉的batchGetCache函数是之前的批量缓存获取实现
// 现在已被更高效的batchGetCache2函数替代
// batchGetCache2相比这个实现有以下优势：
// 1. 支持Redis集群槽位优化
// 2. 更好的错误处理机制
// 3. 支持批量缓存回调接口
// 4. 更高的并发性能

//func batchGetCache[T any, K comparable](
//	ctx context.Context,
//	rcClient *rockscache.Client,
//	expire time.Duration,
//	keys []K,
//	keyFn func(key K) string,
//	fns func(ctx context.Context, key K) (T, error),
//) ([]T, error) {
//	if len(keys) == 0 {
//		return nil, nil
//	}
//	res := make([]T, 0, len(keys))
//	for _, key := range keys {
//		val, err := getCache(ctx, rcClient, keyFn(key), expire, func(ctx context.Context) (T, error) {
//			return fns(ctx, key)
//		})
//		if err != nil {
//			if errs.ErrRecordNotFound.Is(specialerror.ErrCode(errs.Unwrap(err))) {
//				continue
//			}
//			return nil, errs.Wrap(err)
//		}
//		res = append(res, val)
//	}
//
//	return res, nil
//}
