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

	"github.com/IBM/sarama"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/prommetrics"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/controller"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/kafka"
	pbmsg "github.com/openimsdk/protocol/msg"
	"github.com/openimsdk/tools/log"
	"google.golang.org/protobuf/proto"
)

// OnlineHistoryMongoConsumerHandler MongoDB消息持久化处理器
// 负责将消息从Kafka的ToMongoTopic主题消费并持久化到MongoDB
//
// 主要功能：
// 1. 消费ToMongoTopic主题的消息
// 2. 批量写入消息到MongoDB集合
// 3. 提供历史消息的永久存储支持
// 4. 支持消息的检索和查询
type OnlineHistoryMongoConsumerHandler struct {
	// historyConsumerGroup Kafka消费者组，专门消费ToMongoTopic主题
	historyConsumerGroup *kafka.MConsumerGroup
	// msgTransferDatabase 数据库操作接口，用于MongoDB写入操作
	msgTransferDatabase controller.MsgTransferDatabase
}

// NewOnlineHistoryMongoConsumerHandler 创建MongoDB消息持久化处理器
//
// 参数：
//   - kafkaConf: Kafka配置
//   - database: 数据库操作接口
//
// 返回值：
//   - *OnlineHistoryMongoConsumerHandler: MongoDB处理器实例
//   - error: 创建过程中的错误
func NewOnlineHistoryMongoConsumerHandler(kafkaConf *config.Kafka, database controller.MsgTransferDatabase) (*OnlineHistoryMongoConsumerHandler, error) {
	// 创建Kafka消费者组，消费ToMongoTopic主题
	// 参数说明：
	// - kafkaConf.ToMongoGroupID: 消费者组ID
	// - kafkaConf.ToMongoTopic: 消费的主题名称
	// - true: 启用最早偏移量消费（确保不丢失消息）
	historyConsumerGroup, err := kafka.NewMConsumerGroup(kafkaConf.Build(), kafkaConf.ToMongoGroupID, []string{kafkaConf.ToMongoTopic}, true)
	if err != nil {
		return nil, err
	}

	mc := &OnlineHistoryMongoConsumerHandler{
		historyConsumerGroup: historyConsumerGroup,
		msgTransferDatabase:  database,
	}
	return mc, nil
}

// handleChatWs2Mongo 处理消息持久化到MongoDB
// 这是MongoDB处理器的核心方法，负责将消息批量写入MongoDB
//
// 处理流程：
// 1. 反序列化Kafka消息数据
// 2. 验证消息数据的有效性
// 3. 批量插入消息到MongoDB
// 4. 更新Prometheus监控指标
//
// 参数：
//   - ctx: 上下文
//   - cMsg: Kafka消费消息
//   - key: 消息键（通常是会话ID）
//   - session: Kafka消费者会话
func (mc *OnlineHistoryMongoConsumerHandler) handleChatWs2Mongo(ctx context.Context, cMsg *sarama.ConsumerMessage, key string, session sarama.ConsumerGroupSession) {
	msg := cMsg.Value
	msgFromMQ := pbmsg.MsgDataToMongoByMQ{}

	// 1. 反序列化消息数据
	err := proto.Unmarshal(msg, &msgFromMQ)
	if err != nil {
		log.ZError(ctx, "unmarshall failed", err, "key", key, "len", len(msg))
		return
	}

	// 2. 验证消息数据
	if len(msgFromMQ.MsgData) == 0 {
		log.ZError(ctx, "msgFromMQ.MsgData is empty", nil, "cMsg", cMsg)
		return
	}

	log.ZDebug(ctx, "mongo consumer recv msg", "msgs", msgFromMQ.String())

	// 3. 批量插入消息到MongoDB
	// 使用会话ID、消息列表和最后序号进行批量插入
	err = mc.msgTransferDatabase.BatchInsertChat2DB(ctx, msgFromMQ.ConversationID, msgFromMQ.MsgData, msgFromMQ.LastSeq)
	if err != nil {
		log.ZError(
			ctx,
			"single data insert to mongo err",
			err,
			"msg",
			msgFromMQ.MsgData,
			"conversationID",
			msgFromMQ.ConversationID,
		)
		// 4. 更新失败监控指标
		prommetrics.MsgInsertMongoFailedCounter.Inc()
	} else {
		// 4. 更新成功监控指标
		prommetrics.MsgInsertMongoSuccessCounter.Inc()
	}

	// 注释的代码：原本设计在MongoDB插入成功后删除Redis缓存
	// 但为了提高读取性能，现在保留Redis缓存24小时
	// 这样可以快速响应最近消息的查询请求
	//var seqs []int64
	//for _, msg := range msgFromMQ.MsgData {
	//	seqs = append(seqs, msg.Seq)
	//}
	//if err := mc.msgTransferDatabase.DeleteMessagesFromCache(ctx, msgFromMQ.ConversationID, seqs); err != nil {
	//	log.ZError(ctx, "remove cache msg from redis err", err, "msg",
	//		msgFromMQ.MsgData, "conversationID", msgFromMQ.ConversationID)
	//}
}

// Setup Kafka消费者组设置回调
// 在消费者加入组时调用，用于初始化操作
func (*OnlineHistoryMongoConsumerHandler) Setup(_ sarama.ConsumerGroupSession) error {
	return nil
}

// Cleanup Kafka消费者组清理回调
// 在消费者离开组时调用，用于清理操作
func (*OnlineHistoryMongoConsumerHandler) Cleanup(_ sarama.ConsumerGroupSession) error {
	return nil
}

// ConsumeClaim 消费指定分区的消息
// 这是Kafka消费者接口的实现，负责持续消费ToMongoTopic主题的消息
//
// 工作流程：
// 1. 监听分区消息流
// 2. 从消息头部提取上下文信息
// 3. 调用handleChatWs2Mongo处理消息持久化
// 4. 标记消息为已处理
//
// 参数：
//   - sess: Kafka消费者会话
//   - claim: 分区声明，包含要消费的分区信息
//
// 返回值：
//   - error: 消费过程中的错误
func (mc *OnlineHistoryMongoConsumerHandler) ConsumeClaim(sess sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	log.ZDebug(context.Background(), "online new session msg come", "highWaterMarkOffset",
		claim.HighWaterMarkOffset(), "topic", claim.Topic(), "partition", claim.Partition())

	// 持续消费分区中的消息
	for msg := range claim.Messages() {
		// 1. 从消息头部提取上下文信息
		ctx := mc.historyConsumerGroup.GetContextFromMsg(msg)

		// 2. 验证消息有效性并处理
		if len(msg.Value) != 0 {
			mc.handleChatWs2Mongo(ctx, msg, string(msg.Key), sess)
		} else {
			log.ZError(ctx, "mongo msg get from kafka but is nil", nil, "conversationID", msg.Key)
		}

		// 3. 标记消息为已处理，更新消费偏移量
		sess.MarkMessage(msg, "")
	}
	return nil
}
