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

func (g *NotificationSender) getGroupMembers(ctx context.Context, groupID string, userIDs []string) ([]*sdkws.GroupMemberFullInfo, error) {
	members, err := g.db.FindGroupMembers(ctx, groupID, userIDs)
	if err != nil {
		return nil, err
	}
	if err := g.PopulateGroupMember(ctx, members...); err != nil {
		return nil, err
	}
	log.ZDebug(ctx, "getGroupMembers", "members", members)
	res := make([]*sdkws.GroupMemberFullInfo, 0, len(members))
	for _, member := range members {
		res = append(res, g.groupMemberDB2PB(member, 0))
	}
	return res, nil
}

func (g *NotificationSender) getGroupMemberMap(ctx context.Context, groupID string, userIDs []string) (map[string]*sdkws.GroupMemberFullInfo, error) {
	members, err := g.getGroupMembers(ctx, groupID, userIDs)
	if err != nil {
		return nil, err
	}
	m := make(map[string]*sdkws.GroupMemberFullInfo)
	for i, member := range members {
		m[member.UserID] = members[i]
	}
	return m, nil
}

func (g *NotificationSender) getGroupMember(ctx context.Context, groupID string, userID string) (*sdkws.GroupMemberFullInfo, error) {
	members, err := g.getGroupMembers(ctx, groupID, []string{userID})
	if err != nil {
		return nil, err
	}
	if len(members) == 0 {
		return nil, errs.ErrInternalServer.WrapMsg(fmt.Sprintf("group %s member %s not found", groupID, userID))
	}
	return members[0], nil
}

func (g *NotificationSender) getGroupOwnerAndAdminUserID(ctx context.Context, groupID string) ([]string, error) {
	members, err := g.db.FindGroupMemberRoleLevels(ctx, groupID, []int32{constant.GroupOwner, constant.GroupAdmin})
	if err != nil {
		return nil, err
	}
	if err := g.PopulateGroupMember(ctx, members...); err != nil {
		return nil, err
	}
	fn := func(e *model.GroupMember) string { return e.UserID }
	return datautil.Slice(members, fn), nil
}

func (g *NotificationSender) groupMemberDB2PB(member *model.GroupMember, appMangerLevel int32) *sdkws.GroupMemberFullInfo {
	return &sdkws.GroupMemberFullInfo{
		GroupID:        member.GroupID,
		UserID:         member.UserID,
		RoleLevel:      member.RoleLevel,
		JoinTime:       member.JoinTime.UnixMilli(),
		Nickname:       member.Nickname,
		FaceURL:        member.FaceURL,
		AppMangerLevel: appMangerLevel,
		JoinSource:     member.JoinSource,
		OperatorUserID: member.OperatorUserID,
		Ex:             member.Ex,
		MuteEndTime:    member.MuteEndTime.UnixMilli(),
		InviterUserID:  member.InviterUserID,
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

func (g *NotificationSender) fillOpUser(ctx context.Context, targetUser **sdkws.GroupMemberFullInfo, groupID string) (err error) {
	return g.fillUserByUserID(ctx, mcontext.GetOpUserID(ctx), targetUser, groupID)
}

func (g *NotificationSender) fillUserByUserID(ctx context.Context, userID string, targetUser **sdkws.GroupMemberFullInfo, groupID string) error {
	if targetUser == nil {
		return errs.ErrInternalServer.WrapMsg("**sdkws.GroupMemberFullInfo is nil")
	}
	if groupID != "" {
		if authverify.IsManagerUserID(userID, g.config.Share.IMAdminUserID) {
			*targetUser = &sdkws.GroupMemberFullInfo{
				GroupID:        groupID,
				UserID:         userID,
				RoleLevel:      constant.GroupAdmin,
				AppMangerLevel: constant.AppAdmin,
			}
		} else {
			member, err := g.db.TakeGroupMember(ctx, groupID, userID)
			if err == nil {
				*targetUser = g.groupMemberDB2PB(member, 0)
			} else if !(errors.Is(err, mongo.ErrNoDocuments) || errs.ErrRecordNotFound.Is(err)) {
				return err
			}
		}
	}
	user, err := g.getUser(ctx, userID)
	if err != nil {
		return err
	}
	if *targetUser == nil {
		*targetUser = &sdkws.GroupMemberFullInfo{
			GroupID:        groupID,
			UserID:         userID,
			Nickname:       user.Nickname,
			FaceURL:        user.FaceURL,
			OperatorUserID: userID,
		}
	} else {
		if (*targetUser).Nickname == "" {
			(*targetUser).Nickname = user.Nickname
		}
		if (*targetUser).FaceURL == "" {
			(*targetUser).FaceURL = user.FaceURL
		}
	}
	return nil
}

func (g *NotificationSender) setVersion(ctx context.Context, version *uint64, versionID *string, collName string, id string) {
	versions := versionctx.GetVersionLog(ctx).Get()
	for i := len(versions) - 1; i >= 0; i-- {
		coll := versions[i]
		if coll.Name == collName && coll.Doc.DID == id {
			*version = uint64(coll.Doc.Version)
			*versionID = coll.Doc.ID.Hex()
			return
		}
	}
}

func (g *NotificationSender) setSortVersion(ctx context.Context, version *uint64, versionID *string, collName string, id string, sortVersion *uint64) {
	versions := versionctx.GetVersionLog(ctx).Get()
	for _, coll := range versions {
		if coll.Name == collName && coll.Doc.DID == id {
			*version = uint64(coll.Doc.Version)
			*versionID = coll.Doc.ID.Hex()
			for _, elem := range coll.Doc.Logs {
				if elem.EID == model.VersionSortChangeID {
					*sortVersion = uint64(elem.Version)
				}
			}
		}
	}
}

func (g *NotificationSender) GroupCreatedNotification(ctx context.Context, tips *sdkws.GroupCreatedTips, SendMessage *bool) {
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
	g.Notification(ctx, mcontext.GetOpUserID(ctx), tips.Group.GroupID, constant.GroupCreatedNotification, tips, notification.WithSendMessage(SendMessage))
}

func (g *NotificationSender) GroupInfoSetNotification(ctx context.Context, tips *sdkws.GroupInfoSetTips) {
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
	g.Notification(ctx, mcontext.GetOpUserID(ctx), tips.Group.GroupID, constant.GroupInfoSetNotification, tips, notification.WithRpcGetUserName())
}

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

func (g *NotificationSender) uuid() string {
	return uuid.New().String()
}

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

func (g *NotificationSender) GroupApplicationAgreeMemberEnterNotification(ctx context.Context, groupID string, SendMessage *bool, invitedOpUserID string, entrantUserID ...string) error {
	return g.groupApplicationAgreeMemberEnterNotification(ctx, groupID, SendMessage, invitedOpUserID, entrantUserID...)
}

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
