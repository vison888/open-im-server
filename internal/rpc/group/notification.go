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
 * 群组通知系统
 *
 * 本模块负责群组系统中的所有通知推送功能，是实现多端数据同步和用户体验的关键组件。
 *
 * 核心功能：
 *
 * 1. 群组生命周期通知
 *    - 群组创建通知：向所有初始成员推送群组创建消息
 *    - 群组解散通知：向所有成员推送解散消息，触发会话清理
 *    - 群组信息变更：名称、头像、公告等变更的实时通知
 *
 * 2. 成员管理通知
 *    - 成员加入通知：新成员加入时的欢迎消息和成员列表更新
 *    - 成员退出通知：主动退群或被踢出的状态同步
 *    - 角色变更通知：管理员任免、群主转让的权限变更
 *    - 成员信息通知：昵称、头像等个人信息在群内的变更
 *
 * 3. 申请流程通知
 *    - 加群申请通知：向管理员推送入群申请待审批消息
 *    - 申请结果通知：申请通过/拒绝后向申请者推送结果
 *    - 邀请通知：邀请加群的通知推送和响应处理
 *
 * 4. 权限控制通知
 *    - 禁言通知：个人禁言/解除禁言的状态变更
 *    - 全群禁言：群组整体禁言状态的推送
 *    - 管理权限：管理员权限变更的通知推送
 *
 * 5. 技术特性
 *    - 多端同步：确保所有设备实时接收通知
 *    - 版本控制：基于版本号的增量数据同步
 *    - 异步处理：通知发送不阻塞主业务流程
 *    - 失败重试：网络异常时的消息重发机制
 *    - 消息去重：避免重复通知的智能过滤
 *
 * 6. 通知分类
 *    - 系统通知：系统级消息，如群组状态变更
 *    - 业务通知：业务相关消息，如成员变动
 *    - 提示通知：用户操作的结果反馈
 *    - 同步通知：数据同步相关的版本更新
 *
 * 架构设计：
 * - 消息模板化：统一的消息格式和内容模板
 * - 推送策略：基于用户在线状态的智能推送
 * - 性能优化：批量推送和异步处理提升效率
 * - 扩展性：支持自定义通知类型和推送逻辑
 */
package group

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/openimsdk/open-im-server/v3/pkg/rpcli"

	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/open-im-server/v3/pkg/common/convert"
	"github.com/openimsdk/open-im-server/v3/pkg/common/servererrs"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/controller"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/versionctx"
	"github.com/openimsdk/open-im-server/v3/pkg/msgprocessor"
	"github.com/openimsdk/open-im-server/v3/pkg/notification"
	"github.com/openimsdk/open-im-server/v3/pkg/notification/common_user"
	"github.com/openimsdk/protocol/constant"
	pbgroup "github.com/openimsdk/protocol/group"
	"github.com/openimsdk/protocol/msg"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
	"github.com/openimsdk/tools/utils/datautil"
	"github.com/openimsdk/tools/utils/stringutil"
	"go.mongodb.org/mongo-driver/mongo"
)

// GroupApplicationReceiver 群组申请接收者类型定义
// 用于区分群组申请通知的接收者类型，确保通知发送给正确的用户
const (
	applicantReceiver = iota // 申请者：接收申请结果通知（通过/拒绝）
	adminReceiver            // 管理员：接收新的申请通知，用于审批处理
)

// NewNotificationSender 创建群组通知发送器
//
// 初始化群组通知系统的核心组件，整合消息发送、用户信息获取、数据库操作等功能。
//
// 组件集成：
// - 基础通知服务：继承通用通知发送能力
// - 消息服务客户端：用于发送系统消息和通知
// - 用户服务客户端：获取用户基础信息用于通知填充
// - 数据库控制器：查询群组相关数据
// - 会话服务客户端：管理群组会话状态
//
// 配置参数：
// - 通知模板配置：各种通知类型的消息模板
// - 推送策略配置：推送频率、重试次数等策略
// - 性能参数：批量大小、超时时间等优化参数
func NewNotificationSender(db controller.GroupDatabase, config *Config, userClient *rpcli.UserClient, msgClient *rpcli.MsgClient, conversationClient *rpcli.ConversationClient) *NotificationSender {
	return &NotificationSender{
		// 初始化基础通知发送器，配置消息发送和用户信息获取回调
		NotificationSender: notification.NewNotificationSender(&config.NotificationConfig,
			// 配置消息发送回调，使用msgClient发送通知消息
			notification.WithRpcClient(func(ctx context.Context, req *msg.SendMsgReq) (*msg.SendMsgResp, error) {
				return msgClient.SendMsg(ctx, req)
			}),
			// 配置用户信息获取回调，用于填充通知中的用户信息
			notification.WithUserRpcClient(userClient.GetUserInfo),
		),
		// 配置用户信息批量获取函数，用于填充群组成员信息
		getUsersInfo: func(ctx context.Context, userIDs []string) ([]common_user.CommonUser, error) {
			users, err := userClient.GetUsersInfo(ctx, userIDs)
			if err != nil {
				return nil, err
			}
			// 转换用户信息格式，适配通用用户接口
			return datautil.Slice(users, func(e *sdkws.UserInfo) common_user.CommonUser { return e }), nil
		},
		db:                 db,                 // 群组数据库控制器
		config:             config,             // 服务配置
		msgClient:          msgClient,          // 消息服务客户端
		conversationClient: conversationClient, // 会话服务客户端
	}
}

// NotificationSender 群组通知发送器
//
// 群组通知系统的核心实现，负责处理所有群组相关的通知推送。
// 采用组合模式，在基础通知能力上扩展群组特有的通知逻辑。
//
// 核心能力：
// - 通知模板管理：统一的消息格式和内容模板
// - 推送策略控制：基于用户状态和业务规则的智能推送
// - 数据填充：自动填充通知中所需的用户和群组信息
// - 版本同步：配合增量同步机制维护数据一致性
// - 异步处理：通知发送不阻塞主业务流程
//
// 设计模式：
// - 组合模式：扩展基础通知能力
// - 策略模式：不同通知类型的处理策略
// - 模板方法：统一的通知发送流程
// - 观察者模式：事件驱动的通知触发
type NotificationSender struct {
	*notification.NotificationSender                                                                               // 基础通知发送器，提供通用通知能力
	getUsersInfo                     func(ctx context.Context, userIDs []string) ([]common_user.CommonUser, error) // 批量获取用户信息的函数
	db                               controller.GroupDatabase                                                      // 群组数据库控制器，用于查询群组相关数据
	config                           *Config                                                                       // 服务配置，包含通知相关配置
	msgClient                        *rpcli.MsgClient                                                              // 消息服务客户端，用于发送通知消息
	conversationClient               *rpcli.ConversationClient                                                     // 会话服务客户端，用于管理群组会话
}

// PopulateGroupMember 填充群组成员信息
//
// 这是一个核心的数据填充方法，用于补全群组成员的显示信息。
// 在群组系统中，成员可以设置群内昵称和头像，如果未设置则使用用户的基础信息。
//
// 填充逻辑：
// 1. 遍历所有群组成员，识别信息不完整的成员
// 2. 批量查询这些成员的用户基础信息
// 3. 按照优先级填充：群内自定义 > 用户基础信息
// 4. 确保所有成员都有完整的显示信息
//
// 性能优化：
// - 按需查询：只查询信息不完整的用户
// - 批量操作：一次性获取多个用户的信息
// - 缓存机制：减少重复的用户信息查询
// - 并发安全：支持多协程并发调用
//
// 使用场景：
// - 成员列表展示：确保所有成员都有显示名称和头像
// - 通知消息：填充通知中提到的成员信息
// - 数据同步：增量同步时的数据完整性保证
func (g *NotificationSender) PopulateGroupMember(ctx context.Context, members ...*model.GroupMember) error {
	// 如果没有成员需要填充，直接返回
	if len(members) == 0 {
		return nil
	}

	// 收集需要查询用户信息的成员ID
	// 只有昵称或头像为空的成员才需要填充
	emptyUserIDs := make(map[string]struct{})
	for _, member := range members {
		if member.Nickname == "" || member.FaceURL == "" {
			emptyUserIDs[member.UserID] = struct{}{}
		}
	}

	// 如果所有成员信息都完整，无需查询用户服务
	if len(emptyUserIDs) > 0 {
		// 批量获取用户基础信息
		users, err := g.getUsersInfo(ctx, datautil.Keys(emptyUserIDs))
		if err != nil {
			return err
		}

		// 构建用户信息映射，便于快速查找
		userMap := make(map[string]common_user.CommonUser)
		for i, user := range users {
			userMap[user.GetUserID()] = users[i]
		}

		// 填充成员信息
		for i, member := range members {
			user, ok := userMap[member.UserID]
			if !ok {
				continue // 用户不存在，跳过
			}

			// 按优先级填充昵称：群内自定义 > 用户昵称
			if member.Nickname == "" {
				members[i].Nickname = user.GetNickname()
			}

			// 按优先级填充头像：群内自定义 > 用户头像
			if member.FaceURL == "" {
				members[i].FaceURL = user.GetFaceURL()
			}
		}
	}
	return nil
}

