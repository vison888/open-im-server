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
// revoke.go 专门处理消息撤回功能
// 实现了完整的消息撤回机制，包括权限验证、撤回记录、通知发送等
// 支持单聊和群聊的消息撤回，具有完善的权限控制体系
package msg

import (
	"context"
	"encoding/json"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"

	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/open-im-server/v3/pkg/common/servererrs"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/msg"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
	"github.com/openimsdk/tools/utils/datautil"
)

// RevokeMsg 撤回消息
// 这是消息撤回的核心接口，实现了完整的撤回流程
// 包括参数验证、权限检查、消息查找、撤回执行、通知发送等步骤
func (m *msgServer) RevokeMsg(ctx context.Context, req *msg.RevokeMsgReq) (*msg.RevokeMsgResp, error) {
	// ==================== 参数验证阶段 ====================

	// 验证用户ID不能为空
	if req.UserID == "" {
		return nil, errs.ErrArgs.WrapMsg("user_id is empty")
	}

	// 验证会话ID不能为空
	if req.ConversationID == "" {
		return nil, errs.ErrArgs.WrapMsg("conversation_id is empty")
	}

	// 验证消息序列号有效性
	if req.Seq < 0 {
		return nil, errs.ErrArgs.WrapMsg("seq is invalid")
	}

	// ==================== 权限验证阶段 ====================

	// 基础权限验证：检查是否为用户本人或系统管理员
	// CheckAccessV3允许用户操作自己的数据，或管理员操作任意数据
	if err := authverify.CheckAccessV3(ctx, req.UserID, m.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	// ==================== 用户信息获取阶段 ====================

	// 获取撤回操作执行者的用户信息
	// 用于记录撤回者信息和后续的权限验证
	user, err := m.UserLocalCache.GetUserInfo(ctx, req.UserID)
	if err != nil {
		return nil, err
	}

	// ==================== 消息查找阶段 ====================

	// 根据序列号查找要撤回的消息
	// 这里只查找一条消息，因为序列号在会话中是唯一的
	_, _, msgs, err := m.MsgDatabase.GetMsgBySeqs(ctx, req.UserID, req.ConversationID, []int64{req.Seq})
	if err != nil {
		return nil, err
	}

	// 验证消息是否存在
	if len(msgs) == 0 || msgs[0] == nil {
		return nil, errs.ErrRecordNotFound.WrapMsg("msg not found")
	}

	// 检查消息是否已经被撤回
	// 防止重复撤回同一条消息
	if msgs[0].ContentType == constant.MsgRevokeNotification {
		return nil, servererrs.ErrMsgAlreadyRevoke.WrapMsg("msg already revoke")
	}

	// 记录调试日志，便于问题排查
	data, _ := json.Marshal(msgs[0])
	log.ZDebug(ctx, "GetMsgBySeqs", "conversationID", req.ConversationID, "seq", req.Seq, "msg", string(data))

	// ==================== 权限检查阶段 ====================

	var role int32 // 用于记录撤回者的角色级别

	// 如果不是应用管理员，需要进行详细的权限检查
	if !authverify.IsAppManagerUid(ctx, m.config.Share.IMAdminUserID) {
		sessionType := msgs[0].SessionType

		switch sessionType {
		case constant.SingleChatType:
			// 单聊权限验证：只能撤回自己发送的消息
			// 这里再次验证确保消息发送者就是请求撤回的用户
			if err := authverify.CheckAccessV3(ctx, msgs[0].SendID, m.config.Share.IMAdminUserID); err != nil {
				return nil, err
			}
			role = user.AppMangerLevel // 记录用户的应用管理级别

		case constant.ReadGroupChatType:
			// 群聊权限验证：根据群角色决定撤回权限

			// 获取相关群成员信息（撤回请求者和消息发送者）
			members, err := m.GroupLocalCache.GetGroupMemberInfoMap(ctx, msgs[0].GroupID,
				datautil.Distinct([]string{req.UserID, msgs[0].SendID}))
			if err != nil {
				return nil, err
			}

			// 如果撤回者不是消息发送者，需要检查管理权限
			if req.UserID != msgs[0].SendID {
				switch members[req.UserID].RoleLevel {
				case constant.GroupOwner:
					// 群主可以撤回任何人的消息

				case constant.GroupAdmin:
					// 群管理员可以撤回普通成员的消息，但不能撤回群主和其他管理员的消息
					if sendMember, ok := members[msgs[0].SendID]; ok {
						if sendMember.RoleLevel != constant.GroupOrdinaryUsers {
							return nil, errs.ErrNoPermission.WrapMsg("no permission")
						}
					}

				default:
					// 普通成员只能撤回自己的消息
					return nil, errs.ErrNoPermission.WrapMsg("no permission")
				}
			}

			// 记录撤回者在群中的角色级别
			if member := members[req.UserID]; member != nil {
				role = member.RoleLevel
			}

		default:
			// 不支持的会话类型
			return nil, errs.ErrInternalServer.WrapMsg("msg sessionType not supported", "sessionType", sessionType)
		}
	}

	// ==================== 撤回执行阶段 ====================

	// 记录撤回时间
	now := time.Now().UnixMilli()

	// 执行消息撤回操作
	// 这会在数据库中标记消息为已撤回状态，并记录撤回信息
	err = m.MsgDatabase.RevokeMsg(ctx, req.ConversationID, req.Seq, &model.RevokeModel{
		Role:     role,          // 撤回者角色
		UserID:   req.UserID,    // 撤回者用户ID
		Nickname: user.Nickname, // 撤回者昵称
		Time:     now,           // 撤回时间
	})
	if err != nil {
		return nil, err
	}

	// ==================== 通知发送阶段 ====================

	// 获取实际执行撤回操作的用户ID（可能与请求中的UserID不同）
	revokerUserID := mcontext.GetOpUserID(ctx)

	// 判断是否为管理员撤回
	// 这个标志会影响客户端的撤回消息显示
	var flag bool
	if len(m.config.Share.IMAdminUserID) > 0 {
		flag = datautil.Contain(revokerUserID, m.config.Share.IMAdminUserID...)
	}

	// 构建撤回通知消息
	tips := sdkws.RevokeMsgTips{
		RevokerUserID:  revokerUserID,       // 执行撤回的用户ID
		ClientMsgID:    msgs[0].ClientMsgID, // 被撤回消息的客户端ID
		RevokeTime:     now,                 // 撤回时间
		Seq:            req.Seq,             // 被撤回消息的序列号
		SesstionType:   msgs[0].SessionType, // 会话类型
		ConversationID: req.ConversationID,  // 会话ID
		IsAdminRevoke:  flag,                // 是否为管理员撤回
	}

	// 确定通知接收者
	var recvID string
	if msgs[0].SessionType == constant.ReadGroupChatType {
		// 群聊：通知整个群
		recvID = msgs[0].GroupID
	} else {
		// 单聊：通知对方用户
		recvID = msgs[0].RecvID
	}

	// 发送撤回通知
	// 这会通过实时通道（如WebSocket）通知相关用户消息已被撤回
	m.notificationSender.NotificationWithSessionType(ctx, req.UserID, recvID,
		constant.MsgRevokeNotification, msgs[0].SessionType, &tips)

	// ==================== 回调通知阶段 ====================

	// 执行撤回后的webhook回调
	// 通知业务系统消息撤回事件，用于统计、审计等用途
	m.webhookAfterRevokeMsg(ctx, &m.config.WebhooksConfig.AfterRevokeMsg, req)

	return &msg.RevokeMsgResp{}, nil
}
