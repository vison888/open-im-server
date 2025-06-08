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

// Package msgtransfer 消息传输服务包
// 负责处理消息的转发、缓存、持久化等核心功能
// 主要组件：
// 1. OnlineHistoryRedisConsumerHandler - 处理消息到Redis缓存和序号分配
// 2. OnlineHistoryMongoConsumerHandler - 处理消息到MongoDB持久化
package msgtransfer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/openimsdk/tools/discovery/etcd"
	"github.com/openimsdk/tools/utils/jsonutil"
	"github.com/openimsdk/tools/utils/network"

	"github.com/openimsdk/open-im-server/v3/pkg/common/prommetrics"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache/redis"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database/mgo"
	"github.com/openimsdk/tools/db/mongoutil"
	"github.com/openimsdk/tools/db/redisutil"
	"github.com/openimsdk/tools/utils/datautil"

	conf "github.com/openimsdk/open-im-server/v3/pkg/common/config"
	discRegister "github.com/openimsdk/open-im-server/v3/pkg/common/discoveryregister"
	kdisc "github.com/openimsdk/open-im-server/v3/pkg/common/discoveryregister"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/controller"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mw"
	"github.com/openimsdk/tools/system/program"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// MsgTransfer 消息传输服务主结构体
// 负责协调两个核心消费者处理器的运行
type MsgTransfer struct {
	// historyCH Redis消息处理器
	// 功能：
	// 1. 消费Kafka ToRedisTopic主题的消息
	// 2. 批量处理消息并分类（存储/非存储，通知/普通消息）
	// 3. 使用Redis INCRBY原子操作分配唯一序号（核心去重机制）
	// 4. 将消息写入Redis缓存（key: msg:conversationID:seq，TTL: 24小时）
	// 5. 转发消息到推送队列（toPushTopic）和持久化队列（toMongoTopic）
	historyCH *OnlineHistoryRedisConsumerHandler

	// historyMongoCH MongoDB持久化处理器
	// 功能：
	// 1. 消费Kafka ToMongoTopic主题的消息
	// 2. 批量将消息写入MongoDB进行永久存储
	// 3. 提供历史消息查询支持
	historyMongoCH *OnlineHistoryMongoConsumerHandler

	// ctx 上下文，用于控制服务生命周期
	ctx context.Context
	// cancel 取消函数，用于优雅关闭服务
	cancel context.CancelFunc
}

// Config 消息传输服务配置结构体
// 包含所有必要的配置信息
type Config struct {
	MsgTransfer    conf.MsgTransfer // 消息传输服务特定配置
	RedisConfig    conf.Redis       // Redis配置（用于消息缓存和序号管理）
	MongodbConfig  conf.Mongo       // MongoDB配置（用于消息持久化存储）
	KafkaConfig    conf.Kafka       // Kafka配置（消息队列）
	Share          conf.Share       // 共享配置（如服务注册名称）
	WebhooksConfig conf.Webhooks    // Webhook配置
	Discovery      conf.Discovery   // 服务发现配置
}

// Start 启动消息传输服务
// 这是整个MsgTransfer服务的入口函数，负责初始化所有组件并启动服务
//
// 参数:
//   - ctx: 上下文
//   - index: 服务实例索引（用于多实例部署）
//   - config: 服务配置
//
// 返回值:
//   - error: 启动过程中的错误
func Start(ctx context.Context, index int, config *Config) error {

	log.CInfo(ctx, "MSG-TRANSFER server is initializing", "prometheusPorts",
		config.MsgTransfer.Prometheus.Ports, "index", index)

	// 1. 初始化MongoDB连接
	// MongoDB用于消息的永久存储，提供历史消息查询功能
	mgocli, err := mongoutil.NewMongoDB(ctx, config.MongodbConfig.Build())
	if err != nil {
		return err
	}

	// 2. 初始化Redis连接
	// Redis用于消息缓存、序号管理、在线状态等高频访问数据
	rdb, err := redisutil.NewRedisClient(ctx, config.RedisConfig.Build())
	if err != nil {
		return err
	}

	// 3. 初始化服务发现客户端
	// 用于与其他微服务（如group、conversation服务）进行通信
	client, err := discRegister.NewDiscoveryRegister(&config.Discovery, &config.Share, nil)
	if err != nil {
		return err
	}
	client.AddOption(mw.GrpcClient(), grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(fmt.Sprintf(`{"LoadBalancingPolicy": "%s"}`, "round_robin")))

	// 4. 初始化数据层组件
	// 4.1 消息文档模型（MongoDB）
	msgDocModel, err := mgo.NewMsgMongo(mgocli.GetDB())
	if err != nil {
		return err
	}
	// 4.2 消息缓存模型（Redis + MongoDB）
	msgModel := redis.NewMsgCache(rdb, msgDocModel)

	// 4.3 会话序号管理（用于消息去重和排序）
	seqConversation, err := mgo.NewSeqConversationMongo(mgocli.GetDB())
	if err != nil {
		return err
	}
	seqConversationCache := redis.NewSeqConversationCacheRedis(rdb, seqConversation)

	// 4.4 用户序号管理
	seqUser, err := mgo.NewSeqUserMongo(mgocli.GetDB())
	if err != nil {
		return err
	}
	seqUserCache := redis.NewSeqUserCacheRedis(rdb, seqUser)

	// 5. 创建消息传输数据库控制器
	// 整合所有数据操作，提供统一的数据访问接口
	msgTransferDatabase, err := controller.NewMsgTransferDatabase(msgDocModel, msgModel, seqUserCache, seqConversationCache, &config.KafkaConfig)
	if err != nil {
		return err
	}

	// 6. 创建Redis消息处理器
	// 负责处理ToRedisTopic的消息，实现核心的去重和缓存逻辑
	historyCH, err := NewOnlineHistoryRedisConsumerHandler(ctx, client, config, msgTransferDatabase)
	if err != nil {
		return err
	}

	// 7. 创建MongoDB消息处理器
	// 负责处理ToMongoTopic的消息，实现消息的持久化存储
	historyMongoCH, err := NewOnlineHistoryMongoConsumerHandler(&config.KafkaConfig, msgTransferDatabase)
	if err != nil {
		return err
	}

	// 8. 创建MsgTransfer实例并启动服务
	msgTransfer := &MsgTransfer{
		historyCH:      historyCH,
		historyMongoCH: historyMongoCH,
	}
	return msgTransfer.Start(index, config)
}

