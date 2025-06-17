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

// Package redis 群组缓存Redis实现
//
// 本文件实现群组相关的Redis缓存操作，是群组数据缓存层的核心实现
//
// **功能概述：**
// ┌─────────────────────────────────────────────────────────┐
// │                群组缓存系统架构                          │
// ├─────────────────────────────────────────────────────────┤
// │ 1. 群组信息缓存：群组基本信息的高速缓存                   │
// │ 2. 成员关系缓存：群成员列表和关系的缓存管理               │
// │ 3. 权限角色缓存：不同角色级别成员的分类缓存               │
// │ 4. 哈希值缓存：群成员变更的快速检测机制                   │
// │ 5. 版本控制缓存：增量同步的版本管理                       │
// └─────────────────────────────────────────────────────────┘
//
// **缓存策略：**
// - 多级缓存：Redis + RocksCache + 本地缓存
// - 智能失效：链式删除相关缓存键
// - 批量优化：使用batchGetCache2进行槽位优化
// - 版本管理：支持增量更新和冲突检测
//
// **性能特点：**
// - 缓存命中率优化：热点数据常驻内存
// - 批量操作优化：减少网络往返次数
// - 槽位分组：Redis集群环境下的性能优化
// - 异步更新：后台异步更新缓存数据
package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/dtm-labs/rockscache"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache/cachekey"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/common"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/redis/go-redis/v9"
)

const (
	// groupExpireTime 群组缓存过期时间：12小时
	// 设计考虑：群组信息相对稳定，12小时的缓存时间平衡了性能和数据一致性
	groupExpireTime = time.Second * 60 * 60 * 12
)

// errIndex 索引错误，用于批量操作中的错误处理
var errIndex = errs.New("err index")

// GroupCacheRedis 群组缓存Redis实现
//
// **架构设计：**
// ┌─────────────────────────────────────────────────────────┐
// │                GroupCacheRedis                         │
// ├─────────────────────────────────────────────────────────┤
// │ BatchDeleter: 批量删除器                                │
// │   - 支持链式调用的缓存删除                               │
// │   - 批量删除优化性能                                     │
// │   - 事务性删除保证一致性                                 │
// ├─────────────────────────────────────────────────────────┤
// │ 数据库接口层:                                            │
// │   - groupDB: 群组基础数据操作                           │
// │   - groupMemberDB: 群成员数据操作                       │
// │   - groupRequestDB: 群申请数据操作                      │
// ├─────────────────────────────────────────────────────────┤
// │ 缓存客户端:                                              │
// │   - rcClient: RocksCache客户端                          │
// │   - expireTime: 统一的过期时间管理                       │
// │   - groupHash: 群组哈希计算接口                         │
// └─────────────────────────────────────────────────────────┘
//
// **职责分工：**
// - 缓存管理：负责所有群组相关数据的缓存操作
// - 数据一致性：确保缓存与数据库的数据一致性
// - 性能优化：通过批量操作和智能缓存提升性能
// - 版本控制：支持增量同步和版本管理
type GroupCacheRedis struct {
	cache.BatchDeleter                       // 批量删除器，支持链式缓存删除操作
	groupDB            database.Group        // 群组数据库操作接口
	groupMemberDB      database.GroupMember  // 群成员数据库操作接口
	groupRequestDB     database.GroupRequest // 群申请数据库操作接口
	expireTime         time.Duration         // 缓存过期时间
	rcClient           *rockscache.Client    // RocksCache客户端，提供分布式缓存功能
	groupHash          cache.GroupHash       // 群组哈希计算接口，用于检测成员变更
}

// NewGroupCacheRedis 创建群组缓存Redis实例
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
// - groupDB: 群组数据库操作接口
// - groupMemberDB: 群成员数据库操作接口
// - groupRequestDB: 群申请数据库操作接口
// - hashCode: 群组哈希计算接口
// - opts: RocksCache配置选项
//
// **配置特点：**
// - 多级缓存：Redis + 本地缓存的组合
// - 主题订阅：支持缓存失效的消息通知
// - 槽位管理：本地缓存的分片管理
// - 性能调优：根据业务特点优化缓存参数
func NewGroupCacheRedis(
	rdb redis.UniversalClient,
	localCache *config.LocalCache,
	groupDB database.Group,
	groupMemberDB database.GroupMember,
	groupRequestDB database.GroupRequest,
	hashCode cache.GroupHash,
	opts *rockscache.Options,
) cache.GroupCache {
	// **第一步：创建批量删除处理器**
	// 支持本地缓存失效通知，确保多实例间的缓存一致性
	batchHandler := NewBatchDeleterRedis(rdb, opts, []string{localCache.Group.Topic})

	// **第二步：获取本地缓存配置并记录日志**
	g := localCache.Group
	log.ZDebug(context.Background(), "group local cache init",
		"Topic", g.Topic, // 缓存失效通知主题
		"SlotNum", g.SlotNum, // 本地缓存槽位数量
		"SlotSize", g.SlotSize, // 每个槽位的大小
		"enable", g.Enable()) // 是否启用本地缓存

	// **第三步：返回完整的缓存实例**
	return &GroupCacheRedis{
		BatchDeleter:   batchHandler,                     // 批量删除器
		rcClient:       rockscache.NewClient(rdb, *opts), // RocksCache客户端
		expireTime:     groupExpireTime,                  // 统一过期时间
		groupDB:        groupDB,                          // 群组数据库接口
		groupMemberDB:  groupMemberDB,                    // 群成员数据库接口
		groupRequestDB: groupRequestDB,                   // 群申请数据库接口
		groupHash:      hashCode,                         // 群组哈希接口
	}
}

// CloneGroupCache 克隆群组缓存实例
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
func (g *GroupCacheRedis) CloneGroupCache() cache.GroupCache {
	return &GroupCacheRedis{
		BatchDeleter:   g.BatchDeleter.Clone(), // 克隆批量删除器（深拷贝）
		rcClient:       g.rcClient,             // 共享RocksCache客户端
		expireTime:     g.expireTime,           // 共享过期时间配置
		groupDB:        g.groupDB,              // 共享群组数据库接口
		groupMemberDB:  g.groupMemberDB,        // 共享群成员数据库接口
		groupRequestDB: g.groupRequestDB,       // 共享群申请数据库接口
	}
}

// ==================== 缓存键生成函数 ====================
// 以下函数负责生成各种类型的缓存键，统一管理缓存键的命名规则

// getGroupInfoKey 生成群组信息缓存键
// 格式：group:info:{groupID}
// 用途：缓存群组的基本信息（名称、描述、设置等）
func (g *GroupCacheRedis) getGroupInfoKey(groupID string) string {
	return cachekey.GetGroupInfoKey(groupID)
}

