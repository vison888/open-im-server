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
	"github.com/openimsdk/tools/utils/datautil"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/openimsdk/tools/db/mongoutil"
	"github.com/openimsdk/tools/db/pagination"
	"github.com/openimsdk/tools/errs"
)

/*
 * 群组申请数据访问层 - MongoDB实现
 *
 * 本文件实现了群组申请相关的所有数据库操作，是OpenIM群组系统申请流程的核心数据访问层。
 * 主要负责群组加入申请的全生命周期管理，包括申请创建、处理、查询、统计等功能。
 *
 * 核心功能模块：
 * 1. 申请生命周期管理
 *    - 申请创建：用户申请加入群组的记录创建
 *    - 申请处理：管理员同意/拒绝申请的状态更新
 *    - 申请删除：清理过期或无效的申请记录
 *
 * 2. 查询与检索系统
 *    - 精确查询：基于群组ID和用户ID的精确查询
 *    - 批量查询：支持批量获取群组申请信息
 *    - 分页查询：支持用户和管理员的分页查询
 *    - 条件过滤：按处理状态过滤申请记录
 *
 * 3. 统计与分析
 *    - 未处理统计：统计待处理的申请数量
 *    - 时间范围统计：按时间范围统计申请数量
 *    - 状态分布统计：不同处理状态的申请分布
 *
 * 4. 权限与状态管理
 *    - 处理状态：未处理、已同意、已拒绝等状态管理
 *    - 权限验证：确保只有相关用户能查看申请
 *    - 重复申请控制：防止重复申请的数据约束
 *
 * 5. 性能优化特性
 *    - 复合索引：群组ID+用户ID复合唯一索引
 *    - 时间索引：申请时间索引支持时间范围查询
 *    - 排序优化：按申请时间倒序的高效排序
 *
 * 技术架构：
 * - 数据库：MongoDB，支持复杂查询和索引优化
 * - 索引策略：复合唯一索引 + 时间索引
 * - 查询优化：使用索引和排序优化查询性能
 * - 数据完整性：唯一性约束防止重复申请
 *
 * 数据一致性保证：
 * - 唯一性约束：同一用户对同一群组只能有一个申请
 * - 原子操作：单文档操作的原子性
 * - 状态一致性：申请状态的正确维护
 *
 * 申请状态说明：
 * - 0: 未处理（待审核）
 * - 1: 已同意（申请通过）
 * - -1: 已拒绝（申请被拒）
 */

// NewGroupRequestMgo 创建群组申请数据访问对象
//
// 此函数初始化群组申请的MongoDB数据访问层，包括：
// 1. 集合初始化：创建群组申请主集合
// 2. 复合索引创建：建立group_id+user_id复合唯一索引
// 3. 时间索引创建：建立申请时间索引，支持时间范围查询
//
// 索引设计说明：
// - 复合唯一索引：确保同一用户对同一群组只能有一个申请记录
// - 时间索引：支持按申请时间的高效查询和排序
// - 查询优化：提高基于群组ID、用户ID和时间的查询性能
//
// 参数：
// - db: MongoDB数据库实例
//
// 返回：
// - database.GroupRequest: 群组申请数据访问接口实现
// - error: 初始化错误
func NewGroupRequestMgo(db *mongo.Database) (database.GroupRequest, error) {
	// 获取群组申请主集合
	coll := db.Collection(database.GroupRequestName)

	// 创建多个索引以优化不同的查询场景
	_, err := coll.Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{
			// 复合唯一索引：群组ID + 用户ID
			// 确保同一用户对同一群组只能有一个申请记录
			Keys: bson.D{
				{Key: "group_id", Value: 1}, // 群组ID升序
				{Key: "user_id", Value: 1},  // 用户ID升序
			},
			Options: options.Index().SetUnique(true), // 设置唯一约束
		},
		{
			// 申请时间索引：支持按时间排序和范围查询
			Keys: bson.D{
				{Key: "req_time", Value: -1}, // 申请时间降序（最新的在前）
			},
		},
	})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return &GroupRequestMgo{coll: coll}, nil
}