func (m *MsgTransfer) Start(index int, cfg *Config) error {
	m.ctx, m.cancel = context.WithCancel(context.Background())
	var (
		netDone = make(chan struct{}, 1)
		netErr  error
	)

	go m.historyCH.historyConsumerGroup.RegisterHandleAndConsumer(m.ctx, m.historyCH)
	go m.historyMongoCH.historyConsumerGroup.RegisterHandleAndConsumer(m.ctx, m.historyMongoCH)
	go m.historyCH.HandleUserHasReadSeqMessages(m.ctx)
	err := m.historyCH.redisMessageBatches.Start()
	if err != nil {
		return err
	}

	client, err := kdisc.NewDiscoveryRegister(&cfg.Discovery, &cfg.Share, nil)
	if err != nil {
		return errs.WrapMsg(err, "failed to register discovery service")
	}

	registerIP, err := network.GetRpcRegisterIP("")
	if err != nil {
		return err
	}

	getAutoPort := func() (net.Listener, int, error) {
		registerAddr := net.JoinHostPort(registerIP, "0")
		listener, err := net.Listen("tcp", registerAddr)
		if err != nil {
			return nil, 0, errs.WrapMsg(err, "listen err", "registerAddr", registerAddr)
		}
		_, portStr, _ := net.SplitHostPort(listener.Addr().String())
		port, _ := strconv.Atoi(portStr)
		return listener, port, nil
	}

	if cfg.MsgTransfer.Prometheus.AutoSetPorts && cfg.Discovery.Enable != conf.ETCD {
		return errs.New("only etcd support autoSetPorts", "RegisterName", "api").Wrap()
	}

	if cfg.MsgTransfer.Prometheus.Enable {
		var (
			listener       net.Listener
			prometheusPort int
		)

		if cfg.MsgTransfer.Prometheus.AutoSetPorts {
			listener, prometheusPort, err = getAutoPort()
			if err != nil {
				return err
			}

			etcdClient := client.(*etcd.SvcDiscoveryRegistryImpl).GetClient()

			_, err = etcdClient.Put(context.TODO(), prommetrics.BuildDiscoveryKey(prommetrics.MessageTransferKeyName), jsonutil.StructToJsonString(prommetrics.BuildDefaultTarget(registerIP, prometheusPort)))
			if err != nil {
				return errs.WrapMsg(err, "etcd put err")
			}
		} else {
			prometheusPort, err = datautil.GetElemByIndex(cfg.MsgTransfer.Prometheus.Ports, index)
			if err != nil {
				return err
			}
			listener, err = net.Listen("tcp", fmt.Sprintf(":%d", prometheusPort))
			if err != nil {
				return errs.WrapMsg(err, "listen err", "addr", fmt.Sprintf(":%d", prometheusPort))
			}
		}

		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.ZPanic(m.ctx, "MsgTransfer Start Panic", errs.ErrPanic(r))
				}
			}()
			if err := prommetrics.TransferInit(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				netErr = errs.WrapMsg(err, "prometheus start error", "prometheusPort", prometheusPort)
				netDone <- struct{}{}
			}
		}()
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM)
	select {
	case <-sigs:
		program.SIGTERMExit()
		// graceful close kafka client.
		m.cancel()
		m.historyCH.redisMessageBatches.Close()
		m.historyCH.Close()
		m.historyCH.historyConsumerGroup.Close()
		m.historyMongoCH.historyConsumerGroup.Close()
		return nil
	case <-netDone:
		m.cancel()
		m.historyCH.redisMessageBatches.Close()
		m.historyCH.Close()
		m.historyCH.historyConsumerGroup.Close()
		m.historyMongoCH.historyConsumerGroup.Close()
		close(netDone)
		return netErr
	}
}