// getJoinedGroupsKey 生成用户加入群组列表缓存键
// 格式：user:joined_groups:{userID}
// 用途：缓存用户参与的所有群组ID列表
func (g *GroupCacheRedis) getJoinedGroupsKey(userID string) string {
	return cachekey.GetJoinedGroupsKey(userID)
}

// getGroupMembersHashKey 生成群成员哈希值缓存键
// 格式：group:members:hash:{groupID}
// 用途：缓存群成员列表的哈希值，用于快速检测成员变更
func (g *GroupCacheRedis) getGroupMembersHashKey(groupID string) string {
	return cachekey.GetGroupMembersHashKey(groupID)
}

// getGroupMemberIDsKey 生成群成员ID列表缓存键
// 格式：group:member_ids:{groupID}
// 用途：缓存群组中所有成员的用户ID列表
func (g *GroupCacheRedis) getGroupMemberIDsKey(groupID string) string {
	return cachekey.GetGroupMemberIDsKey(groupID)
}

// getGroupMemberInfoKey 生成群成员信息缓存键
// 格式：group:member:{groupID}:{userID}
// 用途：缓存特定用户在特定群组中的成员信息
func (g *GroupCacheRedis) getGroupMemberInfoKey(groupID, userID string) string {
	return cachekey.GetGroupMemberInfoKey(groupID, userID)
}

// getGroupMemberNumKey 生成群成员数量缓存键
// 格式：group:member_num:{groupID}
// 用途：缓存群组的成员总数，避免频繁的计数查询
func (g *GroupCacheRedis) getGroupMemberNumKey(groupID string) string {
	return cachekey.GetGroupMemberNumKey(groupID)
}

// getGroupRoleLevelMemberIDsKey 生成特定角色级别成员ID列表缓存键
// 格式：group:role:{groupID}:{roleLevel}
// 用途：缓存群组中特定角色（群主、管理员、普通成员）的用户ID列表
func (g *GroupCacheRedis) getGroupRoleLevelMemberIDsKey(groupID string, roleLevel int32) string {
	return cachekey.GetGroupRoleLevelMemberIDsKey(groupID, roleLevel)
}

// getGroupMemberMaxVersionKey 生成群成员最大版本号缓存键
// 格式：group:member:max_version:{groupID}
// 用途：缓存群成员的最大版本号，用于增量同步
func (g *GroupCacheRedis) getGroupMemberMaxVersionKey(groupID string) string {
	return cachekey.GetGroupMemberMaxVersionKey(groupID)
}

// getJoinGroupMaxVersionKey 生成用户加群最大版本号缓存键
// 格式：user:join_group:max_version:{userID}
// 用途：缓存用户加入群组的最大版本号，用于增量同步
func (g *GroupCacheRedis) getJoinGroupMaxVersionKey(userID string) string {
	return cachekey.GetJoinGroupMaxVersionKey(userID)
}

// getGroupID 从群组对象中提取群组ID
// 用途：为batchGetCache2函数提供ID提取器
func (g *GroupCacheRedis) getGroupID(group *model.Group) string {
	return group.GroupID
}

// ==================== 群组信息缓存操作 ====================

// GetGroupsInfo 批量获取群组信息
//
// **功能说明：**
// 使用高性能的批量缓存获取机制，支持Redis集群的槽位优化
//
// **核心特性：**
// 1. 槽位优化：使用batchGetCache2进行Redis集群槽位分组
// 2. 缓存穿透保护：使用RocksCache的分布式锁机制
// 3. 批量查询：减少数据库访问次数
// 4. 自动缓存：查询结果自动写入缓存
//
// **处理流程：**
// 1. 按Redis槽位对群组ID进行分组
// 2. 并发查询各槽位的缓存数据
// 3. 对于缓存未命中的数据，批量查询数据库
// 4. 将查询结果写入缓存并返回
//
// **性能优化：**
// - 网络往返：从O(n)优化到O(槽位数)
// - 并发处理：不同槽位并行处理
// - 缓存命中：热点数据快速返回
//
// **参数说明：**
// - ctx: 上下文，用于取消操作和传递请求信息
// - groupIDs: 要查询的群组ID列表
//
// **返回值：**
// - groups: 查询到的群组信息列表
// - err: 错误信息
func (g *GroupCacheRedis) GetGroupsInfo(ctx context.Context, groupIDs []string) (groups []*model.Group, err error) {
	return batchGetCache2(ctx, g.rcClient, g.expireTime, groupIDs, g.getGroupInfoKey, g.getGroupID, g.groupDB.Find)
}

// GetGroupInfo 获取单个群组信息
//
// **功能说明：**
// 从缓存中获取单个群组的详细信息，缓存未命中时查询数据库
//
// **缓存策略：**
// 1. 优先从缓存获取数据
// 2. 缓存未命中时查询数据库
// 3. 查询结果自动写入缓存
// 4. 设置合理的过期时间
//
// **错误处理：**
// - 群组不存在：返回ErrRecordNotFound错误
// - 缓存错误：降级到数据库查询
// - 数据库错误：直接返回错误
//
// **性能特点：**
// - 缓存命中：毫秒级响应
// - 缓存未命中：自动回源并缓存
// - 分布式锁：防止缓存击穿
//
// **参数说明：**
// - ctx: 上下文信息
// - groupID: 群组唯一标识
//
// **返回值：**
// - group: 群组详细信息
// - err: 错误信息
func (g *GroupCacheRedis) GetGroupInfo(ctx context.Context, groupID string) (group *model.Group, err error) {
	return getCache(ctx, g.rcClient, g.getGroupInfoKey(groupID), g.expireTime, func(ctx context.Context) (*model.Group, error) {
		return g.groupDB.Take(ctx, groupID)
	})
}

// DelGroupsInfo 删除群组信息缓存
//
// **功能说明：**
// 批量删除指定群组的信息缓存，支持链式调用
//
// **删除策略：**
// 1. 克隆当前缓存实例，避免状态污染
// 2. 收集所有需要删除的缓存键
// 3. 添加到批量删除队列
// 4. 返回新的缓存实例，支持链式调用
//
// **应用场景：**
// - 群组信息更新后删除旧缓存
// - 群组解散时清理相关缓存
// - 批量操作时的缓存一致性维护
//
// **链式调用示例：**
// ```go
// cache.DelGroupsInfo("group1", "group2").
//
//	DelGroupMembersHash("group1").
//	ChainExecDel(ctx)
//
// ```
//
// **参数说明：**
// - groupIDs: 要删除缓存的群组ID列表（可变参数）
//
// **返回值：**
// - cache.GroupCache: 新的缓存实例，支持链式调用
func (g *GroupCacheRedis) DelGroupsInfo(groupIDs ...string) cache.GroupCache {
	// **第一步：克隆缓存实例**
	// 避免修改原实例的状态，支持并发安全的操作
	newGroupCache := g.CloneGroupCache()

	// **第二步：构建缓存键列表**
	keys := make([]string, 0, len(groupIDs))
	for _, groupID := range groupIDs {
		keys = append(keys, g.getGroupInfoKey(groupID))
	}

	// **第三步：添加到删除队列**
	newGroupCache.AddKeys(keys...)

	return newGroupCache
}

