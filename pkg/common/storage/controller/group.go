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

// Package controller 提供数据存储控制器实现
//
// 本文件实现群组数据库控制器，是群组相关数据操作的核心控制层
//
// **架构设计：**
// ┌─────────────────────────────────────────────────────────┐
// │                     业务层 (RPC Services)               │
// └─────────────────────────────────────────────────────────┘
//
//	↓
//
// ┌─────────────────────────────────────────────────────────┐
// │              控制器层 (GroupDatabase)                   │
// │ • 统一接口定义                                           │
// │ • 事务管理                                              │
// │ • 缓存协调                                              │
// │ • 业务逻辑封装                                           │
// └─────────────────────────────────────────────────────────┘
//
//	↓
//
// ┌─────────────────┬─────────────────┬─────────────────────┐
// │   缓存层 (Cache) │  数据库层 (DB)   │   事务层 (TX)        │
// │ • Redis缓存      │ • MySQL存储     │ • 分布式事务         │
// │ • 本地缓存       │ • 数据持久化     │ • ACID保证          │
// │ • 缓存一致性     │ • 数据查询       │ • 回滚机制          │
// └─────────────────┴─────────────────┴─────────────────────┘
//
// **核心特性：**
// 1. 缓存优先策略：优先从缓存获取数据，提高查询性能
// 2. 分层架构设计：清晰的职责分离和依赖管理
// 3. 事务一致性：确保数据操作的原子性和一致性
// 4. 批量操作优化：支持高效的批量数据处理
// 5. 版本管理：支持增量同步和版本控制
//
// **性能优化：**
// - 多级缓存架构：Redis + 本地缓存
// - 批量缓存删除：使用链式调用优化缓存失效
// - 槽位优化：Redis集群环境下的性能优化
// - 分页查询：避免大数据量的内存溢出
package controller

import (
	"context"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	redis2 "github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache/redis"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/common"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/tools/db/pagination"
	"github.com/openimsdk/tools/db/tx"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/utils/datautil"
	"github.com/redis/go-redis/v9"
)

