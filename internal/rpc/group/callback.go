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

/*
 * 群组Webhook回调系统
 *
 * 本模块实现了群组相关操作的Webhook回调机制，为业务系统提供强大的扩展能力。
 *
 * 核心功能：
 *
 * 1. 操作前回调（Before Callbacks）
 *    - 数据验证：在操作执行前验证业务逻辑
 *    - 参数修改：允许外部系统修改操作参数
 *    - 权限控制：实现自定义的权限验证逻辑
 *    - 业务拦截：在不满足条件时阻止操作执行
 *
 * 2. 操作后回调（After Callbacks）
 *    - 事件通知：向外部系统通知操作完成
 *    - 数据同步：同步数据到第三方系统
 *    - 业务联动：触发相关的业务流程
 *    - 审计日志：记录重要操作的审计信息
 *
 * 3. 回调事件类型
 *    - 群组生命周期：创建、解散、信息变更
 *    - 成员管理：邀请、踢出、角色变更、信息设置
 *    - 申请处理：加群申请、申请响应
 *    - 权限操作：转让群主、设置管理员
 *
 * 4. 数据流处理
 *    - 请求转换：将内部请求转换为外部回调格式
 *    - 响应处理：解析外部系统的回调响应
 *    - 参数合并：将回调响应合并到原始请求中
 *    - 错误处理：处理回调失败和超时情况
 *
 * 5. 业务集成特性
 *    - 同步回调：阻塞式回调，影响操作流程
 *    - 异步回调：非阻塞式回调，用于通知和同步
 *    - 批量处理：支持批量操作的回调通知
 *    - 条件回调：基于配置的条件性回调触发
 *
 * 技术特性：
 * - HTTP POST：使用标准HTTP POST进行回调通信
 * - JSON格式：统一使用JSON格式进行数据交换
 * - 超时控制：可配置的回调超时时间
 * - 重试机制：回调失败时的自动重试
 * - 签名验证：确保回调请求的安全性
 * - 幂等性：支持回调的幂等性处理
 *
 * 扩展场景：
 * - 企业通讯录同步：自动同步群组变更到企业系统
 * - 权限系统集成：集成外部权限管理系统
 * - 审计合规：满足企业审计和合规要求
 * - 业务流程：触发相关的业务审批流程
 * - 数据分析：收集群组操作数据用于分析
 */
package group

import (
	"context"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/apistruct"
	"github.com/openimsdk/open-im-server/v3/pkg/callbackstruct"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/open-im-server/v3/pkg/common/webhook"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/group"
	"github.com/openimsdk/protocol/wrapperspb"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
	"github.com/openimsdk/tools/utils/datautil"
)

