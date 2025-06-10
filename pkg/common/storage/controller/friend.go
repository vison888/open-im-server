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

// Package controller 好友关系控制器包
// 提供好友关系管理的全套功能：好友申请、同意/拒绝、好友列表、关系检查等
// 支持双向好友关系、缓存优化、版本控制和事务安全
package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database/mgo"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/tools/db/pagination"
	"github.com/openimsdk/tools/db/tx"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
	"github.com/openimsdk/tools/utils/datautil"
)

// FriendDatabase 好友数据库接口
// 定义了好友关系管理的所有核心操作，包括关系检查、申请处理、列表查询等
type FriendDatabase interface {
	// CheckIn 检查双向好友关系
	// 检查user2是否在user1的好友列表中(inUser1Friends==true)
	// 检查user1是否在user2的好友列表中(inUser2Friends==true)
	// 这是消息发送和权限检查的基础方法
	CheckIn(ctx context.Context, user1, user2 string) (inUser1Friends bool, inUser2Friends bool, err error)

	// AddFriendRequest 添加或更新好友申请
	// 创建新的好友申请，如果已存在则更新申请信息
	// fromUserID: 申请发起者用户ID
	// toUserID: 申请目标用户ID
	// reqMsg: 申请消息
	// ex: 扩展信息
	AddFriendRequest(ctx context.Context, fromUserID, toUserID string, reqMsg string, ex string) (err error)

	// BecomeFriends 建立好友关系
	// 批量建立双向好友关系，确保数据一致性
	// ownerUserID: 好友关系拥有者
	// friendUserIDs: 要建立关系的好友ID列表
	// addSource: 添加来源（通过申请、管理员添加等）
	BecomeFriends(ctx context.Context, ownerUserID string, friendUserIDs []string, addSource int32) (err error)

	// RefuseFriendRequest 拒绝好友申请
	// 将好友申请标记为拒绝状态
	// friendRequest: 好友申请对象
	RefuseFriendRequest(ctx context.Context, friendRequest *model.FriendRequest) (err error)

	// AgreeFriendRequest 同意好友申请
	// 同意好友申请并建立双向好友关系，处理复杂的状态转换
	// friendRequest: 好友申请对象
	AgreeFriendRequest(ctx context.Context, friendRequest *model.FriendRequest) (err error)

	// Delete 删除好友关系
	// 从拥有者的好友列表中移除指定好友
	// ownerUserID: 好友关系拥有者
	// friendUserIDs: 要删除的好友ID列表
	Delete(ctx context.Context, ownerUserID string, friendUserIDs []string) (err error)

	// UpdateRemark 更新好友备注
	// 修改指定好友的备注名称
	// ownerUserID: 好友关系拥有者
	// friendUserID: 好友用户ID
	// remark: 新的备注名称
	UpdateRemark(ctx context.Context, ownerUserID, friendUserID, remark string) (err error)

	// PageOwnerFriends 分页获取好友列表
	// 分页查询指定用户的好友列表
	// ownerUserID: 好友列表拥有者
	// pagination: 分页参数
	PageOwnerFriends(ctx context.Context, ownerUserID string, pagination pagination.Pagination) (total int64, friends []*model.Friend, err error)

	// PageInWhoseFriends 查找谁把某用户加为好友
	// 分页查询将指定用户ID添加为好友的用户列表
	// friendUserID: 被添加为好友的用户ID
	// pagination: 分页参数
	PageInWhoseFriends(ctx context.Context, friendUserID string, pagination pagination.Pagination) (total int64, friends []*model.Friend, err error)

	// PageFriendRequestFromMe 分页获取我发送的好友申请
	// 查询用户发送的好友申请记录
	// userID: 用户ID
	// handleResults: 处理状态过滤（待处理、已同意、已拒绝等）
	// pagination: 分页参数
	PageFriendRequestFromMe(ctx context.Context, userID string, handleResults []int, pagination pagination.Pagination) (total int64, friends []*model.FriendRequest, err error)

	// PageFriendRequestToMe 分页获取我收到的好友申请
	// 查询用户收到的好友申请记录
	// userID: 用户ID
	// handleResults: 处理状态过滤
	// pagination: 分页参数
	PageFriendRequestToMe(ctx context.Context, userID string, handleResults []int, pagination pagination.Pagination) (total int64, friends []*model.FriendRequest, err error)

	// FindFriendsWithError 查找指定好友信息（严格模式）
	// 获取指定好友的详细信息，如果任何好友不存在则返回错误
	// ownerUserID: 好友列表拥有者
	// friendUserIDs: 好友ID列表
	FindFriendsWithError(ctx context.Context, ownerUserID string, friendUserIDs []string) (friends []*model.Friend, err error)

	// FindFriendUserIDs 获取好友ID列表
	// 从缓存中快速获取用户的所有好友ID
	// ownerUserID: 用户ID
	FindFriendUserIDs(ctx context.Context, ownerUserID string) (friendUserIDs []string, err error)

	// FindBothFriendRequests 查找双向好友申请
	// 查找两个用户之间的所有好友申请记录（双向）
	// fromUserID: 申请发起者
	// toUserID: 申请目标者
	FindBothFriendRequests(ctx context.Context, fromUserID, toUserID string) (friends []*model.FriendRequest, err error)

	// UpdateFriends 批量更新好友信息
	// 批量更新指定好友的属性信息
	// ownerUserID: 好友列表拥有者
	// friendUserIDs: 好友ID列表
	// val: 要更新的字段映射
	UpdateFriends(ctx context.Context, ownerUserID string, friendUserIDs []string, val map[string]any) (err error)

	// FindFriendIncrVersion 查找好友增量版本
	// 用于增量同步，获取指定版本之后的好友变更记录
	// ownerUserID: 用户ID
	// version: 起始版本号
	// limit: 返回记录数限制
	FindFriendIncrVersion(ctx context.Context, ownerUserID string, version uint, limit int) (*model.VersionLog, error)

	// FindMaxFriendVersionCache 获取最大好友版本号（缓存）
	// 从缓存中获取用户好友数据的最新版本号
	// ownerUserID: 用户ID
	FindMaxFriendVersionCache(ctx context.Context, ownerUserID string) (*model.VersionLog, error)

	// FindFriendUserID 查找把某用户加为好友的用户列表
	// 查找将指定用户添加为好友的所有用户ID
	// friendUserID: 被添加为好友的用户ID
	FindFriendUserID(ctx context.Context, friendUserID string) ([]string, error)

	// OwnerIncrVersion 用户好友版本递增
	// 更新用户好友数据的版本号，用于增量同步
	// ownerUserID: 用户ID
	// friendUserIDs: 影响的好友ID列表
	// state: 变更状态（添加、删除、更新）
	OwnerIncrVersion(ctx context.Context, ownerUserID string, friendUserIDs []string, state int32) error

	// GetUnhandledCount 获取未处理申请数量
	// 统计指定时间之后的未处理好友申请数量
	// userID: 用户ID
	// ts: 时间戳
	GetUnhandledCount(ctx context.Context, userID string, ts int64) (int64, error)
}

