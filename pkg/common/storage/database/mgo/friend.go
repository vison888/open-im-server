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
// 本文件实现好友关系(Friend)的MongoDB数据库操作
//
// **功能概述：**
// 好友关系管理是社交应用的基础功能，负责维护用户之间的好友关系状态。
// 本模块提供完整的好友关系生命周期管理，包括：
// - 好友关系的建立与删除
// - 好友信息的查询与更新
// - 好友列表的分页浏览
// - 好友关系的双向维护
// - 好友数据的版本控制
//
// **数据模型设计：**
// Friend 数据模型包含以下核心字段：
// - OwnerUserID: 好友关系的拥有者ID
// - FriendUserID: 好友的用户ID
// - Remark: 好友备注名称
// - CreateTime: 建立好友关系的时间
// - IsPinned: 是否置顶好友
// - Ex: 扩展字段，支持自定义数据
//
// **关系特性：**
// 1. 双向关系：A添加B为好友，需要创建两条记录（A->B 和 B->A）
// 2. 独立备注：每个用户可以为同一个好友设置不同的备注
// 3. 个性化设置：支持置顶、扩展字段等个性化功能
// 4. 版本管理：支持增量同步，优化客户端数据更新
//
// **索引策略：**
// 1. 联合唯一索引：(owner_user_id, friend_user_id) - 防止重复好友关系
// 2. 版本控制索引：支持高效的增量同步查询
//
// **性能优化：**
// - 分页查询支持：避免大量好友数据的性能问题
// - 批量操作支持：提高多好友操作的效率
// - 缓存友好设计：配合Redis缓存提升查询性能
// - 版本控制支持：减少不必要的数据传输
package mgo

import (
	"context"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/openimsdk/tools/db/mongoutil"
	"github.com/openimsdk/tools/db/pagination"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// FriendMgo 基于MongoDB的好友关系数据库实现
//
// **设计理念：**
// 1. 关注分离：专门处理好友关系的数据操作
// 2. 版本控制：集成版本管理机制，支持增量同步
// 3. 性能优化：合理的索引设计和查询优化
// 4. 扩展性：支持复杂的好友关系查询需求
//
// **技术特点：**
// - MongoDB原生支持：充分利用MongoDB的文档特性
// - 事务支持：关键操作使用事务保证数据一致性
// - 批量操作：支持高效的批量好友操作
// - 时间处理：自动处理创建时间等时间字段
//
// **成员说明：**
// - coll: MongoDB好友关系集合的引用
// - owner: 版本日志管理器，用于增量同步功能
type FriendMgo struct {
	coll  *mongo.Collection   // MongoDB好友关系集合引用
	owner database.VersionLog // 版本日志管理器，支持增量同步
}

// NewFriendMongo 创建基于MongoDB的好友关系数据库实例
//
// **功能说明：**
// 初始化MongoDB集合，创建必要的索引，并配置版本控制功能，
// 返回完整的好友关系数据库操作实例
//
// **初始化流程：**
// 1. 获取好友关系集合引用
// 2. 创建联合唯一索引：(owner_user_id, friend_user_id)
// 3. 初始化版本日志管理器
// 4. 返回完整的数据库实例
//
// **索引设计：**
// - 联合唯一索引：确保同一对用户只能有一个好友关系记录
// - 覆盖查询：优化常见的好友查询操作
// - 排序支持：支持按置顶状态和ID排序
//
// **参数：**
// - db: MongoDB数据库实例
//
// **返回值：**
// - database.Friend: 好友关系数据库操作接口
// - error: 初始化过程中的错误信息
//
// **错误处理：**
// - 索引创建失败
// - 版本日志初始化失败
// - 集合访问权限问题
//
// **使用示例：**
// ```go
// friendDB, err := NewFriendMongo(mongoDB)
//
//	if err != nil {
//	    log.Fatal("Failed to create friend database:", err)
//	}
//
// ```
func NewFriendMongo(db *mongo.Database) (database.Friend, error) {
	coll := db.Collection(database.FriendName)
	_, err := coll.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{
			{Key: "owner_user_id", Value: 1},
			{Key: "friend_user_id", Value: 1},
		},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return nil, err
	}
	owner, err := NewVersionLog(db.Collection(database.FriendVersionName))
	if err != nil {
		return nil, err
	}
	return &FriendMgo{coll: coll, owner: owner}, nil
}

