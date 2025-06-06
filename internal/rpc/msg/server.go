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

package msg

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/notification"
	"github.com/openimsdk/open-im-server/v3/pkg/rpcli"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache/redis"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database/mgo"
	"github.com/openimsdk/open-im-server/v3/pkg/common/webhook"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/db/mongoutil"
	"github.com/openimsdk/tools/db/redisutil"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/controller"
	"github.com/openimsdk/open-im-server/v3/pkg/rpccache"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/conversation"
	"github.com/openimsdk/protocol/msg"
	"github.com/openimsdk/tools/discovery"
	"google.golang.org/grpc"
)

// MessageInterceptorFunc 消息拦截器函数类型定义
// 用于在消息处理过程中执行自定义逻辑，支持消息的预处理、后处理等
type MessageInterceptorFunc func(ctx context.Context, globalConfig *Config, req *msg.SendMsgReq) (*sdkws.MsgData, error)

// MessageInterceptorChain 消息拦截器链定义
// 支持多个拦截器的链式调用，实现责任链模式
type MessageInterceptorChain []MessageInterceptorFunc

// Config 消息服务配置结构体
// 包含消息服务运行所需的所有配置信息
type Config struct {
	RpcConfig          config.Msg          // RPC服务配置
	RedisConfig        config.Redis        // Redis缓存配置
	MongodbConfig      config.Mongo        // MongoDB数据库配置
	KafkaConfig        config.Kafka        // Kafka消息队列配置
	NotificationConfig config.Notification // 通知服务配置
	Share              config.Share        // 共享配置信息
	WebhooksConfig     config.Webhooks     // Webhook回调配置
	LocalCacheConfig   config.LocalCache   // 本地缓存配置
	Discovery          config.Discovery    // 服务发现配置
}

// msgServer 消息服务器结构体
// 这是消息模块的核心结构，封装了消息处理所需的所有依赖和功能
type msgServer struct {
	msg.UnimplementedMsgServer

	// RegisterCenter 服务发现注册中心
	// 用于服务注册和发现，支持集群部署和负载均衡
	RegisterCenter discovery.SvcDiscoveryRegistry

	// MsgDatabase 消息数据库接口
	// 封装了消息的存储、查询、同步等数据库操作
	MsgDatabase controller.CommonMsgDatabase

	// 多层缓存系统：本地缓存 + Redis缓存，提升查询性能
	UserLocalCache         *rpccache.UserLocalCache         // 用户信息缓存
	FriendLocalCache       *rpccache.FriendLocalCache       // 好友关系缓存
	GroupLocalCache        *rpccache.GroupLocalCache        // 群组信息缓存
	ConversationLocalCache *rpccache.ConversationLocalCache // 会话信息缓存

	// Handlers 消息处理拦截器链
	// 实现责任链模式，支持消息的预处理、验证、转换等
	Handlers MessageInterceptorChain

	// 通知发送器：用于发送各种类型的通知消息
	notificationSender    *notification.NotificationSender // 通用通知发送器
	msgNotificationSender *MsgNotificationSender           // 消息相关通知发送器

	// config 全局配置信息
	config *Config

	// webhookClient Webhook客户端
	// 用于调用第三方系统的回调接口，支持消息处理的扩展
	webhookClient *webhook.Client

	// conversationClient 会话服务客户端
	// 用于处理消息发送过程中的会话相关操作
	conversationClient *rpcli.ConversationClient
}

// addInterceptorHandler 添加消息拦截器
// 支持动态添加消息处理拦截器，实现插件化扩展
func (m *msgServer) addInterceptorHandler(interceptorFunc ...MessageInterceptorFunc) {
	m.Handlers = append(m.Handlers, interceptorFunc...)
}

