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

// Package controller 会话管理控制器包
// 提供会话的创建、更新、查询、同步等全套功能
// 支持单聊、群聊会话管理，消息免打扰、置顶、已读状态等高级特性
// 实现了会话版本控制和增量同步机制
package controller

import (
	"context"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	relationtb "github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache"
	"github.com/openimsdk/open-im-server/v3/pkg/msgprocessor"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/tools/db/pagination"
	"github.com/openimsdk/tools/db/tx"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/utils/datautil"
	"github.com/openimsdk/tools/utils/stringutil"
)

// ConversationDatabase 会话数据库接口
// 定义了会话管理的所有核心操作，包括会话的CRUD、状态管理、同步等功能
type ConversationDatabase interface {
	// UpdateUsersConversationField 更新多用户的会话属性
	// 批量更新指定用户列表在某个会话中的属性字段
	// userIDs: 用户ID列表
	// conversationID: 会话ID
	// args: 要更新的字段映射
	UpdateUsersConversationField(ctx context.Context, userIDs []string, conversationID string, args map[string]any) error

	// CreateConversation 批量创建会话
	// 为用户批量创建新的会话记录
	// conversations: 会话记录列表
	CreateConversation(ctx context.Context, conversations []*relationtb.Conversation) error

	// SyncPeerUserPrivateConversationTx 同步私聊会话（事务）
	// 确保私聊双方的会话状态同步，使用事务保证一致性
	// conversation: 要同步的会话列表
	SyncPeerUserPrivateConversationTx(ctx context.Context, conversation []*relationtb.Conversation) error

	// FindConversations 查找多个会话
	// 根据会话ID列表获取用户的会话信息
	// ownerUserID: 会话拥有者用户ID
	// conversationIDs: 会话ID列表
	FindConversations(ctx context.Context, ownerUserID string, conversationIDs []string) ([]*relationtb.Conversation, error)

	// GetUserAllConversation 获取用户所有会话
	// 获取用户在服务器上的所有会话记录
	// ownerUserID: 用户ID
	GetUserAllConversation(ctx context.Context, ownerUserID string) ([]*relationtb.Conversation, error)

	// SetUserConversations 设置用户会话
	// 批量设置用户的会话属性，不存在则创建，存在则更新，操作原子性
	// ownerUserID: 会话拥有者
	// conversations: 会话列表
	SetUserConversations(ctx context.Context, ownerUserID string, conversations []*relationtb.Conversation) error

	// SetUsersConversationFieldTx 事务性批量设置会话字段
	// 为多个用户更新指定会话的特定字段，不存在则创建，存在则更新，操作事务性
	// userIDs: 用户ID列表
	// conversation: 会话模板
	// fieldMap: 字段映射
	SetUsersConversationFieldTx(ctx context.Context, userIDs []string, conversation *relationtb.Conversation, fieldMap map[string]any) error

	// CreateGroupChatConversation 创建群聊会话
	// 为指定群组和用户列表创建群聊会话
	// groupID: 群组ID
	// userIDs: 用户ID列表
	CreateGroupChatConversation(ctx context.Context, groupID string, userIDs []string) error

	// GetConversationIDs 获取会话ID列表
	// 获取指定用户的所有会话ID
	// userID: 用户ID
	GetConversationIDs(ctx context.Context, userID string) ([]string, error)

	// GetUserConversationIDsHash 获取用户会话ID哈希
	// 获取用户会话ID列表的哈希值，用于快速比较
	// ownerUserID: 用户ID
	GetUserConversationIDsHash(ctx context.Context, ownerUserID string) (hash uint64, err error)

	// GetAllConversationIDs 获取所有会话ID
	// 获取系统中所有的会话ID列表
	GetAllConversationIDs(ctx context.Context) ([]string, error)

	// GetAllConversationIDsNumber 获取会话ID总数
	// 返回系统中所有会话ID的数量
	GetAllConversationIDsNumber(ctx context.Context) (int64, error)

	// PageConversationIDs 分页获取会话ID
	// 根据分页设置分页获取会话ID列表
	// pagination: 分页参数
	PageConversationIDs(ctx context.Context, pagination pagination.Pagination) (conversationIDs []string, err error)

	// GetConversationsByConversationID 根据会话ID获取会话
	// 通过会话ID列表批量获取会话信息
	// conversationIDs: 会话ID列表
	GetConversationsByConversationID(ctx context.Context, conversationIDs []string) ([]*relationtb.Conversation, error)

	// GetConversationIDsNeedDestruct 获取需要销毁的会话ID
	// 根据特定条件获取需要销毁的会话列表
	GetConversationIDsNeedDestruct(ctx context.Context) ([]*relationtb.Conversation, error)

	// GetConversationNotReceiveMessageUserIDs 获取不接收消息的用户ID
	// 获取在指定会话中设置为不接收消息的用户ID列表
	// conversationID: 会话ID
	GetConversationNotReceiveMessageUserIDs(ctx context.Context, conversationID string) ([]string, error)

	// FindConversationUserVersion 查找会话用户版本
	// 用于增量同步，查找指定版本之后的会话变更
	// userID: 用户ID
	// version: 起始版本号
	// limit: 返回记录数限制
	FindConversationUserVersion(ctx context.Context, userID string, version uint, limit int) (*relationtb.VersionLog, error)

	// FindMaxConversationUserVersionCache 查找最大会话版本（缓存）
	// 从缓存中获取用户会话数据的最新版本号
	// userID: 用户ID
	FindMaxConversationUserVersionCache(ctx context.Context, userID string) (*relationtb.VersionLog, error)

	// GetOwnerConversation 获取拥有者会话（分页）
	// 分页获取用户的会话列表
	// ownerUserID: 用户ID
	// pagination: 分页参数
	GetOwnerConversation(ctx context.Context, ownerUserID string, pagination pagination.Pagination) (int64, []*relationtb.Conversation, error)

	// GetNotNotifyConversationIDs 获取免打扰会话ID
	// 获取用户设置为免打扰的会话ID列表
	// userID: 用户ID
	GetNotNotifyConversationIDs(ctx context.Context, userID string) ([]string, error)

	// GetPinnedConversationIDs 获取置顶会话ID
	// 获取用户设置为置顶的会话ID列表
	// userID: 用户ID
	GetPinnedConversationIDs(ctx context.Context, userID string) ([]string, error)

	// FindRandConversation 查找随机会话
	// 根据指定时间戳和限制数量查找随机会话
	// ts: 时间戳
	// limit: 限制数量
	FindRandConversation(ctx context.Context, ts int64, limit int) ([]*relationtb.Conversation, error)
}

