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
 * 群组核心服务模块
 *
 * 本模块是OpenIM群组系统的核心实现，提供群组管理的所有核心功能：
 *
 * 1. 群组生命周期管理
 *    - 群组创建：支持多成员初始化、角色分配、权限验证
 *    - 群组解散：包含通知推送、数据清理、会话处理
 *    - 群组信息维护：名称、头像、公告、介绍等信息管理
 *
 * 2. 成员管理体系
 *    - 成员邀请：支持批量邀请、权限验证、回调通知
 *    - 成员踢出：管理员权限校验、批量操作、状态同步
 *    - 角色管理：群主、管理员、普通成员的权限控制
 *    - 成员信息：昵称、头像、角色等个性化设置
 *
 * 3. 权限控制系统
 *    - 分级权限：群主 > 管理员 > 普通成员
 *    - 操作权限：加群验证、查看成员、加好友等细分权限
 *    - 禁言管理：个人禁言、全群禁言的时间控制
 *
 * 4. 申请处理流程
 *    - 加群申请：主动申请、邀请验证、审批流程
 *    - 申请列表：待处理申请的分页查询和状态管理
 *    - 申请响应：同意、拒绝的处理逻辑和通知机制
 *
 * 5. 数据同步机制
 *    - 增量同步：基于版本控制的高效数据同步
 *    - 全量同步：故障恢复和首次同步的数据保证
 *    - 多端一致性：确保所有客户端数据的实时一致
 *
 * 6. 技术特性
 *    - 微服务架构：独立部署、水平扩展
 *    - gRPC通信：高性能、跨语言的服务间调用
 *    - 事务保证：MongoDB事务确保数据一致性
 *    - 缓存优化：Redis多级缓存提升性能
 *    - Webhook扩展：业务逻辑的灵活扩展机制
 *
 * 7. 监控与可观测性
 *    - 操作日志：完整的操作链路追踪
 *    - 性能指标：接口耗时、并发量监控
 *    - 错误处理：统一的错误码和异常处理
 *
 * 技术栈：
 * - gRPC: 服务间高性能通信
 * - MongoDB: 群组数据持久化存储
 * - Redis: 高速缓存和分布式锁
 * - Webhook: 业务扩展和第三方集成
 */
package group

import (
	"context"
	"fmt"
	"math/big"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/rpcli"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/common"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database/mgo"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/open-im-server/v3/pkg/common/webhook"
	"github.com/openimsdk/open-im-server/v3/pkg/localcache"

	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/open-im-server/v3/pkg/callbackstruct"
	"github.com/openimsdk/open-im-server/v3/pkg/common/convert"
	"github.com/openimsdk/open-im-server/v3/pkg/common/servererrs"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/controller"
	"github.com/openimsdk/open-im-server/v3/pkg/msgprocessor"
	"github.com/openimsdk/open-im-server/v3/pkg/notification/grouphash"
	"github.com/openimsdk/protocol/constant"
	pbconversation "github.com/openimsdk/protocol/conversation"
	pbgroup "github.com/openimsdk/protocol/group"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/protocol/wrapperspb"
	"github.com/openimsdk/tools/db/mongoutil"
	"github.com/openimsdk/tools/db/redisutil"
	"github.com/openimsdk/tools/discovery"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
	"github.com/openimsdk/tools/mw/specialerror"
	"github.com/openimsdk/tools/utils/datautil"
	"github.com/openimsdk/tools/utils/encrypt"
	"google.golang.org/grpc"
)

// groupServer 群组服务核心结构体
//
// 实现了群组系统的所有gRPC服务接口，采用依赖注入的设计模式，
// 整合了数据库操作、通知服务、缓存管理、Webhook回调等功能模块。
//
// 架构特点：
// - 微服务设计：独立的群组服务，可单独部署和扩展
// - 依赖注入：松耦合的模块设计，便于测试和维护
// - 统一接口：通过gRPC提供标准化的服务接口
// - 事件驱动：基于通知机制的异步处理
type groupServer struct {
	pbgroup.UnimplementedGroupServer                           // gRPC服务基础实现
	db                               controller.GroupDatabase  // 群组数据库操作层，提供CRUD和复杂查询
	notification                     *NotificationSender       // 通知发送器，处理群组相关的所有通知
	config                           *Config                   // 服务配置，包含各种配置参数
	webhookClient                    *webhook.Client           // Webhook客户端，用于业务扩展回调
	userClient                       *rpcli.UserClient         // 用户服务客户端，获取用户信息
	msgClient                        *rpcli.MsgClient          // 消息服务客户端，发送系统消息
	conversationClient               *rpcli.ConversationClient // 会话服务客户端，管理群组会话
}

// Config 群组服务配置结构体
//
// 集中管理群组服务运行所需的所有配置参数，包括：
// - 服务配置：端口、超时、限流等服务运行参数
// - 存储配置：数据库连接、缓存设置
// - 第三方集成：Webhook地址、服务发现配置
// - 业务配置：通知设置、共享配置等
//
// 配置分类：
// 1. 基础设施配置：数据库、缓存、服务发现
// 2. 业务功能配置：通知、Webhook、权限控制
// 3. 性能配置：本地缓存、连接池、超时设置
type Config struct {
	RpcConfig          config.Group        // gRPC服务配置：端口、超时、中间件等
	RedisConfig        config.Redis        // Redis配置：连接信息、连接池设置
	MongodbConfig      config.Mongo        // MongoDB配置：连接字符串、数据库设置
	NotificationConfig config.Notification // 通知服务配置：推送设置、模板配置
	Share              config.Share        // 共享配置：服务名称、管理员ID等
	WebhooksConfig     config.Webhooks     // Webhook配置：回调地址、安全设置
	LocalCacheConfig   config.LocalCache   // 本地缓存配置：大小、过期时间
	Discovery          config.Discovery    // 服务发现配置：注册中心设置
}

// Start 启动群组服务
//
// 这是群组服务的启动入口函数，负责完成所有的初始化工作：
//
// 初始化流程（10个关键步骤）：
// 1. 数据库连接初始化：建立MongoDB和Redis连接
// 2. 数据访问层初始化：创建群组、成员、申请的数据访问对象
// 3. 外部服务连接：建立用户、消息、会话服务的gRPC连接
// 4. 核心组件装配：组装groupServer的所有依赖组件
// 5. 数据库控制器创建：整合所有数据访问层和缓存策略
// 6. 通知服务初始化：配置群组相关的所有通知机制
// 7. 本地缓存启动：启动高性能的本地缓存服务
// 8. gRPC服务注册：将服务注册到gRPC服务器
// 9. 健康检查配置：设置服务健康状态监控
// 10. 优雅启动完成：服务就绪，开始接收请求
//
// 容错设计：
// - 连接失败重试：数据库和外部服务连接失败时的重试机制
// - 启动超时控制：避免启动过程无限等待
// - 资源清理：启动失败时的资源清理逻辑
// - 健康检查：启动后的服务可用性验证
func Start(ctx context.Context, config *Config, client discovery.SvcDiscoveryRegistry, server *grpc.Server) error {
	// 第1步：初始化MongoDB连接
	// 创建MongoDB客户端，用于群组数据的持久化存储
	mgocli, err := mongoutil.NewMongoDB(ctx, config.MongodbConfig.Build())
	if err != nil {
		return err
	}

	// 第2步：初始化Redis连接
	// 创建Redis客户端，用于缓存和分布式锁
	rdb, err := redisutil.NewRedisClient(ctx, config.RedisConfig.Build())
	if err != nil {
		return err
	}

	// 第3步：初始化数据访问层
	// 创建群组基础信息的数据访问对象
	groupDB, err := mgo.NewGroupMongo(mgocli.GetDB())
	if err != nil {
		return err
	}

	// 创建群组成员的数据访问对象
	groupMemberDB, err := mgo.NewGroupMember(mgocli.GetDB())
	if err != nil {
		return err
	}

	// 创建群组申请的数据访问对象
	groupRequestDB, err := mgo.NewGroupRequestMgo(mgocli.GetDB())
	if err != nil {
		return err
	}

	// 第4步：建立外部服务连接
	// 建立用户服务连接，用于获取用户信息和权限验证
	userConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.User)
	if err != nil {
		return err
	}

	// 建立消息服务连接，用于发送群组通知消息
	msgConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.Msg)
	if err != nil {
		return err
	}

	// 建立会话服务连接，用于管理群组会话状态
	conversationConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.Conversation)
	if err != nil {
		return err
	}

	// 第5步：组装群组服务实例
	// 创建群组服务器实例，注入所有依赖
	gs := groupServer{
		config:             config,                                              // 服务配置
		webhookClient:      webhook.NewWebhookClient(config.WebhooksConfig.URL), // Webhook客户端
		userClient:         rpcli.NewUserClient(userConn),                       // 用户服务客户端
		msgClient:          rpcli.NewMsgClient(msgConn),                         // 消息服务客户端
		conversationClient: rpcli.NewConversationClient(conversationConn),       // 会话服务客户端
	}

	// 第6步：初始化数据库控制器
	// 创建统一的数据库控制器，集成缓存、事务、版本控制等功能
	gs.db = controller.NewGroupDatabase(rdb, &config.LocalCacheConfig, groupDB, groupMemberDB, groupRequestDB, mgocli.GetTx(), grouphash.NewGroupHashFromGroupServer(&gs))

	// 第7步：初始化通知服务
	// 创建通知发送器，用于处理群组相关的所有通知
	gs.notification = NewNotificationSender(gs.db, config, gs.userClient, gs.msgClient, gs.conversationClient)

	// 第8步：启动本地缓存
	// 初始化高性能本地缓存，提升热点数据访问速度
	localcache.InitLocalCache(&config.LocalCacheConfig)

	// 第9步：注册gRPC服务
	// 将群组服务注册到gRPC服务器，开始接收客户端请求
	pbgroup.RegisterGroupServer(server, &gs)

	// 第10步：启动完成
	// 服务启动成功，所有组件就绪
	return nil
}