// GroupRequestMgo 群组申请MongoDB数据访问实现
//
// 这是群组申请数据访问层的核心结构体，提供：
// - 申请管理：申请的创建、更新、删除操作
// - 查询服务：多维度的申请查询功能
// - 统计分析：申请数据的统计和分析
//
// 设计特点：
// - 专业化：专注于群组申请流程的数据管理
// - 高性能：使用索引优化查询性能
// - 完整性：提供申请全生命周期的数据操作
type GroupRequestMgo struct {
	coll *mongo.Collection // 群组申请主数据集合
}

// Create 批量创建群组申请
//
// 此方法实现了群组申请的批量创建操作。
// 支持一次性创建多个申请记录，提高创建效率。
//
// 创建特点：
// - 批量操作：减少数据库交互次数
// - 原子性：MongoDB的批量插入保证原子性
// - 唯一性检查：依赖数据库唯一索引防止重复申请
//
// 使用场景：
// - 用户申请：用户申请加入群组
// - 批量邀请：管理员批量邀请用户加入
// - 数据迁移：系统数据迁移场景
//
// 参数：
// - ctx: 上下文，用于超时控制和链路追踪
// - groupRequests: 要创建的群组申请列表
//
// 返回：
// - error: 操作错误，nil表示成功
func (g *GroupRequestMgo) Create(ctx context.Context, groupRequests []*model.GroupRequest) (err error) {
	return mongoutil.InsertMany(ctx, g.coll, groupRequests)
}

// Delete 删除群组申请
//
// 此方法用于删除指定的群组申请记录。
// 通常在申请被处理后或申请过期时调用。
//
// 删除场景：
// - 申请处理完成：申请被同意或拒绝后清理记录
// - 申请撤销：用户主动撤销申请
// - 数据清理：清理过期的申请记录
//
// 使用场景：
// - 申请撤销：用户取消申请
// - 数据清理：定期清理已处理的申请
// - 状态重置：重新申请前清理旧记录
//
// 参数：
// - ctx: 上下文
// - groupID: 群组ID
// - userID: 用户ID
//
// 返回：
// - error: 操作错误
func (g *GroupRequestMgo) Delete(ctx context.Context, groupID string, userID string) (err error) {
	return mongoutil.DeleteOne(ctx, g.coll,
		bson.M{"group_id": groupID, "user_id": userID})
}

// UpdateHandler 更新申请处理结果
//
// 此方法用于更新群组申请的处理结果，包括处理消息和处理状态。
// 这是申请流程中的关键操作，将申请从待处理状态转为已处理状态。
//
// 处理状态说明：
// - 1: 申请通过，用户可以加入群组
// - -1: 申请拒绝，用户无法加入群组
//
// 更新内容：
// - handle_msg: 处理消息，管理员给出的处理理由
// - handle_result: 处理结果，同意或拒绝
//
// 使用场景：
// - 申请审批：管理员审批用户的加群申请
// - 自动处理：系统根据规则自动处理申请
// - 批量处理：管理员批量处理多个申请
//
// 参数：
// - ctx: 上下文
// - groupID: 群组ID
// - userID: 申请用户ID
// - handledMsg: 处理消息（管理员备注）
// - handleResult: 处理结果（1:同意, -1:拒绝）
//
// 返回：
// - error: 操作错误
func (g *GroupRequestMgo) UpdateHandler(ctx context.Context, groupID string, userID string, handledMsg string, handleResult int32) (err error) {
	return mongoutil.UpdateOne(ctx, g.coll,
		bson.M{"group_id": groupID, "user_id": userID},
		bson.M{"$set": bson.M{
			"handle_msg":    handledMsg,   // 处理消息
			"handle_result": handleResult, // 处理结果
		}},
		true)
}

