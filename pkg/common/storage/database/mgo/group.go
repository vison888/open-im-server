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
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"

	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/tools/db/mongoutil"
	"github.com/openimsdk/tools/db/pagination"
	"github.com/openimsdk/tools/errs"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

/*
 * 群组基础信息数据访问层 - MongoDB实现
 *
 * 本文件实现了群组基础信息相关的所有数据库操作，是OpenIM群组系统的核心数据访问层。
 * 主要负责群组的基本信息管理，包括群组的创建、查询、更新、搜索等功能。
 *
 * 核心功能模块：
 * 1. 群组生命周期管理
 *    - 群组创建：支持批量创建群组基础信息
 *    - 群组更新：灵活的字段更新机制
 *    - 状态管理：群组状态的维护和控制
 *
 * 2. 查询与检索系统
 *    - 精确查询：基于群组ID的快速查询
 *    - 批量查询：支持多群组的批量信息获取
 *    - 模糊搜索：基于群组名称的搜索功能
 *    - 分页支持：大数据量场景下的高效分页
 *
 * 3. 统计与分析
 *    - 数量统计：群组总数统计
 *    - 时间范围统计：按时间段的群组创建统计
 *    - 日期分组统计：按日期分组的详细统计
 *
 * 4. 排序与过滤
 *    - 智能排序：按群组名称和创建时间的组合排序
 *    - 状态过滤：过滤已解散群组
 *    - 关键词过滤：支持群组名称的模糊匹配
 *
 * 5. 性能优化特性
 *    - 索引优化：群组ID唯一索引确保查询性能
 *    - 投影查询：只返回需要的字段，减少网络传输
 *    - 聚合查询：使用MongoDB聚合管道进行复杂统计
 *
 * 技术架构：
 * - 数据库：MongoDB，支持复杂查询和聚合操作
 * - 索引策略：group_id 唯一索引
 * - 查询优化：使用投影和排序优化查询性能
 * - 聚合分析：MongoDB聚合管道进行统计分析
 *
 * 数据一致性保证：
 * - 唯一性约束：群组ID的唯一性保证
 * - 原子操作：单文档操作的原子性
 * - 状态一致性：群组状态的正确维护
 */

// NewGroupMongo 创建群组数据访问对象
//
// 此函数初始化群组基础信息的MongoDB数据访问层，包括：
// 1. 集合初始化：创建群组信息主集合
// 2. 索引创建：建立group_id唯一索引，确保群组ID唯一性
//
// 索引设计说明：
// - group_id唯一索引：确保每个群组ID在系统中唯一
// - 提高查询性能：基于群组ID的查询将使用索引
// - 数据完整性：防止重复的群组ID
//
// 参数：
// - db: MongoDB数据库实例
//
// 返回：
// - database.Group: 群组数据访问接口实现
// - error: 初始化错误
func NewGroupMongo(db *mongo.Database) (database.Group, error) {
	// 获取群组信息主集合
	coll := db.Collection(database.GroupName)

	// 创建群组ID唯一索引
	// 确保每个群组ID在系统中唯一，提高查询性能
	_, err := coll.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{
			{Key: "group_id", Value: 1}, // 群组ID升序索引
		},
		Options: options.Index().SetUnique(true), // 设置唯一约束
	})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return &GroupMgo{coll: coll}, nil
}

// GroupMgo 群组MongoDB数据访问实现
//
// 这是群组基础信息数据访问层的核心结构体，提供：
// - 基础数据操作：增删改查的完整实现
// - 查询优化：高效的数据检索和排序
// - 统计分析：群组数据的统计和分析功能
//
// 设计特点：
// - 简洁设计：专注于群组基础信息的管理
// - 性能优化：使用索引和投影优化查询
// - 扩展性：支持复杂的查询和统计需求
type GroupMgo struct {
	coll *mongo.Collection // 群组信息主数据集合
}

// sortGroup 群组排序策略
//
// 定义群组的默认排序规则，确保群组列表的一致性和用户体验：
// 1. 按群组名称升序：字母顺序排列，便于用户查找
// 2. 按创建时间升序：相同名称按创建时间排序
//
// 排序的业务意义：
// - 用户体验：按字母顺序排列便于查找
// - 时间顺序：体现群组创建的先后关系
// - 一致性：保证不同查询的排序一致性
//
// 返回：
// - any: MongoDB排序文档
func (g *GroupMgo) sortGroup() any {
	return bson.D{
		{"group_name", 1},  // 群组名称升序（字母顺序）
		{"create_time", 1}, // 创建时间升序（早创建的在前）
	}
}

