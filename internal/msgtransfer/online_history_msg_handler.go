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

package msgtransfer

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/rpcli"
	"github.com/openimsdk/tools/discovery"

	"github.com/openimsdk/open-im-server/v3/pkg/common/prommetrics"

	"github.com/IBM/sarama"
	"github.com/go-redis/redis"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/controller"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/kafka"
	"github.com/openimsdk/open-im-server/v3/pkg/msgprocessor"
	"github.com/openimsdk/open-im-server/v3/pkg/tools/batcher"
	"github.com/openimsdk/protocol/constant"
	pbconv "github.com/openimsdk/protocol/conversation"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
	"github.com/openimsdk/tools/utils/stringutil"
	"google.golang.org/protobuf/proto"
)

// 批处理器配置常量
const (
	size              = 500                    // 批处理消息数量上限，每批最多处理500条消息
	mainDataBuffer    = 500                    // 主数据缓冲区大小
	subChanBuffer     = 50                     // 子通道缓冲区大小
	worker            = 50                     // 并行工作协程数量，支持50个并发处理器
	interval          = 100 * time.Millisecond // 批处理时间间隔，100毫秒执行一次批处理
	hasReadChanBuffer = 1000                   // 已读序号通道缓冲区大小
)

// ContextMsg 带上下文的消息结构体
// 包装消息数据和对应的上下文信息，用于在处理流程中传递
type ContextMsg struct {
	message *sdkws.MsgData  // 消息数据
	ctx     context.Context // 上下文信息（包含操作ID、追踪信息等）
}

// userHasReadSeq 用户已读序号结构体
// 用于异步写入发送者对某条消息的已读序号到MongoDB
// 例如：发送者发送了序号为10的消息，那么他们对该会话的已读序号应该设置为10
// 这确保了发送者不会看到自己发送的消息为未读状态
type userHasReadSeq struct {
	conversationID string           // 会话ID
	userHasReadMap map[string]int64 // 用户ID -> 已读序号的映射
}

// OnlineHistoryRedisConsumerHandler Redis消息消费处理器
// 这是消息传输服务的核心组件，负责以下关键功能：
// 1. 消息去重：使用Redis INCRBY原子操作为每条消息分配全局唯一序号
// 2. 消息缓存：将消息存储到Redis中，提供快速访问（TTL: 24小时）
// 3. 消息分类：区分存储/非存储消息，通知/普通消息
// 4. 消息转发：将消息分发到推送队列和持久化队列
// 5. 已读状态管理：处理消息已读回执，更新用户已读序号
type OnlineHistoryRedisConsumerHandler struct {
	// historyConsumerGroup Kafka消费者组，消费ToRedisTopic主题
	historyConsumerGroup *kafka.MConsumerGroup

	// redisMessageBatches 批处理器，用于批量处理Kafka消息
	// 关键特性：
	// - 按会话ID进行分片，确保同一会话的消息有序处理
	// - 支持500条消息/批次，100ms时间间隔
	// - 50个并发工作协程
	redisMessageBatches *batcher.Batcher[sarama.ConsumerMessage]

	// msgTransferDatabase 数据库操作接口，提供Redis和MongoDB的统一访问
	msgTransferDatabase controller.MsgTransferDatabase

	// conversationUserHasReadChan 已读序号异步处理通道
	// 用于将用户已读序号更新任务发送到后台协程处理
	conversationUserHasReadChan chan *userHasReadSeq

	// wg WaitGroup，用于等待已读序号处理协程结束
	wg sync.WaitGroup

	// groupClient 群组服务客户端，用于获取群成员信息
	groupClient *rpcli.GroupClient
	// conversationClient 会话服务客户端，用于创建会话
	conversationClient *rpcli.ConversationClient
}

