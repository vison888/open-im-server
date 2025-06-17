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

// Package redis 好友关系缓存Redis实现
//
// 本文件实现好友关系相关的Redis缓存操作，是好友系统数据缓存层的核心实现
//
// **功能概述：**
// ┌─────────────────────────────────────────────────────────┐
// │                好友缓存系统架构                          │
// ├─────────────────────────────────────────────────────────┤
// │ 1. 好友列表缓存：用户的好友ID列表高速缓存                │
// │ 2. 好友信息缓存：好友详细信息的缓存管理                  │
// │ 3. 双向好友缓存：互为好友关系的缓存优化                  │
// │ 4. 版本控制缓存：好友关系变更的增量同步                  │
// └─────────────────────────────────────────────────────────┘
//
// **缓存策略：**
// - 多级缓存：Redis + RocksCache + 本地缓存
// - 智能失效：链式删除相关缓存键
// - 关系对称：处理好友关系的双向特性
// - 版本管理：支持增量更新和冲突检测
//
// **性能特点：**
// - 缓存命中率优化：热点好友数据常驻内存
// - 关系查询优化：快速的好友关系验证
// - 批量操作支持：减少网络往返次数
// - 异步更新：后台异步更新缓存数据
//
// **业务特点：**
// - 双向关系：好友关系具有对称性
// - 状态管理：支持好友申请、确认、删除等状态
// - 隐私保护：支持好友可见性和权限控制
// - 社交功能：支持双向好友、单向关注等模式
package redis

import (
	"context"
	"time"

	"github.com/dtm-labs/rockscache"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache/cachekey"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/utils/datautil"
	"github.com/redis/go-redis/v9"
)

const (
	// friendExpireTime 好友缓存过期时间：12小时
	// 设计考虑：好友关系相对稳定，12小时的缓存时间平衡了性能和数据一致性
	friendExpireTime = time.Second * 60 * 60 * 12
)

// FriendCacheRedis 好友缓存Redis实现
//
// **架构设计：**
// ┌─────────────────────────────────────────────────────────┐
// │                FriendCacheRedis                        │
// ├─────────────────────────────────────────────────────────┤
// │ BatchDeleter: 批量删除器                                │
// │   - 支持链式调用的缓存删除                               │
// │   - 批量删除优化性能                                     │
// │   - 事务性删除保证一致性                                 │
// ├─────────────────────────────────────────────────────────┤
// │ 数据库接口层:                                            │
// │   - friendDB: 好友关系数据操作                          │
// ├─────────────────────────────────────────────────────────┤
// │ 缓存客户端:                                              │
// │   - rcClient: RocksCache客户端                          │
// │   - expireTime: 统一的过期时间管理                       │
// │   - syncCount: 同步计数器（预留扩展）                    │
// └─────────────────────────────────────────────────────────┘
//
// **职责分工：**
// - 缓存管理：负责所有好友相关数据的缓存操作
// - 数据一致性：确保缓存与数据库的数据一致性
// - 性能优化：通过缓存机制提升好友查询性能
// - 版本控制：支持增量同步和版本管理
//
// **好友关系特点：**
// - 双向性：A是B的好友，B也是A的好友
// - 状态性：好友关系有多种状态（申请、确认、删除等）
// - 时效性：好友关系可能会发生变化，需要及时更新缓存
type FriendCacheRedis struct {
	cache.BatchDeleter                    // 批量删除器，支持链式缓存删除操作
	friendDB           database.Friend    // 好友数据库操作接口
	expireTime         time.Duration      // 缓存过期时间
	rcClient           *rockscache.Client // RocksCache客户端，提供分布式缓存功能
	syncCount          int                // 同步计数器（预留字段，用于扩展功能）
}

