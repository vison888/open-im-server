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

// Package relation 用户关系Webhook回调服务模块
//
// 本文件实现了OpenIM用户关系变更的Webhook回调机制，允许外部系统在关系操作前后
// 介入业务流程，实现业务逻辑的扩展和自定义。这是系统开放性和可扩展性的重要体现。
//
// 回调机制设计：
// 1. Before回调：在操作执行前调用，允许外部系统拦截或修改操作
// 2. After回调：在操作执行后调用，通知外部系统操作结果
//
// 支持的回调事件：
// - 添加好友：BeforeAddFriend / AfterAddFriend
// - 删除好友：AfterDeleteFriend
// - 设置备注：BeforeSetFriendRemark / AfterSetFriendRemark
// - 批量导入：BeforeImportFriends / AfterImportFriends
// - 添加黑名单：BeforeAddBlack
// - 移除黑名单：AfterRemoveBlack
// - 处理好友申请：BeforeAddFriendAgree / AfterAddFriendAgree
//
// 回调特性：
// - 条件执行：支持配置条件，只在满足条件时执行回调
// - 同步/异步：Before回调通常是同步的，After回调通常是异步的
// - 数据修改：Before回调可以修改请求参数
// - 流程控制：Before回调可以阻止操作继续执行
//
// 应用场景：
// - 业务规则扩展：在外部系统中实现复杂的业务逻辑
// - 数据同步：将关系变更同步到其他系统
// - 审计日志：记录所有关系操作的详细日志
// - 权限控制：基于外部系统的权限验证
// - 数据校验：在外部系统中进行额外的数据验证
//
// 技术特点：
// - HTTP Webhook：基于HTTP协议的标准Webhook机制
// - 错误处理：完善的超时和错误处理机制
// - 配置灵活：支持多种配置和条件判断
// - 性能友好：异步回调不影响主流程性能
package relation

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/common/webhook"

	cbapi "github.com/openimsdk/open-im-server/v3/pkg/callbackstruct"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/protocol/relation"
)

// webhookAfterDeleteFriend 删除好友后置回调
//
// 在好友删除操作完成后调用外部系统的回调接口，通知外部系统好友关系已删除。
// 这是一个异步回调，不会阻塞主流程，主要用于数据同步和审计日志。
//
// 业务场景：
// - 同步删除其他系统中的好友关系
// - 记录好友删除的审计日志
// - 触发相关的业务流程（如推荐系统更新）
// - 通知第三方应用好友关系变更
//
// 参数说明：
// - ctx: 请求上下文，包含用户身份等信息
// - after: 后置回调配置，包含回调URL、超时设置等
// - req: 删除好友请求，包含操作相关的详细信息
//
// 回调数据：
// - CallbackCommand: 回调命令标识
// - OwnerUserID: 好友关系的拥有者用户ID
// - FriendUserID: 被删除的好友用户ID
func (s *friendServer) webhookAfterDeleteFriend(ctx context.Context, after *config.AfterConfig, req *relation.DeleteFriendReq) {
	cbReq := &cbapi.CallbackAfterDeleteFriendReq{
		CallbackCommand: cbapi.CallbackAfterDeleteFriendCommand,
		OwnerUserID:     req.OwnerUserID,
		FriendUserID:    req.FriendUserID,
	}
	s.webhookClient.AsyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, &cbapi.CallbackAfterDeleteFriendResp{}, after)
}

// webhookBeforeAddFriend 添加好友前置回调
//
// 在用户发起好友申请前调用外部系统的回调接口，允许外部系统根据业务规则
// 决定是否允许此次好友申请，或者修改申请参数。
//
// 业务场景：
// - 反垃圾策略：检查用户是否为恶意用户
// - 权限控制：基于外部系统的权限验证
// - 数据校验：验证申请消息的内容是否合规
// - 业务规则：实现复杂的好友添加业务逻辑
// - 频率限制：控制用户发起好友申请的频率
//
// 参数说明：
// - ctx: 请求上下文
// - before: 前置回调配置，包含回调URL、超时设置、重试配置等
// - req: 好友申请请求，包含申请相关的所有信息
//
// 返回值：
// - error: 如果外部系统拒绝此次申请，返回错误终止流程
// - ErrCallbackContinue: 特殊错误，表示跳过回调继续执行
//
// 回调数据：
// - CallbackCommand: 回调命令标识
// - FromUserID: 申请发起方用户ID
// - ToUserID: 申请目标方用户ID
// - ReqMsg: 申请消息内容
// - Ex: 扩展字段数据
func (s *friendServer) webhookBeforeAddFriend(ctx context.Context, before *config.BeforeConfig, req *relation.ApplyToAddFriendReq) error {
	return webhook.WithCondition(ctx, before, func(ctx context.Context) error {
		cbReq := &cbapi.CallbackBeforeAddFriendReq{
			CallbackCommand: cbapi.CallbackBeforeAddFriendCommand,
			FromUserID:      req.FromUserID,
			ToUserID:        req.ToUserID,
			ReqMsg:          req.ReqMsg,
			Ex:              req.Ex,
		}
		resp := &cbapi.CallbackBeforeAddFriendResp{}

		if err := s.webhookClient.SyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, resp, before); err != nil {
			return err
		}
		return nil
	})
}