// NotificationUserInfoUpdate 处理用户信息更新通知
//
// 当用户的基础信息（昵称、头像等）发生变化时，需要同步更新该用户在所有群组中的显示信息。
// 这个方法实现了用户信息变更的级联更新机制。
//
// 业务逻辑：
// 1. 查找用户参与的所有群组
// 2. 筛选需要更新的群组（用户在群内没有自定义昵称和头像的）
// 3. 增加群组成员的版本号，触发客户端同步
// 4. 发送群组成员信息变更通知
// 5. 清理相关缓存，确保数据一致性
//
// 性能优化：
// - 只更新必要的群组：跳过用户已自定义信息的群组
// - 批量操作：一次性处理多个群组的版本更新
// - 异步通知：通知发送不阻塞主流程
func (g *groupServer) NotificationUserInfoUpdate(ctx context.Context, req *pbgroup.NotificationUserInfoUpdateReq) (*pbgroup.NotificationUserInfoUpdateResp, error) {
	// 查找用户参与的所有群组成员记录
	members, err := g.db.FindGroupMemberUser(ctx, nil, req.UserID)
	if err != nil {
		return nil, err
	}

	// 筛选需要更新的群组ID
	// 如果用户在群内已设置自定义昵称和头像，则无需更新
	groupIDs := make([]string, 0, len(members))
	for _, member := range members {
		if member.Nickname != "" && member.FaceURL != "" {
			continue // 用户已自定义群内信息，跳过
		}
		groupIDs = append(groupIDs, member.GroupID)
	}

	// 批量更新群组成员版本，触发客户端增量同步
	for _, groupID := range groupIDs {
		if err := g.db.MemberGroupIncrVersion(ctx, groupID, []string{req.UserID}, model.VersionStateUpdate); err != nil {
			return nil, err
		}
	}

	// 发送群组成员信息变更通知
	for _, groupID := range groupIDs {
		g.notification.GroupMemberInfoSetNotification(ctx, groupID, req.UserID)
	}

	// 清理群组成员缓存，确保下次查询获取最新数据
	if err = g.db.DeleteGroupMemberHash(ctx, groupIDs); err != nil {
		return nil, err
	}

	return &pbgroup.NotificationUserInfoUpdateResp{}, nil
}

// CheckGroupAdmin 检查用户是否为群组管理员
//
// 这是一个核心的权限验证方法，用于验证操作者是否有权限执行管理员级别的操作。
// 权限层级：系统管理员 > 群主 > 群管理员 > 普通成员
//
// 权限验证逻辑：
// 1. 系统管理员：拥有所有群组的最高权限，可执行任何操作
// 2. 群主：拥有该群组的完全控制权
// 3. 群管理员：拥有该群组的部分管理权限
// 4. 普通成员：只有基础的群组功能权限
//
// 安全设计：
// - 多层权限验证：系统级 -> 群组级 -> 角色级
// - 操作者身份确认：通过上下文获取真实操作者ID
// - 权限等级校验：确保操作者具备足够的权限等级
func (g *groupServer) CheckGroupAdmin(ctx context.Context, groupID string) error {
	// 检查是否为系统管理员，系统管理员拥有所有权限
	if !authverify.IsAppManagerUid(ctx, g.config.Share.IMAdminUserID) {
		// 非系统管理员，需要检查群组内的角色权限
		// 获取操作者在该群组中的成员信息
		groupMember, err := g.db.TakeGroupMember(ctx, groupID, mcontext.GetOpUserID(ctx))
		if err != nil {
			return err
		}

		// 验证角色权限：只有群主和群管理员可以执行管理操作
		if !(groupMember.RoleLevel == constant.GroupOwner || groupMember.RoleLevel == constant.GroupAdmin) {
			return errs.ErrNoPermission.WrapMsg("no group owner or admin")
		}
	}
	return nil
}

// IsNotFound 判断错误是否为记录不存在错误
//
// 这是一个辅助方法，用于统一处理数据库查询的"记录不存在"错误。
// 在群组系统中，经常需要区分"记录不存在"和其他类型的错误，
// 以便提供更准确的错误处理和用户反馈。
//
// 使用场景：
// - 群组不存在的判断
// - 成员不存在的判断
// - 申请记录不存在的判断
// - 权限验证中的存在性检查
func (g *groupServer) IsNotFound(err error) bool {
	return errs.ErrRecordNotFound.Is(specialerror.ErrCode(errs.Unwrap(err)))
}

// GenGroupID 生成群组ID
//
// 群组ID生成策略，支持两种模式：
// 1. 指定ID模式：使用客户端提供的群组ID（需验证唯一性）
// 2. 自动生成模式：系统自动生成唯一的群组ID
//
// 自动生成算法：
// 1. 基础数据：操作ID + 当前纳秒时间戳 + 随机数
// 2. 哈希处理：MD5哈希确保数据混淆
// 3. 进制转换：取前8位转为16进制，再转为10进制
// 4. 唯一性验证：检查生成的ID是否已存在
// 5. 重试机制：最多重试10次，确保ID生成成功
//
// 安全考虑：
// - 防重复：多重验证确保ID唯一性
// - 防预测：使用随机数和时间戳增加不可预测性
// - 防冲突：重试机制处理极低概率的ID冲突
func (g *groupServer) GenGroupID(ctx context.Context, groupID *string) error {
	// 如果指定了群组ID，验证其可用性
	if *groupID != "" {
		_, err := g.db.TakeGroup(ctx, *groupID)
		if err == nil {
			// ID已存在，返回错误
			return servererrs.ErrGroupIDExisted.WrapMsg("group id existed " + *groupID)
		} else if g.IsNotFound(err) {
			// ID不存在，可以使用
			return nil
		} else {
			// 其他错误，返回
			return err
		}
	}

	// 自动生成群组ID，最多重试10次
	for i := 0; i < 10; i++ {
		// 组合基础数据：操作ID + 时间戳 + 随机数
		id := encrypt.Md5(strings.Join([]string{
			mcontext.GetOperationID(ctx),
			strconv.FormatInt(time.Now().UnixNano(), 10),
			strconv.Itoa(rand.Int()),
		}, ",;,"))

		// 取MD5哈希的前8位，转换为大整数
		bi := big.NewInt(0)
		bi.SetString(id[0:8], 16)
		id = bi.String()

		// 验证生成的ID是否可用
		_, err := g.db.TakeGroup(ctx, id)
		if err == nil {
			// ID已存在，继续下一次生成
			continue
		} else if g.IsNotFound(err) {
			// ID可用，设置并返回
			*groupID = id
			return nil
		} else {
			// 其他错误，返回
			return err
		}
	}

	// 10次重试都失败，返回生成失败错误
	return servererrs.ErrData.WrapMsg("group id gen error")
}

func (g *groupServer) CreateGroup(ctx context.Context, req *pbgroup.CreateGroupReq) (*pbgroup.CreateGroupResp, error) {
	if req.GroupInfo.GroupType != constant.WorkingGroup {
		return nil, errs.ErrArgs.WrapMsg(fmt.Sprintf("group type only supports %d", constant.WorkingGroup))
	}
	if req.OwnerUserID == "" {
		return nil, errs.ErrArgs.WrapMsg("no group owner")
	}
	if err := authverify.CheckAccessV3(ctx, req.OwnerUserID, g.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}
	userIDs := append(append(req.MemberUserIDs, req.AdminUserIDs...), req.OwnerUserID)
	opUserID := mcontext.GetOpUserID(ctx)
	if !datautil.Contain(opUserID, userIDs...) {
		userIDs = append(userIDs, opUserID)
	}

	if datautil.Duplicate(userIDs) {
		return nil, errs.ErrArgs.WrapMsg("group member repeated")
	}

	userMap, err := g.userClient.GetUsersInfoMap(ctx, userIDs)
	if err != nil {
		return nil, err
	}

	if len(userMap) != len(userIDs) {
		return nil, servererrs.ErrUserIDNotFound.WrapMsg("user not found")
	}

	if err := g.webhookBeforeCreateGroup(ctx, &g.config.WebhooksConfig.BeforeCreateGroup, req); err != nil && err != servererrs.ErrCallbackContinue {
		return nil, err
	}

	var groupMembers []*model.GroupMember
	group := convert.Pb2DBGroupInfo(req.GroupInfo)
	if err := g.GenGroupID(ctx, &group.GroupID); err != nil {
		return nil, err
	}

	joinGroupFunc := func(userID string, roleLevel int32) {
		groupMember := &model.GroupMember{
			GroupID:        group.GroupID,
			UserID:         userID,
			RoleLevel:      roleLevel,
			OperatorUserID: opUserID,
			JoinSource:     constant.JoinByInvitation,
			InviterUserID:  opUserID,
			JoinTime:       time.Now(),
			MuteEndTime:    time.UnixMilli(0),
		}

		groupMembers = append(groupMembers, groupMember)
	}

	joinGroupFunc(req.OwnerUserID, constant.GroupOwner)

	for _, userID := range req.AdminUserIDs {
		joinGroupFunc(userID, constant.GroupAdmin)
	}

	for _, userID := range req.MemberUserIDs {
		joinGroupFunc(userID, constant.GroupOrdinaryUsers)
	}

	if err := g.webhookBeforeMembersJoinGroup(ctx, &g.config.WebhooksConfig.BeforeMemberJoinGroup, groupMembers, group.GroupID, group.Ex); err != nil && err != servererrs.ErrCallbackContinue {
		return nil, err
	}

	if err := g.db.CreateGroup(ctx, []*model.Group{group}, groupMembers); err != nil {
		return nil, err
	}
	resp := &pbgroup.CreateGroupResp{GroupInfo: &sdkws.GroupInfo{}}

	resp.GroupInfo = convert.Db2PbGroupInfo(group, req.OwnerUserID, uint32(len(userIDs)))
	resp.GroupInfo.MemberCount = uint32(len(userIDs))
	tips := &sdkws.GroupCreatedTips{
		Group:          resp.GroupInfo,
		OperationTime:  group.CreateTime.UnixMilli(),
		GroupOwnerUser: g.groupMemberDB2PB(groupMembers[0], userMap[groupMembers[0].UserID].AppMangerLevel),
	}
	for _, member := range groupMembers {
		member.Nickname = userMap[member.UserID].Nickname
		tips.MemberList = append(tips.MemberList, g.groupMemberDB2PB(member, userMap[member.UserID].AppMangerLevel))
		if member.UserID == opUserID {
			tips.OpUser = g.groupMemberDB2PB(member, userMap[member.UserID].AppMangerLevel)
			break
		}
	}
	g.notification.GroupCreatedNotification(ctx, tips, req.SendMessage)

	if req.GroupInfo.Notification != "" {
		notificationFlag := true
		g.notification.GroupInfoSetAnnouncementNotification(ctx, &sdkws.GroupInfoSetAnnouncementTips{
			Group:  tips.Group,
			OpUser: tips.OpUser,
		}, &notificationFlag)
	}

	reqCallBackAfter := &pbgroup.CreateGroupReq{
		MemberUserIDs: userIDs,
		GroupInfo:     resp.GroupInfo,
		OwnerUserID:   req.OwnerUserID,
		AdminUserIDs:  req.AdminUserIDs,
	}

	g.webhookAfterCreateGroup(ctx, &g.config.WebhooksConfig.AfterCreateGroup, reqCallBackAfter)

	return resp, nil
}

