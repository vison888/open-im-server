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

// Package relation 用户关系服务模块 - 黑名单管理
//
// 本文件实现了OpenIM黑名单管理系统的核心功能，是用户关系管理的重要组成部分。
// 黑名单功能允许用户屏蔽不想接收消息的其他用户，提供了更好的用户体验。
//
// 核心功能：
// 1. 黑名单添加：将指定用户添加到黑名单
// 2. 黑名单移除：从黑名单中移除指定用户
// 3. 黑名单查询：分页查询用户的黑名单列表
// 4. 黑名单验证：检查两个用户之间的黑名单关系
// 5. 指定黑名单查询：查询指定用户的黑名单状态
//
// 黑名单效果：
// - 阻止被屏蔽用户发送消息
// - 隐藏被屏蔽用户的在线状态
// - 防止被屏蔽用户发起好友申请
// - 在群聊中减少被屏蔽用户的消息显示
//
// 技术特点：
// - 双向黑名单检查：支持相互屏蔽
// - 缓存优化：热点黑名单关系缓存
// - 权限控制：严格的用户权限验证
// - 通知机制：黑名单变更实时通知
// - Webhook集成：支持外部系统回调
package relation

import (
	"context"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"

	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/open-im-server/v3/pkg/common/convert"
	"github.com/openimsdk/protocol/relation"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/mcontext"
	"github.com/openimsdk/tools/utils/datautil"
)