// friendDatabase 好友数据库实现
// 整合了好友数据库、好友申请数据库、缓存和事务管理
type friendDatabase struct {
	friend        database.Friend        // 好友关系数据库接口
	friendRequest database.FriendRequest // 好友申请数据库接口
	tx            tx.Tx                  // 事务管理接口
	cache         cache.FriendCache      // 好友缓存接口
}

// NewFriendDatabase 创建好友数据库实例
// 初始化好友管理所需的所有组件
func NewFriendDatabase(friend database.Friend, friendRequest database.FriendRequest, cache cache.FriendCache, tx tx.Tx) FriendDatabase {
	return &friendDatabase{friend: friend, friendRequest: friendRequest, cache: cache, tx: tx}
}

// CheckIn 检查双向好友关系
// 通过缓存快速检查两个用户之间的好友关系，用于消息发送权限验证
func (f *friendDatabase) CheckIn(ctx context.Context, userID1, userID2 string) (inUser1Friends bool, inUser2Friends bool, err error) {
	// 从缓存获取userID1的好友ID列表
	userID1FriendIDs, err := f.cache.GetFriendIDs(ctx, userID1)
	if err != nil {
		err = fmt.Errorf("error retrieving friend IDs for user %s: %w", userID1, err)
		return
	}

	// 从缓存获取userID2的好友ID列表
	userID2FriendIDs, err := f.cache.GetFriendIDs(ctx, userID2)
	if err != nil {
		err = fmt.Errorf("error retrieving friend IDs for user %s: %w", userID2, err)
		return
	}

	// 检查双向好友关系
	// userID2是否在userID1的好友列表中
	// userID1是否在userID2的好友列表中
	inUser1Friends = datautil.Contain(userID2, userID1FriendIDs...)
	inUser2Friends = datautil.Contain(userID1, userID2FriendIDs...)
	return inUser1Friends, inUser2Friends, nil
}

