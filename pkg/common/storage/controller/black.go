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

// Package controller 黑名单控制器包
// 提供用户黑名单的增删查改功能，支持缓存优化和批量操作
package controller

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/tools/db/pagination"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/utils/datautil"
)

// BlackDatabase 黑名单数据库接口
// 定义了黑名单管理的核心操作接口，包括增删查改和关系检查
type BlackDatabase interface {
	// Create 添加黑名单记录
	// 将指定用户添加到黑名单中，支持批量添加
	// blacks: 黑名单记录列表
	// 返回: 错误信息
	Create(ctx context.Context, blacks []*model.Black) (err error)

	// Delete 删除黑名单记录
	// 从黑名单中移除指定用户，支持批量删除
	// blacks: 要删除的黑名单记录列表
	// 返回: 错误信息
	Delete(ctx context.Context, blacks []*model.Black) (err error)

	// FindOwnerBlacks 获取用户的黑名单列表
	// 分页查询指定用户的所有黑名单记录
	// ownerUserID: 黑名单拥有者用户ID
	// pagination: 分页参数
	// 返回: 总数、黑名单记录列表、错误信息
	FindOwnerBlacks(ctx context.Context, ownerUserID string, pagination pagination.Pagination) (total int64, blacks []*model.Black, err error)

	// FindBlackInfos 查找指定用户的黑名单信息
	// 检查指定用户ID列表中哪些在黑名单中
	// ownerUserID: 黑名单拥有者用户ID
	// userIDs: 要检查的用户ID列表
	// 返回: 黑名单记录列表、错误信息
	FindBlackInfos(ctx context.Context, ownerUserID string, userIDs []string) (blacks []*model.Black, err error)

	// CheckIn 检查双向黑名单关系
	// 检查user2是否在user1的黑名单中(inUser1Blacks==true)
	// 检查user1是否在user2的黑名单中(inUser2Blacks==true)
	// userID1: 第一个用户ID
	// userID2: 第二个用户ID
	// 返回: user2是否在user1黑名单中、user1是否在user2黑名单中、错误信息
	CheckIn(ctx context.Context, userID1, userID2 string) (inUser1Blacks bool, inUser2Blacks bool, err error)
}

// blackDatabase 黑名单数据库实现
// 整合了数据库操作和缓存管理，提供高性能的黑名单服务
type blackDatabase struct {
	black database.Black   // 黑名单数据库接口，提供持久化存储
	cache cache.BlackCache // 黑名单缓存接口，提供快速查询能力
}

// NewBlackDatabase 创建黑名单数据库实例
// 初始化黑名单管理所需的数据库和缓存组件
// black: 黑名单数据库接口
// cache: 黑名单缓存接口
// 返回: BlackDatabase接口实例
func NewBlackDatabase(black database.Black, cache cache.BlackCache) BlackDatabase {
	return &blackDatabase{black, cache}
}

// Create 添加黑名单记录
// 将用户添加到黑名单中，同时更新数据库和缓存
func (b *blackDatabase) Create(ctx context.Context, blacks []*model.Black) (err error) {
	// 先更新数据库
	if err := b.black.Create(ctx, blacks); err != nil {
		return err
	}
	// 再清除相关缓存，保证数据一致性
	return b.deleteBlackIDsCache(ctx, blacks)
}

// Delete 删除黑名单记录
// 从黑名单中移除用户，同时更新数据库和缓存
func (b *blackDatabase) Delete(ctx context.Context, blacks []*model.Black) (err error) {
	// 先更新数据库
	if err := b.black.Delete(ctx, blacks); err != nil {
		return err
	}
	// 再清除相关缓存，保证数据一致性
	return b.deleteBlackIDsCache(ctx, blacks)
}

// deleteBlackIDsCache 删除黑名单ID缓存
// 在黑名单变更后清除相关用户的黑名单缓存，确保数据一致性
func (b *blackDatabase) deleteBlackIDsCache(ctx context.Context, blacks []*model.Black) (err error) {
	cache := b.cache.CloneBlackCache()
	for _, black := range blacks {
		// 清除黑名单拥有者的缓存
		cache = cache.DelBlackIDs(ctx, black.OwnerUserID)
	}
	return cache.ChainExecDel(ctx)
}

// FindOwnerBlacks 获取用户的黑名单列表
// 分页查询指定用户的所有黑名单记录，直接从数据库查询
func (b *blackDatabase) FindOwnerBlacks(ctx context.Context, ownerUserID string, pagination pagination.Pagination) (total int64, blacks []*model.Black, err error) {
	return b.black.FindOwnerBlacks(ctx, ownerUserID, pagination)
}

// CheckIn 检查双向黑名单关系
// 通过缓存快速检查两个用户之间的黑名单关系
// 这是一个高频调用的方法，主要用于消息发送前的权限检查
func (b *blackDatabase) CheckIn(ctx context.Context, userID1, userID2 string) (inUser1Blacks bool, inUser2Blacks bool, err error) {
	// 从缓存获取user1的黑名单ID列表
	userID1BlackIDs, err := b.cache.GetBlackIDs(ctx, userID1)
	if err != nil {
		return
	}
	// 从缓存获取user2的黑名单ID列表
	userID2BlackIDs, err := b.cache.GetBlackIDs(ctx, userID2)
	if err != nil {
		return
	}
	// 记录调试日志
	log.ZDebug(ctx, "blackIDs", "user1BlackIDs", userID1BlackIDs, "user2BlackIDs", userID2BlackIDs)

	// 检查双向黑名单关系
	// 检查user2是否在user1的黑名单中
	// 检查user1是否在user2的黑名单中
	return datautil.Contain(userID2, userID1BlackIDs...), datautil.Contain(userID1, userID2BlackIDs...), nil
}

// FindBlackIDs 获取用户的黑名单ID列表
// 从缓存中获取指定用户的所有黑名单用户ID
func (b *blackDatabase) FindBlackIDs(ctx context.Context, ownerUserID string) (blackIDs []string, err error) {
	return b.cache.GetBlackIDs(ctx, ownerUserID)
}

// FindBlackInfos 查找指定用户的黑名单信息
// 查询指定用户ID列表中哪些在黑名单中，返回详细的黑名单记录
func (b *blackDatabase) FindBlackInfos(ctx context.Context, ownerUserID string, userIDs []string) (blacks []*model.Black, err error) {
	return b.black.FindOwnerBlackInfos(ctx, ownerUserID, userIDs)
}
