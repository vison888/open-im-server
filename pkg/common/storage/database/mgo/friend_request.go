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

// Package mgo 提供基于MongoDB的数据存储实现
//
// 本文件实现好友申请(FriendRequest)的MongoDB数据库操作
//
// **功能概述：**
// 好友申请管理是社交应用的核心功能之一，提供用户之间建立好友关系的桥梁。
// 本模块负责处理好友申请的全生命周期管理，包括：
// - 申请创建与发送
// - 申请状态查询与更新
// - 申请处理（同意/拒绝）
// - 申请统计与分析
//
// **数据模型：**
// FriendRequest 包含以下核心字段：
// - FromUserID: 申请发起者ID
// - ToUserID: 申请接收者ID
// - HandleResult: 处理结果（0-待处理, 1-已同意, -1-已拒绝）
// - ReqMsg: 申请消息
// - CreateTime: 创建时间
// - HandleTime: 处理时间
//
// **索引策略：**
// 1. 联合唯一索引：(from_user_id, to_user_id) - 防止重复申请
// 2. 时间排序索引：create_time - 支持按时间排序查询
//
// **业务规则：**
// - 同一对用户之间只能有一个有效的好友申请
// - 申请处理后状态不可逆转
// - 支持双向申请查询（我发出的/收到的）
package mgo

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/openimsdk/tools/db/mongoutil"
	"github.com/openimsdk/tools/db/pagination"
)

// NewFriendRequestMongo 创建基于MongoDB的好友申请数据库实例
//
// **功能说明：**
// 初始化MongoDB集合并创建必要的索引，确保数据查询性能和完整性约束
//
// **索引创建：**
// 1. 联合唯一索引：(from_user_id, to_user_id)
//   - 目的：防止同一对用户之间重复申请
//   - 约束：确保数据唯一性
//
// 2. 时间排序索引：create_time（降序）
//   - 目的：支持按时间排序的高效查询
//   - 优化：提升分页查询性能
//
// **参数：**
// - db: MongoDB数据库实例
//
// **返回值：**
// - database.FriendRequest: 好友申请数据库操作接口
// - error: 索引创建失败时的错误信息
//
// **使用示例：**
// ```go
// friendRequestDB, err := NewFriendRequestMongo(mongoDB)
//
//	if err != nil {
//	    log.Fatal("Failed to create friend request database:", err)
//	}
//
// ```
func NewFriendRequestMongo(db *mongo.Database) (database.FriendRequest, error) {
	coll := db.Collection(database.FriendRequestName)
	_, err := coll.Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "from_user_id", Value: 1},
				{Key: "to_user_id", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "create_time", Value: -1},
			},
		},
	})
	if err != nil {
		return nil, err
	}
	return &FriendRequestMgo{coll: coll}, nil
}

// FriendRequestMgo 基于MongoDB的好友申请数据库实现
//
// **设计特点：**
// 1. 封装MongoDB集合操作
// 2. 统一的排序策略（按创建时间倒序）
// 3. 支持复杂查询条件（处理状态筛选）
// 4. 优化的分页查询实现
//
// **性能优化：**
// - 使用索引优化查询性能
// - 批量操作减少网络开销
// - 合理的查询条件组合
//
// **成员说明：**
// - coll: MongoDB集合引用，用于执行数据库操作
type FriendRequestMgo struct {
	coll *mongo.Collection // MongoDB集合引用
}

// sort 返回默认的排序规则
//
// **功能说明：**
// 定义好友申请的默认排序策略，按创建时间降序排列，确保最新的申请显示在前面
//
// **排序策略：**
// - 字段：create_time
// - 顺序：降序（-1）
// - 目的：最新申请优先显示
//
// **返回值：**
// - any: MongoDB排序文档，格式为 bson.D{{"create_time", -1}}
func (f *FriendRequestMgo) sort() any {
	return bson.D{{Key: "create_time", Value: -1}}
}

