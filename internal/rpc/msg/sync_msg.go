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

// Package msg 消息同步服务包
// 提供消息拉取、同步、搜索等核心功能的RPC服务实现
// 支持增量拉取、全量同步、消息搜索、时间同步等高级特性
// 处理普通消息和通知消息的分类管理，确保消息传输的可靠性和实时性
package msg

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/open-im-server/v3/pkg/msgprocessor"
	"github.com/openimsdk/open-im-server/v3/pkg/util/conversationutil"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/msg"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/utils/datautil"
	"github.com/openimsdk/tools/utils/timeutil"
)

// PullMessageBySeqs 按序列号范围拉取消息
// 支持多会话并发拉取，自动区分普通消息和通知消息
// 实现分片拉取策略，优化大批量消息传输性能
// ctx: 上下文，用于超时控制和链路追踪
// req: 拉取请求，包含用户ID、会话序列号范围、拉取顺序等参数
// 返回: 拉取结果，包含普通消息和通知消息的分类数据
func (m *msgServer) PullMessageBySeqs(ctx context.Context, req *sdkws.PullMessageBySeqsReq) (*sdkws.PullMessageBySeqsResp, error) {
	// 初始化响应结构，分别存储普通消息和通知消息
	resp := &sdkws.PullMessageBySeqsResp{}
	resp.Msgs = make(map[string]*sdkws.PullMsgs)             // 普通消息映射：会话ID -> 消息列表
	resp.NotificationMsgs = make(map[string]*sdkws.PullMsgs) // 通知消息映射：会话ID -> 消息列表

	// 遍历所有请求的序列号范围，支持批量处理多个会话
	for _, seq := range req.SeqRanges {
		// 判断是否为通知类型会话（系统通知、群通知等）
		if !msgprocessor.IsNotification(seq.ConversationID) {
			// 处理普通会话消息（单聊、群聊）
			// 获取用户在该会话中的会话信息，用于权限验证和序列号边界检查
			conversation, err := m.ConversationLocalCache.GetConversation(ctx, req.UserID, seq.ConversationID)
			if err != nil {
				log.ZError(ctx, "GetConversation error", err, "conversationID", seq.ConversationID)
				continue // 获取会话失败，跳过当前会话，继续处理其他会话
			}

			// 按序列号范围拉取消息，支持用户权限过滤和序列号边界控制
			// conversation.MaxSeq: 用户在该会话中的最大可见序列号，用于权限控制
			minSeq, maxSeq, msgs, err := m.MsgDatabase.GetMsgBySeqsRange(ctx, req.UserID, seq.ConversationID,
				seq.Begin, seq.End, seq.Num, conversation.MaxSeq)
			if err != nil {
				log.ZWarn(ctx, "GetMsgBySeqsRange error", err, "conversationID", seq.ConversationID, "seq", seq)
				continue // 拉取失败，跳过当前会话
			}

			// 根据拉取顺序判断是否已到达边界
			var isEnd bool
			switch req.Order {
			case sdkws.PullOrder_PullOrderAsc: // 升序拉取（拉取更新的消息）
				isEnd = maxSeq <= seq.End // 实际最大序列号 <= 请求结束序列号，表示已拉取完毕
			case sdkws.PullOrder_PullOrderDesc: // 降序拉取（拉取更老的消息）
				isEnd = seq.Begin <= minSeq // 请求开始序列号 <= 实际最小序列号，表示已拉取完毕
			}

			// 检查是否有消息返回
			if len(msgs) == 0 {
				log.ZWarn(ctx, "not have msgs", nil, "conversationID", seq.ConversationID, "seq", seq)
				continue // 无消息，跳过
			}

			// 将消息添加到响应的普通消息映射中
			resp.Msgs[seq.ConversationID] = &sdkws.PullMsgs{Msgs: msgs, IsEnd: isEnd}
		} else {
			// 处理通知类型消息（系统通知、入群通知、好友申请通知等）
			// 构建连续的序列号列表，用于精确拉取指定范围内的所有通知
			var seqs []int64
			for i := seq.Begin; i <= seq.End; i++ {
				seqs = append(seqs, i)
			}

			// 按序列号列表拉取通知消息，通知消息通常较少，可以精确拉取
			minSeq, maxSeq, notificationMsgs, err := m.MsgDatabase.GetMsgBySeqs(ctx, req.UserID, seq.ConversationID, seqs)
			if err != nil {
				log.ZWarn(ctx, "GetMsgBySeqs error", err, "conversationID", seq.ConversationID, "seq", seq)
				continue // 拉取失败，跳过当前会话
			}

			// 判断通知消息是否已拉取完毕
			var isEnd bool
			switch req.Order {
			case sdkws.PullOrder_PullOrderAsc: // 升序拉取
				isEnd = maxSeq <= seq.End
			case sdkws.PullOrder_PullOrderDesc: // 降序拉取
				isEnd = seq.Begin <= minSeq
			}

			// 检查是否有通知消息返回
			if len(notificationMsgs) == 0 {
				log.ZWarn(ctx, "not have notificationMsgs", nil, "conversationID", seq.ConversationID, "seq", seq)
				continue // 无通知消息，跳过
			}

			// 将通知消息添加到响应的通知消息映射中
			resp.NotificationMsgs[seq.ConversationID] = &sdkws.PullMsgs{Msgs: notificationMsgs, IsEnd: isEnd}
		}
	}
	return resp, nil
}

