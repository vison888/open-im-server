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

// Package msg 实现消息相关的RPC服务
// as_read.go 专门处理消息已读状态管理功能
// 包含获取会话已读状态、设置已读位置、标记消息已读等核心功能
package msg

import (
	"context"
	"errors"

	cbapi "github.com/openimsdk/open-im-server/v3/pkg/callbackstruct"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/msg"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/utils/datautil"
	"github.com/redis/go-redis/v9"
)

// GetConversationsHasReadAndMaxSeq 获取多个会话的已读位置和最大序列号
// 这是客户端同步已读状态时的核心接口
// 用于快速获取用户在各个会话中的已读进度和最新消息位置
func (m *msgServer) GetConversationsHasReadAndMaxSeq(ctx context.Context, req *msg.GetConversationsHasReadAndMaxSeqReq) (*msg.GetConversationsHasReadAndMaxSeqResp, error) {
	var conversationIDs []string

	// 如果请求中没有指定会话ID列表，则获取用户的所有会话
	// 这种设计允许客户端选择性同步特定会话或全量同步
	if len(req.ConversationIDs) == 0 {
		var err error
		// 从本地缓存获取用户的所有会话ID列表
		// 本地缓存可以显著提高查询性能，避免频繁查询数据库
		conversationIDs, err = m.ConversationLocalCache.GetConversationIDs(ctx, req.UserID)
		if err != nil {
			return nil, err
		}
	} else {
		// 使用请求中指定的会话ID列表
		conversationIDs = req.ConversationIDs
	}

	// 批量获取用户在各个会话中的已读序列号
	// 已读序列号表示用户已经阅读到的最后一条消息的序列号
	hasReadSeqs, err := m.MsgDatabase.GetHasReadSeqs(ctx, req.UserID, conversationIDs)
	if err != nil {
		return nil, err
	}

	// 批量获取会话信息，主要为了获取会话的MaxSeq字段
	// MaxSeq是会话级别维护的最大序列号，可能与消息表中的实际最大序列号不同
	conversations, err := m.ConversationLocalCache.GetConversations(ctx, req.UserID, conversationIDs)
	if err != nil {
		return nil, err
	}

	// 构建会话ID到最大序列号的映射
	// 优先使用会话表中记录的MaxSeq，这通常是为了性能优化
	// 该逻辑应该是为了兼容老的版本，老版本或者所有会话有一定性能问题，新版本直接用GetMaxSeqsWithTime获取MaxSeq
	conversationMaxSeqMap := make(map[string]int64)
	for _, conversation := range conversations {
		// 只有当MaxSeq不为0时才使用，0表示尚未设置或无效
		if conversation.MaxSeq != 0 {
			conversationMaxSeqMap[conversation.ConversationID] = conversation.MaxSeq
		}
	}

	// 获取消息表中各会话的实际最大序列号和对应时间戳
	// 这是从消息存储中查询的真实最大序列号
	maxSeqs, err := m.MsgDatabase.GetMaxSeqsWithTime(ctx, conversationIDs)
	if err != nil {
		return nil, err
	}

	// 构造响应结果
	resp := &msg.GetConversationsHasReadAndMaxSeqResp{Seqs: make(map[string]*msg.Seqs)}
	for conversationID, maxSeq := range maxSeqs {
		resp.Seqs[conversationID] = &msg.Seqs{
			HasReadSeq: hasReadSeqs[conversationID], // 用户已读到的序列号
			MaxSeq:     maxSeq.Seq,                  // 消息表中的最大序列号
			MaxSeqTime: maxSeq.Time,                 // 最大序列号对应的时间戳
		}
		// 如果会话表中有记录的MaxSeq，则优先使用
		// 这种设计允许系统在特殊情况下手动调整会话的最大序列号
		if v, ok := conversationMaxSeqMap[conversationID]; ok {
			resp.Seqs[conversationID].MaxSeq = v
		}
	}
	return resp, nil
}

// SetConversationHasReadSeq 设置会话的已读序列号
// 这是单聊中标记已读的主要接口，设置用户在某个会话中已读到的位置
func (m *msgServer) SetConversationHasReadSeq(ctx context.Context, req *msg.SetConversationHasReadSeqReq) (*msg.SetConversationHasReadSeqResp, error) {
	// 获取会话的最大序列号，用于验证已读序列号的有效性
	maxSeq, err := m.MsgDatabase.GetMaxSeq(ctx, req.ConversationID)
	if err != nil {
		return nil, err
	}

	// 验证已读序列号不能超过最大序列号
	// 这是重要的数据一致性检查，防止客户端传入无效的序列号
	if req.HasReadSeq > maxSeq {
		return nil, errs.ErrArgs.WrapMsg("hasReadSeq must not be bigger than maxSeq")
	}

	// 更新用户在该会话中的已读序列号到数据库
	// 这个操作通常会更新Redis缓存和持久化存储
	if err := m.MsgDatabase.SetHasReadSeq(ctx, req.UserID, req.ConversationID, req.HasReadSeq); err != nil {
		return nil, err
	}

	// 发送已读回执通知
	// 通知对方用户该消息已被阅读，用于实现"已读"状态显示
	// 参数说明：会话ID、单聊类型、发送者ID、接收者ID、消息序列号列表(nil)、已读序列号
	m.sendMarkAsReadNotification(ctx, req.ConversationID, constant.SingleChatType, req.UserID, req.UserID, nil, req.HasReadSeq)

	return &msg.SetConversationHasReadSeqResp{}, nil
}

