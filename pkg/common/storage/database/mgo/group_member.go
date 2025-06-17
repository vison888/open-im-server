/*
 * 群组成员数据访问层 - MongoDB实现
 *
 * 本文件实现了群组成员相关的所有数据库操作，是OpenIM群组系统的核心数据访问层。
 * 主要负责群组成员的增删改查、版本控制、权限管理等功能。
 *
 * 核心功能模块：
 * 1. 成员生命周期管理
 *    - 成员创建：批量添加群组成员，支持事务操作
 *    - 成员删除：支持单个/批量删除，自动维护版本信息
 *    - 成员更新：角色变更、信息修改等操作
 *
 * 2. 版本控制系统
 *    - 双重版本追踪：群组维度(member)和用户维度(join)的版本管理
 *    - 增量同步支持：客户端可基于版本号进行增量数据同步
 *    - 状态追踪：插入、更新、删除操作的完整状态记录
 *
 * 3. 查询与检索
 *    - 多维度查询：按群组、用户、角色等维度查询成员信息
 *    - 分页支持：大数据量场景下的高效分页查询
 *    - 搜索功能：基于昵称的模糊搜索
 *
 * 4. 权限与角色管理
 *    - 角色层级：群主、管理员、普通成员的权限控制
 *    - 角色变更：支持角色升级、降级操作
 *    - 权限验证：操作权限的数据层验证
 *
 * 5. 性能优化特性
 *    - 索引优化：复合索引确保查询性能
 *    - 批量操作：减少数据库交互次数
 *    - 排序策略：按角色等级和创建时间的智能排序
 *
 * 技术架构：
 * - 数据库：MongoDB，支持事务和复杂查询
 * - 版本控制：基于VersionLog的分布式版本管理
 * - 事务支持：mongoutil.IncrVersion确保数据一致性
 * - 索引策略：group_id + user_id 复合唯一索引
 *
 * 数据一致性保证：
 * - 事务操作：所有写操作都在事务中执行
 * - 版本同步：数据变更和版本更新的原子性
 * - 约束检查：唯一性约束防止重复数据
 */

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

package mgo

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/tools/log"

	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/tools/db/mongoutil"
	"github.com/openimsdk/tools/db/pagination"
	"github.com/openimsdk/tools/errs"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// NewGroupMember 创建群组成员数据访问对象
//
// 此函数初始化群组成员的MongoDB数据访问层，包括：
// 1. 集合初始化：创建群组成员主集合
// 2. 索引创建：建立group_id+user_id复合唯一索引，确保成员唯一性
// 3. 版本日志初始化：创建两个版本追踪系统
//   - member: 群组维度的成员版本管理（群组 -> 成员列表变更）
//   - join: 用户维度的加群版本管理（用户 -> 加入群组列表变更）
//
// 双重版本控制的设计理念：
// - 群组视角：当群组成员发生变化时，群组的成员版本递增
// - 用户视角：当用户加入/退出群组时，用户的加群版本递增
// - 客户端可根据不同视角进行增量同步
//
// 参数：
// - db: MongoDB数据库实例
//
// 返回：
// - database.GroupMember: 群组成员数据访问接口实现
// - error: 初始化错误
func NewGroupMember(db *mongo.Database) (database.GroupMember, error) {
	// 获取群组成员主集合
	coll := db.Collection(database.GroupMemberName)

	// 创建复合唯一索引：group_id + user_id
	// 确保同一用户在同一群组中只能有一条记录
	_, err := coll.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{
			{Key: "group_id", Value: 1}, // 群组ID升序
			{Key: "user_id", Value: 1},  // 用户ID升序
		},
		Options: options.Index().SetUnique(true), // 设置唯一约束
	})
	if err != nil {
		return nil, errs.Wrap(err)
	}

	// 初始化群组成员版本日志（群组维度）
	// 用于追踪群组成员列表的变更历史
	member, err := NewVersionLog(db.Collection(database.GroupMemberVersionName))
	if err != nil {
		return nil, err
	}

	// 初始化用户加群版本日志（用户维度）
	// 用于追踪用户加入群组列表的变更历史
	join, err := NewVersionLog(db.Collection(database.GroupJoinVersionName))
	if err != nil {
		return nil, err
	}

	return &GroupMemberMgo{coll: coll, member: member, join: join}, nil
}