// webhookAfterAddFriend 添加好友后置回调
//
// 在好友申请提交成功后调用外部系统的回调接口，通知外部系统有新的好友申请。
// 这是一个异步回调，用于数据同步和后续处理。
//
// 业务场景：
// - 数据同步：将好友申请同步到其他系统
// - 统计分析：更新用户行为统计数据
// - 推送通知：触发额外的推送通知
// - 审计日志：记录好友申请的详细日志
//
// 参数说明：
// - ctx: 请求上下文
// - after: 后置回调配置
// - req: 好友申请请求
//
// 回调数据：
// - CallbackCommand: 回调命令标识
// - FromUserID: 申请发起方用户ID
// - ToUserID: 申请目标方用户ID
// - ReqMsg: 申请消息内容
func (s *friendServer) webhookAfterAddFriend(ctx context.Context, after *config.AfterConfig, req *relation.ApplyToAddFriendReq) {
	cbReq := &cbapi.CallbackAfterAddFriendReq{
		CallbackCommand: cbapi.CallbackAfterAddFriendCommand,
		FromUserID:      req.FromUserID,
		ToUserID:        req.ToUserID,
		ReqMsg:          req.ReqMsg,
	}
	resp := &cbapi.CallbackAfterAddFriendResp{}
	s.webhookClient.AsyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, resp, after)
}

// webhookAfterSetFriendRemark 设置好友备注后置回调
//
// 在好友备注设置成功后调用外部系统的回调接口，通知外部系统好友备注已变更。
//
// 业务场景：
// - 数据同步：将备注变更同步到其他系统
// - 个性化服务：根据备注信息提供个性化服务
// - 数据分析：分析用户的备注习惯和社交关系
//
// 参数说明：
// - ctx: 请求上下文
// - after: 后置回调配置
// - req: 设置备注请求
//
// 回调数据：
// - CallbackCommand: 回调命令标识
// - OwnerUserID: 备注设置者用户ID
// - FriendUserID: 被设置备注的好友用户ID
// - Remark: 新的备注内容
func (s *friendServer) webhookAfterSetFriendRemark(ctx context.Context, after *config.AfterConfig, req *relation.SetFriendRemarkReq) {
	cbReq := &cbapi.CallbackAfterSetFriendRemarkReq{
		CallbackCommand: cbapi.CallbackAfterSetFriendRemarkCommand,
		OwnerUserID:     req.OwnerUserID,
		FriendUserID:    req.FriendUserID,
		Remark:          req.Remark,
	}
	resp := &cbapi.CallbackAfterSetFriendRemarkResp{}
	s.webhookClient.AsyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, resp, after)
}

// webhookAfterImportFriends 批量导入好友后置回调
//
// 在批量导入好友操作完成后调用外部系统的回调接口，通知外部系统好友关系已批量建立。
//
// 业务场景：
// - 数据同步：将批量导入的好友关系同步到其他系统
// - 统计更新：更新用户的好友数量统计
// - 推荐系统：更新用户的社交网络图谱
// - 审计记录：记录管理员的批量操作日志
//
// 参数说明：
// - ctx: 请求上下文
// - after: 后置回调配置
// - req: 批量导入请求
//
// 回调数据：
// - CallbackCommand: 回调命令标识
// - OwnerUserID: 好友关系拥有者用户ID
// - FriendUserIDs: 批量添加的好友用户ID列表
func (s *friendServer) webhookAfterImportFriends(ctx context.Context, after *config.AfterConfig, req *relation.ImportFriendReq) {
	cbReq := &cbapi.CallbackAfterImportFriendsReq{
		CallbackCommand: cbapi.CallbackAfterImportFriendsCommand,
		OwnerUserID:     req.OwnerUserID,
		FriendUserIDs:   req.FriendUserIDs,
	}
	resp := &cbapi.CallbackAfterImportFriendsResp{}
	s.webhookClient.AsyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, resp, after)
}