// MarkMsgsAsRead 标记指定消息为已读
// 这是更精细的已读标记接口，可以标记特定的消息序列号列表
// 通常用于消息列表中的"标记已读"功能
func (m *msgServer) MarkMsgsAsRead(ctx context.Context, req *msg.MarkMsgsAsReadReq) (*msg.MarkMsgsAsReadResp, error) {
	// 验证请求参数：序列号列表不能为空
	if len(req.Seqs) < 1 {
		return nil, errs.ErrArgs.WrapMsg("seqs must not be empty")
	}

	// 获取会话最大序列号用于验证
	maxSeq, err := m.MsgDatabase.GetMaxSeq(ctx, req.ConversationID)
	if err != nil {
		return nil, err
	}

	// 获取要标记已读的最大序列号（通常是序列号列表的最后一个）
	// 假设客户端传入的序列号列表是有序的
	hasReadSeq := req.Seqs[len(req.Seqs)-1]

	// 验证已读序列号的有效性
	if hasReadSeq > maxSeq {
		return nil, errs.ErrArgs.WrapMsg("hasReadSeq must not be bigger than maxSeq")
	}

	// 获取会话信息，用于确定会话类型和构造通知
	conversation, err := m.ConversationLocalCache.GetConversation(ctx, req.UserID, req.ConversationID)
	if err != nil {
		return nil, err
	}

	// 在数据库中标记指定的消息为已读
	// 这会更新消息记录的已读状态，用于单聊的已读回执功能
	if err := m.MsgDatabase.MarkSingleChatMsgsAsRead(ctx, req.UserID, req.ConversationID, req.Seqs); err != nil {
		return nil, err
	}

	// 获取当前用户在该会话中的已读序列号
	currentHasReadSeq, err := m.MsgDatabase.GetHasReadSeq(ctx, req.UserID, req.ConversationID)
	if err != nil && !errors.Is(err, redis.Nil) {
		// redis.Nil 表示没有找到记录，这是正常情况
		return nil, err
	}

	// 如果新的已读序列号大于当前已读序列号，则更新已读位置
	// 这确保已读位置只能向前推进，不能回退
	if hasReadSeq > currentHasReadSeq {
		err = m.MsgDatabase.SetHasReadSeq(ctx, req.UserID, req.ConversationID, hasReadSeq)
		if err != nil {
			return nil, err
		}
	}

	// 构造单聊消息已读回调请求
	// 用于通知业务系统消息已被阅读，可以触发相应的业务逻辑
	reqCallback := &cbapi.CallbackSingleMsgReadReq{
		ConversationID: conversation.ConversationID,
		UserID:         req.UserID,
		Seqs:           req.Seqs,
		ContentType:    conversation.ConversationType,
	}

	// 执行单聊消息已读的webhook回调
	// 这是异步回调，不会阻塞主流程
	m.webhookAfterSingleMsgRead(ctx, &m.config.WebhooksConfig.AfterSingleMsgRead, reqCallback)

	// 发送已读标记通知给相关用户
	// conversationAndGetRecvID 方法根据会话类型确定接收者ID
	m.sendMarkAsReadNotification(ctx, req.ConversationID, conversation.ConversationType, req.UserID,
		m.conversationAndGetRecvID(conversation, req.UserID), req.Seqs, hasReadSeq)

	return &msg.MarkMsgsAsReadResp{}, nil
}