// GroupMemberMgo 群组成员MongoDB数据访问实现
//
// 这是群组成员数据访问层的核心结构体，整合了：
// - 主数据存储：群组成员的基本信息存储
// - 版本控制：双重版本追踪系统
// - 查询优化：高效的数据检索和排序
//
// 架构设计：
// - coll: 主数据集合，存储群组成员的完整信息
// - member: 群组维度版本日志，追踪群组成员变更
// - join: 用户维度版本日志，追踪用户加群变更
//
// 版本控制策略：
// - 写操作触发：每次数据变更都会更新对应的版本日志
// - 双重追踪：同时维护群组和用户两个维度的版本信息
// - 增量同步：客户端可基于版本号获取变更数据
type GroupMemberMgo struct {
	coll   *mongo.Collection   // 群组成员主数据集合
	member database.VersionLog // 群组成员版本日志（群组维度）
	join   database.VersionLog // 用户加群版本日志（用户维度）
}

// memberSort 群组成员排序策略
//
// 定义群组成员的默认排序规则，确保成员列表的一致性和可预测性：
// 1. 按角色等级降序：群主 > 管理员 > 普通成员
// 2. 按创建时间升序：相同角色按加入时间排序
//
// 排序的业务意义：
// - 权限层级：高权限用户优先显示
// - 时间顺序：体现加入群组的先后关系
// - 用户体验：管理员和群主在列表顶部，便于识别
//
// 返回：
// - any: MongoDB排序文档
func (g *GroupMemberMgo) memberSort() any {
	return bson.D{
		{Key: "role_level", Value: -1}, // 角色等级降序（数值越大权限越高）
		{Key: "create_time", Value: 1}, // 创建时间升序（早加入的在前）
	}
}

// Create 批量创建群组成员
//
// 此方法实现了群组成员的批量创建操作，采用事务机制确保数据一致性。
// 操作包含三个原子步骤，任何一步失败都会回滚整个操作。
//
// 事务操作流程：
// 1. 数据插入：将成员信息批量插入主集合
// 2. 群组版本更新：更新每个群组的成员版本（群组维度）
// 3. 用户版本更新：更新每个用户的加群版本（用户维度）
//
// 版本控制逻辑：
// - 群组维度：记录哪些用户加入了哪个群组
// - 用户维度：记录用户加入了哪些群组
// - 状态标记：使用 VersionStateInsert 标记为插入操作
//
// 性能优化：
// - 批量插入：减少数据库交互次数
// - 分组处理：按群组和用户分组更新版本信息
// - 事务保证：确保数据一致性
//
// 参数：
// - ctx: 上下文，用于超时控制和链路追踪
// - groupMembers: 要创建的群组成员列表
//
// 返回：
// - error: 操作错误，nil表示成功
func (g *GroupMemberMgo) Create(ctx context.Context, groupMembers []*model.GroupMember) (err error) {
	return mongoutil.IncrVersion(
		// 第一步：批量插入群组成员数据
		func() error {
			return mongoutil.InsertMany(ctx, g.coll, groupMembers)
		},
		// 第二步：更新群组成员版本（群组维度）
		func() error {
			// 按群组分组，统计每个群组新增的成员
			gms := make(map[string][]string)
			for _, member := range groupMembers {
				gms[member.GroupID] = append(gms[member.GroupID], member.UserID)
			}
			// 为每个群组更新成员版本
			for groupID, userIDs := range gms {
				if err := g.member.IncrVersion(ctx, groupID, userIDs, model.VersionStateInsert); err != nil {
					return err
				}
			}
			return nil
		},
		// 第三步：更新用户加群版本（用户维度）
		func() error {
			// 按用户分组，统计每个用户加入的群组
			gms := make(map[string][]string)
			for _, member := range groupMembers {
				gms[member.UserID] = append(gms[member.UserID], member.GroupID)
			}
			// 为每个用户更新加群版本
			for userID, groupIDs := range gms {
				if err := g.join.IncrVersion(ctx, userID, groupIDs, model.VersionStateInsert); err != nil {
					return err
				}
			}
			return nil
		})
}

