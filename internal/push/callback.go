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

// Package push 推送服务的回调处理模块
// 实现推送前后的webhook回调机制，允许业务系统干预推送过程
package push

import (
	"context"
	"encoding/json"

	"github.com/openimsdk/open-im-server/v3/pkg/common/webhook"

	"github.com/openimsdk/open-im-server/v3/pkg/callbackstruct"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/mcontext"
)

// webhookBeforeOfflinePush 离线推送前的webhook回调
// 在执行离线推送前调用，允许业务系统修改推送用户列表和推送信息
// 参数:
//   - ctx: 上下文
//   - before: webhook配置
//   - userIDs: 原始推送用户列表
//   - msg: 消息数据
//   - offlinePushUserIDs: 实际推送用户列表指针（可被回调修改）
//
// 返回:
//   - error: 回调失败时的错误信息
func (c *ConsumerHandler) webhookBeforeOfflinePush(ctx context.Context, before *config.BeforeConfig, userIDs []string, msg *sdkws.MsgData, offlinePushUserIDs *[]string) error {
	return webhook.WithCondition(ctx, before, func(ctx context.Context) error {
		// 忽略输入状态消息的推送回调
		if msg.ContentType == constant.Typing {
			return nil
		}

		// 构建离线推送前回调请求
		req := &callbackstruct.CallbackBeforePushReq{
			UserStatusBatchCallbackReq: callbackstruct.UserStatusBatchCallbackReq{
				UserStatusBaseCallback: callbackstruct.UserStatusBaseCallback{
					CallbackCommand: callbackstruct.CallbackBeforeOfflinePushCommand,      // 回调命令类型
					OperationID:     mcontext.GetOperationID(ctx),                         // 操作ID，用于追踪
					PlatformID:      int(msg.SenderPlatformID),                            // 发送者平台ID
					Platform:        constant.PlatformIDToName(int(msg.SenderPlatformID)), // 平台名称
				},
				UserIDList: userIDs, // 推送用户列表
			},
			// 消息相关信息
			OfflinePushInfo: msg.OfflinePushInfo, // 离线推送配置
			ClientMsgID:     msg.ClientMsgID,     // 客户端消息ID
			SendID:          msg.SendID,          // 发送者ID
			GroupID:         msg.GroupID,         // 群组ID（如果是群消息）
			ContentType:     msg.ContentType,     // 消息内容类型
			SessionType:     msg.SessionType,     // 会话类型
			AtUserIDs:       msg.AtUserIDList,    // @用户列表
			Content:         GetContent(msg),     // 消息内容
		}

		resp := &callbackstruct.CallbackBeforePushResp{}

		// 发送同步webhook请求
		if err := c.webhookClient.SyncPost(ctx, req.GetCallbackCommand(), req, resp, before); err != nil {
			return err
		}

		// 处理回调响应
		if len(resp.UserIDs) != 0 {
			// 如果回调返回了新的用户列表，使用新列表
			*offlinePushUserIDs = resp.UserIDs
		}
		if resp.OfflinePushInfo != nil {
			// 如果回调返回了新的推送配置，更新消息的推送配置
			msg.OfflinePushInfo = resp.OfflinePushInfo
		}
		return nil
	})
}

// webhookBeforeOnlinePush 在线推送前的webhook回调
// 在执行在线推送前调用，允许业务系统干预在线推送过程
// 参数:
//   - ctx: 上下文
//   - before: webhook配置
//   - userIDs: 推送用户列表
//   - msg: 消息数据
//
// 返回:
//   - error: 回调失败时的错误信息
func (c *ConsumerHandler) webhookBeforeOnlinePush(ctx context.Context, before *config.BeforeConfig, userIDs []string, msg *sdkws.MsgData) error {
	return webhook.WithCondition(ctx, before, func(ctx context.Context) error {
		// 忽略输入状态消息的推送回调
		if msg.ContentType == constant.Typing {
			return nil
		}

		// 构建在线推送前回调请求
		req := callbackstruct.CallbackBeforePushReq{
			UserStatusBatchCallbackReq: callbackstruct.UserStatusBatchCallbackReq{
				UserStatusBaseCallback: callbackstruct.UserStatusBaseCallback{
					CallbackCommand: callbackstruct.CallbackBeforeOnlinePushCommand,       // 在线推送回调命令
					OperationID:     mcontext.GetOperationID(ctx),                         // 操作ID
					PlatformID:      int(msg.SenderPlatformID),                            // 发送者平台ID
					Platform:        constant.PlatformIDToName(int(msg.SenderPlatformID)), // 平台名称
				},
				UserIDList: userIDs, // 推送用户列表
			},
			// 消息相关信息（在线推送不包含OfflinePushInfo）
			ClientMsgID: msg.ClientMsgID,  // 客户端消息ID
			SendID:      msg.SendID,       // 发送者ID
			GroupID:     msg.GroupID,      // 群组ID
			ContentType: msg.ContentType,  // 消息内容类型
			SessionType: msg.SessionType,  // 会话类型
			AtUserIDs:   msg.AtUserIDList, // @用户列表
			Content:     GetContent(msg),  // 消息内容
		}

		resp := &callbackstruct.CallbackBeforePushResp{}

		// 发送同步webhook请求
		if err := c.webhookClient.SyncPost(ctx, req.GetCallbackCommand(), req, resp, before); err != nil {
			return err
		}
		return nil
	})
}