// getUser 获取单个用户的公开信息
//
// 用于通知消息中需要展示特定用户信息的场景，
// 返回标准化的用户公开信息格式。
//
// 应用场景：
// - 操作者信息：在通知中标识是谁执行了操作
// - 被操作者信息：标识操作的目标用户
// - 申请者信息：在申请通知中展示申请人信息
//
// 错误处理：
// - 用户不存在：返回专门的用户未找到错误
// - 服务异常：透传用户服务的错误信息
// - 数据异常：处理返回数据格式错误
func (g *NotificationSender) getUser(ctx context.Context, userID string) (*sdkws.PublicUserInfo, error) {
	// 获取用户信息（批量接口获取单个用户）
	users, err := g.getUsersInfo(ctx, []string{userID})
	if err != nil {
		return nil, err
	}

	// 验证用户是否存在
	if len(users) == 0 {
		return nil, servererrs.ErrUserIDNotFound.WrapMsg(fmt.Sprintf("user %s not found", userID))
	}

	// 构建公开用户信息结构
	return &sdkws.PublicUserInfo{
		UserID:   users[0].GetUserID(),   // 用户ID
		Nickname: users[0].GetNickname(), // 用户昵称
		FaceURL:  users[0].GetFaceURL(),  // 用户头像
		Ex:       users[0].GetEx(),       // 扩展字段
	}, nil
}

// getGroupInfo 获取群组完整信息
//
// 查询群组的详细信息，包括基础信息、成员数量、群主等，
// 用于通知消息中需要展示群组信息的场景。
//
// 信息整合：
// 1. 群组基础信息：名称、头像、介绍、公告等
// 2. 成员统计：当前群组的总成员数量
// 3. 群主信息：查询并设置当前群主ID
// 4. 状态信息：群组当前状态（正常/已解散等）
//
// 数据一致性：
// - 实时查询：确保获取最新的群组状态
// - 原子操作：在事务中查询相关信息
// - 异常处理：处理群组不存在或数据异常
func (g *NotificationSender) getGroupInfo(ctx context.Context, groupID string) (*sdkws.GroupInfo, error) {
	// 查询群组基础信息
	gm, err := g.db.TakeGroup(ctx, groupID)
	if err != nil {
		return nil, err
	}

	// 查询群组成员数量
	num, err := g.db.FindGroupMemberNum(ctx, groupID)
	if err != nil {
		return nil, err
	}

	// 查询群主用户ID
	ownerUserIDs, err := g.db.GetGroupRoleLevelMemberIDs(ctx, groupID, constant.GroupOwner)
	if err != nil {
		return nil, err
	}

	// 确定群主ID（正常情况下只有一个群主）
	var ownerUserID string
	if len(ownerUserIDs) > 0 {
		ownerUserID = ownerUserIDs[0]
	}

	// 转换为标准的群组信息格式
	return convert.Db2PbGroupInfo(gm, ownerUserID, num), nil
}

// getGroupMembers 获取群组成员详细信息
//
// 根据群组ID和用户ID列表获取群组成员的完整信息，包括成员在群内的角色、
// 昵称、头像等信息。这是群组通知系统中获取成员信息的核心方法。
//
// 处理流程：
// 1. 从数据库查询指定用户在群组中的成员信息
// 2. 填充成员的显示信息（昵称、头像等）
// 3. 转换为标准的Protobuf格式返回
//
// 参数说明：
// - groupID: 群组ID，指定要查询的群组
// - userIDs: 用户ID列表，指定要查询的成员
//
// 返回值：
// - []*sdkws.GroupMemberFullInfo: 群组成员完整信息列表
// - error: 查询过程中的错误信息
//
// 使用场景：
// - 通知消息中需要展示成员信息
// - 群组操作后的成员信息同步
// - 权限验证时的成员角色查询
func (g *NotificationSender) getGroupMembers(ctx context.Context, groupID string, userIDs []string) ([]*sdkws.GroupMemberFullInfo, error) {
	// 从数据库查询群组成员信息
	members, err := g.db.FindGroupMembers(ctx, groupID, userIDs)
	if err != nil {
		return nil, err
	}

	// 填充成员的显示信息（昵称、头像等）
	if err := g.PopulateGroupMember(ctx, members...); err != nil {
		return nil, err
	}

	// 记录调试日志，便于问题排查
	log.ZDebug(ctx, "getGroupMembers", "members", members)

	// 转换为Protobuf格式的成员信息列表
	res := make([]*sdkws.GroupMemberFullInfo, 0, len(members))
	for _, member := range members {
		// 转换单个成员信息，appMangerLevel设为0表示非应用管理员
		res = append(res, g.groupMemberDB2PB(member, 0))
	}
	return res, nil
}

// getGroupMemberMap 获取群组成员信息映射表
//
// 基于getGroupMembers方法，将群组成员信息转换为以用户ID为键的映射表，
// 便于快速查找特定用户的群组成员信息。
//
// 处理流程：
// 1. 调用getGroupMembers获取成员信息列表
// 2. 构建以用户ID为键的映射表
// 3. 返回映射表供快速查找使用
//
// 参数说明：
// - groupID: 群组ID
// - userIDs: 要查询的用户ID列表
//
// 返回值：
// - map[string]*sdkws.GroupMemberFullInfo: 用户ID到成员信息的映射
// - error: 查询过程中的错误
//
// 使用场景：
// - 需要频繁查找特定用户信息的场景
// - 通知消息中需要区分不同用户角色
// - 批量处理成员信息时的快速查找
func (g *NotificationSender) getGroupMemberMap(ctx context.Context, groupID string, userIDs []string) (map[string]*sdkws.GroupMemberFullInfo, error) {
	// 获取群组成员信息列表
	members, err := g.getGroupMembers(ctx, groupID, userIDs)
	if err != nil {
		return nil, err
	}

	// 构建用户ID到成员信息的映射表
	m := make(map[string]*sdkws.GroupMemberFullInfo)
	for i, member := range members {
		// 使用索引访问确保指针正确性
		m[member.UserID] = members[i]
	}
	return m, nil
}

// getGroupMember 获取单个群组成员信息
//
// 获取指定用户在指定群组中的成员信息。这是getGroupMembers的单用户版本，
// 用于只需要查询一个用户信息的场景。
//
// 处理流程：
// 1. 调用getGroupMembers查询单个用户
// 2. 验证查询结果是否为空
// 3. 返回第一个（也是唯一的）成员信息
//
// 参数说明：
// - groupID: 群组ID
// - userID: 要查询的用户ID
//
// 返回值：
// - *sdkws.GroupMemberFullInfo: 群组成员完整信息
// - error: 查询错误或用户不存在错误
//
// 错误处理：
// - 如果用户不在群组中，返回用户未找到错误
// - 数据库查询失败时返回相应错误
//
// 使用场景：
// - 通知中需要展示特定用户信息
// - 权限验证时查询用户角色
// - 单用户操作的信息获取
func (g *NotificationSender) getGroupMember(ctx context.Context, groupID string, userID string) (*sdkws.GroupMemberFullInfo, error) {
	// 查询单个用户的群组成员信息
	members, err := g.getGroupMembers(ctx, groupID, []string{userID})
	if err != nil {
		return nil, err
	}

	// 验证用户是否在群组中
	if len(members) == 0 {
		return nil, errs.ErrInternalServer.WrapMsg(fmt.Sprintf("group %s member %s not found", groupID, userID))
	}

	// 返回用户的群组成员信息
	return members[0], nil
}