// webhookAfterRemoveBlack 移除黑名单后置回调
//
// 在黑名单移除操作完成后调用外部系统的回调接口，通知外部系统黑名单关系已移除。
//
// 业务场景：
// - 数据同步：将黑名单变更同步到其他系统
// - 关系恢复：在其他系统中恢复相关的用户关系
// - 审计日志：记录黑名单操作的详细日志
//
// 参数说明：
// - ctx: 请求上下文
// - after: 后置回调配置
// - req: 移除黑名单请求
//
// 回调数据：
// - CallbackCommand: 回调命令标识
// - OwnerUserID: 黑名单拥有者用户ID
// - BlackUserID: 被移除的用户ID
func (s *friendServer) webhookAfterRemoveBlack(ctx context.Context, after *config.AfterConfig, req *relation.RemoveBlackReq) {
	cbReq := &cbapi.CallbackAfterRemoveBlackReq{
		CallbackCommand: cbapi.CallbackAfterRemoveBlackCommand,
		OwnerUserID:     req.OwnerUserID,
		BlackUserID:     req.BlackUserID,
	}
	resp := &cbapi.CallbackAfterRemoveBlackResp{}
	s.webhookClient.AsyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, resp, after)
}

// webhookBeforeSetFriendRemark 设置好友备注前置回调
//
// 在设置好友备注前调用外部系统的回调接口，允许外部系统验证或修改备注内容。
//
// 业务场景：
// - 内容审核：检查备注内容是否包含敏感词汇
// - 长度限制：验证备注长度是否符合业务规则
// - 权限控制：检查用户是否有权限设置备注
// - 内容转换：将备注内容转换为标准格式
//
// 参数说明：
// - ctx: 请求上下文
// - before: 前置回调配置
// - req: 设置备注请求（可能被修改）
//
// 返回值：
// - error: 如果外部系统拒绝此次操作，返回错误终止流程
//
// 回调数据：
// - CallbackCommand: 回调命令标识
// - OwnerUserID: 备注设置者用户ID
// - FriendUserID: 被设置备注的好友用户ID
// - Remark: 原始备注内容
//
// 特殊功能：
// - 内容修改：外部系统可以通过响应修改备注内容
// - 流程控制：外部系统可以阻止备注设置操作
func (s *friendServer) webhookBeforeSetFriendRemark(ctx context.Context, before *config.BeforeConfig, req *relation.SetFriendRemarkReq) error {
	return webhook.WithCondition(ctx, before, func(ctx context.Context) error {
		cbReq := &cbapi.CallbackBeforeSetFriendRemarkReq{
			CallbackCommand: cbapi.CallbackBeforeSetFriendRemarkCommand,
			OwnerUserID:     req.OwnerUserID,
			FriendUserID:    req.FriendUserID,
			Remark:          req.Remark,
		}
		resp := &cbapi.CallbackBeforeSetFriendRemarkResp{}
		if err := s.webhookClient.SyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, resp, before); err != nil {
			return err
		}
		if resp.Remark != "" {
			req.Remark = resp.Remark
		}
		return nil
	})
}

// webhookBeforeAddBlack 添加黑名单前置回调
//
// 在添加用户到黑名单前调用外部系统的回调接口，允许外部系统根据业务规则
// 决定是否允许此次黑名单操作。
//
// 业务场景：
// - 权限控制：检查用户是否有权限将其他用户加入黑名单
// - 业务规则：实现复杂的黑名单添加业务逻辑
// - 关系检查：确保不会意外屏蔽重要联系人
// - 频率限制：控制用户添加黑名单的频率
//
// 参数说明：
// - ctx: 请求上下文
// - before: 前置回调配置
// - req: 添加黑名单请求
//
// 返回值：
// - error: 如果外部系统拒绝此次操作，返回错误终止流程
//
// 回调数据：
// - CallbackCommand: 回调命令标识
// - OwnerUserID: 黑名单拥有者用户ID
// - BlackUserID: 要添加到黑名单的用户ID
func (s *friendServer) webhookBeforeAddBlack(ctx context.Context, before *config.BeforeConfig, req *relation.AddBlackReq) error {
	return webhook.WithCondition(ctx, before, func(ctx context.Context) error {
		cbReq := &cbapi.CallbackBeforeAddBlackReq{
			CallbackCommand: cbapi.CallbackBeforeAddBlackCommand,
			OwnerUserID:     req.OwnerUserID,
			BlackUserID:     req.BlackUserID,
		}
		resp := &cbapi.CallbackBeforeAddBlackResp{}
		return s.webhookClient.SyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, resp, before)
	})
}