// NewOnlineHistoryRedisConsumerHandler 创建Redis消息消费处理器
// 这是MsgTransfer服务的核心组件构造函数，负责初始化所有必要的依赖
//
// 初始化内容：
// 1. Kafka消费者组 - 消费ToRedisTopic主题
// 2. 批处理器 - 实现高效的消息聚合处理
// 3. 微服务客户端 - 与Group和Conversation服务通信
// 4. 异步处理通道 - 用于已读序号的异步持久化
//
// 参数：
//   - ctx: 上下文
//   - client: 服务发现客户端
//   - config: 服务配置
//   - database: 数据库操作接口
//
// 返回值：
//   - *OnlineHistoryRedisConsumerHandler: Redis消息处理器实例
//   - error: 创建过程中的错误
func NewOnlineHistoryRedisConsumerHandler(ctx context.Context, client discovery.SvcDiscoveryRegistry, config *Config, database controller.MsgTransferDatabase) (*OnlineHistoryRedisConsumerHandler, error) {
	kafkaConf := config.KafkaConfig

	// 1. 创建Kafka消费者组
	// - ToRedisGroupID: 消费者组ID，用于负载均衡
	// - ToRedisTopic: 消费的主题，包含待处理的消息
	// - false: 不启用最早偏移量消费（从最新位置开始）
	historyConsumerGroup, err := kafka.NewMConsumerGroup(kafkaConf.Build(), kafkaConf.ToRedisGroupID, []string{kafkaConf.ToRedisTopic}, false)
	if err != nil {
		return nil, err
	}

	// 2. 获取Group服务连接，用于群聊相关操作
	groupConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.Group)
	if err != nil {
		return nil, err
	}

	// 3. 获取Conversation服务连接，用于会话管理
	conversationConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.Conversation)
	if err != nil {
		return nil, err
	}

	// 4. 初始化处理器实例
	var och OnlineHistoryRedisConsumerHandler
	och.msgTransferDatabase = database
	och.conversationUserHasReadChan = make(chan *userHasReadSeq, hasReadChanBuffer)
	och.groupClient = rpcli.NewGroupClient(groupConn)
	och.conversationClient = rpcli.NewConversationClient(conversationConn)
	och.wg.Add(1) // 为异步已读序号处理协程添加计数

	// 5. 创建和配置批处理器
	b := batcher.New[sarama.ConsumerMessage](
		batcher.WithSize(size),                 // 批量大小：500条消息
		batcher.WithWorker(worker),             // 工作协程数：50个
		batcher.WithInterval(interval),         // 时间间隔：100毫秒
		batcher.WithDataBuffer(mainDataBuffer), // 主数据缓冲区：500
		batcher.WithSyncWait(true),             // 启用同步等待，确保处理完成
		batcher.WithBuffer(subChanBuffer),      // 子通道缓冲区：50
	)

	// 6. 设置分片函数 - 根据会话ID分片，确保同一会话的消息有序处理
	b.Sharding = func(key string) int {
		hashCode := stringutil.GetHashCode(key)
		return int(hashCode) % och.redisMessageBatches.Worker()
	}

	// 7. 设置键提取函数 - 从Kafka消息中提取会话ID作为分片键
	b.Key = func(consumerMessage *sarama.ConsumerMessage) string {
		return string(consumerMessage.Key)
	}

	// 8. 设置批处理逻辑 - 指向do()方法
	b.Do = och.do

	och.redisMessageBatches = b
	och.historyConsumerGroup = historyConsumerGroup

	return &och, nil
}

