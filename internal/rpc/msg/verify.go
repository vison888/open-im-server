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
	"math/rand"
	"strconv"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/servererrs"
	"github.com/openimsdk/tools/utils/datautil"
	"github.com/openimsdk/tools/utils/encrypt"
	"github.com/openimsdk/tools/utils/timeutil"

	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/msg"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/errs"
)

// ExcludeContentType 排除的消息内容类型
// 这些类型的消息不需要进行某些特殊处理
var ExcludeContentType = []int{constant.HasReadReceipt}

// Validator 消息验证器接口
// 定义了消息验证的标准接口，支持可扩展的验证策略
type Validator interface {
	validate(pb *msg.SendMsgReq) (bool, int32, string)
}

// MessageRevoked 消息撤回信息结构体
// 记录消息撤回的详细信息，用于撤回通知和审计
type MessageRevoked struct {
	RevokerID                   string `json:"revokerID"`                   // 撤回者ID
	RevokerRole                 int32  `json:"revokerRole"`                 // 撤回者角色（群主、管理员等）
	ClientMsgID                 string `json:"clientMsgID"`                 // 客户端消息ID
	RevokerNickname             string `json:"revokerNickname"`             // 撤回者昵称
	RevokeTime                  int64  `json:"revokeTime"`                  // 撤回时间
	SourceMessageSendTime       int64  `json:"sourceMessageSendTime"`       // 原消息发送时间
	SourceMessageSendID         string `json:"sourceMessageSendID"`         // 原消息发送者ID
	SourceMessageSenderNickname string `json:"sourceMessageSenderNickname"` // 原消息发送者昵称
	SessionType                 int32  `json:"sessionType"`                 // 会话类型
	Seq                         uint32 `json:"seq"`                         // 消息序列号
}

// messageVerification 消息验证的核心方法
// 根据不同的会话类型执行相应的验证逻辑，确保消息发送的合法性和安全性
func (m *msgServer) messageVerification(ctx context.Context, data *msg.SendMsgReq) error {
	switch data.MsgData.SessionType {
	case constant.SingleChatType:
		// 单聊消息验证逻辑

		// 1. 管理员用户跳过验证
		if datautil.Contain(data.MsgData.SendID, m.config.Share.IMAdminUserID...) {
			return nil
		}

		// 2. 系统通知消息跳过验证
		if data.MsgData.ContentType <= constant.NotificationEnd &&
			data.MsgData.ContentType >= constant.NotificationBegin {
			return nil
		}

		// 3. Webhook前置验证：单聊消息发送前回调
		if err := m.webhookBeforeSendSingleMsg(ctx, &m.config.WebhooksConfig.BeforeSendSingleMsg, data); err != nil {
			return err
		}

		// 4. 检查黑名单关系：如果被对方拉黑，则不能发送消息
		black, err := m.FriendLocalCache.IsBlack(ctx, data.MsgData.SendID, data.MsgData.RecvID)
		if err != nil {
			return err
		}
		if black {
			return servererrs.ErrBlockedByPeer.Wrap()
		}

		// 5. 好友关系验证（如果启用了好友验证）
		if m.config.RpcConfig.FriendVerify {
			friend, err := m.FriendLocalCache.IsFriend(ctx, data.MsgData.SendID, data.MsgData.RecvID)
			if err != nil {
				return err
			}
			if !friend {
				return servererrs.ErrNotPeersFriend.Wrap()
			}
			return nil
		}
		return nil

	case constant.ReadGroupChatType:
		// 群聊消息验证逻辑

		// 1. 获取群组信息
		groupInfo, err := m.GroupLocalCache.GetGroupInfo(ctx, data.MsgData.GroupID)
		if err != nil {
			return err
		}

		// 2. 检查群组状态：已解散的群组不能发送消息（解散通知除外）
		if groupInfo.Status == constant.GroupStatusDismissed &&
			data.MsgData.ContentType != constant.GroupDismissedNotification {
			return servererrs.ErrDismissedAlready.Wrap()
		}

		// 3. 超级群组跳过后续验证
		if groupInfo.GroupType == constant.SuperGroup {
			return nil
		}

		// 4. 管理员用户跳过验证
		if datautil.Contain(data.MsgData.SendID, m.config.Share.IMAdminUserID...) {
			return nil
		}

		// 5. 系统通知消息跳过验证
		if data.MsgData.ContentType <= constant.NotificationEnd &&
			data.MsgData.ContentType >= constant.NotificationBegin {
			return nil
		}

		// 6. 检查用户是否为群成员
		memberIDs, err := m.GroupLocalCache.GetGroupMemberIDMap(ctx, data.MsgData.GroupID)
		if err != nil {
			return err
		}
		if _, ok := memberIDs[data.MsgData.SendID]; !ok {
			return servererrs.ErrNotInGroupYet.Wrap()
		}

		// 7. 获取群成员详细信息，检查权限和禁言状态
		groupMemberInfo, err := m.GroupLocalCache.GetGroupMember(ctx, data.MsgData.GroupID, data.MsgData.SendID)
		if err != nil {
			if errs.ErrRecordNotFound.Is(err) {
				return servererrs.ErrNotInGroupYet.WrapMsg(err.Error())
			}
			return err
		}

		// 8. 群主拥有最高权限，跳过禁言检查
		if groupMemberInfo.RoleLevel == constant.GroupOwner {
			return nil
		} else {
			// 9. 检查个人禁言状态
			if groupMemberInfo.MuteEndTime >= time.Now().UnixMilli() {
				return servererrs.ErrMutedInGroup.Wrap()
			}

			// 10. 检查群组禁言状态（管理员不受群组禁言影响）
			if groupInfo.Status == constant.GroupStatusMuted && groupMemberInfo.RoleLevel != constant.GroupAdmin {
				return servererrs.ErrMutedGroup.Wrap()
			}
		}
		return nil

	default:
		// 其他类型的会话（如通知消息）不需要验证
		return nil
	}
}