// AddFriendRequest 添加或更新好友申请
// 如果申请已存在则更新，不存在则创建新申请
func (f *friendDatabase) AddFriendRequest(ctx context.Context, fromUserID, toUserID string, reqMsg string, ex string) (err error) {
	return f.tx.Transaction(ctx, func(ctx context.Context) error {
		// 尝试获取现有申请
		_, err := f.friendRequest.Take(ctx, fromUserID, toUserID)
		switch {
		case err == nil:
			// 申请已存在，更新申请信息
			m := make(map[string]any, 1)
			m["handle_result"] = 0        // 重置为待处理状态
			m["handle_msg"] = ""          // 清空处理消息
			m["req_msg"] = reqMsg         // 更新申请消息
			m["ex"] = ex                  // 更新扩展信息
			m["create_time"] = time.Now() // 更新创建时间
			return f.friendRequest.UpdateByMap(ctx, fromUserID, toUserID, m)
		case mgo.IsNotFound(err):
			// 申请不存在，创建新申请
			return f.friendRequest.Create(
				ctx,
				[]*model.FriendRequest{{
					FromUserID: fromUserID,
					ToUserID:   toUserID,
					ReqMsg:     reqMsg,
					Ex:         ex,
					CreateTime: time.Now(),
					HandleTime: time.Unix(0, 0), // 初始化处理时间为0
				}},
			)
		default:
			return err
		}
	})
}

// BecomeFriends 建立好友关系
// 批量建立双向好友关系，支持去重和增量添加
// (1) 首先检查是否已在好友列表中 (在或不在都不返回错误)
// (2) 对于不在好友列表中的可以插入
func (f *friendDatabase) BecomeFriends(ctx context.Context, ownerUserID string, friendUserIDs []string, addSource int32) (err error) {
	return f.tx.Transaction(ctx, func(ctx context.Context) error {
		cache := f.cache.CloneFriendCache()

		// 查找用户现有的好友关系
		myFriends, err := f.friend.FindFriends(ctx, ownerUserID, friendUserIDs)
		if err != nil {
			return err
		}

		// 查找反向好友关系（谁把ownerUserID加为好友）
		addOwners, err := f.friend.FindReversalFriends(ctx, ownerUserID, friendUserIDs)
		if err != nil {
			return err
		}

		opUserID := mcontext.GetOpUserID(ctx)
		friends := make([]*model.Friend, 0, len(friendUserIDs)*2) // 预分配空间，考虑双向关系

		// 构建已存在好友关系的集合，用于快速查重
		myFriendsSet := datautil.SliceSetAny(myFriends, func(friend *model.Friend) string {
			return friend.FriendUserID
		})
		addOwnersSet := datautil.SliceSetAny(addOwners, func(friend *model.Friend) string {
			return friend.OwnerUserID
		})

		newMyFriendIDs := make([]string, 0, len(friendUserIDs))
		newMyOwnerIDs := make([]string, 0, len(friendUserIDs))

		// 遍历要添加的好友，建立双向关系
		for _, userID := range friendUserIDs {
			if ownerUserID == userID {
				continue // 跳过自己
			}

			// 检查ownerUserID -> userID的关系是否已存在
			if _, ok := myFriendsSet[userID]; !ok {
				myFriendsSet[userID] = struct{}{}
				newMyFriendIDs = append(newMyFriendIDs, userID)
				friends = append(friends, &model.Friend{
					OwnerUserID:    ownerUserID,
					FriendUserID:   userID,
					AddSource:      addSource,
					OperatorUserID: opUserID,
				})
			}

			// 检查userID -> ownerUserID的反向关系是否已存在
			if _, ok := addOwnersSet[userID]; !ok {
				addOwnersSet[userID] = struct{}{}
				newMyOwnerIDs = append(newMyOwnerIDs, userID)
				friends = append(friends, &model.Friend{
					OwnerUserID:    userID,
					FriendUserID:   ownerUserID,
					AddSource:      addSource,
					OperatorUserID: opUserID,
				})
			}
		}

		// 如果没有新的好友关系需要建立，直接返回
		if len(friends) == 0 {
			return nil
		}

		// 批量创建好友关系
		err = f.friend.Create(ctx, friends)
		if err != nil {
			return err
		}

		// 清理相关缓存
		cache = cache.DelFriendIDs(ownerUserID).DelMaxFriendVersion(ownerUserID)

		// 清理新增好友的缓存
		if len(newMyFriendIDs) > 0 {
			cache = cache.DelFriendIDs(newMyFriendIDs...)
			cache = cache.DelFriends(ownerUserID, newMyFriendIDs).DelMaxFriendVersion(newMyFriendIDs...)
		}

		// 清理反向关系的缓存
		if len(newMyOwnerIDs) > 0 {
			cache = cache.DelFriendIDs(newMyOwnerIDs...)
			cache = cache.DelOwner(ownerUserID, newMyOwnerIDs).DelMaxFriendVersion(newMyOwnerIDs...)
		}

		return cache.ChainExecDel(ctx)
	})
}

