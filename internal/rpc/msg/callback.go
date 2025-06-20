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
// callback.go 专门处理消息相关的webhook回调功能
// 提供消息发送前后、消息修改、已读回执等各种业务场景的回调通知
// 支持同步和异步两种回调模式，满足不同业务需求
package msg

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/common/webhook"

	cbapi "github.com/openimsdk/open-im-server/v3/pkg/callbackstruct"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/protocol/constant"
	pbchat "github.com/openimsdk/protocol/msg"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/mcontext"
	"github.com/openimsdk/tools/utils/datautil"
	"google.golang.org/protobuf/proto"
)

// toCommonCallback 将消息请求转换为通用回调请求格式
// 这是所有回调请求的基础转换函数，提取消息的公共字段
// 用于构建统一的回调数据结构，便于业务系统处理
func toCommonCallback(ctx context.Context, msg *pbchat.SendMsgReq, command string) cbapi.CommonCallbackReq {
	return cbapi.CommonCallbackReq{
		SendID:           msg.MsgData.SendID,           // 发送者用户ID
		ServerMsgID:      msg.MsgData.ServerMsgID,      // 服务器消息ID
		CallbackCommand:  command,                      // 回调命令类型
		ClientMsgID:      msg.MsgData.ClientMsgID,      // 客户端消息ID
		OperationID:      mcontext.GetOperationID(ctx), // 操作ID，用于链路追踪
		SenderPlatformID: msg.MsgData.SenderPlatformID, // 发送者平台ID
		SenderNickname:   msg.MsgData.SenderNickname,   // 发送者昵称
		SessionType:      msg.MsgData.SessionType,      // 会话类型
		MsgFrom:          msg.MsgData.MsgFrom,          // 消息来源
		ContentType:      msg.MsgData.ContentType,      // 消息内容类型
		Status:           msg.MsgData.Status,           // 消息状态
		SendTime:         msg.MsgData.SendTime,         // 发送时间
		CreateTime:       msg.MsgData.CreateTime,       // 创建时间
		AtUserIDList:     msg.MsgData.AtUserIDList,     // @用户列表
		SenderFaceURL:    msg.MsgData.SenderFaceURL,    // 发送者头像URL
		Content:          GetContent(msg.MsgData),      // 消息内容（经过特殊处理）
		Seq:              uint32(msg.MsgData.Seq),      // 消息序列号
		Ex:               msg.MsgData.Ex,               // 扩展字段
	}
}

// GetContent 获取消息内容
// 对不同类型的消息内容进行特殊处理，确保回调系统能正确解析消息内容
func GetContent(msg *sdkws.MsgData) string {
	// 判断是否为通知类消息（系统通知、群通知等）
	if msg.ContentType >= constant.NotificationBegin && msg.ContentType <= constant.NotificationEnd {
		// 通知类消息的内容是protobuf格式，需要解析出JSON详情
		var tips sdkws.TipsComm
		_ = proto.Unmarshal(msg.Content, &tips)
		content := tips.JsonDetail // 提取JSON格式的详细信息
		return content
	} else {
		// 普通消息直接返回字节内容的字符串形式
		return string(msg.Content)
	}
}

// webhookBeforeSendSingleMsg 单聊消息发送前的webhook回调
// 这是同步回调，在消息真正发送前执行，可以用于消息过滤、内容检查等
// 如果回调返回错误，消息发送将被阻止
func (m *msgServer) webhookBeforeSendSingleMsg(ctx context.Context, before *config.BeforeConfig, msg *pbchat.SendMsgReq) error {
	return webhook.WithCondition(ctx, before, func(ctx context.Context) error {
		// 排除正在输入消息，这类消息不需要回调处理
		if msg.MsgData.ContentType == constant.Typing {
			return nil
		}

		// 根据配置过滤消息，不满足条件的消息跳过回调
		if !filterBeforeMsg(msg, before) {
			return nil
		}

		// 构建单聊消息发送前回调请求
		cbReq := &cbapi.CallbackBeforeSendSingleMsgReq{
			CommonCallbackReq: toCommonCallback(ctx, msg, cbapi.CallbackBeforeSendSingleMsgCommand),
			RecvID:            msg.MsgData.RecvID, // 接收者用户ID
		}
		resp := &cbapi.CallbackBeforeSendSingleMsgResp{}

		// 执行同步回调，等待业务系统响应
		// 业务系统可以在此阶段拒绝消息发送或修改消息内容
		if err := m.webhookClient.SyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, resp, before); err != nil {
			return err
		}

		return nil
	})
}

