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

// Package relation 用户关系通知服务模块
//
// 本文件实现了OpenIM用户关系变更通知系统的核心功能，是保障多端数据同步的关键组件。
// 当用户关系发生变化时（如好友申请、好友关系变更、黑名单操作等），
// 系统会通过通知机制实时推送相关信息，确保所有端的数据一致性。
//
// 核心功能：
// 1. 好友申请通知：新的好友申请、申请处理结果通知
// 2. 好友关系通知：好友删除、好友信息变更、备注设置等
// 3. 黑名单通知：黑名单添加、移除通知
// 4. 用户信息通知：用户信息更新对好友的通知
//
// 通知类型：
// - FriendApplicationNotification: 好友申请通知
// - FriendApplicationApprovedNotification: 好友申请同意通知
// - FriendApplicationRejectedNotification: 好友申请拒绝通知
// - FriendDeletedNotification: 好友删除通知
// - FriendRemarkSetNotification: 好友备注设置通知
// - FriendsInfoUpdateNotification: 好友信息更新通知
// - BlackAddedNotification: 黑名单添加通知
// - BlackDeletedNotification: 黑名单移除通知
//
// 技术特点：
// - 异步推送：不阻塞主业务流程
// - 多端同步：确保所有设备实时同步
// - 版本管理：支持增量同步机制
// - 用户信息整合：自动获取并填充用户基本信息
// - 可靠推送：基于消息队列的可靠投递
package relation

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/rpcli"
	"github.com/openimsdk/protocol/msg"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/utils/datautil"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/versionctx"

	relationtb "github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/convert"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/controller"
	"github.com/openimsdk/open-im-server/v3/pkg/notification"
	"github.com/openimsdk/open-im-server/v3/pkg/notification/common_user"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/relation"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/mcontext"
)

// FriendNotificationSender 好友关系通知发送器
//
// 这是好友关系模块的通知发送器，负责处理所有与好友关系相关的通知推送。
// 它继承了通用的NotificationSender，专门用于好友关系相关的通知场景。
//
// 主要职责：
// 1. 封装好友关系相关的通知逻辑
// 2. 构造特定格式的通知消息
// 3. 通过消息服务发送通知
// 4. 管理用户信息获取和版本控制
//
// 通知流程：
// 1. 业务操作触发通知需求
// 2. 获取相关用户的基本信息
// 3. 构造通知消息内容
// 4. 通过消息服务异步推送
// 5. 更新版本信息支持增量同步
//
// 集成特性：
// - 用户信息自动获取：支持多种用户信息获取方式
// - 版本管理：与增量同步系统集成
// - 数据库集成：支持好友关系数据查询
type FriendNotificationSender struct {
	// 嵌入通用的通知发送器
	// 提供基础的通知发送能力和配置管理
	*notification.NotificationSender

	// getUsersInfo 用户信息获取函数
	// 这是一个可配置的回调函数，用于获取用户基本信息
	// 支持通过数据库查询或RPC调用等方式获取用户信息
	// 在发送通知时自动填充用户的昵称、头像等信息
	getUsersInfo func(ctx context.Context, userIDs []string) ([]common_user.CommonUser, error)

	// db 好友关系数据库控制器
	// 用于查询好友申请记录、好友关系等数据
	// 主要用途：
	// - 获取好友申请的详细信息
	// - 查询好友关系状态
	// - 支持通知内容的完整性
	db controller.FriendDatabase
}

// friendNotificationSenderOptions 好友通知发送器选项类型
// 用于配置FriendNotificationSender的各种选项，采用函数式选项模式
type friendNotificationSenderOptions func(*FriendNotificationSender)

// WithFriendDB 设置好友数据库控制器选项
//
// 为通知发送器配置好友数据库控制器，使其能够查询好友关系数据。
// 主要用于获取好友申请的详细信息，确保通知内容的完整性。
//
// 参数：
// - db: 好友数据库控制器实例
//
// 返回：
// - 配置选项函数
func WithFriendDB(db controller.FriendDatabase) friendNotificationSenderOptions {
	return func(s *FriendNotificationSender) {
		s.db = db
	}
}