// Start 启动消息服务
// 初始化所有依赖组件，建立数据库连接，注册gRPC服务
func Start(ctx context.Context, config *Config, client discovery.SvcDiscoveryRegistry, server *grpc.Server) error {
	// 1. 初始化MongoDB连接
	mgocli, err := mongoutil.NewMongoDB(ctx, config.MongodbConfig.Build())
	if err != nil {
		return err
	}

	// 2. 初始化Redis连接
	rdb, err := redisutil.NewRedisClient(ctx, config.RedisConfig.Build())
	if err != nil {
		return err
	}

	// 3. 创建消息数据模型DAO
	msgDocModel, err := mgo.NewMsgMongo(mgocli.GetDB())
	if err != nil {
		return err
	}

	// 4. 创建消息缓存层（Redis + MongoDB）
	msgModel := redis.NewMsgCache(rdb, msgDocModel)

	// 5. 创建序列号会话数据模型和缓存
	seqConversation, err := mgo.NewSeqConversationMongo(mgocli.GetDB())
	if err != nil {
		return err
	}
	seqConversationCache := redis.NewSeqConversationCacheRedis(rdb, seqConversation)

	// 6. 创建序列号用户数据模型和缓存
	seqUser, err := mgo.NewSeqUserMongo(mgocli.GetDB())
	if err != nil {
		return err
	}
	seqUserCache := redis.NewSeqUserCacheRedis(rdb, seqUser)

	// 7. 创建统一的消息数据库控制器
	msgDatabase, err := controller.NewCommonMsgDatabase(msgDocModel, msgModel, seqUserCache, seqConversationCache, &config.KafkaConfig)
	if err != nil {
		return err
	}

	// 8. 获取其他服务的连接
	userConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.User)
	if err != nil {
		return err
	}
	groupConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.Group)
	if err != nil {
		return err
	}
	friendConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.Friend)
	if err != nil {
		return err
	}
	conversationConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.Conversation)
	if err != nil {
		return err
	}

	conversationClient := rpcli.NewConversationClient(conversationConn)

	// 9. 创建消息服务器实例
	s := &msgServer{
		MsgDatabase:    msgDatabase,
		RegisterCenter: client,

		// 初始化多层缓存系统
		UserLocalCache:         rpccache.NewUserLocalCache(rpcli.NewUserClient(userConn), &config.LocalCacheConfig, rdb),
		GroupLocalCache:        rpccache.NewGroupLocalCache(rpcli.NewGroupClient(groupConn), &config.LocalCacheConfig, rdb),
		ConversationLocalCache: rpccache.NewConversationLocalCache(conversationClient, &config.LocalCacheConfig, rdb),
		FriendLocalCache:       rpccache.NewFriendLocalCache(rpcli.NewRelationClient(friendConn), &config.LocalCacheConfig, rdb),

		config:             config,
		webhookClient:      webhook.NewWebhookClient(config.WebhooksConfig.URL),
		conversationClient: conversationClient,
	}

	// 10. 初始化通知发送器
	// 通用通知发送器：支持本地消息发送
	s.notificationSender = notification.NewNotificationSender(&config.NotificationConfig, notification.WithLocalSendMsg(s.SendMsg))
	// 消息通知发送器：专门处理消息相关通知
	s.msgNotificationSender = NewMsgNotificationSender(config, notification.WithLocalSendMsg(s.SendMsg))

	// 11. 注册gRPC服务
	msg.RegisterMsgServer(server, s)

	return nil
}

// conversationAndGetRecvID 根据会话信息获取接收者ID
// 这是一个工具方法，用于从会话信息中提取真正的消息接收者
func (m *msgServer) conversationAndGetRecvID(conversation *conversation.Conversation, userID string) string {
	// 单聊和通知聊天：接收者是对话的另一方
	if conversation.ConversationType == constant.SingleChatType ||
		conversation.ConversationType == constant.NotificationChatType {
		if userID == conversation.OwnerUserID {
			// 如果当前用户是会话拥有者，则接收者是会话中的另一个用户
			return conversation.UserID
		} else {
			// 否则接收者是会话拥有者
			return conversation.OwnerUserID
		}
	} else if conversation.ConversationType == constant.ReadGroupChatType {
		// 群聊：接收者是群组ID
		return conversation.GroupID
	}
	return ""
}