// GetPaginationBlacks 分页获取用户黑名单列表
//
// 获取指定用户的黑名单列表，支持分页查询。返回的黑名单信息包含
// 被屏蔽用户的基本信息、添加黑名单的时间、操作者等详细信息。
//
// 业务流程：
// 1. 权限验证：确认用户有权限查询黑名单（只能查询自己的黑名单）
// 2. 数据库查询：从数据库分页查询黑名单记录
// 3. 用户信息填充：获取被屏蔽用户的基本信息
// 4. 数据转换：将数据库模型转换为API响应格式
//
// 参数说明：
// - req.UserID: 要查询黑名单的用户ID
// - req.Pagination: 分页参数（页码、每页数量）
//
// 返回数据：
// - 黑名单总数
// - 当前页的黑名单列表（包含用户基本信息）
//
// 权限控制：
// - 用户只能查询自己的黑名单
// - 管理员可以查询任何用户的黑名单
func (s *friendServer) GetPaginationBlacks(ctx context.Context, req *relation.GetPaginationBlacksReq) (resp *relation.GetPaginationBlacksResp, err error) {
	// 1. 权限验证：检查用户是否有权限查询指定用户的黑名单
	// CheckAccessV3确保用户只能查询自己的数据，或者管理员可以查询任何用户的数据
	if err := authverify.CheckAccessV3(ctx, req.UserID, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	// 2. 从数据库分页查询黑名单记录
	// FindOwnerBlacks方法根据用户ID和分页参数查询黑名单数据
	total, blacks, err := s.blackDatabase.FindOwnerBlacks(ctx, req.UserID, req.Pagination)
	if err != nil {
		return nil, err
	}

	// 3. 初始化响应结构
	resp = &relation.GetPaginationBlacksResp{}

	// 4. 转换黑名单数据并填充用户信息
	// convert.BlackDB2Pb方法将数据库模型转换为API格式，同时通过GetUsersInfoMap获取用户基本信息
	resp.Blacks, err = convert.BlackDB2Pb(ctx, blacks, s.userClient.GetUsersInfoMap)
	if err != nil {
		return nil, err
	}

	// 5. 设置总数
	resp.Total = int32(total)

	return resp, nil
}

// IsBlack 检查两个用户之间的黑名单关系
//
// 检查两个用户是否存在相互的黑名单关系。支持双向检查，
// 即检查用户A是否在用户B的黑名单中，以及用户B是否在用户A的黑名单中。
//
// 应用场景：
// - 消息发送前检查：防止向已屏蔽的用户发送消息
// - 好友申请检查：防止被屏蔽的用户发起好友申请
// - 群聊消息过滤：在群聊中过滤被屏蔽用户的消息
// - 在线状态隐藏：对被屏蔽用户隐藏在线状态
//
// 参数说明：
// - req.UserID1: 第一个用户ID
// - req.UserID2: 第二个用户ID
//
// 返回数据：
// - InUser1Blacks: UserID2是否在UserID1的黑名单中
// - InUser2Blacks: UserID1是否在UserID2的黑名单中
//
// 性能优化：
// - 使用缓存加速查询
// - 批量查询减少数据库访问
func (s *friendServer) IsBlack(ctx context.Context, req *relation.IsBlackReq) (*relation.IsBlackResp, error) {
	// 1. 检查双向黑名单关系
	// CheckIn方法同时检查两个用户是否互相在对方的黑名单中
	// in1: UserID2是否在UserID1的黑名单中
	// in2: UserID1是否在UserID2的黑名单中
	in1, in2, err := s.blackDatabase.CheckIn(ctx, req.UserID1, req.UserID2)
	if err != nil {
		return nil, err
	}

	// 2. 构造响应结果
	resp := &relation.IsBlackResp{}
	resp.InUser1Blacks = in1 // UserID2是否在UserID1的黑名单中
	resp.InUser2Blacks = in2 // UserID1是否在UserID2的黑名单中

	return resp, nil
}

// RemoveBlack 从黑名单中移除用户
//
// 将指定用户从黑名单中移除，恢复正常的用户关系。
// 移除后，被移除的用户可以重新发送消息、发起好友申请等。
//
// 业务流程：
// 1. 权限验证：确认操作者有权限移除黑名单
// 2. 数据库删除：从数据库中删除黑名单记录
// 3. 通知推送：通知相关用户黑名单状态变更
// 4. Webhook回调：通知外部系统黑名单变更
//
// 参数说明：
// - req.OwnerUserID: 黑名单的拥有者用户ID
// - req.BlackUserID: 要从黑名单中移除的用户ID
//
// 安全考虑：
// - 权限验证确保用户只能操作自己的黑名单
// - 防止恶意移除他人黑名单
//
// 通知机制：
// - 向相关用户发送黑名单变更通知
// - 支持多端实时同步
func (s *friendServer) RemoveBlack(ctx context.Context, req *relation.RemoveBlackReq) (*relation.RemoveBlackResp, error) {
	// 1. 权限验证：检查用户是否有权限移除指定的黑名单记录
	// 确保用户只能移除自己添加的黑名单
	if err := authverify.CheckAccessV3(ctx, req.OwnerUserID, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	// 2. 从数据库中删除黑名单记录
	// 构造黑名单模型并调用Delete方法删除
	if err := s.blackDatabase.Delete(ctx, []*model.Black{{OwnerUserID: req.OwnerUserID, BlockUserID: req.BlackUserID}}); err != nil {
		return nil, err
	}

	// 3. 发送黑名单移除通知
	// 通知相关用户黑名单状态已变更，支持多端同步
	s.notificationSender.BlackDeletedNotification(ctx, req)

	// 4. Webhook后置回调：通知外部系统黑名单已移除
	// 异步调用，不会阻塞主流程
	s.webhookAfterRemoveBlack(ctx, &s.config.WebhooksConfig.AfterRemoveBlack, req)

	return &relation.RemoveBlackResp{}, nil
}

// AddBlack 添加用户到黑名单
//
// 将指定用户添加到黑名单中，阻止其发送消息和进行其他互动。
// 这是黑名单功能的核心方法，实现了完整的黑名单添加流程。
//
// 业务流程：
// 1. 权限验证：确认操作者有权限添加黑名单
// 2. Webhook前置回调：允许外部系统干预添加流程
// 3. 用户有效性验证：确认要添加的用户存在
// 4. 黑名单记录创建：在数据库中创建黑名单记录
// 5. 通知推送：通知相关用户黑名单状态变更
//
// 参数说明：
// - req.OwnerUserID: 黑名单的拥有者用户ID
// - req.BlackUserID: 要添加到黑名单的用户ID
// - req.Ex: 扩展字段，用于存储自定义数据
//
// 数据记录：
// - 记录操作者信息
// - 记录添加时间
// - 支持扩展字段
//
// 安全机制：
// - 严格的权限验证
// - 用户有效性检查
// - Webhook前置验证
func (s *friendServer) AddBlack(ctx context.Context, req *relation.AddBlackReq) (*relation.AddBlackResp, error) {
	// 1. 权限验证：检查用户是否有权限添加黑名单
	// 确保用户只能管理自己的黑名单
	if err := authverify.CheckAccessV3(ctx, req.OwnerUserID, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	// 2. Webhook前置回调：在添加黑名单前调用外部系统
	// 允许外部系统根据业务逻辑决定是否允许此次黑名单操作
	if err := s.webhookBeforeAddBlack(ctx, &s.config.WebhooksConfig.BeforeAddBlack, req); err != nil {
		return nil, err
	}

	// 3. 用户有效性验证：确认要添加的用户和操作者都存在于系统中
	// 防止添加不存在的用户到黑名单
	if err := s.userClient.CheckUser(ctx, []string{req.OwnerUserID, req.BlackUserID}); err != nil {
		return nil, err
	}

	// 4. 构造黑名单记录
	// 创建完整的黑名单数据模型，包含所有必要信息
	black := model.Black{
		OwnerUserID:    req.OwnerUserID,           // 黑名单拥有者
		BlockUserID:    req.BlackUserID,           // 被屏蔽的用户
		OperatorUserID: mcontext.GetOpUserID(ctx), // 操作者（通常是拥有者本人）
		CreateTime:     time.Now(),                // 创建时间
		Ex:             req.Ex,                    // 扩展字段
	}

	// 5. 将黑名单记录保存到数据库
	// 使用Create方法持久化存储黑名单关系
	if err := s.blackDatabase.Create(ctx, []*model.Black{&black}); err != nil {
		return nil, err
	}

	// 6. 发送黑名单添加通知
	// 通知相关用户黑名单状态已变更，支持多端同步
	s.notificationSender.BlackAddedNotification(ctx, req)

	return &relation.AddBlackResp{}, nil
}

// GetSpecifiedBlacks 获取指定用户的黑名单状态
//
// 查询指定用户列表中哪些用户在当前用户的黑名单中。
// 这个接口主要用于批量检查用户的黑名单状态，常用于用户列表显示时的状态标识。
//
// 应用场景：
// - 用户列表显示：在用户列表中标识哪些用户已被屏蔽
// - 批量状态检查：一次性检查多个用户的黑名单状态
// - 消息列表过滤：过滤掉被屏蔽用户的消息
// - 搜索结果过滤：在搜索结果中标识被屏蔽的用户
//
// 业务流程：
// 1. 权限验证：确认操作者有权限查询黑名单状态
// 2. 参数校验：验证用户ID列表的合法性
// 3. 用户信息获取：批量获取用户基本信息
// 4. 黑名单状态查询：查询指定用户的黑名单关系
// 5. 数据组装：组装用户信息和黑名单状态
//
// 参数说明：
// - req.OwnerUserID: 黑名单的拥有者用户ID
// - req.UserIDList: 要检查黑名单状态的用户ID列表
//
// 返回数据：
// - 包含用户基本信息和黑名单状态的完整列表
// - 只返回确实在黑名单中的用户信息
//
// 性能优化：
// - 批量查询用户信息
// - 批量查询黑名单状态
// - 使用Map提高查找效率
func (s *friendServer) GetSpecifiedBlacks(ctx context.Context, req *relation.GetSpecifiedBlacksReq) (*relation.GetSpecifiedBlacksResp, error) {
	// 1. 权限验证：检查用户是否有权限查询指定用户的黑名单状态
	if err := authverify.CheckAccessV3(ctx, req.OwnerUserID, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	// 2. 参数校验：检查用户ID列表是否为空
	if len(req.UserIDList) == 0 {
		return nil, errs.ErrArgs.WrapMsg("userIDList is empty")
	}

	// 3. 检查用户ID列表是否有重复
	// 防止重复的用户ID影响查询效率和结果准确性
	if datautil.Duplicate(req.UserIDList) {
		return nil, errs.ErrArgs.WrapMsg("userIDList repeated")
	}

	// 4. 批量获取用户基本信息
	// 获取所有待检查用户的基本信息，用于后续组装响应数据
	userMap, err := s.userClient.GetUsersInfoMap(ctx, req.UserIDList)
	if err != nil {
		return nil, err
	}

	// 5. 查询黑名单信息
	// FindBlackInfos方法查询指定用户列表中哪些用户在黑名单中
	blacks, err := s.blackDatabase.FindBlackInfos(ctx, req.OwnerUserID, req.UserIDList)
	if err != nil {
		return nil, err
	}

	// 6. 将黑名单列表转换为Map，提高查找效率
	// 以被屏蔽用户ID为key，黑名单记录为value
	blackMap := datautil.SliceToMap(blacks, func(e *model.Black) string {
		return e.BlockUserID
	})

	// 7. 初始化响应结构
	resp := &relation.GetSpecifiedBlacksResp{
		Blacks: make([]*sdkws.BlackInfo, 0, len(req.UserIDList)),
	}

	// 8. 定义用户信息转换函数
	// 将用户基本信息转换为公开用户信息格式
	toPublcUser := func(userID string) *sdkws.PublicUserInfo {
		v, ok := userMap[userID]
		if !ok {
			return nil
		}
		return &sdkws.PublicUserInfo{
			UserID:   v.UserID,
			Nickname: v.Nickname,
			FaceURL:  v.FaceURL,
			Ex:       v.Ex,
		}
	}

	// 9. 遍历用户ID列表，组装黑名单信息
	for _, userID := range req.UserIDList {
		if black := blackMap[userID]; black != nil {
			// 如果用户在黑名单中，则添加到响应列表
			resp.Blacks = append(resp.Blacks,
				&sdkws.BlackInfo{
					OwnerUserID:    black.OwnerUserID,            // 黑名单拥有者
					CreateTime:     black.CreateTime.UnixMilli(), // 创建时间（毫秒时间戳）
					BlackUserInfo:  toPublcUser(userID),          // 被屏蔽用户的公开信息
					AddSource:      black.AddSource,              // 添加来源
					OperatorUserID: black.OperatorUserID,         // 操作者
					Ex:             black.Ex,                     // 扩展字段
				})
		}
	}

	// 10. 设置返回的黑名单总数
	resp.Total = int32(len(resp.Blacks))

	return resp, nil
}
