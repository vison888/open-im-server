// Package push 实现OpenIM的消息推送处理器
// 负责处理来自消息传输服务的推送请求，包括在线推送和离线推送
package push

import (
	"context"
	"encoding/json"
	"math/rand"
	"strconv"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/rpcli"

	"github.com/IBM/sarama"
	"github.com/openimsdk/open-im-server/v3/internal/push/offlinepush"
	"github.com/openimsdk/open-im-server/v3/internal/push/offlinepush/options"
	"github.com/openimsdk/open-im-server/v3/pkg/common/prommetrics"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/controller"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/kafka"
	"github.com/openimsdk/open-im-server/v3/pkg/common/webhook"
	"github.com/openimsdk/open-im-server/v3/pkg/msgprocessor"
	"github.com/openimsdk/open-im-server/v3/pkg/rpccache"
	"github.com/openimsdk/open-im-server/v3/pkg/util/conversationutil"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/msggateway"
	pbpush "github.com/openimsdk/protocol/push"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/discovery"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
	"github.com/openimsdk/tools/utils/datautil"
	"github.com/openimsdk/tools/utils/jsonutil"
	"github.com/openimsdk/tools/utils/timeutil"
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/proto"
)

// ConsumerHandler 消息推送处理器
// 负责消费来自Kafka的推送消息，执行在线推送和离线推送
type ConsumerHandler struct {
	pushConsumerGroup      *kafka.MConsumerGroup            // Kafka消费者组，用于接收推送消息
	offlinePusher          offlinepush.OfflinePusher        // 离线推送器接口
	onlinePusher           OnlinePusher                     // 在线推送器接口
	pushDatabase           controller.PushDatabase          // 推送数据库控制器
	onlineCache            *rpccache.OnlineCache            // 在线用户缓存
	groupLocalCache        *rpccache.GroupLocalCache        // 群组本地缓存
	conversationLocalCache *rpccache.ConversationLocalCache // 会话本地缓存
	webhookClient          *webhook.Client                  // Webhook客户端
	config                 *Config                          // 推送服务配置
	userClient             *rpcli.UserClient                // 用户服务RPC客户端
	groupClient            *rpcli.GroupClient               // 群组服务RPC客户端
	msgClient              *rpcli.MsgClient                 // 消息服务RPC客户端
	conversationClient     *rpcli.ConversationClient        // 会话服务RPC客户端
}

// NewConsumerHandler 创建消息推送处理器实例
// 初始化推送处理器的所有依赖组件，包括RPC客户端、缓存、Kafka消费者等
// 参数:
//   - ctx: 上下文
//   - config: 推送服务配置
//   - database: 推送数据库控制器
//   - offlinePusher: 离线推送器
//   - rdb: Redis客户端
//   - client: 服务发现客户端
//
// 返回:
//   - *ConsumerHandler: 推送处理器实例
//   - error: 初始化失败时的错误信息
func NewConsumerHandler(ctx context.Context, config *Config, database controller.PushDatabase, offlinePusher offlinepush.OfflinePusher, rdb redis.UniversalClient,
	client discovery.SvcDiscoveryRegistry) (*ConsumerHandler, error) {
	var consumerHandler ConsumerHandler
	var err error
	consumerHandler.pushConsumerGroup, err = kafka.NewMConsumerGroup(config.KafkaConfig.Build(), config.KafkaConfig.ToPushGroupID,
		[]string{config.KafkaConfig.ToPushTopic}, true)
	if err != nil {
		return nil, err
	}
	userConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.User)
	if err != nil {
		return nil, err
	}
	groupConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.Group)
	if err != nil {
		return nil, err
	}
	msgConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.Msg)
	if err != nil {
		return nil, err
	}
	conversationConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.Conversation)
	if err != nil {
		return nil, err
	}
	consumerHandler.userClient = rpcli.NewUserClient(userConn)
	consumerHandler.groupClient = rpcli.NewGroupClient(groupConn)
	consumerHandler.msgClient = rpcli.NewMsgClient(msgConn)
	consumerHandler.conversationClient = rpcli.NewConversationClient(conversationConn)

	consumerHandler.offlinePusher = offlinePusher
	consumerHandler.onlinePusher = NewOnlinePusher(client, config)
	consumerHandler.groupLocalCache = rpccache.NewGroupLocalCache(consumerHandler.groupClient, &config.LocalCacheConfig, rdb)
	consumerHandler.conversationLocalCache = rpccache.NewConversationLocalCache(consumerHandler.conversationClient, &config.LocalCacheConfig, rdb)
	consumerHandler.webhookClient = webhook.NewWebhookClient(config.WebhooksConfig.URL)
	consumerHandler.config = config
	consumerHandler.pushDatabase = database
	consumerHandler.onlineCache, err = rpccache.NewOnlineCache(consumerHandler.userClient, consumerHandler.groupLocalCache, rdb, config.RpcConfig.FullUserCache, nil)
	if err != nil {
		return nil, err
	}
	return &consumerHandler, nil
}

