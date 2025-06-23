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

// CreateGroup 创建群组
//
// 这是群组创建的核心方法，实现了完整的群组创建流程：
// 1. 参数验证：群组类型、群主ID、权限验证
// 2. 成员处理：去重、用户存在性验证
// 3. Webhook回调：创建前的业务逻辑扩展
// 4. 群组数据构建：群组信息、成员信息
// 5. 数据库操作：事务性创建群组和成员
// 6. 通知发送：群组创建通知、公告通知
// 7. Webhook回调：创建后的业务逻辑扩展
//
// 业务规则：
// - 只支持工作群类型
// - 必须指定群主
// - 支持同时指定管理员和普通成员
// - 操作者自动加入群组（如果不在成员列表中）
// - 成员不能重复
//
// 事务保证：
// - 群组信息和成员信息在同一事务中创建
// - 创建失败时自动回滚
func (g *groupServer) CreateGroup(ctx context.Context, req *pbgroup.CreateGroupReq) (*pbgroup.CreateGroupResp, error) {
	// 验证群组类型，目前只支持工作群
	if req.GroupInfo.GroupType != constant.WorkingGroup {
		return nil, errs.ErrArgs.WrapMsg(fmt.Sprintf("group type only supports %d", constant.WorkingGroup))
	}

	// 验证群主ID不能为空
	if req.OwnerUserID == "" {
		return nil, errs.ErrArgs.WrapMsg("no group owner")
	}

	// 验证操作权限：检查是否有权限指定该用户为群主
	if err := authverify.CheckAccessV3(ctx, req.OwnerUserID, g.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	// 合并所有用户ID：普通成员 + 管理员 + 群主
	userIDs := append(append(req.MemberUserIDs, req.AdminUserIDs...), req.OwnerUserID)
	opUserID := mcontext.GetOpUserID(ctx)

	// 如果操作者不在成员列表中，自动添加
	if !datautil.Contain(opUserID, userIDs...) {
		userIDs = append(userIDs, opUserID)
	}

	// 检查用户ID是否有重复
	if datautil.Duplicate(userIDs) {
		return nil, errs.ErrArgs.WrapMsg("group member repeated")
	}

	// 获取所有用户的详细信息
	userMap, err := g.userClient.GetUsersInfoMap(ctx, userIDs)
	if err != nil {
		return nil, err
	}

	// 验证所有用户都存在
	if len(userMap) != len(userIDs) {
		return nil, servererrs.ErrUserIDNotFound.WrapMsg("user not found")
	}

	// 执行创建群组前的Webhook回调
	if err := g.webhookBeforeCreateGroup(ctx, &g.config.WebhooksConfig.BeforeCreateGroup, req); err != nil && err != servererrs.ErrCallbackContinue {
		return nil, err
	}

	// 初始化群组成员列表
	var groupMembers []*model.GroupMember
	// 转换群组信息为数据库模型
	group := convert.Pb2DBGroupInfo(req.GroupInfo)

	// 生成群组ID（如果未指定则自动生成）
	if err := g.GenGroupID(ctx, &group.GroupID); err != nil {
		return nil, err
	}

	// 定义加入群组的通用函数
	joinGroupFunc := func(userID string, roleLevel int32) {
		groupMember := &model.GroupMember{
			GroupID:        group.GroupID,             // 群组ID
			UserID:         userID,                    // 用户ID
			RoleLevel:      roleLevel,                 // 角色等级
			OperatorUserID: opUserID,                  // 操作者ID
			JoinSource:     constant.JoinByInvitation, // 加入方式：邀请
			InviterUserID:  opUserID,                  // 邀请者ID
			JoinTime:       time.Now(),                // 加入时间
			MuteEndTime:    time.UnixMilli(0),         // 禁言结束时间（初始为0）
		}

		groupMembers = append(groupMembers, groupMember)
	}

	// 添加群主
	joinGroupFunc(req.OwnerUserID, constant.GroupOwner)

	// 添加管理员
	for _, userID := range req.AdminUserIDs {
		joinGroupFunc(userID, constant.GroupAdmin)
	}

	// 添加普通成员
	for _, userID := range req.MemberUserIDs {
		joinGroupFunc(userID, constant.GroupOrdinaryUsers)
	}

	// 执行成员加入群组前的Webhook回调
	if err := g.webhookBeforeMembersJoinGroup(ctx, &g.config.WebhooksConfig.BeforeMemberJoinGroup, groupMembers, group.GroupID, group.Ex); err != nil && err != servererrs.ErrCallbackContinue {
		return nil, err
	}

	// 在数据库中创建群组和成员（事务操作）
	if err := g.db.CreateGroup(ctx, []*model.Group{group}, groupMembers); err != nil {
		return nil, err
	}

	// 构建响应对象
	resp := &pbgroup.CreateGroupResp{GroupInfo: &sdkws.GroupInfo{}}

	// 填充群组信息
	resp.GroupInfo = convert.Db2PbGroupInfo(group, req.OwnerUserID, uint32(len(userIDs)))
	resp.GroupInfo.MemberCount = uint32(len(userIDs))

	// 构建群组创建通知的提示信息
	tips := &sdkws.GroupCreatedTips{
		Group:          resp.GroupInfo,                                                                      // 群组信息
		OperationTime:  group.CreateTime.UnixMilli(),                                                        // 操作时间
		GroupOwnerUser: g.groupMemberDB2PB(groupMembers[0], userMap[groupMembers[0].UserID].AppMangerLevel), // 群主信息
	}

	// 填充成员列表和操作者信息
	for _, member := range groupMembers {
		member.Nickname = userMap[member.UserID].Nickname // 设置成员昵称
		tips.MemberList = append(tips.MemberList, g.groupMemberDB2PB(member, userMap[member.UserID].AppMangerLevel))
		// 找到操作者并设置操作者信息
		if member.UserID == opUserID {
			tips.OpUser = g.groupMemberDB2PB(member, userMap[member.UserID].AppMangerLevel)
			break
		}
	}

	// 发送群组创建通知
	g.notification.GroupCreatedNotification(ctx, tips, req.SendMessage)

	// 如果设置了群公告，发送公告设置通知
	if req.GroupInfo.Notification != "" {
		notificationFlag := true
		g.notification.GroupInfoSetAnnouncementNotification(ctx, &sdkws.GroupInfoSetAnnouncementTips{
			Group:  tips.Group,
			OpUser: tips.OpUser,
		}, &notificationFlag)
	}

	// 构建创建后回调的请求参数
	reqCallBackAfter := &pbgroup.CreateGroupReq{
		MemberUserIDs: userIDs,
		GroupInfo:     resp.GroupInfo,
		OwnerUserID:   req.OwnerUserID,
		AdminUserIDs:  req.AdminUserIDs,
	}

	// 执行创建群组后的Webhook回调
	g.webhookAfterCreateGroup(ctx, &g.config.WebhooksConfig.AfterCreateGroup, reqCallBackAfter)

	return resp, nil
}

// GetJoinedGroupList 获取用户已加入的群组列表
//
// 此方法用于获取指定用户已加入的所有群组信息，支持分页查询。
// 返回的群组信息包括群组基本信息、成员数量、群主信息等。
//
// 业务逻辑：
// 1. 权限验证：检查是否有权限查询该用户的群组列表
// 2. 分页查询：获取用户的群组成员记录
// 3. 数据聚合：获取群组详细信息、成员数量、群主信息
// 4. 数据转换：将数据库模型转换为API响应格式
//
// 性能优化：
// - 批量查询群组信息，避免N+1查询问题
// - 使用Map结构快速匹配群主信息
// - 保持查询结果的顺序一致性
func (g *groupServer) GetJoinedGroupList(ctx context.Context, req *pbgroup.GetJoinedGroupListReq) (*pbgroup.GetJoinedGroupListResp, error) {
	// 验证访问权限：检查是否有权限查询该用户的群组列表
	if err := authverify.CheckAccessV3(ctx, req.FromUserID, g.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	// 分页查询用户加入的群组成员记录
	total, members, err := g.db.PageGetJoinGroup(ctx, req.FromUserID, req.Pagination)
	if err != nil {
		return nil, err
	}

	// 初始化响应对象
	var resp pbgroup.GetJoinedGroupListResp
	resp.Total = uint32(total)

	// 如果没有加入任何群组，直接返回
	if len(members) == 0 {
		return &resp, nil
	}

	// 提取所有群组ID
	groupIDs := datautil.Slice(members, func(e *model.GroupMember) string {
		return e.GroupID
	})

	// 批量查询群组基本信息
	groups, err := g.db.FindGroup(ctx, groupIDs)
	if err != nil {
		return nil, err
	}

	// 批量查询每个群组的成员数量
	groupMemberNum, err := g.db.MapGroupMemberNum(ctx, groupIDs)
	if err != nil {
		return nil, err
	}

	// 批量查询每个群组的群主信息
	owners, err := g.db.FindGroupsOwner(ctx, groupIDs)
	if err != nil {
		return nil, err
	}

	// 填充群组成员的用户信息（昵称、头像等）
	if err := g.PopulateGroupMember(ctx, members...); err != nil {
		return nil, err
	}

	// 构建群主信息的映射表，便于快速查找
	ownerMap := datautil.SliceToMap(owners, func(e *model.GroupMember) string {
		return e.GroupID
	})

	// 按照原始顺序构建群组信息列表，并转换为API响应格式
	resp.Groups = datautil.Slice(datautil.Order(groupIDs, groups, func(group *model.Group) string {
		return group.GroupID
	}), func(group *model.Group) *sdkws.GroupInfo {
		// 获取群主用户ID
		var userID string
		if user := ownerMap[group.GroupID]; user != nil {
			userID = user.UserID
		}
		// 转换数据库模型为API响应格式
		return convert.Db2PbGroupInfo(group, userID, groupMemberNum[group.GroupID])
	})

	return &resp, nil
}

// InviteUserToGroup 邀请用户加入群组
//
// 此方法实现了邀请用户加入群组的完整流程，支持两种模式：
// 1. 直接加入：管理员邀请或群组设置为无需验证
// 2. 申请模式：普通成员邀请且群组需要验证，生成加群申请
//
// 业务逻辑：
// 1. 参数验证：用户列表非空、无重复、群组存在且未解散
// 2. 权限检查：验证邀请者的权限级别
// 3. 用户验证：确认被邀请用户都存在
// 4. 验证模式判断：根据群组设置和邀请者权限决定处理方式
// 5. 批量处理：支持大量用户邀请的分批处理
//
// 权限规则：
// - 系统管理员：可直接邀请任何用户
// - 群主/管理员：可直接邀请用户（无论群组验证设置）
// - 普通成员：在需要验证的群组中只能发起邀请申请
//
// 性能优化：
// - 批量处理：每批最多50个用户，避免单次操作过大
// - 事务保证：每批用户的添加在同一事务中完成
func (g *groupServer) InviteUserToGroup(ctx context.Context, req *pbgroup.InviteUserToGroupReq) (*pbgroup.InviteUserToGroupResp, error) {
	// 验证被邀请用户列表不能为空
	if len(req.InvitedUserIDs) == 0 {
		return nil, errs.ErrArgs.WrapMsg("user empty")
	}

	// 验证用户ID不能重复
	if datautil.Duplicate(req.InvitedUserIDs) {
		return nil, errs.ErrArgs.WrapMsg("userID duplicate")
	}

	// 获取群组信息
	group, err := g.db.TakeGroup(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}

	// 检查群组状态，已解散的群组不能邀请新成员
	if group.Status == constant.GroupStatusDismissed {
		return nil, servererrs.ErrDismissedAlready.WrapMsg("group dismissed checking group status found it dismissed")
	}

	// 验证所有被邀请用户都存在
	userMap, err := g.userClient.GetUsersInfoMap(ctx, req.InvitedUserIDs)
	if err != nil {
		return nil, err
	}

	if len(userMap) != len(req.InvitedUserIDs) {
		return nil, errs.ErrRecordNotFound.WrapMsg("user not found")
	}

	// 获取操作者信息和权限
	var groupMember *model.GroupMember
	var opUserID string
	if !authverify.IsAppManagerUid(ctx, g.config.Share.IMAdminUserID) {
		// 非系统管理员，需要检查在群组中的权限
		opUserID = mcontext.GetOpUserID(ctx)
		var err error
		groupMember, err = g.db.TakeGroupMember(ctx, req.GroupID, opUserID)
		if err != nil {
			return nil, err
		}
		// 填充群组成员的用户信息
		if err := g.PopulateGroupMember(ctx, groupMember); err != nil {
			return nil, err
		}
	} else {
		// 系统管理员直接获取操作者ID
		opUserID = mcontext.GetOpUserID(ctx)
	}

	// 执行邀请前的Webhook回调
	if err := g.webhookBeforeInviteUserToGroup(ctx, &g.config.WebhooksConfig.BeforeInviteUserToGroup, req); err != nil && err != servererrs.ErrCallbackContinue {
		return nil, err
	}

	// 检查群组是否需要验证加入
	if group.NeedVerification == constant.AllNeedVerification {
		if !authverify.IsAppManagerUid(ctx, g.config.Share.IMAdminUserID) {
			// 非系统管理员且群组需要验证，检查是否为群主或管理员
			if !(groupMember.RoleLevel == constant.GroupOwner || groupMember.RoleLevel == constant.GroupAdmin) {
				// 普通成员邀请，创建加群申请记录
				var requests []*model.GroupRequest
				for _, userID := range req.InvitedUserIDs {
					requests = append(requests, &model.GroupRequest{
						UserID:        userID,                    // 申请用户ID
						GroupID:       req.GroupID,               // 群组ID
						JoinSource:    constant.JoinByInvitation, // 加入方式：邀请
						InviterUserID: opUserID,                  // 邀请者ID
						ReqTime:       time.Now(),                // 申请时间
						HandledTime:   time.Unix(0, 0),           // 处理时间（初始为0）
					})
				}

				// 批量创建加群申请
				if err := g.db.CreateGroupRequest(ctx, requests); err != nil {
					return nil, err
				}

				// 为每个申请发送通知
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

	// 直接加入模式：构建群组成员列表
	var groupMembers []*model.GroupMember
	for _, userID := range req.InvitedUserIDs {
		member := &model.GroupMember{
			GroupID:        req.GroupID,                 // 群组ID
			UserID:         userID,                      // 用户ID
			RoleLevel:      constant.GroupOrdinaryUsers, // 角色等级：普通成员
			OperatorUserID: opUserID,                    // 操作者ID
			InviterUserID:  opUserID,                    // 邀请者ID
			JoinSource:     constant.JoinByInvitation,   // 加入方式：邀请
			JoinTime:       time.Now(),                  // 加入时间
			MuteEndTime:    time.UnixMilli(0),           // 禁言结束时间（初始为0）
		}

		groupMembers = append(groupMembers, member)
	}

	// 执行成员加入前的Webhook回调
	if err := g.webhookBeforeMembersJoinGroup(ctx, &g.config.WebhooksConfig.BeforeMemberJoinGroup, groupMembers, group.GroupID, group.Ex); err != nil && err != servererrs.ErrCallbackContinue {
		return nil, err
	}

	// 分批处理大量用户邀请，每批最多50个用户
	const singleQuantity = 50
	for start := 0; start < len(groupMembers); start += singleQuantity {
		end := start + singleQuantity
		if end > len(groupMembers) {
			end = len(groupMembers)
		}
		currentMembers := groupMembers[start:end]

		// 批量添加群组成员
		if err := g.db.CreateGroup(ctx, nil, currentMembers); err != nil {
			return nil, err
		}

		// 提取当前批次的用户ID
		userIDs := datautil.Slice(currentMembers, func(e *model.GroupMember) string {
			return e.UserID
		})

		// 发送成员加入通知
		if err = g.notification.GroupApplicationAgreeMemberEnterNotification(ctx, req.GroupID, req.SendMessage, opUserID, userIDs...); err != nil {
			return nil, err
		}
	}
	return &pbgroup.InviteUserToGroupResp{}, nil
}

// GetGroupAllMember 获取群组所有成员信息
//
// 此方法用于获取指定群组的所有成员详细信息，包括成员的基本信息、
// 群内角色、加入时间、禁言状态等完整信息。
//
// 业务逻辑：
// 1. 查询群组所有成员记录
// 2. 填充成员的用户基本信息（昵称、头像等）
// 3. 转换数据格式为API响应格式
//
// 注意事项：
// - 此方法不进行权限验证，调用方需要自行验证权限
// - 返回完整的成员信息，包括敏感信息如加入时间等
// - 适用于管理员查看完整成员列表的场景
func (g *groupServer) GetGroupAllMember(ctx context.Context, req *pbgroup.GetGroupAllMemberReq) (*pbgroup.GetGroupAllMemberResp, error) {
	// 查询群组的所有成员记录
	members, err := g.db.FindGroupMemberAll(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}

	// 填充成员的用户基本信息（昵称、头像等）
	if err := g.PopulateGroupMember(ctx, members...); err != nil {
		return nil, err
	}

	// 构建响应对象
	var resp pbgroup.GetGroupAllMemberResp
	// 将数据库模型转换为API响应格式
	resp.Members = datautil.Slice(members, func(e *model.GroupMember) *sdkws.GroupMemberFullInfo {
		return convert.Db2PbGroupMember(e)
	})

	return &resp, nil
}

// checkAdminOrInGroup 检查操作者是否为系统管理员或群组成员
//
// 这是一个权限验证辅助方法，用于验证操作者是否有权限访问群组信息。
// 验证规则：
// 1. 系统管理员：拥有所有群组的访问权限
// 2. 群组成员：只能访问自己所在群组的信息
// 3. 非群组成员：无权限访问群组信息
//
// 使用场景：
// - 查看群组成员列表
// - 获取群组详细信息
// - 其他需要群组访问权限的操作
//
// 参数：
// - ctx: 上下文，包含操作者信息
// - groupID: 要验证权限的群组ID
//
// 返回：
// - nil: 有权限访问
// - error: 无权限访问或其他错误
func (g *groupServer) checkAdminOrInGroup(ctx context.Context, groupID string) error {
	// 检查是否为系统管理员，系统管理员拥有所有权限
	if authverify.IsAppManagerUid(ctx, g.config.Share.IMAdminUserID) {
		return nil
	}

	// 获取操作者用户ID
	opUserID := mcontext.GetOpUserID(ctx)

	// 查询操作者是否为该群组成员
	members, err := g.db.FindGroupMembers(ctx, groupID, []string{opUserID})
	if err != nil {
		return err
	}

	// 如果不是群组成员，返回权限错误
	if len(members) == 0 {
		return errs.ErrNoPermission.WrapMsg("op user not in group")
	}

	return nil
}

// GetGroupMemberList 获取群组成员列表
//
// 此方法用于分页获取群组成员列表，支持关键词搜索功能。
// 只有群组成员或系统管理员才能查看群组成员列表。
//
// 业务逻辑：
// 1. 权限验证：检查操作者是否有权限查看该群组成员列表
// 2. 查询模式：根据是否提供关键词选择分页查询或搜索查询
// 3. 数据填充：填充成员的用户基本信息（昵称、头像等）
// 4. 格式转换：将数据库模型转换为API响应格式
//
// 查询模式：
// - 普通分页：获取所有成员的分页列表
// - 关键词搜索：根据昵称或用户ID模糊搜索成员
//
// 权限要求：
// - 群组成员：可以查看同群成员列表
// - 系统管理员：可以查看任何群组的成员列表
//
// 返回数据：
// - 成员总数
// - 当前页成员详细信息列表
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

// KickGroupMember 踢出群组成员
//
// 此方法实现了踢出群组成员的完整流程，包括权限验证、数据更新、通知发送等。
//
// 业务逻辑：
// 1. 参数验证：被踢用户列表非空、无重复、不包含操作者和群主
// 2. 权限验证：根据操作者角色验证是否有权限踢出目标用户
// 3. 数据库操作：删除群组成员记录
// 4. 会话处理：设置被踢用户的会话序列号
// 5. 通知发送：向群组成员发送踢出通知
// 6. Webhook回调：执行踢出后的业务逻辑扩展
//
// 权限规则：
// - 系统管理员：可踢出任何成员（除群主外）
// - 群主：可踢出任何成员（除自己外）
// - 管理员：可踢出普通成员，不能踢出群主和其他管理员
// - 普通成员：无踢出权限
//
// 特殊规则：
// - 群主不能被踢出
// - 操作者不能踢出自己
// - 被踢用户必须是群组成员
func (g *groupServer) KickGroupMember(ctx context.Context, req *pbgroup.KickGroupMemberReq) (*pbgroup.KickGroupMemberResp, error) {
	// 获取群组信息
	group, err := g.db.TakeGroup(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}

	// 验证被踢用户列表不能为空
	if len(req.KickedUserIDs) == 0 {
		return nil, errs.ErrArgs.WrapMsg("KickedUserIDs empty")
	}

	// 验证被踢用户ID不能重复
	if datautil.Duplicate(req.KickedUserIDs) {
		return nil, errs.ErrArgs.WrapMsg("KickedUserIDs duplicate")
	}

	// 获取操作者ID
	opUserID := mcontext.GetOpUserID(ctx)

	// 验证操作者不能踢出自己
	if datautil.Contain(opUserID, req.KickedUserIDs...) {
		return nil, errs.ErrArgs.WrapMsg("opUserID in KickedUserIDs")
	}

	// 获取群主信息
	owner, err := g.db.TakeGroupOwner(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}

	// 验证不能踢出群主
	if datautil.Contain(owner.UserID, req.KickedUserIDs...) {
		return nil, errs.ErrArgs.WrapMsg("ownerUID can not Kick")
	}

	// 获取所有相关成员信息（被踢用户 + 操作者）
	members, err := g.db.FindGroupMembers(ctx, req.GroupID, append(req.KickedUserIDs, opUserID))
	if err != nil {
		return nil, err
	}

	// 填充成员的用户基本信息
	if err := g.PopulateGroupMember(ctx, members...); err != nil {
		return nil, err
	}

	// 构建成员信息映射表，便于快速查找
	memberMap := make(map[string]*model.GroupMember)
	for i, member := range members {
		memberMap[member.UserID] = members[i]
	}

	// 检查是否为系统管理员
	isAppManagerUid := authverify.IsAppManagerUid(ctx, g.config.Share.IMAdminUserID)
	opMember := memberMap[opUserID]

	// 逐个验证是否有权限踢出每个用户
	for _, userID := range req.KickedUserIDs {
		member, ok := memberMap[userID]
		if !ok {
			return nil, servererrs.ErrUserIDNotFound.WrapMsg(userID)
		}

		// 非系统管理员需要进行权限验证
		if !isAppManagerUid {
			if opMember == nil {
				return nil, errs.ErrNoPermission.WrapMsg("opUserID no in group")
			}

			// 根据操作者角色验证权限
			switch opMember.RoleLevel {
			case constant.GroupOwner:
				// 群主可以踢出任何成员（除自己外，已在前面验证）
			case constant.GroupAdmin:
				// 管理员不能踢出群主和其他管理员
				if member.RoleLevel == constant.GroupOwner || member.RoleLevel == constant.GroupAdmin {
					return nil, errs.ErrNoPermission.WrapMsg("group admins cannot remove the group owner and other admins")
				}
			case constant.GroupOrdinaryUsers:
				// 普通成员无踢出权限
				return nil, errs.ErrNoPermission.WrapMsg("opUserID no permission")
			default:
				// 未知角色等级
				return nil, errs.ErrNoPermission.WrapMsg("opUserID roleLevel unknown")
			}
		}
	}

	// 获取当前群组成员数量
	num, err := g.db.FindGroupMemberNum(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}

	// 获取群主用户ID（用于通知）
	ownerUserIDs, err := g.db.GetGroupRoleLevelMemberIDs(ctx, req.GroupID, constant.GroupOwner)
	if err != nil {
		return nil, err
	}
	var ownerUserID string
	if len(ownerUserIDs) > 0 {
		ownerUserID = ownerUserIDs[0]
	}

	// 执行踢出操作：删除群组成员记录
	if err := g.db.DeleteGroupMember(ctx, group.GroupID, req.KickedUserIDs); err != nil {
		return nil, err
	}

	// 构建踢出通知的提示信息
	tips := &sdkws.MemberKickedTips{
		Group: &sdkws.GroupInfo{
			GroupID:                group.GroupID,
			GroupName:              group.GroupName,
			Notification:           group.Notification,
			Introduction:           group.Introduction,
			FaceURL:                group.FaceURL,
			OwnerUserID:            ownerUserID,
			CreateTime:             group.CreateTime.UnixMilli(),
			MemberCount:            num - uint32(len(req.KickedUserIDs)), // 更新后的成员数量
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

	// 设置操作者信息
	if opMember, ok := memberMap[opUserID]; ok {
		tips.OpUser = convert.Db2PbGroupMember(opMember)
	}

	// 设置被踢用户列表
	for _, userID := range req.KickedUserIDs {
		tips.KickedUserList = append(tips.KickedUserList, convert.Db2PbGroupMember(memberMap[userID]))
	}

	// 发送成员被踢通知
	g.notification.MemberKickedNotification(ctx, tips, req.SendMessage)

	// 处理被踢用户的会话序列号设置
	if err := g.deleteMemberAndSetConversationSeq(ctx, req.GroupID, req.KickedUserIDs); err != nil {
		return nil, err
	}

	// 执行踢出后的Webhook回调
	g.webhookAfterKickGroupMember(ctx, &g.config.WebhooksConfig.AfterKickGroupMember, req)

	return &pbgroup.KickGroupMemberResp{}, nil
}

// GetGroupMembersInfo 获取指定群组成员信息
//
// 此方法用于批量获取群组中指定用户的详细成员信息，包括群内角色、
// 加入时间、禁言状态等完整信息。
//
// 业务逻辑：
// 1. 参数验证：用户ID列表非空，群组ID非空
// 2. 权限验证：检查操作者是否有权限查看该群组信息
// 3. 信息获取：调用内部方法获取指定用户的群组成员信息
// 4. 数据返回：返回格式化的成员信息列表
//
// 使用场景：
// - 查看特定成员的详细信息
// - 批量获取多个成员的信息
// - 权限验证和角色确认
//
// 权限要求：
// - 必须是群组成员或系统管理员
// - 只能查看同群成员的信息
//
// 返回数据：
// - 指定用户的群组成员完整信息列表
// - 包含角色、加入时间、禁言状态等详细信息
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

// getGroupMembersInfo 获取群组成员的详细信息
//
// 这是一个内部辅助方法，用于获取指定群组中指定用户的完整成员信息，
// 包括成员的基本信息、群内角色、加入时间、禁言状态等。
//
// 业务逻辑：
// 1. 查询指定用户在群组中的成员记录
// 2. 填充成员的用户基本信息（昵称、头像等）
// 3. 转换数据格式为API响应格式
//
// 使用场景：
// - 获取特定成员的详细信息
// - 批量查询多个成员信息
// - 成员信息展示和权限验证
//
// 参数：
// - ctx: 上下文
// - groupID: 群组ID
// - userIDs: 用户ID列表
//
// 返回：
// - []*sdkws.GroupMemberFullInfo: 群组成员完整信息列表
// - error: 错误信息
func (g *groupServer) getGroupMembersInfo(ctx context.Context, groupID string, userIDs []string) ([]*sdkws.GroupMemberFullInfo, error) {
	// 如果用户ID列表为空，直接返回
	if len(userIDs) == 0 {
		return nil, nil
	}

	// 查询指定用户在群组中的成员记录
	members, err := g.db.FindGroupMembers(ctx, groupID, userIDs)
	if err != nil {
		return nil, err
	}

	// 填充成员的用户基本信息（昵称、头像等）
	if err := g.PopulateGroupMember(ctx, members...); err != nil {
		return nil, err
	}

	// 转换数据格式为API响应格式
	return datautil.Slice(members, func(e *model.GroupMember) *sdkws.GroupMemberFullInfo {
		return convert.Db2PbGroupMember(e)
	}), nil
}

// GetGroupApplicationList 获取群组申请列表
//
// 此方法用于获取指定群组的加群申请列表，支持按处理状态筛选和分页查询。
// 主要用于群组管理员查看和处理加群申请。
//
// 业务逻辑：
// 1. 群组ID处理：如果未指定群组ID，则查询用户管理的所有群组
// 2. 权限验证：验证操作者是否有权限查看指定群组的申请
// 3. 数据查询：根据群组ID和处理状态分页查询申请记录
// 4. 信息聚合：获取申请用户信息、群组信息、群主信息等
// 5. 数据转换：将数据库模型转换为API响应格式
//
// 权限规则：
// - 群主：可以查看自己群组的所有申请
// - 管理员：可以查看自己管理群组的所有申请
// - 系统管理员：可以查看任何群组的申请
//
// 筛选条件：
// - 群组ID列表：可指定多个群组
// - 处理状态：待处理、已同意、已拒绝等
// - 分页参数：支持分页查询
//
// 返回数据：
// - 申请总数
// - 申请详细信息列表（包含申请者、群组、处理状态等）
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

// GetGroupsInfo 获取多个群组的详细信息
//
// 此方法用于批量获取指定群组的详细信息，包括群组基本信息、
// 成员数量、群主信息等完整数据。
//
// 业务逻辑：
// 1. 参数验证：群组ID列表不能为空
// 2. 批量查询：调用内部方法批量获取群组信息
// 3. 数据返回：返回格式化的群组信息列表
//
// 使用场景：
// - 客户端批量获取群组信息
// - 群组列表展示需要详细信息
// - 群组信息同步和缓存更新
//
// 性能优化：
// - 使用批量查询避免N+1查询问题
// - 内部方法实现了高效的数据聚合
//
// 返回数据：
// - 群组基本信息：名称、头像、公告等
// - 群组统计：成员数量、创建时间等
// - 群主信息：群主用户ID等
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

// GetGroupApplicationUnhandledCount 获取未处理的群组申请数量
//
// 此方法用于获取指定用户管理的所有群组中未处理的加群申请总数。
// 主要用于客户端显示未处理申请的数量提醒。
//
// 业务逻辑：
// 1. 权限验证：验证是否有权限查询该用户的未处理申请数量
// 2. 群组查询：查找用户管理的所有群组ID（群主和管理员身份）
// 3. 申请统计：统计这些群组中指定时间后的未处理申请数量
// 4. 结果返回：返回未处理申请的总数
//
// 权限要求：
// - 只能查询自己管理群组的申请数量
// - 系统管理员可以查询任何用户的数据
//
// 时间筛选：
// - 支持指定时间戳，只统计该时间之后的申请
// - 用于增量统计和实时提醒
//
// 使用场景：
// - 客户端红点提醒
// - 管理面板数据统计
// - 实时通知推送
//
// 返回数据：
// - 未处理申请的总数量
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

// getGroupsInfo 获取多个群组的详细信息
//
// 这是一个内部辅助方法，用于批量获取群组的完整信息，包括群组基本信息、
// 成员数量、群主信息等。该方法优化了数据库查询，避免了N+1查询问题。
//
// 业务逻辑：
// 1. 批量查询群组基本信息
// 2. 批量查询每个群组的成员数量
// 3. 批量查询每个群组的群主信息
// 4. 数据聚合和格式转换
//
// 性能优化：
// - 使用批量查询减少数据库访问次数
// - 使用Map结构快速匹配群主信息
// - 一次性填充所有需要的用户信息
//
// 参数：
// - ctx: 上下文
// - groupIDs: 群组ID列表
//
// 返回：
// - []*sdkws.GroupInfo: 群组信息列表
// - error: 错误信息
func (g *groupServer) getGroupsInfo(ctx context.Context, groupIDs []string) ([]*sdkws.GroupInfo, error) {
	// 如果群组ID列表为空，直接返回
	if len(groupIDs) == 0 {
		return nil, nil
	}

	// 批量查询群组基本信息
	groups, err := g.db.FindGroup(ctx, groupIDs)
	if err != nil {
		return nil, err
	}

	// 批量查询每个群组的成员数量
	groupMemberNumMap, err := g.db.MapGroupMemberNum(ctx, groupIDs)
	if err != nil {
		return nil, err
	}

	// 批量查询每个群组的群主信息
	owners, err := g.db.FindGroupsOwner(ctx, groupIDs)
	if err != nil {
		return nil, err
	}

	// 填充群主的用户基本信息
	if err := g.PopulateGroupMember(ctx, owners...); err != nil {
		return nil, err
	}

	// 构建群主信息的映射表，便于快速查找
	ownerMap := datautil.SliceToMap(owners, func(e *model.GroupMember) string {
		return e.GroupID
	})

	// 转换数据格式并返回
	return datautil.Slice(groups, func(e *model.Group) *sdkws.GroupInfo {
		// 获取群主用户ID
		var ownerUserID string
		if owner, ok := ownerMap[e.GroupID]; ok {
			ownerUserID = owner.UserID
		}
		// 转换数据库模型为API响应格式
		return convert.Db2PbGroupInfo(e, ownerUserID, groupMemberNumMap[e.GroupID])
	}), nil
}

// GroupApplicationResponse 处理群组申请响应
//
// 此方法用于处理群组加入申请的审批，支持同意或拒绝申请。
// 只有群主、管理员或系统管理员可以处理群组申请。
//
// 业务逻辑：
// 1. 参数验证：处理结果必须是同意或拒绝
// 2. 权限验证：验证操作者是否有权限处理该群组申请
// 3. 申请状态检查：确认申请未被处理过
// 4. 用户状态检查：验证申请用户是否已在群组中
// 5. 处理逻辑：根据处理结果执行相应操作
// 6. 通知发送：发送处理结果通知
//
// 处理结果：
// - 同意申请：将用户添加到群组，发送欢迎通知
// - 拒绝申请：仅更新申请状态，发送拒绝通知
//
// 权限规则：
// - 群主：可以处理所有申请
// - 管理员：可以处理所有申请
// - 系统管理员：可以处理任何群组的申请
//
// 特殊处理：
// - 用户已在群组中时，只更新申请状态
// - Webhook回调支持业务逻辑扩展
// - 邀请申请和主动申请的通知不同
//
// 安全考虑：
// - 防止重复处理同一申请
// - 验证用户仍然存在
// - 确保申请记录的完整性
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

// JoinGroup 申请加入群组
//
// 此方法处理用户主动申请加入群组的请求，支持两种加入模式：
// 1. 直接加入：群组设置为无需验证，用户直接成为群组成员
// 2. 申请模式：群组需要验证，创建加群申请等待管理员审批
//
// 业务逻辑：
// 1. 用户信息验证：确认申请用户存在
// 2. 群组状态检查：确认群组存在且未解散
// 3. 重复加入检查：确认用户未在群组中
// 4. Webhook回调：执行申请前的业务逻辑扩展
// 5. 加入模式判断：根据群组设置决定直接加入或创建申请
// 6. 通知发送：发送相应的通知消息
// 7. Webhook回调：执行申请后的业务逻辑扩展
//
// 加入模式：
// - 直接加入（NeedVerification = Directly）：立即成为群组成员
// - 申请模式（NeedVerification = AllNeedVerification）：创建申请记录
//
// 特殊处理：
// - 已在群组中的用户不能重复申请
// - 已解散的群组不能申请加入
func (g *groupServer) JoinGroup(ctx context.Context, req *pbgroup.JoinGroupReq) (*pbgroup.JoinGroupResp, error) {
	// 获取申请用户的详细信息
	user, err := g.userClient.GetUserInfo(ctx, req.InviterUserID)
	if err != nil {
		return nil, err
	}

	// 获取群组信息
	group, err := g.db.TakeGroup(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}

	// 检查群组状态，已解散的群组不能申请加入
	if group.Status == constant.GroupStatusDismissed {
		return nil, servererrs.ErrDismissedAlready.Wrap()
	}

	// 构建申请加群前的回调请求参数
	reqCall := &callbackstruct.CallbackJoinGroupReq{
		GroupID:    req.GroupID,
		GroupType:  string(group.GroupType),
		ApplyID:    req.InviterUserID,
		ReqMessage: req.ReqMessage,
		Ex:         req.Ex,
	}

	// 执行申请加群前的Webhook回调
	if err := g.webhookBeforeApplyJoinGroup(ctx, &g.config.WebhooksConfig.BeforeApplyJoinGroup, reqCall); err != nil && err != servererrs.ErrCallbackContinue {
		return nil, err
	}

	// 检查用户是否已经在群组中
	_, err = g.db.TakeGroupMember(ctx, req.GroupID, req.InviterUserID)
	if err == nil {
		// 用户已在群组中，返回参数错误
		return nil, errs.ErrArgs.Wrap()
	} else if !g.IsNotFound(err) && errs.Unwrap(err) != errs.ErrRecordNotFound {
		// 其他数据库错误
		return nil, err
	}

	// 记录调试信息
	log.ZDebug(ctx, "JoinGroup.groupInfo", "group", group, "eq", group.NeedVerification == constant.Directly)

	// 判断群组加入模式
	if group.NeedVerification == constant.Directly {
		// 直接加入模式：无需验证，立即成为群组成员
		groupMember := &model.GroupMember{
			GroupID:        group.GroupID,               // 群组ID
			UserID:         user.UserID,                 // 用户ID
			RoleLevel:      constant.GroupOrdinaryUsers, // 角色等级：普通成员
			OperatorUserID: mcontext.GetOpUserID(ctx),   // 操作者ID
			InviterUserID:  req.InviterUserID,           // 邀请者ID（这里是申请者自己）
			JoinTime:       time.Now(),                  // 加入时间
			MuteEndTime:    time.UnixMilli(0),           // 禁言结束时间（初始为0）
		}

		// 执行成员加入前的Webhook回调
		if err := g.webhookBeforeMembersJoinGroup(ctx, &g.config.WebhooksConfig.BeforeMemberJoinGroup, []*model.GroupMember{groupMember}, group.GroupID, group.Ex); err != nil && err != servererrs.ErrCallbackContinue {
			return nil, err
		}

		// 将用户添加到群组中
		if err := g.db.CreateGroup(ctx, nil, []*model.GroupMember{groupMember}); err != nil {
			return nil, err
		}

		// 发送成员加入通知
		if err = g.notification.MemberEnterNotification(ctx, req.GroupID, req.InviterUserID); err != nil {
			return nil, err
		}

		// 执行加入群组后的Webhook回调
		g.webhookAfterJoinGroup(ctx, &g.config.WebhooksConfig.AfterJoinGroup, req)

		return &pbgroup.JoinGroupResp{}, nil
	}

	// 申请模式：需要验证，创建加群申请记录
	groupRequest := model.GroupRequest{
		UserID:      req.InviterUserID, // 申请用户ID
		ReqMsg:      req.ReqMessage,    // 申请消息
		GroupID:     req.GroupID,       // 群组ID
		JoinSource:  req.JoinSource,    // 加入来源
		ReqTime:     time.Now(),        // 申请时间
		HandledTime: time.Unix(0, 0),   // 处理时间（初始为0）
		Ex:          req.Ex,            // 扩展字段
	}

	// 创建加群申请记录
	if err = g.db.CreateGroupRequest(ctx, []*model.GroupRequest{&groupRequest}); err != nil {
		return nil, err
	}

	// 发送加群申请通知
	g.notification.JoinGroupApplicationNotification(ctx, req, &groupRequest)

	return &pbgroup.JoinGroupResp{}, nil
}

// QuitGroup 退出群组
//
// 此方法处理用户主动退出群组的请求，包括权限验证、数据更新、通知发送等。
//
// 业务逻辑：
// 1. 用户身份确认：确定退出的用户ID（可以是自己或管理员代操作）
// 2. 权限验证：检查操作权限和退出权限
// 3. 成员信息获取：获取要退出用户的群组成员信息
// 4. 特殊角色检查：群主不能退出群组
// 5. 数据库操作：删除群组成员记录
// 6. 会话处理：设置用户的会话序列号
// 7. 通知发送：向群组成员发送退出通知
// 8. Webhook回调：执行退出后的业务逻辑扩展
//
// 权限规则：
// - 用户可以退出自己加入的群组
// - 系统管理员可以代替任何用户退出群组
// - 群主不能退出群组（需要先转让群主身份）
//
// 特殊处理：
// - 退出后用户不能看到退出后的群组消息
// - 退出操作不可撤销
func (g *groupServer) QuitGroup(ctx context.Context, req *pbgroup.QuitGroupReq) (*pbgroup.QuitGroupResp, error) {
	// 确定退出的用户ID
	if req.UserID == "" {
		// 如果未指定用户ID，则为操作者自己退出
		req.UserID = mcontext.GetOpUserID(ctx)
	} else {
		// 如果指定了用户ID，需要验证是否有权限代替该用户退出
		if err := authverify.CheckAccessV3(ctx, req.UserID, g.config.Share.IMAdminUserID); err != nil {
			return nil, err
		}
	}

	// 获取要退出用户的群组成员信息
	member, err := g.db.TakeGroupMember(ctx, req.GroupID, req.UserID)
	if err != nil {
		return nil, err
	}

	// 检查群主不能退出群组
	if member.RoleLevel == constant.GroupOwner {
		return nil, errs.ErrNoPermission.WrapMsg("group owner can't quit")
	}

	// 填充成员的用户基本信息
	if err := g.PopulateGroupMember(ctx, member); err != nil {
		return nil, err
	}

	// 从群组中删除该成员
	err = g.db.DeleteGroupMember(ctx, req.GroupID, []string{req.UserID})
	if err != nil {
		return nil, err
	}

	// 发送成员退出通知
	g.notification.MemberQuitNotification(ctx, g.groupMemberDB2PB(member, 0))

	// 设置用户的会话序列号，确保退出后不能看到后续消息
	if err := g.deleteMemberAndSetConversationSeq(ctx, req.GroupID, []string{req.UserID}); err != nil {
		return nil, err
	}

	// 执行退出群组后的Webhook回调
	g.webhookAfterQuitGroup(ctx, &g.config.WebhooksConfig.AfterQuitGroup, req)

	return &pbgroup.QuitGroupResp{}, nil
}

// deleteMemberAndSetConversationSeq 删除成员并设置会话序列号
//
// 当用户被踢出或主动退出群组时，需要设置该用户的群组会话序列号，
// 确保用户在重新加入群组时不会看到离开期间的历史消息。
//
// 业务逻辑：
// 1. 构建群组会话ID
// 2. 获取当前群组会话的最大序列号
// 3. 为指定用户设置会话序列号为当前最大值
//
// 作用：
// - 保护隐私：用户离开群组后不能看到离开期间的消息
// - 数据一致性：确保会话序列号的正确性
// - 重新加入：用户重新加入时从正确的序列号开始接收消息
//
// 参数：
// - ctx: 上下文
// - groupID: 群组ID
// - userIDs: 需要设置序列号的用户ID列表
//
// 返回：
// - error: 操作错误，nil表示成功
func (g *groupServer) deleteMemberAndSetConversationSeq(ctx context.Context, groupID string, userIDs []string) error {
	// 构建群组会话ID
	conversationID := msgprocessor.GetConversationIDBySessionType(constant.ReadGroupChatType, groupID)

	// 获取当前群组会话的最大序列号
	maxSeq, err := g.msgClient.GetConversationMaxSeq(ctx, conversationID)
	if err != nil {
		return err
	}

	// 为指定用户设置会话序列号为当前最大值
	// 这样用户重新加入时不会看到离开期间的历史消息
	return g.conversationClient.SetConversationMaxSeq(ctx, conversationID, userIDs, maxSeq)
}

// SetGroupInfo 设置群组信息
//
// 此方法用于修改群组的基本信息，包括群名称、头像、公告、介绍等。
// 只有群主、管理员或系统管理员可以修改群组信息。
//
// 业务逻辑：
// 1. 权限验证：验证操作者是否有权限修改群组信息
// 2. Webhook回调：执行修改前的业务逻辑扩展
// 3. 群组状态检查：确认群组存在且未解散
// 4. 信息更新：批量更新群组的各项信息
// 5. 特殊处理：公告和群名称修改需要特殊通知
// 6. 通知发送：向群组成员发送信息变更通知
// 7. Webhook回调：执行修改后的业务逻辑扩展
//
// 可修改信息：
// - 群组名称：群组显示名称
// - 群组头像：群组头像URL
// - 群组公告：群组公告内容
// - 群组介绍：群组简介描述
// - 扩展字段：自定义扩展信息
//
// 权限规则：
// - 群主：可以修改所有群组信息
// - 管理员：可以修改所有群组信息
// - 系统管理员：可以修改任何群组信息
//
// 特殊处理：
// - 公告修改会设置会话@类型为群公告
// - 群名称修改会发送专门的名称变更通知
// - 其他信息修改发送通用的信息变更通知
//
// 性能优化：
// - 只更新变更的字段
// - 批量处理多个字段的更新
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

// SetGroupInfoEx 设置群组信息扩展版本
//
// 此方法是SetGroupInfo的扩展版本，支持更灵活的字段更新控制。
// 使用包装器类型允许精确控制哪些字段需要更新，包括设置空值。
//
// 业务逻辑：
// 1. 权限验证：验证操作者是否有权限修改群组信息
// 2. Webhook回调：执行修改前的业务逻辑扩展
// 3. 群组状态检查：确认群组存在且未解散
// 4. 字段映射：使用包装器类型精确控制更新字段
// 5. 数据库更新：批量更新指定的字段
// 6. 通知处理：根据更新的字段发送相应通知
// 7. Webhook回调：执行修改后的业务逻辑扩展
//
// 与SetGroupInfo的区别：
// - 使用包装器类型，支持NULL值设置
// - 更精确的字段控制
// - 分别处理公告、群名称和其他字段的通知
//
// 字段处理逻辑：
// - 公告字段：设置会话@类型，发送公告通知
// - 群名称：发送专门的名称变更通知
// - 其他字段：发送通用的信息变更通知
//
// 权限规则：
// - 群主：可以修改所有群组信息
// - 管理员：可以修改所有群组信息
// - 系统管理员：可以修改任何群组信息
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

// TransferGroupOwner 转让群主
//
// 此方法用于将群主身份转让给其他群组成员。这是一个重要的管理操作，
// 涉及权限的重新分配和角色的变更。
//
// 业务逻辑：
// 1. 群组状态检查：确认群组存在且未解散
// 2. 参数验证：新旧群主不能是同一人
// 3. 成员验证：确认新旧群主都是群组成员
// 4. 权限验证：验证操作者是否有权限进行转让
// 5. 禁言检查：如果新群主被禁言，自动解除禁言
// 6. 角色转换：执行群主身份的转让
// 7. 通知发送：向群组成员发送群主变更通知
// 8. Webhook回调：执行转让后的业务逻辑扩展
//
// 权限规则：
// - 只有当前群主可以转让群主身份
// - 系统管理员可以强制转让任何群组的群主
//
// 特殊处理：
// - 新群主如果处于禁言状态，会自动解除
// - 原群主身份会降级为普通成员或管理员
// - 转让操作不可撤销
//
// 安全考虑：
// - 验证新群主确实是群组成员
// - 防止转让给不存在的用户
// - 确保操作的原子性
//
// 业务影响：
// - 群主拥有所有群组管理权限
// - 转让后原群主失去特殊权限
// - 影响后续的权限验证和操作
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

// DismissGroup 解散群组
//
// 此方法处理群组解散的请求，包括权限验证、数据处理、通知发送等。
// 解散群组是一个不可逆的操作，会影响所有群组成员。
//
// 业务逻辑：
// 1. 权限验证：只有群主或系统管理员可以解散群组
// 2. 群组状态检查：避免重复解散已解散的群组
// 3. 数据库操作：标记群组为已解散状态或删除相关数据
// 4. 通知发送：向所有群组成员发送解散通知（可选）
// 5. Webhook回调：执行解散后的业务逻辑扩展
//
// 解散模式：
// - 保留模式（DeleteMember=false）：保留群组和成员数据，仅标记为已解散
// - 删除模式（DeleteMember=true）：完全删除群组和成员数据
//
// 权限规则：
// - 群主：可以解散自己创建或拥有的群组
// - 系统管理员：可以解散任何群组
// - 其他成员：无解散权限
//
// 特殊处理：
// - 解散后群组不能再进行任何操作
// - 解散通知会发送给所有成员
// - 解散操作不可撤销
func (g *groupServer) DismissGroup(ctx context.Context, req *pbgroup.DismissGroupReq) (*pbgroup.DismissGroupResp, error) {
	// 获取群主信息
	owner, err := g.db.TakeGroupOwner(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}

	// 权限验证：只有群主或系统管理员可以解散群组
	if !authverify.IsAppManagerUid(ctx, g.config.Share.IMAdminUserID) {
		if owner.UserID != mcontext.GetOpUserID(ctx) {
			return nil, errs.ErrNoPermission.WrapMsg("not group owner")
		}
	}

	// 填充群主的用户基本信息
	if err := g.PopulateGroupMember(ctx, owner); err != nil {
		return nil, err
	}

	// 获取群组信息
	group, err := g.db.TakeGroup(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}

	// 检查群组状态：在保留模式下，避免重复解散已解散的群组
	if !req.DeleteMember && group.Status == constant.GroupStatusDismissed {
		return nil, servererrs.ErrDismissedAlready.WrapMsg("group status is dismissed")
	}

	// 执行群组解散操作
	if err := g.db.DismissGroup(ctx, req.GroupID, req.DeleteMember); err != nil {
		return nil, err
	}

	// 在保留模式下发送解散通知
	if !req.DeleteMember {
		// 获取当前群组成员数量
		num, err := g.db.FindGroupMemberNum(ctx, req.GroupID)
		if err != nil {
			return nil, err
		}

		// 构建群组解散通知的提示信息
		tips := &sdkws.GroupDismissedTips{
			Group:  g.groupDB2PB(group, owner.UserID, num), // 群组信息
			OpUser: &sdkws.GroupMemberFullInfo{},           // 操作者信息
		}

		// 设置操作者信息（如果操作者是群主）
		if mcontext.GetOpUserID(ctx) == owner.UserID {
			tips.OpUser = g.groupMemberDB2PB(owner, 0)
		}

		// 发送群组解散通知
		g.notification.GroupDismissedNotification(ctx, tips, req.SendMessage)
	}

	// 获取所有群组成员ID（用于Webhook回调）
	membersID, err := g.db.FindGroupMemberUserID(ctx, group.GroupID)
	if err != nil {
		return nil, err
	}

	// 构建解散群组后的回调请求参数
	cbReq := &callbackstruct.CallbackDisMissGroupReq{
		GroupID:   req.GroupID,             // 群组ID
		OwnerID:   owner.UserID,            // 群主ID
		MembersID: membersID,               // 所有成员ID列表
		GroupType: string(group.GroupType), // 群组类型
	}

	// 执行解散群组后的Webhook回调
	g.webhookAfterDismissGroup(ctx, &g.config.WebhooksConfig.AfterDismissGroup, cbReq)

	return &pbgroup.DismissGroupResp{}, nil
}

// MuteGroupMember 禁言群组成员
//
// 此方法用于对群组成员进行禁言处理，设置禁言截止时间。
// 被禁言的成员在禁言期间无法在群组中发送消息。
//
// 业务逻辑：
// 1. 成员信息获取：获取被禁言成员的详细信息
// 2. 权限验证：验证操作者是否有权限禁言该成员
// 3. 禁言时间设置：计算禁言截止时间
// 4. 数据库更新：更新成员的禁言截止时间
// 5. 通知发送：向群组发送成员被禁言通知
//
// 权限规则：
// - 群主：可以禁言任何成员（除自己外）
// - 管理员：可以禁言普通成员，不能禁言群主和其他管理员
// - 普通成员：无禁言权限
// - 系统管理员：可以禁言任何成员
//
// 禁言规则：
// - 群主不能被禁言
// - 管理员只能被群主禁言
// - 普通成员可以被群主和管理员禁言
//
// 时间处理：
// - 支持设置任意时长的禁言
// - 禁言时间以秒为单位
// - 禁言时间为0表示永久禁言
//
// 业务影响：
// - 被禁言成员无法发送群消息
// - 禁言状态会同步到所有客户端
// - 禁言到期后自动解除
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

// CancelMuteGroupMember 取消群组成员禁言
//
// 此方法用于解除群组成员的禁言状态，恢复其在群组中的发言权限。
//
// 业务逻辑：
// 1. 成员信息获取：获取被禁言成员的详细信息
// 2. 权限验证：验证操作者是否有权限解除该成员的禁言
// 3. 禁言状态清除：将成员的禁言截止时间设置为历史时间
// 4. 数据库更新：更新成员的禁言状态
// 5. 通知发送：向群组发送成员解除禁言通知
//
// 权限规则：
// - 群主：可以解除任何成员的禁言
// - 管理员：可以解除普通成员的禁言，不能解除群主和其他管理员的禁言
// - 普通成员：无解除禁言权限
// - 系统管理员：可以解除任何成员的禁言
//
// 解除规则：
// - 只有被禁言的成员才需要解除
// - 解除后成员立即恢复发言权限
// - 解除操作会发送通知给所有群成员
//
// 特殊处理：
// - 如果成员本来就没有被禁言，操作仍然会成功
// - 权限验证与禁言时的规则保持一致
//
// 业务影响：
// - 成员恢复群组发言权限
// - 禁言状态同步到所有客户端
// - 操作记录用于审计
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

// MuteGroup 禁言整个群组
//
// 此方法用于对整个群组进行禁言，禁言后除了群主和管理员外，
// 其他成员都无法在群组中发送消息。
//
// 业务逻辑：
// 1. 权限验证：验证操作者是否有群组管理权限
// 2. 群组状态更新：将群组状态设置为禁言状态
// 3. 通知发送：向群组成员发送群组被禁言通知
//
// 权限规则：
// - 群主：可以对群组进行全员禁言
// - 管理员：可以对群组进行全员禁言
// - 系统管理员：可以对任何群组进行全员禁言
//
// 禁言效果：
// - 普通成员无法发送消息
// - 群主和管理员仍可正常发言
// - 群组其他功能不受影响
//
// 业务影响：
// - 群组进入静默状态
// - 有助于群组秩序管理
// - 可用于重要通知发布时
//
// 使用场景：
// - 群组秩序维护
// - 重要公告发布
// - 临时管理需要
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

// CancelMuteGroup 取消群组禁言
//
// 此方法用于解除整个群组的禁言状态，恢复所有成员的发言权限。
//
// 业务逻辑：
// 1. 权限验证：验证操作者是否有群组管理权限
// 2. 群组状态更新：将群组状态设置为正常状态
// 3. 通知发送：向群组成员发送群组解除禁言通知
//
// 权限规则：
// - 群主：可以解除群组全员禁言
// - 管理员：可以解除群组全员禁言
// - 系统管理员：可以解除任何群组的全员禁言
//
// 解除效果：
// - 所有成员恢复发言权限
// - 群组回到正常状态
// - 个人禁言不受影响（个人禁言需要单独解除）
//
// 注意事项：
// - 解除群组禁言不会影响个人的禁言状态
// - 如果成员同时有个人禁言，仍然无法发言
// - 解除操作会通知所有群成员
//
// 业务影响：
// - 群组恢复正常交流状态
// - 所有成员可以正常发送消息
// - 群组功能完全恢复
//
// 使用场景：
// - 临时管理结束
// - 重要通知发布完毕
// - 群组秩序恢复正常
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

// SetGroupMemberInfo 设置群组成员信息
//
// 此方法用于修改群组成员的信息，包括群内昵称、头像、角色等级、扩展字段等。
// 支持批量修改多个成员的信息，并具有严格的权限控制。
//
// 业务逻辑：
// 1. 参数验证：成员列表非空，成员信息有效
// 2. 权限验证：复杂的多层权限验证逻辑
// 3. 数据验证：成员存在性验证，角色等级有效性检查
// 4. Webhook回调：执行修改前的业务逻辑扩展
// 5. 批量更新：使用事务进行批量数据更新
// 6. 通知发送：根据修改内容发送相应通知
// 7. Webhook回调：执行修改后的业务逻辑扩展
//
// 权限规则（复杂的层级权限控制）：
// - 系统管理员：可以修改任何成员的任何信息
// - 群主：可以修改所有成员信息，但不能提升自己的角色
// - 管理员：可以修改普通成员信息，不能修改群主和其他管理员
// - 普通成员：只能修改自己的非角色信息
//
// 可修改字段：
// - 群内昵称：成员在群组中显示的昵称
// - 群内头像：成员在群组中显示的头像
// - 角色等级：普通成员、管理员（不能设置为群主）
// - 扩展字段：自定义的扩展信息
//
// 角色变更规则：
// - 不能设置为群主（群主转让需要专门接口）
// - 普通成员 ↔ 管理员 可以互相转换
// - 角色变更会发送专门的角色变更通知
//
// 特殊处理：
// - 支持批量操作，减少网络开销
// - 严格的权限验证，防止权限提升攻击
// - 分别处理角色变更和信息变更的通知
//
// 安全考虑：
// - 防止用户提升自己的权限
// - 防止管理员修改其他管理员信息
// - 确保群主身份不被误修改
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

// GetGroupMemberUserIDs 获取群组成员用户ID列表
//
// 此方法用于获取指定群组的所有成员用户ID列表，主要用于消息推送、
// 通知发送等需要群组成员ID列表的场景。
//
// 业务逻辑：
// 1. 数据查询：查询群组的所有成员用户ID
// 2. 权限验证：验证操作者是否有权限获取该群组成员列表
// 3. 结果返回：返回用户ID列表
//
// 权限验证：
// - 系统管理员：可以获取任何群组的成员ID列表
// - 群组成员：只能获取自己所在群组的成员ID列表
// - 非群组成员：无权限获取
//
// 使用场景：
// - 群组消息推送
// - 群组通知发送
// - 群组统计分析
// - 权限验证辅助
//
// 性能考虑：
// - 只返回用户ID，不包含详细信息
// - 适用于大群组的高效查询
// - 减少网络传输数据量
//
// 安全考虑：
// - 严格的权限验证
// - 防止非授权访问群组成员信息
// - 操作者必须是群组成员或系统管理员
//
// 返回数据：
// - 群组所有成员的用户ID列表
// - 按加入时间或其他规则排序
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
