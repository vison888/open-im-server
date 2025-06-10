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

// Package relation 用户关系服务模块
//
// 本文件实现了OpenIM好友关系系统的核心功能，是IM系统中用户社交关系管理的核心组件。
//
// 核心功能：
// 1. 好友申请流程：发起申请、处理申请（同意/拒绝）
// 2. 好友关系管理：添加好友、删除好友、设置备注
// 3. 好友列表查询：分页查询、指定查询、关系验证
// 4. 好友申请管理：查询待处理申请、历史申请记录
// 5. 黑名单管理：添加黑名单、移除黑名单、查询黑名单
// 6. 批量导入好友：管理员批量导入好友关系
//
// 技术特点：
// - 基于gRPC的微服务架构
// - MongoDB + Redis的混合存储策略
// - 支持Webhook回调机制
// - 完善的权限验证和安全控制
// - 异步通知机制保证实时性
// - 版本化的增量同步机制
//
// 数据流：
// 客户端 -> gRPC接口 -> 业务逻辑层 -> 数据控制器 -> 存储层(MongoDB/Redis)
//
//	↓
//
// Webhook回调 -> 外部系统
//
//	↓
//
// 通知系统 -> 消息推送
package relation

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/notification/common_user"
	"github.com/openimsdk/open-im-server/v3/pkg/rpcli"

	"github.com/openimsdk/tools/mq/memamq"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache/redis"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database/mgo"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/open-im-server/v3/pkg/common/webhook"
	"github.com/openimsdk/open-im-server/v3/pkg/localcache"
	"github.com/openimsdk/tools/db/redisutil"

	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/open-im-server/v3/pkg/common/convert"
	"github.com/openimsdk/open-im-server/v3/pkg/common/servererrs"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/controller"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/relation"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/db/mongoutil"
	"github.com/openimsdk/tools/discovery"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/utils/datautil"
	"google.golang.org/grpc"
)

// friendServer 好友关系服务器结构体
//
// 这是好友关系服务的核心结构，实现了所有好友相关的gRPC接口。
// 负责处理客户端的好友请求，包括好友的CRUD操作、申请处理、通知推送等功能。
//
// 架构设计：
// - 采用依赖注入模式，通过接口隔离各个组件
// - 分层架构：接口层 -> 业务逻辑层 -> 数据访问层
// - 微服务架构：通过RPC客户端调用其他服务
//
// 安全机制：
// - 权限验证：确保用户只能操作自己的数据
// - 回调验证：支持外部系统的业务逻辑介入
// - 数据校验：防止恶意数据和重复操作
type friendServer struct {
	// 继承gRPC服务的未实现方法，确保接口完整性
	relation.UnimplementedFriendServer

	// db 好友关系数据库控制器
	// 这是好友数据访问层的核心组件，封装了好友关系的所有数据库操作。
	// 主要功能：
	// - MongoDB的好友数据持久化
	// - Redis的好友数据缓存
	// - 好友申请记录管理
	// - 事务管理和数据一致性保证
	db controller.FriendDatabase

	// blackDatabase 黑名单数据库控制器
	// 专门处理黑名单相关的数据操作
	// 主要功能：
	// - 黑名单关系的增删查改
	// - 黑名单状态验证
	// - 黑名单数据缓存管理
	blackDatabase controller.BlackDatabase

	// notificationSender 好友通知发送器
	// 负责发送好友相关的实时通知，确保多端数据同步
	// 主要通知类型：
	// - 好友申请通知
	// - 好友申请处理结果通知
	// - 好友关系变更通知
	// - 黑名单变更通知
	notificationSender *FriendNotificationSender

	// RegisterCenter 服务注册中心
	// 用于服务发现和服务间通信
	RegisterCenter discovery.SvcDiscoveryRegistry

	// config 服务配置信息
	// 包含好友服务运行所需的所有配置参数
	config *Config

	// webhookClient Webhook客户端
	// 用于向外部系统发送回调通知，支持业务逻辑扩展
	// 支持的回调事件：
	// - 添加好友前回调
	// - 添加好友后回调
	// - 删除好友后回调
	// - 设置好友备注前/后回调
	// - 黑名单操作前/后回调
	webhookClient *webhook.Client

	// queue 内存队列
	// 用于异步处理一些非关键业务逻辑，提高接口响应速度
	// 主要用途：
	// - 异步通知处理
	// - 异步版本更新
	// - 异步回调处理
	queue *memamq.MemoryQueue

	// userClient 用户服务客户端
	// 用于获取用户基本信息和验证用户有效性
	// 主要用途：
	// - 验证用户是否存在
	// - 获取用户基本信息用于通知
	// - 用户信息变更时的关联更新
	userClient *rpcli.UserClient
}