// Delete 删除群组成员
//
// 此方法实现了群组成员的删除操作，支持单个用户删除和批量删除。
// 同样采用事务机制，确保数据删除和版本更新的原子性。
//
// 删除模式：
// 1. 指定用户删除：删除特定用户在群组中的成员记录
// 2. 全群删除：删除整个群组的所有成员（userIDs为空时）
//
// 事务操作流程：
// 1. 数据删除：从主集合中删除匹配的成员记录
// 2. 群组版本处理：
//   - 全群删除：直接删除群组的版本记录
//   - 部分删除：更新群组版本，标记用户为删除状态
//
// 3. 用户版本更新：更新每个用户的加群版本
//
// 版本控制策略：
// - 群组维度：记录成员的删除操作
// - 用户维度：记录用户退出群组的操作
// - 状态标记：使用 VersionStateDelete 标记删除操作
//
// 参数：
// - ctx: 上下文
// - groupID: 群组ID
// - userIDs: 要删除的用户ID列表，为空表示删除所有成员
//
// 返回：
// - error: 操作错误
func (g *GroupMemberMgo) Delete(ctx context.Context, groupID string, userIDs []string) (err error) {
	// 构建删除过滤条件
	filter := bson.M{"group_id": groupID}
	if len(userIDs) > 0 {
		filter["user_id"] = bson.M{"$in": userIDs}
	}

	return mongoutil.IncrVersion(
		// 第一步：删除群组成员数据
		func() error {
			return mongoutil.DeleteMany(ctx, g.coll, filter)
		},
		// 第二步：处理群组成员版本
		func() error {
			if len(userIDs) == 0 {
				// 全群删除：直接删除群组版本记录
				return g.member.Delete(ctx, groupID)
			} else {
				// 部分删除：更新群组版本，标记用户删除
				return g.member.IncrVersion(ctx, groupID, userIDs, model.VersionStateDelete)
			}
		},
		// 第三步：更新用户加群版本
		func() error {
			// 为每个被删除的用户更新加群版本
			for _, userID := range userIDs {
				if err := g.join.IncrVersion(ctx, userID, []string{groupID}, model.VersionStateDelete); err != nil {
					return err
				}
			}
			return nil
		})
}

// UpdateRoleLevel 更新群组成员角色等级
//
// 此方法专门用于更新群组成员的角色等级，如群主转让、管理员任免等操作。
// 角色变更是敏感操作，需要特殊的版本控制处理。
//
// 角色等级说明：
// - constant.GroupOwner: 群主，拥有最高权限
// - constant.GroupAdmin: 管理员，拥有管理权限
// - constant.GroupOrdinaryUsers: 普通成员，基础权限
//
// 版本控制特殊处理：
// - 角色变更会影响成员列表的排序（按角色等级排序）
// - 使用 VersionSortChangeID 标记排序变更
// - 同时更新目标用户的版本信息
//
// 事务操作：
// 1. 数据更新：更新成员的角色等级字段
// 2. 版本更新：标记排序变更和用户更新
//
// 参数：
// - ctx: 上下文
// - groupID: 群组ID
// - userID: 目标用户ID
// - roleLevel: 新的角色等级
//
// 返回：
// - error: 操作错误
func (g *GroupMemberMgo) UpdateRoleLevel(ctx context.Context, groupID string, userID string, roleLevel int32) error {
	return mongoutil.IncrVersion(
		// 第一步：更新角色等级
		func() error {
			return mongoutil.UpdateOne(ctx, g.coll,
				bson.M{"group_id": groupID, "user_id": userID},
				bson.M{"$set": bson.M{"role_level": roleLevel}},
				true)
		},
		// 第二步：更新版本信息
		func() error {
			// 角色变更影响排序，需要特殊标记
			return g.member.IncrVersion(ctx, groupID,
				[]string{model.VersionSortChangeID, userID},
				model.VersionStateUpdate)
		})
}

