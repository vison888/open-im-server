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

// Package push 实现OpenIM的消息推送服务
// 包含在线推送、离线推送、推送配置管理等功能
package push

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/internal/push/offlinepush"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache/redis"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/controller"
	pbpush "github.com/openimsdk/protocol/push"
	"github.com/openimsdk/tools/db/redisutil"
	"github.com/openimsdk/tools/discovery"
	"google.golang.org/grpc"
)

// pushServer 推送服务器结构体
// 实现了 PushMsgServiceServer 接口，处理推送相关的RPC请求
type pushServer struct {
	pbpush.UnimplementedPushMsgServiceServer                                // gRPC服务的默认实现
	database                                 controller.PushDatabase        // 推送数据库操作接口
	disCov                                   discovery.SvcDiscoveryRegistry // 服务发现注册中心
	offlinePusher                            offlinepush.OfflinePusher      // 离线推送器接口
	pushCh                                   *ConsumerHandler               // 普通消息推送处理器
	offlinePushCh                            *OfflinePushConsumerHandler    // 离线推送消息处理器
}

// Config 推送服务配置结构体
// 包含推送服务运行所需的所有配置信息
type Config struct {
	RpcConfig          config.Push         // RPC服务配置（端口、推送相关配置等）
	RedisConfig        config.Redis        // Redis配置（缓存、令牌存储等）
	KafkaConfig        config.Kafka        // Kafka配置（消息队列）
	NotificationConfig config.Notification // 通知配置
	Share              config.Share        // 共享配置（管理员用户ID等）
	WebhooksConfig     config.Webhooks     // Webhook配置（推送前后回调）
	LocalCacheConfig   config.LocalCache   // 本地缓存配置
	Discovery          config.Discovery    // 服务发现配置
	FcmConfigPath      string              // FCM配置文件路径

	runTimeEnv string // 运行时环境标识
}

// DelUserPushToken 删除用户推送令牌
// 用于用户注销或更换设备时清理推送令牌
// 参数:
//   - ctx: 上下文，包含请求信息和超时控制
//   - req: 删除令牌请求，包含用户ID和平台ID
//
// 返回:
//   - resp: 删除结果响应
//   - err: 错误信息，成功时为nil
func (p pushServer) DelUserPushToken(ctx context.Context,
	req *pbpush.DelUserPushTokenReq) (resp *pbpush.DelUserPushTokenResp, err error) {
	// 从数据库中删除指定用户在指定平台的FCM令牌
	if err = p.database.DelFcmToken(ctx, req.UserID, int(req.PlatformID)); err != nil {
		return nil, err
	}
	return &pbpush.DelUserPushTokenResp{}, nil
}

// Start 启动推送服务
// 初始化推送服务的所有组件，包括离线推送器、消息处理器等
// 参数:
//   - ctx: 上下文，用于控制服务启动过程
//   - config: 推送服务配置
//   - client: 服务发现客户端
//   - server: gRPC服务器实例
//
// 返回:
//   - error: 启动过程中的错误，成功时为nil
func Start(ctx context.Context, config *Config, client discovery.SvcDiscoveryRegistry, server *grpc.Server) error {
	// 初始化Redis客户端，用于缓存和令牌存储
	rdb, err := redisutil.NewRedisClient(ctx, config.RedisConfig.Build())
	if err != nil {
		return err
	}

	// 创建第三方缓存模型（基于Redis）
	cacheModel := redis.NewThirdCache(rdb)

	// 初始化离线推送器，支持多种推送服务（FCM、个推、极光推送等）
	offlinePusher, err := offlinepush.NewOfflinePusher(&config.RpcConfig, cacheModel, config.FcmConfigPath)
	if err != nil {
		return err
	}

	// 创建推送数据库控制器，处理推送相关的数据操作
	database := controller.NewPushDatabase(cacheModel, &config.KafkaConfig)

	// 创建消息推送处理器，处理来自消息传输服务的推送请求
	consumer, err := NewConsumerHandler(ctx, config, database, offlinePusher, rdb, client)
	if err != nil {
		return err
	}

	// 创建离线推送消息处理器，专门处理离线推送队列中的消息
	offlinePushConsumer, err := NewOfflinePushConsumerHandler(config, offlinePusher)
	if err != nil {
		return err
	}

	// 注册推送服务到gRPC服务器
	pbpush.RegisterPushMsgServiceServer(server, &pushServer{
		database:      database,
		disCov:        client,
		offlinePusher: offlinePusher,
		pushCh:        consumer,
		offlinePushCh: offlinePushConsumer,
	})

	// 启动消息推送消费者协程
	// 监听来自msg_transfer的消息，进行在线和离线推送
	go consumer.pushConsumerGroup.RegisterHandleAndConsumer(ctx, consumer)

	// 启动离线推送消费者协程
	// 专门处理离线推送队列中的消息
	go offlinePushConsumer.OfflinePushConsumerGroup.RegisterHandleAndConsumer(ctx, offlinePushConsumer)

	return nil
}