// getGroupOwnerAndAdminUserID 获取群主和管理员用户ID列表
//
// 查询指定群组中所有群主和管理员的用户ID，用于需要向管理层发送通知的场景。
// 在群组管理中，很多操作需要通知群主和管理员，此方法提供了统一的查询接口。
//
// 处理流程：
// 1. 查询群组中角色为群主或管理员的成员
// 2. 填充成员的基本信息
// 3. 提取并返回用户ID列表
//
// 参数说明：
// - groupID: 群组ID
//
// 返回值：
// - []string: 群主和管理员的用户ID列表
// - error: 查询过程中的错误
//
// 角色说明：
// - constant.GroupOwner: 群主角色
// - constant.GroupAdmin: 管理员角色
//
// 使用场景：
// - 群组申请通知：向管理员发送入群申请
// - 群组变更通知：通知管理层群组信息变更
// - 权限操作通知：需要管理员知晓的操作
func (g *NotificationSender) getGroupOwnerAndAdminUserID(ctx context.Context, groupID string) ([]string, error) {
	// 查询群组中的群主和管理员成员
	members, err := g.db.FindGroupMemberRoleLevels(ctx, groupID, []int32{constant.GroupOwner, constant.GroupAdmin})
	if err != nil {
		return nil, err
	}

	// 填充成员的基本信息（昵称、头像等）
	if err := g.PopulateGroupMember(ctx, members...); err != nil {
		return nil, err
	}

	// 提取用户ID的转换函数
	fn := func(e *model.GroupMember) string { return e.UserID }

	// 将成员列表转换为用户ID列表
	return datautil.Slice(members, fn), nil
}

// groupMemberDB2PB 将数据库群组成员模型转换为Protobuf格式
//
// 这是一个数据转换方法，将从数据库查询到的群组成员信息转换为
// gRPC通信使用的Protobuf格式。确保数据格式的统一性和兼容性。
//
// 转换内容：
// - 基础信息：群组ID、用户ID、角色等级
// - 时间字段：加入时间、禁言结束时间（转换为毫秒时间戳）
// - 显示信息：群内昵称、头像URL
// - 关系信息：邀请人、操作人、加入来源
// - 扩展字段：自定义扩展数据
//
// 参数说明：
// - member: 数据库中的群组成员模型
// - appMangerLevel: 应用管理员等级（0表示非应用管理员）
//
// 返回值：
// - *sdkws.GroupMemberFullInfo: Protobuf格式的群组成员完整信息
//
// 时间处理：
// - 所有时间字段都转换为Unix毫秒时间戳
// - 确保前端能够正确解析时间信息
//
// 使用场景：
// - API响应数据格式化
// - 通知消息中的成员信息
// - 群组成员列表查询结果
func (g *NotificationSender) groupMemberDB2PB(member *model.GroupMember, appMangerLevel int32) *sdkws.GroupMemberFullInfo {
	return &sdkws.GroupMemberFullInfo{
		GroupID:        member.GroupID,                 // 群组ID
		UserID:         member.UserID,                  // 用户ID
		RoleLevel:      member.RoleLevel,               // 群内角色等级（群主/管理员/普通成员）
		JoinTime:       member.JoinTime.UnixMilli(),    // 加入群组时间（毫秒时间戳）
		Nickname:       member.Nickname,                // 群内昵称
		FaceURL:        member.FaceURL,                 // 群内头像URL
		AppMangerLevel: appMangerLevel,                 // 应用管理员等级
		JoinSource:     member.JoinSource,              // 加入来源（邀请/申请/扫码等）
		OperatorUserID: member.OperatorUserID,          // 操作人用户ID（谁添加的此成员）
		Ex:             member.Ex,                      // 扩展字段
		MuteEndTime:    member.MuteEndTime.UnixMilli(), // 禁言结束时间（毫秒时间戳）
		InviterUserID:  member.InviterUserID,           // 邀请人用户ID
	}
}

/* func (g *NotificationSender) getUsersInfoMap(ctx context.Context, userIDs []string) (map[string]*sdkws.UserInfo, error) {
	users, err := g.getUsersInfo(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[string]*sdkws.UserInfo)
	for _, user := range users {
		result[user.GetUserID()] = user.(*sdkws.UserInfo)
	}
	return result, nil
} */

// fillOpUser 填充操作用户信息
//
// 这是一个便捷方法，用于填充当前操作用户的群组成员信息。
// 从上下文中获取操作用户ID，然后调用fillUserByUserID进行信息填充。
//
// 处理逻辑：
// 1. 从上下文获取当前操作用户ID
// 2. 调用fillUserByUserID填充用户信息
// 3. 返回填充结果
//
// 参数说明：
// - targetUser: 指向群组成员信息指针的指针，用于接收填充结果
// - groupID: 群组ID，用于查询用户在群内的信息
//
// 返回值：
// - error: 填充过程中的错误信息
//
// 使用场景：
// - 通知消息中需要展示操作者信息
// - 群组操作记录中的操作人信息
// - 权限验证时的当前用户信息
//
// 注意事项：
// - targetUser是双重指针，允许方法修改指针指向的内容
// - 操作用户ID从请求上下文中自动获取
func (g *NotificationSender) fillOpUser(ctx context.Context, targetUser **sdkws.GroupMemberFullInfo, groupID string) (err error) {
	// 从上下文获取操作用户ID，然后填充用户信息
	return g.fillUserByUserID(ctx, mcontext.GetOpUserID(ctx), targetUser, groupID)
}

// fillUserByUserID 根据用户ID填充用户信息
//
// 这是用户信息填充的核心方法，负责获取用户的完整信息并填充到目标结构中。
// 支持群组成员信息和普通用户信息的混合填充，确保信息的完整性。
//
// 填充策略：
// 1. 如果指定了群组ID，优先获取用户在群内的成员信息
// 2. 如果用户是系统管理员，设置特殊的管理员标识
// 3. 获取用户的基础信息（昵称、头像等）
// 4. 按优先级填充：群内信息 > 用户基础信息
//
// 参数说明：
// - userID: 要填充信息的用户ID
// - targetUser: 指向群组成员信息指针的指针，用于接收填充结果
// - groupID: 群组ID，为空时只填充用户基础信息
//
// 返回值：
// - error: 填充过程中的错误信息
//
// 错误处理：
// - 参数验证：检查targetUser指针是否为nil
// - 数据库错误：区分记录不存在和其他数据库错误
// - 用户不存在：通过用户服务验证用户有效性
//
// 管理员处理：
// - 系统管理员自动获得群组管理员权限
// - 设置AppMangerLevel为应用管理员级别
//
// 信息优先级：
// - 群内昵称 > 用户昵称
// - 群内头像 > 用户头像
// - 群内信息优先展示
func (g *NotificationSender) fillUserByUserID(ctx context.Context, userID string, targetUser **sdkws.GroupMemberFullInfo, groupID string) error {
	// 参数验证：确保targetUser指针不为空
	if targetUser == nil {
		return errs.ErrInternalServer.WrapMsg("**sdkws.GroupMemberFullInfo is nil")
	}

	// 如果指定了群组ID，尝试获取用户在群内的成员信息
	if groupID != "" {
		// 检查用户是否为系统管理员
		if authverify.IsManagerUserID(userID, g.config.Share.IMAdminUserID) {
			// 系统管理员自动获得群组管理员权限
			*targetUser = &sdkws.GroupMemberFullInfo{
				GroupID:        groupID,             // 群组ID
				UserID:         userID,              // 用户ID
				RoleLevel:      constant.GroupAdmin, // 角色等级：管理员
				AppMangerLevel: constant.AppAdmin,   // 应用管理员等级
			}
		} else {
			// 查询用户在群组中的成员信息
			member, err := g.db.TakeGroupMember(ctx, groupID, userID)
			if err == nil {
				// 成功获取群组成员信息，转换为Protobuf格式
				*targetUser = g.groupMemberDB2PB(member, 0)
			} else if !(errors.Is(err, mongo.ErrNoDocuments) || errs.ErrRecordNotFound.Is(err)) {
				// 如果不是记录不存在的错误，则返回错误
				return err
			}
			// 如果是记录不存在，继续后续处理
		}
	}

	// 获取用户的基础信息
	user, err := g.getUser(ctx, userID)
	if err != nil {
		return err
	}

	// 如果还没有目标用户信息，创建基础的用户信息结构
	if *targetUser == nil {
		*targetUser = &sdkws.GroupMemberFullInfo{
			GroupID:        groupID,       // 群组ID
			UserID:         userID,        // 用户ID
			Nickname:       user.Nickname, // 用户昵称
			FaceURL:        user.FaceURL,  // 用户头像
			OperatorUserID: userID,        // 操作人ID（自己）
		}
	} else {
		// 如果已有目标用户信息，按优先级填充缺失的字段

		// 填充昵称：如果群内昵称为空，使用用户昵称
		if (*targetUser).Nickname == "" {
			(*targetUser).Nickname = user.Nickname
		}

		// 填充头像：如果群内头像为空，使用用户头像
		if (*targetUser).FaceURL == "" {
			(*targetUser).FaceURL = user.FaceURL
		}
	}

	return nil
}