// NewFriendCacheRedis 创建好友缓存Redis实例
//
// **初始化流程：**
// 1. 创建批量删除处理器
// 2. 初始化RocksCache客户端
// 3. 配置本地缓存参数
// 4. 组装完整的缓存实例
//
// **参数说明：**
// - rdb: Redis通用客户端，支持单机/集群/哨兵模式
// - localCache: 本地缓存配置，包含主题、槽位等参数
// - friendDB: 好友数据库操作接口
// - options: RocksCache配置选项
//
// **配置特点：**
// - 多级缓存：Redis + 本地缓存的组合
// - 主题订阅：支持缓存失效的消息通知
// - 槽位管理：本地缓存的分片管理
// - 性能调优：根据好友系统特点优化缓存参数
//
// **返回值：**
// - cache.FriendCache: 好友缓存接口实现
func NewFriendCacheRedis(rdb redis.UniversalClient, localCache *config.LocalCache, friendDB database.Friend,
	options *rockscache.Options) cache.FriendCache {
	// **第一步：创建批量删除处理器**
	// 支持本地缓存失效通知，确保多实例间的缓存一致性
	batchHandler := NewBatchDeleterRedis(rdb, options, []string{localCache.Friend.Topic})

	// **第二步：获取本地缓存配置并记录日志**
	f := localCache.Friend
	log.ZDebug(context.Background(), "friend local cache init",
		"Topic", f.Topic, // 缓存失效通知主题
		"SlotNum", f.SlotNum, // 本地缓存槽位数量
		"SlotSize", f.SlotSize, // 每个槽位的大小
		"enable", f.Enable()) // 是否启用本地缓存

	// **第三步：返回完整的缓存实例**
	return &FriendCacheRedis{
		BatchDeleter: batchHandler,                        // 批量删除器
		friendDB:     friendDB,                            // 好友数据库接口
		expireTime:   friendExpireTime,                    // 统一过期时间
		rcClient:     rockscache.NewClient(rdb, *options), // RocksCache客户端
	}
}

// CloneFriendCache 克隆好友缓存实例
//
// **克隆目的：**
// 1. 支持链式调用的缓存删除操作
// 2. 避免并发操作时的状态污染
// 3. 实现事务性的缓存操作
//
// **克隆策略：**
// - 深拷贝：BatchDeleter需要独立的状态
// - 浅拷贝：数据库接口和客户端可以共享
// - 状态隔离：每个克隆实例维护独立的删除队列
//
// **应用场景：**
// - 事务性缓存删除：确保相关缓存的原子性删除
// - 批量操作：收集多个删除操作后统一执行
// - 错误回滚：操作失败时不影响原实例状态
//
// **返回值：**
// - cache.FriendCache: 新的好友缓存实例
func (f *FriendCacheRedis) CloneFriendCache() cache.FriendCache {
	return &FriendCacheRedis{
		BatchDeleter: f.BatchDeleter.Clone(), // 克隆批量删除器（深拷贝）
		friendDB:     f.friendDB,             // 共享好友数据库接口
		expireTime:   f.expireTime,           // 共享过期时间配置
		rcClient:     f.rcClient,             // 共享RocksCache客户端
	}
}

// ==================== 缓存键生成函数 ====================
// 以下函数负责生成各种类型的缓存键，统一管理缓存键的命名规则

// getFriendIDsKey 生成好友ID列表缓存键
// 格式：friend:ids:{ownerUserID}
// 用途：缓存用户的所有好友用户ID列表
func (f *FriendCacheRedis) getFriendIDsKey(ownerUserID string) string {
	return cachekey.GetFriendIDsKey(ownerUserID)
}

// getFriendMaxVersionKey 生成好友最大版本号缓存键
// 格式：friend:max_version:{ownerUserID}
// 用途：缓存用户好友关系的最大版本号，用于增量同步
func (f *FriendCacheRedis) getFriendMaxVersionKey(ownerUserID string) string {
	return cachekey.GetFriendMaxVersionKey(ownerUserID)
}

// getTwoWayFriendsIDsKey 生成双向好友ID列表缓存键
// 格式：friend:two_way:{ownerUserID}
// 用途：缓存与用户互为好友的用户ID列表
func (f *FriendCacheRedis) getTwoWayFriendsIDsKey(ownerUserID string) string {
	return cachekey.GetTwoWayFriendsIDsKey(ownerUserID)
}

// getFriendKey 生成好友信息缓存键
// 格式：friend:info:{ownerUserID}:{friendUserID}
// 用途：缓存特定好友关系的详细信息
func (f *FriendCacheRedis) getFriendKey(ownerUserID, friendUserID string) string {
	return cachekey.GetFriendKey(ownerUserID, friendUserID)
}

// ==================== 好友ID列表缓存操作 ====================