// webhookBeforeCreateGroup 群组创建前回调
//
// 在群组创建操作执行前调用外部Webhook，允许外部系统：
// 1. 验证群组创建的业务逻辑
// 2. 修改群组的初始参数（如群组ID、名称、权限设置等）
// 3. 调整初始成员列表和角色分配
// 4. 实现自定义的创建权限控制
//
// 回调数据包含：
// - 群组基础信息：名称、介绍、头像、权限设置等
// - 初始成员列表：包含群主、管理员和普通成员
// - 操作上下文：操作ID、创建者信息等
//
// 响应处理：
// - 外部系统可以修改群组的任何初始参数
// - 可以调整成员的角色分配
// - 返回错误时将阻止群组创建
//
// 业务应用：
// - 企业群组命名规范检查
// - 群组创建权限验证
// - 自动分配企业群组ID
// - 集成外部审批流程
func (s *groupServer) webhookBeforeCreateGroup(ctx context.Context, before *config.BeforeConfig, req *group.CreateGroupReq) error {
	return webhook.WithCondition(ctx, before, func(ctx context.Context) error {
		cbReq := &callbackstruct.CallbackBeforeCreateGroupReq{
			CallbackCommand: callbackstruct.CallbackBeforeCreateGroupCommand,
			OperationID:     mcontext.GetOperationID(ctx),
			GroupInfo:       req.GroupInfo,
		}
		cbReq.InitMemberList = append(cbReq.InitMemberList, &apistruct.GroupAddMemberInfo{
			UserID:    req.OwnerUserID,
			RoleLevel: constant.GroupOwner,
		})
		for _, userID := range req.AdminUserIDs {
			cbReq.InitMemberList = append(cbReq.InitMemberList, &apistruct.GroupAddMemberInfo{
				UserID:    userID,
				RoleLevel: constant.GroupAdmin,
			})
		}
		for _, userID := range req.MemberUserIDs {
			cbReq.InitMemberList = append(cbReq.InitMemberList, &apistruct.GroupAddMemberInfo{
				UserID:    userID,
				RoleLevel: constant.GroupOrdinaryUsers,
			})
		}
		resp := &callbackstruct.CallbackBeforeCreateGroupResp{}

		if err := s.webhookClient.SyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, resp, before); err != nil {
			return err
		}

		datautil.NotNilReplace(&req.GroupInfo.GroupID, resp.GroupID)
		datautil.NotNilReplace(&req.GroupInfo.GroupName, resp.GroupName)
		datautil.NotNilReplace(&req.GroupInfo.Notification, resp.Notification)
		datautil.NotNilReplace(&req.GroupInfo.Introduction, resp.Introduction)
		datautil.NotNilReplace(&req.GroupInfo.FaceURL, resp.FaceURL)
		datautil.NotNilReplace(&req.GroupInfo.OwnerUserID, resp.OwnerUserID)
		datautil.NotNilReplace(&req.GroupInfo.Ex, resp.Ex)
		datautil.NotNilReplace(&req.GroupInfo.Status, resp.Status)
		datautil.NotNilReplace(&req.GroupInfo.CreatorUserID, resp.CreatorUserID)
		datautil.NotNilReplace(&req.GroupInfo.GroupType, resp.GroupType)
		datautil.NotNilReplace(&req.GroupInfo.NeedVerification, resp.NeedVerification)
		datautil.NotNilReplace(&req.GroupInfo.LookMemberInfo, resp.LookMemberInfo)
		return nil
	})
}

func (s *groupServer) webhookAfterCreateGroup(ctx context.Context, after *config.AfterConfig, req *group.CreateGroupReq) {
	cbReq := &callbackstruct.CallbackAfterCreateGroupReq{
		CallbackCommand: callbackstruct.CallbackAfterCreateGroupCommand,
		GroupInfo:       req.GroupInfo,
	}
	cbReq.InitMemberList = append(cbReq.InitMemberList, &apistruct.GroupAddMemberInfo{
		UserID:    req.OwnerUserID,
		RoleLevel: constant.GroupOwner,
	})
	for _, userID := range req.AdminUserIDs {
		cbReq.InitMemberList = append(cbReq.InitMemberList, &apistruct.GroupAddMemberInfo{
			UserID:    userID,
			RoleLevel: constant.GroupAdmin,
		})
	}
	for _, userID := range req.MemberUserIDs {
		cbReq.InitMemberList = append(cbReq.InitMemberList, &apistruct.GroupAddMemberInfo{
			UserID:    userID,
			RoleLevel: constant.GroupOrdinaryUsers,
		})
	}
	s.webhookClient.AsyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, &callbackstruct.CallbackAfterCreateGroupResp{}, after)
}

func (s *groupServer) webhookBeforeMembersJoinGroup(ctx context.Context, before *config.BeforeConfig, groupMembers []*model.GroupMember, groupID string, groupEx string) error {
	return webhook.WithCondition(ctx, before, func(ctx context.Context) error {
		groupMembersMap := datautil.SliceToMap(groupMembers, func(e *model.GroupMember) string {
			return e.UserID
		})
		var groupMembersCallback []*callbackstruct.CallbackGroupMember

		for _, member := range groupMembers {
			groupMembersCallback = append(groupMembersCallback, &callbackstruct.CallbackGroupMember{
				UserID: member.UserID,
				Ex:     member.Ex,
			})
		}

		cbReq := &callbackstruct.CallbackBeforeMembersJoinGroupReq{
			CallbackCommand: callbackstruct.CallbackBeforeMembersJoinGroupCommand,
			GroupID:         groupID,
			MembersList:     groupMembersCallback,
			GroupEx:         groupEx,
		}
		resp := &callbackstruct.CallbackBeforeMembersJoinGroupResp{}

		if err := s.webhookClient.SyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, resp, before); err != nil {
			return err
		}

		for _, memberCallbackResp := range resp.MemberCallbackList {
			if _, ok := groupMembersMap[(*memberCallbackResp.UserID)]; ok {
				if memberCallbackResp.MuteEndTime != nil {
					groupMembersMap[(*memberCallbackResp.UserID)].MuteEndTime = time.UnixMilli(*memberCallbackResp.MuteEndTime)
				}

				datautil.NotNilReplace(&groupMembersMap[(*memberCallbackResp.UserID)].FaceURL, memberCallbackResp.FaceURL)
				datautil.NotNilReplace(&groupMembersMap[(*memberCallbackResp.UserID)].Ex, memberCallbackResp.Ex)
				datautil.NotNilReplace(&groupMembersMap[(*memberCallbackResp.UserID)].Nickname, memberCallbackResp.Nickname)
				datautil.NotNilReplace(&groupMembersMap[(*memberCallbackResp.UserID)].RoleLevel, memberCallbackResp.RoleLevel)
			}
		}

		return nil
	})
}