// DelGroupsOwner 删除群主信息缓存
//
// **功能说明：**
// 批量删除指定群组的群主角色缓存
//
// **删除原理：**
// 群主信息实际上是角色级别为GroupOwner的成员信息缓存
// 通过删除对应的角色级别缓存来实现群主信息的删除
//
// **应用场景：**
// - 群主转移后清理旧群主缓存
// - 群组解散时清理群主信息
// - 权限变更时的缓存维护
//
// **实现细节：**
// - 使用constant.GroupOwner常量确保角色级别正确
// - 支持批量删除多个群组的群主缓存
// - 返回新实例支持链式调用
//
// **参数说明：**
// - groupIDs: 要删除群主缓存的群组ID列表
//
// **返回值：**
// - cache.GroupCache: 新的缓存实例
func (g *GroupCacheRedis) DelGroupsOwner(groupIDs ...string) cache.GroupCache {
	newGroupCache := g.CloneGroupCache()
	keys := make([]string, 0, len(groupIDs))
	for _, groupID := range groupIDs {
		// 删除群主角色级别的成员ID缓存
		keys = append(keys, g.getGroupRoleLevelMemberIDsKey(groupID, constant.GroupOwner))
	}
	newGroupCache.AddKeys(keys...)

	return newGroupCache
}

// DelGroupRoleLevel 删除指定角色级别的成员缓存
//
// **功能说明：**
// 删除群组中指定角色级别的成员ID列表缓存
//
// **角色级别说明：**
// - constant.GroupOwner: 群主
// - constant.GroupAdmin: 管理员
// - constant.GroupOrdinaryUsers: 普通成员
//
// **应用场景：**
// - 成员角色变更时清理旧角色缓存
// - 成员退群时清理角色相关缓存
// - 权限系统更新时的缓存维护
//
// **批量处理：**
// 支持同时删除多个角色级别的缓存，提高操作效率
//
// **参数说明：**
// - groupID: 群组ID
// - roleLevels: 要删除的角色级别列表
//
// **返回值：**
// - cache.GroupCache: 新的缓存实例
func (g *GroupCacheRedis) DelGroupRoleLevel(groupID string, roleLevels []int32) cache.GroupCache {
	newGroupCache := g.CloneGroupCache()
	keys := make([]string, 0, len(roleLevels))
	for _, roleLevel := range roleLevels {
		keys = append(keys, g.getGroupRoleLevelMemberIDsKey(groupID, roleLevel))
	}
	newGroupCache.AddKeys(keys...)
	return newGroupCache
}

// DelGroupAllRoleLevel 删除群组所有角色级别的缓存
//
// **功能说明：**
// 一次性删除群组中所有角色级别的成员缓存
//
// **删除范围：**
// - 群主(GroupOwner)缓存
// - 管理员(GroupAdmin)缓存
// - 普通成员(GroupOrdinaryUsers)缓存
//
// **应用场景：**
// - 群组解散时的全面缓存清理
// - 群组重构时的缓存重置
// - 权限系统重建时的缓存清空
//
// **实现方式：**
// 调用DelGroupRoleLevel方法，传入所有角色级别常量
//
// **参数说明：**
// - groupID: 群组ID
//
// **返回值：**
// - cache.GroupCache: 新的缓存实例
func (g *GroupCacheRedis) DelGroupAllRoleLevel(groupID string) cache.GroupCache {
	return g.DelGroupRoleLevel(groupID, []int32{constant.GroupOwner, constant.GroupAdmin, constant.GroupOrdinaryUsers})
}

// ==================== 群成员哈希缓存操作 ====================

// GetGroupMembersHash 获取群成员哈希值
//
// **功能说明：**
// 获取群组成员列表的哈希值，用于快速检测群成员是否发生变更
//
// **哈希机制：**
// 1. 基于群组所有成员的用户ID计算哈希值
// 2. 成员增减或角色变更都会导致哈希值改变
// 3. 客户端可通过比较哈希值判断是否需要同步
//
// **应用场景：**
// - 客户端增量同步：比较哈希值决定是否拉取最新成员列表
// - 变更检测：快速判断群成员是否有变化
// - 缓存验证：验证本地缓存的群成员数据是否有效
//
// **性能优势：**
// - 轻量级检测：只需传输一个哈希值而非完整成员列表
// - 快速比较：哈希值比较比成员列表比较快得多
// - 网络优化：减少不必要的数据传输
//
// **错误处理：**
// - groupHash为nil时返回内部服务器错误
// - 缓存未命中时自动查询并缓存哈希值
//
// **参数说明：**
// - ctx: 上下文信息
// - groupID: 群组ID
//
// **返回值：**
// - hashCode: 群成员列表的哈希值
// - err: 错误信息
func (g *GroupCacheRedis) GetGroupMembersHash(ctx context.Context, groupID string) (hashCode uint64, err error) {
	// **安全检查：确保哈希计算接口可用**
	if g.groupHash == nil {
		return 0, errs.ErrInternalServer.WrapMsg("group hash is nil")
	}

	// **缓存获取：优先从缓存获取哈希值**
	return getCache(ctx, g.rcClient, g.getGroupMembersHashKey(groupID), g.expireTime, func(ctx context.Context) (uint64, error) {
		// **回源计算：缓存未命中时重新计算哈希值**
		return g.groupHash.GetGroupHash(ctx, groupID)
	})
}