// FindToUserID 查询指定用户收到的好友申请
//
// **功能说明：**
// 分页查询特定用户收到的好友申请，支持按处理状态筛选，是好友申请列表的核心查询方法
//
// **查询条件：**
// - 基础条件：to_user_id = toUserID（申请接收者）
// - 可选条件：handle_result in handleResults（处理状态筛选）
//
// **处理状态说明：**
// - 0: 待处理
// - 1: 已同意
// - -1: 已拒绝
//
// **分页策略：**
// - 使用统一的分页工具
// - 按创建时间降序排序
// - 支持大数据量的高效分页
//
// **参数：**
// - ctx: 上下文对象，用于超时控制和取消操作
// - toUserID: 申请接收者用户ID
// - handleResults: 处理状态筛选数组，空数组表示不筛选
// - pagination: 分页参数（页码、每页数量）
//
// **返回值：**
// - total: 符合条件的总记录数
// - friendRequests: 当前页的好友申请列表
// - err: 查询过程中的错误信息
//
// **使用场景：**
// - 用户查看收到的好友申请列表
// - 管理员查看待处理的申请
// - 统计分析相关功能
//
// **性能考虑：**
// - 使用索引优化查询性能
// - 分页查询避免内存溢出
// - 合理的查询条件减少数据扫描
func (f *FriendRequestMgo) FindToUserID(ctx context.Context, toUserID string, handleResults []int, pagination pagination.Pagination) (total int64, friendRequests []*model.FriendRequest, err error) {
	filter := bson.M{"to_user_id": toUserID}
	if len(handleResults) > 0 {
		filter["handle_result"] = bson.M{"$in": handleResults}
	}
	return mongoutil.FindPage[*model.FriendRequest](ctx, f.coll, filter, pagination, options.Find().SetSort(f.sort()))
}

// FindFromUserID 查询指定用户发出的好友申请
//
// **功能说明：**
// 分页查询特定用户主动发出的好友申请，支持按处理状态筛选，用于查看申请的处理进度
//
// **查询条件：**
// - 基础条件：from_user_id = fromUserID（申请发起者）
// - 可选条件：handle_result in handleResults（处理状态筛选）
//
// **业务价值：**
// - 用户可以查看自己发出的申请状态
// - 了解申请的处理进度
// - 支持申请管理功能
//
// **参数：**
// - ctx: 上下文对象
// - fromUserID: 申请发起者用户ID
// - handleResults: 处理状态筛选数组
// - pagination: 分页参数
//
// **返回值：**
// - total: 符合条件的总记录数
// - friendRequests: 当前页的好友申请列表
// - err: 查询错误信息
//
// **使用场景：**
// - 用户查看发出的好友申请
// - 申请状态跟踪
// - 重复申请检查
func (f *FriendRequestMgo) FindFromUserID(ctx context.Context, fromUserID string, handleResults []int, pagination pagination.Pagination) (total int64, friendRequests []*model.FriendRequest, err error) {
	filter := bson.M{"from_user_id": fromUserID}
	if len(handleResults) > 0 {
		filter["handle_result"] = bson.M{"$in": handleResults}
	}
	return mongoutil.FindPage[*model.FriendRequest](ctx, f.coll, filter, pagination, options.Find().SetSort(f.sort()))
}