// GetFriendIDs 获取用户的好友ID列表
//
// **功能说明：**
// 获取指定用户的所有好友用户ID列表
//
// **数据特点：**
// - 只包含好友的用户ID，不包含详细信息
// - 数据量相对较小，访问频繁
// - 常用于好友关系验证和列表展示
//
// **应用场景：**
// - 好友列表展示：获取用户的好友列表
// - 权限验证：检查两个用户是否为好友关系
// - 消息路由：确定消息是否可以发送给好友
// - 社交功能：好友推荐、共同好友等功能
//
// **缓存策略：**
// - 优先从缓存获取，提高访问速度
// - 缓存未命中时查询数据库并自动缓存
// - 设置合理的过期时间，平衡性能和一致性
//
// **参数说明：**
// - ctx: 上下文信息
// - ownerUserID: 用户ID（好友关系的拥有者）
//
// **返回值：**
// - friendIDs: 好友用户ID列表
// - err: 错误信息
func (f *FriendCacheRedis) GetFriendIDs(ctx context.Context, ownerUserID string) (friendIDs []string, err error) {
	return getCache(ctx, f.rcClient, f.getFriendIDsKey(ownerUserID), f.expireTime, func(ctx context.Context) ([]string, error) {
		// **数据库查询：获取用户的所有好友用户ID**
		return f.friendDB.FindFriendUserIDs(ctx, ownerUserID)
	})
}

// DelFriendIDs 删除好友ID列表缓存
//
// **功能说明：**
// 批量删除指定用户的好友ID列表缓存
//
// **删除时机：**
// - 用户添加新好友时
// - 用户删除好友时
// - 好友关系状态变更时
// - 批量好友操作时
//
// **批量处理：**
// 支持同时删除多个用户的好友ID列表缓存
// 适用于好友关系变更影响多个用户的场景
//
// **影响范围：**
// 删除此缓存会影响：
// - 好友列表展示
// - 好友关系验证
// - 相关的社交功能
//
// **参数说明：**
// - ownerUserIDs: 用户ID列表（可变参数）
//
// **返回值：**
// - cache.FriendCache: 新的缓存实例，支持链式调用
func (f *FriendCacheRedis) DelFriendIDs(ownerUserIDs ...string) cache.FriendCache {
	newFriendCache := f.CloneFriendCache()
	keys := make([]string, 0, len(ownerUserIDs))
	for _, userID := range ownerUserIDs {
		keys = append(keys, f.getFriendIDsKey(userID))
	}
	newFriendCache.AddKeys(keys...)

	return newFriendCache
}

// ==================== 双向好友关系缓存操作 ====================

// GetTwoWayFriendIDs 获取双向好友ID列表
//
// **功能说明：**
// 获取与指定用户互为好友的用户ID列表
//
// **双向好友定义：**
// - A是B的好友，同时B也是A的好友
// - 这种关系具有对称性和互惠性
// - 通常用于更高级的社交功能
//
// **实现逻辑：**
// 1. 获取用户的所有好友ID列表
// 2. 遍历每个好友，检查对方是否也将用户作为好友
// 3. 收集所有双向好友关系的用户ID
//
// **应用场景：**
// - 亲密好友功能：显示互相关注的好友
// - 权限控制：某些功能只对双向好友开放
// - 社交推荐：基于双向好友进行推荐
// - 隐私设置：双向好友享有更多权限
//
// **性能考虑：**
// - 需要多次查询好友列表，计算开销较大
// - 适合缓存结果，避免重复计算
// - 可以考虑异步计算和更新
//
// **注意事项：**
// 当前实现中存在一个逻辑错误：应该添加friendID而不是ownerUserID到结果列表
//
// **参数说明：**
// - ctx: 上下文信息
// - ownerUserID: 用户ID
//
// **返回值：**
// - twoWayFriendIDs: 双向好友用户ID列表
// - err: 错误信息
func (f *FriendCacheRedis) GetTwoWayFriendIDs(ctx context.Context, ownerUserID string) (twoWayFriendIDs []string, err error) {
	// **第一步：获取用户的所有好友ID列表**
	friendIDs, err := f.GetFriendIDs(ctx, ownerUserID)
	if err != nil {
		return nil, err
	}

	// **第二步：检查每个好友是否也将用户作为好友**
	for _, friendID := range friendIDs {
		// **获取好友的好友列表**
		friendFriendID, err := f.GetFriendIDs(ctx, friendID)
		if err != nil {
			return nil, err
		}

		// **检查用户是否在好友的好友列表中**
		if datautil.Contain(ownerUserID, friendFriendID...) {
			// **注意：这里应该添加friendID，而不是ownerUserID**
			// 当前代码存在逻辑错误，应该修正为：
			// twoWayFriendIDs = append(twoWayFriendIDs, friendID)
			twoWayFriendIDs = append(twoWayFriendIDs, ownerUserID)
		}
	}

	return twoWayFriendIDs, nil
}