// Config 好友服务配置结构体
//
// 包含了好友服务运行所需的所有配置信息，通过依赖注入的方式
// 传递给服务实例，实现了配置与代码的分离。
//
// 配置分类：
// - 核心服务配置：RPC服务端口、监听地址等
// - 存储配置：数据库连接、缓存配置等
// - 外部集成配置：Webhook回调、服务发现等
// - 性能优化配置：本地缓存、队列等
type Config struct {
	RpcConfig          config.Friend       // RPC服务配置：端口、超时、中间件等
	RedisConfig        config.Redis        // Redis缓存配置：连接串、连接池、过期时间等
	MongodbConfig      config.Mongo        // MongoDB数据库配置：连接串、数据库名、集合配置等
	NotificationConfig config.Notification // 通知服务配置：推送策略、重试机制、模板配置等
	Share              config.Share        // 共享配置信息：服务注册名、公共参数等
	WebhooksConfig     config.Webhooks     // Webhook配置：回调URL、认证信息、超时设置等
	LocalCacheConfig   config.LocalCache   // 本地缓存配置：缓存大小、过期策略、清理策略等
	Discovery          config.Discovery    // 服务发现配置：注册中心地址、健康检查等
}

// Start 启动好友关系服务
//
// 这是好友服务的启动入口，负责初始化所有依赖组件并注册gRPC服务。
// 采用了依赖注入和控制反转的设计模式，确保组件之间的松耦合。
//
// 初始化流程：
// 1. 建立数据库连接（MongoDB + Redis）
// 2. 初始化数据访问层组件（好友数据库、黑名单数据库）
// 3. 建立与其他微服务的连接（用户服务、消息服务）
// 4. 初始化通知发送器和Webhook客户端
// 5. 初始化本地缓存和内存队列
// 6. 创建服务实例并注册到gRPC服务器
//
// 参数：
// - ctx: 服务启动的上下文，用于控制启动过程和传递取消信号
// - config: 服务配置信息，包含所有必要的配置参数
// - client: 服务发现注册器，用于注册服务和发现其他服务
// - server: gRPC服务器实例，用于注册好友服务
//
// 返回：
// - error: 启动过程中的错误信息，nil表示启动成功
//
// 错误处理：
// - 数据库连接失败会直接返回错误
// - 服务发现失败会导致启动失败
// - 配置错误会在验证阶段被发现
func Start(ctx context.Context, config *Config, client discovery.SvcDiscoveryRegistry, server *grpc.Server) error {
	// 1. 初始化MongoDB连接
	// MongoDB用于持久化存储好友关系数据、好友申请记录等
	mgocli, err := mongoutil.NewMongoDB(ctx, config.MongodbConfig.Build())
	if err != nil {
		return err
	}

	// 2. 初始化Redis连接
	// Redis用于缓存热点数据，提高查询性能，减少数据库压力
	rdb, err := redisutil.NewRedisClient(ctx, config.RedisConfig.Build())
	if err != nil {
		return err
	}

	// 3. 创建好友关系数据库DAO（数据访问对象）
	// 封装了MongoDB的具体操作，提供了好友关系数据的CRUD接口
	friendMongoDB, err := mgo.NewFriendMongo(mgocli.GetDB())
	if err != nil {
		return err
	}

	// 4. 创建好友申请记录数据库DAO
	// 专门处理好友申请的存储和查询
	friendRequestMongoDB, err := mgo.NewFriendRequestMongo(mgocli.GetDB())
	if err != nil {
		return err
	}

	// 5. 创建黑名单数据库DAO
	// 处理黑名单关系的存储和查询
	blackMongoDB, err := mgo.NewBlackMongo(mgocli.GetDB())
	if err != nil {
		return err
	}

	// 6. 获取其他服务的连接
	// 通过服务发现机制获取其他微服务的连接，实现服务间通信

	// 获取用户服务连接
	userConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.User)
	if err != nil {
		return err
	}

	// 获取消息服务连接
	msgConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.Msg)
	if err != nil {
		return err
	}

	// 7. 创建用户服务客户端
	// 用于调用用户服务的接口，获取用户信息和验证用户有效性
	userClient := rpcli.NewUserClient(userConn)

	// 8. 初始化好友通知发送器
	// 配置通知发送的各种参数和回调函数
	notificationSender := NewFriendNotificationSender(
		&config.NotificationConfig,           // 通知配置
		rpcli.NewMsgClient(msgConn),          // 消息服务客户端
		WithRpcFunc(userClient.GetUsersInfo), // 用户信息获取函数
	)

	// 9. 初始化本地缓存
	// 本地缓存用于缓存频繁访问的数据，减少网络开销
	localcache.InitLocalCache(&config.LocalCacheConfig)

	// 10. 注册好友服务到gRPC服务器
	// 创建好友服务实例并注册，使其能够处理客户端请求
	relation.RegisterFriendServer(server, &friendServer{
		// 初始化好友数据库控制器
		// 组合了MongoDB DAO、Redis缓存和事务管理器
		db: controller.NewFriendDatabase(
			friendMongoDB,        // 好友关系MongoDB DAO
			friendRequestMongoDB, // 好友申请MongoDB DAO
			redis.NewFriendCacheRedis(rdb, &config.LocalCacheConfig, friendMongoDB, redis.GetRocksCacheOptions()), // 好友关系Redis缓存
			mgocli.GetTx(), // MongoDB事务管理器
		),

		// 初始化黑名单数据库控制器
		blackDatabase: controller.NewBlackDatabase(
			blackMongoDB, // 黑名单MongoDB DAO
			redis.NewBlackCacheRedis(rdb, &config.LocalCacheConfig, blackMongoDB, redis.GetRocksCacheOptions()), // 黑名单Redis缓存
		),

		// 组装各种组件
		notificationSender: notificationSender,                                  // 通知发送器
		RegisterCenter:     client,                                              // 服务注册中心
		config:             config,                                              // 服务配置
		webhookClient:      webhook.NewWebhookClient(config.WebhooksConfig.URL), // Webhook客户端
		queue:              memamq.NewMemoryQueue(16, 1024*1024),                // 内存队列：16个worker，1MB缓冲区
		userClient:         userClient,                                          // 用户服务客户端
	})

	return nil
}