func (s *groupServer) webhookBeforeSetGroupMemberInfo(ctx context.Context, before *config.BeforeConfig, req *group.SetGroupMemberInfo) error {
	return webhook.WithCondition(ctx, before, func(ctx context.Context) error {
		cbReq := callbackstruct.CallbackBeforeSetGroupMemberInfoReq{
			CallbackCommand: callbackstruct.CallbackBeforeSetGroupMemberInfoCommand,
			GroupID:         req.GroupID,
			UserID:          req.UserID,
		}
		if req.Nickname != nil {
			cbReq.Nickname = &req.Nickname.Value
		}
		if req.FaceURL != nil {
			cbReq.FaceURL = &req.FaceURL.Value
		}
		if req.RoleLevel != nil {
			cbReq.RoleLevel = &req.RoleLevel.Value
		}
		if req.Ex != nil {
			cbReq.Ex = &req.Ex.Value
		}
		resp := &callbackstruct.CallbackBeforeSetGroupMemberInfoResp{}
		if err := s.webhookClient.SyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, resp, before); err != nil {
			return err
		}
		if resp.FaceURL != nil {
			req.FaceURL = wrapperspb.String(*resp.FaceURL)
		}
		if resp.Nickname != nil {
			req.Nickname = wrapperspb.String(*resp.Nickname)
		}
		if resp.RoleLevel != nil {
			req.RoleLevel = wrapperspb.Int32(*resp.RoleLevel)
		}
		if resp.Ex != nil {
			req.Ex = wrapperspb.String(*resp.Ex)
		}
		return nil
	})
}

func (s *groupServer) webhookAfterSetGroupMemberInfo(ctx context.Context, after *config.AfterConfig, req *group.SetGroupMemberInfo) {
	cbReq := callbackstruct.CallbackAfterSetGroupMemberInfoReq{
		CallbackCommand: callbackstruct.CallbackAfterSetGroupMemberInfoCommand,
		GroupID:         req.GroupID,
		UserID:          req.UserID,
	}
	if req.Nickname != nil {
		cbReq.Nickname = &req.Nickname.Value
	}
	if req.FaceURL != nil {
		cbReq.FaceURL = &req.FaceURL.Value
	}
	if req.RoleLevel != nil {
		cbReq.RoleLevel = &req.RoleLevel.Value
	}
	if req.Ex != nil {
		cbReq.Ex = &req.Ex.Value
	}
	s.webhookClient.AsyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, &callbackstruct.CallbackAfterSetGroupMemberInfoResp{}, after)
}

// webhookAfterQuitGroup 成员退出群组后回调
//
// 在成员主动退出群组后异步通知外部系统，用于：
// 1. 同步成员变更信息到外部系统
// 2. 触发离职或转岗相关的业务流程
// 3. 记录成员退出的审计日志
// 4. 更新企业组织架构信息
//
// 回调数据包含：
// - 群组ID：发生退出操作的群组
// - 用户ID：退出的成员信息
// - 操作时间：退出操作的时间戳
func (s *groupServer) webhookAfterQuitGroup(ctx context.Context, after *config.AfterConfig, req *group.QuitGroupReq) {
	cbReq := &callbackstruct.CallbackQuitGroupReq{
		CallbackCommand: callbackstruct.CallbackAfterQuitGroupCommand,
		GroupID:         req.GroupID,
		UserID:          req.UserID,
	}
	s.webhookClient.AsyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, &callbackstruct.CallbackQuitGroupResp{}, after)
}