// RefuseFriendRequest 拒绝好友申请
// 检查申请状态并将其标记为拒绝，包含完整的状态验证
func (f *friendDatabase) RefuseFriendRequest(ctx context.Context, friendRequest *model.FriendRequest) error {
	// 获取现有的好友申请记录
	fr, err := f.friendRequest.Take(ctx, friendRequest.FromUserID, friendRequest.ToUserID)
	if err != nil {
		return fmt.Errorf("failed to retrieve friend request from %s to %s: %w", friendRequest.FromUserID, friendRequest.ToUserID, err)
	}

	// 检查申请是否已被处理
	if fr.HandleResult != 0 {
		return fmt.Errorf("friend request from %s to %s has already been processed", friendRequest.FromUserID, friendRequest.ToUserID)
	}

	// 记录调试日志
	log.ZDebug(ctx, "Refusing friend request", map[string]interface{}{
		"DB_FriendRequest":  fr,
		"Arg_FriendRequest": friendRequest,
	})

	// 更新申请状态为拒绝
	friendRequest.HandleResult = constant.FriendResponseRefuse
	friendRequest.HandleTime = time.Now()

	if err := f.friendRequest.Update(ctx, friendRequest); err != nil {
		return fmt.Errorf("failed to update friend request from %s to %s as refused: %w", friendRequest.FromUserID, friendRequest.ToUserID, err)
	}

	return nil
}