// Create 批量创建群组
//
// 此方法实现了群组基础信息的批量创建操作。
// 支持一次性创建多个群组，提高创建效率。
//
// 创建特点：
// - 批量操作：减少数据库交互次数
// - 原子性：MongoDB的批量插入保证原子性
// - 性能优化：一次性插入多个文档
//
// 使用场景：
// - 群组创建：用户创建新群组
// - 批量导入：管理员批量创建群组
// - 数据迁移：系统数据迁移场景
//
// 参数：
// - ctx: 上下文，用于超时控制和链路追踪
// - groups: 要创建的群组信息列表
//
// 返回：
// - error: 操作错误，nil表示成功
func (g *GroupMgo) Create(ctx context.Context, groups []*model.Group) (err error) {
	return mongoutil.InsertMany(ctx, g.coll, groups)
}

// UpdateStatus 更新群组状态
//
// 此方法专门用于更新群组的状态字段，如正常、禁言、解散等状态。
// 群组状态是重要的业务字段，需要专门的方法进行管理。
//
// 状态类型说明：
// - constant.GroupOk: 正常状态
// - constant.GroupStatusMuted: 全群禁言状态
// - constant.GroupStatusDismissed: 已解散状态
//
// 使用场景：
// - 群组禁言：管理员设置全群禁言
// - 群组解散：群主解散群组
// - 状态恢复：恢复群组正常状态
//
// 参数：
// - ctx: 上下文
// - groupID: 群组ID
// - status: 新的状态值
//
// 返回：
// - error: 操作错误
func (g *GroupMgo) UpdateStatus(ctx context.Context, groupID string, status int32) (err error) {
	return g.UpdateMap(ctx, groupID, map[string]any{"status": status})
}

// UpdateMap 灵活更新群组信息
//
// 此方法提供灵活的群组信息更新功能，支持更新任意字段。
// 只更新传入的字段，未传入的字段保持不变。
//
// 更新特点：
// - 灵活性：支持更新任意字段组合
// - 安全性：空数据检查，避免无效操作
// - 原子性：单文档更新的原子性保证
//
// 使用场景：
// - 群组信息修改：群组名称、头像、公告等
// - 设置更新：群组各种设置的修改
// - 批量字段更新：一次性更新多个字段
//
// 参数：
// - ctx: 上下文
// - groupID: 群组ID
// - args: 要更新的字段映射
//
// 返回：
// - error: 操作错误
func (g *GroupMgo) UpdateMap(ctx context.Context, groupID string, args map[string]any) (err error) {
	// 空数据检查，避免无效的数据库操作
	if len(args) == 0 {
		return nil
	}
	return mongoutil.UpdateOne(ctx, g.coll,
		bson.M{"group_id": groupID},
		bson.M{"$set": args},
		true)
}

// Find 批量查询群组信息
//
// 此方法用于批量获取指定群组ID列表的群组信息。
// 支持一次性查询多个群组，提高查询效率。
//
// 查询特点：
// - 批量查询：一次性获取多个群组信息
// - 精确匹配：基于群组ID的精确查询
// - 性能优化：使用索引进行快速查询
//
// 使用场景：
// - 群组信息展示：批量获取群组详细信息
// - 权限验证：验证多个群组的存在性
// - 数据聚合：为其他操作准备群组数据
//
// 参数：
// - ctx: 上下文
// - groupIDs: 群组ID列表
//
// 返回：
// - groups: 群组信息列表
// - error: 查询错误
func (g *GroupMgo) Find(ctx context.Context, groupIDs []string) (groups []*model.Group, err error) {
	return mongoutil.Find[*model.Group](ctx, g.coll,
		bson.M{"group_id": bson.M{"$in": groupIDs}})
}

// Take 查询单个群组信息
//
// 此方法用于精确查询指定群组ID的群组信息。
// 返回单条记录或不存在错误。
//
// 查询特点：
// - 精确查询：基于群组ID的唯一查询
// - 快速响应：使用唯一索引的高性能查询
// - 明确结果：返回单条记录或明确的错误
//
// 使用场景：
// - 群组详情：获取特定群组的详细信息
// - 存在性验证：检查群组是否存在
// - 权限验证：验证群组状态和权限
//
// 参数：
// - ctx: 上下文
// - groupID: 群组ID
//
// 返回：
// - group: 群组信息
// - error: 查询错误，记录不存在时返回 ErrRecordNotFound
func (g *GroupMgo) Take(ctx context.Context, groupID string) (group *model.Group, err error) {
	return mongoutil.FindOne[*model.Group](ctx, g.coll,
		bson.M{"group_id": groupID})
}