// webhookBeforeGroupOnlinePush 群组在线推送前的webhook回调
// 在执行群组在线推送前调用，允许业务系统修改群组推送的用户列表
// 参数:
//   - ctx: 上下文
//   - before: webhook配置
//   - groupID: 群组ID
//   - msg: 消息数据
//   - pushToUserIDs: 推送用户列表指针（可被回调修改）
//
// 返回:
//   - error: 回调失败时的错误信息
func (c *ConsumerHandler) webhookBeforeGroupOnlinePush(
	ctx context.Context,
	before *config.BeforeConfig,
	groupID string,
	msg *sdkws.MsgData,
	pushToUserIDs *[]string,
) error {
	return webhook.WithCondition(ctx, before, func(ctx context.Context) error {
		// 忽略输入状态消息的推送回调
		if msg.ContentType == constant.Typing {
			return nil
		}

		// 构建群组在线推送前回调请求
		req := callbackstruct.CallbackBeforeSuperGroupOnlinePushReq{
			UserStatusBaseCallback: callbackstruct.UserStatusBaseCallback{
				CallbackCommand: callbackstruct.CallbackBeforeGroupOnlinePushCommand,  // 群组在线推送回调命令
				OperationID:     mcontext.GetOperationID(ctx),                         // 操作ID
				PlatformID:      int(msg.SenderPlatformID),                            // 发送者平台ID
				Platform:        constant.PlatformIDToName(int(msg.SenderPlatformID)), // 平台名称
			},
			// 群组消息相关信息
			ClientMsgID: msg.ClientMsgID,  // 客户端消息ID
			SendID:      msg.SendID,       // 发送者ID
			GroupID:     groupID,          // 群组ID
			ContentType: msg.ContentType,  // 消息内容类型
			SessionType: msg.SessionType,  // 会话类型
			AtUserIDs:   msg.AtUserIDList, // @用户列表
			Content:     GetContent(msg),  // 消息内容
			Seq:         msg.Seq,          // 消息序号
		}

		resp := &callbackstruct.CallbackBeforeSuperGroupOnlinePushResp{}

		// 发送同步webhook请求
		if err := c.webhookClient.SyncPost(ctx, req.GetCallbackCommand(), req, resp, before); err != nil {
			return err
		}

		// 处理回调响应
		if len(resp.UserIDs) != 0 {
			// 如果回调返回了新的用户列表，使用新列表
			*pushToUserIDs = resp.UserIDs
		}
		return nil
	})
}

// GetContent 提取消息内容
// 根据消息类型提取可显示的消息内容，用于推送显示和回调
// 参数:
//   - msg: 消息数据
//
// 返回:
//   - string: 提取的消息内容字符串
func GetContent(msg *sdkws.MsgData) string {
	// 判断是否为通知类型消息
	if msg.ContentType >= constant.NotificationBegin && msg.ContentType <= constant.NotificationEnd {
		// 通知消息：解析通知元素，提取详细信息
		var notification sdkws.NotificationElem
		if err := json.Unmarshal(msg.Content, &notification); err != nil {
			return "" // 解析失败返回空字符串
		}
		return notification.Detail // 返回通知详情
	} else {
		// 普通消息：直接返回消息内容
		return string(msg.Content)
	}
}
