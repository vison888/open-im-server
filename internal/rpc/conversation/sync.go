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

// Package conversation 会话同步模块
//
// 本文件实现了会话数据的增量同步功能，是IM系统中关键的数据同步机制。
// 主要功能包括：
// 1. 获取用户完整的会话ID列表及其版本信息
// 2. 基于版本差异进行增量会话数据同步
//
// 设计目的：
// - 减少客户端与服务端的数据传输量
// - 确保多端会话数据的一致性
// - 提供高效的会话列表同步机制
package conversation

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/internal/rpc/incrversion"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/open-im-server/v3/pkg/util/hashutil"
	"github.com/openimsdk/protocol/conversation"
)

// GetFullOwnerConversationIDs 获取用户完整的会话ID列表
//
// 这个方法用于获取指定用户的所有会话ID，并提供版本信息用于后续的增量同步。
// 它是会话同步流程的入口点，客户端通过此接口获取会话列表的基线数据。
//
// 工作流程：
// 1. 获取用户的最新会话版本信息
// 2. 查询用户的所有会话ID
// 3. 计算会话ID列表的哈希值
// 4. 与客户端提供的哈希值比较，如果相同则无需返回具体的会话ID
//
// 参数：
// - req.UserID: 用户ID
// - req.IdHash: 客户端当前的会话ID列表哈希值
//
// 返回：
// - Version: 会话ID列表的哈希值（用于后续比较）
// - VersionID: 当前版本的ID（用于增量同步）
// - Equal: 客户端哈希值与服务端是否一致
// - ConversationIDs: 会话ID列表（如果哈希值相同则为空）
func (c *conversationServer) GetFullOwnerConversationIDs(ctx context.Context, req *conversation.GetFullOwnerConversationIDsReq) (*conversation.GetFullOwnerConversationIDsResp, error) {
	// 1. 获取用户的最新会话版本信息
	// 这个版本信息用于后续的增量同步，包含版本ID和版本号
	// 版本信息是增量同步的基础，用于确定客户端需要同步的数据范围
	vl, err := c.conversationDatabase.FindMaxConversationUserVersionCache(ctx, req.UserID)
	if err != nil {
		return nil, err
	}

	// 2. 查询用户的所有会话ID
	// 获取用户当前拥有的所有会话标识符列表
	// 这些ID将用于计算哈希值和后续的数据比较
	conversationIDs, err := c.conversationDatabase.GetConversationIDs(ctx, req.UserID)
	if err != nil {
		return nil, err
	}

	// 3. 计算会话ID列表的哈希值
	// 哈希值用于快速比较客户端和服务端的会话列表是否一致
	// 这是一种高效的数据一致性检查机制，避免传输大量数据
	idHash := hashutil.IdHash(conversationIDs)

	// 4. 如果客户端的哈希值与服务端一致，则无需返回会话ID列表
	// 这是一种优化策略：当数据一致时，不传输具体的会话ID列表
	// 减少网络传输量，提高同步效率
	if req.IdHash == idHash {
		conversationIDs = nil
	}

	return &conversation.GetFullOwnerConversationIDsResp{
		Version:         idHash,               // 当前会话ID列表的哈希值
		VersionID:       vl.ID.Hex(),          // 当前版本的ID（MongoDB ObjectID的十六进制字符串）
		Equal:           req.IdHash == idHash, // 客户端与服务端哈希值是否相等
		ConversationIDs: conversationIDs,      // 会话ID列表（如果哈希值相同则为nil）
	}, nil
}

// GetIncrementalConversation 获取增量会话数据
//
// 这是会话增量同步的核心方法，基于客户端提供的版本信息，
// 返回自该版本以来的所有会话变更（包括新增、更新、删除）。
//
// 增量同步的优势：
// 1. 减少网络传输：只传输变更的数据
// 2. 提高同步效率：避免重复传输未变更的数据
// 3. 降低服务器压力：减少数据查询和序列化的开销
//
// 工作流程：
// 1. 构建增量同步选项，配置各种回调函数
// 2. 验证客户端版本信息的有效性
// 3. 根据版本差异确定同步策略（增量/全量/无需同步）
// 4. 查询版本日志获取变更记录
// 5. 根据变更记录查询具体的会话数据
// 6. 构造并返回同步响应
//
// 参数：
// - req.UserID: 用户ID
// - req.VersionID: 客户端当前版本ID
// - req.Version: 客户端当前版本号
//
// 返回：
// - VersionID: 最新版本ID
// - Version: 最新版本号
// - Full: 是否为全量同步
// - Delete: 删除的会话ID列表
// - Insert: 新增的会话列表
// - Update: 更新的会话列表
func (c *conversationServer) GetIncrementalConversation(ctx context.Context, req *conversation.GetIncrementalConversationReq) (*conversation.GetIncrementalConversationResp, error) {
	// 构建增量同步选项
	// 使用泛型Option来处理会话类型的增量同步
	// 这是一个高度可配置的增量同步框架，支持不同类型的数据同步
	opt := incrversion.Option[*conversation.Conversation, conversation.GetIncrementalConversationResp]{
		Ctx:           ctx,           // 请求上下文，用于传递请求信息和控制超时
		VersionKey:    req.UserID,    // 版本键（用户ID），用于标识特定用户的版本信息
		VersionID:     req.VersionID, // 客户端当前版本ID，用于确定同步起点
		VersionNumber: req.Version,   // 客户端当前版本号，用于版本比较和验证

		// 查询版本日志的回调函数
		// 从数据库中获取指定版本范围的变更日志
		// 这些日志记录了数据的增删改操作，是增量同步的核心数据源
		Version: c.conversationDatabase.FindConversationUserVersion,

		// 缓存最新版本的回调函数（可选）
		// 用于性能优化，快速获取最新版本信息
		// 避免每次都查询数据库，提高响应速度
		CacheMaxVersion: c.conversationDatabase.FindMaxConversationUserVersionCache,

		// 查询具体会话数据的回调函数
		// 根据会话ID列表获取完整的会话数据
		// 这个函数负责将ID转换为完整的会话对象
		Find: func(ctx context.Context, conversationIDs []string) ([]*conversation.Conversation, error) {
			return c.getConversations(ctx, req.UserID, conversationIDs)
		},

		// 构造响应结果的回调函数
		// 将版本信息和数据变更组装成最终的响应格式
		// 这个函数定义了响应的数据结构和格式
		Resp: func(version *model.VersionLog, delIDs []string, insertList, updateList []*conversation.Conversation, full bool) *conversation.GetIncrementalConversationResp {
			return &conversation.GetIncrementalConversationResp{
				VersionID: version.ID.Hex(),        // 最新版本ID（十六进制字符串格式）
				Version:   uint64(version.Version), // 最新版本号（无符号64位整数）
				Full:      full,                    // 是否为全量同步（true: 全量, false: 增量）
				Delete:    delIDs,                  // 删除的会话ID列表
				Insert:    insertList,              // 新增的会话列表
				Update:    updateList,              // 更新的会话列表
			}
		},
	}

	// 执行增量同步并返回结果
	// Build方法会根据配置的选项执行完整的增量同步流程
	// 包括版本验证、数据查询、变更计算和响应构造
	return opt.Build()
}
