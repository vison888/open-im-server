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

// Package conversation 会话通知模块
//
// 本文件实现了会话相关的通知功能，是IM系统中重要的消息推送机制。
// 主要功能包括：
// 1. 会话隐私设置变更通知：当用户设置或取消私聊时推送通知
// 2. 会话状态变更通知：当会话的各种属性发生变化时推送通知
// 3. 会话未读数变更通知：当会话未读消息数量发生变化时推送通知
//
// 设计目的：
// - 确保多端会话状态实时同步
// - 提供及时的会话变更反馈
// - 支持会话相关的实时推送需求
//
// 通知类型：
// - ConversationPrivateChatNotification: 私聊设置变更通知
// - ConversationChangeNotification: 会话变更通知
// - ConversationUnreadNotification: 未读数变更通知
package conversation

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/notification"
	"github.com/openimsdk/open-im-server/v3/pkg/rpcli"
	"github.com/openimsdk/protocol/msg"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/sdkws"
)

// ConversationNotificationSender 会话通知发送器
//
// 这是会话模块的通知发送器，负责处理所有与会话相关的通知推送。
// 它继承了通用的NotificationSender，专门用于会话相关的通知场景。
//
// 主要职责：
// 1. 封装会话相关的通知逻辑
// 2. 构造特定格式的通知消息
// 3. 通过消息服务发送通知
//
// 通知机制：
// - 基于消息系统的可靠推送
// - 支持多端同步
// - 确保通知的及时性和准确性
type ConversationNotificationSender struct {
	// 嵌入通用的通知发送器
	// 提供基础的通知发送能力和配置管理
	*notification.NotificationSender
}

// NewConversationNotificationSender 创建会话通知发送器
//
// 初始化一个专门用于会话通知的发送器实例。
//
// 参数：
// - conf: 通知配置信息，包含推送策略、重试机制等配置
// - msgClient: 消息服务客户端，用于实际的消息发送
//
// 返回：
// - 初始化完成的会话通知发送器实例
func NewConversationNotificationSender(conf *config.Notification, msgClient *rpcli.MsgClient) *ConversationNotificationSender {
	return &ConversationNotificationSender{
		// 使用消息客户端初始化通用通知发送器
		// WithRpcClient选项配置了消息发送的具体实现
		// 这种设计模式实现了依赖注入，使通知发送器与具体的消息服务解耦
		notification.NewNotificationSender(conf, notification.WithRpcClient(func(ctx context.Context, req *msg.SendMsgReq) (*msg.SendMsgResp, error) {
			// 通过RPC调用消息服务发送通知消息
			// 这确保了通知消息与普通消息使用相同的发送机制和可靠性保证
			return msgClient.SendMsg(ctx, req)
		})),
	}
}

// ConversationSetPrivateNotification 发送会话隐私设置变更通知
//
// 当用户设置或取消私聊时，向相关用户发送通知。
// 这确保了双方都能及时了解会话的隐私状态变化。
//
// 应用场景：
// 1. 用户A将与用户B的会话设置为私聊
// 2. 用户A取消与用户B的私聊设置
// 3. 群聊中的私聊设置变更
//
// 参数：
// - ctx: 请求上下文
// - sendID: 发起设置变更的用户ID
// - recvID: 接收通知的用户ID（通常是会话的另一方）
// - isPrivateChat: 是否设置为私聊（true: 设置私聊, false: 取消私聊）
// - conversationID: 相关的会话ID
//
// 通知内容：
// - 包含设置变更的详细信息
// - 明确指示隐私状态的变化
// - 提供会话上下文信息
func (c *ConversationNotificationSender) ConversationSetPrivateNotification(ctx context.Context, sendID, recvID string,
	isPrivateChat bool, conversationID string,
) {
	// 构造会话隐私设置通知的数据结构
	// 这个结构包含了隐私设置变更的所有必要信息
	tips := &sdkws.ConversationSetPrivateTips{
		RecvID:         recvID,         // 接收通知的用户ID
		SendID:         sendID,         // 发起设置的用户ID
		IsPrivate:      isPrivateChat,  // 隐私状态（true: 私聊, false: 非私聊）
		ConversationID: conversationID, // 相关会话ID
	}

	// 发送通知消息
	// 通知类型：ConversationPrivateChatNotification
	// 发送者：sendID（发起设置变更的用户）
	// 接收者：recvID（需要接收通知的用户）
	//
	// 通知流程：
	// 1. 构造系统通知消息
	// 2. 通过消息服务发送给目标用户
	// 3. 客户端收到通知后更新会话隐私状态
	// 4. 实现多端实时同步
	c.Notification(ctx, sendID, recvID, constant.ConversationPrivateChatNotification, tips)
}