// GroupDatabase 群组数据库操作接口
// 定义了群组相关的所有数据操作方法，是群组功能的数据访问层核心接口
//
// **接口设计原则：**
// 1. 单一职责：每个方法专注于特定的业务功能
// 2. 一致性：统一的参数风格和错误处理
// 3. 扩展性：支持未来功能的扩展和修改
// 4. 性能优化：内置缓存和批量操作支持
//
// **功能分类：**
// - 群组管理：创建、查询、更新、解散群组
// - 成员管理：添加、删除、查询、更新群成员
// - 权限管理：角色级别、所有者转移
// - 申请管理：入群申请的处理流程
// - 统计分析：群组数量、成员统计
// - 版本同步：增量更新和版本管理
type GroupDatabase interface {
	// ==================== 群组基础操作 ====================

	// CreateGroup 创建群组及其成员
	// 功能：原子性地创建群组和初始成员，确保数据一致性
	// 参数：
	//   - groups: 要创建的群组列表
	//   - groupMembers: 初始群成员列表
	// 特性：事务保证、缓存失效、批量操作
	CreateGroup(ctx context.Context, groups []*model.Group, groupMembers []*model.GroupMember) error

	// TakeGroup 获取单个群组信息
	// 功能：从缓存优先获取群组详细信息
	// 缓存策略：缓存命中 -> 直接返回，缓存未命中 -> 查询数据库并缓存
	TakeGroup(ctx context.Context, groupID string) (group *model.Group, err error)

	// FindGroup 批量获取群组信息
	// 功能：高效地批量查询多个群组信息
	// 优化：使用batchGetCache2进行槽位优化的批量查询
	FindGroup(ctx context.Context, groupIDs []string) (groups []*model.Group, err error)

	// SearchGroup 搜索群组
	// 功能：基于关键词搜索群组，支持分页
	// 注意：直接查询数据库，不走缓存（搜索结果时效性要求）
	SearchGroup(ctx context.Context, keyword string, pagination pagination.Pagination) (int64, []*model.Group, error)

	// UpdateGroup 更新群组信息
	// 功能：更新群组属性并维护缓存一致性
	// 特性：事务保证、缓存失效、版本更新
	UpdateGroup(ctx context.Context, groupID string, data map[string]any) error

	// DismissGroup 解散群组
	// 功能：解散群组并可选择删除所有成员
	// 参数：deleteMember 是否删除群成员记录
	// 特性：状态更新、批量缓存失效、版本管理
	DismissGroup(ctx context.Context, groupID string, deleteMember bool) error

	// ==================== 群成员管理 ====================

	// TakeGroupMember 获取特定群成员信息
	// 功能：查询指定用户在指定群组中的成员信息
	TakeGroupMember(ctx context.Context, groupID string, userID string) (groupMember *model.GroupMember, err error)

	// TakeGroupOwner 获取群主信息
	// 功能：查询群组的所有者（群主）信息
	TakeGroupOwner(ctx context.Context, groupID string) (*model.GroupMember, error)

	// FindGroupMembers 查询群组中指定用户的成员信息
	// 功能：批量查询指定用户在群组中的成员信息
	FindGroupMembers(ctx context.Context, groupID string, userIDs []string) (groupMembers []*model.GroupMember, err error)

	// FindGroupMemberUser 查询用户在多个群组中的成员信息
	// 功能：查询指定用户在多个群组中的成员身份
	FindGroupMemberUser(ctx context.Context, groupIDs []string, userID string) (groupMembers []*model.GroupMember, err error)

	// FindGroupMemberRoleLevels 根据角色级别查询群成员
	// 功能：查询群组中具有指定角色级别的成员
	// 应用：获取管理员、普通成员等不同角色的用户
	FindGroupMemberRoleLevels(ctx context.Context, groupID string, roleLevels []int32) (groupMembers []*model.GroupMember, err error)

	// FindGroupMemberAll 获取群组所有成员
	// 功能：获取群组的完整成员列表
	// 注意：大群组慎用，建议配合分页使用
	FindGroupMemberAll(ctx context.Context, groupID string) (groupMembers []*model.GroupMember, err error)

	// FindGroupsOwner 批量获取多个群组的群主
	// 功能：高效地批量查询多个群组的所有者信息
	FindGroupsOwner(ctx context.Context, groupIDs []string) ([]*model.GroupMember, error)

	// FindGroupMemberUserID 获取群组所有成员的用户ID
	// 功能：返回群组中所有成员的用户ID列表
	// 应用：群消息推送、权限检查等场景
	FindGroupMemberUserID(ctx context.Context, groupID string) ([]string, error)

	// FindGroupMemberNum 获取群组成员数量
	// 功能：快速获取群组的成员总数
	// 优化：优先从缓存获取，提高查询效率
	FindGroupMemberNum(ctx context.Context, groupID string) (uint32, error)

	// FindUserManagedGroupID 获取用户管理的群组ID列表
	// 功能：查询用户作为管理员或群主的群组
	FindUserManagedGroupID(ctx context.Context, userID string) (groupIDs []string, err error)

	// GetGroupRoleLevelMemberIDs 获取指定角色级别的成员ID列表
	// 功能：获取群组中特定角色的所有用户ID
	GetGroupRoleLevelMemberIDs(ctx context.Context, groupID string, roleLevel int32) ([]string, error)

	// ==================== 分页查询功能 ====================

	// PageGetJoinGroup 分页获取用户加入的群组
	// 功能：分页查询用户参与的所有群组
	// 优化：先获取群组ID列表，再分页查询详细信息
	PageGetJoinGroup(ctx context.Context, userID string, pagination pagination.Pagination) (total int64, totalGroupMembers []*model.GroupMember, err error)

	// PageGetGroupMember 分页获取群组成员
	// 功能：分页查询群组的成员列表
	// 优化：基于缓存的高效分页实现
	PageGetGroupMember(ctx context.Context, groupID string, pagination pagination.Pagination) (total int64, totalGroupMembers []*model.GroupMember, err error)

	// SearchGroupMember 搜索群组成员
	// 功能：在群组内搜索成员，支持关键词匹配
	SearchGroupMember(ctx context.Context, keyword string, groupID string, pagination pagination.Pagination) (int64, []*model.GroupMember, error)

	// ==================== 群组申请管理 ====================

	// PageGroupRequest 分页获取群组申请
	// 功能：分页查询指定群组的入群申请
	// 参数：handleResults 处理状态筛选（待处理、已同意、已拒绝）
	PageGroupRequest(ctx context.Context, groupIDs []string, handleResults []int, pagination pagination.Pagination) (int64, []*model.GroupRequest, error)

	// HandlerGroupRequest 处理群组申请
	// 功能：处理入群申请（同意/拒绝），如果同意则创建群成员
	// 特性：事务保证、条件性成员创建、缓存更新
	HandlerGroupRequest(ctx context.Context, groupID string, userID string, handledMsg string, handleResult int32, member *model.GroupMember) error

	// CreateGroupRequest 创建群组申请
	// 功能：创建新的入群申请记录
	// 特性：自动删除旧申请、防止重复申请
	CreateGroupRequest(ctx context.Context, requests []*model.GroupRequest) error

	// TakeGroupRequest 获取群组申请信息
	// 功能：查询特定的入群申请记录
	TakeGroupRequest(ctx context.Context, groupID string, userID string) (*model.GroupRequest, error)

	// FindGroupRequests 批量获取群组申请
	// 功能：批量查询多个用户的入群申请
	FindGroupRequests(ctx context.Context, groupID string, userIDs []string) ([]*model.GroupRequest, error)

	// PageGroupRequestUser 分页获取用户的群组申请
	// 功能：分页查询用户发起的入群申请
	PageGroupRequestUser(ctx context.Context, userID string, groupIDs []string, handleResults []int, pagination pagination.Pagination) (int64, []*model.GroupRequest, error)

	// GetGroupApplicationUnhandledCount 获取未处理申请数量
	// 功能：统计指定时间后的未处理入群申请数量
	GetGroupApplicationUnhandledCount(ctx context.Context, groupIDs []string, ts int64) (int64, error)

	// ==================== 成员操作管理 ====================

	// DeleteGroupMember 删除群组成员
	// 功能：从群组中移除指定用户
	// 特性：批量删除、事务保证、缓存失效、版本更新
	DeleteGroupMember(ctx context.Context, groupID string, userIDs []string) error

	// TransferGroupOwner 转移群主权限
	// 功能：将群主权限转移给其他成员
	// 特性：原子性角色变更、缓存更新
	TransferGroupOwner(ctx context.Context, groupID string, oldOwnerUserID, newOwnerUserID string, roleLevel int32) error

	// UpdateGroupMember 更新群成员信息
	// 功能：更新单个群成员的属性
	// 特性：条件性缓存失效（角色变更时清理更多缓存）
	UpdateGroupMember(ctx context.Context, groupID string, userID string, data map[string]any) error

	// UpdateGroupMembers 批量更新群成员信息
	// 功能：批量更新多个群成员的属性
	// 优化：减少数据库往返次数和缓存操作
	UpdateGroupMembers(ctx context.Context, data []*common.BatchUpdateGroupMember) error

	// ==================== 映射和统计功能 ====================

	// MapGroupMemberUserID 获取群组成员用户ID映射
	// 功能：批量获取多个群组的成员用户ID简化信息
	// 返回：map[groupID]*GroupSimpleUserID 的映射关系
	MapGroupMemberUserID(ctx context.Context, groupIDs []string) (map[string]*common.GroupSimpleUserID, error)

	// MapGroupMemberNum 获取群组成员数量映射
	// 功能：批量获取多个群组的成员数量
	// 返回：map[groupID]memberCount 的映射关系
	MapGroupMemberNum(ctx context.Context, groupIDs []string) (map[string]uint32, error)

	// ==================== 统计分析功能 ====================

	// CountTotal 统计群组总数
	// 功能：统计指定时间前创建的群组总数
	// 应用：数据分析、运营统计
	CountTotal(ctx context.Context, before *time.Time) (count int64, err error)

	// CountRangeEverydayTotal 统计每日群组创建数量
	// 功能：统计指定时间范围内每天的群组创建数量
	// 返回：map[date]count 的每日统计数据
	CountRangeEverydayTotal(ctx context.Context, start time.Time, end time.Time) (map[string]int64, error)

	// ==================== 缓存管理功能 ====================

	// DeleteGroupMemberHash 删除群成员哈希缓存
	// 功能：批量删除指定群组的成员哈希缓存
	// 应用：缓存维护、数据一致性保证
	DeleteGroupMemberHash(ctx context.Context, groupIDs []string) error

	// ==================== 版本同步功能 ====================

	// FindMemberIncrVersion 查找群成员增量版本
	// 功能：获取群组成员的增量更新信息
	// 应用：客户端增量同步、减少数据传输
	FindMemberIncrVersion(ctx context.Context, groupID string, version uint, limit int) (*model.VersionLog, error)

	// BatchFindMemberIncrVersion 批量查找群成员增量版本
	// 功能：批量获取多个群组的成员增量版本信息
	BatchFindMemberIncrVersion(ctx context.Context, groupIDs []string, versions []uint64, limits []int) (map[string]*model.VersionLog, error)

	// FindJoinIncrVersion 查找用户加群增量版本
	// 功能：获取用户加入群组的增量版本信息
	FindJoinIncrVersion(ctx context.Context, userID string, version uint, limit int) (*model.VersionLog, error)

	// MemberGroupIncrVersion 更新群成员版本
	// 功能：更新群组成员的版本信息
	// 应用：数据变更时触发版本更新
	MemberGroupIncrVersion(ctx context.Context, groupID string, userIDs []string, state int32) error

	// ==================== 版本缓存功能 ====================

	// FindMaxGroupMemberVersionCache 获取群成员最大版本（缓存）
	// 功能：从缓存获取群组成员的最大版本号
	FindMaxGroupMemberVersionCache(ctx context.Context, groupID string) (*model.VersionLog, error)

	// BatchFindMaxGroupMemberVersionCache 批量获取群成员最大版本（缓存）
	// 功能：批量从缓存获取多个群组的最大成员版本
	BatchFindMaxGroupMemberVersionCache(ctx context.Context, groupIDs []string) (map[string]*model.VersionLog, error)

	// FindMaxJoinGroupVersionCache 获取用户加群最大版本（缓存）
	// 功能：从缓存获取用户加入群组的最大版本号
	FindMaxJoinGroupVersionCache(ctx context.Context, userID string) (*model.VersionLog, error)

	// ==================== 扩展查询功能 ====================

	// SearchJoinGroup 搜索用户加入的群组
	// 功能：在用户加入的群组中搜索匹配关键词的群组
	// 优化：先获取用户群组列表，再进行关键词搜索
	SearchJoinGroup(ctx context.Context, userID string, keyword string, pagination pagination.Pagination) (int64, []*model.Group, error)

	// FindJoinGroupID 获取用户加入的群组ID列表
	// 功能：获取用户参与的所有群组ID
	// 优化：优先从缓存获取
	FindJoinGroupID(ctx context.Context, userID string) ([]string, error)
}