// GetGroupMemberHashMap 批量获取群组成员哈希映射
//
// **功能说明：**
// 批量获取多个群组的成员哈希值和成员数量，返回简化的群组信息
//
// **返回数据结构：**
// ```go
//
//	map[string]*common.GroupSimpleUserID{
//	    "groupID1": {Hash: 12345, MemberNum: 100},
//	    "groupID2": {Hash: 67890, MemberNum: 50},
//	}
//
// ```
//
// **应用场景：**
// - 批量同步检查：客户端批量检查多个群组是否需要同步
// - 群组列表展示：显示群组基本信息（成员数量等）
// - 性能优化：一次请求获取多个群组的关键信息
//
// **处理流程：**
// 1. 遍历所有群组ID
// 2. 并发获取每个群组的哈希值和成员数量
// 3. 组装成映射结构返回
//
// **错误处理：**
// - 任一群组获取失败都会导致整个操作失败
// - 确保数据的完整性和一致性
//
// **性能特点：**
// - 并发获取：多个群组的数据并行获取
// - 缓存优化：充分利用已有的缓存数据
// - 批量返回：减少网络往返次数
//
// **参数说明：**
// - ctx: 上下文信息
// - groupIDs: 群组ID列表
//
// **返回值：**
// - map[string]*common.GroupSimpleUserID: 群组ID到简化信息的映射
// - error: 错误信息
func (g *GroupCacheRedis) GetGroupMemberHashMap(ctx context.Context, groupIDs []string) (map[string]*common.GroupSimpleUserID, error) {
	// **安全检查：确保哈希计算接口可用**
	if g.groupHash == nil {
		return nil, errs.ErrInternalServer.WrapMsg("group hash is nil")
	}

	// **初始化结果映射**
	res := make(map[string]*common.GroupSimpleUserID)

	// **遍历处理每个群组**
	for _, groupID := range groupIDs {
		// **获取群组成员哈希值**
		hash, err := g.GetGroupMembersHash(ctx, groupID)
		if err != nil {
			return nil, err
		}

		// **记录调试日志**
		log.ZDebug(ctx, "GetGroupMemberHashMap", "groupID", groupID, "hash", hash)

		// **获取群组成员数量**
		num, err := g.GetGroupMemberNum(ctx, groupID)
		if err != nil {
			return nil, err
		}

		// **组装简化信息**
		res[groupID] = &common.GroupSimpleUserID{
			Hash:      hash,        // 成员列表哈希值
			MemberNum: uint32(num), // 成员数量
		}
	}

	return res, nil
}

// DelGroupMembersHash 删除群成员哈希缓存
//
// **功能说明：**
// 删除指定群组的成员哈希值缓存
//
// **删除时机：**
// - 群成员增加或删除时
// - 群成员角色变更时
// - 群组信息更新时
// - 任何可能影响成员列表的操作
//
// **重要性：**
// 哈希值缓存的及时删除确保客户端能够检测到成员变更
// 避免客户端使用过期的哈希值进行同步判断
//
// **参数说明：**
// - groupID: 群组ID
//
// **返回值：**
// - cache.GroupCache: 新的缓存实例，支持链式调用
func (g *GroupCacheRedis) DelGroupMembersHash(groupID string) cache.GroupCache {
	cache := g.CloneGroupCache()
	cache.AddKeys(g.getGroupMembersHashKey(groupID))

	return cache
}

// ==================== 群成员ID列表缓存操作 ====================

// GetGroupMemberIDs 获取群组成员ID列表
//
// **功能说明：**
// 获取群组中所有成员的用户ID列表
//
// **数据特点：**
// - 只包含用户ID，不包含详细的成员信息
// - 数据量相对较小，适合频繁访问
// - 常用于权限检查和成员遍历
//
// **应用场景：**
// - 权限验证：检查用户是否为群成员
// - 消息推送：获取需要推送消息的用户列表
// - 成员统计：计算群组成员数量
// - 批量操作：作为其他操作的输入参数
//
// **缓存策略：**
// - 优先从缓存获取，提高访问速度
// - 缓存未命中时查询数据库并自动缓存
// - 设置合理的过期时间，平衡性能和一致性
//
// **参数说明：**
// - ctx: 上下文信息
// - groupID: 群组ID
//
// **返回值：**
// - groupMemberIDs: 群成员用户ID列表
// - err: 错误信息
func (g *GroupCacheRedis) GetGroupMemberIDs(ctx context.Context, groupID string) (groupMemberIDs []string, err error) {
	return getCache(ctx, g.rcClient, g.getGroupMemberIDsKey(groupID), g.expireTime, func(ctx context.Context) ([]string, error) {
		// **数据库查询：获取群组中所有成员的用户ID**
		return g.groupMemberDB.FindMemberUserID(ctx, groupID)
	})
}

// DelGroupMemberIDs 删除群成员ID列表缓存
//
// **功能说明：**
// 删除指定群组的成员ID列表缓存
//
// **删除时机：**
// - 有新成员加入群组时
// - 有成员退出群组时
// - 群组解散时
// - 成员信息批量更新时
//
// **影响范围：**
// 删除此缓存会影响所有依赖成员ID列表的功能
// 包括权限检查、消息推送、成员统计等
//
// **参数说明：**
// - groupID: 群组ID
//
// **返回值：**
// - cache.GroupCache: 新的缓存实例，支持链式调用
func (g *GroupCacheRedis) DelGroupMemberIDs(groupID string) cache.GroupCache {
	cache := g.CloneGroupCache()
	cache.AddKeys(g.getGroupMemberIDsKey(groupID))

	return cache
}

// ==================== 用户加群列表缓存操作 ====================

// findUserJoinedGroupID 查找用户加入的群组ID列表（内部方法）
//
// **功能说明：**
// 从数据库查询用户加入的群组，并按照特定顺序排序
//
// **查询流程：**
// 1. 从群成员表查询用户参与的所有群组ID
// 2. 调用群组表的排序方法对群组ID进行排序
// 3. 返回排序后的群组ID列表
//
// **排序意义：**
// - 提供一致的群组显示顺序
// - 可能按照加入时间、活跃度等维度排序
// - 提升用户体验的一致性
//
// **参数说明：**
// - ctx: 上下文信息
// - userID: 用户ID
//
// **返回值：**
// - []string: 排序后的群组ID列表
// - error: 错误信息
func (g *GroupCacheRedis) findUserJoinedGroupID(ctx context.Context, userID string) ([]string, error) {
	// **第一步：查询用户参与的群组ID**
	groupIDs, err := g.groupMemberDB.FindUserJoinedGroupID(ctx, userID)
	if err != nil {
		return nil, err
	}

	// **第二步：对群组ID进行排序**
	return g.groupDB.FindJoinSortGroupID(ctx, groupIDs)
}

// GetJoinedGroupIDs 获取用户加入的群组ID列表
//
// **功能说明：**
// 获取用户参与的所有群组ID列表，按特定顺序排序
//
// **数据特点：**
// - 包含用户作为成员的所有群组
// - 按照一定规则排序（如加入时间、活跃度等）
// - 数据相对稳定，适合缓存
//
// **应用场景：**
// - 用户群组列表展示
// - 消息路由：确定用户需要接收哪些群组消息
// - 权限检查：验证用户对特定群组的访问权限
// - 统计分析：用户的群组参与情况
//
// **缓存策略：**
// - 用户群组列表变化不频繁，适合较长时间缓存
// - 缓存键以用户ID为维度，便于用户相关操作
//
// **性能优化：**
// - 避免每次都查询数据库进行排序
// - 减少复杂的关联查询开销
//
// **参数说明：**
// - ctx: 上下文信息
// - userID: 用户ID
//
// **返回值：**
// - joinedGroupIDs: 用户加入的群组ID列表（已排序）
// - err: 错误信息
func (g *GroupCacheRedis) GetJoinedGroupIDs(ctx context.Context, userID string) (joinedGroupIDs []string, err error) {
	return getCache(ctx, g.rcClient, g.getJoinedGroupsKey(userID), g.expireTime, func(ctx context.Context) ([]string, error) {
		// **调用内部方法获取排序后的群组ID列表**
		return g.findUserJoinedGroupID(ctx, userID)
	})
}

