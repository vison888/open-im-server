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
// 负责处理所有与会话相关的RPC请求，包括会话的创建、查询、更新、排序等核心功能
type conversationServer struct {
	pbconversation.UnimplementedConversationServer

	// conversationDatabase 会话数据库控制器
	// 封装了会话数据的CRUD操作，支持MongoDB和Redis的混合存储
	conversationDatabase controller.ConversationDatabase

	// conversationNotificationSender 会话通知发送器
	// 负责发送会话变更相关的通知消息，如会话设置变更、未读数变更等
	conversationNotificationSender *ConversationNotificationSender

	// config 服务配置信息
	config *Config

	// 依赖的其他RPC服务客户端
	userClient  *rpcli.UserClient  // 用户服务客户端，用于获取用户信息
	msgClient   *rpcli.MsgClient   // 消息服务客户端，用于获取消息相关信息
	groupClient *rpcli.GroupClient // 群组服务客户端，用于获取群组信息
}

// Config 会话服务配置结构体
// 包含了会话服务运行所需的所有配置信息
type Config struct {
	RpcConfig          config.Conversation // RPC服务配置
	RedisConfig        config.Redis        // Redis缓存配置
	MongodbConfig      config.Mongo        // MongoDB数据库配置
	NotificationConfig config.Notification // 通知服务配置
	Share              config.Share        // 共享配置信息
	LocalCacheConfig   config.LocalCache   // 本地缓存配置
	Discovery          config.Discovery    // 服务发现配置
}

// Start 启动会话服务
// 初始化所有依赖组件并注册gRPC服务
func Start(ctx context.Context, config *Config, client discovery.SvcDiscoveryRegistry, server *grpc.Server) error {
	// 初始化MongoDB连接
	mgocli, err := mongoutil.NewMongoDB(ctx, config.MongodbConfig.Build())
	if err != nil {
		return err
	}

	// 初始化Redis连接
	rdb, err := redisutil.NewRedisClient(ctx, config.RedisConfig.Build())
	if err != nil {
		return err
	}

	// 创建会话数据库DAO
	conversationDB, err := mgo.NewConversationMongo(mgocli.GetDB())
	if err != nil {
		return err
	}

	// 获取其他服务的连接
	userConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.User)
	if err != nil {
		return err
	}
	groupConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.Group)
	if err != nil {
		return err
	}
	msgConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.Msg)
	if err != nil {
		return err
	}

	msgClient := rpcli.NewMsgClient(msgConn)

	// 初始化本地缓存
	localcache.InitLocalCache(&config.LocalCacheConfig)

	// 注册会话服务
	pbconversation.RegisterConversationServer(server, &conversationServer{
		conversationNotificationSender: NewConversationNotificationSender(&config.NotificationConfig, msgClient),
		conversationDatabase: controller.NewConversationDatabase(conversationDB,
			redis.NewConversationRedis(rdb, &config.LocalCacheConfig, redis.GetRocksCacheOptions(), conversationDB), mgocli.GetTx()),
		userClient:  rpcli.NewUserClient(userConn),
		groupClient: rpcli.NewGroupClient(groupConn),
		msgClient:   msgClient,
	})
	return nil
}