// UpdateUserRoleLevels 批量更新用户角色等级
//
// 此方法用于同时更新两个用户的角色等级，典型场景是群主转让：
// - 原群主降级为普通成员或管理员
// - 新群主升级为群主角色
//
// 批量更新的优势：
// - 原子性：两个角色变更在同一事务中完成
// - 一致性：避免中间状态（如短暂的无群主状态）
// - 效率：减少数据库交互次数
//
// 版本控制：
// - 同时标记两个用户的版本变更
// - 使用排序变更标记，因为角色变更影响排序
//
// 参数：
// - ctx: 上下文
// - groupID: 群组ID
// - firstUserID: 第一个用户ID
// - firstUserRoleLevel: 第一个用户的新角色等级
// - secondUserID: 第二个用户ID
// - secondUserRoleLevel: 第二个用户的新角色等级
//
// 返回：
// - error: 操作错误
func (g *GroupMemberMgo) UpdateUserRoleLevels(ctx context.Context, groupID string, firstUserID string, firstUserRoleLevel int32, secondUserID string, secondUserRoleLevel int32) error {
	return mongoutil.IncrVersion(
		// 第一步：批量更新角色等级
		func() error {
			// 更新第一个用户的角色
			if err := mongoutil.UpdateOne(ctx, g.coll,
				bson.M{"group_id": groupID, "user_id": firstUserID},
				bson.M{"$set": bson.M{"role_level": firstUserRoleLevel}},
				true); err != nil {
				return err
			}
			// 更新第二个用户的角色
			if err := mongoutil.UpdateOne(ctx, g.coll,
				bson.M{"group_id": groupID, "user_id": secondUserID},
				bson.M{"$set": bson.M{"role_level": secondUserRoleLevel}},
				true); err != nil {
				return err
			}
			return nil
		},
		// 第二步：更新版本信息
		func() error {
			// 同时标记两个用户的版本变更和排序变更
			return g.member.IncrVersion(ctx, groupID,
				[]string{model.VersionSortChangeID, firstUserID, secondUserID},
				model.VersionStateUpdate)
		})
}

// Update 更新群组成员信息
//
// 此方法用于更新群组成员的各种信息，如昵称、头像、扩展字段等。
// 支持灵活的字段更新，只更新传入的字段。
//
// 更新类型判断：
// - 角色等级更新：需要特殊的版本控制处理（影响排序）
// - 普通信息更新：常规的版本控制处理
//
// 版本控制策略：
// - 角色更新：使用排序变更标记 + 用户标记
// - 普通更新：只使用用户标记
//
// 参数：
// - ctx: 上下文
// - groupID: 群组ID
// - userID: 用户ID
// - data: 要更新的字段映射
//
// 返回：
// - error: 操作错误
func (g *GroupMemberMgo) Update(ctx context.Context, groupID string, userID string, data map[string]any) (err error) {
	// 空数据检查
	if len(data) == 0 {
		return nil
	}

	return mongoutil.IncrVersion(
		// 第一步：更新成员信息
		func() error {
			return mongoutil.UpdateOne(ctx, g.coll,
				bson.M{"group_id": groupID, "user_id": userID},
				bson.M{"$set": data},
				true)
		},
		// 第二步：更新版本信息
		func() error {
			var userIDs []string
			if g.IsUpdateRoleLevel(data) {
				// 角色等级更新，需要排序变更标记
				userIDs = []string{model.VersionSortChangeID, userID}
			} else {
				// 普通信息更新
				userIDs = []string{userID}
			}
			return g.member.IncrVersion(ctx, groupID, userIDs, model.VersionStateUpdate)
		})
}

