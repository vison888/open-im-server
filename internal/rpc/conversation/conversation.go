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

// Package conversation 会话服务模块
//
// 本文件实现了OpenIM会话服务的核心功能，是IM系统中负责会话管理的核心组件。
// 会话是用户之间进行通信的载体，包含了聊天的元数据和配置信息。
//
// 核心功能：
// 1. 会话生命周期管理：创建、查询、更新、删除会话
// 2. 会话列表管理：排序、分页、筛选用户的会话列表
// 3. 会话属性管理：置顶、免打扰、私聊设置等
// 4. 会话同步机制：多端会话数据的一致性保证
// 5. 未读数管理：会话未读消息数量的计算和同步
// 6. 会话通知：会话状态变更的实时通知推送
//
// 技术特点：
// - 基于gRPC的微服务架构
// - MongoDB + Redis的混合存储策略
// - 支持大规模并发的会话管理
// - 完善的缓存机制提升性能
// - 版本化的增量同步机制
//
// 数据流：
// 客户端 -> gRPC接口 -> 业务逻辑层 -> 数据控制器 -> 存储层(MongoDB/Redis)
package conversation

import (
	"context"
	"sort"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/rpcli"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache/redis"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database/mgo"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	dbModel "github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/open-im-server/v3/pkg/localcache"
	"github.com/openimsdk/open-im-server/v3/pkg/msgprocessor"
	"github.com/openimsdk/tools/db/redisutil"

	"github.com/openimsdk/open-im-server/v3/pkg/common/convert"
	"github.com/openimsdk/open-im-server/v3/pkg/common/servererrs"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/controller"
	"github.com/openimsdk/protocol/constant"
	pbconversation "github.com/openimsdk/protocol/conversation"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/db/mongoutil"
	"github.com/openimsdk/tools/discovery"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/utils/datautil"
	"google.golang.org/grpc"
)

// conversationServer 会话服务器结构体
//
// 这是会话服务的核心结构，实现了所有会话相关的gRPC接口。
// 负责处理客户端的会话请求，包括会话的CRUD操作、排序、同步等功能。
//
// 架构设计：
// - 采用依赖注入模式，通过接口隔离各个组件
// - 分层架构：接口层 -> 业务逻辑层 -> 数据访问层
// - 微服务架构：通过RPC客户端调用其他服务
//
// 性能优化：
// - 使用本地缓存减少数据库访问
// - 批量操作减少网络开销
// - 异步通知机制避免阻塞
type conversationServer struct {
	// 继承gRPC服务的未实现方法，确保接口完整性
	pbconversation.UnimplementedConversationServer

	// conversationDatabase 会话数据库控制器
	// 这是数据访问层的核心组件，封装了会话数据的所有数据库操作。
	// 主要功能：
	// - MongoDB的会话数据持久化
	// - Redis的会话数据缓存
	// - 事务管理和数据一致性保证
	// - 批量操作优化
	conversationDatabase controller.ConversationDatabase

	// conversationNotificationSender 会话通知发送器
	// 负责发送会话相关的实时通知，确保多端数据同步。
	// 主要通知类型：
	// - 会话状态变更通知（置顶、免打扰等）
	// - 会话隐私设置通知
	// - 未读数变更通知
	conversationNotificationSender *ConversationNotificationSender

	// config 服务配置信息
	// 包含会话服务运行所需的所有配置参数
	config *Config

	// 依赖的其他RPC服务客户端
	// 会话服务需要与其他服务协作完成完整的业务逻辑

	// userClient 用户服务客户端
	// 用于获取用户基本信息，如昵称、头像等
	// 主要用途：
	// - 构造会话列表时填充用户信息
	// - 验证用户的有效性
	userClient *rpcli.UserClient

	// msgClient 消息服务客户端
	// 用于获取消息相关信息，如最新消息、未读数等
	// 主要用途：
	// - 获取会话的最新消息用于排序
	// - 计算会话的未读消息数量
	// - 管理用户的已读序列号
	msgClient *rpcli.MsgClient

	// groupClient 群组服务客户端
	// 用于获取群组信息，如群名称、群头像、成员数等
	// 主要用途：
	// - 构造群聊会话的显示信息
	// - 验证群组的状态（如是否已解散）
	groupClient *rpcli.GroupClient
}

// Config 会话服务配置结构体
//
// 包含了会话服务运行所需的所有配置信息，通过依赖注入的方式
// 传递给服务实例，实现了配置与代码的分离。
//
// 配置分类：
// - 核心服务配置：RPC服务端口、监听地址等
// - 存储配置：数据库连接、缓存配置等
// - 业务配置：通知策略、本地缓存等
// - 服务发现配置：注册中心配置等
type Config struct {
	RpcConfig          config.Conversation // RPC服务配置：端口、超时、中间件等
	RedisConfig        config.Redis        // Redis缓存配置：连接串、连接池、过期时间等
	MongodbConfig      config.Mongo        // MongoDB数据库配置：连接串、数据库名、集合配置等
	NotificationConfig config.Notification // 通知服务配置：推送策略、重试机制、模板配置等
	Share              config.Share        // 共享配置信息：服务注册名、公共参数等
	LocalCacheConfig   config.LocalCache   // 本地缓存配置：缓存大小、过期策略、清理策略等
	Discovery          config.Discovery    // 服务发现配置：注册中心地址、健康检查等
}