// friendSort 返回好友列表的默认排序规则
//
// **功能说明：**
// 定义好友列表的排序策略，确保置顶好友优先显示，
// 同时保持稳定的排序顺序
//
// **排序策略：**
// 1. 首要排序：is_pinned（置顶状态）降序 - 置顶好友优先
// 2. 次要排序：_id 升序 - 保证相同置顶状态下的稳定排序
//
// **设计考虑：**
// - 用户体验：置顶好友始终显示在前面
// - 性能优化：使用索引字段进行排序
// - 稳定性：确保排序结果的一致性
//
// **返回值：**
// - any: MongoDB排序文档 bson.D{{"is_pinned", -1}, {"_id", 1}}
func (f *FriendMgo) friendSort() any {
	return bson.D{{"is_pinned", -1}, {"_id", 1}}
}

// Create 批量创建好友关系记录
//
// **功能说明：**
// 批量插入好友关系记录，自动处理时间字段和版本控制，
// 确保数据一致性和版本同步功能
//
// **数据处理：**
// 1. ID生成：为没有ID的记录自动生成ObjectID
// 2. 时间处理：为没有创建时间的记录设置当前时间
// 3. 版本控制：更新相关用户的版本信息，支持增量同步
// 4. 批量操作：一次性插入多条记录，提高效率
//
// **版本管理：**
// - 按OwnerUserID分组处理版本更新
// - 为每个用户记录其新增的好友列表
// - 支持客户端增量同步数据变更
//
// **参数：**
// - ctx: 上下文对象，支持超时和取消操作
// - friends: 要创建的好友关系记录数组
//
// **返回值：**
// - error: 创建过程中的错误信息
//
// **错误处理：**
// - 重复好友关系（违反唯一性约束）
// - 版本更新失败
// - 数据库写入异常
//
// **事务保证：**
// 使用mongoutil.IncrVersion确保数据插入和版本更新的原子性
//
// **使用场景：**
// - 用户添加好友后创建双向关系
// - 批量导入好友数据
// - 系统初始化好友关系
//
// **性能考虑：**
// - 批量插入减少数据库往返
// - 按用户分组优化版本更新
// - 合理的错误处理避免部分失败
func (f *FriendMgo) Create(ctx context.Context, friends []*model.Friend) error {
	for i, friend := range friends {
		if friend.ID.IsZero() {
			friends[i].ID = primitive.NewObjectID()
		}
		if friend.CreateTime.IsZero() {
			friends[i].CreateTime = time.Now()
		}
	}
	return mongoutil.IncrVersion(func() error {
		return mongoutil.InsertMany(ctx, f.coll, friends)
	}, func() error {
		mp := make(map[string][]string)
		for _, friend := range friends {
			mp[friend.OwnerUserID] = append(mp[friend.OwnerUserID], friend.FriendUserID)
		}
		for ownerUserID, friendUserIDs := range mp {
			if err := f.owner.IncrVersion(ctx, ownerUserID, friendUserIDs, model.VersionStateInsert); err != nil {
				return err
			}
		}
		return nil
	})
}

