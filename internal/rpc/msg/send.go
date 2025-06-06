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

package msg

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/common/prommetrics"
	"github.com/openimsdk/open-im-server/v3/pkg/msgprocessor"
	"github.com/openimsdk/open-im-server/v3/pkg/util/conversationutil"
	"github.com/openimsdk/protocol/constant"
	pbconversation "github.com/openimsdk/protocol/conversation"
	pbmsg "github.com/openimsdk/protocol/msg"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/protocol/wrapperspb"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
	"github.com/openimsdk/tools/utils/datautil"
)

// SendMsg 发送消息的核心入口方法
// 这是整个消息系统的核心方法，负责处理所有类型的消息发送请求
// 根据不同的会话类型（单聊、群聊、通知）分发到对应的处理流程
func (m *msgServer) SendMsg(ctx context.Context, req *pbmsg.SendMsgReq) (*pbmsg.SendMsgResp, error) {
	if req.MsgData != nil {
		// 封装消息数据：生成服务器消息ID、设置发送时间、处理消息选项等
		m.encapsulateMsgData(req.MsgData)

		// 根据会话类型分发到不同的处理流程
		switch req.MsgData.SessionType {
		case constant.SingleChatType:
			// 单聊消息处理：需要验证好友关系、黑名单等
			return m.sendMsgSingleChat(ctx, req)
		case constant.NotificationChatType:
			// 通知消息处理：系统通知、业务通知等
			return m.sendMsgNotification(ctx, req)
		case constant.ReadGroupChatType:
			// 群聊消息处理：需要验证群成员身份、禁言状态等
			return m.sendMsgGroupChat(ctx, req)
		default:
			return nil, errs.ErrArgs.WrapMsg("unknown sessionType")
		}
	}
	return nil, errs.ErrArgs.WrapMsg("msgData is nil")
}

// sendMsgGroupChat 处理群聊消息发送
// 群聊消息的处理流程比单聊更复杂，需要考虑群组状态、成员权限、禁言等因素
func (m *msgServer) sendMsgGroupChat(ctx context.Context, req *pbmsg.SendMsgReq) (resp *pbmsg.SendMsgResp, err error) {
	// 1. 消息验证：检查发送者是否为群成员、是否被禁言、群组是否有效等
	if err = m.messageVerification(ctx, req); err != nil {
		// 记录群聊消息处理失败指标
		prommetrics.GroupChatMsgProcessFailedCounter.Inc()
		return nil, err
	}

	// 2. Webhook前置处理：群消息发送前回调
	// 允许第三方系统在消息发送前进行拦截、记录或修改
	if err = m.webhookBeforeSendGroupMsg(ctx, &m.config.WebhooksConfig.BeforeSendGroupMsg, req); err != nil {
		return nil, err
	}

	// 3. 消息内容修改回调：允许第三方系统修改消息内容
	if err := m.webhookBeforeMsgModify(ctx, &m.config.WebhooksConfig.BeforeMsgModify, req); err != nil {
		return nil, err
	}

	// 4. 将消息投递到消息队列(Kafka)
	// 使用群组ID生成唯一的会话键，确保同一群组的消息顺序性
	err = m.MsgDatabase.MsgToMQ(ctx, conversationutil.GenConversationUniqueKeyForGroup(req.MsgData.GroupID), req.MsgData)
	if err != nil {
		return nil, err
	}

	// 5. 特殊处理@消息：异步设置会话的@信息
	if req.MsgData.ContentType == constant.AtText {
		go m.setConversationAtInfo(ctx, req.MsgData)
	}

	// 6. Webhook后置处理：群消息发送后回调（异步）
	m.webhookAfterSendGroupMsg(ctx, &m.config.WebhooksConfig.AfterSendGroupMsg, req)

	// 7. 记录群聊消息处理成功指标
	prommetrics.GroupChatMsgProcessSuccessCounter.Inc()

	// 8. 构造响应
	resp = &pbmsg.SendMsgResp{}
	resp.SendTime = req.MsgData.SendTime
	resp.ServerMsgID = req.MsgData.ServerMsgID
	resp.ClientMsgID = req.MsgData.ClientMsgID
	return resp, nil
}

