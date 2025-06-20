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
// notification.go 专门处理消息相关的系统通知功能
// 提供统一的通知发送接口，用于向用户推送各种消息事件
// 是消息系统与通知系统之间的桥梁
package msg

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/notification"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/sdkws"
)

// MsgNotificationSender 消息通知发送器
// 这是一个组合结构体，包装了通用的通知发送器
// 提供消息服务专用的通知发送方法，简化消息相关通知的发送流程
type MsgNotificationSender struct {
	*notification.NotificationSender // 嵌入通用通知发送器，继承其所有方法
}

// NewMsgNotificationSender 创建新的消息通知发送器
// 基于通用通知发送器构建消息专用的通知发送器
// 支持传入额外的选项参数来定制发送器行为
func NewMsgNotificationSender(config *Config, opts ...notification.NotificationSenderOptions) *MsgNotificationSender {
	// 使用配置中的通知配置创建基础通知发送器
	// 然后包装成消息专用的通知发送器
	return &MsgNotificationSender{
		notification.NewNotificationSender(&config.NotificationConfig, opts...),
	}
}

// UserDeleteMsgsNotification 发送用户删除消息通知
// 当用户删除消息时，向该用户的其他设备发送同步通知
// 用于多设备消息状态同步，确保删除操作在所有设备上生效
func (m *MsgNotificationSender) UserDeleteMsgsNotification(ctx context.Context, userID, conversationID string, seqs []int64) {
	// 构建删除消息通知内容
	tips := sdkws.DeleteMsgsTips{
		UserID:         userID,         // 执行删除操作的用户ID
		ConversationID: conversationID, // 被删除消息所在的会话ID
		Seqs:           seqs,           // 被删除消息的序列号列表
	}

	// 发送通知给用户自己（用于多设备同步）
	// 发送者和接收者都是同一个用户，通知类型为删除消息通知
	m.Notification(ctx, userID, userID, constant.DeleteMsgsNotification, &tips)
}

// MarkAsReadNotification 发送标记已读通知
// 当用户标记消息为已读时，发送通知给相关用户
// 单聊时通知对方，群聊时根据需要决定是否通知其他成员
func (m *MsgNotificationSender) MarkAsReadNotification(ctx context.Context, conversationID string, sessionType int32, sendID, recvID string, seqs []int64, hasReadSeq int64) {
	// 构建已读标记通知内容
	tips := &sdkws.MarkAsReadTips{
		MarkAsReadUserID: sendID,         // 执行已读标记的用户ID
		ConversationID:   conversationID, // 会话ID
		Seqs:             seqs,           // 被标记为已读的消息序列号列表
		HasReadSeq:       hasReadSeq,     // 用户已读到的最新序列号
	}

	// 使用指定的会话类型发送通知
	// sessionType决定通知的分发范围：
	// - 单聊：通知对方用户消息已被阅读
	// - 群聊：根据群设置决定是否通知其他成员
	m.NotificationWithSessionType(ctx, sendID, recvID, constant.HasReadReceipt, sessionType, tips)
}
