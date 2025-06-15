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

// Package push 离线推送处理模块
// 专门处理来自离线推送队列的消息，执行第三方推送服务的推送操作
package push

import (
	"context"

	"github.com/IBM/sarama"
	"github.com/openimsdk/open-im-server/v3/internal/push/offlinepush"
	"github.com/openimsdk/open-im-server/v3/internal/push/offlinepush/options"
	"github.com/openimsdk/open-im-server/v3/pkg/common/prommetrics"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/kafka"
	"github.com/openimsdk/protocol/constant"
	pbpush "github.com/openimsdk/protocol/push"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/utils/jsonutil"
	"google.golang.org/protobuf/proto"
)

// OfflinePushConsumerHandler 离线推送消费者处理器
// 专门处理离线推送队列中的消息，与第三方推送服务进行交互
type OfflinePushConsumerHandler struct {
	OfflinePushConsumerGroup *kafka.MConsumerGroup     // Kafka消费者组，监听离线推送主题
	offlinePusher            offlinepush.OfflinePusher // 离线推送器，执行实际的第三方推送
}

// NewOfflinePushConsumerHandler 创建离线推送消费者处理器实例
// 参数:
//   - config: 推送服务配置
//   - offlinePusher: 离线推送器实例
//
// 返回:
//   - *OfflinePushConsumerHandler: 离线推送处理器实例
//   - error: 创建失败时的错误信息
func NewOfflinePushConsumerHandler(config *Config, offlinePusher offlinepush.OfflinePusher) (*OfflinePushConsumerHandler, error) {
	var offlinePushConsumerHandler OfflinePushConsumerHandler
	var err error

	// 设置离线推送器
	offlinePushConsumerHandler.offlinePusher = offlinePusher

	// 创建Kafka消费者组，监听离线推送主题
	offlinePushConsumerHandler.OfflinePushConsumerGroup, err = kafka.NewMConsumerGroup(
		config.KafkaConfig.Build(),                      // Kafka配置
		config.KafkaConfig.ToOfflineGroupID,             // 消费者组ID
		[]string{config.KafkaConfig.ToOfflinePushTopic}, // 监听的主题列表
		true, // 启用自动提交
	)
	if err != nil {
		return nil, err
	}

	return &offlinePushConsumerHandler, nil
}

// Setup Kafka消费者组设置方法
// Sarama接口要求实现的方法，在消费者组启动时调用
func (*OfflinePushConsumerHandler) Setup(sarama.ConsumerGroupSession) error { return nil }

// Cleanup Kafka消费者组清理方法
// Sarama接口要求实现的方法，在消费者组关闭时调用
func (*OfflinePushConsumerHandler) Cleanup(sarama.ConsumerGroupSession) error { return nil }

// ConsumeClaim 消费Kafka消息
// 核心消费逻辑，持续监听并处理离线推送队列中的消息
// 参数:
//   - sess: Kafka消费者会话
//   - claim: Kafka消息声明
//
// 返回:
//   - error: 消费过程中的错误
func (o *OfflinePushConsumerHandler) ConsumeClaim(sess sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	// 持续监听消息通道
	for msg := range claim.Messages() {
		// 从消息中提取上下文信息
		ctx := o.OfflinePushConsumerGroup.GetContextFromMsg(msg)

		// 处理离线推送消息
		o.handleMsg2OfflinePush(ctx, msg.Value)

		// 标记消息已处理
		sess.MarkMessage(msg, "")
	}
	return nil
}

// handleMsg2OfflinePush 处理离线推送消息
// 解析Kafka消息并执行第三方推送服务的推送操作
// 参数:
//   - ctx: 上下文
//   - msg: 原始消息字节数组
func (o *OfflinePushConsumerHandler) handleMsg2OfflinePush(ctx context.Context, msg []byte) {
	// 解析protobuf消息
	offlinePushMsg := pbpush.PushMsgReq{}
	if err := proto.Unmarshal(msg, &offlinePushMsg); err != nil {
		log.ZError(ctx, "offline push Unmarshal msg err", err, "msg", string(msg))
		return
	}

	// 验证消息完整性
	if offlinePushMsg.MsgData == nil || offlinePushMsg.UserIDs == nil {
		log.ZError(ctx, "offline push msg is empty", errs.New("offlinePushMsg is empty"),
			"userIDs", offlinePushMsg.UserIDs, "msg", offlinePushMsg.MsgData)
		return
	}

	// 确保消息状态正确
	if offlinePushMsg.MsgData.Status == constant.MsgStatusSending {
		offlinePushMsg.MsgData.Status = constant.MsgStatusSendSuccess
	}

	log.ZInfo(ctx, "receive to OfflinePush MQ", "userIDs", offlinePushMsg.UserIDs, "msg", offlinePushMsg.MsgData)

	// 执行离线推送
	err := o.offlinePushMsg(ctx, offlinePushMsg.MsgData, offlinePushMsg.UserIDs)
	if err != nil {
		log.ZWarn(ctx, "offline push failed", err, "msg", offlinePushMsg.String())
	}
}

