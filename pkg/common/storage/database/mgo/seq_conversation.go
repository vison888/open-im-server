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

// Package mgo 会话序列号管理MongoDB实现
// seq_conversation.go 实现会话序列号的原子分配和管理
//
// 核心功能：
// 1. 序列号原子分配：使用MongoDB的FindOneAndUpdate实现原子操作
// 2. 会话序列号管理：维护每个会话的最大和最小序列号
// 3. 批量序列号分配：支持一次性分配多个序列号
// 4. 序列号查询：提供高效的序列号查询接口
//
// 设计原理：
// - 利用MongoDB的原子操作特性确保序列号分配的唯一性
// - 使用$inc操作符实现高并发下的序列号递增
// - 通过Upsert机制自动创建不存在的会话记录
// - 支持分布式环境下的序列号一致性
package mgo

import (
	"context"
	"errors"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/tools/db/mongoutil"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// NewSeqConversationMongo 创建会话序列号MongoDB实现
//
// 功能说明：
// 1. 初始化MongoDB集合连接
// 2. 创建conversation_id索引以优化查询性能
// 3. 返回序列号管理接口实现
//
// 索引设计：
// - conversation_id: 单字段索引，支持高效的会话查询
// - 索引类型：升序索引，适合范围查询和排序
//
// 参数：
//   - db: MongoDB数据库连接
//
// 返回值：
//   - database.SeqConversation: 序列号管理接口
//   - error: 索引创建失败时的错误
func NewSeqConversationMongo(db *mongo.Database) (database.SeqConversation, error) {
	coll := db.Collection(database.SeqConversationName)

	// 创建conversation_id索引，提升查询性能
	// 这个索引对于高并发的序列号分配操作至关重要
	_, err := coll.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{
			{Key: "conversation_id", Value: 1}, // 升序索引
		},
	})
	if err != nil {
		return nil, err
	}
	return &seqConversationMongo{coll: coll}, nil
}

// seqConversationMongo 会话序列号MongoDB实现结构体
//
// 数据模型：
//
//	{
//	  "_id": ObjectId,
//	  "conversation_id": "si_user1_user2",  // 会话ID
//	  "min_seq": 1,                         // 最小序列号
//	  "max_seq": 1000                       // 最大序列号
//	}
//
// 设计特点：
// - 每个会话对应一个文档记录
// - min_seq和max_seq维护序列号范围
// - 支持原子操作和并发安全
type seqConversationMongo struct {
	coll *mongo.Collection // MongoDB集合连接
}

// setSeq 设置会话的序列号（通用方法）
//
// 核心机制：使用Upsert操作实现"存在则更新，不存在则创建"
//
// Upsert工作流程：
// 1. 根据filter查找匹配的文档
// 2. 如果找到：执行$set更新指定字段
// 3. 如果未找到：使用$setOnInsert创建新文档
//
// 参数：
//   - ctx: 上下文
//   - conversationID: 会话ID
//   - seq: 要设置的序列号值
//   - field: 要更新的字段名（"min_seq" 或 "max_seq"）
//
// 返回值：
//   - error: 操作失败时的错误
func (s *seqConversationMongo) setSeq(ctx context.Context, conversationID string, seq int64, field string) error {
	// 查询条件：根据会话ID查找
	filter := map[string]any{
		"conversation_id": conversationID,
	}

	// 插入模板：当文档不存在时创建的默认值
	insert := bson.M{
		"conversation_id": conversationID,
		"min_seq":         0, // 默认最小序列号
		"max_seq":         0, // 默认最大序列号
	}
	// 移除要更新的字段，避免$setOnInsert覆盖$set的值
	delete(insert, field)

	// 更新操作定义
	update := map[string]any{
		"$set": bson.M{
			field: seq, // 设置指定字段的值
		},
		"$setOnInsert": insert, // 仅在插入时设置的字段
	}

	// 启用Upsert选项：不存在时自动创建
	opt := options.Update().SetUpsert(true)

	return mongoutil.UpdateOne(ctx, s.coll, filter, update, false, opt)
}

