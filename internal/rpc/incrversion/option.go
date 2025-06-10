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

// Package incrversion 增量版本管理包 - 单个目标同步选项
//
// 本文件实现了单个目标的增量版本同步机制，是BatchOption的简化版本。
// 主要用于单个用户或单个会话的数据同步场景。
package incrversion

import (
	"context"
	"fmt"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/tools/errs"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// 同步相关常量定义

// syncLimit 单次同步的最大记录数量限制
// 设置为200是为了平衡性能和内存使用，避免单次同步过多数据导致内存压力
const syncLimit = 200

// 同步策略标签常量
// 用于标识不同的同步策略，决定如何处理客户端的同步请求
const (
	tagQuery = iota + 1 // 增量查询：需要从指定版本号开始查询变更数据
	tagFull             // 全量同步：返回所有数据，通常在版本信息无效或差异过大时使用
	tagEqual            // 版本相等：客户端版本与服务端一致，无需同步
)

// Option 单个目标的增量版本同步选项
//
// 这是针对单个目标（如单个用户的会话列表、好友列表等）的增量同步结构。
// 相比BatchOption，它简化了处理逻辑，适用于单个目标的同步场景。
//
// 泛型参数说明：
// - A: 数据实体类型（如[]*Conversation）
// - B: 响应结果类型（如*GetIncrementalConversationResp）
//
// 工作原理：
// 1. 客户端提供当前的版本ID和版本号
// 2. 服务端验证版本信息的有效性
// 3. 根据版本差异确定同步策略（增量/全量/无需同步）
// 4. 查询并返回相应的数据变更
type Option[A, B any] struct {
	Ctx           context.Context // 请求上下文
	VersionKey    string          // 版本键（通常是用户ID或其他唯一标识）
	VersionID     string          // 客户端当前版本ID（MongoDB ObjectID的十六进制字符串）
	VersionNumber uint64          // 客户端当前版本号

	// 核心回调函数，由调用方实现具体的数据访问逻辑

	// CacheMaxVersion 获取缓存中最新版本的回调函数（可选）
	// 用于性能优化，快速获取最新版本信息进行比较
	// 参数：dId-目标ID
	// 返回：最新版本日志
	CacheMaxVersion func(ctx context.Context, dId string) (*model.VersionLog, error)

	// Version 获取版本日志的回调函数
	// 这是核心的版本查询函数，根据版本号和限制条件查询版本日志
	// 参数：dId-目标ID, version-起始版本号, limit-最大查询条数
	// 返回：版本日志（包含变更记录）
	Version func(ctx context.Context, dId string, version uint, limit int) (*model.VersionLog, error)

	// Find 根据ID列表查询具体数据的回调函数
	// 用于根据版本日志中的数据ID获取实际的数据内容
	// 参数：ids-数据ID列表
	// 返回：查询到的数据实体列表
	Find func(ctx context.Context, ids []string) ([]A, error)

	// Resp 构造最终响应结果的回调函数
	// 将版本信息和数据变更组装成最终的响应格式
	// 参数：version-版本日志, deleteIds-删除的ID列表, insertList-新增数据列表,
	//      updateList-更新数据列表, full-是否全量同步
	// 返回：构造的响应结果
	Resp func(version *model.VersionLog, deleteIds []string, insertList, updateList []A, full bool) *B
}

// newError 创建统一格式的内部服务器错误
func (o *Option[A, B]) newError(msg string) error {
	return errs.ErrInternalServer.WrapMsg(msg)
}

// check 验证Option的必要参数是否完整
// 确保所有必需的回调函数都已设置，避免运行时错误
func (o *Option[A, B]) check() error {
	if o.Ctx == nil {
		return o.newError("opt ctx is nil")
	}
	if o.VersionKey == "" {
		return o.newError("versionKey is empty")
	}
	if o.Version == nil {
		return o.newError("func version is nil")
	}
	if o.Find == nil {
		return o.newError("func find is nil")
	}
	if o.Resp == nil {
		return o.newError("func resp is nil")
	}
	return nil
}

// validVersion 验证客户端提供的版本信息是否有效
// 有效的版本信息需要满足：
// 1. VersionID是有效的MongoDB ObjectID十六进制字符串
// 2. ObjectID不为零值
// 3. VersionNumber大于0
// 返回：版本信息是否有效
func (o *Option[A, B]) validVersion() bool {
	objID, err := primitive.ObjectIDFromHex(o.VersionID)
	return err == nil && (!objID.IsZero()) && o.VersionNumber > 0
}

// equalID 检查客户端版本ID与服务端版本ID是否一致
// 用于判断客户端版本是否是基于服务端当前版本的
func (o *Option[A, B]) equalID(objID primitive.ObjectID) bool {
	return o.VersionID == objID.Hex()
}

// getVersion 获取版本日志数据并确定同步策略
// 这是同步逻辑的核心方法，根据是否有缓存版本查询函数，
// 采用不同的策略来确定同步方式并获取版本日志
//
// 同步策略决策逻辑：
// 1. 如果版本信息无效 -> 全量同步
// 2. 如果版本ID不匹配 -> 全量同步
// 3. 如果版本号相等 -> 无需同步
// 4. 如果版本号不等 -> 增量同步
func (o *Option[A, B]) getVersion(tag *int) (*model.VersionLog, error) {
	// 如果没有缓存版本查询函数，直接查询数据库
	if o.CacheMaxVersion == nil {
		if o.validVersion() {
			// 版本有效，进行增量查询
			*tag = tagQuery
			return o.Version(o.Ctx, o.VersionKey, uint(o.VersionNumber), syncLimit)
		}
		// 版本无效，进行全量同步
		*tag = tagFull
		return o.Version(o.Ctx, o.VersionKey, 0, 0)
	} else {
		// 有缓存版本查询函数，先从缓存获取最新版本进行比较
		cache, err := o.CacheMaxVersion(o.Ctx, o.VersionKey)
		if err != nil {
			return nil, err
		}

		// 验证客户端版本有效性
		if !o.validVersion() {
			// 版本无效，返回缓存的最新版本（全量同步）
			*tag = tagFull
			return cache, nil
		}

		// 检查版本ID是否匹配
		if !o.equalID(cache.ID) {
			// 版本ID不匹配，返回缓存的最新版本（全量同步）
			*tag = tagFull
			return cache, nil
		}

		// 检查版本号是否相等
		if o.VersionNumber == uint64(cache.Version) {
			// 版本号相等，无需同步
			*tag = tagEqual
			return cache, nil
		}

		// 版本号不等，进行增量查询
		*tag = tagQuery
		return o.Version(o.Ctx, o.VersionKey, uint(o.VersionNumber), syncLimit)
	}
}

// Build 构建并执行单个目标的增量同步
// 这是Option的主要执行方法，完成整个同步流程：
// 1. 参数验证
// 2. 获取版本日志并确定同步策略
// 3. 根据同步策略决定是否需要全量同步
// 4. 解析版本日志获取变更的数据ID
// 5. 查询具体的数据内容
// 6. 构造并返回最终结果
func (o *Option[A, B]) Build() (*B, error) {
	// 1. 验证必要参数
	if err := o.check(); err != nil {
		return nil, err
	}

	// 2. 获取版本日志并确定同步策略
	var tag int
	version, err := o.getVersion(&tag)
	if err != nil {
		return nil, err
	}

	// 3. 根据同步策略确定是否需要全量同步
	var full bool
	switch tag {
	case tagQuery:
		// 增量查询：检查版本日志完整性，决定是否需要全量同步
		// 如果版本ID不匹配、版本号落后或日志不完整，则需要全量同步
		full = version.ID.Hex() != o.VersionID || uint64(version.Version) < o.VersionNumber || len(version.Logs) != version.LogLen
	case tagFull:
		// 全量同步
		full = true
	case tagEqual:
		// 版本相等，无需同步
		full = false
	default:
		panic(fmt.Errorf("undefined tag %d", tag))
	}

	// 4. 解析版本日志获取变更的数据ID
	var (
		insertIds []string // 新增数据ID列表
		deleteIds []string // 删除数据ID列表
		updateIds []string // 更新数据ID列表
	)

	// 只有增量同步时才需要解析版本日志
	if !full {
		// 从版本日志中提取增删改的数据ID
		insertIds, deleteIds, updateIds = version.DeleteAndChangeIDs()
	}

	// 5. 查询具体的数据内容
	var (
		insertList []A // 新增数据内容列表
		updateList []A // 更新数据内容列表
	)

	// 查询新增的数据
	if len(insertIds) > 0 {
		insertList, err = o.Find(o.Ctx, insertIds)
		if err != nil {
			return nil, err
		}
	}

	// 查询更新的数据
	if len(updateIds) > 0 {
		updateList, err = o.Find(o.Ctx, updateIds)
		if err != nil {
			return nil, err
		}
	}

	// 6. 构造并返回最终响应结果
	return o.Resp(version, deleteIds, insertList, updateList, full), nil
}