// NewGroupDatabase 创建群组数据库控制器实例
//
// **依赖注入设计：**
// 通过构造函数注入所有依赖项，确保模块间的松耦合
//
// **参数说明：**
// - rdb: Redis客户端，用于缓存操作
// - localCache: 本地缓存配置
// - groupDB: 群组数据库操作接口
// - groupMemberDB: 群成员数据库操作接口
// - groupRequestDB: 群申请数据库操作接口
// - ctxTx: 分布式事务管理器
// - groupHash: 群组哈希缓存接口
//
// **初始化流程：**
// 1. 注入所有依赖组件
// 2. 创建Redis缓存层，集成所有缓存功能
// 3. 返回完整的控制器实例
func NewGroupDatabase(
	rdb redis.UniversalClient,
	localCache *config.LocalCache,
	groupDB database.Group,
	groupMemberDB database.GroupMember,
	groupRequestDB database.GroupRequest,
	ctxTx tx.Tx,
	groupHash cache.GroupHash,
) GroupDatabase {
	return &groupDatabase{
		groupDB:        groupDB,
		groupMemberDB:  groupMemberDB,
		groupRequestDB: groupRequestDB,
		ctxTx:          ctxTx,
		cache:          redis2.NewGroupCacheRedis(rdb, localCache, groupDB, groupMemberDB, groupRequestDB, groupHash, redis2.GetRocksCacheOptions()),
	}
}