// WithDBFunc 设置数据库函数选项
//
// 通过数据库查询函数配置用户信息获取方式。
// 这种方式直接查询数据库获取用户信息，适用于单体应用或数据库直连场景。
//
// 参数：
// - fn: 数据库查询函数，接收用户ID列表，返回用户信息列表
//
// 返回：
// - 配置选项函数
//
// 使用场景：
// - 单体应用直接查询数据库
// - 测试环境简化配置
// - 特殊场景的自定义用户信息获取
func WithDBFunc(fn func(ctx context.Context, userIDs []string) (users []*relationtb.User, err error)) friendNotificationSenderOptions {
	return func(s *FriendNotificationSender) {
		// 将数据库查询函数适配为通用的用户信息获取函数
		f := func(ctx context.Context, userIDs []string) (result []common_user.CommonUser, err error) {
			// 调用数据库查询函数获取用户信息
			users, err := fn(ctx, userIDs)
			if err != nil {
				return nil, err
			}

			// 将数据库用户模型转换为通用用户接口
			for _, user := range users {
				result = append(result, user)
			}
			return result, nil
		}
		s.getUsersInfo = f
	}
}

// WithRpcFunc 设置RPC函数选项
//
// 通过RPC调用配置用户信息获取方式。
// 这是推荐的方式，适用于微服务架构，通过用户服务RPC接口获取用户信息。
//
// 参数：
// - fn: RPC调用函数，接收用户ID列表，返回用户信息列表
//
// 返回：
// - 配置选项函数
//
// 使用场景：
// - 微服务架构（推荐）
// - 分布式系统
// - 需要实时用户信息的场景
//
// 优势：
// - 数据实时性：直接从用户服务获取最新信息
// - 服务解耦：不直接依赖用户数据库
// - 扩展性好：支持用户服务的独立扩展
func WithRpcFunc(fn func(ctx context.Context, userIDs []string) ([]*sdkws.UserInfo, error)) friendNotificationSenderOptions {
	return func(s *FriendNotificationSender) {
		// 将RPC调用函数适配为通用的用户信息获取函数
		f := func(ctx context.Context, userIDs []string) (result []common_user.CommonUser, err error) {
			// 调用RPC函数获取用户信息
			users, err := fn(ctx, userIDs)
			if err != nil {
				return nil, err
			}

			// 将RPC返回的用户信息转换为通用用户接口
			for _, user := range users {
				result = append(result, user)
			}
			return result, err
		}
		s.getUsersInfo = f
	}
}

// NewFriendNotificationSender 创建好友通知发送器
//
// 初始化一个专门用于好友关系通知的发送器实例。
// 采用函数式选项模式，支持灵活的配置。
//
// 参数：
// - conf: 通知配置信息，包含推送策略、重试机制等配置
// - msgClient: 消息服务客户端，用于实际的消息发送
// - opts: 可变数量的配置选项，用于定制发送器行为
//
// 返回：
// - 初始化完成的好友通知发送器实例
//
// 配置流程：
// 1. 创建基础通知发送器实例
// 2. 应用所有配置选项
// 3. 返回完整配置的发送器
func NewFriendNotificationSender(conf *config.Notification, msgClient *rpcli.MsgClient, opts ...friendNotificationSenderOptions) *FriendNotificationSender {
	// 1. 创建基础的好友通知发送器实例
	f := &FriendNotificationSender{
		// 使用消息客户端初始化通用通知发送器
		// WithRpcClient选项配置了消息发送的具体实现
		NotificationSender: notification.NewNotificationSender(conf, notification.WithRpcClient(func(ctx context.Context, req *msg.SendMsgReq) (*msg.SendMsgResp, error) {
			return msgClient.SendMsg(ctx, req)
		})),
	}

	// 2. 应用所有配置选项
	// 遍历所有选项函数，依次配置发送器的各种属性
	for _, opt := range opts {
		opt(f)
	}

	return f
}

// getUsersInfoMap 获取用户信息映射
//
// 这是一个内部辅助方法，用于批量获取用户信息并转换为Map格式。
// 主要用于通知内容的用户信息填充。
//
// 参数：
// - ctx: 请求上下文
// - userIDs: 要查询的用户ID列表
//
// 返回：
// - 用户ID到用户信息的映射
// - 错误信息
//
// 使用场景：
// - 通知内容需要显示用户昵称、头像等信息
// - 批量获取多个用户的信息
// - 提高查询效率，减少重复查询
func (f *FriendNotificationSender) getUsersInfoMap(ctx context.Context, userIDs []string) (map[string]*sdkws.UserInfo, error) {
	// 1. 通过配置的用户信息获取函数批量查询用户信息
	users, err := f.getUsersInfo(ctx, userIDs)
	if err != nil {
		return nil, err
	}

	// 2. 将用户信息列表转换为Map格式
	result := make(map[string]*sdkws.UserInfo)
	for _, user := range users {
		// 类型断言：将通用用户接口转换为具体的用户信息结构
		result[user.GetUserID()] = user.(*sdkws.UserInfo)
	}

	return result, nil
}