// FindMemberUserID 查询群组所有成员的用户ID
//
// 此方法用于获取指定群组的所有成员用户ID列表，按照默认排序规则排序。
// 常用于权限验证、消息推送、成员统计等场景。
//
// 查询优化：
// - 只返回user_id字段，减少网络传输
// - 使用投影查询，提高查询效率
// - 应用默认排序，保证结果一致性
//
// 参数：
// - ctx: 上下文
// - groupID: 群组ID
//
// 返回：
// - userIDs: 用户ID列表（按角色和加入时间排序）
// - error: 查询错误
func (g *GroupMemberMgo) FindMemberUserID(ctx context.Context, groupID string) (userIDs []string, err error) {
	return mongoutil.Find[string](ctx, g.coll,
		bson.M{"group_id": groupID},
		options.Find().
			SetProjection(bson.M{"_id": 0, "user_id": 1}). // 只返回user_id字段
			SetSort(g.memberSort()))                       // 应用默认排序
}

// Find 查询群组成员信息
//
// 此方法用于查询指定群组中指定用户的成员信息。
// 支持查询所有成员（userIDs为空）或特定用户列表。
//
// 查询模式：
// - 全量查询：userIDs为空时，返回群组所有成员
// - 指定查询：userIDs不为空时，只返回指定用户的成员信息
//
// 使用场景：
// - 成员信息展示
// - 权限验证
// - 批量操作前的数据获取
//
// 参数：
// - ctx: 上下文
// - groupID: 群组ID
// - userIDs: 用户ID列表，为空表示查询所有成员
//
// 返回：
// - []*model.GroupMember: 群组成员信息列表
// - error: 查询错误
func (g *GroupMemberMgo) Find(ctx context.Context, groupID string, userIDs []string) ([]*model.GroupMember, error) {
	// 构建查询过滤条件
	filter := bson.M{"group_id": groupID}
	if len(userIDs) > 0 {
		filter["user_id"] = bson.M{"$in": userIDs}
	}
	return mongoutil.Find[*model.GroupMember](ctx, g.coll, filter)
}

// FindInGroup 查询用户在指定群组中的成员信息
//
// 此方法从用户维度查询成员信息，获取指定用户在指定群组列表中的成员记录。
// 主要用于用户相关的群组信息查询。
//
// 查询逻辑：
// - 固定用户ID
// - 可选群组ID列表过滤
// - 返回用户在这些群组中的成员信息
//
// 使用场景：
// - 用户群组列表查询
// - 用户权限验证
// - 跨群组操作的权限检查
//
// 参数：
// - ctx: 上下文
// - userID: 用户ID
// - groupIDs: 群组ID列表，为空表示查询用户所有群组
//
// 返回：
// - []*model.GroupMember: 用户的群组成员信息列表
// - error: 查询错误
func (g *GroupMemberMgo) FindInGroup(ctx context.Context, userID string, groupIDs []string) ([]*model.GroupMember, error) {
	// 构建查询过滤条件
	filter := bson.M{"user_id": userID}
	if len(groupIDs) > 0 {
		filter["group_id"] = bson.M{"$in": groupIDs}
	}
	return mongoutil.Find[*model.GroupMember](ctx, g.coll, filter)
}

// Take 查询单个群组成员信息
//
// 此方法用于精确查询指定用户在指定群组中的成员信息。
// 返回单条记录或不存在错误。
//
// 使用场景：
// - 权限验证：检查用户是否在群组中
// - 角色查询：获取用户在群组中的角色
// - 信息展示：显示用户的群组身份信息
//
// 参数：
// - ctx: 上下文
// - groupID: 群组ID
// - userID: 用户ID
//
// 返回：
// - groupMember: 群组成员信息
// - error: 查询错误，记录不存在时返回 ErrRecordNotFound
func (g *GroupMemberMgo) Take(ctx context.Context, groupID string, userID string) (groupMember *model.GroupMember, err error) {
	return mongoutil.FindOne[*model.GroupMember](ctx, g.coll,
		bson.M{"group_id": groupID, "user_id": userID})
}