// ConversationChangeNotification 发送会话变更通知
//
// 当会话的属性发生变化时，向用户发送通知。
// 这是最常用的会话通知类型，涵盖了大部分会话状态变更场景。
//
// 触发场景：
// 1. 会话置顶状态变更
// 2. 消息免打扰设置变更
// 3. 会话扩展信息更新
// 4. 群聊@类型变更
// 5. 消息销毁设置变更
// 6. 其他会话属性的修改
//
// 参数：
// - ctx: 请求上下文
// - userID: 接收通知的用户ID
// - conversationIDs: 发生变更的会话ID列表（支持批量通知）
//
// 设计特点：
// - 支持批量会话变更通知，提高效率
// - 通知发送给会话所有者，确保多端同步
// - 客户端收到通知后可以刷新相应的会话数据
func (c *ConversationNotificationSender) ConversationChangeNotification(ctx context.Context, userID string, conversationIDs []string) {
	// 构造会话变更通知的数据结构
	// 包含用户ID和发生变更的会话ID列表
	tips := &sdkws.ConversationUpdateTips{
		UserID:             userID,          // 接收通知的用户ID
		ConversationIDList: conversationIDs, // 发生变更的会话ID列表
	}

	// 发送通知消息
	// 通知类型：ConversationChangeNotification
	// 发送者和接收者都是同一个用户（userID）
	// 这种设计是因为会话变更通知主要用于多端同步
	//
	// 多端同步机制：
	// 1. 用户在设备A上修改会话设置
	// 2. 服务端更新数据库并发送通知
	// 3. 用户的其他设备（B、C、D等）收到通知
	// 4. 其他设备自动更新本地会话状态
	// 5. 实现所有设备的会话状态一致性
	//
	// 批量处理优势：
	// - 支持一次通知多个会话的变更
	// - 减少网络开销和通知频率
	// - 提高多会话操作的效率
	c.Notification(ctx, userID, userID, constant.ConversationChangeNotification, tips)
}

// ConversationUnreadChangeNotification 发送会话未读数变更通知
//
// 当会话的未读消息数量发生变化时，向用户发送通知。
// 这对于实现实时的未读数同步和角标更新非常重要。
//
// 触发场景：
// 1. 用户阅读消息，未读数减少
// 2. 收到新消息，未读数增加
// 3. 用户清除会话未读数
// 4. 会话的已读序列号更新
//
// 参数：
// - ctx: 请求上下文
// - userID: 接收通知的用户ID
// - conversationID: 发生未读数变更的会话ID
// - unreadCountTime: 未读数统计时间戳（用于时间同步）
// - hasReadSeq: 用户已读的最大消息序列号
//
// 应用价值：
// - 确保多端未读数实时同步
// - 支持应用角标的准确显示
// - 提供用户阅读状态的及时反馈
func (c *ConversationNotificationSender) ConversationUnreadChangeNotification(
	ctx context.Context,
	userID, conversationID string,
	unreadCountTime, hasReadSeq int64,
) {
	// 构造会话未读数变更通知的数据结构
	// 包含未读数计算所需的所有关键信息
	tips := &sdkws.ConversationHasReadTips{
		UserID:          userID,          // 接收通知的用户ID
		ConversationID:  conversationID,  // 发生变更的会话ID
		HasReadSeq:      hasReadSeq,      // 用户已读的最大消息序列号
		UnreadCountTime: unreadCountTime, // 未读数统计的时间戳
	}

	// 发送通知消息
	// 通知类型：ConversationUnreadNotification
	// 发送者和接收者都是同一个用户（userID）
	// 这种设计用于多端未读数同步
	//
	// 未读数同步机制：
	// 1. 用户在设备A上阅读消息
	// 2. 服务端更新用户的已读序列号
	// 3. 计算新的未读数并发送通知
	// 4. 用户的其他设备收到通知后更新未读数显示
	// 5. 所有设备的未读数保持一致
	//
	// 关键参数说明：
	// - hasReadSeq: 用户已读的最大消息序列号，用于计算未读数
	// - unreadCountTime: 统计时间戳，用于时间同步和去重
	// - conversationID: 特定会话的标识，支持单会话未读数更新
	//
	// 应用场景：
	// - 用户阅读消息后的未读数更新
	// - 消息撤回后的未读数重新计算
	// - 会话清空后的未读数重置
	// - 多端登录时的未读数同步
	c.Notification(ctx, userID, userID, constant.ConversationUnreadNotification, tips)
}