// getFromToUserNickname 获取发送方和接收方用户昵称
//
// 这是一个辅助方法，用于获取通知相关用户的昵称信息。
// 主要用于构造通知内容时的用户名显示。
//
// 参数：
// - ctx: 请求上下文
// - fromUserID: 发送方用户ID
// - toUserID: 接收方用户ID
//
// 返回：
// - 发送方用户昵称
// - 接收方用户昵称
// - 错误信息
//
// 注意：此方法当前标记为unused，可能在某些通知场景中使用
//
//nolint:unused
func (f *FriendNotificationSender) getFromToUserNickname(ctx context.Context, fromUserID, toUserID string) (string, string, error) {
	// 1. 批量获取用户信息
	users, err := f.getUsersInfoMap(ctx, []string{fromUserID, toUserID})
	if err != nil {
		// 如果获取用户信息失败，返回空字符串而不是错误
		// 这样可以确保通知依然能够发送，只是缺少用户昵称信息
		return "", "", nil
	}

	// 2. 提取用户昵称
	return users[fromUserID].Nickname, users[toUserID].Nickname, nil
}

// UserInfoUpdatedNotification 用户信息更新通知
//
// 当用户信息发生变更时，向指定用户发送通知。
// 这个通知主要用于通知其他用户某个用户的信息已更新。
//
// 应用场景：
// - 用户修改昵称、头像等基本信息
// - 需要通知相关好友信息已更新
// - 触发客户端刷新用户信息缓存
//
// 参数：
// - ctx: 请求上下文
// - changedUserID: 信息发生变更的用户ID
//
// 通知内容：
// - 包含发生变更的用户ID
// - 触发客户端更新相应的用户信息显示
func (f *FriendNotificationSender) UserInfoUpdatedNotification(ctx context.Context, changedUserID string) {
	// 构造用户信息更新通知的数据结构
	tips := sdkws.UserInfoUpdatedTips{UserID: changedUserID}

	// 发送通知消息
	// 通知类型：UserInfoUpdatedNotification
	// 发送者：当前操作用户（通常是管理员或用户本人）
	// 接收者：信息发生变更的用户
	f.Notification(ctx, mcontext.GetOpUserID(ctx), changedUserID, constant.UserInfoUpdatedNotification, &tips)
}

// getCommonUserMap 获取通用用户映射
//
// 将用户信息列表转换为通用用户接口的映射格式。
// 这是一个内部辅助方法，用于统一用户信息的处理。
//
// 参数：
// - ctx: 请求上下文
// - userIDs: 要查询的用户ID列表
//
// 返回：
// - 用户ID到通用用户接口的映射
// - 错误信息
//
// 设计目的：
// - 提供统一的用户信息获取接口
// - 支持不同的用户信息源（数据库、RPC等）
// - 简化通知内容的用户信息处理
func (f *FriendNotificationSender) getCommonUserMap(ctx context.Context, userIDs []string) (map[string]common_user.CommonUser, error) {
	// 1. 通过配置的用户信息获取函数批量查询用户信息
	users, err := f.getUsersInfo(ctx, userIDs)
	if err != nil {
		return nil, err
	}

	// 2. 将用户信息列表转换为Map格式
	// 使用datautil.SliceToMap工具函数提高转换效率
	return datautil.SliceToMap(users, func(e common_user.CommonUser) string {
		return e.GetUserID()
	}), nil
}

