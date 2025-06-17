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

// Package controller 推送数据库控制器实现
//
// 本文件实现推送相关的数据库控制器，主要负责离线推送和设备令牌管理
//
// **功能概述：**
// ┌─────────────────────────────────────────────────────────┐
// │                 推送系统架构                             │
// ├─────────────────────────────────────────────────────────┤
// │ 1. FCM Token管理：设备令牌的缓存和清理                    │
// │ 2. 离线推送队列：消息投递到Kafka的离线推送主题             │
// │ 3. 异步处理：通过消息队列实现异步推送处理                  │
// │ 4. 可靠投递：Kafka确保消息的可靠传输                      │
// └─────────────────────────────────────────────────────────┘
//
// **推送流程：**
// 用户离线消息 → PushDatabase → Kafka队列 → 推送服务 → FCM/APNs → 用户设备
//
// **设计特点：**
// - 简洁接口：只暴露核心的推送相关功能
// - 异步处理：通过Kafka实现消息的异步推送
// - 缓存管理：统一管理设备令牌的缓存
// - 可扩展性：支持多种推送服务的扩展
package controller

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/kafka"
	"github.com/openimsdk/protocol/push"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/log"
)

// PushDatabase 推送数据库操作接口
// 定义推送相关的数据操作方法，专注于设备令牌管理和离线推送
//
// **接口职责：**
// 1. 设备令牌管理：FCM令牌的删除和维护
// 2. 离线推送：消息投递到离线推送队列
//
// **设计理念：**
// - 最小接口原则：只包含推送必需的核心功能
// - 异步优先：通过消息队列实现异步处理
// - 状态无关：不维护推送状态，专注数据传输
type PushDatabase interface {
	// DelFcmToken 删除FCM设备令牌
	// 功能：从缓存中删除指定用户和平台的FCM令牌
	//
	// **应用场景：**
	// - 用户退出登录时清理令牌
	// - 令牌失效时的清理操作
	// - 设备切换时的令牌更新
	//
	// **参数说明：**
	// - userID: 用户ID
	// - platformID: 平台标识（iOS、Android等）
	//
	// **实现要点：**
	// - 直接操作缓存层，不涉及数据库
	// - 清理失效令牌，避免无效推送
	DelFcmToken(ctx context.Context, userID string, platformID int) error

	// MsgToOfflinePushMQ 发送消息到离线推送队列
	// 功能：将离线消息投递到Kafka的离线推送主题
	//
	// **核心流程：**
	// 1. 构建推送消息结构
	// 2. 发送到Kafka离线推送主题
	// 3. 记录推送日志
	//
	// **参数说明：**
	// - key: Kafka分区键，用于消息路由
	// - userIDs: 目标用户ID列表
	// - msg2mq: 消息数据结构
	//
	// **可靠性保证：**
	// - Kafka生产者确认机制
	// - 消息分区策略优化
	// - 详细的推送日志记录
	//
	// **性能优化：**
	// - 批量用户推送
	// - 异步处理模式
	// - 分区负载均衡
	MsgToOfflinePushMQ(ctx context.Context, key string, userIDs []string, msg2mq *sdkws.MsgData) error
}

// pushDataBase 推送数据库控制器实现
//
// **架构组成：**
// ┌─────────────────────────────────────────────────────────┐
// │                pushDataBase                            │
// ├─────────────────┬───────────────────────────────────────┤
// │   缓存组件       │            Kafka组件                  │
// │ ┌─────────────┐ │ ┌───────────────────────────────────┐ │
// │ │ ThirdCache  │ │ │     离线推送生产者                 │ │
// │ │ - FCM令牌   │ │ │ - 异步消息投递                     │ │
// │ │ - 设备信息   │ │ │ - 分区路由策略                     │ │
// │ │ - 令牌管理   │ │ │ - 可靠性保证                       │ │
// │ └─────────────┘ │ └───────────────────────────────────┘ │
// └─────────────────┴───────────────────────────────────────┘
//
// **职责分离：**
// - cache: 负责设备令牌的缓存管理
// - producerToOfflinePush: 负责离线推送消息的Kafka投递
type pushDataBase struct {
	cache                 cache.ThirdCache // 第三方服务缓存接口（FCM令牌等）
	producerToOfflinePush *kafka.Producer  // 离线推送Kafka生产者
}