// GetConversation 获取单个会话信息
// 根据用户ID和会话ID查询会话详细信息
func (c *conversationServer) GetConversation(ctx context.Context, req *pbconversation.GetConversationReq) (*pbconversation.GetConversationResp, error) {
	// 从数据库查询会话信息
	conversations, err := c.conversationDatabase.FindConversations(ctx, req.OwnerUserID, []string{req.ConversationID})
	if err != nil {
		return nil, err
	}

	// 检查会话是否存在
	if len(conversations) < 1 {
		return nil, errs.ErrRecordNotFound.WrapMsg("conversation not found")
	}

	// 构造响应
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
// 返回用户的完整会话列表，不包含消息内容和排序
func (c *conversationServer) GetAllConversations(ctx context.Context, req *pbconversation.GetAllConversationsReq) (*pbconversation.GetAllConversationsResp, error) {
	// 查询用户的所有会话
	conversations, err := c.conversationDatabase.GetUserAllConversation(ctx, req.OwnerUserID)
	if err != nil {
		return nil, err
	}

	// 构造响应
	resp := &pbconversation.GetAllConversationsResp{Conversations: []*pbconversation.Conversation{}}
	resp.Conversations = convert.ConversationsDB2Pb(conversations)
	return resp, nil
}

func (c *conversationServer) GetConversations(ctx context.Context, req *pbconversation.GetConversationsReq) (*pbconversation.GetConversationsResp, error) {
	conversations, err := c.getConversations(ctx, req.OwnerUserID, req.ConversationIDs)
	if err != nil {
		return nil, err
	}
	return &pbconversation.GetConversationsResp{
		Conversations: conversations,
	}, nil
}

func (c *conversationServer) getConversations(ctx context.Context, ownerUserID string, conversationIDs []string) ([]*pbconversation.Conversation, error) {
	conversations, err := c.conversationDatabase.FindConversations(ctx, ownerUserID, conversationIDs)
	if err != nil {
		return nil, err
	}
	resp := &pbconversation.GetConversationsResp{Conversations: []*pbconversation.Conversation{}}
	resp.Conversations = convert.ConversationsDB2Pb(conversations)
	return convert.ConversationsDB2Pb(conversations), nil
}

func (c *conversationServer) SetConversation(ctx context.Context, req *pbconversation.SetConversationReq) (*pbconversation.SetConversationResp, error) {
	var conversation dbModel.Conversation
	if err := datautil.CopyStructFields(&conversation, req.Conversation); err != nil {
		return nil, err
	}
	err := c.conversationDatabase.SetUserConversations(ctx, req.Conversation.OwnerUserID, []*dbModel.Conversation{&conversation})
	if err != nil {
		return nil, err
	}
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
func (c *conversationServer) CreateSingleChatConversations(ctx context.Context,
	req *pbconversation.CreateSingleChatConversationsReq,
) (*pbconversation.CreateSingleChatConversationsResp, error) {
	switch req.ConversationType {
	case constant.SingleChatType:
		var conversation dbModel.Conversation
		conversation.ConversationID = req.ConversationID
		conversation.ConversationType = req.ConversationType
		conversation.OwnerUserID = req.SendID
		conversation.UserID = req.RecvID
		err := c.conversationDatabase.CreateConversation(ctx, []*dbModel.Conversation{&conversation})
		if err != nil {
			log.ZWarn(ctx, "create conversation failed", err, "conversation", conversation)
		}

		conversation2 := conversation
		conversation2.OwnerUserID = req.RecvID
		conversation2.UserID = req.SendID
		err = c.conversationDatabase.CreateConversation(ctx, []*dbModel.Conversation{&conversation2})
		if err != nil {
			log.ZWarn(ctx, "create conversation failed", err, "conversation2", conversation)
		}
	case constant.NotificationChatType:
		var conversation dbModel.Conversation
		conversation.ConversationID = req.ConversationID
		conversation.ConversationType = req.ConversationType
		conversation.OwnerUserID = req.RecvID
		conversation.UserID = req.SendID
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

func (c *conversationServer) conversationSort(conversations map[int64]string, resp *pbconversation.GetSortedConversationListResp, conversation_unreadCount map[string]int64, conversationMsg map[string]*pbconversation.ConversationElem) {
	keys := []int64{}
	for key := range conversations {
		keys = append(keys, key)
	}

	sort.Slice(keys, func(i, j int) bool {
		return keys[i] > keys[j]
	})
	index := 0

	cons := make([]*pbconversation.ConversationElem, len(conversations))
	for _, v := range keys {
		conversationID := conversations[v]
		conversationElem := conversationMsg[conversationID]
		conversationElem.UnreadCount = conversation_unreadCount[conversationID]
		cons[index] = conversationElem
		index++
	}
	resp.ConversationElems = append(resp.ConversationElems, cons...)
}

func (c *conversationServer) getConversationInfo(
	ctx context.Context,
	chatLogs map[string]*sdkws.MsgData,
	userID string) (map[string]*pbconversation.ConversationElem, error) {
	var (
		sendIDs         []string
		groupIDs        []string
		sendMap         = make(map[string]*sdkws.UserInfo)
		groupMap        = make(map[string]*sdkws.GroupInfo)
		conversationMsg = make(map[string]*pbconversation.ConversationElem)
	)
	for _, chatLog := range chatLogs {
		switch chatLog.SessionType {
		case constant.SingleChatType:
			if chatLog.SendID == userID {
				sendIDs = append(sendIDs, chatLog.RecvID)
			}
			sendIDs = append(sendIDs, chatLog.SendID)
		case constant.WriteGroupChatType, constant.ReadGroupChatType:
			groupIDs = append(groupIDs, chatLog.GroupID)
			sendIDs = append(sendIDs, chatLog.SendID)
		}
	}
	if len(sendIDs) != 0 {
		sendInfos, err := c.userClient.GetUsersInfo(ctx, sendIDs)
		if err != nil {
			return nil, err
		}
		for _, sendInfo := range sendInfos {
			sendMap[sendInfo.UserID] = sendInfo
		}
	}
	if len(groupIDs) != 0 {
		groupInfos, err := c.groupClient.GetGroupsInfo(ctx, groupIDs)
		if err != nil {
			return nil, err
		}
		for _, groupInfo := range groupInfos {
			groupMap[groupInfo.GroupID] = groupInfo
		}
	}
	for conversationID, chatLog := range chatLogs {
		pbchatLog := &pbconversation.ConversationElem{}
		msgInfo := &pbconversation.MsgInfo{}
		if err := datautil.CopyStructFields(msgInfo, chatLog); err != nil {
			return nil, err
		}
		switch chatLog.SessionType {
		case constant.SingleChatType:
			if chatLog.SendID == userID {
				if recv, ok := sendMap[chatLog.RecvID]; ok {
					msgInfo.FaceURL = recv.FaceURL
					msgInfo.SenderName = recv.Nickname
				}
				break
			}
			if send, ok := sendMap[chatLog.SendID]; ok {
				msgInfo.FaceURL = send.FaceURL
				msgInfo.SenderName = send.Nickname
			}
		case constant.WriteGroupChatType, constant.ReadGroupChatType:
			msgInfo.GroupID = chatLog.GroupID
			if group, ok := groupMap[chatLog.GroupID]; ok {
				msgInfo.GroupName = group.GroupName
				msgInfo.GroupFaceURL = group.FaceURL
				msgInfo.GroupMemberCount = group.MemberCount
				msgInfo.GroupType = group.GroupType
			}
			if send, ok := sendMap[chatLog.SendID]; ok {
				msgInfo.SenderName = send.Nickname
			}
		}
		pbchatLog.ConversationID = conversationID
		msgInfo.LatestMsgRecvTime = chatLog.SendTime
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