// ApplyToAddFriend 申请添加好友
//
// 用户发起添加好友申请的核心方法。实现了完整的好友申请流程，
// 包括权限验证、重复性检查、外部回调、数据存储和通知推送。
//
// 业务流程：
// 1. 权限验证：确认用户有权限发起申请
// 2. 基础校验：防止自己添加自己等非法操作
// 3. Webhook前置回调：允许外部系统干预申请流程
// 4. 用户有效性验证：确认申请双方用户都存在
// 5. 关系状态检查：防止重复申请已是好友的用户
// 6. 申请记录存储：保存申请信息到数据库
// 7. 异步通知推送：通知目标用户有新的好友申请
// 8. Webhook后置回调：通知外部系统申请已完成
//
// 参数说明：
// - req.FromUserID: 发起申请的用户ID
// - req.ToUserID: 目标用户ID（被申请加为好友的用户）
// - req.ReqMsg: 申请消息内容
// - req.Ex: 扩展字段，用于自定义数据
//
// 返回值：
// - 成功时返回空响应
// - 失败时返回具体的错误信息
//
// 错误类型：
// - 权限错误：用户无权限发起申请
// - 参数错误：不能添加自己为好友
// - 业务逻辑错误：用户已是好友关系
// - 系统错误：数据库操作失败、网络错误等
func (s *friendServer) ApplyToAddFriend(ctx context.Context, req *relation.ApplyToAddFriendReq) (resp *relation.ApplyToAddFriendResp, err error) {
	resp = &relation.ApplyToAddFriendResp{}

	// 1. 权限验证：检查用户是否有权限发起好友申请
	// 使用CheckAccessV3进行权限验证，确保只有授权用户才能操作
	if err := authverify.CheckAccessV3(ctx, req.FromUserID, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	// 2. 基础参数校验：防止用户添加自己为好友
	if req.ToUserID == req.FromUserID {
		return nil, servererrs.ErrCanNotAddYourself.WrapMsg("req.ToUserID", req.ToUserID)
	}

	// 3. Webhook前置回调：在添加好友前调用外部系统
	// 允许外部系统根据业务逻辑决定是否允许此次好友申请
	// 如果回调返回错误且不是Continue类型，则终止申请流程
	if err = s.webhookBeforeAddFriend(ctx, &s.config.WebhooksConfig.BeforeAddFriend, req); err != nil && err != servererrs.ErrCallbackContinue {
		return nil, err
	}

	// 4. 用户有效性验证：确认申请双方用户都存在于系统中
	// 通过用户服务验证用户ID的有效性，防止向不存在的用户发起申请
	if err := s.userClient.CheckUser(ctx, []string{req.ToUserID, req.FromUserID}); err != nil {
		return nil, err
	}

	// 5. 好友关系状态检查：验证双方是否已经是好友关系
	// CheckIn方法检查两个用户是否互相在对方的好友列表中
	// in1: FromUser是否在ToUser的好友列表中
	// in2: ToUser是否在FromUser的好友列表中
	in1, in2, err := s.db.CheckIn(ctx, req.FromUserID, req.ToUserID)
	if err != nil {
		return nil, err
	}

	// 如果双方都已经是好友关系，则返回错误
	if in1 && in2 {
		return nil, servererrs.ErrRelationshipAlready.WrapMsg("already friends has f")
	}

	// 6. 添加好友申请记录到数据库
	// 将申请信息持久化存储，包括申请消息和扩展字段
	if err = s.db.AddFriendRequest(ctx, req.FromUserID, req.ToUserID, req.ReqMsg, req.Ex); err != nil {
		return nil, err
	}

	// 7. 异步发送好友申请通知
	// 通知目标用户有新的好友申请，支持多端实时同步
	s.notificationSender.FriendApplicationAddNotification(ctx, req)

	// 8. Webhook后置回调：通知外部系统好友申请已完成
	// 异步调用，不会阻塞主流程
	s.webhookAfterAddFriend(ctx, &s.config.WebhooksConfig.AfterAddFriend, req)

	return resp, nil
}

// ImportFriends 批量导入好友关系
//
// 管理员专用接口，用于批量导入好友关系。主要用于数据迁移、
// 系统初始化或特殊业务场景下的批量好友关系建立。
//
// 业务流程：
// 1. 管理员权限验证：确保只有管理员能调用此接口
// 2. 用户有效性验证：验证所有相关用户都存在
// 3. 参数合法性检查：防止自己添加自己、重复用户ID等
// 4. Webhook前置回调：允许外部系统干预导入流程
// 5. 批量建立好友关系：直接在数据库中建立好友关系
// 6. 批量发送通知：为每个新好友发送通知
// 7. Webhook后置回调：通知外部系统导入已完成
//
// 特点：
// - 跳过申请流程：直接建立好友关系
// - 批量操作：一次性处理多个好友关系
// - 管理员专用：普通用户无法调用
// - 支持事务：保证数据一致性
//
// 参数说明：
// - req.OwnerUserID: 好友关系的拥有者用户ID
// - req.FriendUserIDs: 要添加为好友的用户ID列表
//
// 安全考虑：
// - 严格的管理员权限验证
// - 防止重复和自引用
// - 完整的参数校验
func (s *friendServer) ImportFriends(ctx context.Context, req *relation.ImportFriendReq) (resp *relation.ImportFriendResp, err error) {
	// 1. 管理员权限验证：确保只有系统管理员才能批量导入好友
	// 这是一个高权限操作，必须严格控制访问权限
	if err := authverify.CheckAdmin(ctx, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	// 2. 用户有效性验证：验证拥有者和所有好友用户都存在于系统中
	// 将拥有者ID和好友ID列表合并进行批量验证，提高效率
	if err := s.userClient.CheckUser(ctx, append([]string{req.OwnerUserID}, req.FriendUserIDs...)); err != nil {
		return nil, err
	}

	// 3. 防止自己添加自己为好友
	// 检查拥有者ID是否出现在好友ID列表中
	if datautil.Contain(req.OwnerUserID, req.FriendUserIDs...) {
		return nil, servererrs.ErrCanNotAddYourself.WrapMsg("can not add yourself")
	}

	// 4. 检查好友ID列表是否有重复
	// 防止重复的好友ID导致数据异常
	if datautil.Duplicate(req.FriendUserIDs) {
		return nil, errs.ErrArgs.WrapMsg("friend userID repeated")
	}

	// 5. Webhook前置回调：在批量导入前调用外部系统
	// 允许外部系统根据业务逻辑修改或拦截导入操作
	if err := s.webhookBeforeImportFriends(ctx, &s.config.WebhooksConfig.BeforeImportFriends, req); err != nil && err != servererrs.ErrCallbackContinue {
		return nil, err
	}

	// 6. 批量建立好友关系
	// 使用BecomeFriends方法直接在数据库中建立好友关系
	// constant.BecomeFriendByImport 标识这是通过导入方式建立的好友关系
	if err := s.db.BecomeFriends(ctx, req.OwnerUserID, req.FriendUserIDs, constant.BecomeFriendByImport); err != nil {
		return nil, err
	}

	// 7. 为每个新好友发送好友申请同意通知
	// 模拟好友申请被同意的通知，保持通知的一致性
	for _, userID := range req.FriendUserIDs {
		s.notificationSender.FriendApplicationAgreedNotification(ctx, &relation.RespondFriendApplyReq{
			FromUserID:   req.OwnerUserID,              // 申请方（实际是导入的拥有者）
			ToUserID:     userID,                       // 被申请方（新添加的好友）
			HandleResult: constant.FriendResponseAgree, // 处理结果：同意
		})
	}

	// 8. Webhook后置回调：通知外部系统批量导入已完成
	// 异步调用，不会阻塞主流程
	s.webhookAfterImportFriends(ctx, &s.config.WebhooksConfig.AfterImportFriends, req)

	return &relation.ImportFriendResp{}, nil
}

// ok.
func (s *friendServer) RespondFriendApply(ctx context.Context, req *relation.RespondFriendApplyReq) (resp *relation.RespondFriendApplyResp, err error) {
	resp = &relation.RespondFriendApplyResp{}
	if err := authverify.CheckAccessV3(ctx, req.ToUserID, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	friendRequest := model.FriendRequest{
		FromUserID:   req.FromUserID,
		ToUserID:     req.ToUserID,
		HandleMsg:    req.HandleMsg,
		HandleResult: req.HandleResult,
	}
	if req.HandleResult == constant.FriendResponseAgree {
		if err := s.webhookBeforeAddFriendAgree(ctx, &s.config.WebhooksConfig.BeforeAddFriendAgree, req); err != nil && err != servererrs.ErrCallbackContinue {
			return nil, err
		}
		err := s.db.AgreeFriendRequest(ctx, &friendRequest)
		if err != nil {
			return nil, err
		}
		s.webhookAfterAddFriendAgree(ctx, &s.config.WebhooksConfig.AfterAddFriendAgree, req)
		s.notificationSender.FriendApplicationAgreedNotification(ctx, req)
		return resp, nil
	}
	if req.HandleResult == constant.FriendResponseRefuse {
		err := s.db.RefuseFriendRequest(ctx, &friendRequest)
		if err != nil {
			return nil, err
		}
		s.notificationSender.FriendApplicationRefusedNotification(ctx, req)
		return resp, nil
	}
	return nil, errs.ErrArgs.WrapMsg("req.HandleResult != -1/1")
}

// ok.
func (s *friendServer) DeleteFriend(ctx context.Context, req *relation.DeleteFriendReq) (resp *relation.DeleteFriendResp, err error) {
	if err := authverify.CheckAccessV3(ctx, req.OwnerUserID, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	_, err = s.db.FindFriendsWithError(ctx, req.OwnerUserID, []string{req.FriendUserID})
	if err != nil {
		return nil, err
	}

	if err := s.db.Delete(ctx, req.OwnerUserID, []string{req.FriendUserID}); err != nil {
		return nil, err
	}

	s.notificationSender.FriendDeletedNotification(ctx, req)
	s.webhookAfterDeleteFriend(ctx, &s.config.WebhooksConfig.AfterDeleteFriend, req)

	return &relation.DeleteFriendResp{}, nil
}

// ok.
func (s *friendServer) SetFriendRemark(ctx context.Context, req *relation.SetFriendRemarkReq) (resp *relation.SetFriendRemarkResp, err error) {
	if err = s.webhookBeforeSetFriendRemark(ctx, &s.config.WebhooksConfig.BeforeSetFriendRemark, req); err != nil && err != servererrs.ErrCallbackContinue {
		return nil, err
	}

	if err := authverify.CheckAccessV3(ctx, req.OwnerUserID, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	_, err = s.db.FindFriendsWithError(ctx, req.OwnerUserID, []string{req.FriendUserID})
	if err != nil {
		return nil, err
	}

	if err := s.db.UpdateRemark(ctx, req.OwnerUserID, req.FriendUserID, req.Remark); err != nil {
		return nil, err
	}

	s.webhookAfterSetFriendRemark(ctx, &s.config.WebhooksConfig.AfterSetFriendRemark, req)
	s.notificationSender.FriendRemarkSetNotification(ctx, req.OwnerUserID, req.FriendUserID)

	return &relation.SetFriendRemarkResp{}, nil
}

func (s *friendServer) GetFriendInfo(ctx context.Context, req *relation.GetFriendInfoReq) (*relation.GetFriendInfoResp, error) {
	if err := authverify.CheckAccessV3(ctx, req.OwnerUserID, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}
	friends, err := s.db.FindFriendsWithError(ctx, req.OwnerUserID, req.FriendUserIDs)
	if err != nil {
		return nil, err
	}
	return &relation.GetFriendInfoResp{FriendInfos: convert.FriendOnlyDB2PbOnly(friends)}, nil
}

func (s *friendServer) GetDesignatedFriends(ctx context.Context, req *relation.GetDesignatedFriendsReq) (resp *relation.GetDesignatedFriendsResp, err error) {
	resp = &relation.GetDesignatedFriendsResp{}
	if datautil.Duplicate(req.FriendUserIDs) {
		return nil, errs.ErrArgs.WrapMsg("friend userID repeated")
	}
	if err := authverify.CheckAccessV3(ctx, req.OwnerUserID, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}
	friends, err := s.getFriend(ctx, req.OwnerUserID, req.FriendUserIDs)
	if err != nil {
		return nil, err
	}
	return &relation.GetDesignatedFriendsResp{
		FriendsInfo: friends,
	}, nil
}

func (s *friendServer) getFriend(ctx context.Context, ownerUserID string, friendUserIDs []string) ([]*sdkws.FriendInfo, error) {
	if len(friendUserIDs) == 0 {
		return nil, nil
	}
	friends, err := s.db.FindFriendsWithError(ctx, ownerUserID, friendUserIDs)
	if err != nil {
		return nil, err
	}
	return convert.FriendsDB2Pb(ctx, friends, s.userClient.GetUsersInfoMap)
}

// Get the list of friend requests sent out proactively.
func (s *friendServer) GetDesignatedFriendsApply(ctx context.Context,
	req *relation.GetDesignatedFriendsApplyReq,
) (resp *relation.GetDesignatedFriendsApplyResp, err error) {
	friendRequests, err := s.db.FindBothFriendRequests(ctx, req.FromUserID, req.ToUserID)
	if err != nil {
		return nil, err
	}
	resp = &relation.GetDesignatedFriendsApplyResp{}
	resp.FriendRequests, err = convert.FriendRequestDB2Pb(ctx, friendRequests, s.getCommonUserMap)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Get received friend requests (i.e., those initiated by others).
func (s *friendServer) GetPaginationFriendsApplyTo(ctx context.Context, req *relation.GetPaginationFriendsApplyToReq) (resp *relation.GetPaginationFriendsApplyToResp, err error) {
	if err := authverify.CheckAccessV3(ctx, req.UserID, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	handleResults := datautil.Slice(req.HandleResults, func(e int32) int {
		return int(e)
	})
	total, friendRequests, err := s.db.PageFriendRequestToMe(ctx, req.UserID, handleResults, req.Pagination)
	if err != nil {
		return nil, err
	}

	resp = &relation.GetPaginationFriendsApplyToResp{}
	resp.FriendRequests, err = convert.FriendRequestDB2Pb(ctx, friendRequests, s.getCommonUserMap)
	if err != nil {
		return nil, err
	}

	resp.Total = int32(total)

	return resp, nil
}

func (s *friendServer) GetPaginationFriendsApplyFrom(ctx context.Context, req *relation.GetPaginationFriendsApplyFromReq) (resp *relation.GetPaginationFriendsApplyFromResp, err error) {
	resp = &relation.GetPaginationFriendsApplyFromResp{}

	if err := authverify.CheckAccessV3(ctx, req.UserID, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	handleResults := datautil.Slice(req.HandleResults, func(e int32) int {
		return int(e)
	})
	total, friendRequests, err := s.db.PageFriendRequestFromMe(ctx, req.UserID, handleResults, req.Pagination)
	if err != nil {
		return nil, err
	}

	resp.FriendRequests, err = convert.FriendRequestDB2Pb(ctx, friendRequests, s.getCommonUserMap)
	if err != nil {
		return nil, err
	}

	resp.Total = int32(total)

	return resp, nil
}

// ok.
func (s *friendServer) IsFriend(ctx context.Context, req *relation.IsFriendReq) (resp *relation.IsFriendResp, err error) {
	resp = &relation.IsFriendResp{}
	resp.InUser1Friends, resp.InUser2Friends, err = s.db.CheckIn(ctx, req.UserID1, req.UserID2)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *friendServer) GetPaginationFriends(ctx context.Context, req *relation.GetPaginationFriendsReq) (resp *relation.GetPaginationFriendsResp, err error) {
	if err := authverify.CheckAccessV3(ctx, req.UserID, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	total, friends, err := s.db.PageOwnerFriends(ctx, req.UserID, req.Pagination)
	if err != nil {
		return nil, err
	}

	resp = &relation.GetPaginationFriendsResp{}
	resp.FriendsInfo, err = convert.FriendsDB2Pb(ctx, friends, s.userClient.GetUsersInfoMap)
	if err != nil {
		return nil, err
	}

	resp.Total = int32(total)

	return resp, nil
}

func (s *friendServer) GetFriendIDs(ctx context.Context, req *relation.GetFriendIDsReq) (resp *relation.GetFriendIDsResp, err error) {
	if err := authverify.CheckAccessV3(ctx, req.UserID, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	resp = &relation.GetFriendIDsResp{}
	resp.FriendIDs, err = s.db.FindFriendUserIDs(ctx, req.UserID)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (s *friendServer) GetSpecifiedFriendsInfo(ctx context.Context, req *relation.GetSpecifiedFriendsInfoReq) (*relation.GetSpecifiedFriendsInfoResp, error) {
	if len(req.UserIDList) == 0 {
		return nil, errs.ErrArgs.WrapMsg("userIDList is empty")
	}

	if datautil.Duplicate(req.UserIDList) {
		return nil, errs.ErrArgs.WrapMsg("userIDList repeated")
	}

	if err := authverify.CheckAccessV3(ctx, req.OwnerUserID, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	userMap, err := s.userClient.GetUsersInfoMap(ctx, req.UserIDList)
	if err != nil {
		return nil, err
	}

	friends, err := s.db.FindFriendsWithError(ctx, req.OwnerUserID, req.UserIDList)
	if err != nil {
		return nil, err
	}

	blacks, err := s.blackDatabase.FindBlackInfos(ctx, req.OwnerUserID, req.UserIDList)
	if err != nil {
		return nil, err
	}

	friendMap := datautil.SliceToMap(friends, func(e *model.Friend) string {
		return e.FriendUserID
	})

	blackMap := datautil.SliceToMap(blacks, func(e *model.Black) string {
		return e.BlockUserID
	})

	resp := &relation.GetSpecifiedFriendsInfoResp{
		Infos: make([]*relation.GetSpecifiedFriendsInfoInfo, 0, len(req.UserIDList)),
	}

	for _, userID := range req.UserIDList {
		user := userMap[userID]

		if user == nil {
			continue
		}

		var friendInfo *sdkws.FriendInfo
		if friend := friendMap[userID]; friend != nil {
			friendInfo = &sdkws.FriendInfo{
				OwnerUserID:    friend.OwnerUserID,
				Remark:         friend.Remark,
				CreateTime:     friend.CreateTime.UnixMilli(),
				AddSource:      friend.AddSource,
				OperatorUserID: friend.OperatorUserID,
				Ex:             friend.Ex,
				IsPinned:       friend.IsPinned,
			}
		}

		var blackInfo *sdkws.BlackInfo
		if black := blackMap[userID]; black != nil {
			blackInfo = &sdkws.BlackInfo{
				OwnerUserID:    black.OwnerUserID,
				CreateTime:     black.CreateTime.UnixMilli(),
				AddSource:      black.AddSource,
				OperatorUserID: black.OperatorUserID,
				Ex:             black.Ex,
			}
		}

		resp.Infos = append(resp.Infos, &relation.GetSpecifiedFriendsInfoInfo{
			UserInfo:   user,
			FriendInfo: friendInfo,
			BlackInfo:  blackInfo,
		})
	}

	return resp, nil
}

func (s *friendServer) UpdateFriends(
	ctx context.Context,
	req *relation.UpdateFriendsReq,
) (*relation.UpdateFriendsResp, error) {
	if len(req.FriendUserIDs) == 0 {
		return nil, errs.ErrArgs.WrapMsg("friendIDList is empty")
	}
	if datautil.Duplicate(req.FriendUserIDs) {
		return nil, errs.ErrArgs.WrapMsg("friendIDList repeated")
	}

	if err := authverify.CheckAccessV3(ctx, req.OwnerUserID, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	_, err := s.db.FindFriendsWithError(ctx, req.OwnerUserID, req.FriendUserIDs)
	if err != nil {
		return nil, err
	}

	val := make(map[string]any)

	if req.IsPinned != nil {
		val["is_pinned"] = req.IsPinned.Value
	}
	if req.Remark != nil {
		val["remark"] = req.Remark.Value
	}
	if req.Ex != nil {
		val["ex"] = req.Ex.Value
	}
	if err = s.db.UpdateFriends(ctx, req.OwnerUserID, req.FriendUserIDs, val); err != nil {
		return nil, err
	}

	resp := &relation.UpdateFriendsResp{}

	s.notificationSender.FriendsInfoUpdateNotification(ctx, req.OwnerUserID, req.FriendUserIDs)
	return resp, nil
}

func (s *friendServer) GetSelfUnhandledApplyCount(ctx context.Context, req *relation.GetSelfUnhandledApplyCountReq) (*relation.GetSelfUnhandledApplyCountResp, error) {
	if err := authverify.CheckAccessV3(ctx, req.UserID, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	count, err := s.db.GetUnhandledCount(ctx, req.UserID, req.Time)
	if err != nil {
		return nil, err
	}

	return &relation.GetSelfUnhandledApplyCountResp{
		Count: count,
	}, nil
}

func (s *friendServer) getCommonUserMap(ctx context.Context, userIDs []string) (map[string]common_user.CommonUser, error) {
	users, err := s.userClient.GetUsersInfo(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	return datautil.SliceToMapAny(users, func(e *sdkws.UserInfo) (string, common_user.CommonUser) {
		return e.UserID, e
	}), nil
}