// Search 搜索群组
//
// 此方法提供基于群组名称的搜索功能，支持模糊匹配和分页。
// 自动过滤已解散的群组，只返回有效的群组。
//
// 搜索特性：
// - 模糊匹配：基于群组名称的正则表达式搜索
// - 状态过滤：自动过滤已解散的群组
// - 分页支持：处理大数据量的搜索结果
// - 时间排序：按创建时间倒序排列，新群组在前
//
// 使用场景：
// - 群组搜索：用户搜索感兴趣的群组
// - 管理功能：管理员搜索群组进行管理
// - 推荐系统：为用户推荐相关群组
//
// 参数：
// - ctx: 上下文
// - keyword: 搜索关键词（群组名称）
// - pagination: 分页参数
//
// 返回：
// - total: 匹配的总记录数
// - groups: 当前页的群组列表
// - error: 搜索错误
func (g *GroupMgo) Search(ctx context.Context, keyword string, pagination pagination.Pagination) (total int64, groups []*model.Group, err error) {
	// 定义排序选项：按创建时间倒序，新群组在前
	opts := options.Find().SetSort(bson.D{{Key: "create_time", Value: -1}})

	// 执行搜索：群组名称模糊匹配 + 状态过滤 + 分页 + 排序
	return mongoutil.FindPage[*model.Group](ctx, g.coll, bson.M{
		"group_name": bson.M{"$regex": keyword},                    // 群组名称模糊匹配
		"status":     bson.M{"$ne": constant.GroupStatusDismissed}, // 过滤已解散群组
	}, pagination, opts)
}

// CountTotal 统计群组总数
//
// 此方法用于统计系统中的群组总数，支持按时间范围统计。
// 可以统计全部群组或指定时间之前创建的群组。
//
// 统计模式：
// - 全量统计：before为nil时，统计所有群组
// - 时间范围统计：before不为nil时，统计指定时间之前的群组
//
// 使用场景：
// - 系统统计：显示系统群组总数
// - 增长分析：分析群组数量的增长趋势
// - 报表生成：生成系统运营报表
//
// 参数：
// - ctx: 上下文
// - before: 时间截止点，nil表示统计所有群组
//
// 返回：
// - count: 群组数量
// - error: 统计错误
func (g *GroupMgo) CountTotal(ctx context.Context, before *time.Time) (count int64, err error) {
	if before == nil {
		// 统计所有群组
		return mongoutil.Count(ctx, g.coll, bson.M{})
	}
	// 统计指定时间之前创建的群组
	return mongoutil.Count(ctx, g.coll,
		bson.M{"create_time": bson.M{"$lt": before}})
}

// CountRangeEverydayTotal 按日期统计群组创建数量
//
// 此方法使用MongoDB聚合管道，按日期分组统计指定时间范围内每天的群组创建数量。
// 返回日期到创建数量的映射，用于生成时间序列图表。
//
// 聚合流程：
// 1. $match: 过滤指定时间范围内的群组
// 2. $group: 按日期分组，统计每天的创建数量
// 3. 结果处理：转换为日期字符串到数量的映射
//
// 使用场景：
// - 数据分析：分析群组创建的时间分布
// - 图表展示：生成群组创建趋势图
// - 运营分析：了解用户活跃度和增长趋势
//
// 参数：
// - ctx: 上下文
// - start: 开始时间
// - end: 结束时间
//
// 返回：
// - map[string]int64: 日期字符串到创建数量的映射
// - error: 统计错误
func (g *GroupMgo) CountRangeEverydayTotal(ctx context.Context, start time.Time, end time.Time) (map[string]int64, error) {
	// 构建聚合管道
	pipeline := bson.A{
		// 第一阶段：过滤时间范围
		bson.M{
			"$match": bson.M{
				"create_time": bson.M{
					"$gte": start, // 大于等于开始时间
					"$lt":  end,   // 小于结束时间
				},
			},
		},
		// 第二阶段：按日期分组并计数
		bson.M{
			"$group": bson.M{
				"_id": bson.M{
					"$dateToString": bson.M{
						"format": "%Y-%m-%d",     // 日期格式：YYYY-MM-DD
						"date":   "$create_time", // 使用创建时间字段
					},
				},
				"count": bson.M{
					"$sum": 1, // 计数：每个文档计为1
				},
			},
		},
	}

	// 定义结果结构体
	type Item struct {
		Date  string `bson:"_id"`   // 日期字符串
		Count int64  `bson:"count"` // 创建数量
	}

	// 执行聚合查询
	items, err := mongoutil.Aggregate[Item](ctx, g.coll, pipeline)
	if err != nil {
		return nil, err
	}

	// 转换结果为映射格式
	res := make(map[string]int64, len(items))
	for _, item := range items {
		res[item.Date] = item.Count
	}
	return res, nil
}