// encapsulateMsgData 封装消息数据
// 为消息设置服务器端的必要信息，如消息ID、发送时间、消息选项等
func (m *msgServer) encapsulateMsgData(msg *sdkws.MsgData) {
	// 生成唯一的服务器消息ID
	msg.ServerMsgID = GetMsgID(msg.SendID)

	// 设置发送时间（如果客户端没有提供）
	if msg.SendTime == 0 {
		msg.SendTime = timeutil.GetCurrentTimestampByMill()
	}

	// 根据消息类型设置默认的消息选项
	switch msg.ContentType {
	case constant.Text:
		fallthrough
	case constant.Picture:
		fallthrough
	case constant.Voice:
		fallthrough
	case constant.Video:
		fallthrough
	case constant.File:
		fallthrough
	case constant.AtText:
		fallthrough
	case constant.Merger:
		fallthrough
	case constant.Card:
		fallthrough
	case constant.Location:
		fallthrough
	case constant.Custom:
		fallthrough
	case constant.Quote:
		// 普通消息类型使用默认设置

	case constant.Revoke:
		// 撤回消息：不计入未读数，不推送离线消息
		datautil.SetSwitchFromOptions(msg.Options, constant.IsUnreadCount, false)
		datautil.SetSwitchFromOptions(msg.Options, constant.IsOfflinePush, false)

	case constant.HasReadReceipt:
		// 已读回执：不更新会话，不计入未读数，不推送离线消息
		datautil.SetSwitchFromOptions(msg.Options, constant.IsConversationUpdate, false)
		datautil.SetSwitchFromOptions(msg.Options, constant.IsSenderConversationUpdate, false)
		datautil.SetSwitchFromOptions(msg.Options, constant.IsUnreadCount, false)
		datautil.SetSwitchFromOptions(msg.Options, constant.IsOfflinePush, false)

	case constant.Typing:
		// 正在输入消息：不存储历史，不持久化，不同步，不更新会话，不计入未读数，不推送离线消息
		datautil.SetSwitchFromOptions(msg.Options, constant.IsHistory, false)
		datautil.SetSwitchFromOptions(msg.Options, constant.IsPersistent, false)
		datautil.SetSwitchFromOptions(msg.Options, constant.IsSenderSync, false)
		datautil.SetSwitchFromOptions(msg.Options, constant.IsConversationUpdate, false)
		datautil.SetSwitchFromOptions(msg.Options, constant.IsSenderConversationUpdate, false)
		datautil.SetSwitchFromOptions(msg.Options, constant.IsUnreadCount, false)
		datautil.SetSwitchFromOptions(msg.Options, constant.IsOfflinePush, false)
	}
}

// GetMsgID 生成唯一的消息ID
// 使用时间戳、发送者ID和随机数生成MD5哈希作为消息的唯一标识
func GetMsgID(sendID string) string {
	t := timeutil.GetCurrentTimeFormatted()
	return encrypt.Md5(t + "-" + sendID + "-" + strconv.Itoa(rand.Int()))
}

// modifyMessageByUserMessageReceiveOpt 根据用户消息接收设置修改消息
// 检查接收方的全局消息接收设置，决定是否发送消息以及如何处理
func (m *msgServer) modifyMessageByUserMessageReceiveOpt(ctx context.Context, userID, conversationID string, sessionType int, pb *msg.SendMsgReq) (bool, error) {
	// 获取用户的全局消息接收设置
	opt, err := m.UserLocalCache.GetUserGlobalMsgRecvOpt(ctx, userID)
	if err != nil {
		return false, err
	}

	switch opt {
	case constant.ReceiveMessage:
		// 正常接收消息

	case constant.NotReceiveMessage:
		// 不接收任何消息
		return false, nil

	case constant.ReceiveNotNotifyMessage:
		// 接收消息但不推送通知
		if pb.MsgData.Options == nil {
			pb.MsgData.Options = make(map[string]bool, 10)
		}
		// 关闭离线推送选项
		datautil.SetSwitchFromOptions(pb.MsgData.Options, constant.IsOfflinePush, false)
		return true, nil
	}
	singleOpt, err := m.ConversationLocalCache.GetSingleConversationRecvMsgOpt(ctx, userID, conversationID)
	if errs.ErrRecordNotFound.Is(err) {
		return true, nil
	} else if err != nil {
		return false, err
	}
	switch singleOpt {
	case constant.ReceiveMessage:
		return true, nil
	case constant.NotReceiveMessage:
		if datautil.Contain(int(pb.MsgData.ContentType), ExcludeContentType...) {
			return true, nil
		}
		return false, nil
	case constant.ReceiveNotNotifyMessage:
		if pb.MsgData.Options == nil {
			pb.MsgData.Options = make(map[string]bool, 10)
		}
		datautil.SetSwitchFromOptions(pb.MsgData.Options, constant.IsOfflinePush, false)
		return true, nil
	}
	return true, nil
}