// Delete 删除指定用户的好友关系
//
// **功能说明：**
// 批量删除指定拥有者与多个好友之间的关系记录，同时更新版本信息支持增量同步
//
// **删除策略：**
// - 基于拥有者ID和好友ID列表进行批量删除
// - 使用事务保证删除操作和版本更新的原子性
// - 自动更新版本日志，支持客户端增量同步
//
// **参数：**
// - ctx: 上下文对象
// - ownerUserID: 好友关系拥有者的用户ID
// - friendUserIDs: 要删除的好友用户ID列表
//
// **返回值：**
// - error: 删除过程中的错误信息
//
// **版本控制：**
// - 删除操作会自动记录版本变更
// - 标记状态为 model.VersionStateDelete
// - 支持客户端通过版本号获取变更信息
//
// **使用场景：**
// - 用户删除好友时清理关系记录
// - 批量清理失效的好友关系
// - 用户注销时清理相关数据
//
// **注意事项：**
// - 此操作只删除单向关系，双向删除需要调用两次
// - 删除操作不可逆，建议谨慎使用
// - 大批量删除时需要考虑性能影响
func (f *FriendMgo) Delete(ctx context.Context, ownerUserID string, friendUserIDs []string) error {
	filter := bson.M{
		"owner_user_id":  ownerUserID,
		"friend_user_id": bson.M{"$in": friendUserIDs},
	}
	return mongoutil.IncrVersion(func() error {
		return mongoutil.DeleteOne(ctx, f.coll, filter)
	}, func() error {
		return f.owner.IncrVersion(ctx, ownerUserID, friendUserIDs, model.VersionStateDelete)
	})
}

// UpdateByMap updates specific fields of a friend document using a map.
func (f *FriendMgo) UpdateByMap(ctx context.Context, ownerUserID string, friendUserID string, args map[string]any) error {
	if len(args) == 0 {
		return nil
	}
	filter := bson.M{
		"owner_user_id":  ownerUserID,
		"friend_user_id": friendUserID,
	}
	return mongoutil.IncrVersion(func() error {
		return mongoutil.UpdateOne(ctx, f.coll, filter, bson.M{"$set": args}, true)
	}, func() error {
		var friendUserIDs []string
		if f.IsUpdateIsPinned(args) {
			friendUserIDs = []string{model.VersionSortChangeID, friendUserID}
		} else {
			friendUserIDs = []string{friendUserID}
		}
		return f.owner.IncrVersion(ctx, ownerUserID, friendUserIDs, model.VersionStateUpdate)
	})
}

// UpdateRemark updates the remark for a specific friend.
func (f *FriendMgo) UpdateRemark(ctx context.Context, ownerUserID, friendUserID, remark string) error {
	return f.UpdateByMap(ctx, ownerUserID, friendUserID, map[string]any{"remark": remark})
}

// fillTime 填充好友记录的创建时间
//
// **功能说明：**
// 对于没有明确创建时间的好友记录，使用MongoDB ObjectID中的时间戳
// 作为创建时间，确保所有记录都有有效的时间信息
//
// **实现逻辑：**
// 1. 遍历所有好友记录
// 2. 检查CreateTime是否为零值
// 3. 如果为零值，则从ObjectID提取时间戳
// 4. 更新记录的创建时间
//
// **参数：**
// - friends: 可变参数，需要填充时间的好友记录
//
// **设计考虑：**
// - 向后兼容：处理历史数据缺少时间字段的情况
// - 性能优化：只处理真正需要填充的记录
// - 数据一致性：确保所有记录都有时间信息
func (f *FriendMgo) fillTime(friends ...*model.Friend) {
	for i, friend := range friends {
		if friend.CreateTime.IsZero() {
			friends[i].CreateTime = friend.ID.Timestamp()
		}
	}
}

func (f *FriendMgo) findOne(ctx context.Context, filter any) (*model.Friend, error) {
	friend, err := mongoutil.FindOne[*model.Friend](ctx, f.coll, filter)
	if err != nil {
		return nil, err
	}
	f.fillTime(friend)
	return friend, nil
}