// webhookAfterKickGroupMember 踢出群成员后回调
//
// 在管理员踢出群成员后异步通知外部系统，用于：
// 1. 同步成员变更到企业系统
// 2. 触发人员调整相关的业务流程
// 3. 记录管理操作的审计日志
// 4. 发送相关的HR通知
//
// 回调数据包含：
// - 群组ID：发生踢出操作的群组
// - 被踢出的用户ID列表：支持批量踢出
// - 踢出原因：管理员提供的操作理由
// - 操作者信息：执行踢出操作的管理员
func (s *groupServer) webhookAfterKickGroupMember(ctx context.Context, after *config.AfterConfig, req *group.KickGroupMemberReq) {
	cbReq := &callbackstruct.CallbackKillGroupMemberReq{
		CallbackCommand: callbackstruct.CallbackAfterKickGroupCommand,
		GroupID:         req.GroupID,
		KickedUserIDs:   req.KickedUserIDs,
		Reason:          req.Reason,
	}
	s.webhookClient.AsyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, &callbackstruct.CallbackKillGroupMemberResp{}, after)
}

// webhookAfterDismissGroup 群组解散后回调
//
// 在群组解散后异步通知外部系统，用于：
// 1. 同步群组状态变更到企业系统
// 2. 触发项目结束或部门调整的业务流程
// 3. 记录群组解散的审计日志
// 4. 清理相关的企业资源和权限
//
// 回调数据包含：
// - 群组ID：被解散的群组
// - 群主ID：执行解散操作的群主
// - 成员ID列表：群组解散时的所有成员
// - 群组类型：被解散群组的类型信息
//
// 注意：如果选择删除成员（deleteMembe=true），
// 成员相关的数据将被物理删除，外部系统需要相应处理
func (s *groupServer) webhookAfterDismissGroup(ctx context.Context, after *config.AfterConfig, req *callbackstruct.CallbackDisMissGroupReq) {
	req.CallbackCommand = callbackstruct.CallbackAfterDisMissGroupCommand
	s.webhookClient.AsyncPost(ctx, req.GetCallbackCommand(), req, &callbackstruct.CallbackDisMissGroupResp{}, after)
}

// webhookBeforeApplyJoinGroup 申请加入群组前回调
//
// 在用户申请加入群组前调用外部Webhook，用于：
// 1. 验证用户是否有权限申请加入该群组
// 2. 实现自定义的准入规则和条件
// 3. 检查企业组织架构权限
// 4. 集成外部审批流程
//
// 回调数据包含：
// - 群组ID：用户申请加入的群组
// - 群组类型：群组的业务类型
// - 申请者ID：发起申请的用户
// - 申请消息：用户填写的申请理由
// - 扩展字段：其他业务相关信息
//
// 权限验证场景：
// - 企业员工只能申请加入本部门群组
// - 特定角色才能申请加入管理群组
// - 外部用户需要特殊审批流程
// - 基于项目权限的群组准入控制
//
// 返回错误时将阻止申请提交，
// 成功时申请将进入正常的审批流程
func (s *groupServer) webhookBeforeApplyJoinGroup(ctx context.Context, before *config.BeforeConfig, req *callbackstruct.CallbackJoinGroupReq) (err error) {
	return webhook.WithCondition(ctx, before, func(ctx context.Context) error {
		req.CallbackCommand = callbackstruct.CallbackBeforeJoinGroupCommand
		resp := &callbackstruct.CallbackJoinGroupResp{}
		if err := s.webhookClient.SyncPost(ctx, req.GetCallbackCommand(), req, resp, before); err != nil {
			return err
		}
		return nil
	})
}

func (s *groupServer) webhookAfterTransferGroupOwner(ctx context.Context, after *config.AfterConfig, req *group.TransferGroupOwnerReq) {
	cbReq := &callbackstruct.CallbackTransferGroupOwnerReq{
		CallbackCommand: callbackstruct.CallbackAfterTransferGroupOwnerCommand,
		GroupID:         req.GroupID,
		OldOwnerUserID:  req.OldOwnerUserID,
		NewOwnerUserID:  req.NewOwnerUserID,
	}
	s.webhookClient.AsyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, &callbackstruct.CallbackTransferGroupOwnerResp{}, after)
}