// do 批处理消息的核心处理方法
// 这是整个消息处理流程的入口，负责协调各个处理步骤
//
// 处理流程：
// 1. 解析Kafka消息并提取上下文信息
// 2. 处理已读回执消息，更新用户已读序号
// 3. 对消息进行分类（存储/非存储，通知/普通消息）
// 4. 分别处理不同类型的消息
//
// 参数：
//   - ctx: 上下文
//   - channelID: 处理通道ID（用于并发处理）
//   - val: 批处理消息包装器
func (och *OnlineHistoryRedisConsumerHandler) do(ctx context.Context, channelID int, val *batcher.Msg[sarama.ConsumerMessage]) {
	// 1. 设置触发ID到上下文中，用于追踪
	ctx = mcontext.WithTriggerIDContext(ctx, val.TriggerID())

	// 2. 解析Kafka消息，提取消息数据和上下文
	ctxMessages := och.parseConsumerMessages(ctx, val.Val())
	ctx = withAggregationCtx(ctx, ctxMessages)
	log.ZInfo(ctx, "msg arrived channel", "channel id", channelID, "msgList length", len(ctxMessages), "key", val.Key())

	// 3. 处理已读回执消息，更新用户已读序号
	och.doSetReadSeq(ctx, ctxMessages)

	// 4. 对消息进行分类
	// - storageMsgList: 需要存储的普通消息
	// - notStorageMsgList: 不需要存储的普通消息（仅推送）
	// - storageNotificationList: 需要存储的通知消息
	// - notStorageNotificationList: 不需要存储的通知消息
	storageMsgList, notStorageMsgList, storageNotificationList, notStorageNotificationList :=
		och.categorizeMessageLists(ctxMessages)
	log.ZDebug(ctx, "number of categorized messages", "storageMsgList", len(storageMsgList), "notStorageMsgList",
		len(notStorageMsgList), "storageNotificationList", len(storageNotificationList), "notStorageNotificationList", len(notStorageNotificationList))

	// 5. 获取会话ID
	conversationIDMsg := msgprocessor.GetChatConversationIDByMsg(ctxMessages[0].message)
	conversationIDNotification := msgprocessor.GetNotificationConversationIDByMsg(ctxMessages[0].message)

	// 6. 分别处理不同类型的消息
	och.handleMsg(ctx, val.Key(), conversationIDMsg, storageMsgList, notStorageMsgList)
	och.handleNotification(ctx, val.Key(), conversationIDNotification, storageNotificationList, notStorageNotificationList)
}

// doSetReadSeq 处理消息已读回执，更新用户已读序号
// 这个方法专门处理已读回执消息（ContentType == HasReadReceipt），提取用户的已读序号信息
//
// 处理逻辑：
// 1. 遍历消息列表，筛选出已读回执消息
// 2. 解析已读回执内容，提取用户ID和已读序号
// 3. 合并相同用户的已读序号，取最大值
// 4. 将已读序号写入数据库
//
// 参数：
//   - ctx: 上下文
//   - msgs: 消息列表
func (och *OnlineHistoryRedisConsumerHandler) doSetReadSeq(ctx context.Context, msgs []*ContextMsg) {

	var conversationID string
	var userSeqMap map[string]int64

	// 1. 遍历消息列表，筛选已读回执消息
	for _, msg := range msgs {
		// 只处理已读回执类型的消息
		if msg.message.ContentType != constant.HasReadReceipt {
			continue
		}

		// 2. 解析通知元素
		var elem sdkws.NotificationElem
		if err := json.Unmarshal(msg.message.Content, &elem); err != nil {
			log.ZWarn(ctx, "handlerConversationRead Unmarshal NotificationElem msg err", err, "msg", msg)
			continue
		}

		// 3. 解析已读标记提示信息
		var tips sdkws.MarkAsReadTips
		if err := json.Unmarshal([]byte(elem.Detail), &tips); err != nil {
			log.ZWarn(ctx, "handlerConversationRead Unmarshal MarkAsReadTips msg err", err, "msg", msg)
			continue
		}

		// 批处理器处理的每批消息的会话ID是相同的
		conversationID = tips.ConversationID

		// 4. 处理序号列表，找出最大的已读序号
		if len(tips.Seqs) > 0 {
			for _, seq := range tips.Seqs {
				if tips.HasReadSeq < seq {
					tips.HasReadSeq = seq
				}
			}
			// 清空序号列表，避免重复处理
			clear(tips.Seqs)
			tips.Seqs = nil
		}

		// 5. 跳过无效的已读序号
		if tips.HasReadSeq < 0 {
			continue
		}

		// 6. 初始化用户序号映射
		if userSeqMap == nil {
			userSeqMap = make(map[string]int64)
		}

		// 7. 合并相同用户的已读序号，取最大值
		if userSeqMap[tips.MarkAsReadUserID] > tips.HasReadSeq {
			continue
		}
		userSeqMap[tips.MarkAsReadUserID] = tips.HasReadSeq
	}

	// 8. 如果没有有效的已读序号，直接返回
	if userSeqMap == nil {
		return
	}

	// 9. 验证会话ID
	if len(conversationID) == 0 {
		log.ZWarn(ctx, "conversation err", nil, "conversationID", conversationID)
	}

	// 10. 将已读序号写入数据库
	if err := och.msgTransferDatabase.SetHasReadSeqToDB(ctx, conversationID, userSeqMap); err != nil {
		log.ZWarn(ctx, "set read seq to db error", err, "conversationID", conversationID, "userSeqMap", userSeqMap)
	}
}