// DelTwoWayFriendIDs 删除双向好友ID列表缓存
//
// **功能说明：**
// 删除指定用户的双向好友ID列表缓存
//
// **删除时机：**
// - 用户的好友关系发生变化时
// - 双向好友关系被破坏时
// - 需要重新计算双向好友时
//
// **重要性：**
// 双向好友关系的计算成本较高，缓存的及时删除很重要
// 确保用户看到的双向好友信息是准确的
//
// **参数说明：**
// - ctx: 上下文信息（当前未使用，预留扩展）
// - ownerUserID: 用户ID
//
// **返回值：**
// - cache.FriendCache: 新的缓存实例，支持链式调用
func (f *FriendCacheRedis) DelTwoWayFriendIDs(ctx context.Context, ownerUserID string) cache.FriendCache {
	newFriendCache := f.CloneFriendCache()
	newFriendCache.AddKeys(f.getTwoWayFriendsIDsKey(ownerUserID))

	return newFriendCache
}

// ==================== 好友详细信息缓存操作 ====================

// GetFriend 获取好友详细信息
//
// **功能说明：**
// 获取指定用户与指定好友之间的详细好友关系信息
//
// **好友信息包含：**
// - 基本信息：用户ID、好友用户ID
// - 关系信息：好友状态、添加时间
// - 扩展信息：备注名、分组信息等
// - 权限信息：消息权限、可见性设置等
//
// **应用场景：**
// - 好友信息展示：显示好友的详细信息
// - 权限检查：验证好友关系的具体权限
// - 关系管理：好友关系的增删改查
// - 社交功能：基于好友关系的各种功能
//
// **缓存策略：**
// - 好友详细信息相对稳定，适合缓存
// - 缓存键包含双方用户ID，精确定位
// - 关系变更时需要及时清理缓存
//
// **错误处理：**
// - 好友关系不存在时返回ErrRecordNotFound
// - 用户不存在时返回相应错误
//
// **参数说明：**
// - ctx: 上下文信息
// - ownerUserID: 用户ID（好友关系的拥有者）
// - friendUserID: 好友用户ID
//
// **返回值：**
// - friend: 好友关系详细信息
// - err: 错误信息
func (f *FriendCacheRedis) GetFriend(ctx context.Context, ownerUserID, friendUserID string) (friend *model.Friend, err error) {
	return getCache(ctx, f.rcClient, f.getFriendKey(ownerUserID, friendUserID), f.expireTime, func(ctx context.Context) (*model.Friend, error) {
		// **数据库查询：获取好友关系的详细信息**
		return f.friendDB.Take(ctx, ownerUserID, friendUserID)
	})
}

// DelFriend 删除好友信息缓存
//
// **功能说明：**
// 删除指定好友关系的详细信息缓存
//
// **删除时机：**
// - 好友关系被删除时
// - 好友信息被更新时
// - 好友权限发生变更时
//
// **关系对称性：**
// 注意好友关系可能具有方向性，需要考虑是否同时删除反向关系的缓存
//
// **参数说明：**
// - ownerUserID: 用户ID
// - friendUserID: 好友用户ID
//
// **返回值：**
// - cache.FriendCache: 新的缓存实例，支持链式调用
func (f *FriendCacheRedis) DelFriend(ownerUserID, friendUserID string) cache.FriendCache {
	newFriendCache := f.CloneFriendCache()
	newFriendCache.AddKeys(f.getFriendKey(ownerUserID, friendUserID))

	return newFriendCache
}

// DelFriends 批量删除好友信息缓存
//
// **功能说明：**
// 批量删除指定用户与多个好友的关系信息缓存
//
// **应用场景：**
// - 用户注销时清理所有好友关系缓存
// - 批量好友操作时的缓存清理
// - 好友列表重构时的缓存重置
//
// **批量优势：**
// - 提高删除效率，减少操作次数
// - 保证相关缓存的一致性删除
// - 支持事务性的缓存清理
//
// **参数说明：**
// - ownerUserID: 用户ID
// - friendUserIDs: 好友用户ID列表
//
// **返回值：**
// - cache.FriendCache: 新的缓存实例，支持链式调用
func (f *FriendCacheRedis) DelFriends(ownerUserID string, friendUserIDs []string) cache.FriendCache {
	newFriendCache := f.CloneFriendCache()

	for _, friendUserID := range friendUserIDs {
		key := f.getFriendKey(ownerUserID, friendUserID)
		newFriendCache.AddKeys(key) // 添加缓存键到删除队列
	}

	return newFriendCache
}