// NewConversationDatabase 创建会话数据库实例
// 初始化会话管理所需的所有组件：数据库、缓存、事务管理
func NewConversationDatabase(conversation database.Conversation, cache cache.ConversationCache, tx tx.Tx) ConversationDatabase {
	return &conversationDatabase{
		conversationDB: conversation,
		cache:          cache,
		tx:             tx,
	}
}

// conversationDatabase 会话数据库实现
// 整合了会话数据库操作、缓存管理和事务控制
type conversationDatabase struct {
	conversationDB database.Conversation   // 会话数据库接口，提供持久化存储
	cache          cache.ConversationCache // 会话缓存接口，提供高性能查询
	tx             tx.Tx                   // 事务管理接口，确保数据一致性
}

// SetUsersConversationFieldTx 事务性批量设置会话字段
// 为多个用户更新指定会话的特定字段，处理复杂的创建和更新逻辑
func (c *conversationDatabase) SetUsersConversationFieldTx(ctx context.Context, userIDs []string, conversation *relationtb.Conversation, fieldMap map[string]any) (err error) {
	return c.tx.Transaction(ctx, func(ctx context.Context) error {
		cache := c.cache.CloneConversationCache()

		// 如果是群组会话，清理群组相关缓存
		if conversation.GroupID != "" {
			cache = cache.DelSuperGroupRecvMsgNotNotifyUserIDs(conversation.GroupID).DelSuperGroupRecvMsgNotNotifyUserIDsHash(conversation.GroupID)
		}

		// 查找已存在会话的用户ID
		haveUserIDs, err := c.conversationDB.FindUserID(ctx, userIDs, []string{conversation.ConversationID})
		if err != nil {
			return err
		}

		// 更新已存在的会话
		if len(haveUserIDs) > 0 {
			_, err = c.conversationDB.UpdateByMap(ctx, haveUserIDs, conversation.ConversationID, fieldMap)
			if err != nil {
				return err
			}
			cache = cache.DelUsersConversation(conversation.ConversationID, haveUserIDs...)

			// 如果更新已读序列号，清理相关缓存
			if _, ok := fieldMap["has_read_seq"]; ok {
				for _, userID := range haveUserIDs {
					cache = cache.DelUserAllHasReadSeqs(userID, conversation.ConversationID)
				}
			}

			// 如果更新消息接收选项，清理相关缓存
			if _, ok := fieldMap["recv_msg_opt"]; ok {
				cache = cache.DelConversationNotReceiveMessageUserIDs(conversation.ConversationID)
				cache = cache.DelConversationNotNotifyMessageUserIDs(userIDs...)
			}

			// 如果更新置顶状态，清理相关缓存
			if _, ok := fieldMap["is_pinned"]; ok {
				cache = cache.DelConversationPinnedMessageUserIDs(userIDs...)
			}
			cache = cache.DelConversationVersionUserIDs(haveUserIDs...)
		}

		// 计算不存在会话的用户ID
		NotUserIDs := stringutil.DifferenceString(haveUserIDs, userIDs)
		log.ZDebug(ctx, "SetUsersConversationFieldTx", "NotUserIDs", NotUserIDs, "haveUserIDs", haveUserIDs, "userIDs", userIDs)

		// 为不存在会话的用户创建新会话
		var conversations []*relationtb.Conversation
		now := time.Now()
		for _, v := range NotUserIDs {
			temp := new(relationtb.Conversation)
			if err = datautil.CopyStructFields(temp, conversation); err != nil {
				return err
			}
			temp.OwnerUserID = v
			temp.CreateTime = now
			conversations = append(conversations, temp)
		}

		// 批量创建新会话
		if len(conversations) > 0 {
			err = c.conversationDB.Create(ctx, conversations)
			if err != nil {
				return err
			}
			cache = cache.DelConversationIDs(NotUserIDs...).DelUserConversationIDsHash(NotUserIDs...).DelConversations(conversation.ConversationID, NotUserIDs...)
		}

		return cache.ChainExecDel(ctx)
	})
}