// DelJoinedGroupID 删除用户加群列表缓存
//
// **功能说明：**
// 批量删除指定用户的加群列表缓存
//
// **删除时机：**
// - 用户加入新群组时
// - 用户退出群组时
// - 用户被踢出群组时
// - 群组解散影响到用户时
//
// **批量处理：**
// 支持同时删除多个用户的加群列表缓存
// 适用于群组解散等影响多个用户的场景
//
// **影响范围：**
// 删除此缓存会影响用户的群组列表显示
// 下次访问时会重新从数据库加载最新数据
//
// **参数说明：**
// - userIDs: 用户ID列表（可变参数）
//
// **返回值：**
// - cache.GroupCache: 新的缓存实例，支持链式调用
func (g *GroupCacheRedis) DelJoinedGroupID(userIDs ...string) cache.GroupCache {
	keys := make([]string, 0, len(userIDs))
	for _, userID := range userIDs {
		keys = append(keys, g.getJoinedGroupsKey(userID))
	}
	cache := g.CloneGroupCache()
	cache.AddKeys(keys...)

	return cache
}

// ==================== 群成员详细信息缓存操作 ====================

// GetGroupMemberInfo 获取单个群成员信息
//
// **功能说明：**
// 获取指定用户在指定群组中的详细成员信息
//
// **成员信息包含：**
// - 基本信息：用户ID、群组ID、昵称等
// - 角色信息：角色级别（群主/管理员/普通成员）
// - 时间信息：加入时间、最后活跃时间等
// - 状态信息：禁言状态、权限设置等
//
// **应用场景：**
// - 权限检查：验证用户在群组中的角色和权限
// - 信息展示：显示群成员的详细信息
// - 操作验证：确认用户是否有权限执行特定操作
// - 审计日志：记录成员相关的操作历史
//
// **缓存策略：**
// - 成员信息相对稳定，适合缓存
// - 缓存键包含群组ID和用户ID，精确定位
// - 角色变更时需要及时清理缓存
//
// **错误处理：**
// - 用户不是群成员时返回ErrRecordNotFound
// - 群组不存在时返回相应错误
//
// **参数说明：**
// - ctx: 上下文信息
// - groupID: 群组ID
// - userID: 用户ID
//
// **返回值：**
// - groupMember: 群成员详细信息
// - err: 错误信息
func (g *GroupCacheRedis) GetGroupMemberInfo(ctx context.Context, groupID, userID string) (groupMember *model.GroupMember, err error) {
	return getCache(ctx, g.rcClient, g.getGroupMemberInfoKey(groupID, userID), g.expireTime, func(ctx context.Context) (*model.GroupMember, error) {
		// **数据库查询：获取群成员的详细信息**
		return g.groupMemberDB.Take(ctx, groupID, userID)
	})
}

// GetGroupMembersInfo 批量获取群成员信息
//
// **功能说明：**
// 批量获取指定群组中多个用户的成员信息
//
// **批量优化：**
// 1. 使用batchGetCache2进行Redis集群槽位优化
// 2. 减少网络往返次数，提高查询效率
// 3. 支持缓存穿透保护和分布式锁
//
// **处理流程：**
// 1. 构建每个用户的缓存键
// 2. 按Redis槽位对缓存键进行分组
// 3. 并发查询各槽位的缓存数据
// 4. 对于缓存未命中的用户，批量查询数据库
// 5. 将查询结果写入缓存并返回
//
// **应用场景：**
// - 群成员列表展示：显示群组中所有成员信息
// - 权限批量检查：批量验证多个用户的权限
// - 消息推送：获取需要推送的用户详细信息
// - 统计分析：分析群成员的角色分布等
//
// **性能特点：**
// - 槽位优化：Redis集群环境下的性能优化
// - 批量处理：减少数据库查询次数
// - 并发安全：使用分布式锁防止缓存击穿
//
// **参数说明：**
// - ctx: 上下文信息
// - groupID: 群组ID
// - userIDs: 用户ID列表
//
// **返回值：**
// - []*model.GroupMember: 群成员信息列表
// - error: 错误信息
func (g *GroupCacheRedis) GetGroupMembersInfo(ctx context.Context, groupID string, userIDs []string) ([]*model.GroupMember, error) {
	return batchGetCache2(ctx, g.rcClient, g.expireTime, userIDs,
		// **缓存键生成函数**
		func(userID string) string {
			return g.getGroupMemberInfoKey(groupID, userID)
		},
		// **ID提取函数**
		func(member *model.GroupMember) string {
			return member.UserID
		},
		// **数据库查询函数**
		func(ctx context.Context, userIDs []string) ([]*model.GroupMember, error) {
			return g.groupMemberDB.Find(ctx, groupID, userIDs)
		})
}

// GetAllGroupMembersInfo 获取群组所有成员信息
//
// **功能说明：**
// 获取指定群组中所有成员的详细信息
//
// **实现策略：**
// 1. 先获取群组的所有成员ID列表
// 2. 再批量获取这些成员的详细信息
// 3. 充分利用已有的缓存机制
//
// **两阶段查询的优势：**
// - 复用现有缓存：成员ID列表和成员详细信息都有独立缓存
// - 灵活性：可以根据需要只获取ID或详细信息
// - 一致性：确保获取的是最新的成员列表
//
// **应用场景：**
// - 群成员管理：管理员查看所有成员信息
// - 全员操作：需要对所有成员执行某些操作
// - 数据导出：导出群组的完整成员信息
// - 统计分析：分析群组的成员构成
//
// **性能考虑：**
// - 大群组可能返回大量数据，需要考虑分页
// - 利用批量查询优化性能
// - 缓存机制减少数据库压力
//
// **参数说明：**
// - ctx: 上下文信息
// - groupID: 群组ID
//
// **返回值：**
// - groupMembers: 所有群成员信息列表
// - err: 错误信息
func (g *GroupCacheRedis) GetAllGroupMembersInfo(ctx context.Context, groupID string) (groupMembers []*model.GroupMember, err error) {
	// **第一步：获取群组所有成员的用户ID列表**
	groupMemberIDs, err := g.GetGroupMemberIDs(ctx, groupID)
	if err != nil {
		return nil, err
	}

	// **第二步：批量获取这些用户的详细成员信息**
	return g.GetGroupMembersInfo(ctx, groupID, groupMemberIDs)
}