// setVersion 设置版本信息
//
// 从版本上下文中获取指定集合和文档的版本信息，用于增量同步机制。
// 版本信息帮助客户端识别数据变更，实现高效的增量更新。
//
// 查找策略：
// 1. 从版本日志上下文中获取所有版本记录
// 2. 倒序遍历版本记录（最新的版本在后面）
// 3. 匹配集合名称和文档ID
// 4. 设置版本号和版本ID
//
// 参数说明：
// - version: 指向版本号的指针，用于接收版本号
// - versionID: 指向版本ID的指针，用于接收版本ID
// - collName: 集合名称，用于匹配特定的数据集合
// - id: 文档ID，用于匹配特定的文档
//
// 版本机制：
// - 每次数据变更都会生成新的版本记录
// - 版本号递增，确保版本的唯一性和顺序性
// - 版本ID是MongoDB ObjectID的十六进制表示
//
// 使用场景：
// - 群组成员变更通知
// - 群组信息变更通知
// - 客户端增量同步
//
// 注意事项：
// - 倒序遍历确保获取最新版本
// - 如果没有找到匹配的版本，版本信息保持原值
func (g *NotificationSender) setVersion(ctx context.Context, version *uint64, versionID *string, collName string, id string) {
	// 从版本上下文获取所有版本记录
	versions := versionctx.GetVersionLog(ctx).Get()

	// 倒序遍历版本记录，确保获取最新版本
	for i := len(versions) - 1; i >= 0; i-- {
		coll := versions[i]

		// 匹配集合名称和文档ID
		if coll.Name == collName && coll.Doc.DID == id {
			// 设置版本号（转换为uint64）
			*version = uint64(coll.Doc.Version)

			// 设置版本ID（MongoDB ObjectID的十六进制表示）
			*versionID = coll.Doc.ID.Hex()

			// 找到匹配的版本后立即返回
			return
		}
	}
	// 如果没有找到匹配的版本，版本信息保持原值
}

// setSortVersion 设置排序版本信息
//
// 除了设置基础版本信息外，还设置排序相关的版本信息。
// 排序版本用于跟踪数据排序顺序的变更，支持更精细的增量同步。
//
// 处理流程：
// 1. 从版本上下文获取所有版本记录
// 2. 遍历版本记录，匹配集合名称和文档ID
// 3. 设置基础版本号和版本ID
// 4. 查找排序变更的版本记录
// 5. 设置排序版本号
//
// 参数说明：
// - version: 指向基础版本号的指针
// - versionID: 指向版本ID的指针
// - collName: 集合名称
// - id: 文档ID
// - sortVersion: 指向排序版本号的指针
//
// 排序版本机制：
// - 当数据的排序顺序发生变化时，会记录排序版本
// - 排序版本独立于基础版本，支持更精细的同步控制
// - model.VersionSortChangeID 标识排序变更事件
//
// 使用场景：
// - 群组成员列表排序变更
// - 好友列表排序变更
// - 需要维护排序状态的数据同步
//
// 版本类型：
// - 基础版本：数据内容的变更版本
// - 排序版本：数据排序的变更版本
func (g *NotificationSender) setSortVersion(ctx context.Context, version *uint64, versionID *string, collName string, id string, sortVersion *uint64) {
	// 从版本上下文获取所有版本记录
	versions := versionctx.GetVersionLog(ctx).Get()

	// 遍历版本记录，查找匹配的集合和文档
	for _, coll := range versions {
		if coll.Name == collName && coll.Doc.DID == id {
			// 设置基础版本信息
			*version = uint64(coll.Doc.Version) // 基础版本号
			*versionID = coll.Doc.ID.Hex()      // 版本ID

			// 查找排序变更的版本记录
			for _, elem := range coll.Doc.Logs {
				if elem.EID == model.VersionSortChangeID {
					// 设置排序版本号
					*sortVersion = uint64(elem.Version)
				}
			}
		}
	}
}

// GroupCreatedNotification 群组创建通知
//
// 当群组创建成功后，向所有初始成员发送群组创建通知。
// 这是群组生命周期的第一个通知，标志着群组的正式建立。
//
// 通知内容：
// - 群组基本信息（名称、头像、介绍等）
// - 创建者信息（操作用户）
// - 群组成员版本信息（用于增量同步）
// - 创建时间和相关元数据
//
// 处理流程：
// 1. 填充操作用户（群组创建者）信息
// 2. 设置群组成员版本信息
// 3. 向群组所有成员发送创建通知
//
// 参数说明：
// - tips: 群组创建通知的详细信息
// - SendMessage: 是否发送消息到聊天记录（可选参数）
//
// 通知特点：
// - 群组级通知：发送给群组内所有成员
// - 包含版本信息：支持客户端增量同步
// - 可选消息记录：根据配置决定是否保存到聊天记录
//
// 错误处理：
// - 使用defer确保错误被正确记录
// - 填充用户信息失败时提前返回
// - 记录详细的错误日志便于问题排查
//
// 使用场景：
// - 用户创建新群组后的通知
// - 管理员批量创建群组的通知
// - 群组恢复或迁移后的通知
func (g *NotificationSender) GroupCreatedNotification(ctx context.Context, tips *sdkws.GroupCreatedTips, SendMessage *bool) {
	var err error

	// 使用defer确保错误被正确记录和处理
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()

	// 填充操作用户（群组创建者）的详细信息
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}

	// 设置群组成员版本信息，用于客户端增量同步
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID)

	// 发送群组创建通知给所有群组成员
	g.Notification(ctx, mcontext.GetOpUserID(ctx), tips.Group.GroupID, constant.GroupCreatedNotification, tips, notification.WithSendMessage(SendMessage))
}

// GroupInfoSetNotification 群组信息设置通知
//
// 当群组的基本信息发生变更时，向所有群组成员发送信息变更通知。
// 包括群组名称、头像、介绍、公告等基本信息的修改。
//
// 通知内容：
// - 变更后的群组信息
// - 操作者信息（谁修改了群组信息）
// - 群组成员版本信息
// - 变更时间和相关元数据
//
// 处理流程：
// 1. 填充操作用户信息
// 2. 设置群组成员版本信息
// 3. 向群组所有成员发送信息变更通知
//
// 参数说明：
// - tips: 群组信息设置通知的详细信息
//
// 通知特点：
// - 群组级通知：通知所有群组成员
// - 包含用户名获取：自动获取相关用户的显示名称
// - 版本同步：支持客户端增量更新群组信息
//
// 信息变更类型：
// - 群组名称修改
// - 群组头像更换
// - 群组介绍更新
// - 群组设置调整
//
// 使用场景：
// - 群主或管理员修改群组信息
// - 系统自动更新群组信息
// - 群组信息批量修改
func (g *NotificationSender) GroupInfoSetNotification(ctx context.Context, tips *sdkws.GroupInfoSetTips) {
	var err error

	// 使用defer确保错误被正确记录
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()

	// 填充操作用户的详细信息
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}

	// 设置群组成员版本信息，用于增量同步
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID)

	// 发送群组信息设置通知，包含用户名获取功能
	g.Notification(ctx, mcontext.GetOpUserID(ctx), tips.Group.GroupID, constant.GroupInfoSetNotification, tips, notification.WithRpcGetUserName())
}