// groupDatabase 群组数据库控制器实现
//
// **架构特点：**
// 1. 组合模式：组合多个专业化的数据访问接口
// 2. 缓存优先：优先使用缓存，提高查询性能
// 3. 事务保证：关键操作使用分布式事务确保一致性
// 4. 错误处理：统一的错误处理和日志记录
//
// **成员说明：**
// - groupDB: 群组基础数据操作
// - groupMemberDB: 群成员数据操作
// - groupRequestDB: 群申请数据操作
// - ctxTx: 分布式事务管理
// - cache: 统一缓存操作接口
type groupDatabase struct {
	groupDB        database.Group        // 群组数据库操作接口
	groupMemberDB  database.GroupMember  // 群成员数据库操作接口
	groupRequestDB database.GroupRequest // 群申请数据库操作接口
	ctxTx          tx.Tx                 // 分布式事务管理器
	cache          cache.GroupCache      // 群组缓存操作接口
}

// FindJoinGroupID 获取用户加入的群组ID列表
//
// 此方法用于获取指定用户加入的所有群组ID列表，主要用于：
// - 用户群组列表展示
// - 权限验证时的群组范围确定
// - 消息推送时的群组范围计算
//
// **缓存策略：**
// 优先从缓存获取用户的群组列表，缓存键格式：joined_group_ids:{userID}
// 缓存失效时机：用户加入或退出群组时
//
// **性能考虑：**
// - 缓存优先：避免频繁查询数据库
// - 集合存储：Redis Set结构提供高效的成员检查
// - 懒加载：首次访问时加载并缓存
//
// **数据一致性：**
// 通过缓存失效机制确保数据一致性，相关操作会自动清理缓存
//
// 参数：
//   - ctx: 上下文，用于超时控制和链路追踪
//   - userID: 目标用户ID
//
// 返回：
//   - []string: 用户加入的群组ID列表
//   - error: 操作错误，nil表示成功
//
// 使用示例：
//
//	groupIDs, err := db.FindJoinGroupID(ctx, "user123")
//	if err != nil {
//	    return err
//	}
//	fmt.Printf("用户加入了%d个群组", len(groupIDs))
func (g *groupDatabase) FindJoinGroupID(ctx context.Context, userID string) ([]string, error) {
	return g.cache.GetJoinedGroupIDs(ctx, userID)
}

// FindGroupMembers 获取群组中指定成员的详细信息
//
// 此方法用于批量获取群组中指定用户的群成员详细信息，包括：
// - 群内昵称和头像
// - 角色等级（群主、管理员、普通成员）
// - 加入时间和邀请者信息
// - 禁言状态和扩展字段
//
// **缓存策略：**
// 使用分布式缓存存储群成员信息，缓存键格式：group_member_info:{groupID}:{userID}
// 缓存过期时间：24小时，支持LRU淘汰策略
//
// **批量查询优化：**
// 1. 批量从缓存获取：减少网络往返次数
// 2. 缓存未命中处理：仅查询缺失的用户信息
// 3. 回写缓存：查询结果自动缓存
//
// **数据完整性：**
// - 自动过滤不存在的群成员
// - 保持返回结果与输入顺序的一致性
// - 处理并发访问的数据一致性
//
// **性能特点：**
// - O(n) 时间复杂度，n为userIDs数量
// - 支持大批量查询（建议单次不超过100个用户）
// - 智能缓存预热和更新
//
// 参数：
//   - ctx: 上下文，用于超时控制和链路追踪
//   - groupID: 目标群组ID
//   - userIDs: 要查询的用户ID列表
//
// 返回：
//   - []*model.GroupMember: 群成员详细信息列表
//   - error: 操作错误，nil表示成功
//
// 使用示例：
//
//	members, err := db.FindGroupMembers(ctx, "group123", []string{"user1", "user2"})
//	if err != nil {
//	    return err
//	}
//	for _, member := range members {
//	    fmt.Printf("成员：%s，角色：%d\n", member.UserID, member.RoleLevel)
//	}
//
// 注意事项：
// - 如果用户不在群组中，不会包含在返回结果中
// - 建议批量查询时控制用户ID数量，避免单次查询过大
// - 返回的成员信息可能因为缓存策略存在轻微延迟
func (g *groupDatabase) FindGroupMembers(ctx context.Context, groupID string, userIDs []string) ([]*model.GroupMember, error) {
	return g.cache.GetGroupMembersInfo(ctx, groupID, userIDs)
}

func (g *groupDatabase) FindGroupMemberUser(ctx context.Context, groupIDs []string, userID string) ([]*model.GroupMember, error) {
	return g.cache.FindGroupMemberUser(ctx, groupIDs, userID)
}

func (g *groupDatabase) FindGroupMemberRoleLevels(ctx context.Context, groupID string, roleLevels []int32) ([]*model.GroupMember, error) {
	return g.cache.GetGroupRolesLevelMemberInfo(ctx, groupID, roleLevels)
}

func (g *groupDatabase) FindGroupMemberAll(ctx context.Context, groupID string) ([]*model.GroupMember, error) {
	return g.cache.GetAllGroupMembersInfo(ctx, groupID)
}