// AgreeFriendRequest 同意好友申请
// 处理复杂的好友申请同意逻辑，包括双向申请处理和好友关系建立
func (f *friendDatabase) AgreeFriendRequest(ctx context.Context, friendRequest *model.FriendRequest) (err error) {
	return f.tx.Transaction(ctx, func(ctx context.Context) error {
		now := time.Now()

		// 获取当前申请记录
		fr, err := f.friendRequest.Take(ctx, friendRequest.FromUserID, friendRequest.ToUserID)
		if err != nil {
			return err
		}

		// 检查申请状态
		if fr.HandleResult != 0 {
			return errs.ErrArgs.WrapMsg("the friend request has been processed")
		}

		// 更新申请状态为已同意
		friendRequest.HandlerUserID = mcontext.GetOpUserID(ctx)
		friendRequest.HandleResult = constant.FriendResponseAgree
		friendRequest.HandleTime = now
		err = f.friendRequest.Update(ctx, friendRequest)
		if err != nil {
			return err
		}

		// 检查是否存在反向申请，如果存在且未处理则自动同意
		fr2, err := f.friendRequest.Take(ctx, friendRequest.ToUserID, friendRequest.FromUserID)
		if err == nil && fr2.HandleResult == constant.FriendResponseNotHandle {
			// 存在未处理的反向申请，自动同意
			fr2.HandlerUserID = mcontext.GetOpUserID(ctx)
			fr2.HandleResult = constant.FriendResponseAgree
			fr2.HandleTime = now
			err = f.friendRequest.Update(ctx, fr2)
			if err != nil {
				return err
			}
		} else if err != nil && (!mgo.IsNotFound(err)) {
			return err
		}

		// 检查现有好友关系状态
		exists, err := f.friend.FindUserState(ctx, friendRequest.FromUserID, friendRequest.ToUserID)
		if err != nil {
			return err
		}

		// 构建已存在关系的映射
		existsMap := datautil.SliceSet(datautil.Slice(exists, func(friend *model.Friend) [2]string {
			return [...]string{friend.OwnerUserID, friend.FriendUserID} // 我 - 好友
		}))

		var adds []*model.Friend

		// 检查并添加缺失的好友关系
		// 添加 ToUserID -> FromUserID 的关系
		if _, ok := existsMap[[...]string{friendRequest.ToUserID, friendRequest.FromUserID}]; !ok {
			adds = append(
				adds,
				&model.Friend{
					OwnerUserID:    friendRequest.ToUserID,
					FriendUserID:   friendRequest.FromUserID,
					AddSource:      int32(constant.BecomeFriendByApply),
					OperatorUserID: friendRequest.FromUserID,
				},
			)
		}

		// 添加 FromUserID -> ToUserID 的关系
		if _, ok := existsMap[[...]string{friendRequest.FromUserID, friendRequest.ToUserID}]; !ok {
			adds = append(
				adds,
				&model.Friend{
					OwnerUserID:    friendRequest.FromUserID,
					FriendUserID:   friendRequest.ToUserID,
					AddSource:      int32(constant.BecomeFriendByApply),
					OperatorUserID: friendRequest.FromUserID,
				},
			)
		}

		// 创建新的好友关系
		if len(adds) > 0 {
			if err := f.friend.Create(ctx, adds); err != nil {
				return err
			}
		}

		// 清理缓存并更新版本
		return f.cache.DelFriendIDs(friendRequest.ToUserID, friendRequest.FromUserID).
			DelMaxFriendVersion(friendRequest.ToUserID, friendRequest.FromUserID).
			ChainExecDel(ctx)
	})
}

// Delete 删除好友关系
// 从好友列表中移除指定好友，外部调用者需要验证好友关系状态
func (f *friendDatabase) Delete(ctx context.Context, ownerUserID string, friendUserIDs []string) (err error) {
	if err := f.friend.Delete(ctx, ownerUserID, friendUserIDs); err != nil {
		return err
	}

	// 清理相关用户的缓存
	userIds := append(friendUserIDs, ownerUserID)
	return f.cache.DelFriendIDs(userIds...).DelMaxFriendVersion(userIds...).ChainExecDel(ctx)
}

// UpdateRemark 更新好友备注
// 支持零值备注的更新
func (f *friendDatabase) UpdateRemark(ctx context.Context, ownerUserID, friendUserID, remark string) (err error) {
	if err := f.friend.UpdateRemark(ctx, ownerUserID, friendUserID, remark); err != nil {
		return err
	}

	// 清理相关缓存
	return f.cache.DelFriend(ownerUserID, friendUserID).DelMaxFriendVersion(ownerUserID).ChainExecDel(ctx)
}

// PageOwnerFriends 分页获取好友列表
// 获取拥有者的好友列表，结果为空时不返回错误
func (f *friendDatabase) PageOwnerFriends(ctx context.Context, ownerUserID string, pagination pagination.Pagination) (total int64, friends []*model.Friend, err error) {
	return f.friend.FindOwnerFriends(ctx, ownerUserID, pagination)
}

// PageInWhoseFriends 查找谁把某用户加为好友
// 查找将friendUserID添加到好友列表的用户
func (f *friendDatabase) PageInWhoseFriends(ctx context.Context, friendUserID string, pagination pagination.Pagination) (total int64, friends []*model.Friend, err error) {
	return f.friend.FindInWhoseFriends(ctx, friendUserID, pagination)
}

// PageFriendRequestFromMe 分页获取我发送的好友申请
// 查询用户发送的好友申请记录，结果为空时不返回错误
func (f *friendDatabase) PageFriendRequestFromMe(ctx context.Context, userID string, handleResults []int, pagination pagination.Pagination) (total int64, friends []*model.FriendRequest, err error) {
	return f.friendRequest.FindFromUserID(ctx, userID, handleResults, pagination)
}