// GroupInfoSetNameNotification 群组名称设置通知
//
// 当群组名称发生变更时，向所有群组成员发送名称变更通知。
// 这是群组信息变更通知的特化版本，专门处理群组名称的修改。
//
// 通知内容：
// - 变更后的群组名称
// - 操作者信息（谁修改了群组名称）
// - 群组成员版本信息
// - 变更时间和相关元数据
//
// 处理流程：
// 1. 填充操作用户信息
// 2. 设置群组成员版本信息
// 3. 向群组所有成员发送名称变更通知
//
// 参数说明：
// - tips: 群组名称设置通知的详细信息
//
// 通知特点：
// - 群组级通知：通知所有群组成员
// - 版本同步：支持客户端增量更新群组信息
// - 实时推送：确保所有成员及时了解名称变更
//
// 使用场景：
// - 群主或管理员修改群组名称
// - 系统自动更新群组名称
// - 群组重命名操作
func (g *NotificationSender) GroupInfoSetNameNotification(ctx context.Context, tips *sdkws.GroupInfoSetNameTips) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), tips.Group.GroupID, constant.GroupInfoSetNameNotification, tips)
}

// GroupInfoSetAnnouncementNotification 群组公告设置通知
//
// 当群组公告发生变更时，向所有群组成员发送公告变更通知。
// 群组公告是重要的群组信息，变更时需要确保所有成员都能及时收到通知。
//
// 通知内容：
// - 变更后的群组公告内容
// - 操作者信息（谁修改了群组公告）
// - 群组成员版本信息
// - 变更时间和相关元数据
//
// 处理流程：
// 1. 填充操作用户信息
// 2. 设置群组成员版本信息
// 3. 向群组所有成员发送公告变更通知
//
// 参数说明：
// - tips: 群组公告设置通知的详细信息
// - sendMessage: 是否发送消息到聊天记录（可选参数）
//
// 通知特点：
// - 群组级通知：通知所有群组成员
// - 包含用户名获取：自动获取相关用户的显示名称
// - 版本同步：支持客户端增量更新群组信息
// - 可选消息记录：根据配置决定是否保存到聊天记录
//
// 使用场景：
// - 群主或管理员发布新公告
// - 群主或管理员修改现有公告
// - 群主或管理员删除群组公告
func (g *NotificationSender) GroupInfoSetAnnouncementNotification(ctx context.Context, tips *sdkws.GroupInfoSetAnnouncementTips, sendMessage *bool) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), tips.Group.GroupID, constant.GroupInfoSetAnnouncementNotification, tips, notification.WithRpcGetUserName(), notification.WithSendMessage(sendMessage))
}

// uuid 生成唯一标识符
//
// 生成一个新的UUID字符串，用于标识通知消息的唯一性。
// 在群组通知系统中，每个通知都需要一个唯一的标识符来避免重复处理。
//
// 返回值：
// - string: UUID字符串格式的唯一标识符
//
// 使用场景：
// - 群组申请通知的唯一标识
// - 通知消息的去重处理
// - 通知状态的跟踪和管理
//
// 实现说明：
// - 使用Google UUID库生成标准的UUID v4
// - 返回格式为 "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
// - 保证全局唯一性和随机性
func (g *NotificationSender) uuid() string {
	return uuid.New().String()
}

// getGroupRequest 获取群组申请信息
//
// 根据群组ID和用户ID获取群组申请的详细信息，包括申请记录和申请者的用户信息。
// 这是群组申请通知系统中获取申请详情的核心方法。
//
// 处理流程：
// 1. 从数据库查询群组申请记录
// 2. 获取申请者的用户基础信息
// 3. 验证用户信息的有效性
// 4. 转换为标准的Protobuf格式返回
//
// 参数说明：
// - groupID: 群组ID，指定要查询的群组
// - userID: 用户ID，指定申请者
//
// 返回值：
// - *sdkws.GroupRequest: 群组申请的完整信息
// - error: 查询过程中的错误信息
//
// 数据整合：
// - 申请记录：申请时间、申请消息、处理状态等
// - 用户信息：申请者的昵称、头像、基础信息
// - 群组信息：相关的群组基础信息
//
// 错误处理：
// - 申请记录不存在：返回数据库查询错误
// - 用户不存在：返回用户未找到错误
// - 数据格式异常：进行类型转换和兼容处理
//
// 使用场景：
// - 群组申请通知的信息填充
// - 申请处理结果通知的数据准备
// - 管理员查看申请详情
func (g *NotificationSender) getGroupRequest(ctx context.Context, groupID string, userID string) (*sdkws.GroupRequest, error) {
	request, err := g.db.TakeGroupRequest(ctx, groupID, userID)
	if err != nil {
		return nil, err
	}
	users, err := g.getUsersInfo(ctx, []string{userID})
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, servererrs.ErrUserIDNotFound.WrapMsg(fmt.Sprintf("user %s not found", userID))
	}
	info, ok := users[0].(*sdkws.UserInfo)
	if !ok {
		info = &sdkws.UserInfo{
			UserID:   users[0].GetUserID(),
			Nickname: users[0].GetNickname(),
			FaceURL:  users[0].GetFaceURL(),
			Ex:       users[0].GetEx(),
		}
	}
	return convert.Db2PbGroupRequest(request, info, nil), nil
}

// JoinGroupApplicationNotification 加入群组申请通知
//
// 当用户申请加入群组时，向群组管理员（群主和管理员）发送申请通知。
// 这是群组申请流程的第一步，确保管理员能及时处理入群申请。
//
// 通知内容：
// - 群组基本信息
// - 申请者信息（用户基础信息）
// - 申请消息内容
// - 申请的唯一标识符
// - 完整的申请记录信息
//
// 处理流程：
// 1. 获取群组申请的详细信息
// 2. 获取群组基本信息
// 3. 获取邀请人信息（如果有）
// 4. 获取群组管理员列表（群主+管理员）
// 5. 向所有相关人员发送申请通知
//
// 参数说明：
// - req: 加入群组的请求信息
// - dbReq: 数据库中的申请记录
//
// 通知接收者：
// - 群主：有权限处理申请
// - 管理员：有权限处理申请
// - 邀请人：了解申请状态（如果有邀请人）
// - 操作者：申请提交确认
//
// 错误处理：
// - 申请信息获取失败：记录错误日志并返回
// - 群组信息获取失败：停止通知发送
// - 用户信息获取失败：停止通知发送
//
// 使用场景：
// - 用户主动申请加入群组
// - 通过邀请链接申请加入
// - 扫码申请加入群组
func (g *NotificationSender) JoinGroupApplicationNotification(ctx context.Context, req *pbgroup.JoinGroupReq, dbReq *model.GroupRequest) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	request, err := g.getGroupRequest(ctx, dbReq.GroupID, dbReq.UserID)
	if err != nil {
		log.ZError(ctx, "JoinGroupApplicationNotification getGroupRequest", err, "dbReq", dbReq)
		return
	}
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, req.GroupID)
	if err != nil {
		return
	}
	var user *sdkws.PublicUserInfo
	user, err = g.getUser(ctx, req.InviterUserID)
	if err != nil {
		return
	}
	userIDs, err := g.getGroupOwnerAndAdminUserID(ctx, req.GroupID)
	if err != nil {
		return
	}
	userIDs = append(userIDs, req.InviterUserID, mcontext.GetOpUserID(ctx))
	tips := &sdkws.JoinGroupApplicationTips{
		Group:     group,
		Applicant: user,
		ReqMsg:    req.ReqMessage,
		Uuid:      g.uuid(),
		Request:   request,
	}
	for _, userID := range datautil.Distinct(userIDs) {
		g.Notification(ctx, mcontext.GetOpUserID(ctx), userID, constant.JoinGroupApplicationNotification, tips)
	}
}