// UpdateUsersConversationField 更新多用户的会话属性
// 批量更新指定用户在指定会话中的属性，并清理相关缓存
func (c *conversationDatabase) UpdateUsersConversationField(ctx context.Context, userIDs []string, conversationID string, args map[string]any) error {
	_, err := c.conversationDB.UpdateByMap(ctx, userIDs, conversationID, args)
	if err != nil {
		return err
	}

	cache := c.cache.CloneConversationCache()
	cache = cache.DelUsersConversation(conversationID, userIDs...).DelConversationVersionUserIDs(userIDs...)

	// 根据更新的字段类型清理对应缓存
	if _, ok := args["recv_msg_opt"]; ok {
		cache = cache.DelConversationNotReceiveMessageUserIDs(conversationID)
		cache = cache.DelConversationNotNotifyMessageUserIDs(userIDs...)
	}
	if _, ok := args["is_pinned"]; ok {
		cache = cache.DelConversationPinnedMessageUserIDs(userIDs...)
	}
	return cache.ChainExecDel(ctx)
}

// CreateConversation 批量创建会话
// 创建新的会话记录并清理相关缓存，支持消息免打扰和置顶状态管理
func (c *conversationDatabase) CreateConversation(ctx context.Context, conversations []*relationtb.Conversation) error {
	if err := c.conversationDB.Create(ctx, conversations); err != nil {
		return err
	}

	var (
		userIDs          []string
		notNotifyUserIDs []string
		pinnedUserIDs    []string
	)

	cache := c.cache.CloneConversationCache()
	for _, conversation := range conversations {
		cache = cache.DelConversations(conversation.OwnerUserID, conversation.ConversationID)
		cache = cache.DelConversationNotReceiveMessageUserIDs(conversation.ConversationID)
		userIDs = append(userIDs, conversation.OwnerUserID)

		// 收集免打扰用户
		if conversation.RecvMsgOpt == constant.ReceiveNotNotifyMessage {
			notNotifyUserIDs = append(notNotifyUserIDs, conversation.OwnerUserID)
		}

		// 收集置顶用户
		if conversation.IsPinned {
			pinnedUserIDs = append(pinnedUserIDs, conversation.OwnerUserID)
		}
	}

	// 批量清理缓存
	return cache.DelConversationIDs(userIDs...).
		DelUserConversationIDsHash(userIDs...).
		DelConversationVersionUserIDs(userIDs...).
		DelConversationNotNotifyMessageUserIDs(notNotifyUserIDs...).
		DelConversationPinnedMessageUserIDs(pinnedUserIDs...).
		ChainExecDel(ctx)
}