// GetSeqMessage 获取指定序列号的消息
// 支持批量获取多个会话的指定序列号消息，自动处理边界检测
// 用于消息补全、离线消息同步等场景
// ctx: 上下文
// req: 获取请求，包含用户ID、会话序列号列表、拉取顺序
// 返回: 消息结果，包含普通消息和通知消息，以及边界信息
func (m *msgServer) GetSeqMessage(ctx context.Context, req *msg.GetSeqMessageReq) (*msg.GetSeqMessageResp, error) {
	// 初始化响应结构
	resp := &msg.GetSeqMessageResp{
		Msgs:             make(map[string]*sdkws.PullMsgs), // 普通消息映射
		NotificationMsgs: make(map[string]*sdkws.PullMsgs), // 通知消息映射
	}

	// 遍历所有请求的会话
	for _, conv := range req.Conversations {
		// 获取指定序列号的消息，同时检测边界状态
		// isEnd: 是否已到达边界，endSeq: 边界序列号
		isEnd, endSeq, msgs, err := m.MsgDatabase.GetMessagesBySeqWithBounds(ctx, req.UserID, conv.ConversationID, conv.Seqs, req.GetOrder())
		if err != nil {
			return nil, err // 发生错误，直接返回
		}

		// 根据会话类型选择存储位置
		var pullMsgs *sdkws.PullMsgs
		if ok := false; conversationutil.IsNotificationConversationID(conv.ConversationID) {
			// 通知类型会话
			pullMsgs, ok = resp.NotificationMsgs[conv.ConversationID]
			if !ok {
				pullMsgs = &sdkws.PullMsgs{}
				resp.NotificationMsgs[conv.ConversationID] = pullMsgs
			}
		} else {
			// 普通会话
			pullMsgs, ok = resp.Msgs[conv.ConversationID]
			if !ok {
				pullMsgs = &sdkws.PullMsgs{}
				resp.Msgs[conv.ConversationID] = pullMsgs
			}
		}

		// 累加消息到现有列表，支持多次调用合并结果
		pullMsgs.Msgs = append(pullMsgs.Msgs, msgs...)
		pullMsgs.IsEnd = isEnd   // 设置边界状态
		pullMsgs.EndSeq = endSeq // 设置边界序列号
	}
	return resp, nil
}