func (s *groupServer) webhookBeforeInviteUserToGroup(ctx context.Context, before *config.BeforeConfig, req *group.InviteUserToGroupReq) (err error) {
	return webhook.WithCondition(ctx, before, func(ctx context.Context) error {
		cbReq := &callbackstruct.CallbackBeforeInviteUserToGroupReq{
			CallbackCommand: callbackstruct.CallbackBeforeInviteJoinGroupCommand,
			OperationID:     mcontext.GetOperationID(ctx),
			GroupID:         req.GroupID,
			Reason:          req.Reason,
			InvitedUserIDs:  req.InvitedUserIDs,
		}

		resp := &callbackstruct.CallbackBeforeInviteUserToGroupResp{}
		if err := s.webhookClient.SyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, resp, before); err != nil {
			return err
		}

		// Handle the scenario where certain members are refused
		// You might want to update the req.Members list or handle it as per your business logic

		// if len(resp.RefusedMembersAccount) > 0 {
		// implement members are refused
		// }

		return nil
	})
}

func (s *groupServer) webhookAfterJoinGroup(ctx context.Context, after *config.AfterConfig, req *group.JoinGroupReq) {
	cbReq := &callbackstruct.CallbackAfterJoinGroupReq{
		CallbackCommand: callbackstruct.CallbackAfterJoinGroupCommand,
		OperationID:     mcontext.GetOperationID(ctx),
		GroupID:         req.GroupID,
		ReqMessage:      req.ReqMessage,
		JoinSource:      req.JoinSource,
		InviterUserID:   req.InviterUserID,
	}
	s.webhookClient.AsyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, &callbackstruct.CallbackAfterJoinGroupResp{}, after)
}

func (s *groupServer) webhookBeforeSetGroupInfo(ctx context.Context, before *config.BeforeConfig, req *group.SetGroupInfoReq) error {
	return webhook.WithCondition(ctx, before, func(ctx context.Context) error {
		cbReq := &callbackstruct.CallbackBeforeSetGroupInfoReq{
			CallbackCommand: callbackstruct.CallbackBeforeSetGroupInfoCommand,
			GroupID:         req.GroupInfoForSet.GroupID,
			Notification:    req.GroupInfoForSet.Notification,
			Introduction:    req.GroupInfoForSet.Introduction,
			FaceURL:         req.GroupInfoForSet.FaceURL,
			GroupName:       req.GroupInfoForSet.GroupName,
		}
		if req.GroupInfoForSet.Ex != nil {
			cbReq.Ex = req.GroupInfoForSet.Ex.Value
		}
		log.ZDebug(ctx, "debug CallbackBeforeSetGroupInfo", "ex", cbReq.Ex)
		if req.GroupInfoForSet.NeedVerification != nil {
			cbReq.NeedVerification = req.GroupInfoForSet.NeedVerification.Value
		}
		if req.GroupInfoForSet.LookMemberInfo != nil {
			cbReq.LookMemberInfo = req.GroupInfoForSet.LookMemberInfo.Value
		}
		if req.GroupInfoForSet.ApplyMemberFriend != nil {
			cbReq.ApplyMemberFriend = req.GroupInfoForSet.ApplyMemberFriend.Value
		}
		resp := &callbackstruct.CallbackBeforeSetGroupInfoResp{}

		if err := s.webhookClient.SyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, resp, before); err != nil {
			return err
		}

		if resp.Ex != nil {
			req.GroupInfoForSet.Ex = wrapperspb.String(*resp.Ex)
		}
		if resp.NeedVerification != nil {
			req.GroupInfoForSet.NeedVerification = wrapperspb.Int32(*resp.NeedVerification)
		}
		if resp.LookMemberInfo != nil {
			req.GroupInfoForSet.LookMemberInfo = wrapperspb.Int32(*resp.LookMemberInfo)
		}
		if resp.ApplyMemberFriend != nil {
			req.GroupInfoForSet.ApplyMemberFriend = wrapperspb.Int32(*resp.ApplyMemberFriend)
		}
		datautil.NotNilReplace(&req.GroupInfoForSet.GroupID, &resp.GroupID)
		datautil.NotNilReplace(&req.GroupInfoForSet.GroupName, &resp.GroupName)
		datautil.NotNilReplace(&req.GroupInfoForSet.FaceURL, &resp.FaceURL)
		datautil.NotNilReplace(&req.GroupInfoForSet.Introduction, &resp.Introduction)
		return nil
	})
}