// SyncPeerUserPrivateConversationTx 同步私聊会话（事务）
// 确保私聊双方的会话状态同步，处理私聊标记等特殊属性
func (c *conversationDatabase) SyncPeerUserPrivateConversationTx(ctx context.Context, conversations []*relationtb.Conversation) error {
	return c.tx.Transaction(ctx, func(ctx context.Context) error {
		cache := c.cache.CloneConversationCache()

		for _, conversation := range conversations {
			cache = cache.DelConversationVersionUserIDs(conversation.OwnerUserID, conversation.UserID)

			// 处理双向私聊会话同步
			for _, v := range [][2]string{{conversation.OwnerUserID, conversation.UserID}, {conversation.UserID, conversation.OwnerUserID}} {
				ownerUserID := v[0]
				userID := v[1]

				// 检查会话是否已存在
				haveUserIDs, err := c.conversationDB.FindUserID(ctx, []string{ownerUserID}, []string{conversation.ConversationID})
				if err != nil {
					return err
				}

				if len(haveUserIDs) > 0 {
					// 更新已存在的会话
					_, err := c.conversationDB.UpdateByMap(ctx, []string{ownerUserID}, conversation.ConversationID, map[string]any{"is_private_chat": conversation.IsPrivateChat})
					if err != nil {
						return err
					}
					cache = cache.DelUsersConversation(conversation.ConversationID, ownerUserID)
				} else {
					// 创建新的会话
					newConversation := *conversation
					newConversation.OwnerUserID = ownerUserID
					newConversation.UserID = userID
					newConversation.ConversationID = conversation.ConversationID
					newConversation.IsPrivateChat = conversation.IsPrivateChat

					if err := c.conversationDB.Create(ctx, []*relationtb.Conversation{&newConversation}); err != nil {
						return err
					}
					cache = cache.DelConversationIDs(ownerUserID).DelUserConversationIDsHash(ownerUserID)
				}
			}
		}
		return cache.ChainExecDel(ctx)
	})
}

// FindConversations 查找多个会话
// 从缓存中获取用户的多个会话信息
func (c *conversationDatabase) FindConversations(ctx context.Context, ownerUserID string, conversationIDs []string) ([]*relationtb.Conversation, error) {
	return c.cache.GetConversations(ctx, ownerUserID, conversationIDs)
}

// GetConversation 获取单个会话
// 从缓存中获取用户的单个会话信息
func (c *conversationDatabase) GetConversation(ctx context.Context, ownerUserID string, conversationID string) (*relationtb.Conversation, error) {
	return c.cache.GetConversation(ctx, ownerUserID, conversationID)
}

// GetUserAllConversation 获取用户所有会话
// 从缓存中获取用户的所有会话记录
func (c *conversationDatabase) GetUserAllConversation(ctx context.Context, ownerUserID string) ([]*relationtb.Conversation, error) {
	return c.cache.GetUserAllConversations(ctx, ownerUserID)
}

// SetUserConversations 设置用户会话
// 批量设置用户的会话，支持创建和更新操作，使用事务确保一致性
func (c *conversationDatabase) SetUserConversations(ctx context.Context, ownerUserID string, conversations []*relationtb.Conversation) error {
	return c.tx.Transaction(ctx, func(ctx context.Context) error {
		cache := c.cache.CloneConversationCache()
		cache = cache.DelConversationVersionUserIDs(ownerUserID).
			DelConversationNotNotifyMessageUserIDs(ownerUserID).
			DelConversationPinnedMessageUserIDs(ownerUserID)

		// 收集群组ID并清理群组相关缓存
		groupIDs := datautil.Distinct(datautil.Filter(conversations, func(e *relationtb.Conversation) (string, bool) {
			return e.GroupID, e.GroupID != ""
		}))
		for _, groupID := range groupIDs {
			cache = cache.DelSuperGroupRecvMsgNotNotifyUserIDs(groupID).DelSuperGroupRecvMsgNotNotifyUserIDsHash(groupID)
		}

		var conversationIDs []string
		for _, conversation := range conversations {
			conversationIDs = append(conversationIDs, conversation.ConversationID)
			cache = cache.DelConversations(conversation.OwnerUserID, conversation.ConversationID)
		}

		// 查找已存在的会话
		existConversations, err := c.conversationDB.Find(ctx, ownerUserID, conversationIDs)
		if err != nil {
			return err
		}

		// 更新已存在的会话
		if len(existConversations) > 0 {
			for _, conversation := range conversations {
				err = c.conversationDB.Update(ctx, conversation)
				if err != nil {
					return err
				}
			}
		}

		// 计算已存在的会话ID
		var existConversationIDs []string
		for _, conversation := range existConversations {
			existConversationIDs = append(existConversationIDs, conversation.ConversationID)
		}

		// 创建不存在的会话
		var notExistConversations []*relationtb.Conversation
		for _, conversation := range conversations {
			if !datautil.Contain(conversation.ConversationID, existConversationIDs...) {
				notExistConversations = append(notExistConversations, conversation)
			}
		}

		if len(notExistConversations) > 0 {
			err = c.conversationDB.Create(ctx, notExistConversations)
			if err != nil {
				return err
			}
			cache = cache.DelConversationIDs(ownerUserID).
				DelUserConversationIDsHash(ownerUserID).
				DelConversationNotReceiveMessageUserIDs(datautil.Slice(notExistConversations, func(e *relationtb.Conversation) string { return e.ConversationID })...)
		}
		return cache.ChainExecDel(ctx)
	})
}