// webhookAfterSendSingleMsg 单聊消息发送后的webhook回调
// 这是异步回调，在消息成功发送后执行，用于通知业务系统消息已发送
// 不会影响消息发送流程，适合做日志记录、统计分析等
func (m *msgServer) webhookAfterSendSingleMsg(ctx context.Context, after *config.AfterConfig, msg *pbchat.SendMsgReq) {
	// 排除正在输入消息
	if msg.MsgData.ContentType == constant.Typing {
		return
	}

	// 过滤不需要回调的消息
	if !filterAfterMsg(msg, after) {
		return
	}

	// 构建单聊消息发送后回调请求
	cbReq := &cbapi.CallbackAfterSendSingleMsgReq{
		CommonCallbackReq: toCommonCallback(ctx, msg, cbapi.CallbackAfterSendSingleMsgCommand),
		RecvID:            msg.MsgData.RecvID,
	}

	// 执行异步回调，不等待响应结果
	// 这样设计可以避免回调失败影响正常的消息发送流程
	m.webhookClient.AsyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, &cbapi.CallbackAfterSendSingleMsgResp{}, after)
}

// webhookBeforeSendGroupMsg 群聊消息发送前的webhook回调
// 功能类似单聊发送前回调，但针对群聊场景进行了优化
// 可以用于群消息的权限检查、内容审核等
func (m *msgServer) webhookBeforeSendGroupMsg(ctx context.Context, before *config.BeforeConfig, msg *pbchat.SendMsgReq) error {
	return webhook.WithCondition(ctx, before, func(ctx context.Context) error {
		// 过滤不需要回调的消息
		if !filterBeforeMsg(msg, before) {
			return nil
		}

		// 排除正在输入消息
		if msg.MsgData.ContentType == constant.Typing {
			return nil
		}

		// 构建群聊消息发送前回调请求
		cbReq := &cbapi.CallbackBeforeSendGroupMsgReq{
			CommonCallbackReq: toCommonCallback(ctx, msg, cbapi.CallbackBeforeSendGroupMsgCommand),
			GroupID:           msg.MsgData.GroupID, // 群组ID
		}
		resp := &cbapi.CallbackBeforeSendGroupMsgResp{}

		// 执行同步回调
		if err := m.webhookClient.SyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, resp, before); err != nil {
			return err
		}
		return nil
	})
}

// webhookAfterSendGroupMsg 群聊消息发送后的webhook回调
// 异步通知业务系统群消息已发送，用于统计、分析等用途
func (m *msgServer) webhookAfterSendGroupMsg(ctx context.Context, after *config.AfterConfig, msg *pbchat.SendMsgReq) {
	// 排除正在输入消息
	if msg.MsgData.ContentType == constant.Typing {
		return
	}

	// 过滤不需要回调的消息
	if !filterAfterMsg(msg, after) {
		return
	}

	// 构建群聊消息发送后回调请求
	cbReq := &cbapi.CallbackAfterSendGroupMsgReq{
		CommonCallbackReq: toCommonCallback(ctx, msg, cbapi.CallbackAfterSendGroupMsgCommand),
		GroupID:           msg.MsgData.GroupID,
	}

	// 执行异步回调
	m.webhookClient.AsyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, &cbapi.CallbackAfterSendGroupMsgResp{}, after)
}