// Start 启动会话服务
//
// 这是会话服务的启动入口，负责初始化所有依赖组件并注册gRPC服务。
// 采用了依赖注入和控制反转的设计模式，确保组件之间的松耦合。
//
// 初始化流程：
// 1. 建立数据库连接（MongoDB + Redis）
// 2. 初始化数据访问层组件
// 3. 建立与其他微服务的连接
// 4. 初始化本地缓存
// 5. 创建服务实例并注册到gRPC服务器
//
// 参数：
// - ctx: 服务启动的上下文，用于控制启动过程和传递取消信号
// - config: 服务配置信息，包含所有必要的配置参数
// - client: 服务发现注册器，用于注册服务和发现其他服务
// - server: gRPC服务器实例，用于注册会话服务
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
	// MongoDB用于持久化存储会话数据，包括会话基本信息、配置等
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

	// 3. 创建会话数据库DAO（数据访问对象）
	// 封装了MongoDB的具体操作，提供了会话数据的CRUD接口
	conversationDB, err := mgo.NewConversationMongo(mgocli.GetDB())
	if err != nil {
		return err
	}

	// 4. 获取其他服务的连接
	// 通过服务发现机制获取其他微服务的连接，实现服务间通信

	// 获取用户服务连接
	userConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.User)
	if err != nil {
		return err
	}

	// 获取群组服务连接
	groupConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.Group)
	if err != nil {
		return err
	}

	// 获取消息服务连接
	msgConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.Msg)
	if err != nil {
		return err
	}

	// 5. 创建消息服务客户端
	// 用于调用消息服务的接口，获取消息相关信息
	msgClient := rpcli.NewMsgClient(msgConn)

	// 6. 初始化本地缓存
	// 本地缓存用于缓存频繁访问的数据，减少网络开销
	localcache.InitLocalCache(&config.LocalCacheConfig)

	// 7. 注册会话服务到gRPC服务器
	// 创建会话服务实例并注册，使其能够处理客户端请求
	pbconversation.RegisterConversationServer(server, &conversationServer{
		// 初始化通知发送器
		conversationNotificationSender: NewConversationNotificationSender(&config.NotificationConfig, msgClient),

		// 初始化数据库控制器
		// 组合了MongoDB DAO、Redis缓存和事务管理器
		conversationDatabase: controller.NewConversationDatabase(
			conversationDB, // MongoDB数据访问层
			redis.NewConversationRedis(rdb, &config.LocalCacheConfig, redis.GetRocksCacheOptions(), conversationDB), // Redis缓存层
			mgocli.GetTx(), // MongoDB事务管理器
		),

		// 初始化RPC客户端
		userClient:  rpcli.NewUserClient(userConn),   // 用户服务客户端
		groupClient: rpcli.NewGroupClient(groupConn), // 群组服务客户端
		msgClient:   msgClient,                       // 消息服务客户端
	})

	return nil
}

// GetConversation 获取单个会话信息
//
// 根据用户ID和会话ID查询会话的详细信息。
// 这是一个基础的查询接口，用于获取特定会话的完整配置和状态。
//
// 参数：
// - req.OwnerUserID: 会话所有者的用户ID
// - req.ConversationID: 要查询的会话ID
//
// 返回：
// - 会话的完整信息，包括配置、状态、扩展信息等
//
// 错误处理：
// - 如果会话不存在，返回ErrRecordNotFound错误
// - 如果数据库查询失败，返回相应的数据库错误
func (c *conversationServer) GetConversation(ctx context.Context, req *pbconversation.GetConversationReq) (*pbconversation.GetConversationResp, error) {
	// 从数据库查询会话信息
	// 使用批量查询接口，虽然只查询一个会话，但保持接口一致性
	conversations, err := c.conversationDatabase.FindConversations(ctx, req.OwnerUserID, []string{req.ConversationID})
	if err != nil {
		return nil, err
	}

	// 检查会话是否存在
	// 如果查询结果为空，说明会话不存在或用户无权访问
	if len(conversations) < 1 {
		return nil, errs.ErrRecordNotFound.WrapMsg("conversation not found")
	}

	// 构造响应
	// 将数据库模型转换为protobuf格式
	resp := &pbconversation.GetConversationResp{Conversation: &pbconversation.Conversation{}}
	resp.Conversation = convert.ConversationDB2Pb(conversations[0])
	return resp, nil
}

// GetSortedConversationList 获取排序后的会话列表
// 这是会话模块的核心方法，负责：
// 1. 查询用户的所有会话
// 2. 获取每个会话的最新消息和未读数
// 3. 按置顶状态和最新消息时间排序
// 4. 分页返回结果
func (c *conversationServer) GetSortedConversationList(ctx context.Context, req *pbconversation.GetSortedConversationListReq) (resp *pbconversation.GetSortedConversationListResp, err error) {
	log.ZDebug(ctx, "GetSortedConversationList", "seqs", req, "userID", req.UserID)

	var conversationIDs []string

	// 获取会话ID列表：如果请求中没有指定，则获取用户的所有会话ID
	if len(req.ConversationIDs) == 0 {
		conversationIDs, err = c.conversationDatabase.GetConversationIDs(ctx, req.UserID)
		if err != nil {
			return nil, err
		}
	} else {
		conversationIDs = req.ConversationIDs
	}

	// 批量查询会话详细信息
	conversations, err := c.conversationDatabase.FindConversations(ctx, req.UserID, conversationIDs)
	if err != nil {
		return nil, err
	}
	if len(conversations) == 0 {
		return nil, errs.ErrRecordNotFound.Wrap()
	}

	// 获取每个会话的最大序列号（用于计算未读数）
	maxSeqs, err := c.msgClient.GetMaxSeqs(ctx, conversationIDs)
	if err != nil {
		return nil, err
	}

	// 获取每个会话的最新消息
	chatLogs, err := c.msgClient.GetMsgByConversationIDs(ctx, conversationIDs, maxSeqs)
	if err != nil {
		return nil, err
	}

	// 获取会话信息（包括最新消息的详细信息）
	conversationMsg, err := c.getConversationInfo(ctx, chatLogs, req.UserID)
	if err != nil {
		return nil, err
	}

	// 获取用户在每个会话中的已读序列号
	hasReadSeqs, err := c.msgClient.GetHasReadSeqs(ctx, conversationIDs, req.UserID)
	if err != nil {
		return nil, err
	}

	// 计算未读数：未读数 = 最大序列号 - 已读序列号
	var unreadTotal int64
	conversation_unreadCount := make(map[string]int64)
	for conversationID, maxSeq := range maxSeqs {
		unreadCount := maxSeq - hasReadSeqs[conversationID]
		conversation_unreadCount[conversationID] = unreadCount
		unreadTotal += unreadCount
	}

	// 分类会话：置顶会话和非置顶会话
	conversation_isPinTime := make(map[int64]string)  // 置顶会话：时间戳->会话ID
	conversation_notPinTime := make(map[int64]string) // 非置顶会话：时间戳->会话ID

	for _, v := range conversations {
		conversationID := v.ConversationID
		// 使用最新消息的接收时间作为排序依据
		time := conversationMsg[conversationID].MsgInfo.LatestMsgRecvTime
		conversationMsg[conversationID].RecvMsgOpt = v.RecvMsgOpt

		if v.IsPinned {
			// 置顶会话
			conversationMsg[conversationID].IsPinned = v.IsPinned
			conversation_isPinTime[time] = conversationID
			continue
		}
		// 非置顶会话
		conversation_notPinTime[time] = conversationID
	}

	// 初始化响应结构
	resp = &pbconversation.GetSortedConversationListResp{
		ConversationTotal: int64(len(chatLogs)),
		ConversationElems: []*pbconversation.ConversationElem{},
		UnreadTotal:       unreadTotal,
	}

	// 先排序置顶会话，再排序非置顶会话（置顶会话优先显示）
	c.conversationSort(conversation_isPinTime, resp, conversation_unreadCount, conversationMsg)
	c.conversationSort(conversation_notPinTime, resp, conversation_unreadCount, conversationMsg)

	// 分页处理
	resp.ConversationElems = datautil.Paginate(resp.ConversationElems, int(req.Pagination.GetPageNumber()), int(req.Pagination.GetShowNumber()))
	return resp, nil
}