// getFriendRequests 获取好友申请详细信息
//
// 查询指定用户之间的好友申请记录，用于构造通知内容。
// 这个方法确保通知包含完整的好友申请信息。
//
// 参数：
// - ctx: 请求上下文
// - fromUserID: 申请发起方用户ID
// - toUserID: 申请目标方用户ID
//
// 返回：
// - 好友申请详细信息
// - 错误信息
//
// 查询逻辑：
// 1. 查询双方的好友申请记录
// 2. 转换为API格式并填充用户信息
// 3. 找到匹配的申请记录并返回
//
// 错误处理：
// - 数据库查询失败
// - 申请记录不存在
// - 用户信息获取失败
func (f *FriendNotificationSender) getFriendRequests(ctx context.Context, fromUserID, toUserID string) (*sdkws.FriendRequest, error) {
	// 1. 检查数据库控制器是否已配置
	if f.db == nil {
		return nil, errs.ErrInternalServer.WithDetail("db is nil")
	}

	// 2. 查询双方的好友申请记录
	// FindBothFriendRequests方法查询fromUserID和toUserID之间的所有申请记录
	friendRequests, err := f.db.FindBothFriendRequests(ctx, fromUserID, toUserID)
	if err != nil {
		return nil, err
	}

	// 3. 转换申请记录为API格式并填充用户信息
	// convert.FriendRequestDB2Pb方法将数据库模型转换为API格式
	// 同时通过getCommonUserMap获取并填充用户基本信息
	requests, err := convert.FriendRequestDB2Pb(ctx, friendRequests, f.getCommonUserMap)
	if err != nil {
		return nil, err
	}

	// 4. 查找匹配的申请记录
	// 遍历所有申请记录，找到方向匹配的记录
	for _, request := range requests {
		if request.FromUserID == fromUserID && request.ToUserID == toUserID {
			return request, nil
		}
	}

	// 5. 如果没有找到匹配的申请记录，返回错误
	return nil, errs.ErrRecordNotFound.WrapMsg("friend request not found", "fromUserID", fromUserID, "toUserID", toUserID)
}

// FriendApplicationAddNotification 好友申请添加通知
//
// 当用户发起好友申请时，向目标用户发送通知。
// 这是好友系统中最重要的通知之一，确保用户能及时知道有新的好友申请。
//
// 应用场景：
// - 用户A向用户B发起好友申请
// - 系统向用户B推送好友申请通知
// - 客户端显示新的好友申请消息
//
// 参数：
// - ctx: 请求上下文
// - req: 好友申请请求，包含申请相关信息
//
// 通知内容：
// - 申请发起方和目标方的用户ID
// - 完整的好友申请信息（包含用户基本信息）
// - 申请消息和时间等详细信息
//
// 错误处理：
// - 如果获取申请详细信息失败，记录错误日志但不阻塞流程
// - 这样设计确保即使通知发送失败，主业务逻辑依然可以继续
func (f *FriendNotificationSender) FriendApplicationAddNotification(ctx context.Context, req *relation.ApplyToAddFriendReq) {
	// 1. 获取好友申请的详细信息
	// 包含申请的完整信息，如申请消息、时间、用户信息等
	request, err := f.getFriendRequests(ctx, req.FromUserID, req.ToUserID)
	if err != nil {
		// 如果获取申请信息失败，记录错误日志
		// 但不返回错误，避免影响主业务流程
		log.ZError(ctx, "FriendApplicationAddNotification get friend request", err, "fromUserID", req.FromUserID, "toUserID", req.ToUserID)
		return
	}

	// 2. 构造好友申请通知的数据结构
	tips := sdkws.FriendApplicationTips{
		// 申请相关的用户ID信息
		FromToUserID: &sdkws.FromToUserID{
			FromUserID: req.FromUserID, // 申请发起方
			ToUserID:   req.ToUserID,   // 申请目标方
		},
		// 完整的好友申请信息
		Request: request,
	}

	// 3. 发送好友申请通知
	// 通知类型：FriendApplicationNotification
	// 发送者：申请发起方（FromUserID）
	// 接收者：申请目标方（ToUserID）
	f.Notification(ctx, req.FromUserID, req.ToUserID, constant.FriendApplicationNotification, &tips)
}