// parseConsumerMessages 解析Kafka消费消息
// 将原始的Kafka消息数据转换为带上下文的消息结构体
//
// 处理流程：
// 1. 遍历Kafka消息列表
// 2. 反序列化消息内容为MsgData结构体
// 3. 提取消息头部信息构建上下文
// 4. 封装为ContextMsg结构体
//
// 参数：
//   - ctx: 上下文
//   - consumerMessages: Kafka消费消息列表
//
// 返回值：
//   - []*ContextMsg: 带上下文的消息列表
func (och *OnlineHistoryRedisConsumerHandler) parseConsumerMessages(ctx context.Context, consumerMessages []*sarama.ConsumerMessage) []*ContextMsg {
	var ctxMessages []*ContextMsg

	// 遍历Kafka消息列表
	for i := 0; i < len(consumerMessages); i++ {
		ctxMsg := &ContextMsg{}
		msgFromMQ := &sdkws.MsgData{}

		// 1. 反序列化消息内容
		err := proto.Unmarshal(consumerMessages[i].Value, msgFromMQ)
		if err != nil {
			log.ZWarn(ctx, "msg_transfer Unmarshal msg err", err, string(consumerMessages[i].Value))
			continue
		}

		// 2. 提取和记录消息头部信息（用于调试）
		var arr []string
		for i, header := range consumerMessages[i].Headers {
			arr = append(arr, strconv.Itoa(i), string(header.Key), string(header.Value))
		}
		log.ZDebug(ctx, "consumer.kafka.GetContextWithMQHeader", "len", len(consumerMessages[i].Headers),
			"header", strings.Join(arr, ", "))

		// 3. 从消息头部构建上下文信息
		// 头部包含操作ID、追踪信息等重要的上下文数据
		ctxMsg.ctx = kafka.GetContextWithMQHeader(consumerMessages[i].Headers)
		ctxMsg.message = msgFromMQ

		log.ZDebug(ctx, "message parse finish", "message", msgFromMQ, "key",
			string(consumerMessages[i].Key))

		// 4. 添加到结果列表
		ctxMessages = append(ctxMessages, ctxMsg)
	}
	return ctxMessages
}