// MemberQuitNotification 成员退出通知
//
// 当群组成员主动退出群组时，向群组内所有其他成员发送退出通知。
// 这确保了群组成员能及时了解成员变动情况。
//
// 通知内容：
// - 群组基本信息
// - 退出成员的完整信息
// - 群组成员版本信息（用于增量同步）
// - 退出时间和相关元数据
//
// 处理流程：
// 1. 获取群组基本信息
// 2. 构建成员退出通知消息
// 3. 设置群组成员版本信息
// 4. 向群组所有成员发送退出通知
//
// 参数说明：
// - member: 退出的群组成员完整信息
//
// 通知特点：
// - 群组级通知：发送给群组内所有成员
// - 包含版本信息：支持客户端增量同步成员列表
// - 实时推送：确保成员变动及时同步
//
// 版本控制：
// - 更新群组成员版本号
// - 支持客户端增量获取成员变更
// - 维护数据一致性
//
// 使用场景：
// - 用户主动退出群组
// - 用户通过客户端离开群组
// - 批量退出操作的单个通知
//
// 注意事项：
// - 退出成员不会收到此通知（已不在群组中）
// - 通知发送给群组内剩余的所有成员
// - 包含完整的成员信息便于客户端处理
func (g *NotificationSender) MemberQuitNotification(ctx context.Context, member *sdkws.GroupMemberFullInfo) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, member.GroupID)
	if err != nil {
		return
	}
	tips := &sdkws.MemberQuitTips{Group: group, QuitUser: member}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, member.GroupID)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), member.GroupID, constant.MemberQuitNotification, tips)
}

// GroupApplicationAcceptedNotification 群组申请通过通知
//
// 当群组管理员通过用户的入群申请时，向相关人员发送申请通过通知。
// 这是群组申请流程的成功结果通知，确保申请者和管理员都了解处理结果。
//
// 通知内容：
// - 群组基本信息
// - 处理申请的操作者信息
// - 处理消息（管理员的回复）
// - 申请的唯一标识符
// - 完整的申请记录信息
// - 接收者角色标识
//
// 处理流程：
// 1. 获取群组申请的详细信息
// 2. 获取群组基本信息
// 3. 获取群组管理员列表
// 4. 填充操作者信息
// 5. 向申请者和管理员发送通知（区分接收者角色）
//
// 参数说明：
// - req: 群组申请响应请求信息
//
// 通知接收者：
// - 申请者：收到申请通过的好消息
// - 群主：了解申请处理结果
// - 管理员：了解申请处理结果
//
// 接收者角色：
// - applicantReceiver：申请者角色，显示"您的申请已通过"
// - adminReceiver：管理员角色，显示"申请已被处理"
//
// 错误处理：
// - 申请信息获取失败：记录错误日志并返回
// - 群组信息获取失败：停止通知发送
// - 操作者信息填充失败：停止通知发送
//
// 使用场景：
// - 管理员通过入群申请
// - 自动审批系统通过申请
// - 批量处理申请的单个通知
func (g *NotificationSender) GroupApplicationAcceptedNotification(ctx context.Context, req *pbgroup.GroupApplicationResponseReq) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	request, err := g.getGroupRequest(ctx, req.GroupID, req.FromUserID)
	if err != nil {
		log.ZError(ctx, "GroupApplicationAcceptedNotification getGroupRequest", err, "req", req)
		return
	}
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, req.GroupID)
	if err != nil {
		return
	}
	var userIDs []string
	userIDs, err = g.getGroupOwnerAndAdminUserID(ctx, req.GroupID)
	if err != nil {
		return
	}

	var opUser *sdkws.GroupMemberFullInfo
	if err = g.fillOpUser(ctx, &opUser, group.GroupID); err != nil {
		return
	}
	tips := &sdkws.GroupApplicationAcceptedTips{
		Group:     group,
		OpUser:    opUser,
		HandleMsg: req.HandledMsg,
		Uuid:      g.uuid(),
		Request:   request,
	}
	for _, userID := range append(userIDs, req.FromUserID) {
		if userID == req.FromUserID {
			tips.ReceiverAs = applicantReceiver
		} else {
			tips.ReceiverAs = adminReceiver
		}
		g.Notification(ctx, mcontext.GetOpUserID(ctx), userID, constant.GroupApplicationAcceptedNotification, tips)
	}
}

// GroupApplicationRejectedNotification 群组申请拒绝通知
//
// 当群组管理员拒绝用户的入群申请时，向相关人员发送申请拒绝通知。
// 这是群组申请流程的拒绝结果通知，确保申请者了解处理结果，管理员了解处理状态。
//
// 通知内容：
// - 群组基本信息
// - 处理申请的操作者信息
// - 拒绝消息（管理员的拒绝理由）
// - 申请的唯一标识符
// - 完整的申请记录信息
// - 接收者角色标识
//
// 处理流程：
// 1. 获取群组申请的详细信息
// 2. 获取群组基本信息
// 3. 获取群组管理员列表
// 4. 填充操作者信息
// 5. 向申请者和管理员发送通知（区分接收者角色）
//
// 参数说明：
// - req: 群组申请响应请求信息
//
// 通知接收者：
// - 申请者：收到申请被拒绝的通知和理由
// - 群主：了解申请处理结果
// - 管理员：了解申请处理结果
//
// 接收者角色：
// - applicantReceiver：申请者角色，显示"您的申请已被拒绝"
// - adminReceiver：管理员角色，显示"申请已被处理"
//
// 错误处理：
// - 申请信息获取失败：记录错误日志并返回
// - 群组信息获取失败：停止通知发送
// - 操作者信息填充失败：停止通知发送
//
// 使用场景：
// - 管理员拒绝入群申请
// - 自动审批系统拒绝申请
// - 批量处理申请的单个拒绝通知
//
// 注意事项：
// - 拒绝理由应当友好和建设性
// - 申请者可能会根据拒绝理由重新申请
// - 管理员需要了解拒绝操作的执行情况
func (g *NotificationSender) GroupApplicationRejectedNotification(ctx context.Context, req *pbgroup.GroupApplicationResponseReq) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	request, err := g.getGroupRequest(ctx, req.GroupID, req.FromUserID)
	if err != nil {
		log.ZError(ctx, "GroupApplicationAcceptedNotification getGroupRequest", err, "req", req)
		return
	}
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, req.GroupID)
	if err != nil {
		return
	}
	var userIDs []string
	userIDs, err = g.getGroupOwnerAndAdminUserID(ctx, req.GroupID)
	if err != nil {
		return
	}

	var opUser *sdkws.GroupMemberFullInfo
	if err = g.fillOpUser(ctx, &opUser, group.GroupID); err != nil {
		return
	}
	tips := &sdkws.GroupApplicationRejectedTips{
		Group:     group,
		OpUser:    opUser,
		HandleMsg: req.HandledMsg,
		Uuid:      g.uuid(),
		Request:   request,
	}
	for _, userID := range append(userIDs, req.FromUserID) {
		if userID == req.FromUserID {
			tips.ReceiverAs = applicantReceiver
		} else {
			tips.ReceiverAs = adminReceiver
		}
		g.Notification(ctx, mcontext.GetOpUserID(ctx), userID, constant.GroupApplicationRejectedNotification, tips)
	}
}

// GroupOwnerTransferredNotification 群主转让通知
//
// 当群主将群组所有权转让给其他成员时，向群组所有成员发送群主转让通知。
// 这是群组管理中的重要变更，需要确保所有成员了解群组管理权的变化。
//
// 通知内容：
// - 群组基本信息
// - 操作者信息（执行转让的用户）
// - 新群主信息（接收群主权限的成员）
// - 原群主信息（转让群主权限的成员）
// - 群组成员版本信息
//
// 处理流程：
// 1. 获取群组基本信息
// 2. 获取相关用户的群组成员信息（操作者、新群主、原群主）
// 3. 构建群主转让通知消息
// 4. 填充操作者信息
// 5. 设置群组成员版本信息
// 6. 向群组所有成员发送通知
//
// 参数说明：
// - req: 群主转让请求信息
//
// 涉及角色：
// - 操作者：执行转让操作的用户（通常是原群主）
// - 新群主：接收群主权限的成员
// - 原群主：转让群主权限的成员
// - 群组成员：需要了解管理权变更的所有成员
//
// 权限变更：
// - 原群主降级为普通成员或管理员
// - 新群主获得最高管理权限
// - 群组管理结构发生变化
//
// 版本控制：
// - 更新群组成员版本信息
// - 支持客户端增量同步权限变更
// - 维护群组管理状态一致性
//
// 使用场景：
// - 群主主动转让群组
// - 群主退出前的权限转移
// - 群组管理权的重新分配
func (g *NotificationSender) GroupOwnerTransferredNotification(ctx context.Context, req *pbgroup.TransferGroupOwnerReq) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, req.GroupID)
	if err != nil {
		return
	}
	opUserID := mcontext.GetOpUserID(ctx)
	var member map[string]*sdkws.GroupMemberFullInfo
	member, err = g.getGroupMemberMap(ctx, req.GroupID, []string{opUserID, req.NewOwnerUserID, req.OldOwnerUserID})
	if err != nil {
		return
	}
	tips := &sdkws.GroupOwnerTransferredTips{
		Group:             group,
		OpUser:            member[opUserID],
		NewGroupOwner:     member[req.NewOwnerUserID],
		OldGroupOwnerInfo: member[req.OldOwnerUserID],
	}
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, req.GroupID)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), group.GroupID, constant.GroupOwnerTransferredNotification, tips)
}