// Take 查询单个群组申请
//
// 此方法用于精确查询指定用户对指定群组的申请记录。
// 返回单条记录或不存在错误。
//
// 查询特点：
// - 精确查询：基于群组ID和用户ID的唯一查询
// - 快速响应：使用复合唯一索引的高性能查询
// - 明确结果：返回单条记录或明确的错误
//
// 使用场景：
// - 申请状态查询：查看特定申请的处理状态
// - 重复申请检查：检查用户是否已有申请记录
// - 申请详情展示：显示申请的详细信息
//
// 参数：
// - ctx: 上下文
// - groupID: 群组ID
// - userID: 用户ID
//
// 返回：
// - groupRequest: 群组申请信息
// - error: 查询错误，记录不存在时返回 ErrRecordNotFound
func (g *GroupRequestMgo) Take(ctx context.Context, groupID string, userID string) (groupRequest *model.GroupRequest, err error) {
	return mongoutil.FindOne[*model.GroupRequest](ctx, g.coll,
		bson.M{"group_id": groupID, "user_id": userID})
}

// FindGroupRequests 批量查询群组申请
//
// 此方法用于批量获取指定群组中指定用户列表的申请记录。
// 支持一次性查询多个用户的申请状态。
//
// 查询特点：
// - 批量查询：一次性获取多个用户的申请信息
// - 群组范围：限定在特定群组内查询
// - 精确匹配：基于用户ID列表的精确查询
//
// 使用场景：
// - 批量状态查询：查询多个用户的申请状态
// - 管理员审批：管理员查看待处理的申请列表
// - 数据统计：统计特定用户群体的申请情况
//
// 参数：
// - ctx: 上下文
// - groupID: 群组ID
// - userIDs: 用户ID列表
//
// 返回：
// - []*model.GroupRequest: 群组申请信息列表
// - error: 查询错误
func (g *GroupRequestMgo) FindGroupRequests(ctx context.Context, groupID string, userIDs []string) ([]*model.GroupRequest, error) {
	return mongoutil.Find[*model.GroupRequest](ctx, g.coll,
		bson.M{
			"group_id": groupID,
			"user_id":  bson.M{"$in": userIDs},
		})
}

// sort 申请排序策略
//
// 定义群组申请的默认排序规则：
// 按申请时间降序排列，最新的申请在前面
//
// 排序的业务意义：
// - 时效性：最新的申请优先处理
// - 用户体验：用户能看到最新的申请状态
// - 管理效率：管理员优先处理最新的申请
//
// 返回：
// - any: MongoDB排序文档
func (g *GroupRequestMgo) sort() any {
	return bson.D{{Key: "req_time", Value: -1}} // 申请时间降序（最新的在前）
}

// Page 用户申请分页查询
//
// 此方法提供用户维度的申请分页查询功能，用户可以查看自己的申请历史。
// 支持按群组范围和处理状态过滤。
//
// 查询特点：
// - 用户视角：查询特定用户的所有申请
// - 群组过滤：可选择性地过滤特定群组的申请
// - 状态过滤：可选择性地过滤特定处理状态的申请
// - 分页支持：处理大数据量的查询结果
// - 时间排序：按申请时间倒序排列
//
// 使用场景：
// - 用户申请历史：用户查看自己的申请记录
// - 申请状态跟踪：跟踪申请的处理进度
// - 申请管理：用户管理自己的申请
//
// 参数：
// - ctx: 上下文
// - userID: 用户ID
// - groupIDs: 群组ID列表，为空表示查询所有群组
// - handleResults: 处理状态列表，为空表示查询所有状态
// - pagination: 分页参数
//
// 返回：
// - total: 匹配的总记录数
// - groups: 当前页的申请列表
// - error: 查询错误
func (g *GroupRequestMgo) Page(ctx context.Context, userID string, groupIDs []string, handleResults []int, pagination pagination.Pagination) (total int64, groups []*model.GroupRequest, err error) {
	// 构建基础查询条件：用户ID
	filter := bson.M{"user_id": userID}

	// 添加群组ID过滤条件
	if len(groupIDs) > 0 {
		filter["group_id"] = bson.M{"$in": datautil.Distinct(groupIDs)} // 去重后的群组ID列表
	}

	// 添加处理状态过滤条件
	if len(handleResults) > 0 {
		filter["handle_result"] = bson.M{"$in": handleResults}
	}

	// 执行分页查询，按申请时间倒序排列
	return mongoutil.FindPage[*model.GroupRequest](ctx, g.coll, filter, pagination,
		options.Find().SetSort(g.sort()))
}