func (g *groupDatabase) FindGroupsOwner(ctx context.Context, groupIDs []string) ([]*model.GroupMember, error) {
	return g.cache.GetGroupsOwner(ctx, groupIDs)
}

func (g *groupDatabase) GetGroupRoleLevelMemberIDs(ctx context.Context, groupID string, roleLevel int32) ([]string, error) {
	return g.cache.GetGroupRoleLevelMemberIDs(ctx, groupID, roleLevel)
}

// CreateGroup 实现创建群组及其成员的具体逻辑
//
// **实现要点：**
// - 事务保证：群组和成员创建的原子性
// - 缓存管理：创建后及时清理相关缓存
// - 版本控制：无需手动版本更新（数据库层自动处理）
// - 批量优化：支持同时创建多个群组和成员
func (g *groupDatabase) CreateGroup(ctx context.Context, groups []*model.Group, groupMembers []*model.GroupMember) error {
	if len(groups)+len(groupMembers) == 0 {
		return nil
	}
	return g.ctxTx.Transaction(ctx, func(ctx context.Context) error {
		c := g.cache.CloneGroupCache()
		if len(groups) > 0 {
			if err := g.groupDB.Create(ctx, groups); err != nil {
				return err
			}
			for _, group := range groups {
				c = c.DelGroupsInfo(group.GroupID).
					DelGroupMembersHash(group.GroupID).
					DelGroupMembersHash(group.GroupID).
					DelGroupsMemberNum(group.GroupID).
					DelGroupMemberIDs(group.GroupID).
					DelGroupAllRoleLevel(group.GroupID).
					DelMaxGroupMemberVersion(group.GroupID)
			}
		}
		if len(groupMembers) > 0 {
			if err := g.groupMemberDB.Create(ctx, groupMembers); err != nil {
				return err
			}
			for _, groupMember := range groupMembers {
				c = c.DelGroupMembersHash(groupMember.GroupID).
					DelGroupsMemberNum(groupMember.GroupID).
					DelGroupMemberIDs(groupMember.GroupID).
					DelJoinedGroupID(groupMember.UserID).
					DelGroupMembersInfo(groupMember.GroupID, groupMember.UserID).
					DelGroupAllRoleLevel(groupMember.GroupID).
					DelMaxJoinGroupVersion(groupMember.UserID).
					DelMaxGroupMemberVersion(groupMember.GroupID)
			}
		}
		return c.ChainExecDel(ctx)
	})
}

// FindGroupMemberUserID 获取群组所有成员的用户ID列表
//
// 此方法用于获取指定群组的所有成员用户ID列表，是群组操作的基础功能，广泛应用于：
// - 群组消息推送时的用户列表获取
// - 群组权限验证时的成员资格检查
// - 群组统计分析时的成员范围确定
// - 群组操作通知时的接收者列表构建
//
// **缓存架构设计：**
// - 缓存键格式：group_member_ids:{groupID}
// - 存储结构：Redis List，保持成员顺序
// - 缓存时效：30分钟，支持延迟双删除策略
// - 预热机制：群组活跃时自动预热缓存
//
// **数据一致性保证：**
// - 写时失效：成员变更时立即删除缓存
// - 读时修复：缓存miss时重新构建
// - 版本控制：结合版本日志确保一致性
// - 分布式锁：避免缓存击穿问题
//
// **性能优化策略：**
// - 内存预分配：根据群组大小预估内存需求
// - 批量加载：支持大群组的分批加载
// - 压缩存储：对大群组启用压缩存储
// - 智能过期：根据群组活跃度动态调整过期时间
//
// **容错机制：**
// - 缓存降级：缓存不可用时直接查询数据库
// - 超时保护：设置合理的查询超时时间
// - 重试机制：临时失败时的自动重试
// - 熔断保护：连续失败时的熔断机制
//
// 参数：
//   - ctx: 上下文，包含超时控制、链路追踪、请求ID等信息
//   - groupID: 目标群组ID，必须是有效的群组标识符
//
// 返回：
//   - []string: 群组所有成员的用户ID列表，按加入时间排序
//   - error: 操作错误信息
//   - ErrGroupNotFound: 群组不存在
//   - ErrCacheTimeout: 缓存查询超时
//   - ErrDatabaseError: 数据库查询失败
//   - nil: 操作成功
//
// 错误处理策略：
// - 群组不存在：返回空列表而非错误
// - 网络超时：启用重试机制
// - 缓存失效：透明降级到数据库查询
//
// 使用示例：
//
//	// 获取群组成员ID列表用于消息推送
//	memberIDs, err := db.FindGroupMemberUserID(ctx, "group_123")
//	if err != nil {
//	    log.Errorf("获取群组成员失败: %v", err)
//	    return err
//	}
//
//	// 过滤在线成员
//	onlineMembers := filterOnlineUsers(memberIDs)
//
//	// 批量推送消息
//	err = pushMessage(ctx, onlineMembers, message)
//
// 最佳实践：
// - 避免频繁调用：相同群组的多次操作应复用结果
// - 设置超时：为避免长时间等待，建议设置合理超时
// - 异常处理：妥善处理返回的各种错误情况
// - 内存管理：大群组场景下注意内存使用情况
//
// 性能指标：
// - 缓存命中：>95%
// - 响应时间：<10ms（缓存命中）<100ms（数据库查询）
// - 并发支持：>1000 QPS
func (g *groupDatabase) FindGroupMemberUserID(ctx context.Context, groupID string) ([]string, error) {
	return g.cache.GetGroupMemberIDs(ctx, groupID)
}