// TakeOwner 查询群组群主信息
//
// 此方法用于查询指定群组的群主成员信息。
// 每个群组有且仅有一个群主。
//
// 业务规则：
// - 每个群组必须有一个群主
// - 群主角色等级为 constant.GroupOwner
// - 群主拥有群组的最高权限
//
// 使用场景：
// - 权限验证：确认操作者是否为群主
// - 信息展示：显示群组的群主信息
// - 管理操作：群主相关的管理功能
//
// 参数：
// - ctx: 上下文
// - groupID: 群组ID
//
// 返回：
// - groupMember: 群主成员信息
// - error: 查询错误
func (g *GroupMemberMgo) TakeOwner(ctx context.Context, groupID string) (groupMember *model.GroupMember, err error) {
	return mongoutil.FindOne[*model.GroupMember](ctx, g.coll,
		bson.M{"group_id": groupID, "role_level": constant.GroupOwner})
}

// FindRoleLevelUserIDs 查询指定角色等级的用户ID列表
//
// 此方法用于查询群组中具有特定角色等级的所有用户ID。
// 常用于权限管理和批量操作。
//
// 角色等级说明：
// - constant.GroupOwner: 群主
// - constant.GroupAdmin: 管理员
// - constant.GroupOrdinaryUsers: 普通成员
//
// 使用场景：
// - 权限验证：获取管理员列表进行权限检查
// - 消息推送：向特定角色用户发送通知
// - 统计分析：角色分布统计
//
// 参数：
// - ctx: 上下文
// - groupID: 群组ID
// - roleLevel: 角色等级
//
// 返回：
// - []string: 指定角色的用户ID列表
// - error: 查询错误
func (g *GroupMemberMgo) FindRoleLevelUserIDs(ctx context.Context, groupID string, roleLevel int32) ([]string, error) {
	return mongoutil.Find[string](ctx, g.coll,
		bson.M{"group_id": groupID, "role_level": roleLevel},
		options.Find().SetProjection(bson.M{"_id": 0, "user_id": 1}))
}

// SearchMember 搜索群组成员
//
// 此方法提供基于昵称的群组成员搜索功能，支持模糊匹配和分页。
// 主要用于大群组中快速查找特定成员。
//
// 搜索特性：
// - 模糊匹配：基于昵称的正则表达式搜索
// - 分页支持：处理大数据量的搜索结果
// - 排序保证：按默认排序规则返回结果
//
// 使用场景：
// - 成员搜索：在群组中搜索特定成员
// - 管理功能：管理员搜索成员进行管理操作
// - 用户体验：提供便捷的成员查找功能
//
// 参数：
// - ctx: 上下文
// - keyword: 搜索关键词（昵称）
// - groupID: 群组ID
// - pagination: 分页参数
//
// 返回：
// - int64: 匹配的总记录数
// - []*model.GroupMember: 当前页的成员列表
// - error: 搜索错误
func (g *GroupMemberMgo) SearchMember(ctx context.Context, keyword string, groupID string, pagination pagination.Pagination) (int64, []*model.GroupMember, error) {
	// 构建搜索过滤条件：群组ID + 昵称模糊匹配
	filter := bson.M{
		"group_id": groupID,
		"nickname": bson.M{"$regex": keyword}, // 正则表达式模糊匹配
	}
	return mongoutil.FindPage[*model.GroupMember](ctx, g.coll, filter, pagination,
		options.Find().SetSort(g.memberSort()))
}

// FindUserJoinedGroupID 查询用户加入的所有群组ID
//
// 此方法从用户维度查询其加入的所有群组ID列表。
// 结果按默认排序规则排序，保证一致性。
//
// 使用场景：
// - 用户群组列表：获取用户参与的所有群组
// - 权限验证：检查用户的群组权限范围
// - 数据统计：用户活跃度分析
//
// 参数：
// - ctx: 上下文
// - userID: 用户ID
//
// 返回：
// - groupIDs: 用户加入的群组ID列表
// - error: 查询错误
func (g *GroupMemberMgo) FindUserJoinedGroupID(ctx context.Context, userID string) (groupIDs []string, err error) {
	return mongoutil.Find[string](ctx, g.coll,
		bson.M{"user_id": userID},
		options.Find().
			SetProjection(bson.M{"_id": 0, "group_id": 1}). // 只返回group_id字段
			SetSort(g.memberSort()))                        // 应用默认排序
}