// GetAllConversations 获取用户的所有会话
//
// 返回用户的完整会话列表，不包含消息内容和排序。
// 这是一个简单的会话列表查询接口，主要用于管理端或特殊场景。
//
// 与GetSortedConversationList的区别：
// - 不包含最新消息信息
// - 不包含未读数统计
// - 不进行排序处理
// - 性能更高，适合批量操作
//
// 参数：
// - req.OwnerUserID: 会话所有者的用户ID
//
// 返回：
// - 用户的所有会话基本信息列表
//
// 使用场景：
// - 管理端查询用户会话
// - 数据导出和备份
// - 批量会话操作的前置查询
func (c *conversationServer) GetAllConversations(ctx context.Context, req *pbconversation.GetAllConversationsReq) (*pbconversation.GetAllConversationsResp, error) {
	// 查询用户的所有会话
	// 直接从数据库获取，不经过缓存和复杂的业务逻辑处理
	conversations, err := c.conversationDatabase.GetUserAllConversation(ctx, req.OwnerUserID)
	if err != nil {
		return nil, err
	}

	// 构造响应
	// 批量转换数据库模型为protobuf格式
	resp := &pbconversation.GetAllConversationsResp{Conversations: []*pbconversation.Conversation{}}
	resp.Conversations = convert.ConversationsDB2Pb(conversations)
	return resp, nil
}

// GetConversations 批量获取会话信息
//
// 根据会话ID列表批量查询会话详细信息。
// 这是一个高效的批量查询接口，避免多次单独查询的开销。
//
// 参数：
// - req.OwnerUserID: 会话所有者的用户ID
// - req.ConversationIDs: 要查询的会话ID列表
//
// 返回：
// - 会话信息列表，按查询顺序返回
//
// 特点：
// - 支持批量查询，提高性能
// - 只返回用户有权访问的会话
// - 如果某个会话不存在，不会影响其他会话的查询
func (c *conversationServer) GetConversations(ctx context.Context, req *pbconversation.GetConversationsReq) (*pbconversation.GetConversationsResp, error) {
	// 调用内部方法进行批量查询
	conversations, err := c.getConversations(ctx, req.OwnerUserID, req.ConversationIDs)
	if err != nil {
		return nil, err
	}
	return &pbconversation.GetConversationsResp{
		Conversations: conversations,
	}, nil
}

// getConversations 内部方法：批量获取会话信息
//
// 这是一个内部辅助方法，被多个公开接口复用。
// 负责实际的数据库查询和格式转换逻辑。
//
// 参数：
// - ctx: 请求上下文
// - ownerUserID: 会话所有者的用户ID
// - conversationIDs: 要查询的会话ID列表
//
// 返回：
// - protobuf格式的会话信息列表
//
// 设计目的：
// - 代码复用，避免重复实现
// - 统一的数据查询和转换逻辑
// - 便于维护和测试
func (c *conversationServer) getConversations(ctx context.Context, ownerUserID string, conversationIDs []string) ([]*pbconversation.Conversation, error) {
	// 从数据库批量查询会话信息
	conversations, err := c.conversationDatabase.FindConversations(ctx, ownerUserID, conversationIDs)
	if err != nil {
		return nil, err
	}

	// 将数据库模型转换为protobuf格式
	// 注意：这里创建了临时的响应对象，但实际只使用转换结果
	resp := &pbconversation.GetConversationsResp{Conversations: []*pbconversation.Conversation{}}
	resp.Conversations = convert.ConversationsDB2Pb(conversations)
	return convert.ConversationsDB2Pb(conversations), nil
}

// SetConversation 设置单个会话信息
//
// 更新指定会话的配置信息，包括置顶、免打扰、扩展信息等。
// 这是一个简单的单会话设置接口，适用于单个会话的快速配置。
//
// 参数：
// - req.Conversation: 要设置的会话信息，包含所有需要更新的字段
//
// 功能特点：
// - 支持会话的所有可配置属性
// - 自动发送变更通知，确保多端同步
// - 使用事务保证数据一致性
//
// 通知机制：
// - 设置成功后自动发送ConversationChangeNotification
// - 通知会话所有者，触发客户端数据刷新
//
// 使用场景：
// - 用户手动设置会话属性
// - 单个会话的快速配置
// - 简单的会话管理操作
func (c *conversationServer) SetConversation(ctx context.Context, req *pbconversation.SetConversationReq) (*pbconversation.SetConversationResp, error) {
	// 将protobuf格式转换为数据库模型
	var conversation dbModel.Conversation
	if err := datautil.CopyStructFields(&conversation, req.Conversation); err != nil {
		return nil, err
	}

	// 更新会话信息到数据库
	// 使用批量接口，虽然只更新一个会话，但保持接口一致性
	err := c.conversationDatabase.SetUserConversations(ctx, req.Conversation.OwnerUserID, []*dbModel.Conversation{&conversation})
	if err != nil {
		return nil, err
	}

	// 发送会话变更通知
	// 确保多端数据同步，客户端能及时感知到会话状态变化
	c.conversationNotificationSender.ConversationChangeNotification(ctx, req.Conversation.OwnerUserID, []string{req.Conversation.ConversationID})

	resp := &pbconversation.SetConversationResp{}
	return resp, nil
}