func (f *FriendMgo) find(ctx context.Context, filter any) ([]*model.Friend, error) {
	friends, err := mongoutil.Find[*model.Friend](ctx, f.coll, filter)
	if err != nil {
		return nil, err
	}
	f.fillTime(friends...)
	return friends, nil
}

func (f *FriendMgo) findPage(ctx context.Context, filter any, pagination pagination.Pagination, opts ...*options.FindOptions) (int64, []*model.Friend, error) {
	return mongoutil.FindPage[*model.Friend](ctx, f.coll, filter, pagination, opts...)
}

// Take 获取单个好友关系记录
//
// **功能说明：**
// 根据拥有者ID和好友ID精确查找好友关系记录，如果记录不存在则返回错误
//
// **查询条件：**
// - owner_user_id: 好友关系的拥有者
// - friend_user_id: 好友的用户ID
//
// **参数：**
// - ctx: 上下文对象
// - ownerUserID: 好友关系拥有者的用户ID
// - friendUserID: 好友的用户ID
//
// **返回值：**
// - *model.Friend: 找到的好友关系记录
// - error: 查询错误或记录不存在错误
//
// **使用场景：**
// - 检查两个用户之间是否为好友关系
// - 获取好友关系的详细信息（备注、置顶状态等）
// - 验证好友关系的存在性
//
// **错误处理：**
// - 记录不存在：返回 mongo.ErrNoDocuments
// - 数据库连接错误
// - 查询权限错误
func (f *FriendMgo) Take(ctx context.Context, ownerUserID, friendUserID string) (*model.Friend, error) {
	filter := bson.M{
		"owner_user_id":  ownerUserID,
		"friend_user_id": friendUserID,
	}
	return f.findOne(ctx, filter)
}

// FindUserState finds the friendship status between two users.
func (f *FriendMgo) FindUserState(ctx context.Context, userID1, userID2 string) ([]*model.Friend, error) {
	filter := bson.M{
		"$or": []bson.M{
			{"owner_user_id": userID1, "friend_user_id": userID2},
			{"owner_user_id": userID2, "friend_user_id": userID1},
		},
	}
	return f.find(ctx, filter)
}

// FindFriends retrieves a list of friends for a given owner. Missing friends do not cause an error.
func (f *FriendMgo) FindFriends(ctx context.Context, ownerUserID string, friendUserIDs []string) ([]*model.Friend, error) {
	filter := bson.M{
		"owner_user_id":  ownerUserID,
		"friend_user_id": bson.M{"$in": friendUserIDs},
	}
	return f.find(ctx, filter)
}

// FindReversalFriends finds users who have added the specified user as a friend.
func (f *FriendMgo) FindReversalFriends(ctx context.Context, friendUserID string, ownerUserIDs []string) ([]*model.Friend, error) {
	filter := bson.M{
		"owner_user_id":  bson.M{"$in": ownerUserIDs},
		"friend_user_id": friendUserID,
	}
	return f.find(ctx, filter)
}

// FindOwnerFriends 分页查询指定用户的所有好友关系
//
// **功能说明：**
// 分页获取指定用户作为拥有者的所有好友关系记录，支持置顶排序和稳定分页
//
// **查询特点：**
// - 基于拥有者ID进行筛选
// - 按照置顶状态和ID进行排序（置顶好友优先）
// - 支持高效的分页查询
// - 自动填充创建时间信息
//
// **排序规则：**
// 1. is_pinned 降序 - 置顶好友优先显示
// 2. _id 升序 - 保证排序的稳定性
//
// **参数：**
// - ctx: 上下文对象
// - ownerUserID: 好友关系拥有者的用户ID
// - pagination: 分页参数（页码、每页数量）
//
// **返回值：**
// - int64: 符合条件的好友总数
// - []*model.Friend: 当前页的好友关系列表
// - error: 查询过程中的错误信息
//
// **使用场景：**
// - 用户查看自己的好友列表
// - 好友列表的分页展示
// - 好友关系的统计分析
//
// **性能优化：**
// - 使用复合索引优化查询和排序
// - 分页查询避免大数据量的内存问题
// - 智能排序提升用户体验
func (f *FriendMgo) FindOwnerFriends(ctx context.Context, ownerUserID string, pagination pagination.Pagination) (int64, []*model.Friend, error) {
	filter := bson.M{"owner_user_id": ownerUserID}
	opt := options.Find().SetSort(f.friendSort())
	return f.findPage(ctx, filter, pagination, opt)
}