// handleMs2PsChat 处理来自消息传输服务的推送消息
// 这是消息推送的入口方法，解析Kafka消息并根据会话类型执行不同的推送逻辑
// 参数:
//   - ctx: 上下文
//   - msg: 来自Kafka的protobuf编码消息
func (c *ConsumerHandler) handleMs2PsChat(ctx context.Context, msg []byte) {
	msgFromMQ := pbpush.PushMsgReq{}
	if err := proto.Unmarshal(msg, &msgFromMQ); err != nil {
		log.ZError(ctx, "push Unmarshal msg err", err, "msg", string(msg))
		return
	}

	sec := msgFromMQ.MsgData.SendTime / 1000
	nowSec := timeutil.GetCurrentTimestampBySecond()

	if nowSec-sec > 10 {
		prommetrics.MsgLoneTimePushCounter.Inc()
		log.ZWarn(ctx, "it's been a while since the message was sent", nil, "msg", msgFromMQ.String(), "sec", sec, "nowSec", nowSec, "nowSec-sec", nowSec-sec)
	}
	var err error

	switch msgFromMQ.MsgData.SessionType {
	case constant.ReadGroupChatType:
		err = c.Push2Group(ctx, msgFromMQ.MsgData.GroupID, msgFromMQ.MsgData)
	default:
		var pushUserIDList []string
		isSenderSync := datautil.GetSwitchFromOptions(msgFromMQ.MsgData.Options, constant.IsSenderSync)
		if !isSenderSync || msgFromMQ.MsgData.SendID == msgFromMQ.MsgData.RecvID {
			pushUserIDList = append(pushUserIDList, msgFromMQ.MsgData.RecvID)
		} else {
			pushUserIDList = append(pushUserIDList, msgFromMQ.MsgData.RecvID, msgFromMQ.MsgData.SendID)
		}
		err = c.Push2User(ctx, pushUserIDList, msgFromMQ.MsgData)
	}
	if err != nil {
		log.ZWarn(ctx, "push failed", err, "msg", msgFromMQ.String())
	}
}

func (*ConsumerHandler) Setup(sarama.ConsumerGroupSession) error { return nil }

func (*ConsumerHandler) Cleanup(sarama.ConsumerGroupSession) error { return nil }

func (c *ConsumerHandler) ConsumeClaim(sess sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	c.onlineCache.Lock.Lock()
	for c.onlineCache.CurrentPhase.Load() < rpccache.DoSubscribeOver {
		c.onlineCache.Cond.Wait()
	}
	c.onlineCache.Lock.Unlock()
	ctx := mcontext.SetOperationID(context.TODO(), strconv.FormatInt(time.Now().UnixNano()+int64(rand.Uint32()), 10))
	log.ZInfo(ctx, "begin consume messages")

	for msg := range claim.Messages() {
		ctx := c.pushConsumerGroup.GetContextFromMsg(msg)
		ctx = mcontext.WithOpUserIDContext(ctx, c.config.Share.IMAdminUserID[0])
		c.handleMs2PsChat(ctx, msg.Value)
		sess.MarkMessage(msg, "")
	}
	return nil
}