func (c *conversationServer) SetConversations(ctx context.Context, req *pbconversation.SetConversationsReq) (*pbconversation.SetConversationsResp, error) {
	if req.Conversation == nil {
		return nil, errs.ErrArgs.WrapMsg("conversation must not be nil")
	}

	if req.Conversation.ConversationType == constant.WriteGroupChatType {
		groupInfo, err := c.groupClient.GetGroupInfo(ctx, req.Conversation.GroupID)
		if err != nil {
			return nil, err
		}
		if groupInfo.Status == constant.GroupStatusDismissed {
			return nil, servererrs.ErrDismissedAlready.WrapMsg("group dismissed")
		}
	}

	conversationMap := make(map[string]*dbModel.Conversation)
	var needUpdateUsersList []string

	for _, userID := range req.UserIDs {
		conversationList, err := c.conversationDatabase.FindConversations(ctx, userID, []string{req.Conversation.ConversationID})
		if err != nil {
			return nil, err
		}
		if len(conversationList) != 0 {
			conversationMap[userID] = conversationList[0]
		} else {
			needUpdateUsersList = append(needUpdateUsersList, userID)
		}
	}

	var conversation dbModel.Conversation
	conversation.ConversationID = req.Conversation.ConversationID
	conversation.ConversationType = req.Conversation.ConversationType
	conversation.UserID = req.Conversation.UserID
	conversation.GroupID = req.Conversation.GroupID

	m := make(map[string]any)

	setConversationFieldsFunc := func() {
		if req.Conversation.RecvMsgOpt != nil {
			conversation.RecvMsgOpt = req.Conversation.RecvMsgOpt.Value
			m["recv_msg_opt"] = req.Conversation.RecvMsgOpt.Value
		}
		if req.Conversation.AttachedInfo != nil {
			conversation.AttachedInfo = req.Conversation.AttachedInfo.Value
			m["attached_info"] = req.Conversation.AttachedInfo.Value
		}
		if req.Conversation.Ex != nil {
			conversation.Ex = req.Conversation.Ex.Value
			m["ex"] = req.Conversation.Ex.Value
		}
		if req.Conversation.IsPinned != nil {
			conversation.IsPinned = req.Conversation.IsPinned.Value
			m["is_pinned"] = req.Conversation.IsPinned.Value
		}
		if req.Conversation.GroupAtType != nil {
			conversation.GroupAtType = req.Conversation.GroupAtType.Value
			m["group_at_type"] = req.Conversation.GroupAtType.Value
		}
		if req.Conversation.MsgDestructTime != nil {
			conversation.MsgDestructTime = req.Conversation.MsgDestructTime.Value
			m["msg_destruct_time"] = req.Conversation.MsgDestructTime.Value
		}
		if req.Conversation.IsMsgDestruct != nil {
			conversation.IsMsgDestruct = req.Conversation.IsMsgDestruct.Value
			m["is_msg_destruct"] = req.Conversation.IsMsgDestruct.Value
		}
		if req.Conversation.BurnDuration != nil {
			conversation.BurnDuration = req.Conversation.BurnDuration.Value
			m["burn_duration"] = req.Conversation.BurnDuration.Value
		}
	}

	// set need set field in conversation
	setConversationFieldsFunc()

	for userID := range conversationMap {
		unequal := len(m)

		if req.Conversation.RecvMsgOpt != nil {
			if req.Conversation.RecvMsgOpt.Value == conversationMap[userID].RecvMsgOpt {
				unequal--
			}
		}

		if req.Conversation.AttachedInfo != nil {
			if req.Conversation.AttachedInfo.Value == conversationMap[userID].AttachedInfo {
				unequal--
			}
		}

		if req.Conversation.Ex != nil {
			if req.Conversation.Ex.Value == conversationMap[userID].Ex {
				unequal--
			}
		}
		if req.Conversation.IsPinned != nil {
			if req.Conversation.IsPinned.Value == conversationMap[userID].IsPinned {
				unequal--
			}
		}

		if req.Conversation.GroupAtType != nil {
			if req.Conversation.GroupAtType.Value == conversationMap[userID].GroupAtType {
				unequal--
			}
		}

		if req.Conversation.MsgDestructTime != nil {
			if req.Conversation.MsgDestructTime.Value == conversationMap[userID].MsgDestructTime {
				unequal--
			}
		}

		if req.Conversation.IsMsgDestruct != nil {
			if req.Conversation.IsMsgDestruct.Value == conversationMap[userID].IsMsgDestruct {
				unequal--
			}
		}

		if req.Conversation.BurnDuration != nil {
			if req.Conversation.BurnDuration.Value == conversationMap[userID].BurnDuration {
				unequal--
			}
		}

		if unequal > 0 {
			needUpdateUsersList = append(needUpdateUsersList, userID)
		}
	}
	if len(m) != 0 && len(needUpdateUsersList) != 0 {
		if err := c.conversationDatabase.SetUsersConversationFieldTx(ctx, needUpdateUsersList, &conversation, m); err != nil {
			return nil, err
		}

		for _, v := range needUpdateUsersList {
			c.conversationNotificationSender.ConversationChangeNotification(ctx, v, []string{req.Conversation.ConversationID})
		}
	}
	if req.Conversation.IsPrivateChat != nil && req.Conversation.ConversationType != constant.ReadGroupChatType {
		var conversations []*dbModel.Conversation
		for _, ownerUserID := range req.UserIDs {
			transConversation := conversation
			transConversation.OwnerUserID = ownerUserID
			transConversation.IsPrivateChat = req.Conversation.IsPrivateChat.Value
			conversations = append(conversations, &transConversation)
		}

		if err := c.conversationDatabase.SyncPeerUserPrivateConversationTx(ctx, conversations); err != nil {
			return nil, err
		}

		for _, userID := range req.UserIDs {
			c.conversationNotificationSender.ConversationSetPrivateNotification(ctx, userID, req.Conversation.UserID,
				req.Conversation.IsPrivateChat.Value, req.Conversation.ConversationID)
		}
	}

	return &pbconversation.SetConversationsResp{}, nil
}