// NewPushDatabase 创建推送数据库控制器实例
//
// **初始化流程：**
// 1. 构建Kafka生产者配置
// 2. 创建离线推送主题的生产者
// 3. 组装控制器实例
//
// **配置管理：**
// - Kafka连接配置（地址、认证等）
// - 离线推送主题配置
// - 生产者性能参数
//
// **错误处理：**
// - 配置验证失败返回nil
// - 生产者创建失败返回nil
// - 确保系统的健壮性
//
// **参数说明：**
// - cache: 第三方服务缓存实例
// - kafkaConf: Kafka配置信息
func NewPushDatabase(cache cache.ThirdCache, kafkaConf *config.Kafka) PushDatabase {
	// **第一步：构建Kafka生产者配置**
	// 基于用户配置构建标准的Kafka生产者配置
	conf, err := kafka.BuildProducerConfig(*kafkaConf.Build())
	if err != nil {
		// 配置构建失败，返回nil（系统会处理降级逻辑）
		return nil
	}

	// **第二步：创建离线推送专用的Kafka生产者**
	// 绑定特定的主题（ToOfflinePushTopic），专门处理离线推送消息
	producerToOfflinePush, err := kafka.NewKafkaProducer(conf, kafkaConf.Address, kafkaConf.ToOfflinePushTopic)
	if err != nil {
		// 生产者创建失败，返回nil
		return nil
	}

	// **第三步：返回完整的控制器实例**
	return &pushDataBase{
		cache:                 cache,
		producerToOfflinePush: producerToOfflinePush,
	}
}

// DelFcmToken 删除FCM设备令牌实现
//
// **功能说明：**
// 从Redis缓存中删除指定用户在指定平台的FCM令牌
//
// **调用时机：**
// - 用户主动退出登录
// - 设备令牌更换时清理旧令牌
// - 推送失败时清理无效令牌
// - 用户卸载应用时的清理
//
// **实现特点：**
// - 直接委托给缓存层处理
// - 不涉及数据库操作
// - 操作简单高效
func (p *pushDataBase) DelFcmToken(ctx context.Context, userID string, platformID int) error {
	return p.cache.DelFcmToken(ctx, userID, platformID)
}

// MsgToOfflinePushMQ 发送消息到离线推送队列实现
//
// **消息流转过程：**
// 1. 构建推送消息请求（PushMsgReq）
// 2. 使用指定key发送到Kafka
// 3. 记录详细的推送日志
//
// **关键实现点：**
//
// **消息结构：**
// - MsgData: 原始消息数据
// - UserIDs: 目标用户列表
//
// **分区策略：**
// - 使用业务key进行分区路由
// - 确保相关消息的有序性
//
// **日志记录：**
// - 记录推送的关键信息
// - 便于问题追踪和性能分析
// - 包含用户列表和消息内容
//
// **可靠性：**
// - Kafka的持久化保证
// - 生产者确认机制
// - 分区副本容错
func (p *pushDataBase) MsgToOfflinePushMQ(ctx context.Context, key string, userIDs []string, msg2mq *sdkws.MsgData) error {
	// **发送消息到Kafka离线推送主题**
	// 构建推送消息请求并发送到指定主题
	_, _, err := p.producerToOfflinePush.SendMessage(ctx, key, &push.PushMsgReq{
		MsgData: msg2mq,  // 消息数据
		UserIDs: userIDs, // 目标用户列表
	})

	// **记录推送日志**
	// 详细记录推送操作的关键信息，便于监控和排查
	log.ZInfo(ctx, "message is push to offlinePush topic",
		"key", key, // 分区键
		"userIDs", userIDs, // 目标用户
		"msg", msg2mq.String()) // 消息内容

	return err
}