// DelGroupMembersInfo 删除群成员信息缓存
//
// **功能说明：**
// 批量删除指定群组中多个用户的成员信息缓存
//
// **删除时机：**
// - 成员退出群组时
// - 成员被踢出群组时
// - 成员角色发生变更时
// - 成员信息更新时
//
// **批量删除优势：**
// - 提高删除效率，减少操作次数
// - 保证相关缓存的一致性删除
// - 支持事务性的缓存清理
//
// **影响范围：**
// 删除成员信息缓存会影响：
// - 权限检查功能
// - 成员信息展示
// - 角色相关的操作验证
//
// **参数说明：**
// - groupID: 群组ID
// - userIDs: 要删除缓存的用户ID列表（可变参数）
//
// **返回值：**
// - cache.GroupCache: 新的缓存实例，支持链式调用
func (g *GroupCacheRedis) DelGroupMembersInfo(groupID string, userIDs ...string) cache.GroupCache {
	keys := make([]string, 0, len(userIDs))
	for _, userID := range userIDs {
		keys = append(keys, g.getGroupMemberInfoKey(groupID, userID))
	}
	cache := g.CloneGroupCache()
	cache.AddKeys(keys...)

	return cache
}

// ==================== 群成员数量缓存操作 ====================

// GetGroupMemberNum 获取群成员数量
//
// **功能说明：**
// 获取指定群组的成员总数
//
// **数据特点：**
// - 数据量小，访问频繁
// - 变化相对不频繁，适合缓存
// - 常用于群组信息展示和权限检查
//
// **应用场景：**
// - 群组信息展示：显示群组的成员数量
// - 权限检查：验证群组是否达到成员上限
// - 统计分析：群组规模的统计
// - 业务逻辑：基于成员数量的业务判断
//
// **缓存策略：**
// - 高频访问数据，优先从缓存获取
// - 成员变动时需要及时更新缓存
// - 设置合理的过期时间
//
// **性能优化：**
// - 避免每次都执行COUNT查询
// - 减少数据库的计算压力
// - 提供毫秒级的响应速度
//
// **参数说明：**
// - ctx: 上下文信息
// - groupID: 群组ID
//
// **返回值：**
// - memberNum: 群成员数量
// - err: 错误信息
func (g *GroupCacheRedis) GetGroupMemberNum(ctx context.Context, groupID string) (memberNum int64, err error) {
	return getCache(ctx, g.rcClient, g.getGroupMemberNumKey(groupID), g.expireTime, func(ctx context.Context) (int64, error) {
		// **数据库查询：统计群组的成员数量**
		return g.groupMemberDB.TakeGroupMemberNum(ctx, groupID)
	})
}

// DelGroupsMemberNum 删除群成员数量缓存
//
// **功能说明：**
// 批量删除指定群组的成员数量缓存
//
// **删除时机：**
// - 有新成员加入群组时
// - 有成员退出群组时
// - 群组解散时
// - 成员数量发生任何变化时
//
// **重要性：**
// 成员数量缓存的准确性对业务逻辑很重要
// 必须在成员变动时及时清理缓存
//
// **批量处理：**
// 支持同时删除多个群组的成员数量缓存
// 适用于批量操作的场景
//
// **参数说明：**
// - groupID: 群组ID列表（可变参数）
//
// **返回值：**
// - cache.GroupCache: 新的缓存实例，支持链式调用
func (g *GroupCacheRedis) DelGroupsMemberNum(groupID ...string) cache.GroupCache {
	keys := make([]string, 0, len(groupID))
	for _, groupID := range groupID {
		keys = append(keys, g.getGroupMemberNumKey(groupID))
	}
	cache := g.CloneGroupCache()
	cache.AddKeys(keys...)

	return cache
}

// ==================== 群主和角色级别缓存操作 ====================

// GetGroupOwner 获取群主信息
//
// **功能说明：**
// 获取指定群组的群主成员信息
//
// **实现原理：**
// 群主实际上是角色级别为GroupOwner的特殊成员
// 通过查询角色级别缓存来获取群主信息
//
// **业务规则：**
// - 每个群组有且仅有一个群主
// - 群主拥有群组的最高权限
// - 群主可以转移群主身份给其他成员
//
// **错误处理：**
// - 群组不存在群主时返回ErrRecordNotFound错误
// - 包含详细的错误信息，便于问题排查
//
// **应用场景：**
// - 权限验证：检查操作者是否为群主
// - 信息展示：显示群主信息
// - 业务逻辑：群主相关的特殊处理
//
// **参数说明：**
// - ctx: 上下文信息
// - groupID: 群组ID
//
// **返回值：**
// - *model.GroupMember: 群主成员信息
// - error: 错误信息
func (g *GroupCacheRedis) GetGroupOwner(ctx context.Context, groupID string) (*model.GroupMember, error) {
	// **获取群主角色级别的所有成员**
	members, err := g.GetGroupRoleLevelMemberInfo(ctx, groupID, constant.GroupOwner)
	if err != nil {
		return nil, err
	}

	// **验证群主存在性**
	if len(members) == 0 {
		return nil, errs.ErrRecordNotFound.WrapMsg(fmt.Sprintf("group %s owner not found", groupID))
	}

	// **返回群主信息（正常情况下只有一个群主）**
	return members[0], nil
}

// GetGroupsOwner 批量获取多个群组的群主信息
//
// **功能说明：**
// 批量获取多个群组的群主成员信息
//
// **处理策略：**
// 1. 遍历所有群组ID
// 2. 逐个获取每个群组的群主信息
// 3. 收集所有存在群主的群组信息
//
// **容错处理：**
// - 如果某个群组没有群主，跳过该群组
// - 不会因为单个群组的问题影响整体结果
// - 返回所有成功获取的群主信息
//
// **应用场景：**
// - 批量权限检查：验证多个群组的群主权限
// - 管理界面：显示多个群组的群主信息
// - 统计分析：分析群主的分布情况
//
// **性能考虑：**
// - 利用现有的角色级别缓存机制
// - 避免重复的数据库查询
// - 支持并发处理提高效率
//
// **参数说明：**
// - ctx: 上下文信息
// - groupIDs: 群组ID列表
//
// **返回值：**
// - []*model.GroupMember: 群主信息列表
// - error: 错误信息
func (g *GroupCacheRedis) GetGroupsOwner(ctx context.Context, groupIDs []string) ([]*model.GroupMember, error) {
	members := make([]*model.GroupMember, 0, len(groupIDs))

	// **遍历处理每个群组**
	for _, groupID := range groupIDs {
		// **获取当前群组的群主角色成员**
		items, err := g.GetGroupRoleLevelMemberInfo(ctx, groupID, constant.GroupOwner)
		if err != nil {
			return nil, err
		}

		// **如果存在群主，添加到结果列表**
		if len(items) > 0 {
			members = append(members, items[0])
		}
	}

	return members, nil
}