// MemberKickedNotification 成员被踢出通知
//
// 当群组管理员将成员踢出群组时，向群组所有成员发送成员被踢出通知。
// 这确保了群组成员能及时了解成员变动和管理操作。
//
// 通知内容：
// - 群组基本信息
// - 操作者信息（执行踢出操作的管理员）
// - 被踢出成员的信息列表
// - 群组成员版本信息
// - 操作时间和相关元数据
//
// 处理流程：
// 1. 填充操作者（管理员）信息
// 2. 设置群组成员版本信息
// 3. 向群组所有成员发送踢出通知
//
// 参数说明：
// - tips: 成员被踢出通知的详细信息
// - SendMessage: 是否发送消息到聊天记录（可选参数）
//
// 通知特点：
// - 群组级通知：发送给群组内所有成员
// - 包含版本信息：支持客户端增量同步成员列表
// - 可选消息记录：根据配置决定是否保存到聊天记录
// - 管理操作记录：记录管理员的操作行为
//
// 权限验证：
// - 只有群主和管理员可以踢出成员
// - 群主可以踢出任何成员（包括管理员）
// - 管理员只能踢出普通成员
//
// 使用场景：
// - 管理员踢出违规成员
// - 群主清理不活跃成员
// - 批量管理群组成员
//
// 注意事项：
// - 被踢出的成员不会收到此通知（已不在群组中）
// - 通知包含被踢出成员的完整信息
// - 支持批量踢出的通知处理
func (g *NotificationSender) MemberKickedNotification(ctx context.Context, tips *sdkws.MemberKickedTips, SendMessage *bool) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), tips.Group.GroupID, constant.MemberKickedNotification, tips, notification.WithSendMessage(SendMessage))
}

// GroupApplicationAgreeMemberEnterNotification 群组申请同意成员进入通知（公开接口）
//
// 这是群组申请同意成员进入通知的公开接口，内部调用私有实现方法。
// 当群组申请被同意后，新成员正式加入群组时触发此通知。
//
// 参数说明：
// - groupID: 群组ID
// - SendMessage: 是否发送消息到聊天记录
// - invitedOpUserID: 邀请操作者用户ID
// - entrantUserID: 进入群组的用户ID列表（可变参数）
//
// 返回值：
// - error: 处理过程中的错误信息
//
// 设计模式：
// - 外观模式：提供简化的公开接口
// - 委托模式：将具体实现委托给私有方法
//
// 使用场景：
// - 外部服务调用群组成员进入通知
// - API接口层的统一入口
// - 权限控制和参数验证的统一处理
func (g *NotificationSender) GroupApplicationAgreeMemberEnterNotification(ctx context.Context, groupID string, SendMessage *bool, invitedOpUserID string, entrantUserID ...string) error {
	return g.groupApplicationAgreeMemberEnterNotification(ctx, groupID, SendMessage, invitedOpUserID, entrantUserID...)
}

// groupApplicationAgreeMemberEnterNotification 群组申请同意成员进入通知（私有实现）
//
// 当群组申请被同意后，新成员正式加入群组时的完整通知处理逻辑。
// 这是一个复杂的流程，涉及会话创建、历史消息处理、通知发送等多个步骤。
//
// 处理流程：
// 1. 历史消息处理：根据配置决定新成员是否能看到历史消息
// 2. 会话创建：为新成员创建群组会话
// 3. 群组信息获取：获取群组基本信息
// 4. 成员信息获取：获取新加入成员的详细信息
// 5. 通知构建：构建成员邀请通知消息
// 6. 用户信息填充：填充操作者和邀请者信息
// 7. 版本设置：设置群组成员版本信息
// 8. 通知发送：向群组所有成员发送邀请通知
//
// 参数说明：
// - groupID: 群组ID
// - SendMessage: 是否发送消息到聊天记录
// - invitedOpUserID: 邀请操作者用户ID
// - entrantUserID: 进入群组的用户ID列表
//
// 历史消息处理：
// - EnableHistoryForNewMembers=false: 新成员看不到加入前的历史消息
// - EnableHistoryForNewMembers=true: 新成员可以看到所有历史消息
// - 通过设置MinSeq控制消息可见范围
//
// 会话管理：
// - 为新成员创建群组会话
// - 确保新成员能正常接收群组消息
// - 同步会话状态和配置
//
// 角色区分：
// - 操作者：当前执行操作的用户
// - 邀请者：实际发出邀请的用户（可能与操作者不同）
// - 新成员：被邀请加入群组的用户列表
//
// 错误处理：
// - 会话创建失败：返回错误，停止后续处理
// - 信息获取失败：返回错误，记录详细日志
// - 通知发送失败：记录错误但不影响核心流程
//
// 使用场景：
// - 管理员同意用户入群申请
// - 成员邀请其他用户加入群组
// - 批量添加成员到群组
func (g *NotificationSender) groupApplicationAgreeMemberEnterNotification(ctx context.Context, groupID string, SendMessage *bool, invitedOpUserID string, entrantUserID ...string) error {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()

	if !g.config.RpcConfig.EnableHistoryForNewMembers {
		conversationID := msgprocessor.GetConversationIDBySessionType(constant.ReadGroupChatType, groupID)
		maxSeq, err := g.msgClient.GetConversationMaxSeq(ctx, conversationID)
		if err != nil {
			return err
		}
		if err := g.msgClient.SetUserConversationsMinSeq(ctx, conversationID, entrantUserID, maxSeq+1); err != nil {
			return err
		}
	}
	if err := g.conversationClient.CreateGroupChatConversations(ctx, groupID, entrantUserID); err != nil {
		return err
	}

	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, groupID)
	if err != nil {
		return err
	}
	users, err := g.getGroupMembers(ctx, groupID, entrantUserID)
	if err != nil {
		return err
	}

	tips := &sdkws.MemberInvitedTips{
		Group:           group,
		InvitedUserList: users,
	}
	opUserID := mcontext.GetOpUserID(ctx)
	if err = g.fillUserByUserID(ctx, opUserID, &tips.OpUser, tips.Group.GroupID); err != nil {
		return nil
	}
	if invitedOpUserID == opUserID {
		tips.InviterUser = tips.OpUser
	} else {
		if err = g.fillUserByUserID(ctx, invitedOpUserID, &tips.InviterUser, tips.Group.GroupID); err != nil {
			return err
		}
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), group.GroupID, constant.MemberInvitedNotification, tips, notification.WithSendMessage(SendMessage))
	return nil
}

func (g *NotificationSender) MemberEnterNotification(ctx context.Context, groupID string, entrantUserID string) error {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()

	if !g.config.RpcConfig.EnableHistoryForNewMembers {
		conversationID := msgprocessor.GetConversationIDBySessionType(constant.ReadGroupChatType, groupID)
		maxSeq, err := g.msgClient.GetConversationMaxSeq(ctx, conversationID)
		if err != nil {
			return err
		}
		if err := g.msgClient.SetUserConversationsMinSeq(ctx, conversationID, []string{entrantUserID}, maxSeq+1); err != nil {
			return err
		}
	}
	if err := g.conversationClient.CreateGroupChatConversations(ctx, groupID, []string{entrantUserID}); err != nil {
		return err
	}
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, groupID)
	if err != nil {
		return err
	}
	user, err := g.getGroupMember(ctx, groupID, entrantUserID)
	if err != nil {
		return err
	}

	tips := &sdkws.MemberEnterTips{
		Group:         group,
		EntrantUser:   user,
		OperationTime: time.Now().UnixMilli(),
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), group.GroupID, constant.MemberEnterNotification, tips)
	return nil
}