// MarkConversationAsRead 标记整个会话为已读
// 这是最常用的已读标记接口，用于"一键已读"功能
// 可以将会话中从当前已读位置到指定位置的所有消息标记为已读
func (m *msgServer) MarkConversationAsRead(ctx context.Context, req *msg.MarkConversationAsReadReq) (*msg.MarkConversationAsReadResp, error) {
	// 获取会话信息，确定会话类型和相关参数
	conversation, err := m.ConversationLocalCache.GetConversation(ctx, req.UserID, req.ConversationID)
	if err != nil {
		return nil, err
	}

	// 获取用户当前在该会话中的已读序列号
	hasReadSeq, err := m.MsgDatabase.GetHasReadSeq(ctx, req.UserID, req.ConversationID)
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}

	// 用于存储需要标记为已读的消息序列号列表
	var seqs []int64

	log.ZDebug(ctx, "MarkConversationAsRead", "hasReadSeq", hasReadSeq, "req.HasReadSeq", req.HasReadSeq)

	// 根据会话类型进行不同的处理逻辑
	if conversation.ConversationType == constant.SingleChatType {
		// 单聊处理逻辑：需要标记具体的消息为已读

		// 那不是直接这个逻辑，连req.Seqs都不要了
		// 生成从当前已读位置+1到新已读位置的所有序列号
		// 这确保了连续性，不会遗漏中间的消息
		for i := hasReadSeq + 1; i <= req.HasReadSeq; i++ {
			seqs = append(seqs, i)
		}

		// 避免客户端因为消息乱序导致的遗漏
		// 将请求中指定的序列号也加入到待标记列表中
		// datautil.Contain 检查元素是否已存在，避免重复
		for _, val := range req.Seqs {
			if !datautil.Contain(val, seqs...) {
				seqs = append(seqs, val)
			}
		}

		// 如果有需要标记的消息，则批量标记为已读
		if len(seqs) > 0 {
			log.ZDebug(ctx, "MarkConversationAsRead", "seqs", seqs, "conversationID", req.ConversationID)
			if err = m.MsgDatabase.MarkSingleChatMsgsAsRead(ctx, req.UserID, req.ConversationID, seqs); err != nil {
				return nil, err
			}
		}

		// 如果新的已读序列号大于当前已读序列号，则更新已读位置
		if req.HasReadSeq > hasReadSeq {
			err = m.MsgDatabase.SetHasReadSeq(ctx, req.UserID, req.ConversationID, req.HasReadSeq)
			if err != nil {
				return nil, err
			}
			hasReadSeq = req.HasReadSeq
		}

		// 发送单聊已读通知
		m.sendMarkAsReadNotification(ctx, req.ConversationID, conversation.ConversationType, req.UserID,
			m.conversationAndGetRecvID(conversation, req.UserID), seqs, hasReadSeq)

	} else if conversation.ConversationType == constant.ReadGroupChatType ||
		conversation.ConversationType == constant.NotificationChatType {
		// 群聊和通知类会话处理逻辑：只需要更新已读位置，不需要标记具体消息

		// 群聊中通常不需要标记每条消息的已读状态，只需要记录用户的已读位置
		if req.HasReadSeq > hasReadSeq {
			err = m.MsgDatabase.SetHasReadSeq(ctx, req.UserID, req.ConversationID, req.HasReadSeq)
			if err != nil {
				return nil, err
			}
			hasReadSeq = req.HasReadSeq
		}

		// 群聊的已读通知发送给自己，不需要通知其他群成员
		// 这是因为群聊中个人的已读状态通常是私密的
		m.sendMarkAsReadNotification(ctx, req.ConversationID, constant.SingleChatType, req.UserID,
			req.UserID, seqs, hasReadSeq)
	}

	// 根据会话类型执行相应的webhook回调
	if conversation.ConversationType == constant.SingleChatType {
		// 单聊消息已读回调
		reqCall := &cbapi.CallbackSingleMsgReadReq{
			ConversationID: conversation.ConversationID,
			UserID:         conversation.OwnerUserID, // 会话拥有者，即当前操作的用户
			Seqs:           req.Seqs,
			ContentType:    conversation.ConversationType,
		}
		m.webhookAfterSingleMsgRead(ctx, &m.config.WebhooksConfig.AfterSingleMsgRead, reqCall)

	} else if conversation.ConversationType == constant.ReadGroupChatType {
		// 群聊消息已读回调
		reqCall := &cbapi.CallbackGroupMsgReadReq{
			SendID:       conversation.OwnerUserID, // 会话拥有者
			ReceiveID:    req.UserID,               // 执行已读操作的用户
			UnreadMsgNum: req.HasReadSeq,           // 已读到的序列号（这里字段名可能有些误导性）
			ContentType:  int64(conversation.ConversationType),
		}
		m.webhookAfterGroupMsgRead(ctx, &m.config.WebhooksConfig.AfterGroupMsgRead, reqCall)
	}

	return &msg.MarkConversationAsReadResp{}, nil
}

// sendMarkAsReadNotification 发送已读标记通知
// 这是一个内部辅助方法，用于发送已读回执通知给相关用户
// 通知内容包括已读用户、会话ID、已读消息列表和已读序列号
func (m *msgServer) sendMarkAsReadNotification(ctx context.Context, conversationID string, sessionType int32, sendID, recvID string, seqs []int64, hasReadSeq int64) {
	// 构造已读标记通知的消息体
	tips := &sdkws.MarkAsReadTips{
		MarkAsReadUserID: sendID,         // 执行已读操作的用户ID
		ConversationID:   conversationID, // 会话ID
		Seqs:             seqs,           // 被标记为已读的消息序列号列表
		HasReadSeq:       hasReadSeq,     // 用户已读到的最新序列号
	}

	// 使用通知发送器发送已读回执通知
	// 这会通过WebSocket或其他实时通道通知相关用户
	// constant.HasReadReceipt 是已读回执的消息类型
	m.notificationSender.NotificationWithSessionType(ctx, sendID, recvID, constant.HasReadReceipt, sessionType, tips)
}