func (g *groupServer) GetJoinedGroupList(ctx context.Context, req *pbgroup.GetJoinedGroupListReq) (*pbgroup.GetJoinedGroupListResp, error) {
	if err := authverify.CheckAccessV3(ctx, req.FromUserID, g.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}
	total, members, err := g.db.PageGetJoinGroup(ctx, req.FromUserID, req.Pagination)
	if err != nil {
		return nil, err
	}
	var resp pbgroup.GetJoinedGroupListResp
	resp.Total = uint32(total)
	if len(members) == 0 {
		return &resp, nil
	}
	groupIDs := datautil.Slice(members, func(e *model.GroupMember) string {
		return e.GroupID
	})
	groups, err := g.db.FindGroup(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	groupMemberNum, err := g.db.MapGroupMemberNum(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	owners, err := g.db.FindGroupsOwner(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	if err := g.PopulateGroupMember(ctx, members...); err != nil {
		return nil, err
	}
	ownerMap := datautil.SliceToMap(owners, func(e *model.GroupMember) string {
		return e.GroupID
	})
	resp.Groups = datautil.Slice(datautil.Order(groupIDs, groups, func(group *model.Group) string {
		return group.GroupID
	}), func(group *model.Group) *sdkws.GroupInfo {
		var userID string
		if user := ownerMap[group.GroupID]; user != nil {
			userID = user.UserID
		}
		return convert.Db2PbGroupInfo(group, userID, groupMemberNum[group.GroupID])
	})
	return &resp, nil
}

func (g *groupServer) InviteUserToGroup(ctx context.Context, req *pbgroup.InviteUserToGroupReq) (*pbgroup.InviteUserToGroupResp, error) {
	if len(req.InvitedUserIDs) == 0 {
		return nil, errs.ErrArgs.WrapMsg("user empty")
	}
	if datautil.Duplicate(req.InvitedUserIDs) {
		return nil, errs.ErrArgs.WrapMsg("userID duplicate")
	}
	group, err := g.db.TakeGroup(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}

	if group.Status == constant.GroupStatusDismissed {
		return nil, servererrs.ErrDismissedAlready.WrapMsg("group dismissed checking group status found it dismissed")
	}

	userMap, err := g.userClient.GetUsersInfoMap(ctx, req.InvitedUserIDs)
	if err != nil {
		return nil, err
	}

	if len(userMap) != len(req.InvitedUserIDs) {
		return nil, errs.ErrRecordNotFound.WrapMsg("user not found")
	}

	var groupMember *model.GroupMember
	var opUserID string
	if !authverify.IsAppManagerUid(ctx, g.config.Share.IMAdminUserID) {
		opUserID = mcontext.GetOpUserID(ctx)
		var err error
		groupMember, err = g.db.TakeGroupMember(ctx, req.GroupID, opUserID)
		if err != nil {
			return nil, err
		}
		if err := g.PopulateGroupMember(ctx, groupMember); err != nil {
			return nil, err
		}
	} else {
		opUserID = mcontext.GetOpUserID(ctx)
	}

	if err := g.webhookBeforeInviteUserToGroup(ctx, &g.config.WebhooksConfig.BeforeInviteUserToGroup, req); err != nil && err != servererrs.ErrCallbackContinue {
		return nil, err
	}

	if group.NeedVerification == constant.AllNeedVerification {
		if !authverify.IsAppManagerUid(ctx, g.config.Share.IMAdminUserID) {
			if !(groupMember.RoleLevel == constant.GroupOwner || groupMember.RoleLevel == constant.GroupAdmin) {
				var requests []*model.GroupRequest
				for _, userID := range req.InvitedUserIDs {
					requests = append(requests, &model.GroupRequest{
						UserID:        userID,
						GroupID:       req.GroupID,
						JoinSource:    constant.JoinByInvitation,
						InviterUserID: opUserID,
						ReqTime:       time.Now(),
						HandledTime:   time.Unix(0, 0),
					})
				}
				if err := g.db.CreateGroupRequest(ctx, requests); err != nil {
					return nil, err
				}
				for _, request := range requests {
					g.notification.JoinGroupApplicationNotification(ctx, &pbgroup.JoinGroupReq{
						GroupID:       request.GroupID,
						ReqMessage:    request.ReqMsg,
						JoinSource:    request.JoinSource,
						InviterUserID: request.InviterUserID,
					}, request)
				}
				return &pbgroup.InviteUserToGroupResp{}, nil
			}
		}
	}

	var groupMembers []*model.GroupMember
	for _, userID := range req.InvitedUserIDs {
		member := &model.GroupMember{
			GroupID:        req.GroupID,
			UserID:         userID,
			RoleLevel:      constant.GroupOrdinaryUsers,
			OperatorUserID: opUserID,
			InviterUserID:  opUserID,
			JoinSource:     constant.JoinByInvitation,
			JoinTime:       time.Now(),
			MuteEndTime:    time.UnixMilli(0),
		}

		groupMembers = append(groupMembers, member)
	}

	if err := g.webhookBeforeMembersJoinGroup(ctx, &g.config.WebhooksConfig.BeforeMemberJoinGroup, groupMembers, group.GroupID, group.Ex); err != nil && err != servererrs.ErrCallbackContinue {
		return nil, err
	}

	const singleQuantity = 50
	for start := 0; start < len(groupMembers); start += singleQuantity {
		end := start + singleQuantity
		if end > len(groupMembers) {
			end = len(groupMembers)
		}
		currentMembers := groupMembers[start:end]

		if err := g.db.CreateGroup(ctx, nil, currentMembers); err != nil {
			return nil, err
		}

		userIDs := datautil.Slice(currentMembers, func(e *model.GroupMember) string {
			return e.UserID
		})

		if err = g.notification.GroupApplicationAgreeMemberEnterNotification(ctx, req.GroupID, req.SendMessage, opUserID, userIDs...); err != nil {
			return nil, err
		}
	}
	return &pbgroup.InviteUserToGroupResp{}, nil
}

func (g *groupServer) GetGroupAllMember(ctx context.Context, req *pbgroup.GetGroupAllMemberReq) (*pbgroup.GetGroupAllMemberResp, error) {
	members, err := g.db.FindGroupMemberAll(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}
	if err := g.PopulateGroupMember(ctx, members...); err != nil {
		return nil, err
	}
	var resp pbgroup.GetGroupAllMemberResp
	resp.Members = datautil.Slice(members, func(e *model.GroupMember) *sdkws.GroupMemberFullInfo {
		return convert.Db2PbGroupMember(e)
	})
	return &resp, nil
}

func (g *groupServer) checkAdminOrInGroup(ctx context.Context, groupID string) error {
	if authverify.IsAppManagerUid(ctx, g.config.Share.IMAdminUserID) {
		return nil
	}
	opUserID := mcontext.GetOpUserID(ctx)
	members, err := g.db.FindGroupMembers(ctx, groupID, []string{opUserID})
	if err != nil {
		return err
	}
	if len(members) == 0 {
		return errs.ErrNoPermission.WrapMsg("op user not in group")
	}
	return nil
}

func (g *groupServer) GetGroupMemberList(ctx context.Context, req *pbgroup.GetGroupMemberListReq) (*pbgroup.GetGroupMemberListResp, error) {
	if err := g.checkAdminOrInGroup(ctx, req.GroupID); err != nil {
		return nil, err
	}
	var (
		total   int64
		members []*model.GroupMember
		err     error
	)
	if req.Keyword == "" {
		total, members, err = g.db.PageGetGroupMember(ctx, req.GroupID, req.Pagination)
	} else {
		total, members, err = g.db.SearchGroupMember(ctx, req.GroupID, req.Keyword, req.Pagination)
	}
	if err != nil {
		return nil, err
	}
	if err := g.PopulateGroupMember(ctx, members...); err != nil {
		return nil, err
	}
	return &pbgroup.GetGroupMemberListResp{
		Total:   uint32(total),
		Members: datautil.Batch(convert.Db2PbGroupMember, members),
	}, nil
}

func (g *groupServer) KickGroupMember(ctx context.Context, req *pbgroup.KickGroupMemberReq) (*pbgroup.KickGroupMemberResp, error) {
	group, err := g.db.TakeGroup(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}
	if len(req.KickedUserIDs) == 0 {
		return nil, errs.ErrArgs.WrapMsg("KickedUserIDs empty")
	}
	if datautil.Duplicate(req.KickedUserIDs) {
		return nil, errs.ErrArgs.WrapMsg("KickedUserIDs duplicate")
	}
	opUserID := mcontext.GetOpUserID(ctx)
	if datautil.Contain(opUserID, req.KickedUserIDs...) {
		return nil, errs.ErrArgs.WrapMsg("opUserID in KickedUserIDs")
	}
	owner, err := g.db.TakeGroupOwner(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}
	if datautil.Contain(owner.UserID, req.KickedUserIDs...) {
		return nil, errs.ErrArgs.WrapMsg("ownerUID can not Kick")
	}

	members, err := g.db.FindGroupMembers(ctx, req.GroupID, append(req.KickedUserIDs, opUserID))
	if err != nil {
		return nil, err
	}
	if err := g.PopulateGroupMember(ctx, members...); err != nil {
		return nil, err
	}
	memberMap := make(map[string]*model.GroupMember)
	for i, member := range members {
		memberMap[member.UserID] = members[i]
	}
	isAppManagerUid := authverify.IsAppManagerUid(ctx, g.config.Share.IMAdminUserID)
	opMember := memberMap[opUserID]
	for _, userID := range req.KickedUserIDs {
		member, ok := memberMap[userID]
		if !ok {
			return nil, servererrs.ErrUserIDNotFound.WrapMsg(userID)
		}
		if !isAppManagerUid {
			if opMember == nil {
				return nil, errs.ErrNoPermission.WrapMsg("opUserID no in group")
			}
			switch opMember.RoleLevel {
			case constant.GroupOwner:
			case constant.GroupAdmin:
				if member.RoleLevel == constant.GroupOwner || member.RoleLevel == constant.GroupAdmin {
					return nil, errs.ErrNoPermission.WrapMsg("group admins cannot remove the group owner and other admins")
				}
			case constant.GroupOrdinaryUsers:
				return nil, errs.ErrNoPermission.WrapMsg("opUserID no permission")
			default:
				return nil, errs.ErrNoPermission.WrapMsg("opUserID roleLevel unknown")
			}
		}
	}
	num, err := g.db.FindGroupMemberNum(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}
	ownerUserIDs, err := g.db.GetGroupRoleLevelMemberIDs(ctx, req.GroupID, constant.GroupOwner)
	if err != nil {
		return nil, err
	}
	var ownerUserID string
	if len(ownerUserIDs) > 0 {
		ownerUserID = ownerUserIDs[0]
	}
	if err := g.db.DeleteGroupMember(ctx, group.GroupID, req.KickedUserIDs); err != nil {
		return nil, err
	}
	tips := &sdkws.MemberKickedTips{
		Group: &sdkws.GroupInfo{
			GroupID:                group.GroupID,
			GroupName:              group.GroupName,
			Notification:           group.Notification,
			Introduction:           group.Introduction,
			FaceURL:                group.FaceURL,
			OwnerUserID:            ownerUserID,
			CreateTime:             group.CreateTime.UnixMilli(),
			MemberCount:            num - uint32(len(req.KickedUserIDs)),
			Ex:                     group.Ex,
			Status:                 group.Status,
			CreatorUserID:          group.CreatorUserID,
			GroupType:              group.GroupType,
			NeedVerification:       group.NeedVerification,
			LookMemberInfo:         group.LookMemberInfo,
			ApplyMemberFriend:      group.ApplyMemberFriend,
			NotificationUpdateTime: group.NotificationUpdateTime.UnixMilli(),
			NotificationUserID:     group.NotificationUserID,
		},
		KickedUserList: []*sdkws.GroupMemberFullInfo{},
	}
	if opMember, ok := memberMap[opUserID]; ok {
		tips.OpUser = convert.Db2PbGroupMember(opMember)
	}
	for _, userID := range req.KickedUserIDs {
		tips.KickedUserList = append(tips.KickedUserList, convert.Db2PbGroupMember(memberMap[userID]))
	}
	g.notification.MemberKickedNotification(ctx, tips, req.SendMessage)
	if err := g.deleteMemberAndSetConversationSeq(ctx, req.GroupID, req.KickedUserIDs); err != nil {
		return nil, err
	}
	g.webhookAfterKickGroupMember(ctx, &g.config.WebhooksConfig.AfterKickGroupMember, req)

	return &pbgroup.KickGroupMemberResp{}, nil
}

func (g *groupServer) GetGroupMembersInfo(ctx context.Context, req *pbgroup.GetGroupMembersInfoReq) (*pbgroup.GetGroupMembersInfoResp, error) {
	if len(req.UserIDs) == 0 {
		return nil, errs.ErrArgs.WrapMsg("userIDs empty")
	}
	if req.GroupID == "" {
		return nil, errs.ErrArgs.WrapMsg("groupID empty")
	}
	if err := g.checkAdminOrInGroup(ctx, req.GroupID); err != nil {
		return nil, err
	}
	members, err := g.getGroupMembersInfo(ctx, req.GroupID, req.UserIDs)
	if err != nil {
		return nil, err
	}
	return &pbgroup.GetGroupMembersInfoResp{
		Members: members,
	}, nil
}

func (g *groupServer) getGroupMembersInfo(ctx context.Context, groupID string, userIDs []string) ([]*sdkws.GroupMemberFullInfo, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	members, err := g.db.FindGroupMembers(ctx, groupID, userIDs)
	if err != nil {
		return nil, err
	}
	if err := g.PopulateGroupMember(ctx, members...); err != nil {
		return nil, err
	}
	return datautil.Slice(members, func(e *model.GroupMember) *sdkws.GroupMemberFullInfo {
		return convert.Db2PbGroupMember(e)
	}), nil
}

// GetGroupApplicationList handles functions that get a list of group requests.
func (g *groupServer) GetGroupApplicationList(ctx context.Context, req *pbgroup.GetGroupApplicationListReq) (*pbgroup.GetGroupApplicationListResp, error) {
	var (
		groupIDs []string
		err      error
	)
	if len(req.GroupIDs) == 0 {
		groupIDs, err = g.db.FindUserManagedGroupID(ctx, req.FromUserID)
		if err != nil {
			return nil, err
		}
	} else {
		req.GroupIDs = datautil.Distinct(req.GroupIDs)
		if !authverify.IsAppManagerUid(ctx, g.config.Share.IMAdminUserID) {
			for _, groupID := range req.GroupIDs {
				if err := g.CheckGroupAdmin(ctx, groupID); err != nil {
					return nil, err
				}
			}
		}
		groupIDs = req.GroupIDs
	}
	resp := &pbgroup.GetGroupApplicationListResp{}
	if len(groupIDs) == 0 {
		return resp, nil
	}
	handleResults := datautil.Slice(req.HandleResults, func(e int32) int {
		return int(e)
	})
	total, groupRequests, err := g.db.PageGroupRequest(ctx, groupIDs, handleResults, req.Pagination)
	if err != nil {
		return nil, err
	}
	resp.Total = uint32(total)
	if len(groupRequests) == 0 {
		return resp, nil
	}
	var userIDs []string

	for _, gr := range groupRequests {
		userIDs = append(userIDs, gr.UserID)
	}
	userIDs = datautil.Distinct(userIDs)
	userMap, err := g.userClient.GetUsersInfoMap(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	groups, err := g.db.FindGroup(ctx, datautil.Distinct(groupIDs))
	if err != nil {
		return nil, err
	}
	groupMap := datautil.SliceToMap(groups, func(e *model.Group) string {
		return e.GroupID
	})
	if ids := datautil.Single(datautil.Keys(groupMap), groupIDs); len(ids) > 0 {
		return nil, servererrs.ErrGroupIDNotFound.WrapMsg(strings.Join(ids, ","))
	}
	groupMemberNumMap, err := g.db.MapGroupMemberNum(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	owners, err := g.db.FindGroupsOwner(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	if err := g.PopulateGroupMember(ctx, owners...); err != nil {
		return nil, err
	}
	ownerMap := datautil.SliceToMap(owners, func(e *model.GroupMember) string {
		return e.GroupID
	})
	resp.GroupRequests = datautil.Slice(groupRequests, func(e *model.GroupRequest) *sdkws.GroupRequest {
		var ownerUserID string
		if owner, ok := ownerMap[e.GroupID]; ok {
			ownerUserID = owner.UserID
		}
		return convert.Db2PbGroupRequest(e, userMap[e.UserID], convert.Db2PbGroupInfo(groupMap[e.GroupID], ownerUserID, groupMemberNumMap[e.GroupID]))
	})
	return resp, nil
}

func (g *groupServer) GetGroupsInfo(ctx context.Context, req *pbgroup.GetGroupsInfoReq) (*pbgroup.GetGroupsInfoResp, error) {
	if len(req.GroupIDs) == 0 {
		return nil, errs.ErrArgs.WrapMsg("groupID is empty")
	}
	groups, err := g.getGroupsInfo(ctx, req.GroupIDs)
	if err != nil {
		return nil, err
	}
	return &pbgroup.GetGroupsInfoResp{
		GroupInfos: groups,
	}, nil
}

func (g *groupServer) GetGroupApplicationUnhandledCount(ctx context.Context, req *pbgroup.GetGroupApplicationUnhandledCountReq) (*pbgroup.GetGroupApplicationUnhandledCountResp, error) {
	if err := authverify.CheckAccessV3(ctx, req.UserID, g.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}
	groupIDs, err := g.db.FindUserManagedGroupID(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	count, err := g.db.GetGroupApplicationUnhandledCount(ctx, groupIDs, req.Time)
	if err != nil {
		return nil, err
	}
	return &pbgroup.GetGroupApplicationUnhandledCountResp{
		Count: count,
	}, nil
}

func (g *groupServer) getGroupsInfo(ctx context.Context, groupIDs []string) ([]*sdkws.GroupInfo, error) {
	if len(groupIDs) == 0 {
		return nil, nil
	}
	groups, err := g.db.FindGroup(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	groupMemberNumMap, err := g.db.MapGroupMemberNum(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	owners, err := g.db.FindGroupsOwner(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	if err := g.PopulateGroupMember(ctx, owners...); err != nil {
		return nil, err
	}
	ownerMap := datautil.SliceToMap(owners, func(e *model.GroupMember) string {
		return e.GroupID
	})
	return datautil.Slice(groups, func(e *model.Group) *sdkws.GroupInfo {
		var ownerUserID string
		if owner, ok := ownerMap[e.GroupID]; ok {
			ownerUserID = owner.UserID
		}
		return convert.Db2PbGroupInfo(e, ownerUserID, groupMemberNumMap[e.GroupID])
	}), nil
}

func (g *groupServer) GroupApplicationResponse(ctx context.Context, req *pbgroup.GroupApplicationResponseReq) (*pbgroup.GroupApplicationResponseResp, error) {
	if !datautil.Contain(req.HandleResult, constant.GroupResponseAgree, constant.GroupResponseRefuse) {
		return nil, errs.ErrArgs.WrapMsg("HandleResult unknown")
	}
	if !authverify.IsAppManagerUid(ctx, g.config.Share.IMAdminUserID) {
		groupMember, err := g.db.TakeGroupMember(ctx, req.GroupID, mcontext.GetOpUserID(ctx))
		if err != nil {
			return nil, err
		}
		if !(groupMember.RoleLevel == constant.GroupOwner || groupMember.RoleLevel == constant.GroupAdmin) {
			return nil, errs.ErrNoPermission.WrapMsg("no group owner or admin")
		}
	}
	group, err := g.db.TakeGroup(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}
	groupRequest, err := g.db.TakeGroupRequest(ctx, req.GroupID, req.FromUserID)
	if err != nil {
		return nil, err
	}
	if groupRequest.HandleResult != 0 {
		return nil, servererrs.ErrGroupRequestHandled.WrapMsg("group request already processed")
	}
	var inGroup bool
	if _, err := g.db.TakeGroupMember(ctx, req.GroupID, req.FromUserID); err == nil {
		inGroup = true // Already in group
	} else if !g.IsNotFound(err) {
		return nil, err
	}
	if err := g.userClient.CheckUser(ctx, []string{req.FromUserID}); err != nil {
		return nil, err
	}
	var member *model.GroupMember
	if (!inGroup) && req.HandleResult == constant.GroupResponseAgree {
		member = &model.GroupMember{
			GroupID:        req.GroupID,
			UserID:         req.FromUserID,
			Nickname:       "",
			FaceURL:        "",
			RoleLevel:      constant.GroupOrdinaryUsers,
			JoinTime:       time.Now(),
			JoinSource:     groupRequest.JoinSource,
			MuteEndTime:    time.Unix(0, 0),
			InviterUserID:  groupRequest.InviterUserID,
			OperatorUserID: mcontext.GetOpUserID(ctx),
		}

		if err := g.webhookBeforeMembersJoinGroup(ctx, &g.config.WebhooksConfig.BeforeMemberJoinGroup, []*model.GroupMember{member}, group.GroupID, group.Ex); err != nil && err != servererrs.ErrCallbackContinue {
			return nil, err
		}
	}
	log.ZDebug(ctx, "GroupApplicationResponse", "inGroup", inGroup, "HandleResult", req.HandleResult, "member", member)
	if err := g.db.HandlerGroupRequest(ctx, req.GroupID, req.FromUserID, req.HandledMsg, req.HandleResult, member); err != nil {
		return nil, err
	}
	switch req.HandleResult {
	case constant.GroupResponseAgree:
		g.notification.GroupApplicationAcceptedNotification(ctx, req)
		if member == nil {
			log.ZDebug(ctx, "GroupApplicationResponse", "member is nil")
		} else {
			if groupRequest.InviterUserID == "" {
				if err = g.notification.MemberEnterNotification(ctx, req.GroupID, req.FromUserID); err != nil {
					return nil, err
				}
			} else {
				if err = g.notification.GroupApplicationAgreeMemberEnterNotification(ctx, req.GroupID, nil, groupRequest.InviterUserID, req.FromUserID); err != nil {
					return nil, err
				}
			}
		}
	case constant.GroupResponseRefuse:
		g.notification.GroupApplicationRejectedNotification(ctx, req)
	}

	return &pbgroup.GroupApplicationResponseResp{}, nil
}

func (g *groupServer) JoinGroup(ctx context.Context, req *pbgroup.JoinGroupReq) (*pbgroup.JoinGroupResp, error) {
	user, err := g.userClient.GetUserInfo(ctx, req.InviterUserID)
	if err != nil {
		return nil, err
	}
	group, err := g.db.TakeGroup(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}
	if group.Status == constant.GroupStatusDismissed {
		return nil, servererrs.ErrDismissedAlready.Wrap()
	}

	reqCall := &callbackstruct.CallbackJoinGroupReq{
		GroupID:    req.GroupID,
		GroupType:  string(group.GroupType),
		ApplyID:    req.InviterUserID,
		ReqMessage: req.ReqMessage,
		Ex:         req.Ex,
	}

	if err := g.webhookBeforeApplyJoinGroup(ctx, &g.config.WebhooksConfig.BeforeApplyJoinGroup, reqCall); err != nil && err != servererrs.ErrCallbackContinue {
		return nil, err
	}

	_, err = g.db.TakeGroupMember(ctx, req.GroupID, req.InviterUserID)
	if err == nil {
		return nil, errs.ErrArgs.Wrap()
	} else if !g.IsNotFound(err) && errs.Unwrap(err) != errs.ErrRecordNotFound {
		return nil, err
	}
	log.ZDebug(ctx, "JoinGroup.groupInfo", "group", group, "eq", group.NeedVerification == constant.Directly)
	if group.NeedVerification == constant.Directly {
		groupMember := &model.GroupMember{
			GroupID:        group.GroupID,
			UserID:         user.UserID,
			RoleLevel:      constant.GroupOrdinaryUsers,
			OperatorUserID: mcontext.GetOpUserID(ctx),
			InviterUserID:  req.InviterUserID,
			JoinTime:       time.Now(),
			MuteEndTime:    time.UnixMilli(0),
		}

		if err := g.webhookBeforeMembersJoinGroup(ctx, &g.config.WebhooksConfig.BeforeMemberJoinGroup, []*model.GroupMember{groupMember}, group.GroupID, group.Ex); err != nil && err != servererrs.ErrCallbackContinue {
			return nil, err
		}

		if err := g.db.CreateGroup(ctx, nil, []*model.GroupMember{groupMember}); err != nil {
			return nil, err
		}

		if err = g.notification.MemberEnterNotification(ctx, req.GroupID, req.InviterUserID); err != nil {
			return nil, err
		}
		g.webhookAfterJoinGroup(ctx, &g.config.WebhooksConfig.AfterJoinGroup, req)

		return &pbgroup.JoinGroupResp{}, nil
	}

	groupRequest := model.GroupRequest{
		UserID:      req.InviterUserID,
		ReqMsg:      req.ReqMessage,
		GroupID:     req.GroupID,
		JoinSource:  req.JoinSource,
		ReqTime:     time.Now(),
		HandledTime: time.Unix(0, 0),
		Ex:          req.Ex,
	}
	if err = g.db.CreateGroupRequest(ctx, []*model.GroupRequest{&groupRequest}); err != nil {
		return nil, err
	}
	g.notification.JoinGroupApplicationNotification(ctx, req, &groupRequest)
	return &pbgroup.JoinGroupResp{}, nil
}

func (g *groupServer) QuitGroup(ctx context.Context, req *pbgroup.QuitGroupReq) (*pbgroup.QuitGroupResp, error) {
	if req.UserID == "" {
		req.UserID = mcontext.GetOpUserID(ctx)
	} else {
		if err := authverify.CheckAccessV3(ctx, req.UserID, g.config.Share.IMAdminUserID); err != nil {
			return nil, err
		}
	}
	member, err := g.db.TakeGroupMember(ctx, req.GroupID, req.UserID)
	if err != nil {
		return nil, err
	}
	if member.RoleLevel == constant.GroupOwner {
		return nil, errs.ErrNoPermission.WrapMsg("group owner can't quit")
	}
	if err := g.PopulateGroupMember(ctx, member); err != nil {
		return nil, err
	}
	err = g.db.DeleteGroupMember(ctx, req.GroupID, []string{req.UserID})
	if err != nil {
		return nil, err
	}
	g.notification.MemberQuitNotification(ctx, g.groupMemberDB2PB(member, 0))
	if err := g.deleteMemberAndSetConversationSeq(ctx, req.GroupID, []string{req.UserID}); err != nil {
		return nil, err
	}
	g.webhookAfterQuitGroup(ctx, &g.config.WebhooksConfig.AfterQuitGroup, req)

	return &pbgroup.QuitGroupResp{}, nil
}

func (g *groupServer) deleteMemberAndSetConversationSeq(ctx context.Context, groupID string, userIDs []string) error {
	conversationID := msgprocessor.GetConversationIDBySessionType(constant.ReadGroupChatType, groupID)
	maxSeq, err := g.msgClient.GetConversationMaxSeq(ctx, conversationID)
	if err != nil {
		return err
	}
	return g.conversationClient.SetConversationMaxSeq(ctx, conversationID, userIDs, maxSeq)
}

func (g *groupServer) SetGroupInfo(ctx context.Context, req *pbgroup.SetGroupInfoReq) (*pbgroup.SetGroupInfoResp, error) {
	var opMember *model.GroupMember
	if !authverify.IsAppManagerUid(ctx, g.config.Share.IMAdminUserID) {
		var err error
		opMember, err = g.db.TakeGroupMember(ctx, req.GroupInfoForSet.GroupID, mcontext.GetOpUserID(ctx))
		if err != nil {
			return nil, err
		}
		if !(opMember.RoleLevel == constant.GroupOwner || opMember.RoleLevel == constant.GroupAdmin) {
			return nil, errs.ErrNoPermission.WrapMsg("no group owner or admin")
		}
		if err := g.PopulateGroupMember(ctx, opMember); err != nil {
			return nil, err
		}
	}

	if err := g.webhookBeforeSetGroupInfo(ctx, &g.config.WebhooksConfig.BeforeSetGroupInfo, req); err != nil && err != servererrs.ErrCallbackContinue {
		return nil, err
	}

	group, err := g.db.TakeGroup(ctx, req.GroupInfoForSet.GroupID)
	if err != nil {
		return nil, err
	}
	if group.Status == constant.GroupStatusDismissed {
		return nil, servererrs.ErrDismissedAlready.Wrap()
	}

	count, err := g.db.FindGroupMemberNum(ctx, group.GroupID)
	if err != nil {
		return nil, err
	}
	owner, err := g.db.TakeGroupOwner(ctx, group.GroupID)
	if err != nil {
		return nil, err
	}
	if err := g.PopulateGroupMember(ctx, owner); err != nil {
		return nil, err
	}
	update := UpdateGroupInfoMap(ctx, req.GroupInfoForSet)
	if len(update) == 0 {
		return &pbgroup.SetGroupInfoResp{}, nil
	}
	if err := g.db.UpdateGroup(ctx, group.GroupID, update); err != nil {
		return nil, err
	}
	group, err = g.db.TakeGroup(ctx, req.GroupInfoForSet.GroupID)
	if err != nil {
		return nil, err
	}
	tips := &sdkws.GroupInfoSetTips{
		Group:    g.groupDB2PB(group, owner.UserID, count),
		MuteTime: 0,
		OpUser:   &sdkws.GroupMemberFullInfo{},
	}
	if opMember != nil {
		tips.OpUser = g.groupMemberDB2PB(opMember, 0)
	}
	num := len(update)
	if req.GroupInfoForSet.Notification != "" {
		num -= 3
		func() {
			conversation := &pbconversation.ConversationReq{
				ConversationID:   msgprocessor.GetConversationIDBySessionType(constant.ReadGroupChatType, req.GroupInfoForSet.GroupID),
				ConversationType: constant.ReadGroupChatType,
				GroupID:          req.GroupInfoForSet.GroupID,
			}
			resp, err := g.GetGroupMemberUserIDs(ctx, &pbgroup.GetGroupMemberUserIDsReq{GroupID: req.GroupInfoForSet.GroupID})
			if err != nil {
				log.ZWarn(ctx, "GetGroupMemberIDs is failed.", err)
				return
			}
			conversation.GroupAtType = &wrapperspb.Int32Value{Value: constant.GroupNotification}
			if err := g.conversationClient.SetConversations(ctx, resp.UserIDs, conversation); err != nil {
				log.ZWarn(ctx, "SetConversations", err, "UserIDs", resp.UserIDs, "conversation", conversation)
			}
		}()
		notficationFlag := true
		g.notification.GroupInfoSetAnnouncementNotification(ctx, &sdkws.GroupInfoSetAnnouncementTips{Group: tips.Group, OpUser: tips.OpUser}, &notficationFlag)
	}
	if req.GroupInfoForSet.GroupName != "" {
		num--
		g.notification.GroupInfoSetNameNotification(ctx, &sdkws.GroupInfoSetNameTips{Group: tips.Group, OpUser: tips.OpUser})
	}
	if num > 0 {
		g.notification.GroupInfoSetNotification(ctx, tips)
	}

	g.webhookAfterSetGroupInfo(ctx, &g.config.WebhooksConfig.AfterSetGroupInfo, req)

	return &pbgroup.SetGroupInfoResp{}, nil
}

func (g *groupServer) SetGroupInfoEx(ctx context.Context, req *pbgroup.SetGroupInfoExReq) (*pbgroup.SetGroupInfoExResp, error) {
	var opMember *model.GroupMember

	if !authverify.IsAppManagerUid(ctx, g.config.Share.IMAdminUserID) {
		var err error

		opMember, err = g.db.TakeGroupMember(ctx, req.GroupID, mcontext.GetOpUserID(ctx))
		if err != nil {
			return nil, err
		}

		if !(opMember.RoleLevel == constant.GroupOwner || opMember.RoleLevel == constant.GroupAdmin) {
			return nil, errs.ErrNoPermission.WrapMsg("no group owner or admin")
		}

		if err := g.PopulateGroupMember(ctx, opMember); err != nil {
			return nil, err
		}
	}

	if err := g.webhookBeforeSetGroupInfoEx(ctx, &g.config.WebhooksConfig.BeforeSetGroupInfoEx, req); err != nil && err != servererrs.ErrCallbackContinue {
		return nil, err
	}

	group, err := g.db.TakeGroup(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}
	if group.Status == constant.GroupStatusDismissed {
		return nil, servererrs.ErrDismissedAlready.Wrap()
	}

	count, err := g.db.FindGroupMemberNum(ctx, group.GroupID)
	if err != nil {
		return nil, err
	}

	owner, err := g.db.TakeGroupOwner(ctx, group.GroupID)
	if err != nil {
		return nil, err
	}

	if err := g.PopulateGroupMember(ctx, owner); err != nil {
		return nil, err
	}

	updatedData, normalFlag, groupNameFlag, notificationFlag, err := UpdateGroupInfoExMap(ctx, req)
	if len(updatedData) == 0 {
		return &pbgroup.SetGroupInfoExResp{}, nil
	}

	if err != nil {
		return nil, err
	}

	if err := g.db.UpdateGroup(ctx, group.GroupID, updatedData); err != nil {
		return nil, err
	}

	group, err = g.db.TakeGroup(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}

	tips := &sdkws.GroupInfoSetTips{
		Group:    g.groupDB2PB(group, owner.UserID, count),
		MuteTime: 0,
		OpUser:   &sdkws.GroupMemberFullInfo{},
	}

	if opMember != nil {
		tips.OpUser = g.groupMemberDB2PB(opMember, 0)
	}

	if notificationFlag {
		if req.Notification.Value != "" {
			conversation := &pbconversation.ConversationReq{
				ConversationID:   msgprocessor.GetConversationIDBySessionType(constant.ReadGroupChatType, req.GroupID),
				ConversationType: constant.ReadGroupChatType,
				GroupID:          req.GroupID,
			}

			resp, err := g.GetGroupMemberUserIDs(ctx, &pbgroup.GetGroupMemberUserIDsReq{GroupID: req.GroupID})
			if err != nil {
				log.ZWarn(ctx, "GetGroupMemberIDs is failed.", err)
				return nil, err
			}

			conversation.GroupAtType = &wrapperspb.Int32Value{Value: constant.GroupNotification}
			if err := g.conversationClient.SetConversations(ctx, resp.UserIDs, conversation); err != nil {
				log.ZWarn(ctx, "SetConversations", err, "UserIDs", resp.UserIDs, "conversation", conversation)
			}

			g.notification.GroupInfoSetAnnouncementNotification(ctx, &sdkws.GroupInfoSetAnnouncementTips{Group: tips.Group, OpUser: tips.OpUser}, &notificationFlag)
		} else {
			notificationFlag = false
			g.notification.GroupInfoSetAnnouncementNotification(ctx, &sdkws.GroupInfoSetAnnouncementTips{Group: tips.Group, OpUser: tips.OpUser}, &notificationFlag)
		}
	}

	if groupNameFlag {
		g.notification.GroupInfoSetNameNotification(ctx, &sdkws.GroupInfoSetNameTips{Group: tips.Group, OpUser: tips.OpUser})
	}

	// if updatedData > 0, send the normal notification
	if normalFlag {
		g.notification.GroupInfoSetNotification(ctx, tips)
	}

	g.webhookAfterSetGroupInfoEx(ctx, &g.config.WebhooksConfig.AfterSetGroupInfoEx, req)

	return &pbgroup.SetGroupInfoExResp{}, nil
}

func (g *groupServer) TransferGroupOwner(ctx context.Context, req *pbgroup.TransferGroupOwnerReq) (*pbgroup.TransferGroupOwnerResp, error) {
	group, err := g.db.TakeGroup(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}

	if group.Status == constant.GroupStatusDismissed {
		return nil, servererrs.ErrDismissedAlready.Wrap()
	}

	if req.OldOwnerUserID == req.NewOwnerUserID {
		return nil, errs.ErrArgs.WrapMsg("OldOwnerUserID == NewOwnerUserID")
	}

	members, err := g.db.FindGroupMembers(ctx, req.GroupID, []string{req.OldOwnerUserID, req.NewOwnerUserID})
	if err != nil {
		return nil, err
	}

	if err := g.PopulateGroupMember(ctx, members...); err != nil {
		return nil, err
	}

	memberMap := datautil.SliceToMap(members, func(e *model.GroupMember) string { return e.UserID })
	if ids := datautil.Single([]string{req.OldOwnerUserID, req.NewOwnerUserID}, datautil.Keys(memberMap)); len(ids) > 0 {
		return nil, errs.ErrArgs.WrapMsg("user not in group " + strings.Join(ids, ","))
	}

	oldOwner := memberMap[req.OldOwnerUserID]
	if oldOwner == nil {
		return nil, errs.ErrArgs.WrapMsg("OldOwnerUserID not in group " + req.NewOwnerUserID)
	}

	newOwner := memberMap[req.NewOwnerUserID]
	if newOwner == nil {
		return nil, errs.ErrArgs.WrapMsg("NewOwnerUser not in group " + req.NewOwnerUserID)
	}

	if !authverify.IsAppManagerUid(ctx, g.config.Share.IMAdminUserID) {
		if !(mcontext.GetOpUserID(ctx) == oldOwner.UserID && oldOwner.RoleLevel == constant.GroupOwner) {
			return nil, errs.ErrNoPermission.WrapMsg("no permission transfer group owner")
		}
	}

	if newOwner.MuteEndTime.After(time.Now()) {
		if _, err := g.CancelMuteGroupMember(ctx, &pbgroup.CancelMuteGroupMemberReq{
			GroupID: group.GroupID,
			UserID:  req.NewOwnerUserID}); err != nil {
			return nil, err
		}
	}

	if err := g.db.TransferGroupOwner(ctx, req.GroupID, req.OldOwnerUserID, req.NewOwnerUserID, newOwner.RoleLevel); err != nil {
		return nil, err
	}

	g.webhookAfterTransferGroupOwner(ctx, &g.config.WebhooksConfig.AfterTransferGroupOwner, req)

	g.notification.GroupOwnerTransferredNotification(ctx, req)

	return &pbgroup.TransferGroupOwnerResp{}, nil
}

func (g *groupServer) GetGroups(ctx context.Context, req *pbgroup.GetGroupsReq) (*pbgroup.GetGroupsResp, error) {
	var (
		group []*model.Group
		err   error
	)
	var resp pbgroup.GetGroupsResp
	if req.GroupID != "" {
		group, err = g.db.FindGroup(ctx, []string{req.GroupID})
		resp.Total = uint32(len(group))
	} else {
		var total int64
		total, group, err = g.db.SearchGroup(ctx, req.GroupName, req.Pagination)
		resp.Total = uint32(total)
	}

	if err != nil {
		return nil, err
	}

	groupIDs := datautil.Slice(group, func(e *model.Group) string {
		return e.GroupID
	})

	ownerMembers, err := g.db.FindGroupsOwner(ctx, groupIDs)
	if err != nil {
		return nil, err
	}

	ownerMemberMap := datautil.SliceToMap(ownerMembers, func(e *model.GroupMember) string {
		return e.GroupID
	})
	groupMemberNumMap, err := g.db.MapGroupMemberNum(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	resp.Groups = datautil.Slice(group, func(group *model.Group) *pbgroup.CMSGroup {
		var (
			userID   string
			username string
		)
		if member, ok := ownerMemberMap[group.GroupID]; ok {
			userID = member.UserID
			username = member.Nickname
		}
		return convert.Db2PbCMSGroup(group, userID, username, groupMemberNumMap[group.GroupID])
	})
	return &resp, nil
}

func (g *groupServer) GetGroupMembersCMS(ctx context.Context, req *pbgroup.GetGroupMembersCMSReq) (*pbgroup.GetGroupMembersCMSResp, error) {
	if err := g.checkAdminOrInGroup(ctx, req.GroupID); err != nil {
		return nil, err
	}
	total, members, err := g.db.SearchGroupMember(ctx, req.UserName, req.GroupID, req.Pagination)
	if err != nil {
		return nil, err
	}
	var resp pbgroup.GetGroupMembersCMSResp
	resp.Total = uint32(total)
	if err := g.PopulateGroupMember(ctx, members...); err != nil {
		return nil, err
	}
	resp.Members = datautil.Slice(members, func(e *model.GroupMember) *sdkws.GroupMemberFullInfo {
		return convert.Db2PbGroupMember(e)
	})
	return &resp, nil
}

func (g *groupServer) GetUserReqApplicationList(ctx context.Context, req *pbgroup.GetUserReqApplicationListReq) (*pbgroup.GetUserReqApplicationListResp, error) {
	user, err := g.userClient.GetUserInfo(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	handleResults := datautil.Slice(req.HandleResults, func(e int32) int {
		return int(e)
	})
	total, requests, err := g.db.PageGroupRequestUser(ctx, req.UserID, req.GroupIDs, handleResults, req.Pagination)
	if err != nil {
		return nil, err
	}
	if len(requests) == 0 {
		return &pbgroup.GetUserReqApplicationListResp{Total: uint32(total)}, nil
	}
	groupIDs := datautil.Distinct(datautil.Slice(requests, func(e *model.GroupRequest) string {
		return e.GroupID
	}))
	groups, err := g.db.FindGroup(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	groupMap := datautil.SliceToMap(groups, func(e *model.Group) string {
		return e.GroupID
	})
	owners, err := g.db.FindGroupsOwner(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	if err := g.PopulateGroupMember(ctx, owners...); err != nil {
		return nil, err
	}
	ownerMap := datautil.SliceToMap(owners, func(e *model.GroupMember) string {
		return e.GroupID
	})
	groupMemberNum, err := g.db.MapGroupMemberNum(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	return &pbgroup.GetUserReqApplicationListResp{
		Total: uint32(total),
		GroupRequests: datautil.Slice(requests, func(e *model.GroupRequest) *sdkws.GroupRequest {
			var ownerUserID string
			if owner, ok := ownerMap[e.GroupID]; ok {
				ownerUserID = owner.UserID
			}
			return convert.Db2PbGroupRequest(e, user, convert.Db2PbGroupInfo(groupMap[e.GroupID], ownerUserID, groupMemberNum[e.GroupID]))
		}),
	}, nil
}

func (g *groupServer) DismissGroup(ctx context.Context, req *pbgroup.DismissGroupReq) (*pbgroup.DismissGroupResp, error) {
	owner, err := g.db.TakeGroupOwner(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}
	if !authverify.IsAppManagerUid(ctx, g.config.Share.IMAdminUserID) {
		if owner.UserID != mcontext.GetOpUserID(ctx) {
			return nil, errs.ErrNoPermission.WrapMsg("not group owner")
		}
	}
	if err := g.PopulateGroupMember(ctx, owner); err != nil {
		return nil, err
	}
	group, err := g.db.TakeGroup(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}
	if !req.DeleteMember && group.Status == constant.GroupStatusDismissed {
		return nil, servererrs.ErrDismissedAlready.WrapMsg("group status is dismissed")
	}
	if err := g.db.DismissGroup(ctx, req.GroupID, req.DeleteMember); err != nil {
		return nil, err
	}
	if !req.DeleteMember {
		num, err := g.db.FindGroupMemberNum(ctx, req.GroupID)
		if err != nil {
			return nil, err
		}
		tips := &sdkws.GroupDismissedTips{
			Group:  g.groupDB2PB(group, owner.UserID, num),
			OpUser: &sdkws.GroupMemberFullInfo{},
		}
		if mcontext.GetOpUserID(ctx) == owner.UserID {
			tips.OpUser = g.groupMemberDB2PB(owner, 0)
		}
		g.notification.GroupDismissedNotification(ctx, tips, req.SendMessage)
	}
	membersID, err := g.db.FindGroupMemberUserID(ctx, group.GroupID)
	if err != nil {
		return nil, err
	}
	cbReq := &callbackstruct.CallbackDisMissGroupReq{
		GroupID:   req.GroupID,
		OwnerID:   owner.UserID,
		MembersID: membersID,
		GroupType: string(group.GroupType),
	}

	g.webhookAfterDismissGroup(ctx, &g.config.WebhooksConfig.AfterDismissGroup, cbReq)

	return &pbgroup.DismissGroupResp{}, nil
}

func (g *groupServer) MuteGroupMember(ctx context.Context, req *pbgroup.MuteGroupMemberReq) (*pbgroup.MuteGroupMemberResp, error) {
	member, err := g.db.TakeGroupMember(ctx, req.GroupID, req.UserID)
	if err != nil {
		return nil, err
	}
	if err := g.PopulateGroupMember(ctx, member); err != nil {
		return nil, err
	}
	if !authverify.IsAppManagerUid(ctx, g.config.Share.IMAdminUserID) {
		opMember, err := g.db.TakeGroupMember(ctx, req.GroupID, mcontext.GetOpUserID(ctx))
		if err != nil {
			return nil, err
		}
		switch member.RoleLevel {
		case constant.GroupOwner:
			return nil, errs.ErrNoPermission.WrapMsg("set group owner mute")
		case constant.GroupAdmin:
			if opMember.RoleLevel != constant.GroupOwner {
				return nil, errs.ErrNoPermission.WrapMsg("set group admin mute")
			}
		case constant.GroupOrdinaryUsers:
			if !(opMember.RoleLevel == constant.GroupAdmin || opMember.RoleLevel == constant.GroupOwner) {
				return nil, errs.ErrNoPermission.WrapMsg("set group ordinary users mute")
			}
		}
	}
	data := UpdateGroupMemberMutedTimeMap(time.Now().Add(time.Second * time.Duration(req.MutedSeconds)))
	if err := g.db.UpdateGroupMember(ctx, member.GroupID, member.UserID, data); err != nil {
		return nil, err
	}
	g.notification.GroupMemberMutedNotification(ctx, req.GroupID, req.UserID, req.MutedSeconds)
	return &pbgroup.MuteGroupMemberResp{}, nil
}

func (g *groupServer) CancelMuteGroupMember(ctx context.Context, req *pbgroup.CancelMuteGroupMemberReq) (*pbgroup.CancelMuteGroupMemberResp, error) {
	member, err := g.db.TakeGroupMember(ctx, req.GroupID, req.UserID)
	if err != nil {
		return nil, err
	}

	if err := g.PopulateGroupMember(ctx, member); err != nil {
		return nil, err
	}

	if !authverify.IsAppManagerUid(ctx, g.config.Share.IMAdminUserID) {
		opMember, err := g.db.TakeGroupMember(ctx, req.GroupID, mcontext.GetOpUserID(ctx))
		if err != nil {
			return nil, err
		}

		switch member.RoleLevel {
		case constant.GroupOwner:
			return nil, errs.ErrNoPermission.WrapMsg("Can not set group owner unmute")
		case constant.GroupAdmin:
			if opMember.RoleLevel != constant.GroupOwner {
				return nil, errs.ErrNoPermission.WrapMsg("Can not set group admin unmute")
			}
		case constant.GroupOrdinaryUsers:
			if !(opMember.RoleLevel == constant.GroupAdmin || opMember.RoleLevel == constant.GroupOwner) {
				return nil, errs.ErrNoPermission.WrapMsg("Can not set group ordinary users unmute")
			}
		}
	}

	data := UpdateGroupMemberMutedTimeMap(time.Unix(0, 0))
	if err := g.db.UpdateGroupMember(ctx, member.GroupID, member.UserID, data); err != nil {
		return nil, err
	}

	g.notification.GroupMemberCancelMutedNotification(ctx, req.GroupID, req.UserID)

	return &pbgroup.CancelMuteGroupMemberResp{}, nil
}

func (g *groupServer) MuteGroup(ctx context.Context, req *pbgroup.MuteGroupReq) (*pbgroup.MuteGroupResp, error) {
	if err := g.CheckGroupAdmin(ctx, req.GroupID); err != nil {
		return nil, err
	}
	if err := g.db.UpdateGroup(ctx, req.GroupID, UpdateGroupStatusMap(constant.GroupStatusMuted)); err != nil {
		return nil, err
	}
	g.notification.GroupMutedNotification(ctx, req.GroupID)
	return &pbgroup.MuteGroupResp{}, nil
}

func (g *groupServer) CancelMuteGroup(ctx context.Context, req *pbgroup.CancelMuteGroupReq) (*pbgroup.CancelMuteGroupResp, error) {
	if err := g.CheckGroupAdmin(ctx, req.GroupID); err != nil {
		return nil, err
	}
	if err := g.db.UpdateGroup(ctx, req.GroupID, UpdateGroupStatusMap(constant.GroupOk)); err != nil {
		return nil, err
	}
	g.notification.GroupCancelMutedNotification(ctx, req.GroupID)
	return &pbgroup.CancelMuteGroupResp{}, nil
}

func (g *groupServer) SetGroupMemberInfo(ctx context.Context, req *pbgroup.SetGroupMemberInfoReq) (*pbgroup.SetGroupMemberInfoResp, error) {
	if len(req.Members) == 0 {
		return nil, errs.ErrArgs.WrapMsg("members empty")
	}
	opUserID := mcontext.GetOpUserID(ctx)
	if opUserID == "" {
		return nil, errs.ErrNoPermission.WrapMsg("no op user id")
	}
	isAppManagerUid := authverify.IsAppManagerUid(ctx, g.config.Share.IMAdminUserID)
	groupMembers := make(map[string][]*pbgroup.SetGroupMemberInfo)
	for i, member := range req.Members {
		if member.RoleLevel != nil {
			switch member.RoleLevel.Value {
			case constant.GroupOwner:
				return nil, errs.ErrNoPermission.WrapMsg("cannot set ungroup owner")
			case constant.GroupAdmin, constant.GroupOrdinaryUsers:
			default:
				return nil, errs.ErrArgs.WrapMsg("invalid role level")
			}
		}
		groupMembers[member.GroupID] = append(groupMembers[member.GroupID], req.Members[i])
	}
	for groupID, members := range groupMembers {
		temp := make(map[string]struct{})
		userIDs := make([]string, 0, len(members)+1)
		for _, member := range members {
			if _, ok := temp[member.UserID]; ok {
				return nil, errs.ErrArgs.WrapMsg(fmt.Sprintf("repeat group %s user %s", member.GroupID, member.UserID))
			}
			temp[member.UserID] = struct{}{}
			userIDs = append(userIDs, member.UserID)
		}
		if _, ok := temp[opUserID]; !ok {
			userIDs = append(userIDs, opUserID)
		}
		dbMembers, err := g.db.FindGroupMembers(ctx, groupID, userIDs)
		if err != nil {
			return nil, err
		}
		opUserIndex := -1
		for i, member := range dbMembers {
			if member.UserID == opUserID {
				opUserIndex = i
				break
			}
		}
		switch len(userIDs) - len(dbMembers) {
		case 0:
			if !isAppManagerUid {
				roleLevel := dbMembers[opUserIndex].RoleLevel
				var (
					dbSelf  = &model.GroupMember{}
					reqSelf *pbgroup.SetGroupMemberInfo
				)
				switch roleLevel {
				case constant.GroupOwner:
					for _, member := range dbMembers {
						if member.UserID == opUserID {
							dbSelf = member
							break
						}
					}
				case constant.GroupAdmin:
					for _, member := range dbMembers {
						if member.UserID == opUserID {
							dbSelf = member
						}
						if member.RoleLevel == constant.GroupOwner {
							return nil, errs.ErrNoPermission.WrapMsg("admin can not change group owner")
						}
						if member.RoleLevel == constant.GroupAdmin && member.UserID != opUserID {
							return nil, errs.ErrNoPermission.WrapMsg("admin can not change other group admin")
						}
					}
				case constant.GroupOrdinaryUsers:
					for _, member := range dbMembers {
						if member.UserID == opUserID {
							dbSelf = member
						}
						if !(member.RoleLevel == constant.GroupOrdinaryUsers && member.UserID == opUserID) {
							return nil, errs.ErrNoPermission.WrapMsg("ordinary users can not change other role level")
						}
					}
				default:
					for _, member := range dbMembers {
						if member.UserID == opUserID {
							dbSelf = member
						}
						if member.RoleLevel >= roleLevel {
							return nil, errs.ErrNoPermission.WrapMsg("can not change higher role level")
						}
					}
				}
				for _, member := range req.Members {
					if member.UserID == opUserID {
						reqSelf = member
						break
					}
				}
				if reqSelf != nil && reqSelf.RoleLevel != nil {
					if reqSelf.RoleLevel.GetValue() > dbSelf.RoleLevel {
						return nil, errs.ErrNoPermission.WrapMsg("can not improve role level by self")
					}
					if roleLevel == constant.GroupOwner {
						return nil, errs.ErrArgs.WrapMsg("group owner can not change own role level") // Prevent the absence of a group owner
					}
				}
			}
		case 1:
			if opUserIndex >= 0 {
				return nil, errs.ErrArgs.WrapMsg("user not in group")
			}
			if !isAppManagerUid {
				return nil, errs.ErrNoPermission.WrapMsg("user not in group")
			}
		default:
			return nil, errs.ErrArgs.WrapMsg("user not in group")
		}
	}

	for i := 0; i < len(req.Members); i++ {

		if err := g.webhookBeforeSetGroupMemberInfo(ctx, &g.config.WebhooksConfig.BeforeSetGroupMemberInfo, req.Members[i]); err != nil && err != servererrs.ErrCallbackContinue {
			return nil, err
		}

	}
	if err := g.db.UpdateGroupMembers(ctx, datautil.Slice(req.Members, func(e *pbgroup.SetGroupMemberInfo) *common.BatchUpdateGroupMember {
		return &common.BatchUpdateGroupMember{
			GroupID: e.GroupID,
			UserID:  e.UserID,
			Map:     UpdateGroupMemberMap(e),
		}
	})); err != nil {
		return nil, err
	}
	for _, member := range req.Members {
		if member.RoleLevel != nil {
			switch member.RoleLevel.Value {
			case constant.GroupAdmin:
				g.notification.GroupMemberSetToAdminNotification(ctx, member.GroupID, member.UserID)
			case constant.GroupOrdinaryUsers:
				g.notification.GroupMemberSetToOrdinaryUserNotification(ctx, member.GroupID, member.UserID)
			}
		}
		if member.Nickname != nil || member.FaceURL != nil || member.Ex != nil {
			g.notification.GroupMemberInfoSetNotification(ctx, member.GroupID, member.UserID)
		}
	}
	for i := 0; i < len(req.Members); i++ {
		g.webhookAfterSetGroupMemberInfo(ctx, &g.config.WebhooksConfig.AfterSetGroupMemberInfo, req.Members[i])
	}

	return &pbgroup.SetGroupMemberInfoResp{}, nil
}

func (g *groupServer) GetGroupAbstractInfo(ctx context.Context, req *pbgroup.GetGroupAbstractInfoReq) (*pbgroup.GetGroupAbstractInfoResp, error) {
	if len(req.GroupIDs) == 0 {
		return nil, errs.ErrArgs.WrapMsg("groupIDs empty")
	}
	if datautil.Duplicate(req.GroupIDs) {
		return nil, errs.ErrArgs.WrapMsg("groupIDs duplicate")
	}
	for _, groupID := range req.GroupIDs {
		if err := g.checkAdminOrInGroup(ctx, groupID); err != nil {
			return nil, err
		}
	}
	groups, err := g.db.FindGroup(ctx, req.GroupIDs)
	if err != nil {
		return nil, err
	}
	if ids := datautil.Single(req.GroupIDs, datautil.Slice(groups, func(group *model.Group) string {
		return group.GroupID
	})); len(ids) > 0 {
		return nil, servererrs.ErrGroupIDNotFound.WrapMsg("not found group " + strings.Join(ids, ","))
	}
	groupUserMap, err := g.db.MapGroupMemberUserID(ctx, req.GroupIDs)
	if err != nil {
		return nil, err
	}
	if ids := datautil.Single(req.GroupIDs, datautil.Keys(groupUserMap)); len(ids) > 0 {
		return nil, servererrs.ErrGroupIDNotFound.WrapMsg(fmt.Sprintf("group %s not found member", strings.Join(ids, ",")))
	}
	return &pbgroup.GetGroupAbstractInfoResp{
		GroupAbstractInfos: datautil.Slice(groups, func(group *model.Group) *pbgroup.GroupAbstractInfo {
			users := groupUserMap[group.GroupID]
			return convert.Db2PbGroupAbstractInfo(group.GroupID, users.MemberNum, users.Hash)
		}),
	}, nil
}

func (g *groupServer) GetUserInGroupMembers(ctx context.Context, req *pbgroup.GetUserInGroupMembersReq) (*pbgroup.GetUserInGroupMembersResp, error) {
	if len(req.GroupIDs) == 0 {
		return nil, errs.ErrArgs.WrapMsg("groupIDs empty")
	}
	if err := authverify.CheckAccessV3(ctx, req.UserID, g.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}
	members, err := g.db.FindGroupMemberUser(ctx, req.GroupIDs, req.UserID)
	if err != nil {
		return nil, err
	}
	if err := g.PopulateGroupMember(ctx, members...); err != nil {
		return nil, err
	}
	return &pbgroup.GetUserInGroupMembersResp{
		Members: datautil.Slice(members, func(e *model.GroupMember) *sdkws.GroupMemberFullInfo {
			return convert.Db2PbGroupMember(e)
		}),
	}, nil
}

func (g *groupServer) GetGroupMemberUserIDs(ctx context.Context, req *pbgroup.GetGroupMemberUserIDsReq) (*pbgroup.GetGroupMemberUserIDsResp, error) {
	userIDs, err := g.db.FindGroupMemberUserID(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}
	if !authverify.IsAppManagerUid(ctx, g.config.Share.IMAdminUserID) {
		if !datautil.Contain(mcontext.GetOpUserID(ctx), userIDs...) {
			return nil, errs.ErrNoPermission.WrapMsg("opUser no permission")
		}
	}
	return &pbgroup.GetGroupMemberUserIDsResp{
		UserIDs: userIDs,
	}, nil
}

func (g *groupServer) GetGroupMemberRoleLevel(ctx context.Context, req *pbgroup.GetGroupMemberRoleLevelReq) (*pbgroup.GetGroupMemberRoleLevelResp, error) {
	if len(req.RoleLevels) == 0 {
		return nil, errs.ErrArgs.WrapMsg("RoleLevels empty")
	}
	if err := g.checkAdminOrInGroup(ctx, req.GroupID); err != nil {
		return nil, err
	}
	members, err := g.db.FindGroupMemberRoleLevels(ctx, req.GroupID, req.RoleLevels)
	if err != nil {
		return nil, err
	}
	if err := g.PopulateGroupMember(ctx, members...); err != nil {
		return nil, err
	}
	return &pbgroup.GetGroupMemberRoleLevelResp{
		Members: datautil.Slice(members, func(e *model.GroupMember) *sdkws.GroupMemberFullInfo {
			return convert.Db2PbGroupMember(e)
		}),
	}, nil
}

func (g *groupServer) GetGroupUsersReqApplicationList(ctx context.Context, req *pbgroup.GetGroupUsersReqApplicationListReq) (*pbgroup.GetGroupUsersReqApplicationListResp, error) {
	requests, err := g.db.FindGroupRequests(ctx, req.GroupID, req.UserIDs)
	if err != nil {
		return nil, err
	}

	if len(requests) == 0 {
		return &pbgroup.GetGroupUsersReqApplicationListResp{}, nil
	}

	groupIDs := datautil.Distinct(datautil.Slice(requests, func(e *model.GroupRequest) string {
		return e.GroupID
	}))

	groups, err := g.db.FindGroup(ctx, groupIDs)
	if err != nil {
		return nil, err
	}

	groupMap := datautil.SliceToMap(groups, func(e *model.Group) string {
		return e.GroupID
	})

	if ids := datautil.Single(groupIDs, datautil.Keys(groupMap)); len(ids) > 0 {
		return nil, servererrs.ErrGroupIDNotFound.WrapMsg(strings.Join(ids, ","))
	}

	userMap, err := g.userClient.GetUsersInfoMap(ctx, req.UserIDs)
	if err != nil {
		return nil, err
	}

	owners, err := g.db.FindGroupsOwner(ctx, groupIDs)
	if err != nil {
		return nil, err
	}

	if err := g.PopulateGroupMember(ctx, owners...); err != nil {
		return nil, err
	}

	ownerMap := datautil.SliceToMap(owners, func(e *model.GroupMember) string {
		return e.GroupID
	})

	groupMemberNum, err := g.db.MapGroupMemberNum(ctx, groupIDs)
	if err != nil {
		return nil, err
	}

	return &pbgroup.GetGroupUsersReqApplicationListResp{
		Total: int64(len(requests)),
		GroupRequests: datautil.Slice(requests, func(e *model.GroupRequest) *sdkws.GroupRequest {
			var ownerUserID string
			if owner, ok := ownerMap[e.GroupID]; ok {
				ownerUserID = owner.UserID
			}

			var userInfo *sdkws.UserInfo
			if user, ok := userMap[e.UserID]; !ok {
				userInfo = user
			}

			return convert.Db2PbGroupRequest(e, userInfo, convert.Db2PbGroupInfo(groupMap[e.GroupID], ownerUserID, groupMemberNum[e.GroupID]))
		}),
	}, nil
}

func (g *groupServer) GetSpecifiedUserGroupRequestInfo(ctx context.Context, req *pbgroup.GetSpecifiedUserGroupRequestInfoReq) (*pbgroup.GetSpecifiedUserGroupRequestInfoResp, error) {
	opUserID := mcontext.GetOpUserID(ctx)

	owners, err := g.db.FindGroupsOwner(ctx, []string{req.GroupID})
	if err != nil {
		return nil, err
	}

	if req.UserID != opUserID {
		adminIDs, err := g.db.GetGroupRoleLevelMemberIDs(ctx, req.GroupID, constant.GroupAdmin)
		if err != nil {
			return nil, err
		}

		adminIDs = append(adminIDs, owners[0].UserID)
		adminIDs = append(adminIDs, g.config.Share.IMAdminUserID...)

		if !datautil.Contain(opUserID, adminIDs...) {
			return nil, errs.ErrNoPermission.WrapMsg("opUser no permission")
		}
	}

	requests, err := g.db.FindGroupRequests(ctx, req.GroupID, []string{req.UserID})
	if err != nil {
		return nil, err
	}

	if len(requests) == 0 {
		return &pbgroup.GetSpecifiedUserGroupRequestInfoResp{}, nil
	}

	groups, err := g.db.FindGroup(ctx, []string{req.GroupID})
	if err != nil {
		return nil, err
	}

	userInfos, err := g.userClient.GetUsersInfo(ctx, []string{req.UserID})
	if err != nil {
		return nil, err
	}

	groupMemberNum, err := g.db.MapGroupMemberNum(ctx, []string{req.GroupID})
	if err != nil {
		return nil, err
	}

	resp := &pbgroup.GetSpecifiedUserGroupRequestInfoResp{
		GroupRequests: make([]*sdkws.GroupRequest, 0, len(requests)),
	}

	for _, request := range requests {
		resp.GroupRequests = append(resp.GroupRequests, convert.Db2PbGroupRequest(request, userInfos[0], convert.Db2PbGroupInfo(groups[0], owners[0].UserID, groupMemberNum[groups[0].GroupID])))
	}

	resp.Total = uint32(len(requests))

	return resp, nil
}
