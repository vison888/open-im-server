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

// Package msg 消息服务包
// delete.go 专门处理消息删除相关功能
// 包含清空会话消息、删除指定消息、物理删除等多种删除策略
// 支持逻辑删除和物理删除两种模式，满足不同的业务需求
package msg

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/conversation"
	"github.com/openimsdk/protocol/msg"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/utils/timeutil"
)

// getMinSeqs 根据最大序列号映射生成最小序列号映射
// 将最大序列号+1作为新的最小序列号，用于逻辑删除消息
// 这种方式可以"隐藏"指定序列号之前的所有消息，而不需要物理删除
func (m *msgServer) getMinSeqs(maxSeqs map[string]int64) map[string]int64 {
	minSeqs := make(map[string]int64)
	for k, v := range maxSeqs {
		minSeqs[k] = v + 1 // 最小序列号设置为最大序列号+1
	}
	return minSeqs
}

// validateDeleteSyncOpt 验证并解析删除同步选项
// 返回是否同步给自己和是否同步给其他人的标志
// 这个设计允许灵活控制删除操作的同步范围
func (m *msgServer) validateDeleteSyncOpt(opt *msg.DeleteSyncOpt) (isSyncSelf, isSyncOther bool) {
	if opt == nil {
		// 如果没有提供选项，默认都不同步
		return
	}
	return opt.IsSyncSelf, opt.IsSyncOther
}