// webhookBeforeAddFriendAgree 好友申请同意前置回调
//
// 在处理好友申请（同意或拒绝）前调用外部系统的回调接口，允许外部系统
// 根据业务规则决定是否允许此次处理操作。
//
// 业务场景：
// - 自动处理：基于外部规则自动同意或拒绝申请
// - 权限控制：检查用户是否有权限处理好友申请
// - 业务逻辑：实现复杂的好友申请处理逻辑
// - 数据验证：验证处理请求的合法性
//
// 参数说明：
// - ctx: 请求上下文
// - before: 前置回调配置
// - req: 处理好友申请请求
//
// 返回值：
// - error: 如果外部系统拒绝此次操作，返回错误终止流程
//
// 回调数据：
// - CallbackCommand: 回调命令标识
// - FromUserID: 原申请发起方用户ID
// - ToUserID: 申请处理方用户ID
// - HandleMsg: 处理消息内容
// - HandleResult: 处理结果（同意/拒绝）
func (s *friendServer) webhookBeforeAddFriendAgree(ctx context.Context, before *config.BeforeConfig, req *relation.RespondFriendApplyReq) error {
	return webhook.WithCondition(ctx, before, func(ctx context.Context) error {
		cbReq := &cbapi.CallbackBeforeAddFriendAgreeReq{
			CallbackCommand: cbapi.CallbackBeforeAddFriendAgreeCommand,
			FromUserID:      req.FromUserID,
			ToUserID:        req.ToUserID,
			HandleMsg:       req.HandleMsg,
			HandleResult:    req.HandleResult,
		}
		resp := &cbapi.CallbackBeforeAddFriendAgreeResp{}
		return s.webhookClient.SyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, resp, before)
	})
}

// webhookAfterAddFriendAgree 好友申请处理后置回调
//
// 在好友申请处理完成后调用外部系统的回调接口，通知外部系统处理结果。
//
// 业务场景：
// - 数据同步：将处理结果同步到其他系统
// - 统计更新：更新好友申请处理的统计数据
// - 后续流程：触发基于处理结果的后续业务流程
// - 审计日志：记录好友申请处理的详细日志
//
// 参数说明：
// - ctx: 请求上下文
// - after: 后置回调配置
// - req: 处理好友申请请求
//
// 回调数据：
// - CallbackCommand: 回调命令标识
// - FromUserID: 原申请发起方用户ID
// - ToUserID: 申请处理方用户ID
// - HandleMsg: 处理消息内容
// - HandleResult: 处理结果（同意/拒绝）
func (s *friendServer) webhookAfterAddFriendAgree(ctx context.Context, after *config.AfterConfig, req *relation.RespondFriendApplyReq) {
	cbReq := &cbapi.CallbackAfterAddFriendAgreeReq{
		CallbackCommand: cbapi.CallbackAfterAddFriendAgreeCommand,
		FromUserID:      req.FromUserID,
		ToUserID:        req.ToUserID,
		HandleMsg:       req.HandleMsg,
		HandleResult:    req.HandleResult,
	}
	resp := &cbapi.CallbackAfterAddFriendAgreeResp{}
	s.webhookClient.AsyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, resp, after)
}

// webhookBeforeImportFriends 批量导入好友前置回调
//
// 在批量导入好友前调用外部系统的回调接口，允许外部系统验证或修改导入列表。
//
// 业务场景：
// - 权限控制：检查用户是否有权限进行批量导入
// - 数据过滤：过滤掉不符合条件的用户ID
// - 重复检查：避免导入已存在的好友关系
// - 数量限制：控制单次导入的好友数量
//
// 参数说明：
// - ctx: 请求上下文
// - before: 前置回调配置
// - req: 批量导入请求（可能被修改）
//
// 返回值：
// - error: 如果外部系统拒绝此次操作，返回错误终止流程
//
// 回调数据：
// - CallbackCommand: 回调命令标识
// - OwnerUserID: 好友关系拥有者用户ID
// - FriendUserIDs: 要导入的好友用户ID列表
//
// 特殊功能：
// - 列表修改：外部系统可以通过响应修改要导入的好友列表
// - 流程控制：外部系统可以阻止批量导入操作
func (s *friendServer) webhookBeforeImportFriends(ctx context.Context, before *config.BeforeConfig, req *relation.ImportFriendReq) error {
	return webhook.WithCondition(ctx, before, func(ctx context.Context) error {
		cbReq := &cbapi.CallbackBeforeImportFriendsReq{
			CallbackCommand: cbapi.CallbackBeforeImportFriendsCommand,
			OwnerUserID:     req.OwnerUserID,
			FriendUserIDs:   req.FriendUserIDs,
		}
		resp := &cbapi.CallbackBeforeImportFriendsResp{}
		if err := s.webhookClient.SyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, resp, before); err != nil {
			return err
		}
		if len(resp.FriendUserIDs) > 0 {
			req.FriendUserIDs = resp.FriendUserIDs
		}
		return nil
	})
}