// categorizeMessageLists 消息分类处理
// 根据消息选项将消息分为四类：存储消息、非存储消息、存储通知、非存储通知
// 这种分类有助于后续的差异化处理策略
//
// 分类逻辑：
// 1. 通知消息 vs 普通消息：根据IsNotNotification()判断
// 2. 存储 vs 非存储：根据IsHistory()判断
// 3. 特殊处理：通知消息如果需要发送，会克隆为普通消息
//
// 参数：
//   - totalMsgs: 待分类的消息列表
//
// 返回值：
//   - storageMsgList: 需要存储的普通消息
//   - notStorageMsgList: 不需要存储的普通消息（仅推送）
//   - storageNotificationList: 需要存储的通知消息
//   - notStorageNotificationList: 不需要存储的通知消息
func (och *OnlineHistoryRedisConsumerHandler) categorizeMessageLists(totalMsgs []*ContextMsg) (storageMsgList,
	notStorageMsgList, storageNotificationList, notStorageNotificationList []*ContextMsg) {

	for _, v := range totalMsgs {
		options := msgprocessor.Options(v.message.Options)

		// 1. 判断是否为通知消息
		if !options.IsNotNotification() {
			// 这是通知消息

			// 2. 如果通知消息需要发送，克隆为普通消息
			if options.IsSendMsg() {
				// 克隆通知消息为普通消息，保持离线推送和未读计数选项
				msg := proto.Clone(v.message).(*sdkws.MsgData)

				// 初始化消息选项
				if v.message.Options != nil {
					msg.Options = msgprocessor.NewMsgOptions()
				}

				// 为克隆的消息设置推送和未读计数选项
				msg.Options = msgprocessor.WithOptions(msg.Options,
					msgprocessor.WithOfflinePush(options.IsOfflinePush()),
					msgprocessor.WithUnreadCount(options.IsUnreadCount()),
				)

				// 原通知消息关闭推送和未读计数
				v.message.Options = msgprocessor.WithOptions(
					v.message.Options,
					msgprocessor.WithOfflinePush(false),
					msgprocessor.WithUnreadCount(false),
				)

				// 将克隆的消息作为普通消息处理
				ctxMsg := &ContextMsg{
					message: msg,
					ctx:     v.ctx,
				}
				storageMsgList = append(storageMsgList, ctxMsg)
			}

			// 3. 根据是否需要历史记录分类通知消息
			if options.IsHistory() {
				storageNotificationList = append(storageNotificationList, v)
			} else {
				notStorageNotificationList = append(notStorageNotificationList, v)
			}
		} else {
			// 这是普通消息

			// 4. 根据是否需要历史记录分类普通消息
			if options.IsHistory() {
				storageMsgList = append(storageMsgList, v)
			} else {
				notStorageMsgList = append(notStorageMsgList, v)
			}
		}
	}
	return
}