// CreateGroupChatConversation 创建群聊会话
// 为指定群组和用户列表创建群聊会话，处理已存在用户的序列号重置
func (c *conversationDatabase) CreateGroupChatConversation(ctx context.Context, groupID string, userIDs []string) error {
	return c.tx.Transaction(ctx, func(ctx context.Context) error {
		cache := c.cache.CloneConversationCache()
		conversationID := msgprocessor.GetConversationIDBySessionType(constant.ReadGroupChatType, groupID)

		// 查找已存在会话的用户
		existConversationUserIDs, err := c.conversationDB.FindUserID(ctx, userIDs, []string{conversationID})
		if err != nil {
			return err
		}

		// 计算不存在会话的用户
		notExistUserIDs := stringutil.DifferenceString(userIDs, existConversationUserIDs)

		// 为不存在会话的用户创建会话
		var conversations []*relationtb.Conversation
		for _, v := range notExistUserIDs {
			conversation := relationtb.Conversation{
				ConversationType: constant.ReadGroupChatType,
				GroupID:          groupID,
				OwnerUserID:      v,
				ConversationID:   conversationID,
			}
			conversations = append(conversations, &conversation)
			cache = cache.DelConversations(v, conversationID).DelConversationNotReceiveMessageUserIDs(conversationID)
		}
		cache = cache.DelConversationIDs(notExistUserIDs...).DelUserConversationIDsHash(notExistUserIDs...)

		// 批量创建会话
		if len(conversations) > 0 {
			err = c.conversationDB.Create(ctx, conversations)
			if err != nil {
				return err
			}
		}

		// 重置已存在用户的最大序列号
		_, err = c.conversationDB.UpdateByMap(ctx, existConversationUserIDs, conversationID, map[string]any{"max_seq": 0})
		if err != nil {
			return err
		}

		// 清理已存在用户的缓存
		for _, v := range existConversationUserIDs {
			cache = cache.DelConversations(v, conversationID)
		}
		return cache.ChainExecDel(ctx)
	})
}

// GetConversationIDs 获取会话ID列表
// 从缓存获取用户的所有会话ID
func (c *conversationDatabase) GetConversationIDs(ctx context.Context, userID string) ([]string, error) {
	return c.cache.GetUserConversationIDs(ctx, userID)
}

// GetUserConversationIDsHash 获取用户会话ID哈希
// 获取用户会话ID列表的哈希值，用于快速变更检测
func (c *conversationDatabase) GetUserConversationIDsHash(ctx context.Context, ownerUserID string) (hash uint64, err error) {
	return c.cache.GetUserConversationIDsHash(ctx, ownerUserID)
}

// GetAllConversationIDs 获取所有会话ID
// 从数据库获取系统中所有的会话ID
func (c *conversationDatabase) GetAllConversationIDs(ctx context.Context) ([]string, error) {
	return c.conversationDB.GetAllConversationIDs(ctx)
}

// GetAllConversationIDsNumber 获取会话ID总数
// 获取系统中所有会话ID的数量统计
func (c *conversationDatabase) GetAllConversationIDsNumber(ctx context.Context) (int64, error) {
	return c.conversationDB.GetAllConversationIDsNumber(ctx)
}