// setConversationAtInfo 设置会话的@信息
// 当群聊中有@消息时，需要为被@的用户设置特殊的会话状态
// 这个方法异步执行，不影响消息发送的主流程
func (m *msgServer) setConversationAtInfo(nctx context.Context, msg *sdkws.MsgData) {

	log.ZDebug(nctx, "setConversationAtInfo", "msg", msg)

	// 异常恢复机制：防止panic影响主流程
	defer func() {
		if r := recover(); r != nil {
			log.ZPanic(nctx, "setConversationAtInfo Panic", errs.ErrPanic(r))
		}
	}()

	// 创建新的上下文，避免与原请求的超时影响
	ctx := mcontext.NewCtx("@@@" + mcontext.GetOperationID(nctx))

	var atUserID []string

	// 构造会话请求信息
	conversation := &pbconversation.ConversationReq{
		ConversationID:   msgprocessor.GetConversationIDByMsg(msg),
		ConversationType: msg.SessionType,
		GroupID:          msg.GroupID,
	}

	// 获取群组所有成员ID列表
	memberUserIDList, err := m.GroupLocalCache.GetGroupMemberIDs(ctx, msg.GroupID)
	if err != nil {
		log.ZWarn(ctx, "GetGroupMemberIDs", err)
		return
	}

	// 检查是否@了所有人
	tagAll := datautil.Contain(constant.AtAllString, msg.AtUserIDList...)
	if tagAll {
		// 处理@所有人的情况

		// 从成员列表中移除发送者（发送者不需要收到@通知）
		memberUserIDList = datautil.DeleteElems(memberUserIDList, msg.SendID)

		// 从@列表中筛选出除@所有人外的特定用户
		atUserID = datautil.Single([]string{constant.AtAllString}, msg.AtUserIDList)

		if len(atUserID) == 0 {
			// 只@了所有人，没有@特定用户
			conversation.GroupAtType = &wrapperspb.Int32Value{Value: constant.AtAll}
		} else {
			// 既@了所有人，又@了特定用户
			conversation.GroupAtType = &wrapperspb.Int32Value{Value: constant.AtAllAtMe}

			// 筛选出有效的@用户（必须是群成员）
			atUserID = datautil.SliceIntersectFuncs(atUserID, memberUserIDList, func(a string) string { return a }, func(b string) string {
				return b
			})

			// 为特定@用户设置会话状态
			if err := m.conversationClient.SetConversations(ctx, atUserID, conversation); err != nil {
				log.ZWarn(ctx, "SetConversations", err, "userID", atUserID, "conversation", conversation)
			}

			// 从成员列表中移除已处理的特定@用户
			memberUserIDList = datautil.Single(atUserID, memberUserIDList)
		}

		// 为所有其他成员设置@所有人状态
		conversation.GroupAtType = &wrapperspb.Int32Value{Value: constant.AtAll}
		if err := m.conversationClient.SetConversations(ctx, memberUserIDList, conversation); err != nil {
			log.ZWarn(ctx, "SetConversations", err, "userID", memberUserIDList, "conversation", conversation)
		}

		return
	}

	// 处理@特定用户的情况
	// 筛选出有效的@用户（必须是群成员）
	atUserID = datautil.SliceIntersectFuncs(msg.AtUserIDList, memberUserIDList, func(a string) string { return a }, func(b string) string {
		return b
	})

	// 设置@我状态
	conversation.GroupAtType = &wrapperspb.Int32Value{Value: constant.AtMe}

	// 为被@用户设置会话状态
	if err := m.conversationClient.SetConversations(ctx, atUserID, conversation); err != nil {
		log.ZWarn(ctx, "SetConversations", err, atUserID, conversation)
	}
}

// sendMsgNotification 处理通知消息发送
// 通知消息通常是系统消息，不需要复杂的验证流程
func (m *msgServer) sendMsgNotification(ctx context.Context, req *pbmsg.SendMsgReq) (resp *pbmsg.SendMsgResp, err error) {
	// 直接将通知消息投递到消息队列
	// 通知消息使用发送者和接收者生成会话键
	if err := m.MsgDatabase.MsgToMQ(ctx, conversationutil.GenConversationUniqueKeyForSingle(req.MsgData.SendID, req.MsgData.RecvID), req.MsgData); err != nil {
		return nil, err
	}

	// 构造响应
	resp = &pbmsg.SendMsgResp{
		ServerMsgID: req.MsgData.ServerMsgID,
		ClientMsgID: req.MsgData.ClientMsgID,
		SendTime:    req.MsgData.SendTime,
	}
	return resp, nil
}

// sendMsgSingleChat 处理单聊消息发送
// 单聊消息需要考虑好友关系、黑名单、消息接收设置等因素
func (m *msgServer) sendMsgSingleChat(ctx context.Context, req *pbmsg.SendMsgReq) (resp *pbmsg.SendMsgResp, err error) {
	// 1. 消息验证：检查好友关系、黑名单等
	if err := m.messageVerification(ctx, req); err != nil {
		return nil, err
	}

	isSend := true
	isNotification := msgprocessor.IsNotificationByMsg(req.MsgData)

	// 2. 如果不是通知消息，需要检查接收方的消息接收设置
	if !isNotification {
		isSend, err = m.modifyMessageByUserMessageReceiveOpt(
			ctx,
			req.MsgData.RecvID, // 接收者ID
			conversationutil.GenConversationIDForSingle(req.MsgData.SendID, req.MsgData.RecvID), // 会话ID
			constant.SingleChatType, // 会话类型
			req,
		)
		if err != nil {
			return nil, err
		}
	}

	// 3. 根据接收设置决定是否发送消息
	if !isSend {
		// 接收方设置了不接收消息
		prommetrics.SingleChatMsgProcessFailedCounter.Inc()
		return nil, nil
	} else {
		// 4. Webhook消息内容修改回调
		if err := m.webhookBeforeMsgModify(ctx, &m.config.WebhooksConfig.BeforeMsgModify, req); err != nil {
			return nil, err
		}

		// 5. 将消息投递到消息队列
		if err := m.MsgDatabase.MsgToMQ(ctx, conversationutil.GenConversationUniqueKeyForSingle(req.MsgData.SendID, req.MsgData.RecvID), req.MsgData); err != nil {
			prommetrics.SingleChatMsgProcessFailedCounter.Inc()
			return nil, err
		}

		// 6. Webhook后置处理：单聊消息发送后回调
		m.webhookAfterSendSingleMsg(ctx, &m.config.WebhooksConfig.AfterSendSingleMsg, req)

		// 7. 记录单聊消息处理成功指标
		prommetrics.SingleChatMsgProcessSuccessCounter.Inc()

		// 8. 构造响应
		return &pbmsg.SendMsgResp{
			ServerMsgID: req.MsgData.ServerMsgID,
			ClientMsgID: req.MsgData.ClientMsgID,
			SendTime:    req.MsgData.SendTime,
		}, nil
	}
}