func (s *groupServer) webhookAfterSetGroupInfo(ctx context.Context, after *config.AfterConfig, req *group.SetGroupInfoReq) {
	cbReq := &callbackstruct.CallbackAfterSetGroupInfoReq{
		CallbackCommand: callbackstruct.CallbackAfterSetGroupInfoCommand,
		GroupID:         req.GroupInfoForSet.GroupID,
		Notification:    req.GroupInfoForSet.Notification,
		Introduction:    req.GroupInfoForSet.Introduction,
		FaceURL:         req.GroupInfoForSet.FaceURL,
		GroupName:       req.GroupInfoForSet.GroupName,
	}
	if req.GroupInfoForSet.Ex != nil {
		cbReq.Ex = &req.GroupInfoForSet.Ex.Value
	}
	if req.GroupInfoForSet.NeedVerification != nil {
		cbReq.NeedVerification = &req.GroupInfoForSet.NeedVerification.Value
	}
	if req.GroupInfoForSet.LookMemberInfo != nil {
		cbReq.LookMemberInfo = &req.GroupInfoForSet.LookMemberInfo.Value
	}
	if req.GroupInfoForSet.ApplyMemberFriend != nil {
		cbReq.ApplyMemberFriend = &req.GroupInfoForSet.ApplyMemberFriend.Value
	}
	s.webhookClient.AsyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, &callbackstruct.CallbackAfterSetGroupInfoResp{}, after)
}

func (s *groupServer) webhookBeforeSetGroupInfoEx(ctx context.Context, before *config.BeforeConfig, req *group.SetGroupInfoExReq) error {
	return webhook.WithCondition(ctx, before, func(ctx context.Context) error {
		cbReq := &callbackstruct.CallbackBeforeSetGroupInfoExReq{
			CallbackCommand: callbackstruct.CallbackBeforeSetGroupInfoExCommand,
			GroupID:         req.GroupID,
			GroupName:       req.GroupName,
			Notification:    req.Notification,
			Introduction:    req.Introduction,
			FaceURL:         req.FaceURL,
		}

		if req.Ex != nil {
			cbReq.Ex = req.Ex
		}
		log.ZDebug(ctx, "debug CallbackBeforeSetGroupInfoEx", "ex", cbReq.Ex)

		if req.NeedVerification != nil {
			cbReq.NeedVerification = req.NeedVerification
		}
		if req.LookMemberInfo != nil {
			cbReq.LookMemberInfo = req.LookMemberInfo
		}
		if req.ApplyMemberFriend != nil {
			cbReq.ApplyMemberFriend = req.ApplyMemberFriend
		}

		resp := &callbackstruct.CallbackBeforeSetGroupInfoExResp{}

		if err := s.webhookClient.SyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, resp, before); err != nil {
			return err
		}

		datautil.NotNilReplace(&req.GroupID, &resp.GroupID)
		datautil.NotNilReplace(&req.GroupName, &resp.GroupName)
		datautil.NotNilReplace(&req.FaceURL, &resp.FaceURL)
		datautil.NotNilReplace(&req.Introduction, &resp.Introduction)
		datautil.NotNilReplace(&req.Ex, &resp.Ex)
		datautil.NotNilReplace(&req.NeedVerification, &resp.NeedVerification)
		datautil.NotNilReplace(&req.LookMemberInfo, &resp.LookMemberInfo)
		datautil.NotNilReplace(&req.ApplyMemberFriend, &resp.ApplyMemberFriend)

		return nil
	})
}

func (s *groupServer) webhookAfterSetGroupInfoEx(ctx context.Context, after *config.AfterConfig, req *group.SetGroupInfoExReq) {
	cbReq := &callbackstruct.CallbackAfterSetGroupInfoExReq{
		CallbackCommand: callbackstruct.CallbackAfterSetGroupInfoExCommand,
		GroupID:         req.GroupID,
		GroupName:       req.GroupName,
		Notification:    req.Notification,
		Introduction:    req.Introduction,
		FaceURL:         req.FaceURL,
	}

	if req.Ex != nil {
		cbReq.Ex = req.Ex
	}
	if req.NeedVerification != nil {
		cbReq.NeedVerification = req.NeedVerification
	}
	if req.LookMemberInfo != nil {
		cbReq.LookMemberInfo = req.LookMemberInfo
	}
	if req.ApplyMemberFriend != nil {
		cbReq.ApplyMemberFriend = req.ApplyMemberFriend
	}

	s.webhookClient.AsyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, &callbackstruct.CallbackAfterSetGroupInfoExResp{}, after)
}