// handleMsg 处理普通消息
// 这是普通消息的核心处理方法，负责消息的缓存、去重、转发和持久化
//
// 处理流程：
// 1. 立即推送非存储消息（实时性优先）
// 2. 批量处理存储消息：分配序号、写入缓存
// 3. 更新用户已读序号
// 4. 处理新会话创建逻辑
// 5. 发送消息到MongoDB持久化队列
// 6. 发送存储消息到推送队列
//
// 参数：
//   - ctx: 上下文
//   - key: 消息键（用于分片）
//   - conversationID: 会话ID
//   - storageList: 需要存储的消息列表
//   - notStorageList: 不需要存储的消息列表
func (och *OnlineHistoryRedisConsumerHandler) handleMsg(ctx context.Context, key, conversationID string, storageList, notStorageList []*ContextMsg) {
	log.ZInfo(ctx, "handle storage msg")
	for _, storageMsg := range storageList {
		log.ZDebug(ctx, "handle storage msg", "msg", storageMsg.message.String())
	}

	// 1. 立即推送非存储消息（如临时通知等）
	// 这类消息不需要缓存，直接推送以保证实时性
	och.toPushTopic(ctx, key, conversationID, notStorageList)

	// 2. 处理需要存储的消息
	var storageMessageList []*sdkws.MsgData
	for _, msg := range storageList {
		storageMessageList = append(storageMessageList, msg.message)
	}

	if len(storageMessageList) > 0 {
		msg := storageMessageList[0]

		// 3. 批量插入消息到Redis缓存
		// 这里会使用Redis INCRBY原子操作分配序号，实现去重
		lastSeq, isNewConversation, userSeqMap, err := och.msgTransferDatabase.BatchInsertChat2Cache(ctx, conversationID, storageMessageList)
		if err != nil && !errors.Is(errs.Unwrap(err), redis.Nil) {
			log.ZWarn(ctx, "batch data insert to redis err", err, "storageMsgList", storageMessageList)
			return
		}
		log.ZInfo(ctx, "BatchInsertChat2Cache end")

		// 4. 设置用户已读序号（发送者对自己发送的消息标记为已读）
		err = och.msgTransferDatabase.SetHasReadSeqs(ctx, conversationID, userSeqMap)
		if err != nil {
			log.ZWarn(ctx, "SetHasReadSeqs error", err, "userSeqMap", userSeqMap, "conversationID", conversationID)
			prommetrics.SeqSetFailedCounter.Inc()
		}

		// 5. 异步处理已读序号持久化到MongoDB
		och.conversationUserHasReadChan <- &userHasReadSeq{
			conversationID: conversationID,
			userHasReadMap: userSeqMap,
		}

		// 6. 处理新会话创建逻辑
		if isNewConversation {
			ctx := storageList[0].ctx
			switch msg.SessionType {
			case constant.ReadGroupChatType:
				// 群聊首次创建会话
				log.ZDebug(ctx, "group chat first create conversation", "conversationID",
					conversationID)

				userIDs, err := och.groupClient.GetGroupMemberUserIDs(ctx, msg.GroupID)
				if err != nil {
					log.ZWarn(ctx, "get group member ids error", err, "conversationID",
						conversationID)
				} else {
					log.ZInfo(ctx, "GetGroupMemberIDs end")

					if err := och.conversationClient.CreateGroupChatConversations(ctx, msg.GroupID, userIDs); err != nil {
						log.ZWarn(ctx, "single chat first create conversation error", err,
							"conversationID", conversationID)
					}
				}
			case constant.SingleChatType, constant.NotificationChatType:
				// 单聊或通知聊天首次创建会话
				req := &pbconv.CreateSingleChatConversationsReq{
					RecvID:           msg.RecvID,
					SendID:           msg.SendID,
					ConversationID:   conversationID,
					ConversationType: msg.SessionType,
				}
				if err := och.conversationClient.CreateSingleChatConversations(ctx, req); err != nil {
					log.ZWarn(ctx, "single chat or notification first create conversation error", err,
						"conversationID", conversationID, "sessionType", msg.SessionType)
				}
			default:
				log.ZWarn(ctx, "unknown session type", nil, "sessionType",
					msg.SessionType)
			}
		}

		// 7. 发送消息到MongoDB持久化队列
		log.ZInfo(ctx, "success incr to next topic")
		err = och.msgTransferDatabase.MsgToMongoMQ(ctx, key, conversationID, storageMessageList, lastSeq)
		if err != nil {
			log.ZError(ctx, "Msg To MongoDB MQ error", err, "conversationID",
				conversationID, "storageList", storageMessageList, "lastSeq", lastSeq)
		}
		log.ZInfo(ctx, "MsgToMongoMQ end")

		// 8. 发送存储消息到推送队列
		och.toPushTopic(ctx, key, conversationID, storageList)
		log.ZInfo(ctx, "toPushTopic end")
	}
}