// FriendApplicationAgreedNotification 好友申请同意通知
//
// 当用户同意好友申请时，向申请发起方发送通知。
// 告知申请发起方其好友申请已被同意，双方现在是好友关系。
//
// 应用场景：
// - 用户B同意了用户A的好友申请
// - 系统向用户A推送申请被同意的通知
// - 客户端更新好友列表并显示成功消息
//
// 参数：
// - ctx: 请求上下文
// - req: 好友申请处理请求，包含处理结果和相关信息
//
// 通知内容：
// - 申请处理的双方用户ID
// - 处理消息（可能包含感谢话语等）
// - 原始申请的详细信息
//
// 业务影响：
// - 触发客户端刷新好友列表
// - 更新会话列表（可能创建新的会话）
// - 显示好友申请成功的提示
func (f *FriendNotificationSender) FriendApplicationAgreedNotification(ctx context.Context, req *relation.RespondFriendApplyReq) {
	// 1. 获取原始好友申请的详细信息
	// 确保通知包含完整的申请上下文信息
	request, err := f.getFriendRequests(ctx, req.FromUserID, req.ToUserID)
	if err != nil {
		// 如果获取申请信息失败，记录错误日志
		log.ZError(ctx, "FriendApplicationAgreedNotification get friend request", err, "fromUserID", req.FromUserID, "toUserID", req.ToUserID)
		return
	}

	// 2. 构造好友申请同意通知的数据结构
	tips := sdkws.FriendApplicationApprovedTips{
		// 申请相关的用户ID信息
		FromToUserID: &sdkws.FromToUserID{
			FromUserID: req.FromUserID, // 原申请发起方
			ToUserID:   req.ToUserID,   // 原申请目标方（现在的处理方）
		},
		HandleMsg: req.HandleMsg, // 处理消息（如："很高兴认识你"）
		Request:   request,       // 原始申请的完整信息
	}

	// 3. 发送好友申请同意通知
	// 通知类型：FriendApplicationApprovedNotification
	// 发送者：申请处理方（ToUserID）
	// 接收者：原申请发起方（FromUserID）
	f.Notification(ctx, req.ToUserID, req.FromUserID, constant.FriendApplicationApprovedNotification, &tips)
}

// FriendApplicationRefusedNotification 好友申请拒绝通知
//
// 当用户拒绝好友申请时，向申请发起方发送通知。
// 告知申请发起方其好友申请已被拒绝。
//
// 应用场景：
// - 用户B拒绝了用户A的好友申请
// - 系统向用户A推送申请被拒绝的通知
// - 客户端显示申请被拒绝的消息
//
// 参数：
// - ctx: 请求上下文
// - req: 好友申请处理请求，包含拒绝原因等信息
//
// 通知内容：
// - 申请处理的双方用户ID
// - 拒绝理由或处理消息
// - 原始申请的详细信息
//
// 用户体验考虑：
// - 通知内容应该礼貌和友善
// - 可能包含"暂时不方便添加好友"等温和的拒绝信息
// - 不会过度打扰用户
func (f *FriendNotificationSender) FriendApplicationRefusedNotification(ctx context.Context, req *relation.RespondFriendApplyReq) {
	// 1. 获取原始好友申请的详细信息
	request, err := f.getFriendRequests(ctx, req.FromUserID, req.ToUserID)
	if err != nil {
		// 如果获取申请信息失败，记录错误日志
		log.ZError(ctx, "FriendApplicationRefusedNotification get friend request", err, "fromUserID", req.FromUserID, "toUserID", req.ToUserID)
		return
	}

	// 2. 构造好友申请拒绝通知的数据结构
	tips := sdkws.FriendApplicationRejectedTips{
		// 申请相关的用户ID信息
		FromToUserID: &sdkws.FromToUserID{
			FromUserID: req.FromUserID, // 原申请发起方
			ToUserID:   req.ToUserID,   // 原申请目标方（现在的处理方）
		},
		HandleMsg: req.HandleMsg, // 拒绝理由或处理消息
		Request:   request,       // 原始申请的完整信息
	}

	// 3. 发送好友申请拒绝通知
	// 通知类型：FriendApplicationRejectedNotification
	// 发送者：申请处理方（ToUserID）
	// 接收者：原申请发起方（FromUserID）
	f.Notification(ctx, req.ToUserID, req.FromUserID, constant.FriendApplicationRejectedNotification, &tips)
}

//func (f *FriendNotificationSender) FriendAddedNotification(ctx context.Context, operationID, opUserID, fromUserID, toUserID string) error {
//	tips := sdkws.FriendAddedTips{Friend: &sdkws.FriendInfo{}, OpUser: &sdkws.PublicUserInfo{}}
//	user, err := f.getUsersInfo(ctx, []string{opUserID})
//	if err != nil {
//		return err
//	}
//	tips.OpUser.UserID = user[0].GetUserID()
//	tips.OpUser.Ex = user[0].GetEx()
//	tips.OpUser.Nickname = user[0].GetNickname()
//	tips.OpUser.FaceURL = user[0].GetFaceURL()
//	friends, err := f.db.FindFriendsWithError(ctx, fromUserID, []string{toUserID})
//	if err != nil {
//		return err
//	}
//	tips.Friend, err = convert.FriendDB2Pb(ctx, friends[0], f.getUsersInfoMap)
//	if err != nil {
//		return err
//	}
//	f.Notification(ctx, fromUserID, toUserID, constant.FriendAddedNotification, &tips)
//	return nil
//}