// Malloc 原子分配序列号（核心方法）
//
// 🔥 FindOneAndUpdate 详细工作机制：
//
// 1. 原子性保证：
//
//   - 整个"查找-更新-返回"操作在MongoDB内部是原子的
//
//   - 即使高并发情况下也不会出现序列号重复分配
//
//   - 利用MongoDB文档级锁确保操作的一致性
//
//     2. Upsert机制（数据不存在时自动创建）：
//     ┌─────────────────────────────────────────────────────────┐
//     │                  FindOneAndUpdate流程                    │
//     ├─────────────────────────────────────────────────────────┤
//     │ Step 1: 根据filter查找文档                               │
//     │         filter: {"conversation_id": "si_user1_user2"}   │
//     ├─────────────────────────────────────────────────────────┤
//     │ Step 2: 判断文档是否存在                                 │
//     │   ├─ 存在: 执行$inc操作，max_seq += size                │
//     │   └─ 不存在: 创建新文档，初始max_seq = size             │
//     ├─────────────────────────────────────────────────────────┤
//     │ Step 3: 返回更新后的值                                   │
//     │         ReturnDocument(After) 返回更新后的max_seq       │
//     └─────────────────────────────────────────────────────────┘
//
// 3. 并发安全性：
//   - 多个客户端同时请求序列号时，MongoDB保证每次分配的序列号都是唯一的
//   - 使用文档级写锁，确保同一会话的序列号分配是串行的
//   - 不同会话的序列号分配可以并行执行
//
// 4. 返回值计算：
//   - 返回的是本次分配的起始序列号
//   - 计算公式：startSeq = 更新后的max_seq - 分配的size
//   - 例如：max_seq从100更新到110，size=10，则返回100（起始序列号）
//
// 参数：
//   - ctx: 上下文
//   - conversationID: 会话ID
//   - size: 要分配的序列号数量
//
// 返回值：
//   - int64: 本次分配的起始序列号
//   - error: 分配失败时的错误
//
// 使用示例：
//
//	startSeq, err := Malloc(ctx, "si_user1_user2", 10)
//	// 如果成功，startSeq=100，则本次分配的序列号范围是 [100, 109]
func (s *seqConversationMongo) Malloc(ctx context.Context, conversationID string, size int64) (int64, error) {
	// 参数验证
	if size < 0 {
		return 0, errors.New("size must be greater than 0")
	}

	// 特殊情况：size=0时仅查询当前最大序列号
	if size == 0 {
		return s.GetMaxSeq(ctx, conversationID)
	}

	// 查询条件：根据会话ID查找
	filter := map[string]any{"conversation_id": conversationID}

	// 更新操作：原子递增max_seq，确保min_seq为0
	update := map[string]any{
		"$inc": map[string]any{"max_seq": size},     // 原子递增操作
		"$set": map[string]any{"min_seq": int64(0)}, // 确保min_seq为0
	}

	// FindOneAndUpdate选项配置
	opt := options.FindOneAndUpdate().
		SetUpsert(true).                                      // 🔑 关键：启用Upsert，文档不存在时自动创建
		SetReturnDocument(options.After).                     // 🔑 关键：返回更新后的文档
		SetProjection(map[string]any{"_id": 0, "max_seq": 1}) // 仅返回max_seq字段，优化网络传输

	// 🔥 执行FindOneAndUpdate操作
	// 这里是整个序列号分配的核心：
	// 1. 如果conversationID对应的文档存在：
	//    - 执行$inc操作，将max_seq增加size
	//    - 返回更新后的max_seq值
	// 2. 如果conversationID对应的文档不存在：
	//    - MongoDB自动创建新文档：{"conversation_id": conversationID, "min_seq": 0, "max_seq": size}
	//    - 返回新创建文档的max_seq值（即size）
	lastSeq, err := mongoutil.FindOneAndUpdate[int64](ctx, s.coll, filter, update, opt)
	if err != nil {
		return 0, err
	}

	// 返回本次分配的起始序列号
	// 计算逻辑：更新后的max_seq - 本次分配的size = 起始序列号
	// 例如：原max_seq=100，分配size=10，更新后max_seq=110，返回110-10=100
	return lastSeq - size, nil
}