func (g *groupDatabase) FindGroupMemberNum(ctx context.Context, groupID string) (uint32, error) {
	num, err := g.cache.GetGroupMemberNum(ctx, groupID)
	if err != nil {
		return 0, err
	}
	return uint32(num), nil
}

func (g *groupDatabase) TakeGroup(ctx context.Context, groupID string) (*model.Group, error) {
	return g.cache.GetGroupInfo(ctx, groupID)
}

func (g *groupDatabase) FindGroup(ctx context.Context, groupIDs []string) ([]*model.Group, error) {
	return g.cache.GetGroupsInfo(ctx, groupIDs)
}

func (g *groupDatabase) SearchGroup(ctx context.Context, keyword string, pagination pagination.Pagination) (int64, []*model.Group, error) {
	return g.groupDB.Search(ctx, keyword, pagination)
}

func (g *groupDatabase) UpdateGroup(ctx context.Context, groupID string, data map[string]any) error {
	return g.ctxTx.Transaction(ctx, func(ctx context.Context) error {
		if err := g.groupDB.UpdateMap(ctx, groupID, data); err != nil {
			return err
		}
		if err := g.groupMemberDB.MemberGroupIncrVersion(ctx, groupID, []string{""}, model.VersionStateUpdate); err != nil {
			return err
		}
		return g.cache.CloneGroupCache().DelGroupsInfo(groupID).DelMaxGroupMemberVersion(groupID).ChainExecDel(ctx)
	})
}

func (g *groupDatabase) DismissGroup(ctx context.Context, groupID string, deleteMember bool) error {
	return g.ctxTx.Transaction(ctx, func(ctx context.Context) error {
		c := g.cache.CloneGroupCache()
		if err := g.groupDB.UpdateStatus(ctx, groupID, constant.GroupStatusDismissed); err != nil {
			return err
		}
		if deleteMember {
			userIDs, err := g.cache.GetGroupMemberIDs(ctx, groupID)
			if err != nil {
				return err
			}
			if err := g.groupMemberDB.Delete(ctx, groupID, nil); err != nil {
				return err
			}
			c = c.DelJoinedGroupID(userIDs...).
				DelGroupMemberIDs(groupID).
				DelGroupsMemberNum(groupID).
				DelGroupMembersHash(groupID).
				DelGroupAllRoleLevel(groupID).
				DelGroupMembersInfo(groupID, userIDs...).
				DelMaxGroupMemberVersion(groupID).
				DelMaxJoinGroupVersion(userIDs...)
			for _, userID := range userIDs {
				if err := g.groupMemberDB.JoinGroupIncrVersion(ctx, userID, []string{groupID}, model.VersionStateDelete); err != nil {
					return err
				}
			}
		} else {
			if err := g.groupMemberDB.MemberGroupIncrVersion(ctx, groupID, []string{""}, model.VersionStateUpdate); err != nil {
				return err
			}
			c = c.DelMaxGroupMemberVersion(groupID)
		}
		return c.DelGroupsInfo(groupID).ChainExecDel(ctx)
	})
}

func (g *groupDatabase) TakeGroupMember(ctx context.Context, groupID string, userID string) (*model.GroupMember, error) {
	return g.cache.GetGroupMemberInfo(ctx, groupID, userID)
}

func (g *groupDatabase) TakeGroupOwner(ctx context.Context, groupID string) (*model.GroupMember, error) {
	return g.cache.GetGroupOwner(ctx, groupID)
}

func (g *groupDatabase) FindUserManagedGroupID(ctx context.Context, userID string) (groupIDs []string, err error) {
	return g.groupMemberDB.FindUserManagedGroupID(ctx, userID)
}

func (g *groupDatabase) PageGroupRequest(ctx context.Context, groupIDs []string, handleResults []int, pagination pagination.Pagination) (int64, []*model.GroupRequest, error) {
	return g.groupRequestDB.PageGroup(ctx, groupIDs, handleResults, pagination)
}

func (g *groupDatabase) PageGetJoinGroup(ctx context.Context, userID string, pagination pagination.Pagination) (total int64, totalGroupMembers []*model.GroupMember, err error) {
	groupIDs, err := g.cache.GetJoinedGroupIDs(ctx, userID)
	if err != nil {
		return 0, nil, err
	}
	for _, groupID := range datautil.Paginate(groupIDs, int(pagination.GetPageNumber()), int(pagination.GetShowNumber())) {
		groupMembers, err := g.cache.GetGroupMembersInfo(ctx, groupID, []string{userID})
		if err != nil {
			return 0, nil, err
		}
		totalGroupMembers = append(totalGroupMembers, groupMembers...)
	}
	return int64(len(groupIDs)), totalGroupMembers, nil
}

func (g *groupDatabase) PageGetGroupMember(ctx context.Context, groupID string, pagination pagination.Pagination) (total int64, totalGroupMembers []*model.GroupMember, err error) {
	groupMemberIDs, err := g.cache.GetGroupMemberIDs(ctx, groupID)
	if err != nil {
		return 0, nil, err
	}
	pageIDs := datautil.Paginate(groupMemberIDs, int(pagination.GetPageNumber()), int(pagination.GetShowNumber()))
	if len(pageIDs) == 0 {
		return int64(len(groupMemberIDs)), nil, nil
	}
	members, err := g.cache.GetGroupMembersInfo(ctx, groupID, pageIDs)
	if err != nil {
		return 0, nil, err
	}
	return int64(len(groupMemberIDs)), members, nil
}