func (f *FriendNotificationSender) FriendDeletedNotification(ctx context.Context, req *relation.DeleteFriendReq) {
	tips := sdkws.FriendDeletedTips{FromToUserID: &sdkws.FromToUserID{
		FromUserID: req.OwnerUserID,
		ToUserID:   req.FriendUserID,
	}}
	f.Notification(ctx, req.OwnerUserID, req.FriendUserID, constant.FriendDeletedNotification, &tips)
}

func (f *FriendNotificationSender) setVersion(ctx context.Context, version *uint64, versionID *string, collName string, id string) {
	versions := versionctx.GetVersionLog(ctx).Get()
	for _, coll := range versions {
		if coll.Name == collName && coll.Doc.DID == id {
			*version = uint64(coll.Doc.Version)
			*versionID = coll.Doc.ID.Hex()
			return
		}
	}
}

func (f *FriendNotificationSender) setSortVersion(ctx context.Context, version *uint64, versionID *string, collName string, id string, sortVersion *uint64) {
	versions := versionctx.GetVersionLog(ctx).Get()
	for _, coll := range versions {
		if coll.Name == collName && coll.Doc.DID == id {
			*version = uint64(coll.Doc.Version)
			*versionID = coll.Doc.ID.Hex()
			for _, elem := range coll.Doc.Logs {
				if elem.EID == relationtb.VersionSortChangeID {
					*sortVersion = uint64(elem.Version)
				}
			}
		}
	}
}

func (f *FriendNotificationSender) FriendRemarkSetNotification(ctx context.Context, fromUserID, toUserID string) {
	tips := sdkws.FriendInfoChangedTips{FromToUserID: &sdkws.FromToUserID{}}
	tips.FromToUserID.FromUserID = fromUserID
	tips.FromToUserID.ToUserID = toUserID
	f.setSortVersion(ctx, &tips.FriendVersion, &tips.FriendVersionID, database.FriendVersionName, toUserID, &tips.FriendSortVersion)
	f.Notification(ctx, fromUserID, toUserID, constant.FriendRemarkSetNotification, &tips)
}

func (f *FriendNotificationSender) FriendsInfoUpdateNotification(ctx context.Context, toUserID string, friendIDs []string) {
	tips := sdkws.FriendsInfoUpdateTips{FromToUserID: &sdkws.FromToUserID{}}
	tips.FromToUserID.ToUserID = toUserID
	tips.FriendIDs = friendIDs
	f.Notification(ctx, toUserID, toUserID, constant.FriendsInfoUpdateNotification, &tips)
}

func (f *FriendNotificationSender) BlackAddedNotification(ctx context.Context, req *relation.AddBlackReq) {
	tips := sdkws.BlackAddedTips{FromToUserID: &sdkws.FromToUserID{}}
	tips.FromToUserID.FromUserID = req.OwnerUserID
	tips.FromToUserID.ToUserID = req.BlackUserID
	f.Notification(ctx, req.OwnerUserID, req.BlackUserID, constant.BlackAddedNotification, &tips)
}

func (f *FriendNotificationSender) BlackDeletedNotification(ctx context.Context, req *relation.RemoveBlackReq) {
	blackDeletedTips := sdkws.BlackDeletedTips{FromToUserID: &sdkws.FromToUserID{
		FromUserID: req.OwnerUserID,
		ToUserID:   req.BlackUserID,
	}}
	f.Notification(ctx, req.OwnerUserID, req.BlackUserID, constant.BlackDeletedNotification, &blackDeletedTips)
}

func (f *FriendNotificationSender) FriendInfoUpdatedNotification(ctx context.Context, changedUserID string, needNotifiedUserID string) {
	tips := sdkws.UserInfoUpdatedTips{UserID: changedUserID}
	f.Notification(ctx, mcontext.GetOpUserID(ctx), needNotifiedUserID, constant.FriendInfoUpdatedNotification, &tips)
}