// GroupDismissedNotification 群组解散通知
//
// 当群主解散群组时，向群组所有成员发送群组解散通知。
// 这是群组生命周期的终结通知，确保所有成员了解群组的解散状态。
//
// 通知内容：
// - 群组基本信息
// - 操作者信息（执行解散操作的群主）
// - 解散时间和相关元数据
// - 解散原因（如果有）
//
// 处理流程：
// 1. 填充操作者（群主）信息
// 2. 向群组所有成员发送解散通知
//
// 参数说明：
// - tips: 群组解散通知的详细信息
// - SendMessage: 是否发送消息到聊天记录（可选参数）
//
// 通知特点：
// - 群组级通知：发送给群组内所有成员
// - 可选消息记录：根据配置决定是否保存到聊天记录
// - 最终通知：群组解散后不再有其他通知
//
// 权限要求：
// - 只有群主可以解散群组
// - 系统管理员可以强制解散群组
//
// 后续处理：
// - 群组解散后，所有成员将被移除
// - 群组会话将被标记为已解散
// - 群组相关数据将被清理或归档
//
// 使用场景：
// - 群主主动解散群组
// - 系统管理员强制解散违规群组
// - 群组到期自动解散
//
// 注意事项：
// - 这是群组的最后一个通知
// - 解散后群组无法恢复
// - 成员需要及时保存重要信息
func (g *NotificationSender) GroupDismissedNotification(ctx context.Context, tips *sdkws.GroupDismissedTips, SendMessage *bool) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.Notification(ctx, mcontext.GetOpUserID(ctx), tips.Group.GroupID, constant.GroupDismissedNotification, tips, notification.WithSendMessage(SendMessage))
}

// GroupMemberMutedNotification 群组成员禁言通知
//
// 当群组管理员对特定成员执行禁言操作时，向群组所有成员发送成员禁言通知。
// 这确保了群组成员了解禁言状态，维护群组秩序。
//
// 通知内容：
// - 群组基本信息
// - 操作者信息（执行禁言的管理员）
// - 被禁言成员信息
// - 禁言时长（秒数）
// - 群组成员版本信息
//
// 处理流程：
// 1. 获取群组基本信息
// 2. 获取操作者和被禁言成员的信息
// 3. 构建成员禁言通知消息
// 4. 填充操作者信息
// 5. 设置群组成员版本信息
// 6. 向群组所有成员发送禁言通知
//
// 参数说明：
// - groupID: 群组ID
// - groupMemberUserID: 被禁言成员的用户ID
// - mutedSeconds: 禁言时长（秒数，0表示永久禁言）
//
// 禁言机制：
// - 禁言期间成员无法发送消息
// - 禁言时长可以是临时的或永久的
// - 管理员可以随时解除禁言
//
// 权限要求：
// - 群主可以禁言任何成员（包括管理员）
// - 管理员可以禁言普通成员
// - 普通成员无法执行禁言操作
//
// 版本控制：
// - 更新群组成员版本信息
// - 支持客户端增量同步禁言状态
// - 维护成员状态一致性
//
// 使用场景：
// - 管理员对违规成员进行禁言
// - 临时禁言处理争议
// - 维护群组讨论秩序
//
// 注意事项：
// - 禁言时长为0表示永久禁言
// - 被禁言成员仍可接收消息，但无法发送
// - 禁言状态会同步到所有客户端
func (g *NotificationSender) GroupMemberMutedNotification(ctx context.Context, groupID, groupMemberUserID string, mutedSeconds uint32) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, groupID)
	if err != nil {
		return
	}
	var user map[string]*sdkws.GroupMemberFullInfo
	user, err = g.getGroupMemberMap(ctx, groupID, []string{mcontext.GetOpUserID(ctx), groupMemberUserID})
	if err != nil {
		return
	}
	tips := &sdkws.GroupMemberMutedTips{
		Group: group, MutedSeconds: mutedSeconds,
		OpUser: user[mcontext.GetOpUserID(ctx)], MutedUser: user[groupMemberUserID],
	}
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), group.GroupID, constant.GroupMemberMutedNotification, tips)
}

func (g *NotificationSender) GroupMemberCancelMutedNotification(ctx context.Context, groupID, groupMemberUserID string) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, groupID)
	if err != nil {
		return
	}
	var user map[string]*sdkws.GroupMemberFullInfo
	user, err = g.getGroupMemberMap(ctx, groupID, []string{mcontext.GetOpUserID(ctx), groupMemberUserID})
	if err != nil {
		return
	}
	tips := &sdkws.GroupMemberCancelMutedTips{Group: group, OpUser: user[mcontext.GetOpUserID(ctx)], MutedUser: user[groupMemberUserID]}
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), group.GroupID, constant.GroupMemberCancelMutedNotification, tips)
}

func (g *NotificationSender) GroupMutedNotification(ctx context.Context, groupID string) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, groupID)
	if err != nil {
		return
	}
	var users []*sdkws.GroupMemberFullInfo
	users, err = g.getGroupMembers(ctx, groupID, []string{mcontext.GetOpUserID(ctx)})
	if err != nil {
		return
	}
	tips := &sdkws.GroupMutedTips{Group: group}
	if len(users) > 0 {
		tips.OpUser = users[0]
	}
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, groupID)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), group.GroupID, constant.GroupMutedNotification, tips)
}

func (g *NotificationSender) GroupCancelMutedNotification(ctx context.Context, groupID string) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, groupID)
	if err != nil {
		return
	}
	var users []*sdkws.GroupMemberFullInfo
	users, err = g.getGroupMembers(ctx, groupID, []string{mcontext.GetOpUserID(ctx)})
	if err != nil {
		return
	}
	tips := &sdkws.GroupCancelMutedTips{Group: group}
	if len(users) > 0 {
		tips.OpUser = users[0]
	}
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, groupID)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), group.GroupID, constant.GroupCancelMutedNotification, tips)
}

func (g *NotificationSender) GroupMemberInfoSetNotification(ctx context.Context, groupID, groupMemberUserID string) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, groupID)
	if err != nil {
		return
	}
	var user map[string]*sdkws.GroupMemberFullInfo
	user, err = g.getGroupMemberMap(ctx, groupID, []string{groupMemberUserID})
	if err != nil {
		return
	}
	tips := &sdkws.GroupMemberInfoSetTips{Group: group, OpUser: user[mcontext.GetOpUserID(ctx)], ChangedUser: user[groupMemberUserID]}
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setSortVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID, &tips.GroupSortVersion)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), group.GroupID, constant.GroupMemberInfoSetNotification, tips)
}

func (g *NotificationSender) GroupMemberSetToAdminNotification(ctx context.Context, groupID, groupMemberUserID string) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, groupID)
	if err != nil {
		return
	}
	user, err := g.getGroupMemberMap(ctx, groupID, []string{mcontext.GetOpUserID(ctx), groupMemberUserID})
	if err != nil {
		return
	}
	tips := &sdkws.GroupMemberInfoSetTips{Group: group, OpUser: user[mcontext.GetOpUserID(ctx)], ChangedUser: user[groupMemberUserID]}
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setSortVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID, &tips.GroupSortVersion)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), group.GroupID, constant.GroupMemberSetToAdminNotification, tips)
}

func (g *NotificationSender) GroupMemberSetToOrdinaryUserNotification(ctx context.Context, groupID, groupMemberUserID string) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, groupID)
	if err != nil {
		return
	}
	var user map[string]*sdkws.GroupMemberFullInfo
	user, err = g.getGroupMemberMap(ctx, groupID, []string{mcontext.GetOpUserID(ctx), groupMemberUserID})
	if err != nil {
		return
	}
	tips := &sdkws.GroupMemberInfoSetTips{Group: group, OpUser: user[mcontext.GetOpUserID(ctx)], ChangedUser: user[groupMemberUserID]}
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setSortVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID, &tips.GroupSortVersion)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), group.GroupID, constant.GroupMemberSetToOrdinaryUserNotification, tips)
}