// webhookBeforeMsgModify 消息修改前的webhook回调
// 这是一个特殊的回调，允许业务系统在消息发送前修改消息内容
// 主要用于内容过滤、敏感词替换、格式转换等场景
func (m *msgServer) webhookBeforeMsgModify(ctx context.Context, before *config.BeforeConfig, msg *pbchat.SendMsgReq) error {
	return webhook.WithCondition(ctx, before, func(ctx context.Context) error {
		// 只处理文本消息，其他类型消息不支持内容修改
		if msg.MsgData.ContentType != constant.Text {
			return nil
		}

		// 过滤不需要回调的消息
		if !filterBeforeMsg(msg, before) {
			return nil
		}

		// 构建消息修改回调请求
		cbReq := &cbapi.CallbackMsgModifyCommandReq{
			CommonCallbackReq: toCommonCallback(ctx, msg, cbapi.CallbackBeforeMsgModifyCommand),
		}
		resp := &cbapi.CallbackMsgModifyCommandResp{}

		// 执行同步回调，获取修改后的消息内容
		if err := m.webhookClient.SyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, resp, before); err != nil {
			return err
		}

		// 根据回调响应更新消息内容和相关字段
		// 使用 datautil.NotNilReplace 确保只有非空值才会替换原有内容
		if resp.Content != nil {
			msg.MsgData.Content = []byte(*resp.Content) // 更新消息内容
		}

		// 以下字段允许业务系统在回调中修改
		datautil.NotNilReplace(msg.MsgData.OfflinePushInfo, resp.OfflinePushInfo)    // 离线推送信息
		datautil.NotNilReplace(&msg.MsgData.RecvID, resp.RecvID)                     // 接收者ID
		datautil.NotNilReplace(&msg.MsgData.GroupID, resp.GroupID)                   // 群组ID
		datautil.NotNilReplace(&msg.MsgData.ClientMsgID, resp.ClientMsgID)           // 客户端消息ID
		datautil.NotNilReplace(&msg.MsgData.ServerMsgID, resp.ServerMsgID)           // 服务器消息ID
		datautil.NotNilReplace(&msg.MsgData.SenderPlatformID, resp.SenderPlatformID) // 发送者平台ID
		datautil.NotNilReplace(&msg.MsgData.SenderNickname, resp.SenderNickname)     // 发送者昵称
		datautil.NotNilReplace(&msg.MsgData.SenderFaceURL, resp.SenderFaceURL)       // 发送者头像URL
		datautil.NotNilReplace(&msg.MsgData.SessionType, resp.SessionType)           // 会话类型
		datautil.NotNilReplace(&msg.MsgData.MsgFrom, resp.MsgFrom)                   // 消息来源
		datautil.NotNilReplace(&msg.MsgData.ContentType, resp.ContentType)           // 内容类型
		datautil.NotNilReplace(&msg.MsgData.Status, resp.Status)                     // 消息状态
		datautil.NotNilReplace(&msg.MsgData.Options, resp.Options)                   // 消息选项
		datautil.NotNilReplace(&msg.MsgData.AtUserIDList, resp.AtUserIDList)         // @用户列表
		datautil.NotNilReplace(&msg.MsgData.AttachedInfo, resp.AttachedInfo)         // 附加信息
		datautil.NotNilReplace(&msg.MsgData.Ex, resp.Ex)                             // 扩展字段

		return nil
	})
}

// webhookAfterGroupMsgRead 群聊消息已读后的webhook回调
// 异步通知业务系统群聊消息已被某用户阅读
// 用于群聊已读状态统计、分析等场景
func (m *msgServer) webhookAfterGroupMsgRead(ctx context.Context, after *config.AfterConfig, req *cbapi.CallbackGroupMsgReadReq) {
	// 设置回调命令类型
	req.CallbackCommand = cbapi.CallbackAfterGroupMsgReadCommand

	// 执行异步回调，通知业务系统群消息已读事件
	m.webhookClient.AsyncPost(ctx, req.GetCallbackCommand(), req, &cbapi.CallbackGroupMsgReadResp{}, after)
}

// webhookAfterSingleMsgRead 单聊消息已读后的webhook回调
// 异步通知业务系统单聊消息已被阅读
// 用于已读回执统计、用户活跃度分析等
func (m *msgServer) webhookAfterSingleMsgRead(ctx context.Context, after *config.AfterConfig, req *cbapi.CallbackSingleMsgReadReq) {
	// 设置回调命令类型
	req.CallbackCommand = cbapi.CallbackAfterSingleMsgReadCommand

	// 执行异步回调，通知业务系统单聊消息已读事件
	m.webhookClient.AsyncPost(ctx, req.GetCallbackCommand(), req, &cbapi.CallbackSingleMsgReadResp{}, after)
}

// webhookAfterRevokeMsg 消息撤回后的webhook回调
// 异步通知业务系统消息已被撤回
// 用于撤回统计、内容审核记录等业务场景
func (m *msgServer) webhookAfterRevokeMsg(ctx context.Context, after *config.AfterConfig, req *pbchat.RevokeMsgReq) {
	// 构建消息撤回回调请求
	callbackReq := &cbapi.CallbackAfterRevokeMsgReq{
		CallbackCommand: cbapi.CallbackAfterRevokeMsgCommand,
		ConversationID:  req.ConversationID, // 会话ID
		Seq:             req.Seq,            // 被撤回消息的序列号
		UserID:          req.UserID,         // 执行撤回操作的用户ID
	}

	// 执行异步回调，通知业务系统消息撤回事件
	m.webhookClient.AsyncPost(ctx, callbackReq.GetCallbackCommand(), callbackReq, &cbapi.CallbackAfterRevokeMsgResp{}, after)
}