// getOfflinePushInfos 获取离线推送信息
// 解析消息内容，构建推送标题、内容和选项
// 参数:
//   - msg: 消息数据
//
// 返回:
//   - title: 推送标题
//   - content: 推送内容
//   - opts: 推送选项
//   - err: 解析失败时的错误信息
func (o *OfflinePushConsumerHandler) getOfflinePushInfos(msg *sdkws.MsgData) (title, content string, opts *options.Opts, err error) {
	// @消息的文本元素结构体定义
	type AtTextElem struct {
		Text       string   `json:"text,omitempty"`       // 消息文本内容
		AtUserList []string `json:"atUserList,omitempty"` // @用户列表
		IsAtSelf   bool     `json:"isAtSelf"`             // 是否@自己
	}

	// 初始化推送选项，设置客户端消息ID
	opts = &options.Opts{Signal: &options.Signal{ClientMsgID: msg.ClientMsgID}}

	// 如果消息包含离线推送配置，使用配置中的选项
	if msg.OfflinePushInfo != nil {
		opts.IOSBadgeCount = msg.OfflinePushInfo.IOSBadgeCount // iOS徽章数量控制
		opts.IOSPushSound = msg.OfflinePushInfo.IOSPushSound   // iOS推送音效
		opts.Ex = msg.OfflinePushInfo.Ex                       // 扩展信息
	}

	// 优先使用消息中指定的推送标题和内容
	if msg.OfflinePushInfo != nil {
		title = msg.OfflinePushInfo.Title
		content = msg.OfflinePushInfo.Desc
	}

	// 如果没有指定标题，根据消息类型生成默认标题
	if title == "" {
		switch msg.ContentType {
		case constant.Text:
			fallthrough
		case constant.Picture:
			fallthrough
		case constant.Voice:
			fallthrough
		case constant.Video:
			fallthrough
		case constant.File:
			// 常规消息类型：使用预定义的推送内容
			title = constant.ContentType2PushContent[int64(msg.ContentType)]

		case constant.AtText:
			// @消息：解析消息内容（当前仅设置结构体，未使用）
			ac := AtTextElem{}
			_ = jsonutil.JsonStringToStruct(string(msg.Content), &ac)

		case constant.SignalingNotification:
			// 信令通知：使用信号消息的推送内容
			title = constant.ContentType2PushContent[constant.SignalMsg]

		default:
			// 其他类型：使用通用推送内容
			title = constant.ContentType2PushContent[constant.Common]
		}
	}

	// 如果没有指定内容，使用标题作为内容
	if content == "" {
		content = title
	}

	return
}

// offlinePushMsg 执行离线推送
// 获取推送信息并调用第三方推送服务
// 参数:
//   - ctx: 上下文
//   - msg: 消息数据
//   - offlinePushUserIDs: 需要推送的用户ID列表
//
// 返回:
//   - error: 推送失败时的错误信息
func (o *OfflinePushConsumerHandler) offlinePushMsg(ctx context.Context, msg *sdkws.MsgData, offlinePushUserIDs []string) error {
	// 获取推送标题、内容和选项
	title, content, opts, err := o.getOfflinePushInfos(msg)
	if err != nil {
		return err
	}

	// 调用第三方推送服务执行推送
	err = o.offlinePusher.Push(ctx, offlinePushUserIDs, title, content, opts)
	if err != nil {
		// 推送失败时增加监控计数器
		prommetrics.MsgOfflinePushFailedCounter.Inc()
		return err
	}

	return nil
}