func (f *FriendMgo) FindOwnerFriendUserIds(ctx context.Context, ownerUserID string, limit int) ([]string, error) {
	filter := bson.M{"owner_user_id": ownerUserID}
	opt := options.Find().SetProjection(bson.M{"_id": 0, "friend_user_id": 1}).SetSort(f.friendSort()).SetLimit(int64(limit))
	return mongoutil.Find[string](ctx, f.coll, filter, opt)
}

// FindInWhoseFriends finds users who have added the specified user as a friend, with pagination.
func (f *FriendMgo) FindInWhoseFriends(ctx context.Context, friendUserID string, pagination pagination.Pagination) (int64, []*model.Friend, error) {
	filter := bson.M{"friend_user_id": friendUserID}
	opt := options.Find().SetSort(f.friendSort())
	return f.findPage(ctx, filter, pagination, opt)
}

// FindFriendUserIDs retrieves a list of friend user IDs for a given owner.
func (f *FriendMgo) FindFriendUserIDs(ctx context.Context, ownerUserID string) ([]string, error) {
	filter := bson.M{"owner_user_id": ownerUserID}
	return mongoutil.Find[string](ctx, f.coll, filter, options.Find().SetProjection(bson.M{"_id": 0, "friend_user_id": 1}).SetSort(f.friendSort()))
}

func (f *FriendMgo) UpdateFriends(ctx context.Context, ownerUserID string, friendUserIDs []string, val map[string]any) error {
	// Ensure there are IDs to update
	if len(friendUserIDs) == 0 || len(val) == 0 {
		return nil // Or return an error if you expect there to always be IDs
	}

	// Create a filter to match documents with the specified ownerUserID and any of the friendUserIDs
	filter := bson.M{
		"owner_user_id":  ownerUserID,
		"friend_user_id": bson.M{"$in": friendUserIDs},
	}

	// Create an update document
	update := bson.M{"$set": val}

	return mongoutil.IncrVersion(func() error {
		return mongoutil.Ignore(mongoutil.UpdateMany(ctx, f.coll, filter, update))
	}, func() error {
		var userIDs []string
		if f.IsUpdateIsPinned(val) {
			userIDs = append([]string{model.VersionSortChangeID}, friendUserIDs...)
		} else {
			userIDs = friendUserIDs
		}
		return f.owner.IncrVersion(ctx, ownerUserID, userIDs, model.VersionStateUpdate)
	})
}

func (f *FriendMgo) FindIncrVersion(ctx context.Context, ownerUserID string, version uint, limit int) (*model.VersionLog, error) {
	return f.owner.FindChangeLog(ctx, ownerUserID, version, limit)
}

func (f *FriendMgo) FindFriendUserID(ctx context.Context, friendUserID string) ([]string, error) {
	filter := bson.M{
		"friend_user_id": friendUserID,
	}
	return mongoutil.Find[string](ctx, f.coll, filter, options.Find().SetProjection(bson.M{"_id": 0, "owner_user_id": 1}).SetSort(f.friendSort()))
}

func (f *FriendMgo) IncrVersion(ctx context.Context, ownerUserID string, friendUserIDs []string, state int32) error {
	return f.owner.IncrVersion(ctx, ownerUserID, friendUserIDs, state)
}

func (f *FriendMgo) IsUpdateIsPinned(data map[string]any) bool {
	if data == nil {
		return false
	}
	_, ok := data["is_pinned"]
	return ok
}