// handleNotification 处理通知消息
// 通知消息的处理相对简单，不需要处理会话创建和复杂的已读状态逻辑
//
// 处理流程：
// 1. 立即推送非存储通知消息
// 2. 批量处理存储通知消息：分配序号、写入缓存
// 3. 发送通知消息到MongoDB持久化队列
// 4. 发送存储通知消息到推送队列
//
// 参数：
//   - ctx: 上下文
//   - key: 消息键（用于分片）
//   - conversationID: 会话ID
//   - storageList: 需要存储的通知消息列表
//   - notStorageList: 不需要存储的通知消息列表
func (och *OnlineHistoryRedisConsumerHandler) handleNotification(ctx context.Context, key, conversationID string,
	storageList, notStorageList []*ContextMsg) {

	// 1. 立即推送非存储通知消息（如临时系统通知）
	och.toPushTopic(ctx, key, conversationID, notStorageList)

	// 2. 处理需要存储的通知消息
	var storageMessageList []*sdkws.MsgData
	for _, msg := range storageList {
		storageMessageList = append(storageMessageList, msg.message)
	}

	if len(storageMessageList) > 0 {
		// 3. 批量插入通知消息到Redis缓存
		// 通知消息同样需要分配序号，但不需要处理已读状态和会话创建
		lastSeq, _, _, err := och.msgTransferDatabase.BatchInsertChat2Cache(ctx, conversationID, storageMessageList)
		if err != nil {
			log.ZError(ctx, "notification batch insert to redis error", err, "conversationID", conversationID,
				"storageList", storageMessageList)
			return
		}
		log.ZDebug(ctx, "success to next topic", "conversationID", conversationID)

		// 4. 发送通知消息到MongoDB持久化队列
		err = och.msgTransferDatabase.MsgToMongoMQ(ctx, key, conversationID, storageMessageList, lastSeq)
		if err != nil {
			log.ZError(ctx, "Msg To MongoDB MQ error", err, "conversationID",
				conversationID, "storageList", storageMessageList, "lastSeq", lastSeq)
		}

		// 5. 发送存储通知消息到推送队列
		och.toPushTopic(ctx, key, conversationID, storageList)
	}
}

// HandleUserHasReadSeqMessages 异步处理用户已读序号持久化
// 这是一个后台协程，专门负责将用户已读序号从内存异步持久化到MongoDB
// 采用异步处理可以避免阻塞主消息处理流程，提高系统吞吐量
//
// 工作机制：
// 1. 监听conversationUserHasReadChan通道
// 2. 接收到已读序号更新请求后，写入MongoDB
// 3. 处理过程中的错误不会影响主流程
// 4. 服务关闭时优雅退出
//
// 参数：
//   - ctx: 上下文
func (och *OnlineHistoryRedisConsumerHandler) HandleUserHasReadSeqMessages(ctx context.Context) {
	// 异常恢复机制，确保协程不会因为panic而崩溃
	defer func() {
		if r := recover(); r != nil {
			log.ZPanic(ctx, "HandleUserHasReadSeqMessages Panic", errs.ErrPanic(r))
		}
	}()

	// 协程结束时通知WaitGroup
	defer och.wg.Done()

	// 持续监听已读序号更新请求
	for msg := range och.conversationUserHasReadChan {
		// 将已读序号持久化到MongoDB
		// 这里的错误不会影响消息的正常处理流程
		if err := och.msgTransferDatabase.SetHasReadSeqToDB(ctx, msg.conversationID, msg.userHasReadMap); err != nil {
			log.ZWarn(ctx, "set read seq to db error", err, "conversationID", msg.conversationID, "userSeqMap", msg.userHasReadMap)
		}
	}

	log.ZInfo(ctx, "Channel closed, exiting handleUserHasReadSeqMessages")
}

// Close 优雅关闭Redis消息处理器
// 确保所有异步处理协程安全退出
func (och *OnlineHistoryRedisConsumerHandler) Close() {
	// 关闭已读序号处理通道，触发后台协程退出
	close(och.conversationUserHasReadChan)
	// 等待所有协程结束
	och.wg.Wait()
}

// toPushTopic 发送消息到推送队列
// 将消息发送到Kafka的ToPushTopic主题，供Push服务消费处理
//
// 功能说明：
// 1. 遍历消息列表，逐条发送到推送队列
// 2. 保持消息的上下文信息，确保推送时的追踪能力
// 3. 使用消息原始的上下文，而不是批处理的聚合上下文
//
// 参数：
//   - ctx: 上下文
//   - key: 消息键（用于Kafka分区）
//   - conversationID: 会话ID
//   - msgs: 待推送的消息列表
func (och *OnlineHistoryRedisConsumerHandler) toPushTopic(ctx context.Context, key, conversationID string, msgs []*ContextMsg) {
	for _, v := range msgs {
		log.ZDebug(ctx, "push msg to topic", "msg", v.message.String())
		// 使用消息原始的上下文，保持追踪链路的完整性
		_, _, _ = och.msgTransferDatabase.MsgToPushMQ(v.ctx, key, conversationID, v.message)
	}
}