// PageFriendRequestToMe 分页获取我收到的好友申请
// 查询用户收到的好友申请记录，结果为空时不返回错误
func (f *friendDatabase) PageFriendRequestToMe(ctx context.Context, userID string, handleResults []int, pagination pagination.Pagination) (total int64, friends []*model.FriendRequest, err error) {
	return f.friendRequest.FindToUserID(ctx, userID, handleResults, pagination)
}

// FindFriendsWithError 查找指定好友信息（严格模式）
// 获取拥有者的指定好友信息，如果任何好友不存在则返回错误
func (f *friendDatabase) FindFriendsWithError(ctx context.Context, ownerUserID string, friendUserIDs []string) (friends []*model.Friend, err error) {
	friends, err = f.friend.FindFriends(ctx, ownerUserID, friendUserIDs)
	if err != nil {
		return
	}
	return
}

// FindFriendUserIDs 获取好友ID列表
// 从缓存中快速获取用户的所有好友ID
func (f *friendDatabase) FindFriendUserIDs(ctx context.Context, ownerUserID string) (friendUserIDs []string, err error) {
	return f.cache.GetFriendIDs(ctx, ownerUserID)
}

// FindBothFriendRequests 查找双向好友申请
// 查找两个用户之间的所有好友申请记录（双向查询）
func (f *friendDatabase) FindBothFriendRequests(ctx context.Context, fromUserID, toUserID string) (friends []*model.FriendRequest, err error) {
	return f.friendRequest.FindBothFriendRequests(ctx, fromUserID, toUserID)
}

// UpdateFriends 批量更新好友信息
// 批量更新指定好友的属性，支持事务安全
func (f *friendDatabase) UpdateFriends(ctx context.Context, ownerUserID string, friendUserIDs []string, val map[string]any) (err error) {
	if len(val) == 0 {
		return nil
	}
	return f.tx.Transaction(ctx, func(ctx context.Context) error {
		if err := f.friend.UpdateFriends(ctx, ownerUserID, friendUserIDs, val); err != nil {
			return err
		}
		return f.cache.DelFriends(ownerUserID, friendUserIDs).DelMaxFriendVersion(ownerUserID).ChainExecDel(ctx)
	})
}

// FindFriendIncrVersion 查找好友增量版本
// 用于增量同步，获取指定版本之后的好友变更记录
func (f *friendDatabase) FindFriendIncrVersion(ctx context.Context, ownerUserID string, version uint, limit int) (*model.VersionLog, error) {
	return f.friend.FindIncrVersion(ctx, ownerUserID, version, limit)
}

// FindMaxFriendVersionCache 获取最大好友版本号（缓存）
// 从缓存中获取用户好友数据的最新版本号
func (f *friendDatabase) FindMaxFriendVersionCache(ctx context.Context, ownerUserID string) (*model.VersionLog, error) {
	return f.cache.FindMaxFriendVersion(ctx, ownerUserID)
}

// FindFriendUserID 查找把某用户加为好友的用户列表
// 查找将指定用户添加为好友的所有用户ID
func (f *friendDatabase) FindFriendUserID(ctx context.Context, friendUserID string) ([]string, error) {
	return f.friend.FindFriendUserID(ctx, friendUserID)
}

// OwnerIncrVersion 用户好友版本递增
// 更新用户好友数据的版本号，用于增量同步机制
func (f *friendDatabase) OwnerIncrVersion(ctx context.Context, ownerUserID string, friendUserIDs []string, state int32) error {
	if err := f.friend.IncrVersion(ctx, ownerUserID, friendUserIDs, state); err != nil {
		return err
	}
	return f.cache.DelMaxFriendVersion(ownerUserID).ChainExecDel(ctx)
}

// GetUnhandledCount 获取未处理申请数量
// 统计指定时间之后的未处理好友申请数量，用于消息提醒
func (f *friendDatabase) GetUnhandledCount(ctx context.Context, userID string, ts int64) (int64, error) {
	return f.friendRequest.GetUnhandledCount(ctx, userID, ts)
}