// Get user IDs with "Do Not Disturb" enabled in super large groups.
func (c *conversationServer) GetRecvMsgNotNotifyUserIDs(ctx context.Context, req *pbconversation.GetRecvMsgNotNotifyUserIDsReq) (*pbconversation.GetRecvMsgNotNotifyUserIDsResp, error) {
	return nil, errs.New("deprecated")
}

// create conversation without notification for msg redis transfer.
// CreateSingleChatConversations 创建单聊会话
//
// 这是一个内部方法，用于在消息传输过程中自动创建单聊会话。
// 当用户首次发送消息时，如果会话不存在，系统会自动调用此方法创建会话。
//
// 设计特点：
// - 不发送通知，避免在消息传输过程中产生额外的通知开销
// - 支持单聊和通知类型的会话创建
// - 自动为双方用户创建对应的会话记录
//
// 参数：
// - req.ConversationType: 会话类型（单聊或通知）
// - req.ConversationID: 会话ID
// - req.SendID: 发送者用户ID
// - req.RecvID: 接收者用户ID
//
// 会话创建规则：
// 1. 单聊类型：为发送者和接收者各创建一个会话记录
// 2. 通知类型：只为接收者创建会话记录
//
// 错误处理：
// - 创建失败时只记录警告日志，不中断流程
// - 这种设计确保消息发送不会因为会话创建失败而中断
func (c *conversationServer) CreateSingleChatConversations(ctx context.Context,
	req *pbconversation.CreateSingleChatConversationsReq,
) (*pbconversation.CreateSingleChatConversationsResp, error) {
	switch req.ConversationType {
	case constant.SingleChatType:
		// 为发送者创建会话记录
		var conversation dbModel.Conversation
		conversation.ConversationID = req.ConversationID
		conversation.ConversationType = req.ConversationType
		conversation.OwnerUserID = req.SendID // 会话所有者是发送者
		conversation.UserID = req.RecvID      // 会话对方是接收者
		err := c.conversationDatabase.CreateConversation(ctx, []*dbModel.Conversation{&conversation})
		if err != nil {
			log.ZWarn(ctx, "create conversation failed", err, "conversation", conversation)
		}

		// 为接收者创建会话记录
		conversation2 := conversation
		conversation2.OwnerUserID = req.RecvID // 会话所有者是接收者
		conversation2.UserID = req.SendID      // 会话对方是发送者
		err = c.conversationDatabase.CreateConversation(ctx, []*dbModel.Conversation{&conversation2})
		if err != nil {
			log.ZWarn(ctx, "create conversation failed", err, "conversation2", conversation)
		}
	case constant.NotificationChatType:
		// 通知类型会话只为接收者创建
		var conversation dbModel.Conversation
		conversation.ConversationID = req.ConversationID
		conversation.ConversationType = req.ConversationType
		conversation.OwnerUserID = req.RecvID // 会话所有者是接收者
		conversation.UserID = req.SendID      // 会话对方是发送者（通常是系统）
		err := c.conversationDatabase.CreateConversation(ctx, []*dbModel.Conversation{&conversation})
		if err != nil {
			log.ZWarn(ctx, "create conversation failed", err, "conversation2", conversation)
		}
	}

	return &pbconversation.CreateSingleChatConversationsResp{}, nil
}

func (c *conversationServer) CreateGroupChatConversations(ctx context.Context, req *pbconversation.CreateGroupChatConversationsReq) (*pbconversation.CreateGroupChatConversationsResp, error) {
	err := c.conversationDatabase.CreateGroupChatConversation(ctx, req.GroupID, req.UserIDs)
	if err != nil {
		return nil, err
	}
	conversationID := msgprocessor.GetConversationIDBySessionType(constant.ReadGroupChatType, req.GroupID)
	if err := c.msgClient.SetUserConversationMaxSeq(ctx, conversationID, req.UserIDs, 0); err != nil {
		return nil, err
	}
	return &pbconversation.CreateGroupChatConversationsResp{}, nil
}

func (c *conversationServer) SetConversationMaxSeq(ctx context.Context, req *pbconversation.SetConversationMaxSeqReq) (*pbconversation.SetConversationMaxSeqResp, error) {
	if err := c.msgClient.SetUserConversationMaxSeq(ctx, req.ConversationID, req.OwnerUserID, req.MaxSeq); err != nil {
		return nil, err
	}
	if err := c.conversationDatabase.UpdateUsersConversationField(ctx, req.OwnerUserID, req.ConversationID,
		map[string]any{"max_seq": req.MaxSeq}); err != nil {
		return nil, err
	}
	for _, userID := range req.OwnerUserID {
		c.conversationNotificationSender.ConversationChangeNotification(ctx, userID, []string{req.ConversationID})
	}
	return &pbconversation.SetConversationMaxSeqResp{}, nil
}

func (c *conversationServer) SetConversationMinSeq(ctx context.Context, req *pbconversation.SetConversationMinSeqReq) (*pbconversation.SetConversationMinSeqResp, error) {
	if err := c.msgClient.SetUserConversationMin(ctx, req.ConversationID, req.OwnerUserID, req.MinSeq); err != nil {
		return nil, err
	}
	if err := c.conversationDatabase.UpdateUsersConversationField(ctx, req.OwnerUserID, req.ConversationID,
		map[string]any{"min_seq": req.MinSeq}); err != nil {
		return nil, err
	}
	for _, userID := range req.OwnerUserID {
		c.conversationNotificationSender.ConversationChangeNotification(ctx, userID, []string{req.ConversationID})
	}
	return &pbconversation.SetConversationMinSeqResp{}, nil
}