// GetMaxSeq 获取用户所有会话的最大序列号
// 用于客户端初始化、增量同步起点确定等场景
// 自动包含用户的所有会话（普通会话 + 对应的通知会话 + 个人通知会话）
// ctx: 上下文
// req: 请求参数，包含用户ID
// 返回: 最大序列号映射，key为会话ID，value为该会话的最大序列号
func (m *msgServer) GetMaxSeq(ctx context.Context, req *sdkws.GetMaxSeqReq) (*sdkws.GetMaxSeqResp, error) {
	// 权限验证：检查用户是否有权限访问
	if err := authverify.CheckAccessV3(ctx, req.UserID, m.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	// 获取用户参与的所有会话ID列表
	conversationIDs, err := m.ConversationLocalCache.GetConversationIDs(ctx, req.UserID)
	if err != nil {
		return nil, err
	}

	// 为每个普通会话添加对应的通知会话ID
	// 通知会话用于接收群通知、好友申请通知等系统消息
	for _, conversationID := range conversationIDs {
		conversationIDs = append(conversationIDs, conversationutil.GetNotificationConversationIDByConversationID(conversationID))
	}

	// 添加用户个人通知会话（用于接收系统级通知）
	conversationIDs = append(conversationIDs, conversationutil.GetSelfNotificationConversationID(req.UserID))

	log.ZDebug(ctx, "GetMaxSeq", "conversationIDs", conversationIDs)

	// 批量获取所有会话的最大序列号
	maxSeqs, err := m.MsgDatabase.GetMaxSeqs(ctx, conversationIDs)
	if err != nil {
		log.ZWarn(ctx, "GetMaxSeqs error", err, "conversationIDs", conversationIDs, "maxSeqs", maxSeqs)
		return nil, err
	}

	// 过滤掉最大序列号为0的会话，避免客户端拉取空会话
	// 最大序列号为0表示该会话尚未有任何消息
	for conversationID, seq := range maxSeqs {
		if seq == 0 {
			delete(maxSeqs, conversationID)
		}
	}

	// 构建响应
	resp := new(sdkws.GetMaxSeqResp)
	resp.MaxSeqs = maxSeqs
	return resp, nil
}

// SearchMessage 搜索消息
// 提供全文搜索功能，支持跨会话搜索、关键词匹配、分页查询
// 自动补全发送者、接收者、群组信息，提供完整的消息上下文
// ctx: 上下文
// req: 搜索请求，包含搜索关键词、会话过滤、时间范围、分页参数等
// 返回: 搜索结果，包含匹配的消息列表和总数统计
func (m *msgServer) SearchMessage(ctx context.Context, req *msg.SearchMessageReq) (resp *msg.SearchMessageResp, err error) {
	// 执行消息搜索，从数据库中查询匹配的消息
	var chatLogs []*msg.SearchedMsgData
	var total int64
	resp = &msg.SearchMessageResp{}

	if total, chatLogs, err = m.MsgDatabase.SearchMessage(ctx, req); err != nil {
		return nil, err
	}

	// 初始化用户信息和群组信息的映射表
	var (
		sendIDs  []string                            // 需要查询信息的发送者ID列表
		recvIDs  []string                            // 需要查询信息的接收者ID列表
		groupIDs []string                            // 需要查询信息的群组ID列表
		sendMap  = make(map[string]string)           // 发送者ID -> 昵称映射
		recvMap  = make(map[string]string)           // 接收者ID -> 昵称映射
		groupMap = make(map[string]*sdkws.GroupInfo) // 群组ID -> 群组信息映射
	)

	// 收集需要查询的用户ID和群组ID
	for _, chatLog := range chatLogs {
		// 如果发送者昵称为空，需要查询发送者信息
		if chatLog.MsgData.SenderNickname == "" {
			sendIDs = append(sendIDs, chatLog.MsgData.SendID)
		}

		// 根据会话类型收集相关ID
		switch chatLog.MsgData.SessionType {
		case constant.SingleChatType, constant.NotificationChatType:
			// 单聊和通知类型：收集接收者ID
			recvIDs = append(recvIDs, chatLog.MsgData.RecvID)
		case constant.WriteGroupChatType, constant.ReadGroupChatType:
			// 群聊类型：收集群组ID
			groupIDs = append(groupIDs, chatLog.MsgData.GroupID)
		}
	}

	// 批量获取发送者信息
	if len(sendIDs) != 0 {
		sendInfos, err := m.UserLocalCache.GetUsersInfo(ctx, sendIDs)
		if err != nil {
			return nil, err
		}
		// 构建发送者ID到昵称的映射
		for _, sendInfo := range sendInfos {
			sendMap[sendInfo.UserID] = sendInfo.Nickname
		}
	}

	// 批量获取接收者信息
	if len(recvIDs) != 0 {
		recvInfos, err := m.UserLocalCache.GetUsersInfo(ctx, recvIDs)
		if err != nil {
			return nil, err
		}
		// 构建接收者ID到昵称的映射
		for _, recvInfo := range recvInfos {
			recvMap[recvInfo.UserID] = recvInfo.Nickname
		}
	}

	// 批量获取群组信息，包括实时成员数量统计
	if len(groupIDs) != 0 {
		groupInfos, err := m.GroupLocalCache.GetGroupInfos(ctx, groupIDs)
		if err != nil {
			return nil, err
		}
		for _, groupInfo := range groupInfos {
			groupMap[groupInfo.GroupID] = groupInfo
			// 获取群组实际成员数量，确保数据准确性
			memberIDs, err := m.GroupLocalCache.GetGroupMemberIDs(ctx, groupInfo.GroupID)
			if err == nil {
				groupInfo.MemberCount = uint32(len(memberIDs)) // 更新为实际成员数量
			}
		}
	}

	// 构建搜索结果响应，补全用户和群组信息
	for _, chatLog := range chatLogs {
		// 创建聊天日志结构
		pbchatLog := &msg.ChatLog{}
		datautil.CopyStructFields(pbchatLog, chatLog.MsgData)
		pbchatLog.SendTime = chatLog.MsgData.SendTime
		pbchatLog.CreateTime = chatLog.MsgData.CreateTime

		// 补全发送者昵称（如果原始数据中没有）
		if chatLog.MsgData.SenderNickname == "" {
			pbchatLog.SenderNickname = sendMap[chatLog.MsgData.SendID]
		}

		// 根据会话类型补全相关信息
		switch chatLog.MsgData.SessionType {
		case constant.SingleChatType, constant.NotificationChatType:
			// 单聊：补全接收者昵称
			pbchatLog.RecvNickname = recvMap[chatLog.MsgData.RecvID]
		case constant.ReadGroupChatType:
			// 群聊：补全群组详细信息
			groupInfo := groupMap[chatLog.MsgData.GroupID]
			pbchatLog.SenderFaceURL = groupInfo.FaceURL        // 群组头像
			pbchatLog.GroupMemberCount = groupInfo.MemberCount // 实际成员数量
			pbchatLog.RecvID = groupInfo.GroupID               // 接收方为群组ID
			pbchatLog.GroupName = groupInfo.GroupName          // 群组名称
			pbchatLog.GroupOwner = groupInfo.OwnerUserID       // 群主ID
			pbchatLog.GroupType = groupInfo.GroupType          // 群组类型
		}

		// 创建搜索结果条目，包含消息撤回状态
		searchChatLog := &msg.SearchChatLog{ChatLog: pbchatLog, IsRevoked: chatLog.IsRevoked}
		resp.ChatLogs = append(resp.ChatLogs, searchChatLog)
	}

	// 设置搜索结果总数
	resp.ChatLogsNum = int32(total)
	return resp, nil
}

// GetServerTime 获取服务器当前时间
// 用于客户端与服务器时间同步，确保消息时间戳的准确性
// 支持时区处理和毫秒级精度
// ctx: 上下文
// req: 空请求参数
// 返回: 服务器当前时间戳（毫秒）
func (m *msgServer) GetServerTime(ctx context.Context, _ *msg.GetServerTimeReq) (*msg.GetServerTimeResp, error) {
	return &msg.GetServerTimeResp{ServerTime: timeutil.GetCurrentTimestampByMill()}, nil
}

// GetLastMessage 获取会话最后一条消息
// 用于会话列表显示、未读消息预览等场景
// 支持批量获取多个会话的最后一条消息，提高查询效率
// ctx: 上下文
// req: 请求参数，包含用户ID和会话ID列表
// 返回: 会话ID到最后一条消息的映射
func (m *msgServer) GetLastMessage(ctx context.Context, req *msg.GetLastMessageReq) (*msg.GetLastMessageResp, error) {
	// 批量获取指定会话的最后一条消息
	// 自动处理消息删除、撤回状态，返回用户可见的最后一条消息
	msgs, err := m.MsgDatabase.GetLastMessage(ctx, req.ConversationIDs, req.UserID)
	if err != nil {
		return nil, err
	}
	return &msg.GetLastMessageResp{Msgs: msgs}, nil
}