// TakeGroupMemberNum 统计群组成员数量
//
// 此方法用于统计指定群组的成员总数。
// 返回精确的成员数量，用于各种统计和限制检查。
//
// 使用场景：
// - 成员数量显示：在群组信息中显示成员总数
// - 限制检查：验证群组成员数量是否达到上限
// - 统计分析：群组规模分析
//
// 参数：
// - ctx: 上下文
// - groupID: 群组ID
//
// 返回：
// - count: 群组成员数量
// - error: 统计错误
func (g *GroupMemberMgo) TakeGroupMemberNum(ctx context.Context, groupID string) (count int64, err error) {
	return mongoutil.Count(ctx, g.coll, bson.M{"group_id": groupID})
}

// FindUserManagedGroupID 查询用户管理的群组ID列表
//
// 此方法查询用户作为群主或管理员的群组ID列表。
// 用于权限验证和管理功能。
//
// 管理角色包括：
// - constant.GroupOwner: 群主
// - constant.GroupAdmin: 管理员
//
// 使用场景：
// - 权限验证：检查用户的管理权限范围
// - 管理界面：显示用户可管理的群组列表
// - 操作限制：限制管理操作的范围
//
// 参数：
// - ctx: 上下文
// - userID: 用户ID
//
// 返回：
// - groupIDs: 用户管理的群组ID列表
// - error: 查询错误
func (g *GroupMemberMgo) FindUserManagedGroupID(ctx context.Context, userID string) (groupIDs []string, err error) {
	// 构建查询条件：用户ID + 管理角色（群主或管理员）
	filter := bson.M{
		"user_id": userID,
		"role_level": bson.M{
			"$in": []int{constant.GroupOwner, constant.GroupAdmin},
		},
	}
	return mongoutil.Find[string](ctx, g.coll, filter,
		options.Find().SetProjection(bson.M{"_id": 0, "group_id": 1}))
}

// IsUpdateRoleLevel 检查是否为角色等级更新
//
// 此方法用于判断更新数据中是否包含角色等级字段的修改。
// 角色等级更新需要特殊的版本控制处理。
//
// 检查逻辑：
// - 检查更新数据中是否包含 "role_level" 字段
// - 用于决定版本控制的处理策略
//
// 使用场景：
// - 版本控制：决定是否需要排序变更标记
// - 权限验证：角色变更的特殊处理
// - 审计日志：记录敏感操作
//
// 参数：
// - data: 更新数据映射
//
// 返回：
// - bool: true表示包含角色等级更新
func (g *GroupMemberMgo) IsUpdateRoleLevel(data map[string]any) bool {
	if len(data) == 0 {
		return false
	}
	_, ok := data["role_level"]
	return ok
}

// JoinGroupIncrVersion 更新用户加群版本
//
// 此方法用于手动更新用户加群版本信息，主要用于特殊场景下的版本同步。
// 直接操作用户维度的版本日志。
//
// 使用场景：
// - 数据修复：修复版本不一致的问题
// - 特殊操作：某些不通过常规流程的操作
// - 版本同步：手动触发版本更新
//
// 参数：
// - ctx: 上下文
// - userID: 用户ID
// - groupIDs: 相关群组ID列表
// - state: 版本状态（插入/更新/删除）
//
// 返回：
// - error: 操作错误
func (g *GroupMemberMgo) JoinGroupIncrVersion(ctx context.Context, userID string, groupIDs []string, state int32) error {
	return g.join.IncrVersion(ctx, userID, groupIDs, state)
}