// GetGroupRoleLevelMemberIDs 获取指定角色级别的成员ID列表
//
// **功能说明：**
// 获取群组中指定角色级别的所有成员用户ID列表
//
// **角色级别定义：**
// - constant.GroupOwner (1): 群主
// - constant.GroupAdmin (2): 管理员
// - constant.GroupOrdinaryUsers (3): 普通成员
//
// **应用场景：**
// - 权限管理：获取有特定权限的用户列表
// - 消息推送：向特定角色的用户推送消息
// - 统计分析：分析不同角色的用户数量
// - 批量操作：对特定角色的用户执行操作
//
// **缓存策略：**
// - 角色信息相对稳定，适合缓存
// - 角色变更时需要及时清理缓存
// - 支持高频的权限检查需求
//
// **性能优化：**
// - 避免复杂的角色查询SQL
// - 提供快速的权限验证能力
// - 减少数据库的查询压力
//
// **参数说明：**
// - ctx: 上下文信息
// - groupID: 群组ID
// - roleLevel: 角色级别
//
// **返回值：**
// - []string: 指定角色级别的用户ID列表
// - error: 错误信息
func (g *GroupCacheRedis) GetGroupRoleLevelMemberIDs(ctx context.Context, groupID string, roleLevel int32) ([]string, error) {
	return getCache(ctx, g.rcClient, g.getGroupRoleLevelMemberIDsKey(groupID, roleLevel), g.expireTime, func(ctx context.Context) ([]string, error) {
		// **数据库查询：获取指定角色级别的用户ID列表**
		return g.groupMemberDB.FindRoleLevelUserIDs(ctx, groupID, roleLevel)
	})
}

// GetGroupRoleLevelMemberInfo 获取指定角色级别的成员详细信息
//
// **功能说明：**
// 获取群组中指定角色级别的所有成员详细信息
//
// **实现策略：**
// 1. 先获取指定角色级别的用户ID列表
// 2. 再批量获取这些用户的详细成员信息
// 3. 充分利用现有的缓存机制
//
// **两阶段查询优势：**
// - 复用缓存：角色ID列表和成员详细信息都有独立缓存
// - 灵活性：可以根据需要只获取ID或详细信息
// - 性能优化：避免复杂的关联查询
//
// **应用场景：**
// - 管理界面：显示特定角色的成员列表
// - 权限操作：对特定角色的成员执行操作
// - 信息展示：展示管理员、群主等关键角色信息
//
// **参数说明：**
// - ctx: 上下文信息
// - groupID: 群组ID
// - roleLevel: 角色级别
//
// **返回值：**
// - []*model.GroupMember: 指定角色级别的成员信息列表
// - error: 错误信息
func (g *GroupCacheRedis) GetGroupRoleLevelMemberInfo(ctx context.Context, groupID string, roleLevel int32) ([]*model.GroupMember, error) {
	// **第一步：获取指定角色级别的用户ID列表**
	userIDs, err := g.GetGroupRoleLevelMemberIDs(ctx, groupID, roleLevel)
	if err != nil {
		return nil, err
	}

	// **第二步：批量获取这些用户的详细成员信息**
	return g.GetGroupMembersInfo(ctx, groupID, userIDs)
}

// GetGroupRolesLevelMemberInfo 获取多个角色级别的成员信息
//
// **功能说明：**
// 获取群组中多个角色级别的所有成员详细信息
//
// **处理流程：**
// 1. 遍历所有指定的角色级别
// 2. 获取每个角色级别的用户ID列表
// 3. 合并所有用户ID
// 4. 批量获取成员详细信息
//
// **应用场景：**
// - 管理功能：获取管理员和群主的信息
// - 权限检查：验证用户是否具有管理权限
// - 批量操作：对多个角色级别的用户执行操作
//
// **使用示例：**
// ```go
// // 获取群主和管理员信息
// members, err := cache.GetGroupRolesLevelMemberInfo(ctx, groupID,
//
//	[]int32{constant.GroupOwner, constant.GroupAdmin})
//
// ```
//
// **性能优化：**
// - 合并用户ID后进行批量查询
// - 避免多次重复的数据库访问
// - 利用批量缓存机制提高效率
//
// **参数说明：**
// - ctx: 上下文信息
// - groupID: 群组ID
// - roleLevels: 角色级别列表
//
// **返回值：**
// - []*model.GroupMember: 多个角色级别的成员信息列表
// - error: 错误信息
func (g *GroupCacheRedis) GetGroupRolesLevelMemberInfo(ctx context.Context, groupID string, roleLevels []int32) ([]*model.GroupMember, error) {
	var userIDs []string

	// **遍历所有角色级别，收集用户ID**
	for _, roleLevel := range roleLevels {
		ids, err := g.GetGroupRoleLevelMemberIDs(ctx, groupID, roleLevel)
		if err != nil {
			return nil, err
		}
		userIDs = append(userIDs, ids...)
	}

	// **批量获取所有用户的成员信息**
	return g.GetGroupMembersInfo(ctx, groupID, userIDs)
}

// ==================== 跨群组成员查找 ====================

// FindGroupMemberUser 查找用户在指定群组中的成员信息
//
// **功能说明：**
// 查找指定用户在多个群组中的成员信息
//
// **参数处理：**
// - 如果groupIDs为空，自动获取用户加入的所有群组
// - 如果groupIDs不为空，只查找指定群组中的成员信息
//
// **实现策略：**
// 使用batchGetCache2进行批量查询优化：
// 1. 按Redis槽位对群组进行分组
// 2. 并发查询各槽位的缓存数据
// 3. 对于缓存未命中的群组，批量查询数据库
//
// **应用场景：**
// - 用户信息聚合：获取用户在所有群组中的身份信息
// - 权限检查：验证用户在特定群组中的权限
// - 数据同步：同步用户的群组成员信息
// - 统计分析：分析用户的群组参与情况
//
// **性能特点：**
// - 槽位优化：Redis集群环境下的性能优化
// - 批量处理：减少数据库查询次数
// - 自动发现：可以自动发现用户的所有群组
//
// **参数说明：**
// - ctx: 上下文信息
// - groupIDs: 群组ID列表（为空时查询用户的所有群组）
// - userID: 用户ID
//
// **返回值：**
// - []*model.GroupMember: 用户在各群组中的成员信息列表
// - error: 错误信息
func (g *GroupCacheRedis) FindGroupMemberUser(ctx context.Context, groupIDs []string, userID string) ([]*model.GroupMember, error) {
	// **处理空群组ID列表的情况**
	if len(groupIDs) == 0 {
		var err error
		// **自动获取用户加入的所有群组ID**
		groupIDs, err = g.GetJoinedGroupIDs(ctx, userID)
		if err != nil {
			return nil, err
		}
	}

	// **使用批量缓存查询优化**
	return batchGetCache2(ctx, g.rcClient, g.expireTime, groupIDs,
		// **缓存键生成函数**
		func(groupID string) string {
			return g.getGroupMemberInfoKey(groupID, userID)
		},
		// **ID提取函数**
		func(member *model.GroupMember) string {
			return member.GroupID
		},
		// **数据库查询函数**
		func(ctx context.Context, groupIDs []string) ([]*model.GroupMember, error) {
			return g.groupMemberDB.FindInGroup(ctx, userID, groupIDs)
		})
}