// Push2User 向用户推送消息
// 适用于单聊和通知消息的推送，包含在线推送和离线推送逻辑
// 推送流程：在线推送 -> 判断是否需要离线推送 -> 执行离线推送
// 参数:
//   - ctx: 上下文
//   - userIDs: 目标用户ID列表
//   - msg: 消息数据
//
// 返回:
//   - err: 推送失败时的错误信息
func (c *ConsumerHandler) Push2User(ctx context.Context, userIDs []string, msg *sdkws.MsgData) (err error) {
	log.ZInfo(ctx, "Get msg from msg_transfer And push msg", "userIDs", userIDs, "msg", msg.String())
	defer func(duration time.Time) {
		t := time.Since(duration)
		log.ZInfo(ctx, "Get msg from msg_transfer And push msg end", "msg", msg.String(), "time cost", t)
	}(time.Now())
	if err := c.webhookBeforeOnlinePush(ctx, &c.config.WebhooksConfig.BeforeOnlinePush, userIDs, msg); err != nil {
		return err
	}

	wsResults, err := c.GetConnsAndOnlinePush(ctx, msg, userIDs)
	if err != nil {
		return err
	}

	log.ZDebug(ctx, "single and notification push result", "result", wsResults, "msg", msg, "push_to_userID", userIDs)
	log.ZInfo(ctx, "single and notification push end")

	if !c.shouldPushOffline(ctx, msg) {
		return nil
	}
	log.ZInfo(ctx, "pushOffline start")

	for _, v := range wsResults {
		//message sender do not need offline push
		if msg.SendID == v.UserID {
			continue
		}
		//receiver online push success
		if v.OnlinePush {
			return nil
		}
	}
	needOfflinePushUserID := []string{msg.RecvID}
	var offlinePushUserID []string

	//receiver offline push
	if err = c.webhookBeforeOfflinePush(ctx, &c.config.WebhooksConfig.BeforeOfflinePush, needOfflinePushUserID, msg, &offlinePushUserID); err != nil {
		return err
	}

	if len(offlinePushUserID) > 0 {
		needOfflinePushUserID = offlinePushUserID
	}
	err = c.offlinePushMsg(ctx, msg, needOfflinePushUserID)
	if err != nil {
		log.ZDebug(ctx, "offlinePushMsg failed", err, "needOfflinePushUserID", needOfflinePushUserID, "msg", msg)
		log.ZWarn(ctx, "offlinePushMsg failed", err, "needOfflinePushUserID length", len(needOfflinePushUserID), "msg", msg)
		return nil
	}

	return nil
}

func (c *ConsumerHandler) shouldPushOffline(_ context.Context, msg *sdkws.MsgData) bool {
	isOfflinePush := datautil.GetSwitchFromOptions(msg.Options, constant.IsOfflinePush)
	if !isOfflinePush {
		return false
	}
	switch msg.ContentType {
	case constant.RoomParticipantsConnectedNotification:
		return false
	case constant.RoomParticipantsDisconnectedNotification:
		return false
	}
	return true
}

func (c *ConsumerHandler) GetConnsAndOnlinePush(ctx context.Context, msg *sdkws.MsgData, pushToUserIDs []string) ([]*msggateway.SingleMsgToUserResults, error) {
	if msg != nil && msg.Status == constant.MsgStatusSending {
		msg.Status = constant.MsgStatusSendSuccess
	}
	onlineUserIDs, offlineUserIDs, err := c.onlineCache.GetUsersOnline(ctx, pushToUserIDs)
	if err != nil {
		return nil, err
	}

	log.ZDebug(ctx, "GetConnsAndOnlinePush online cache", "sendID", msg.SendID, "recvID", msg.RecvID, "groupID", msg.GroupID, "sessionType", msg.SessionType, "clientMsgID", msg.ClientMsgID, "serverMsgID", msg.ServerMsgID, "offlineUserIDs", offlineUserIDs, "onlineUserIDs", onlineUserIDs)
	var result []*msggateway.SingleMsgToUserResults
	if len(onlineUserIDs) > 0 {
		var err error
		result, err = c.onlinePusher.GetConnsAndOnlinePush(ctx, msg, onlineUserIDs)
		if err != nil {
			return nil, err
		}
	}
	for _, userID := range offlineUserIDs {
		result = append(result, &msggateway.SingleMsgToUserResults{
			UserID: userID,
		})
	}
	return result, nil
}