// SetMaxSeq 设置会话的最大序列号
//
// 应用场景：
// - 管理员手动调整序列号
// - 系统维护时重置序列号
// - 数据迁移时同步序列号
//
// 参数：
//   - ctx: 上下文
//   - conversationID: 会话ID
//   - seq: 要设置的最大序列号
//
// 返回值：
//   - error: 设置失败时的错误
func (s *seqConversationMongo) SetMaxSeq(ctx context.Context, conversationID string, seq int64) error {
	return s.setSeq(ctx, conversationID, seq, "max_seq")
}

// GetMaxSeq 获取会话的最大序列号
//
// 查询逻辑：
// 1. 根据conversation_id查找文档
// 2. 如果找到：返回max_seq字段值
// 3. 如果未找到：返回0（表示新会话）
//
// 性能优化：
// - 使用投影仅返回max_seq字段，减少网络传输
// - 利用conversation_id索引提升查询速度
//
// 参数：
//   - ctx: 上下文
//   - conversationID: 会话ID
//
// 返回值：
//   - int64: 最大序列号，新会话返回0
//   - error: 查询失败时的错误
func (s *seqConversationMongo) GetMaxSeq(ctx context.Context, conversationID string) (int64, error) {
	// 执行查询，仅返回max_seq字段
	seq, err := mongoutil.FindOne[int64](ctx, s.coll,
		bson.M{"conversation_id": conversationID},
		options.FindOne().SetProjection(map[string]any{"_id": 0, "max_seq": 1}))

	if err == nil {
		return seq, nil
	} else if IsNotFound(err) {
		// 文档不存在，返回0表示新会话
		return 0, nil
	} else {
		return 0, err
	}
}

// GetMinSeq 获取会话的最小序列号
//
// 功能说明：
// - 返回会话的最小有效序列号
// - 用于消息清理和历史消息查询的边界确定
// - 新会话默认返回0
//
// 参数：
//   - ctx: 上下文
//   - conversationID: 会话ID
//
// 返回值：
//   - int64: 最小序列号，新会话返回0
//   - error: 查询失败时的错误
func (s *seqConversationMongo) GetMinSeq(ctx context.Context, conversationID string) (int64, error) {
	seq, err := mongoutil.FindOne[int64](ctx, s.coll,
		bson.M{"conversation_id": conversationID},
		options.FindOne().SetProjection(map[string]any{"_id": 0, "min_seq": 1}))

	if err == nil {
		return seq, nil
	} else if IsNotFound(err) {
		return 0, nil
	} else {
		return 0, err
	}
}

// SetMinSeq 设置会话的最小序列号
//
// 应用场景：
// - 消息清理：设置最小序列号，标记之前的消息已清理
// - 历史消息管理：调整消息的有效范围
// - 存储优化：清理过期消息后更新边界
//
// 参数：
//   - ctx: 上下文
//   - conversationID: 会话ID
//   - seq: 要设置的最小序列号
//
// 返回值：
//   - error: 设置失败时的错误
func (s *seqConversationMongo) SetMinSeq(ctx context.Context, conversationID string, seq int64) error {
	return s.setSeq(ctx, conversationID, seq, "min_seq")
}

// GetConversation 获取完整的会话序列号信息
//
// 返回内容：
// - conversation_id: 会话ID
// - min_seq: 最小序列号
// - max_seq: 最大序列号
//
// 应用场景：
// - 会话状态检查
// - 序列号范围查询
// - 系统监控和调试
//
// 参数：
//   - ctx: 上下文
//   - conversationID: 会话ID
//
// 返回值：
//   - *model.SeqConversation: 完整的序列号信息
//   - error: 查询失败时的错误（包括文档不存在）
func (s *seqConversationMongo) GetConversation(ctx context.Context, conversationID string) (*model.SeqConversation, error) {
	return mongoutil.FindOne[*model.SeqConversation](ctx, s.coll, bson.M{"conversation_id": conversationID})
}