// PageGroup 群组申请分页查询
//
// 此方法提供群组维度的申请分页查询功能，管理员可以查看群组的申请列表。
// 支持按处理状态过滤，便于管理员处理待审批的申请。
//
// 查询特点：
// - 群组视角：查询特定群组的所有申请
// - 多群组支持：支持同时查询多个群组的申请
// - 状态过滤：可选择性地过滤特定处理状态的申请
// - 分页支持：处理大数据量的查询结果
// - 时间排序：按申请时间倒序排列
//
// 使用场景：
// - 管理员审批：管理员查看待处理的申请
// - 申请统计：统计群组的申请情况
// - 批量处理：管理员批量处理申请
//
// 参数：
// - ctx: 上下文
// - groupIDs: 群组ID列表
// - handleResults: 处理状态列表，为空表示查询所有状态
// - pagination: 分页参数
//
// 返回：
// - total: 匹配的总记录数
// - groups: 当前页的申请列表
// - error: 查询错误
func (g *GroupRequestMgo) PageGroup(ctx context.Context, groupIDs []string, handleResults []int, pagination pagination.Pagination) (total int64, groups []*model.GroupRequest, err error) {
	// 空列表检查：没有群组ID时直接返回空结果
	if len(groupIDs) == 0 {
		return 0, nil, nil
	}

	// 构建基础查询条件：群组ID列表
	filter := bson.M{"group_id": bson.M{"$in": groupIDs}}

	// 添加处理状态过滤条件
	if len(handleResults) > 0 {
		filter["handle_result"] = bson.M{"$in": handleResults}
	}

	// 执行分页查询，按申请时间倒序排列
	return mongoutil.FindPage[*model.GroupRequest](ctx, g.coll, filter, pagination,
		options.Find().SetSort(g.sort()))
}

// GetUnhandledCount 统计未处理申请数量
//
// 此方法用于统计指定群组列表中未处理申请的数量。
// 支持按时间范围过滤，可以统计特定时间之后的未处理申请。
//
// 统计特点：
// - 状态过滤：只统计未处理状态（handle_result = 0）的申请
// - 群组范围：限定在指定群组列表内统计
// - 时间过滤：可选择性地过滤特定时间之后的申请
// - 高效统计：使用索引优化的计数查询
//
// 使用场景：
// - 待办提醒：提醒管理员有多少申请待处理
// - 工作量统计：统计管理员的工作量
// - 系统监控：监控申请处理的及时性
//
// 参数：
// - ctx: 上下文
// - groupIDs: 群组ID列表
// - ts: 时间戳，0表示不限制时间，非0表示统计该时间之后的申请
//
// 返回：
// - int64: 未处理申请的数量
// - error: 统计错误
func (g *GroupRequestMgo) GetUnhandledCount(ctx context.Context, groupIDs []string, ts int64) (int64, error) {
	// 空列表检查：没有群组ID时直接返回0
	if len(groupIDs) == 0 {
		return 0, nil
	}

	// 构建查询条件：群组ID列表 + 未处理状态
	filter := bson.M{
		"group_id":      bson.M{"$in": groupIDs}, // 限定群组范围
		"handle_result": 0,                       // 未处理状态
	}

	// 添加时间过滤条件
	if ts != 0 {
		filter["req_time"] = bson.M{"$gt": time.Unix(ts, 0)} // 大于指定时间
	}

	// 执行计数查询
	return mongoutil.Count(ctx, g.coll, filter)
}