// Push2Group 向群组推送消息
// 处理群组消息的推送，包括获取群成员、在线推送、离线推送等完整流程
// 支持webhook回调干预推送过程
// 参数:
//   - ctx: 上下文
//   - groupID: 群组ID
//   - msg: 消息数据
//
// 返回:
//   - err: 推送失败时的错误信息
func (c *ConsumerHandler) Push2Group(ctx context.Context, groupID string, msg *sdkws.MsgData) (err error) {
	log.ZInfo(ctx, "Get group msg from msg_transfer and push msg", "msg", msg.String(), "groupID", groupID)
	defer func(duration time.Time) {
		t := time.Since(duration)
		log.ZInfo(ctx, "Get group msg from msg_transfer and push msg end", "msg", msg.String(), "groupID", groupID, "time cost", t)
	}(time.Now())
	var pushToUserIDs []string
	if err = c.webhookBeforeGroupOnlinePush(ctx, &c.config.WebhooksConfig.BeforeGroupOnlinePush, groupID, msg,
		&pushToUserIDs); err != nil {
		return err
	}

	err = c.groupMessagesHandler(ctx, groupID, &pushToUserIDs, msg)
	if err != nil {
		return err
	}

	wsResults, err := c.GetConnsAndOnlinePush(ctx, msg, pushToUserIDs)
	if err != nil {
		return err
	}

	log.ZDebug(ctx, "group push result", "result", wsResults, "msg", msg)
	log.ZInfo(ctx, "online group push end")

	if !c.shouldPushOffline(ctx, msg) {
		return nil
	}
	needOfflinePushUserIDs := c.onlinePusher.GetOnlinePushFailedUserIDs(ctx, msg, wsResults, &pushToUserIDs)
	//filter some user, like don not disturb or don't need offline push etc.
	needOfflinePushUserIDs, err = c.filterGroupMessageOfflinePush(ctx, groupID, msg, needOfflinePushUserIDs)
	if err != nil {
		return err
	}
	log.ZInfo(ctx, "filterGroupMessageOfflinePush end")

	// Use offline push messaging
	if len(needOfflinePushUserIDs) > 0 {
		c.asyncOfflinePush(ctx, needOfflinePushUserIDs, msg)
	}

	return nil
}

func (c *ConsumerHandler) asyncOfflinePush(ctx context.Context, needOfflinePushUserIDs []string, msg *sdkws.MsgData) {
	var offlinePushUserIDs []string
	err := c.webhookBeforeOfflinePush(ctx, &c.config.WebhooksConfig.BeforeOfflinePush, needOfflinePushUserIDs, msg, &offlinePushUserIDs)
	if err != nil {
		log.ZWarn(ctx, "webhookBeforeOfflinePush failed", err, "msg", msg)
		return
	}

	if len(offlinePushUserIDs) > 0 {
		needOfflinePushUserIDs = offlinePushUserIDs
	}
	if err := c.pushDatabase.MsgToOfflinePushMQ(ctx, conversationutil.GenConversationUniqueKeyForSingle(msg.SendID, msg.RecvID), needOfflinePushUserIDs, msg); err != nil {
		log.ZDebug(ctx, "Msg To OfflinePush MQ error", err, "needOfflinePushUserIDs",
			needOfflinePushUserIDs, "msg", msg)
		log.ZWarn(ctx, "Msg To OfflinePush MQ error", err, "needOfflinePushUserIDs length",
			len(needOfflinePushUserIDs), "msg", msg)
		prommetrics.GroupChatMsgProcessFailedCounter.Inc()
		return
	}
}

func (c *ConsumerHandler) groupMessagesHandler(ctx context.Context, groupID string, pushToUserIDs *[]string, msg *sdkws.MsgData) (err error) {
	if len(*pushToUserIDs) == 0 {
		*pushToUserIDs, err = c.groupLocalCache.GetGroupMemberIDs(ctx, groupID)
		if err != nil {
			return err
		}
		switch msg.ContentType {
		case constant.MemberQuitNotification:
			var tips sdkws.MemberQuitTips
			if unmarshalNotificationElem(msg.Content, &tips) != nil {
				return err
			}
			if err = c.DeleteMemberAndSetConversationSeq(ctx, groupID, []string{tips.QuitUser.UserID}); err != nil {
				log.ZError(ctx, "MemberQuitNotification DeleteMemberAndSetConversationSeq", err, "groupID", groupID, "userID", tips.QuitUser.UserID)
			}
			*pushToUserIDs = append(*pushToUserIDs, tips.QuitUser.UserID)
		case constant.MemberKickedNotification:
			var tips sdkws.MemberKickedTips
			if unmarshalNotificationElem(msg.Content, &tips) != nil {
				return err
			}
			kickedUsers := datautil.Slice(tips.KickedUserList, func(e *sdkws.GroupMemberFullInfo) string { return e.UserID })
			if err = c.DeleteMemberAndSetConversationSeq(ctx, groupID, kickedUsers); err != nil {
				log.ZError(ctx, "MemberKickedNotification DeleteMemberAndSetConversationSeq", err, "groupID", groupID, "userIDs", kickedUsers)
			}

			*pushToUserIDs = append(*pushToUserIDs, kickedUsers...)
		case constant.GroupDismissedNotification:
			if msgprocessor.IsNotification(msgprocessor.GetConversationIDByMsg(msg)) {
				var tips sdkws.GroupDismissedTips
				if unmarshalNotificationElem(msg.Content, &tips) != nil {
					return err
				}
				log.ZDebug(ctx, "GroupDismissedNotificationInfo****", "groupID", groupID, "num", len(*pushToUserIDs), "list", pushToUserIDs)
				if len(c.config.Share.IMAdminUserID) > 0 {
					ctx = mcontext.WithOpUserIDContext(ctx, c.config.Share.IMAdminUserID[0])
				}
				defer func(groupID string) {
					if err := c.groupClient.DismissGroup(ctx, groupID, true); err != nil {
						log.ZError(ctx, "DismissGroup Notification clear members", err, "groupID", groupID)
					}
				}(groupID)
			}
		}
	}
	return err
}