// ==================== 版本控制缓存操作 ====================

// DelMaxGroupMemberVersion 删除群成员最大版本号缓存
//
// **功能说明：**
// 删除指定群组的成员最大版本号缓存
//
// **版本控制机制：**
// - 每次群成员变更都会生成新的版本号
// - 客户端通过比较版本号进行增量同步
// - 版本号缓存提高同步检查的效率
//
// **删除时机：**
// - 群成员发生任何变更时
// - 需要强制客户端重新同步时
// - 版本号计算逻辑更新时
//
// **应用场景：**
// - 增量同步：支持客户端的增量数据同步
// - 变更通知：通知客户端数据已更新
// - 一致性保证：确保客户端数据的一致性
//
// **参数说明：**
// - groupIDs: 群组ID列表（可变参数）
//
// **返回值：**
// - cache.GroupCache: 新的缓存实例，支持链式调用
func (g *GroupCacheRedis) DelMaxGroupMemberVersion(groupIDs ...string) cache.GroupCache {
	keys := make([]string, 0, len(groupIDs))
	for _, groupID := range groupIDs {
		keys = append(keys, g.getGroupMemberMaxVersionKey(groupID))
	}
	cache := g.CloneGroupCache()
	cache.AddKeys(keys...)
	return cache
}

// DelMaxJoinGroupVersion 删除用户加群最大版本号缓存
//
// **功能说明：**
// 删除指定用户的加群最大版本号缓存
//
// **版本控制机制：**
// - 用户加入或退出群组时版本号会更新
// - 客户端通过版本号判断群组列表是否需要同步
// - 支持用户维度的增量同步
//
// **删除时机：**
// - 用户加入新群组时
// - 用户退出群组时
// - 用户群组列表发生任何变化时
//
// **应用场景：**
// - 群组列表同步：客户端同步用户的群组列表
// - 变更检测：检测用户群组参与情况的变化
// - 同步优化：避免不必要的群组列表传输
//
// **参数说明：**
// - userIDs: 用户ID列表（可变参数）
//
// **返回值：**
// - cache.GroupCache: 新的缓存实例，支持链式调用
func (g *GroupCacheRedis) DelMaxJoinGroupVersion(userIDs ...string) cache.GroupCache {
	keys := make([]string, 0, len(userIDs))
	for _, userID := range userIDs {
		keys = append(keys, g.getJoinGroupMaxVersionKey(userID))
	}
	cache := g.CloneGroupCache()
	cache.AddKeys(keys...)
	return cache
}

// FindMaxGroupMemberVersion 查找群成员最大版本号
//
// **功能说明：**
// 获取指定群组的成员最大版本号信息
//
// **版本日志信息：**
// - DID: 数据标识（群组ID）
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
// - groupID: 群组ID
//
// **返回值：**
// - *model.VersionLog: 版本日志信息
// - error: 错误信息
func (g *GroupCacheRedis) FindMaxGroupMemberVersion(ctx context.Context, groupID string) (*model.VersionLog, error) {
	return getCache(ctx, g.rcClient, g.getGroupMemberMaxVersionKey(groupID), g.expireTime, func(ctx context.Context) (*model.VersionLog, error) {
		// **数据库查询：获取群成员的最大版本号**
		// 参数说明：groupID, version=0, limit=0 表示获取最大版本
		return g.groupMemberDB.FindMemberIncrVersion(ctx, groupID, 0, 0)
	})
}

// BatchFindMaxGroupMemberVersion 批量查找群成员最大版本号
//
// **功能说明：**
// 批量获取多个群组的成员最大版本号信息
//
// **批量优化：**
// 1. 使用batchGetCache2进行Redis集群槽位优化
// 2. 减少网络往返次数，提高查询效率
// 3. 支持缓存穿透保护和分布式锁
//
// **应用场景：**
// - 批量同步检查：客户端批量检查多个群组的版本
// - 服务端优化：减少版本查询的开销
// - 数据一致性：批量验证数据的一致性
//
// **实现细节：**
// - 为每个群组创建版本和限制参数（都设为0表示获取最大版本）
// - 使用批量数据库查询接口提高效率
//
// **参数说明：**
// - ctx: 上下文信息
// - groupIDs: 群组ID列表
//
// **返回值：**
// - []*model.VersionLog: 版本日志信息列表
// - error: 错误信息
func (g *GroupCacheRedis) BatchFindMaxGroupMemberVersion(ctx context.Context, groupIDs []string) ([]*model.VersionLog, error) {
	return batchGetCache2(ctx, g.rcClient, g.expireTime, groupIDs,
		// **缓存键生成函数**
		func(groupID string) string {
			return g.getGroupMemberMaxVersionKey(groupID)
		},
		// **ID提取函数**
		func(versionLog *model.VersionLog) string {
			return versionLog.DID
		},
		// **数据库查询函数**
		func(ctx context.Context, groupIDs []string) ([]*model.VersionLog, error) {
			// **创建批量查询参数**
			// 为每个群组创建版本号和限制参数，都设为0表示获取最大版本
			versions := make([]uint, len(groupIDs))
			limits := make([]int, len(groupIDs))

			// **批量查询数据库**
			return g.groupMemberDB.BatchFindMemberIncrVersion(ctx, groupIDs, versions, limits)
		})
}

// FindMaxJoinGroupVersion 查找用户加群最大版本号
//
// **功能说明：**
// 获取指定用户的加群最大版本号信息
//
// **版本控制机制：**
// - 用户每次加入或退出群组都会更新版本号
// - 客户端通过版本号判断群组列表是否需要同步
// - 支持用户维度的增量数据同步
//
// **应用场景：**
// - 群组列表同步：客户端同步用户的群组列表
// - 变更检测：检测用户群组参与情况的变化
// - 同步优化：避免不必要的群组列表传输
//
// **缓存策略：**
// - 用户加群版本信息访问频繁，适合缓存
// - 用户群组变更时需要及时清理缓存
// - 支持高并发的版本检查需求
//
// **参数说明：**
// - ctx: 上下文信息
// - userID: 用户ID
//
// **返回值：**
// - *model.VersionLog: 版本日志信息
// - error: 错误信息
func (g *GroupCacheRedis) FindMaxJoinGroupVersion(ctx context.Context, userID string) (*model.VersionLog, error) {
	return getCache(ctx, g.rcClient, g.getJoinGroupMaxVersionKey(userID), g.expireTime, func(ctx context.Context) (*model.VersionLog, error) {
		// **数据库查询：获取用户加群的最大版本号**
		// 参数说明：userID, version=0, limit=0 表示获取最大版本
		return g.groupMemberDB.FindJoinIncrVersion(ctx, userID, 0, 0)
	})
}