// withAggregationCtx 创建聚合上下文
// 将批处理中多个消息的操作ID聚合为一个，便于追踪批处理操作
//
// 聚合规则：
// 1. 提取每个消息的操作ID
// 2. 使用"$"符号连接多个操作ID
// 3. 设置聚合后的操作ID到上下文中
//
// 这样做的好处：
// - 保持操作追踪的连续性
// - 能够在日志中看到批处理涉及的所有操作ID
// - 便于问题排查和性能分析
//
// 参数：
//   - ctx: 基础上下文
//   - values: 消息列表
//
// 返回值：
//   - context.Context: 包含聚合操作ID的新上下文
func withAggregationCtx(ctx context.Context, values []*ContextMsg) context.Context {
	var allMessageOperationID string

	// 遍历所有消息，提取操作ID
	for i, v := range values {
		if opid := mcontext.GetOperationID(v.ctx); opid != "" {
			if i == 0 {
				allMessageOperationID += opid
			} else {
				// 使用"$"分隔多个操作ID
				allMessageOperationID += "$" + opid
			}
		}
	}

	// 设置聚合后的操作ID
	return mcontext.SetOperationID(ctx, allMessageOperationID)
}

// Setup Kafka消费者组设置回调
// 在Redis消息处理器加入消费者组时调用
func (och *OnlineHistoryRedisConsumerHandler) Setup(_ sarama.ConsumerGroupSession) error {
	return nil
}

// Cleanup Kafka消费者组清理回调
// 在Redis消息处理器离开消费者组时调用
func (och *OnlineHistoryRedisConsumerHandler) Cleanup(_ sarama.ConsumerGroupSession) error {
	return nil
}

// ConsumeClaim 消费ToRedisTopic主题的消息
// 这是Redis消息处理器的Kafka消费入口，将消息投递给批处理器处理
//
// 工作流程：
// 1. 监听指定分区的消息流
// 2. 将消息投递给批处理器（batcher）
// 3. 批处理器会自动聚合消息并调用do()方法处理
// 4. 处理完成后自动提交Kafka偏移量
//
// 批处理机制：
// - 批处理器按会话ID进行分片，确保消息顺序性
// - 达到批量大小(500条)或时间间隔(100ms)时触发处理
// - 处理完成后自动标记消息和提交偏移量
//
// 参数：
//   - session: Kafka消费者会话
//   - claim: 分区声明，包含要消费的分区信息
//
// 返回值：
//   - error: 消费过程中的错误
func (och *OnlineHistoryRedisConsumerHandler) ConsumeClaim(session sarama.ConsumerGroupSession,
	claim sarama.ConsumerGroupClaim) error {
	log.ZDebug(context.Background(), "online new session msg come", "highWaterMarkOffset",
		claim.HighWaterMarkOffset(), "topic", claim.Topic(), "partition", claim.Partition())

	// 设置批处理完成回调，用于提交Kafka偏移量
	och.redisMessageBatches.OnComplete = func(lastMessage *sarama.ConsumerMessage, totalCount int) {
		// 标记最后一条消息为已处理
		session.MarkMessage(lastMessage, "")
		// 提交偏移量，确保消息不会重复消费
		session.Commit()
	}

	// 持续消费消息
	for {
		select {
		case msg, ok := <-claim.Messages():
			// 通道关闭，退出消费
			if !ok {
				return nil
			}

			// 跳过空消息
			if len(msg.Value) == 0 {
				continue
			}

			// 将消息投递给批处理器
			// 批处理器会根据消息键（会话ID）进行分片，确保同一会话的消息有序处理
			err := och.redisMessageBatches.Put(context.Background(), msg)
			if err != nil {
				log.ZWarn(context.Background(), "put msg to batcher error", err, "msg", msg)
			}
		case <-session.Context().Done():
			// 会话上下文结束，退出消费
			return nil
		}
	}
}