func (c *ConsumerHandler) offlinePushMsg(ctx context.Context, msg *sdkws.MsgData, offlinePushUserIDs []string) error {
	title, content, opts, err := c.getOfflinePushInfos(msg)
	if err != nil {
		log.ZError(ctx, "getOfflinePushInfos failed", err, "msg", msg)
		return err
	}
	err = c.offlinePusher.Push(ctx, offlinePushUserIDs, title, content, opts)
	if err != nil {
		prommetrics.MsgOfflinePushFailedCounter.Inc()
		return err
	}
	return nil
}

func (c *ConsumerHandler) filterGroupMessageOfflinePush(ctx context.Context, groupID string, msg *sdkws.MsgData,
	offlinePushUserIDs []string) (userIDs []string, err error) {
	needOfflinePushUserIDs, err := c.conversationClient.GetConversationOfflinePushUserIDs(ctx, conversationutil.GenGroupConversationID(groupID), offlinePushUserIDs)
	if err != nil {
		return nil, err
	}
	return needOfflinePushUserIDs, nil
}

func (c *ConsumerHandler) getOfflinePushInfos(msg *sdkws.MsgData) (title, content string, opts *options.Opts, err error) {
	type AtTextElem struct {
		Text       string   `json:"text,omitempty"`
		AtUserList []string `json:"atUserList,omitempty"`
		IsAtSelf   bool     `json:"isAtSelf"`
	}

	opts = &options.Opts{Signal: &options.Signal{ClientMsgID: msg.ClientMsgID}}
	if msg.OfflinePushInfo != nil {
		opts.IOSBadgeCount = msg.OfflinePushInfo.IOSBadgeCount
		opts.IOSPushSound = msg.OfflinePushInfo.IOSPushSound
		opts.Ex = msg.OfflinePushInfo.Ex
	}

	if msg.OfflinePushInfo != nil {
		title = msg.OfflinePushInfo.Title
		content = msg.OfflinePushInfo.Desc
	}
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
			title = constant.ContentType2PushContent[int64(msg.ContentType)]
		case constant.AtText:
			ac := AtTextElem{}
			_ = jsonutil.JsonStringToStruct(string(msg.Content), &ac)
		case constant.SignalingNotification:
			title = constant.ContentType2PushContent[constant.SignalMsg]
		default:
			title = constant.ContentType2PushContent[constant.Common]
		}
	}
	if content == "" {
		content = title
	}
	return
}

func (c *ConsumerHandler) DeleteMemberAndSetConversationSeq(ctx context.Context, groupID string, userIDs []string) error {
	conversationID := msgprocessor.GetConversationIDBySessionType(constant.ReadGroupChatType, groupID)
	maxSeq, err := c.msgClient.GetConversationMaxSeq(ctx, conversationID)
	if err != nil {
		return err
	}
	return c.conversationClient.SetConversationMaxSeq(ctx, conversationID, userIDs, maxSeq)
}

func unmarshalNotificationElem(bytes []byte, t any) error {
	var notification sdkws.NotificationElem
	if err := json.Unmarshal(bytes, &notification); err != nil {
		return err
	}
	return json.Unmarshal([]byte(notification.Detail), t)
}