// FindJoinSortGroupID 查询并排序群组ID列表
//
// 此方法用于获取指定群组ID列表的排序结果，按照默认排序规则排序。
// 自动过滤已解散的群组，只返回有效群组的ID。
//
// 排序优化：
// - 少量群组：直接返回原列表，避免不必要的数据库查询
// - 多个群组：查询数据库并按排序规则排序
// - 状态过滤：自动过滤已解散的群组
//
// 使用场景：
// - 群组列表排序：为用户展示排序后的群组列表
// - 推荐排序：按特定规则排序推荐的群组
// - 界面展示：确保群组显示的一致性
//
// 参数：
// - ctx: 上下文
// - groupIDs: 原始群组ID列表
//
// 返回：
// - []string: 排序后的群组ID列表
// - error: 查询错误
func (g *GroupMgo) FindJoinSortGroupID(ctx context.Context, groupIDs []string) ([]string, error) {
	// 优化：少量群组直接返回，避免不必要的数据库查询
	if len(groupIDs) < 2 {
		return groupIDs, nil
	}

	// 构建查询条件：指定群组ID + 过滤已解散群组
	filter := bson.M{
		"group_id": bson.M{"$in": groupIDs},                      // 指定群组ID列表
		"status":   bson.M{"$ne": constant.GroupStatusDismissed}, // 过滤已解散群组
	}

	// 设置查询选项：排序 + 投影（只返回group_id字段）
	opt := options.Find().
		SetSort(g.sortGroup()).                        // 应用默认排序
		SetProjection(bson.M{"_id": 0, "group_id": 1}) // 只返回group_id字段

	return mongoutil.Find[string](ctx, g.coll, filter, opt)
}

// SearchJoin 在指定群组中搜索
//
// 此方法在指定的群组ID列表中进行搜索，支持关键词过滤和分页。
// 主要用于在用户已加入的群组中进行搜索。
//
// 搜索特点：
// - 范围限制：只在指定的群组列表中搜索
// - 关键词过滤：支持群组名称的模糊匹配
// - 状态过滤：自动过滤已解散的群组
// - 分页支持：处理大数据量的搜索结果
//
// 使用场景：
// - 用户群组搜索：在用户已加入的群组中搜索
// - 管理员搜索：在管理的群组中搜索
// - 权限范围搜索：在有权限的群组范围内搜索
//
// 参数：
// - ctx: 上下文
// - groupIDs: 搜索范围的群组ID列表
// - keyword: 搜索关键词，为空表示不过滤
// - pagination: 分页参数
//
// 返回：
// - int64: 匹配的总记录数
// - []*model.Group: 当前页的群组列表
// - error: 搜索错误
func (g *GroupMgo) SearchJoin(ctx context.Context, groupIDs []string, keyword string, pagination pagination.Pagination) (int64, []*model.Group, error) {
	// 空列表检查：没有群组ID时直接返回空结果
	if len(groupIDs) == 0 {
		return 0, nil, nil
	}

	// 构建基础查询条件：指定群组ID + 状态过滤
	filter := bson.M{
		"group_id": bson.M{"$in": groupIDs},                      // 限制在指定群组范围内
		"status":   bson.M{"$ne": constant.GroupStatusDismissed}, // 过滤已解散群组
	}

	// 添加关键词过滤条件
	if keyword != "" {
		filter["group_name"] = bson.M{"$regex": keyword} // 群组名称模糊匹配
	}

	// 定义排序选项
	opts := options.Find().SetSort(g.sortGroup())

	// 执行分页搜索
	return mongoutil.FindPage[*model.Group](ctx, g.coll, filter, pagination, opts)
}