// DelOwner 删除作为好友的用户相关缓存
//
// **功能说明：**
// 删除多个用户与指定好友用户的关系信息缓存
//
// **应用场景：**
// - 用户注销时，清理其他用户对该用户的好友关系缓存
// - 用户信息变更时，更新相关的好友关系缓存
// - 批量清理特定用户相关的好友缓存
//
// **关系方向：**
// 这个函数处理的是反向关系：其他用户将指定用户作为好友的关系
// 与DelFriends相对应，处理好友关系的另一个方向
//
// **参数说明：**
// - friendUserID: 好友用户ID（被其他用户添加为好友的用户）
// - ownerUserIDs: 用户ID列表（将friendUserID添加为好友的用户列表）
//
// **返回值：**
// - cache.FriendCache: 新的缓存实例，支持链式调用
func (f *FriendCacheRedis) DelOwner(friendUserID string, ownerUserIDs []string) cache.FriendCache {
	newFriendCache := f.CloneFriendCache()

	for _, ownerUserID := range ownerUserIDs {
		key := f.getFriendKey(ownerUserID, friendUserID)
		newFriendCache.AddKeys(key) // 添加缓存键到删除队列
	}

	return newFriendCache
}

// ==================== 版本控制缓存操作 ====================

// DelMaxFriendVersion 删除好友最大版本号缓存
//
// **功能说明：**
// 删除指定用户的好友关系最大版本号缓存
//
// **版本控制机制：**
// - 用户的好友关系发生变更时版本号会更新
// - 客户端通过版本号判断好友列表是否需要同步
// - 支持用户维度的增量同步
//
// **删除时机：**
// - 用户添加新好友时
// - 用户删除好友时
// - 好友关系状态变更时
// - 好友信息更新时
//
// **应用场景：**
// - 好友列表同步：客户端同步用户的好友列表
// - 变更检测：检测用户好友关系的变化
// - 数据一致性：保证用户好友数据的一致性
//
// **参数说明：**
// - ownerUserIDs: 用户ID列表（可变参数）
//
// **返回值：**
// - cache.FriendCache: 新的缓存实例，支持链式调用
func (f *FriendCacheRedis) DelMaxFriendVersion(ownerUserIDs ...string) cache.FriendCache {
	newFriendCache := f.CloneFriendCache()
	for _, ownerUserID := range ownerUserIDs {
		key := f.getFriendMaxVersionKey(ownerUserID)
		newFriendCache.AddKeys(key) // 添加版本缓存键到删除队列
	}

	return newFriendCache
}

// FindMaxFriendVersion 查找好友最大版本号
//
// **功能说明：**
// 获取指定用户的好友关系最大版本号信息
//
// **版本日志信息：**
// - DID: 数据标识（用户ID）
// - Version: 版本号
// - LogID: 日志ID
// - Time: 更新时间
//
// **应用场景：**
// - 增量同步：客户端比较版本号决定是否同步
// - 变更检测：服务端检测数据是否有更新
// - 同步优化：避免不必要的数据传输
//
// **缓存策略：**
// - 版本信息访问频繁，适合缓存
// - 数据变更时需要及时清理缓存
// - 支持高并发的版本检查需求
//
// **参数说明：**
// - ctx: 上下文信息
// - ownerUserID: 用户ID
//
// **返回值：**
// - *model.VersionLog: 版本日志信息
// - error: 错误信息
func (f *FriendCacheRedis) FindMaxFriendVersion(ctx context.Context, ownerUserID string) (*model.VersionLog, error) {
	return getCache(ctx, f.rcClient, f.getFriendMaxVersionKey(ownerUserID), f.expireTime, func(ctx context.Context) (*model.VersionLog, error) {
		// **数据库查询：获取用户好友关系的最大版本号**
		// 参数说明：ownerUserID, version=0, limit=0 表示获取最大版本
		return f.friendDB.FindIncrVersion(ctx, ownerUserID, 0, 0)
	})
}