// FindBothFriendRequests 查询两个用户之间的所有好友申请
//
// **功能说明：**
// 查询两个用户之间的双向好友申请记录，包括A->B和B->A的申请，用于关系状态检查和历史记录查询
//
// **查询逻辑：**
// 使用OR条件查询两个方向的申请：
// - 条件1：from_user_id = fromUserID AND to_user_id = toUserID
// - 条件2：from_user_id = toUserID AND to_user_id = fromUserID
//
// **业务应用：**
// - 检查用户间的申请历史
// - 防止重复申请处理
// - 关系状态分析
// - 双向申请合并显示
//
// **参数：**
// - ctx: 上下文对象
// - fromUserID: 第一个用户ID
// - toUserID: 第二个用户ID
//
// **返回值：**
// - friends: 两用户间的所有好友申请记录
// - err: 查询错误信息
//
// **数据特点：**
// - 最多返回2条记录（双向申请）
// - 通常用于关系状态检查
// - 支持复杂的业务逻辑判断
func (f *FriendRequestMgo) FindBothFriendRequests(ctx context.Context, fromUserID, toUserID string) (friends []*model.FriendRequest, err error) {
	filter := bson.M{"$or": []bson.M{
		{"from_user_id": fromUserID, "to_user_id": toUserID},
		{"from_user_id": toUserID, "to_user_id": fromUserID},
	}}
	return mongoutil.Find[*model.FriendRequest](ctx, f.coll, filter)
}

// Create 批量创建好友申请记录
//
// **功能说明：**
// 批量插入好友申请记录到数据库，支持一次性创建多个申请，提高操作效率
//
// **批量操作优势：**
// - 减少数据库连接开销
// - 提高插入性能
// - 保证批量操作的原子性
// - 统一的错误处理
//
// **数据验证：**
// - 依赖数据库层面的唯一性约束
// - 自动处理重复申请的错误
// - 保证数据完整性
//
// **参数：**
// - ctx: 上下文对象
// - friendRequests: 要创建的好友申请记录数组
//
// **返回值：**
// - error: 创建过程中的错误信息
//
// **错误处理：**
// - 重复申请错误（违反唯一性约束）
// - 数据库连接错误
// - 数据格式错误
//
// **使用场景：**
// - 批量导入申请数据
// - 系统初始化
// - 数据迁移操作
//
// **注意事项：**
// - 大批量操作需要考虑性能影响
// - 建议单次操作控制在合理范围内
// - 需要适当的错误处理和重试机制
func (f *FriendRequestMgo) Create(ctx context.Context, friendRequests []*model.FriendRequest) error {
	return mongoutil.InsertMany(ctx, f.coll, friendRequests)
}

// Delete 删除特定的好友申请记录
//
// **功能说明：**
// 删除指定用户对之间的好友申请记录，通常用于申请撤销或清理过期申请
//
// **删除条件：**
// - from_user_id = fromUserID
// - to_user_id = toUserID
// - 精确匹配，只删除指定方向的申请
//
// **业务场景：**
// - 用户撤销已发送的好友申请
// - 清理过期或无效的申请
// - 申请处理后的清理工作
// - 系统维护和数据清理
//
// **参数：**
// - ctx: 上下文对象
// - fromUserID: 申请发起者用户ID
// - toUserID: 申请接收者用户ID
//
// **返回值：**
// - err: 删除操作的错误信息
//
// **安全性：**
// - 精确匹配删除条件
// - 避免误删其他用户的申请
// - 支持事务操作
//
// **性能考虑：**
// - 使用复合索引优化删除性能
// - 单条记录删除效率高
// - 不涉及全表扫描
func (f *FriendRequestMgo) Delete(ctx context.Context, fromUserID, toUserID string) (err error) {
	return mongoutil.DeleteOne(ctx, f.coll, bson.M{"from_user_id": fromUserID, "to_user_id": toUserID})
}

// UpdateByMap 通过Map更新好友申请信息
//
// **功能说明：**
// 使用键值对的方式灵活更新好友申请的指定字段，支持部分字段更新，提高更新效率
//
// **更新策略：**
// - 仅更新Map中指定的字段
// - 保持其他字段不变
// - 支持upsert操作（不存在则创建）
// - 原子性更新操作
//
// **常用更新字段：**
// - handle_result: 处理结果
// - handle_msg: 处理消息
// - handler_user_id: 处理人ID
// - handle_time: 处理时间
// - ex: 扩展字段
//
// **参数：**
// - ctx: 上下文对象
// - formUserID: 申请发起者用户ID（注意：这里的拼写是form，可能是历史原因）
// - toUserID: 申请接收者用户ID
// - args: 要更新的字段映射表
//
// **返回值：**
// - err: 更新操作的错误信息
//
// **优化特性：**
// - 空Map检查，避免无效操作
// - 批量字段更新
// - 条件匹配更新
func (f *FriendRequestMgo) UpdateByMap(ctx context.Context, formUserID, toUserID string, args map[string]any) (err error) {
	if len(args) == 0 {
		return nil
	}
	return mongoutil.UpdateOne(ctx, f.coll, bson.M{"from_user_id": formUserID, "to_user_id": toUserID}, bson.M{"$set": args}, true)
}