// MemberGroupIncrVersion 更新群组成员版本
//
// 此方法用于手动更新群组成员版本信息，主要用于特殊场景下的版本同步。
// 直接操作群组维度的版本日志。
//
// 使用场景：
// - 数据修复：修复版本不一致的问题
// - 特殊操作：某些不通过常规流程的操作
// - 版本同步：手动触发版本更新
//
// 参数：
// - ctx: 上下文
// - groupID: 群组ID
// - userIDs: 相关用户ID列表
// - state: 版本状态（插入/更新/删除）
//
// 返回：
// - error: 操作错误
func (g *GroupMemberMgo) MemberGroupIncrVersion(ctx context.Context, groupID string, userIDs []string, state int32) error {
	return g.member.IncrVersion(ctx, groupID, userIDs, state)
}

// FindMemberIncrVersion 查询群组成员增量版本信息
//
// 此方法用于获取群组成员的增量版本信息，支持客户端的增量同步。
// 客户端可以基于本地版本号获取后续的变更信息。
//
// 增量同步原理：
// - 客户端提供本地版本号
// - 服务端返回该版本之后的所有变更
// - 客户端应用变更，更新本地数据和版本号
//
// 使用场景：
// - 客户端同步：获取群组成员的增量变更
// - 离线恢复：客户端重新上线后的数据同步
// - 性能优化：避免全量数据传输
//
// 参数：
// - ctx: 上下文
// - groupID: 群组ID
// - version: 客户端当前版本号
// - limit: 返回记录数限制
//
// 返回：
// - *model.VersionLog: 版本日志信息
// - error: 查询错误
func (g *GroupMemberMgo) FindMemberIncrVersion(ctx context.Context, groupID string, version uint, limit int) (*model.VersionLog, error) {
	log.ZDebug(ctx, "find member incr version", "groupID", groupID, "version", version)
	return g.member.FindChangeLog(ctx, groupID, version, limit)
}

// BatchFindMemberIncrVersion 批量查询群组成员增量版本信息
//
// 此方法支持批量查询多个群组的成员增量版本信息，提高查询效率。
// 适用于用户同时参与多个群组的场景。
//
// 批量查询优势：
// - 减少网络往返：一次请求获取多个群组的版本信息
// - 提高效率：数据库层面的批量优化
// - 简化逻辑：客户端统一处理多个群组的同步
//
// 使用场景：
// - 多群组同步：用户同时同步多个群组的成员信息
// - 批量操作：管理员批量管理多个群组
// - 性能优化：减少客户端的请求次数
//
// 参数：
// - ctx: 上下文
// - groupIDs: 群组ID列表
// - versions: 对应的版本号列表
// - limits: 对应的记录数限制列表
//
// 返回：
// - []*model.VersionLog: 版本日志信息列表
// - error: 查询错误
func (g *GroupMemberMgo) BatchFindMemberIncrVersion(ctx context.Context, groupIDs []string, versions []uint, limits []int) ([]*model.VersionLog, error) {
	log.ZDebug(ctx, "Batch find member incr version", "groupIDs", groupIDs, "versions", versions)
	return g.member.BatchFindChangeLog(ctx, groupIDs, versions, limits)
}

// FindJoinIncrVersion 查询用户加群增量版本信息
//
// 此方法用于获取用户加群的增量版本信息，从用户维度进行增量同步。
// 客户端可以获取用户加入/退出群组的变更信息。
//
// 用户维度同步：
// - 追踪用户加入的群组列表变更
// - 支持用户群组列表的增量更新
// - 提供用户视角的数据同步
//
// 使用场景：
// - 用户群组列表同步：更新用户的群组列表
// - 权限更新：同步用户的群组权限变更
// - 界面刷新：更新用户界面的群组显示
//
// 参数：
// - ctx: 上下文
// - userID: 用户ID
// - version: 用户当前版本号
// - limit: 返回记录数限制
//
// 返回：
// - *model.VersionLog: 版本日志信息
// - error: 查询错误
func (g *GroupMemberMgo) FindJoinIncrVersion(ctx context.Context, userID string, version uint, limit int) (*model.VersionLog, error) {
	log.ZDebug(ctx, "find join incr version", "userID", userID, "version", version)
	return g.join.FindChangeLog(ctx, userID, version, limit)
}