// PageConversationIDs 分页获取会话ID
// 根据分页参数获取会话ID列表
func (c *conversationDatabase) PageConversationIDs(ctx context.Context, pagination pagination.Pagination) ([]string, error) {
	return c.conversationDB.PageConversationIDs(ctx, pagination)
}

// GetConversationsByConversationID 根据会话ID获取会话
// 通过会话ID列表批量获取会话详细信息
func (c *conversationDatabase) GetConversationsByConversationID(ctx context.Context, conversationIDs []string) ([]*relationtb.Conversation, error) {
	return c.conversationDB.GetConversationsByConversationID(ctx, conversationIDs)
}

// GetConversationIDsNeedDestruct 获取需要销毁的会话ID
// 获取需要进行销毁处理的会话列表
func (c *conversationDatabase) GetConversationIDsNeedDestruct(ctx context.Context) ([]*relationtb.Conversation, error) {
	return c.conversationDB.GetConversationIDsNeedDestruct(ctx)
}

// GetConversationNotReceiveMessageUserIDs 获取不接收消息的用户ID
// 从缓存获取指定会话中设置为不接收消息的用户ID列表
func (c *conversationDatabase) GetConversationNotReceiveMessageUserIDs(ctx context.Context, conversationID string) ([]string, error) {
	return c.cache.GetConversationNotReceiveMessageUserIDs(ctx, conversationID)
}

// FindConversationUserVersion 查找会话用户版本
// 查找指定版本之后的会话变更记录，用于增量同步
func (c *conversationDatabase) FindConversationUserVersion(ctx context.Context, userID string, version uint, limit int) (*relationtb.VersionLog, error) {
	return c.conversationDB.FindConversationUserVersion(ctx, userID, version, limit)
}

// FindMaxConversationUserVersionCache 查找最大会话版本（缓存）
// 从缓存获取用户会话数据的最新版本号
func (c *conversationDatabase) FindMaxConversationUserVersionCache(ctx context.Context, userID string) (*relationtb.VersionLog, error) {
	return c.cache.FindMaxConversationUserVersion(ctx, userID)
}

// GetOwnerConversation 获取拥有者会话（分页）
// 分页获取用户的会话列表，优先从缓存获取
func (c *conversationDatabase) GetOwnerConversation(ctx context.Context, ownerUserID string, pagination pagination.Pagination) (int64, []*relationtb.Conversation, error) {
	// 先获取用户的所有会话ID
	conversationIDs, err := c.cache.GetUserConversationIDs(ctx, ownerUserID)
	if err != nil {
		return 0, nil, err
	}

	// 根据分页参数获取指定范围的会话ID
	findConversationIDs := datautil.Paginate(conversationIDs, int(pagination.GetPageNumber()), int(pagination.GetShowNumber()))
	conversations := make([]*relationtb.Conversation, 0, len(findConversationIDs))

	// 批量获取会话详细信息
	for _, conversationID := range findConversationIDs {
		conversation, err := c.cache.GetConversation(ctx, ownerUserID, conversationID)
		if err != nil {
			return 0, nil, err
		}
		conversations = append(conversations, conversation)
	}
	return int64(len(conversationIDs)), conversations, nil
}

// GetNotNotifyConversationIDs 获取免打扰会话ID
// 从缓存获取用户设置为免打扰的会话ID列表
func (c *conversationDatabase) GetNotNotifyConversationIDs(ctx context.Context, userID string) ([]string, error) {
	conversationIDs, err := c.cache.GetUserNotNotifyConversationIDs(ctx, userID)
	if err != nil {
		return nil, err
	}
	return conversationIDs, nil
}

// GetPinnedConversationIDs 获取置顶会话ID
// 从缓存获取用户设置为置顶的会话ID列表
func (c *conversationDatabase) GetPinnedConversationIDs(ctx context.Context, userID string) ([]string, error) {
	conversationIDs, err := c.cache.GetPinnedConversationIDs(ctx, userID)
	if err != nil {
		return nil, err
	}
	return conversationIDs, nil
}

// FindRandConversation 查找随机会话
// 根据时间戳和限制数量查找随机会话，用于数据分析
func (c *conversationDatabase) FindRandConversation(ctx context.Context, ts int64, limit int) ([]*relationtb.Conversation, error) {
	return c.conversationDB.FindRandConversation(ctx, ts, limit)
}