func (f *FriendRequestMgo) Update(ctx context.Context, friendRequest *model.FriendRequest) (err error) {
	updater := bson.M{}
	if friendRequest.HandleResult != 0 {
		updater["handle_result"] = friendRequest.HandleResult
	}
	if friendRequest.ReqMsg != "" {
		updater["req_msg"] = friendRequest.ReqMsg
	}
	if friendRequest.HandlerUserID != "" {
		updater["handler_user_id"] = friendRequest.HandlerUserID
	}
	if friendRequest.HandleMsg != "" {
		updater["handle_msg"] = friendRequest.HandleMsg
	}
	if !friendRequest.HandleTime.IsZero() {
		updater["handle_time"] = friendRequest.HandleTime
	}
	if friendRequest.Ex != "" {
		updater["ex"] = friendRequest.Ex
	}
	if len(updater) == 0 {
		return nil
	}
	filter := bson.M{"from_user_id": friendRequest.FromUserID, "to_user_id": friendRequest.ToUserID}
	return mongoutil.UpdateOne(ctx, f.coll, filter, bson.M{"$set": updater}, true)
}

func (f *FriendRequestMgo) Find(ctx context.Context, fromUserID, toUserID string) (friendRequest *model.FriendRequest, err error) {
	return mongoutil.FindOne[*model.FriendRequest](ctx, f.coll, bson.M{"from_user_id": fromUserID, "to_user_id": toUserID})
}

func (f *FriendRequestMgo) Take(ctx context.Context, fromUserID, toUserID string) (friendRequest *model.FriendRequest, err error) {
	return f.Find(ctx, fromUserID, toUserID)
}

// GetUnhandledCount 获取未处理的好友申请数量
//
// **功能说明：**
// 统计指定用户收到的未处理好友申请数量，支持时间范围筛选，用于通知提醒和统计分析
//
// **查询条件：**
// - to_user_id = userID（申请接收者）
// - handle_result = 0（未处理状态）
// - create_time > ts（可选，时间戳筛选）
//
// **参数说明：**
// - ctx: 上下文对象
// - userID: 用户ID，统计该用户收到的申请
// - ts: 时间戳筛选，0表示不限制时间，大于0表示统计该时间之后的申请
//
// **返回值：**
// - int64: 未处理申请的数量
// - error: 统计过程中的错误信息
//
// **业务应用：**
// - 消息通知提醒（红点提示）
// - 用户界面显示未读数量
// - 数据统计和分析
// - 定期清理提醒
//
// **性能优化：**
// - 使用索引加速计数查询
// - 条件筛选减少扫描范围
// - 避免大数据量统计的性能问题
//
// **使用示例：**
// ```go
// // 获取所有未处理申请数量
// count1, _ := db.GetUnhandledCount(ctx, "user123", 0)
//
// // 获取最近24小时的未处理申请数量
// yesterday := time.Now().AddDate(0, 0, -1).Unix()
// count2, _ := db.GetUnhandledCount(ctx, "user123", yesterday)
// ```
func (f *FriendRequestMgo) GetUnhandledCount(ctx context.Context, userID string, ts int64) (int64, error) {
	filter := bson.M{"to_user_id": userID, "handle_result": 0}
	if ts != 0 {
		filter["create_time"] = bson.M{"$gt": time.Unix(ts, 0)}
	}
	return mongoutil.Count(ctx, f.coll, filter)
}