// ClearConversationsMsg 清空指定会话的消息
// 这是批量清理会话消息的主要接口
// 支持逻辑删除（对用户隐藏）和物理删除两种模式
func (m *msgServer) ClearConversationsMsg(ctx context.Context, req *msg.ClearConversationsMsgReq) (*msg.ClearConversationsMsgResp, error) {
	// 权限验证：检查是否为会话所有者或系统管理员
	// CheckAccessV3允许用户操作自己的数据，或管理员操作任意数据
	if err := authverify.CheckAccessV3(ctx, req.UserID, m.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	// 执行会话清理操作
	if err := m.clearConversation(ctx, req.ConversationIDs, req.UserID, req.DeleteSyncOpt); err != nil {
		return nil, err
	}

	return &msg.ClearConversationsMsgResp{}, nil
}

// UserClearAllMsg 清空用户的所有消息
// 这是用户级别的消息清理功能，会清空用户参与的所有会话
// 通常用于用户注销、数据清理等场景
func (m *msgServer) UserClearAllMsg(ctx context.Context, req *msg.UserClearAllMsgReq) (*msg.UserClearAllMsgResp, error) {
	// 权限验证
	if err := authverify.CheckAccessV3(ctx, req.UserID, m.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	// 获取用户参与的所有会话ID列表
	// 这将包括单聊、群聊等各种类型的会话
	conversationIDs, err := m.ConversationLocalCache.GetConversationIDs(ctx, req.UserID)
	if err != nil {
		return nil, err
	}

	// 批量清理所有会话
	if err := m.clearConversation(ctx, conversationIDs, req.UserID, req.DeleteSyncOpt); err != nil {
		return nil, err
	}

	return &msg.UserClearAllMsgResp{}, nil
}

// DeleteMsgs 删除指定的消息
// 支持按序列号精确删除消息，可以选择逻辑删除或物理删除
// 是最灵活的消息删除接口
func (m *msgServer) DeleteMsgs(ctx context.Context, req *msg.DeleteMsgsReq) (*msg.DeleteMsgsResp, error) {
	// 权限验证
	if err := authverify.CheckAccessV3(ctx, req.UserID, m.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	// 解析同步选项
	isSyncSelf, isSyncOther := m.validateDeleteSyncOpt(req.DeleteSyncOpt)

	if isSyncOther {
		// 物理删除模式：从数据库中彻底删除消息
		// 这种模式会影响所有用户，消息对所有人都不可见
		if err := m.MsgDatabase.DeleteMsgsPhysicalBySeqs(ctx, req.ConversationID, req.Seqs); err != nil {
			return nil, err
		}

		// 获取会话信息用于发送通知
		conv, err := m.conversationClient.GetConversationsByConversationID(ctx, req.ConversationID)
		if err != nil {
			return nil, err
		}

		// 构建删除消息通知
		tips := &sdkws.DeleteMsgsTips{
			UserID:         req.UserID,
			ConversationID: req.ConversationID,
			Seqs:           req.Seqs,
		}

		// 发送删除通知给会话中的其他用户
		// conversationAndGetRecvID根据会话类型确定接收者
		m.notificationSender.NotificationWithSessionType(ctx, req.UserID,
			m.conversationAndGetRecvID(conv, req.UserID),
			constant.DeleteMsgsNotification, conv.ConversationType, tips)

	} else {
		// 逻辑删除模式：只对指定用户隐藏消息
		// 消息在数据库中仍然存在，但对该用户不可见
		if err := m.MsgDatabase.DeleteUserMsgsBySeqs(ctx, req.UserID, req.ConversationID, req.Seqs); err != nil {
			return nil, err
		}

		// 如果需要同步给自己的其他设备
		if isSyncSelf {
			tips := &sdkws.DeleteMsgsTips{
				UserID:         req.UserID,
				ConversationID: req.ConversationID,
				Seqs:           req.Seqs,
			}

			// 发送删除通知给自己（用于多设备同步）
			m.notificationSender.NotificationWithSessionType(ctx, req.UserID, req.UserID,
				constant.DeleteMsgsNotification, constant.SingleChatType, tips)
		}
	}

	return &msg.DeleteMsgsResp{}, nil
}

// DeleteMsgPhysicalBySeq 按序列号物理删除消息
// 这是一个简化的物理删除接口，直接删除指定序列号的消息
// 主要用于内部调用和管理员操作
func (m *msgServer) DeleteMsgPhysicalBySeq(ctx context.Context, req *msg.DeleteMsgPhysicalBySeqReq) (*msg.DeleteMsgPhysicalBySeqResp, error) {
	// 直接执行物理删除，不做额外的权限检查
	// 调用方需要确保有适当的权限
	err := m.MsgDatabase.DeleteMsgsPhysicalBySeqs(ctx, req.ConversationID, req.Seqs)
	if err != nil {
		return nil, err
	}

	return &msg.DeleteMsgPhysicalBySeqResp{}, nil
}

// DeleteMsgPhysical 根据时间戳物理删除消息
// 删除指定时间之前的所有消息，用于定期清理历史数据
// 这是管理员专用的批量清理功能
func (m *msgServer) DeleteMsgPhysical(ctx context.Context, req *msg.DeleteMsgPhysicalReq) (*msg.DeleteMsgPhysicalResp, error) {
	// 严格的管理员权限检查
	if err := authverify.CheckAdmin(ctx, m.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	// 计算需要删除的消息时间范围
	// remainTime = 当前时间 - 请求的时间戳 = 消息的"年龄"
	remainTime := timeutil.GetCurrentTimestampBySecond() - req.Timestamp

	// 调用硬删除接口，删除指定时间之前的消息
	// 这里设置了较大的limit(9999)以便批量处理
	if _, err := m.DestructMsgs(ctx, &msg.DestructMsgsReq{
		Timestamp: remainTime,
		Limit:     9999,
	}); err != nil {
		return nil, err
	}

	return &msg.DeleteMsgPhysicalResp{}, nil
}

// clearConversation 清理会话的核心实现
// 这是一个内部方法，实现了会话消息清理的具体逻辑
// 支持逻辑删除和物理删除两种模式
func (m *msgServer) clearConversation(ctx context.Context, conversationIDs []string, userID string, deleteSyncOpt *msg.DeleteSyncOpt) error {
	// 批量获取会话信息，确保会话存在
	conversations, err := m.conversationClient.GetConversationsByConversationIDs(ctx, conversationIDs)
	if err != nil {
		return err
	}

	// 过滤出实际存在的会话
	var existConversations []*conversation.Conversation
	var existConversationIDs []string
	for _, conversation := range conversations {
		existConversations = append(existConversations, conversation)
		existConversationIDs = append(existConversationIDs, conversation.ConversationID)
	}

	log.ZDebug(ctx, "ClearConversationsMsg", "existConversationIDs", existConversationIDs)

	// 获取每个会话的最大序列号
	// 这将用于确定清理的范围
	maxSeqs, err := m.MsgDatabase.GetMaxSeqs(ctx, existConversationIDs)
	if err != nil {
		return err
	}

	// 解析同步选项
	isSyncSelf, isSyncOther := m.validateDeleteSyncOpt(deleteSyncOpt)

	if !isSyncOther {
		// 逻辑删除模式：只对指定用户隐藏消息

		// 计算新的最小序列号（最大序列号+1）
		// 这样所有现有消息都会被"隐藏"
		setSeqs := m.getMinSeqs(maxSeqs)

		// 为用户设置会话的最小序列号
		// 这会让用户看不到小于最小序列号的所有消息
		if err := m.MsgDatabase.SetUserConversationsMinSeqs(ctx, userID, setSeqs); err != nil {
			return err
		}

		// 更新会话服务中的最小序列号
		// 这确保会话列表中也不会显示被清理的消息
		ownerUserIDs := []string{userID}
		for conversationID, seq := range setSeqs {
			if err := m.conversationClient.SetConversationMinSeq(ctx, conversationID, ownerUserIDs, seq); err != nil {
				return err
			}
		}

		// 如果需要同步给自己的其他设备
		if isSyncSelf {
			tips := &sdkws.ClearConversationTips{
				UserID:          userID,
				ConversationIDs: existConversationIDs,
			}

			// 发送清理通知给自己（多设备同步）
			m.notificationSender.NotificationWithSessionType(ctx, userID, userID,
				constant.ClearConversationNotification, constant.SingleChatType, tips)
		}

	} else {
		// 物理删除模式：彻底删除消息

		// 全局设置最小序列号，影响所有用户
		if err := m.MsgDatabase.SetMinSeqs(ctx, m.getMinSeqs(maxSeqs)); err != nil {
			return err
		}

		// 向每个会话的参与者发送清理通知
		for _, conversation := range existConversations {
			tips := &sdkws.ClearConversationTips{
				UserID:          userID,
				ConversationIDs: []string{conversation.ConversationID},
			}

			// 发送通知给会话中的其他用户
			m.notificationSender.NotificationWithSessionType(ctx, userID,
				m.conversationAndGetRecvID(conversation, userID),
				constant.ClearConversationNotification, conversation.ConversationType, tips)
		}
	}

	// 更新用户的已读序列号为最大序列号
	// 这样可以避免在清理后出现未读消息计数异常
	// 因为所有消息都被清理了，用户的已读位置应该设置为最新
	if err := m.MsgDatabase.UserSetHasReadSeqs(ctx, userID, maxSeqs); err != nil {
		return err
	}

	return nil
}
