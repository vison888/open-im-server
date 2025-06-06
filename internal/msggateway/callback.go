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

// callback.go - Webhook回调处理模块
//
// 功能概述:
// 1. 处理用户状态变更的webhook回调通知
// 2. 支持用户上线、下线、踢下线等事件的异步通知
// 3. 与外部系统集成，实现事件驱动的架构
//
// 设计思路:
// - 采用异步推送机制，避免阻塞主业务流程
// - 统一的回调结构体设计，便于扩展和维护
// - 支持配置化的回调地址和参数
// - 自动生成唯一的操作ID，便于追踪和调试
//
// 主要特性:
// - 事件驱动: 基于用户状态变更触发回调
// - 异步处理: 不阻塞用户连接和消息处理
// - 统一格式: 标准化的回调请求格式
// - 可扩展性: 易于添加新的回调事件类型

package msggateway

import (
	"context"
	"time"

	cbapi "github.com/openimsdk/open-im-server/v3/pkg/callbackstruct"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/tools/mcontext"
)

// webhookAfterUserOnline 用户上线后的webhook回调处理
// 设计思路：
// 1. 在用户成功建立WebSocket连接后触发
// 2. 异步发送通知，不影响用户连接性能
// 3. 包含完整的用户上线信息：用户ID、平台、后台状态等
// 4. 支持连接唯一标识，便于问题追踪
//
// 应用场景：
// - 好友在线状态通知
// - 用户活跃度统计
// - 第三方系统集成
// - 业务日志记录
//
// 参数：
//   - ctx: 上下文信息，包含操作ID等
//   - after: 回调配置信息，包含URL和超时设置
//   - userID: 上线用户的唯一标识
//   - platformID: 用户登录的平台ID（iOS、Android、Web等）
//   - isAppBackground: 是否为应用后台模式
//   - connID: WebSocket连接的唯一标识
func (ws *WsServer) webhookAfterUserOnline(ctx context.Context, after *config.AfterConfig, userID string, platformID int, isAppBackground bool, connID string) {
	// 构建用户上线回调请求
	req := cbapi.CallbackUserOnlineReq{
		UserStatusCallbackReq: cbapi.UserStatusCallbackReq{
			UserStatusBaseCallback: cbapi.UserStatusBaseCallback{
				CallbackCommand: cbapi.CallbackAfterUserOnlineCommand,  // 回调命令类型
				OperationID:     mcontext.GetOperationID(ctx),          // 操作ID，便于追踪
				PlatformID:      platformID,                            // 平台ID
				Platform:        constant.PlatformIDToName(platformID), // 平台名称
			},
			UserID: userID, // 用户ID
		},
		Seq:             time.Now().UnixMilli(), // 时间戳序列号，确保事件顺序
		IsAppBackground: isAppBackground,        // 是否后台模式
		ConnID:          connID,                 // 连接ID
	}
	// 异步发送webhook回调，避免阻塞主流程
	ws.webhookClient.AsyncPost(ctx, req.GetCallbackCommand(), req, &cbapi.CommonCallbackResp{}, after)
}

// webhookAfterUserOffline 用户下线后的webhook回调处理
// 设计思路：
// 1. 在用户WebSocket连接断开后触发
// 2. 记录用户下线的完整信息，便于统计分析
// 3. 支持主动下线和异常断线的区分
// 4. 提供连接级别的精确追踪
//
// 应用场景：
// - 好友离线状态更新
// - 用户在线时长统计
// - 异常断线监控
// - 会话结束处理
//
// 参数：
//   - ctx: 上下文信息
//   - after: 回调配置信息
//   - userID: 下线用户的唯一标识
//   - platformID: 用户下线的平台ID
//   - connID: 断开的WebSocket连接标识
func (ws *WsServer) webhookAfterUserOffline(ctx context.Context, after *config.AfterConfig, userID string, platformID int, connID string) {
	req := &cbapi.CallbackUserOfflineReq{
		UserStatusCallbackReq: cbapi.UserStatusCallbackReq{
			UserStatusBaseCallback: cbapi.UserStatusBaseCallback{
				CallbackCommand: cbapi.CallbackAfterUserOfflineCommand, // 用户下线回调命令
				OperationID:     mcontext.GetOperationID(ctx),
				PlatformID:      platformID,
				Platform:        constant.PlatformIDToName(platformID),
			},
			UserID: userID,
		},
		Seq:    time.Now().UnixMilli(), // 下线时间戳
		ConnID: connID,                 // 断开的连接ID
	}
	ws.webhookClient.AsyncPost(ctx, req.GetCallbackCommand(), req, &cbapi.CallbackUserOfflineResp{}, after)
}

// webhookAfterUserKickOff 用户被踢下线后的webhook回调处理
// 设计思路：
// 1. 在系统主动踢下线用户后触发
// 2. 区别于用户主动断线，属于系统管理行为
// 3. 常用于多端登录策略执行和安全管控
// 4. 提供完整的踢下线事件记录
//
// 应用场景：
// - 多端登录冲突处理
// - 用户权限变更后的强制下线
// - 安全策略执行（如异常登录检测）
// - 系统维护时的用户疏散
//
// 参数：
//   - ctx: 上下文信息
//   - after: 回调配置信息
//   - userID: 被踢下线的用户ID
//   - platformID: 被踢下线的平台ID
func (ws *WsServer) webhookAfterUserKickOff(ctx context.Context, after *config.AfterConfig, userID string, platformID int) {
	req := &cbapi.CallbackUserKickOffReq{
		UserStatusCallbackReq: cbapi.UserStatusCallbackReq{
			UserStatusBaseCallback: cbapi.UserStatusBaseCallback{
				CallbackCommand: cbapi.CallbackAfterUserKickOffCommand, // 踢下线回调命令
				OperationID:     mcontext.GetOperationID(ctx),
				PlatformID:      platformID,
				Platform:        constant.PlatformIDToName(platformID),
			},
			UserID: userID,
		},
		Seq: time.Now().UnixMilli(), // 踢下线时间戳
	}
	ws.webhookClient.AsyncPost(ctx, req.GetCallbackCommand(), req, &cbapi.CommonCallbackResp{}, after)
}