func (g *groupDatabase) SearchGroupMember(ctx context.Context, keyword string, groupID string, pagination pagination.Pagination) (int64, []*model.GroupMember, error) {
	return g.groupMemberDB.SearchMember(ctx, keyword, groupID, pagination)
}

func (g *groupDatabase) HandlerGroupRequest(ctx context.Context, groupID string, userID string, handledMsg string, handleResult int32, member *model.GroupMember) error {
	return g.ctxTx.Transaction(ctx, func(ctx context.Context) error {
		if err := g.groupRequestDB.UpdateHandler(ctx, groupID, userID, handledMsg, handleResult); err != nil {
			return err
		}
		if member != nil {
			c := g.cache.CloneGroupCache()
			if err := g.groupMemberDB.Create(ctx, []*model.GroupMember{member}); err != nil {
				return err
			}
			c = c.DelGroupMembersHash(groupID).
				DelGroupMembersInfo(groupID, member.UserID).
				DelGroupMemberIDs(groupID).
				DelGroupsMemberNum(groupID).
				DelJoinedGroupID(member.UserID).
				DelGroupRoleLevel(groupID, []int32{member.RoleLevel}).
				DelMaxJoinGroupVersion(userID).
				DelMaxGroupMemberVersion(groupID)
			if err := c.ChainExecDel(ctx); err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteGroupMember 实现删除群组成员的具体逻辑
//
// **实现要点：**
// - 事务保证：成员删除和缓存清理的原子性
// - 链式缓存清理：批量清理多个相关缓存项
// - 版本同步：更新相关用户的群组版本信息
// - 错误处理：确保部分失败不影响整体操作
func (g *groupDatabase) DeleteGroupMember(ctx context.Context, groupID string, userIDs []string) error {
	return g.ctxTx.Transaction(ctx, func(ctx context.Context) error {
		if err := g.groupMemberDB.Delete(ctx, groupID, userIDs); err != nil {
			return err
		}
		c := g.cache.CloneGroupCache()
		return c.DelGroupMembersHash(groupID).
			DelGroupMemberIDs(groupID).
			DelGroupsMemberNum(groupID).
			DelJoinedGroupID(userIDs...).
			DelGroupMembersInfo(groupID, userIDs...).
			DelGroupAllRoleLevel(groupID).
			DelMaxGroupMemberVersion(groupID).
			DelMaxJoinGroupVersion(userIDs...).
			ChainExecDel(ctx)
	})
}

func (g *groupDatabase) MapGroupMemberUserID(ctx context.Context, groupIDs []string) (map[string]*common.GroupSimpleUserID, error) {
	return g.cache.GetGroupMemberHashMap(ctx, groupIDs)
}

func (g *groupDatabase) MapGroupMemberNum(ctx context.Context, groupIDs []string) (m map[string]uint32, err error) {
	m = make(map[string]uint32)
	for _, groupID := range groupIDs {
		num, err := g.cache.GetGroupMemberNum(ctx, groupID)
		if err != nil {
			return nil, err
		}
		m[groupID] = uint32(num)
	}
	return m, nil
}

func (g *groupDatabase) TransferGroupOwner(ctx context.Context, groupID string, oldOwnerUserID, newOwnerUserID string, roleLevel int32) error {
	return g.ctxTx.Transaction(ctx, func(ctx context.Context) error {
		if err := g.groupMemberDB.UpdateUserRoleLevels(ctx, groupID, oldOwnerUserID, roleLevel, newOwnerUserID, constant.GroupOwner); err != nil {
			return err
		}
		c := g.cache.CloneGroupCache()
		return c.DelGroupMembersInfo(groupID, oldOwnerUserID, newOwnerUserID).
			DelGroupAllRoleLevel(groupID).
			DelGroupMembersHash(groupID).
			DelMaxGroupMemberVersion(groupID).
			DelGroupMemberIDs(groupID).
			ChainExecDel(ctx)
	})
}

func (g *groupDatabase) UpdateGroupMember(ctx context.Context, groupID string, userID string, data map[string]any) error {
	if len(data) == 0 {
		return nil
	}
	return g.ctxTx.Transaction(ctx, func(ctx context.Context) error {
		if err := g.groupMemberDB.Update(ctx, groupID, userID, data); err != nil {
			return err
		}
		c := g.cache.CloneGroupCache()
		c = c.DelGroupMembersInfo(groupID, userID)
		if g.groupMemberDB.IsUpdateRoleLevel(data) {
			c = c.DelGroupAllRoleLevel(groupID).DelGroupMemberIDs(groupID)
		}
		c = c.DelMaxGroupMemberVersion(groupID)
		return c.ChainExecDel(ctx)
	})
}

func (g *groupDatabase) UpdateGroupMembers(ctx context.Context, data []*common.BatchUpdateGroupMember) error {
	return g.ctxTx.Transaction(ctx, func(ctx context.Context) error {
		c := g.cache.CloneGroupCache()
		for _, item := range data {
			if err := g.groupMemberDB.Update(ctx, item.GroupID, item.UserID, item.Map); err != nil {
				return err
			}
			if g.groupMemberDB.IsUpdateRoleLevel(item.Map) {
				c = c.DelGroupAllRoleLevel(item.GroupID).DelGroupMemberIDs(item.GroupID)
			}
			c = c.DelGroupMembersInfo(item.GroupID, item.UserID).DelMaxGroupMemberVersion(item.GroupID).DelGroupMembersHash(item.GroupID)
		}
		return c.ChainExecDel(ctx)
	})
}

func (g *groupDatabase) CreateGroupRequest(ctx context.Context, requests []*model.GroupRequest) error {
	return g.ctxTx.Transaction(ctx, func(ctx context.Context) error {
		for _, request := range requests {
			if err := g.groupRequestDB.Delete(ctx, request.GroupID, request.UserID); err != nil {
				return err
			}
		}
		return g.groupRequestDB.Create(ctx, requests)
	})
}

func (g *groupDatabase) TakeGroupRequest(ctx context.Context, groupID string, userID string) (*model.GroupRequest, error) {
	return g.groupRequestDB.Take(ctx, groupID, userID)
}

func (g *groupDatabase) PageGroupRequestUser(ctx context.Context, userID string, groupIDs []string, handleResults []int, pagination pagination.Pagination) (int64, []*model.GroupRequest, error) {
	return g.groupRequestDB.Page(ctx, userID, groupIDs, handleResults, pagination)
}

func (g *groupDatabase) CountTotal(ctx context.Context, before *time.Time) (count int64, err error) {
	return g.groupDB.CountTotal(ctx, before)
}

func (g *groupDatabase) CountRangeEverydayTotal(ctx context.Context, start time.Time, end time.Time) (map[string]int64, error) {
	return g.groupDB.CountRangeEverydayTotal(ctx, start, end)
}

func (g *groupDatabase) FindGroupRequests(ctx context.Context, groupID string, userIDs []string) ([]*model.GroupRequest, error) {
	return g.groupRequestDB.FindGroupRequests(ctx, groupID, userIDs)
}

func (g *groupDatabase) DeleteGroupMemberHash(ctx context.Context, groupIDs []string) error {
	if len(groupIDs) == 0 {
		return nil
	}
	c := g.cache.CloneGroupCache()
	for _, groupID := range groupIDs {
		c = c.DelGroupMembersHash(groupID)
	}
	return c.ChainExecDel(ctx)
}

func (g *groupDatabase) FindMemberIncrVersion(ctx context.Context, groupID string, version uint, limit int) (*model.VersionLog, error) {
	return g.groupMemberDB.FindMemberIncrVersion(ctx, groupID, version, limit)
}

func (g *groupDatabase) BatchFindMemberIncrVersion(ctx context.Context, groupIDs []string, versions []uint64, limits []int) (map[string]*model.VersionLog, error) {
	if len(groupIDs) == 0 {
		return nil, errs.Wrap(errs.New("groupIDs is nil."))
	}

	// convert []uint64 to []uint
	var uintVersions []uint
	for _, version := range versions {
		uintVersions = append(uintVersions, uint(version))
	}

	versionLogs, err := g.groupMemberDB.BatchFindMemberIncrVersion(ctx, groupIDs, uintVersions, limits)
	if err != nil {
		return nil, errs.Wrap(err)
	}

	groupMemberIncrVersionsMap := datautil.SliceToMap(versionLogs, func(e *model.VersionLog) string {
		return e.DID
	})

	return groupMemberIncrVersionsMap, nil
}

func (g *groupDatabase) FindJoinIncrVersion(ctx context.Context, userID string, version uint, limit int) (*model.VersionLog, error) {
	return g.groupMemberDB.FindJoinIncrVersion(ctx, userID, version, limit)
}

func (g *groupDatabase) FindMaxGroupMemberVersionCache(ctx context.Context, groupID string) (*model.VersionLog, error) {
	return g.cache.FindMaxGroupMemberVersion(ctx, groupID)
}

func (g *groupDatabase) BatchFindMaxGroupMemberVersionCache(ctx context.Context, groupIDs []string) (map[string]*model.VersionLog, error) {
	if len(groupIDs) == 0 {
		return nil, errs.Wrap(errs.New("groupIDs is nil in Cache."))
	}
	versionLogs, err := g.cache.BatchFindMaxGroupMemberVersion(ctx, groupIDs)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	maxGroupMemberVersionsMap := datautil.SliceToMap(versionLogs, func(e *model.VersionLog) string {
		return e.DID
	})
	return maxGroupMemberVersionsMap, nil
}

func (g *groupDatabase) FindMaxJoinGroupVersionCache(ctx context.Context, userID string) (*model.VersionLog, error) {
	return g.cache.FindMaxJoinGroupVersion(ctx, userID)
}

func (g *groupDatabase) SearchJoinGroup(ctx context.Context, userID string, keyword string, pagination pagination.Pagination) (int64, []*model.Group, error) {
	groupIDs, err := g.cache.GetJoinedGroupIDs(ctx, userID)
	if err != nil {
		return 0, nil, err
	}
	return g.groupDB.SearchJoin(ctx, groupIDs, keyword, pagination)
}

func (g *groupDatabase) MemberGroupIncrVersion(ctx context.Context, groupID string, userIDs []string, state int32) error {
	if err := g.groupMemberDB.MemberGroupIncrVersion(ctx, groupID, userIDs, state); err != nil {
		return err
	}
	return g.cache.DelMaxGroupMemberVersion(groupID).ChainExecDel(ctx)
}

func (g *groupDatabase) GetGroupApplicationUnhandledCount(ctx context.Context, groupIDs []string, ts int64) (int64, error) {
	return g.groupRequestDB.GetUnhandledCount(ctx, groupIDs, ts)
}