func (c *conversationServer) GetConversationIDs(ctx context.Context, req *pbconversation.GetConversationIDsReq) (*pbconversation.GetConversationIDsResp, error) {
	conversationIDs, err := c.conversationDatabase.GetConversationIDs(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	return &pbconversation.GetConversationIDsResp{ConversationIDs: conversationIDs}, nil
}

func (c *conversationServer) GetUserConversationIDsHash(ctx context.Context, req *pbconversation.GetUserConversationIDsHashReq) (*pbconversation.GetUserConversationIDsHashResp, error) {
	hash, err := c.conversationDatabase.GetUserConversationIDsHash(ctx, req.OwnerUserID)
	if err != nil {
		return nil, err
	}
	return &pbconversation.GetUserConversationIDsHashResp{Hash: hash}, nil
}

func (c *conversationServer) GetConversationsByConversationID(
	ctx context.Context,
	req *pbconversation.GetConversationsByConversationIDReq,
) (*pbconversation.GetConversationsByConversationIDResp, error) {
	conversations, err := c.conversationDatabase.GetConversationsByConversationID(ctx, req.ConversationIDs)
	if err != nil {
		return nil, err
	}
	return &pbconversation.GetConversationsByConversationIDResp{Conversations: convert.ConversationsDB2Pb(conversations)}, nil
}

func (c *conversationServer) GetConversationOfflinePushUserIDs(ctx context.Context, req *pbconversation.GetConversationOfflinePushUserIDsReq) (*pbconversation.GetConversationOfflinePushUserIDsResp, error) {
	if req.ConversationID == "" {
		return nil, errs.ErrArgs.WrapMsg("conversationID is empty")
	}
	if len(req.UserIDs) == 0 {
		return &pbconversation.GetConversationOfflinePushUserIDsResp{}, nil
	}
	userIDs, err := c.conversationDatabase.GetConversationNotReceiveMessageUserIDs(ctx, req.ConversationID)
	if err != nil {
		return nil, err
	}
	if len(userIDs) == 0 {
		return &pbconversation.GetConversationOfflinePushUserIDsResp{UserIDs: req.UserIDs}, nil
	}
	userIDSet := make(map[string]struct{})
	for _, userID := range req.UserIDs {
		userIDSet[userID] = struct{}{}
	}
	for _, userID := range userIDs {
		delete(userIDSet, userID)
	}
	return &pbconversation.GetConversationOfflinePushUserIDsResp{UserIDs: datautil.Keys(userIDSet)}, nil
}

// conversationSort 会话排序处理方法
//
// 这是会话列表排序的核心方法，负责将会话按时间倒序排列。
// 支持置顶会话和普通会话的分别排序，置顶会话始终显示在前面。
//
// 排序规则：
// 1. 置顶会话优先显示
// 2. 在同一类别内，按最新消息时间倒序排列
// 3. 时间越新的会话排在越前面
//
// 参数：
// - conversations: 时间戳到会话ID的映射（key: 时间戳, value: 会话ID）
// - resp: 响应对象，排序结果会追加到其ConversationElems字段
// - conversation_unreadCount: 会话未读数映射
// - conversationMsg: 会话消息信息映射
//
// 处理流程：
// 1. 提取所有时间戳并排序
// 2. 按排序后的时间戳顺序构造会话列表
// 3. 为每个会话设置未读数
// 4. 将结果追加到响应对象
//
// 设计特点：
// - 支持增量追加，可以先处理置顶会话，再处理普通会话
// - 时间复杂度O(n log n)，适合大量会话的排序
// - 内存友好，避免不必要的数据复制
func (c *conversationServer) conversationSort(conversations map[int64]string, resp *pbconversation.GetSortedConversationListResp, conversation_unreadCount map[string]int64, conversationMsg map[string]*pbconversation.ConversationElem) {
	// 提取所有时间戳键值
	keys := []int64{}
	for key := range conversations {
		keys = append(keys, key)
	}

	// 按时间戳倒序排序（最新的在前面）
	sort.Slice(keys, func(i, j int) bool {
		return keys[i] > keys[j]
	})

	// 初始化索引计数器
	index := 0

	// 预分配会话元素切片，提高性能
	cons := make([]*pbconversation.ConversationElem, len(conversations))

	// 按排序后的时间戳顺序构造会话列表
	for _, v := range keys {
		conversationID := conversations[v]                                      // 获取会话ID
		conversationElem := conversationMsg[conversationID]                     // 获取会话消息信息
		conversationElem.UnreadCount = conversation_unreadCount[conversationID] // 设置未读数
		cons[index] = conversationElem                                          // 添加到结果列表
		index++
	}

	// 将排序结果追加到响应对象
	// 这种设计允许先处理置顶会话，再处理普通会话
	resp.ConversationElems = append(resp.ConversationElems, cons...)
}

// getConversationInfo 构造会话信息
//
// 这是会话列表构造的核心方法，负责将消息数据转换为会话显示信息。
// 它会批量获取用户和群组信息，然后构造完整的会话元素。
//
// 主要功能：
// 1. 分析消息数据，提取需要查询的用户ID和群组ID
// 2. 批量查询用户信息和群组信息
// 3. 根据会话类型构造不同的显示信息
// 4. 返回完整的会话元素映射
//
// 参数：
// - ctx: 请求上下文
// - chatLogs: 会话ID到最新消息的映射
// - userID: 当前查询的用户ID
//
// 返回：
// - 会话ID到会话元素的映射，包含显示所需的所有信息
//
// 性能优化：
// - 批量查询用户和群组信息，避免N+1查询问题
// - 使用映射缓存查询结果，避免重复查询
// - 按会话类型分别处理，提高处理效率
func (c *conversationServer) getConversationInfo(
	ctx context.Context,
	chatLogs map[string]*sdkws.MsgData,
	userID string) (map[string]*pbconversation.ConversationElem, error) {
	var (
		sendIDs         []string                                            // 需要查询的用户ID列表
		groupIDs        []string                                            // 需要查询的群组ID列表
		sendMap         = make(map[string]*sdkws.UserInfo)                  // 用户ID到用户信息的映射
		groupMap        = make(map[string]*sdkws.GroupInfo)                 // 群组ID到群组信息的映射
		conversationMsg = make(map[string]*pbconversation.ConversationElem) // 会话ID到会话元素的映射
	)

	// 第一步：分析消息数据，收集需要查询的用户ID和群组ID
	for _, chatLog := range chatLogs {
		switch chatLog.SessionType {
		case constant.SingleChatType:
			// 单聊：需要查询对方用户的信息
			if chatLog.SendID == userID {
				// 如果当前用户是发送者，查询接收者信息
				sendIDs = append(sendIDs, chatLog.RecvID)
			}
			// 总是查询发送者信息（用于显示最新消息的发送者）
			sendIDs = append(sendIDs, chatLog.SendID)
		case constant.WriteGroupChatType, constant.ReadGroupChatType:
			// 群聊：需要查询群组信息和发送者信息
			groupIDs = append(groupIDs, chatLog.GroupID)
			sendIDs = append(sendIDs, chatLog.SendID)
		}
	}

	// 第二步：批量查询用户信息
	if len(sendIDs) != 0 {
		sendInfos, err := c.userClient.GetUsersInfo(ctx, sendIDs)
		if err != nil {
			return nil, err
		}
		// 构建用户ID到用户信息的映射，便于后续快速查找
		for _, sendInfo := range sendInfos {
			sendMap[sendInfo.UserID] = sendInfo
		}
	}

	// 第三步：批量查询群组信息
	if len(groupIDs) != 0 {
		groupInfos, err := c.groupClient.GetGroupsInfo(ctx, groupIDs)
		if err != nil {
			return nil, err
		}
		// 构建群组ID到群组信息的映射，便于后续快速查找
		for _, groupInfo := range groupInfos {
			groupMap[groupInfo.GroupID] = groupInfo
		}
	}

	// 第四步：构造会话元素
	for conversationID, chatLog := range chatLogs {
		pbchatLog := &pbconversation.ConversationElem{}
		msgInfo := &pbconversation.MsgInfo{}

		// 复制消息基本信息
		if err := datautil.CopyStructFields(msgInfo, chatLog); err != nil {
			return nil, err
		}

		// 根据会话类型设置不同的显示信息
		switch chatLog.SessionType {
		case constant.SingleChatType:
			// 单聊：显示对方用户的头像和昵称
			if chatLog.SendID == userID {
				// 当前用户是发送者，显示接收者信息
				if recv, ok := sendMap[chatLog.RecvID]; ok {
					msgInfo.FaceURL = recv.FaceURL     // 对方头像
					msgInfo.SenderName = recv.Nickname // 对方昵称
				}
				break
			}
			// 当前用户是接收者，显示发送者信息
			if send, ok := sendMap[chatLog.SendID]; ok {
				msgInfo.FaceURL = send.FaceURL     // 发送者头像
				msgInfo.SenderName = send.Nickname // 发送者昵称
			}
		case constant.WriteGroupChatType, constant.ReadGroupChatType:
			// 群聊：显示群组信息和最新消息发送者信息
			msgInfo.GroupID = chatLog.GroupID
			if group, ok := groupMap[chatLog.GroupID]; ok {
				msgInfo.GroupName = group.GroupName          // 群名称
				msgInfo.GroupFaceURL = group.FaceURL         // 群头像
				msgInfo.GroupMemberCount = group.MemberCount // 群成员数
				msgInfo.GroupType = group.GroupType          // 群类型
			}
			// 设置最新消息发送者的昵称
			if send, ok := sendMap[chatLog.SendID]; ok {
				msgInfo.SenderName = send.Nickname
			}
		}

		// 设置会话基本信息
		pbchatLog.ConversationID = conversationID
		msgInfo.LatestMsgRecvTime = chatLog.SendTime // 最新消息接收时间
		pbchatLog.MsgInfo = msgInfo
		conversationMsg[conversationID] = pbchatLog
	}

	return conversationMsg, nil
}

func (c *conversationServer) GetConversationNotReceiveMessageUserIDs(ctx context.Context, req *pbconversation.GetConversationNotReceiveMessageUserIDsReq) (*pbconversation.GetConversationNotReceiveMessageUserIDsResp, error) {
	userIDs, err := c.conversationDatabase.GetConversationNotReceiveMessageUserIDs(ctx, req.ConversationID)
	if err != nil {
		return nil, err
	}
	return &pbconversation.GetConversationNotReceiveMessageUserIDsResp{UserIDs: userIDs}, nil
}

func (c *conversationServer) UpdateConversation(ctx context.Context, req *pbconversation.UpdateConversationReq) (*pbconversation.UpdateConversationResp, error) {
	m := make(map[string]any)
	if req.RecvMsgOpt != nil {
		m["recv_msg_opt"] = req.RecvMsgOpt.Value
	}
	if req.AttachedInfo != nil {
		m["attached_info"] = req.AttachedInfo.Value
	}
	if req.Ex != nil {
		m["ex"] = req.Ex.Value
	}
	if req.IsPinned != nil {
		m["is_pinned"] = req.IsPinned.Value
	}
	if req.GroupAtType != nil {
		m["group_at_type"] = req.GroupAtType.Value
	}
	if req.MsgDestructTime != nil {
		m["msg_destruct_time"] = req.MsgDestructTime.Value
	}
	if req.IsMsgDestruct != nil {
		m["is_msg_destruct"] = req.IsMsgDestruct.Value
	}
	if req.BurnDuration != nil {
		m["burn_duration"] = req.BurnDuration.Value
	}
	if req.IsPrivateChat != nil {
		m["is_private_chat"] = req.IsPrivateChat.Value
	}
	if req.MinSeq != nil {
		m["min_seq"] = req.MinSeq.Value
	}
	if req.MaxSeq != nil {
		m["max_seq"] = req.MaxSeq.Value
	}
	if req.LatestMsgDestructTime != nil {
		m["latest_msg_destruct_time"] = time.UnixMilli(req.LatestMsgDestructTime.Value)
	}
	if len(m) > 0 {
		if err := c.conversationDatabase.UpdateUsersConversationField(ctx, req.UserIDs, req.ConversationID, m); err != nil {
			return nil, err
		}
	}
	return &pbconversation.UpdateConversationResp{}, nil
}

func (c *conversationServer) GetOwnerConversation(ctx context.Context, req *pbconversation.GetOwnerConversationReq) (*pbconversation.GetOwnerConversationResp, error) {
	total, conversations, err := c.conversationDatabase.GetOwnerConversation(ctx, req.UserID, req.Pagination)
	if err != nil {
		return nil, err
	}
	return &pbconversation.GetOwnerConversationResp{
		Total:         total,
		Conversations: convert.ConversationsDB2Pb(conversations),
	}, nil
}

func (c *conversationServer) GetConversationsNeedClearMsg(ctx context.Context, _ *pbconversation.GetConversationsNeedClearMsgReq) (*pbconversation.GetConversationsNeedClearMsgResp, error) {
	num, err := c.conversationDatabase.GetAllConversationIDsNumber(ctx)
	if err != nil {
		log.ZError(ctx, "GetAllConversationIDsNumber failed", err)
		return nil, err
	}
	const batchNum = 100

	if num == 0 {
		return nil, errs.New("Need Destruct Msg is nil").Wrap()
	}

	maxPage := (num + batchNum - 1) / batchNum

	temp := make([]*model.Conversation, 0, maxPage*batchNum)

	for pageNumber := 0; pageNumber < int(maxPage); pageNumber++ {
		pagination := &sdkws.RequestPagination{
			PageNumber: int32(pageNumber),
			ShowNumber: batchNum,
		}

		conversationIDs, err := c.conversationDatabase.PageConversationIDs(ctx, pagination)
		if err != nil {
			log.ZError(ctx, "PageConversationIDs failed", err, "pageNumber", pageNumber)
			continue
		}

		// log.ZDebug(ctx, "PageConversationIDs success", "pageNumber", pageNumber, "conversationIDsNum", len(conversationIDs), "conversationIDs", conversationIDs)
		if len(conversationIDs) == 0 {
			continue
		}

		conversations, err := c.conversationDatabase.GetConversationsByConversationID(ctx, conversationIDs)
		if err != nil {
			log.ZError(ctx, "GetConversationsByConversationID failed", err, "conversationIDs", conversationIDs)
			continue
		}

		for _, conversation := range conversations {
			if conversation.IsMsgDestruct && conversation.MsgDestructTime != 0 && ((time.Now().UnixMilli() > (conversation.MsgDestructTime + conversation.LatestMsgDestructTime.UnixMilli() + 8*60*60)) || // 8*60*60 is UTC+8
				conversation.LatestMsgDestructTime.IsZero()) {
				temp = append(temp, conversation)
			}
		}
	}

	return &pbconversation.GetConversationsNeedClearMsgResp{Conversations: convert.ConversationsDB2Pb(temp)}, nil
}

func (c *conversationServer) GetNotNotifyConversationIDs(ctx context.Context, req *pbconversation.GetNotNotifyConversationIDsReq) (*pbconversation.GetNotNotifyConversationIDsResp, error) {
	conversationIDs, err := c.conversationDatabase.GetNotNotifyConversationIDs(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	return &pbconversation.GetNotNotifyConversationIDsResp{ConversationIDs: conversationIDs}, nil
}

func (c *conversationServer) GetPinnedConversationIDs(ctx context.Context, req *pbconversation.GetPinnedConversationIDsReq) (*pbconversation.GetPinnedConversationIDsResp, error) {
	conversationIDs, err := c.conversationDatabase.GetPinnedConversationIDs(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	return &pbconversation.GetPinnedConversationIDsResp{ConversationIDs: conversationIDs}, nil
}

func (c *conversationServer) ClearUserConversationMsg(ctx context.Context, req *pbconversation.ClearUserConversationMsgReq) (*pbconversation.ClearUserConversationMsgResp, error) {
	conversations, err := c.conversationDatabase.FindRandConversation(ctx, req.Timestamp, int(req.Limit))
	if err != nil {
		return nil, err
	}
	latestMsgDestructTime := time.UnixMilli(req.Timestamp)
	for i, conversation := range conversations {
		if conversation.IsMsgDestruct == false || conversation.MsgDestructTime == 0 {
			continue
		}
		seq, err := c.msgClient.GetLastMessageSeqByTime(ctx, conversation.ConversationID, req.Timestamp-(conversation.MsgDestructTime*1000))
		if err != nil {
			return nil, err
		}
		if seq <= 0 {
			log.ZDebug(ctx, "ClearUserConversationMsg GetLastMessageSeqByTime seq <= 0", "index", i, "conversationID", conversation.ConversationID, "ownerUserID", conversation.OwnerUserID, "msgDestructTime", conversation.MsgDestructTime, "seq", seq)
			if err := c.setConversationMinSeqAndLatestMsgDestructTime(ctx, conversation.ConversationID, conversation.OwnerUserID, -1, latestMsgDestructTime); err != nil {
				return nil, err
			}
			continue
		}
		seq++
		if err := c.setConversationMinSeqAndLatestMsgDestructTime(ctx, conversation.ConversationID, conversation.OwnerUserID, seq, latestMsgDestructTime); err != nil {
			return nil, err
		}
		log.ZDebug(ctx, "ClearUserConversationMsg set min seq", "index", i, "conversationID", conversation.ConversationID, "ownerUserID", conversation.OwnerUserID, "seq", seq, "msgDestructTime", conversation.MsgDestructTime)
	}
	return &pbconversation.ClearUserConversationMsgResp{Count: int32(len(conversations))}, nil
}

func (c *conversationServer) setConversationMinSeqAndLatestMsgDestructTime(ctx context.Context, conversationID string, ownerUserID string, minSeq int64, latestMsgDestructTime time.Time) error {
	update := map[string]any{
		"latest_msg_destruct_time": latestMsgDestructTime,
	}
	if minSeq >= 0 {
		if err := c.msgClient.SetUserConversationMin(ctx, conversationID, []string{ownerUserID}, minSeq); err != nil {
			return err
		}
		update["min_seq"] = minSeq
	}

	if err := c.conversationDatabase.UpdateUsersConversationField(ctx, []string{ownerUserID}, conversationID, update); err != nil {
		return err
	}
	c.conversationNotificationSender.ConversationChangeNotification(ctx, ownerUserID, []string{conversationID})
	return nil
}
