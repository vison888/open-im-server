// Copyright © 2024 OpenIM. All rights reserved.
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

package rpccache

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache/cachekey"
	"github.com/openimsdk/open-im-server/v3/pkg/localcache"
	"github.com/openimsdk/open-im-server/v3/pkg/rpcli"
	pbconversation "github.com/openimsdk/protocol/conversation"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/utils/datautil"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/errgroup"
)

const (
	// conversationWorkerCount 会话并发处理的工作协程数量
	// 用于限制并发获取多个会话信息时的协程数量，避免过多协程导致性能问题
	conversationWorkerCount = 20
)

// NewConversationLocalCache 创建会话本地缓存实例
// 参数:
//   - client: 会话RPC客户端，用于调用远程会话服务
//   - localCache: 本地缓存配置，包含缓存槽数量、大小、TTL等配置
//   - cli: Redis客户端，用于订阅缓存失效通知
//
// 返回:
//   - *ConversationLocalCache: 会话本地缓存实例
//
// 功能:
//   - 初始化本地缓存配置（槽数量、槽大小、成功/失败TTL）
//   - 如果启用缓存，启动Redis订阅协程监听缓存失效事件
//   - 提供会话数据的本地缓存能力，减少RPC调用
func NewConversationLocalCache(client *rpcli.ConversationClient, localCache *config.LocalCache, cli redis.UniversalClient) *ConversationLocalCache {
	lc := localCache.Conversation
	// 输出缓存配置信息用于调试
	log.ZDebug(context.Background(), "ConversationLocalCache", "topic", lc.Topic, "slotNum", lc.SlotNum, "slotSize", lc.SlotSize, "enable", lc.Enable())

	x := &ConversationLocalCache{
		client: client,
		// 创建本地缓存实例，配置缓存参数
		local: localcache.New[[]byte](
			localcache.WithLocalSlotNum(lc.SlotNum),      // 本地缓存槽数量
			localcache.WithLocalSlotSize(lc.SlotSize),    // 每个槽的大小
			localcache.WithLinkSlotNum(lc.SlotNum),       // 链接槽数量
			localcache.WithLocalSuccessTTL(lc.Success()), // 成功缓存的TTL
			localcache.WithLocalFailedTTL(lc.Failed()),   // 失败缓存的TTL
		),
	}

	// 如果启用缓存，启动Redis订阅协程
	// 监听Redis发布的缓存失效消息，主动删除本地缓存
	if lc.Enable() {
		go subscriberRedisDeleteCache(context.Background(), cli, lc.Topic, x.local.DelLocal)
	}
	return x
}

// ConversationLocalCache 会话本地缓存结构体
// 提供会话相关数据的本地缓存功能，减少对远程RPC服务的调用频率
type ConversationLocalCache struct {
	client *rpcli.ConversationClient // 会话RPC客户端，用于获取远程数据
	local  localcache.Cache[[]byte]  // 本地缓存实例，存储序列化后的会话数据
}

// GetConversationIDs 获取用户的会话ID列表
// 参数:
//   - ctx: 上下文，用于控制请求超时和取消
//   - ownerUserID: 会话拥有者的用户ID
//
// 返回:
//   - []string: 会话ID列表
//   - error: 错误信息
//
// 功能:
//   - 首先尝试从本地缓存获取数据
//   - 如果缓存未命中，调用RPC获取数据并缓存
//   - 返回用户拥有的所有会话ID列表
func (c *ConversationLocalCache) GetConversationIDs(ctx context.Context, ownerUserID string) (val []string, err error) {
	resp, err := c.getConversationIDs(ctx, ownerUserID)
	if err != nil {
		return nil, err
	}
	return resp.ConversationIDs, nil
}

// getConversationIDs 内部方法：获取会话ID响应对象
// 参数:
//   - ctx: 上下文
//   - ownerUserID: 会话拥有者用户ID
//
// 返回:
//   - *pbconversation.GetConversationIDsResp: 完整的响应对象
//   - error: 错误信息
//
// 功能:
//   - 实现缓存逻辑：先查本地缓存，未命中则调用RPC
//   - 使用cachekey生成统一的缓存键
//   - 记录详细的调试日志
func (c *ConversationLocalCache) getConversationIDs(ctx context.Context, ownerUserID string) (val *pbconversation.GetConversationIDsResp, err error) {
	log.ZDebug(ctx, "ConversationLocalCache getConversationIDs req", "ownerUserID", ownerUserID)
	defer func() {
		if err == nil {
			log.ZDebug(ctx, "ConversationLocalCache getConversationIDs return", "ownerUserID", ownerUserID, "value", val)
		} else {
			log.ZError(ctx, "ConversationLocalCache getConversationIDs return", err, "ownerUserID", ownerUserID)
		}
	}()

	var cache cacheProto[pbconversation.GetConversationIDsResp]
	// 从本地缓存获取数据，如果未命中则执行回调函数获取数据
	return cache.Unmarshal(c.local.Get(ctx, cachekey.GetConversationIDsKey(ownerUserID), func(ctx context.Context) ([]byte, error) {
		log.ZDebug(ctx, "ConversationLocalCache getConversationIDs rpc", "ownerUserID", ownerUserID)
		// 调用RPC获取数据并序列化
		return cache.Marshal(c.client.ConversationClient.GetConversationIDs(ctx, &pbconversation.GetConversationIDsReq{UserID: ownerUserID}))
	}))
}

// GetConversation 获取指定会话的详细信息
// 参数:
//   - ctx: 上下文
//   - userID: 用户ID
//   - conversationID: 会话ID
//
// 返回:
//   - *pbconversation.Conversation: 会话详细信息
//   - error: 错误信息
//
// 功能:
//   - 获取单个会话的完整信息（包括会话设置、最新消息等）
//   - 优先从本地缓存获取，缓存未命中时调用RPC
//   - 适用于会话详情页面、消息发送前的会话信息获取等场景
func (c *ConversationLocalCache) GetConversation(ctx context.Context, userID, conversationID string) (val *pbconversation.Conversation, err error) {
	log.ZDebug(ctx, "ConversationLocalCache GetConversation req", "userID", userID, "conversationID", conversationID)
	defer func() {
		if err == nil {
			log.ZDebug(ctx, "ConversationLocalCache GetConversation return", "userID", userID, "conversationID", conversationID, "value", val)
		} else {
			log.ZWarn(ctx, "ConversationLocalCache GetConversation return", err, "userID", userID, "conversationID", conversationID)
		}
	}()

	var cache cacheProto[pbconversation.Conversation]
	// 使用用户ID和会话ID生成缓存键
	return cache.Unmarshal(c.local.Get(ctx, cachekey.GetConversationKey(userID, conversationID), func(ctx context.Context) ([]byte, error) {
		log.ZDebug(ctx, "ConversationLocalCache GetConversation rpc", "userID", userID, "conversationID", conversationID)
		// 调用RPC客户端获取会话信息
		return cache.Marshal(c.client.GetConversation(ctx, conversationID, userID))
	}))
}

// GetSingleConversationRecvMsgOpt 获取单聊会话的消息接收选项
// 参数:
//   - ctx: 上下文
//   - userID: 用户ID
//   - conversationID: 会话ID
//
// 返回:
//   - int32: 消息接收选项 (0=正常接收, 1=不接收消息, 2=接收但不提醒)
//   - error: 错误信息
//
// 功能:
//   - 专门用于获取会话的消息接收设置
//   - 在消息推送时用于判断是否需要推送给用户
//   - 复用GetConversation的缓存，避免重复RPC调用
func (c *ConversationLocalCache) GetSingleConversationRecvMsgOpt(ctx context.Context, userID, conversationID string) (int32, error) {
	conv, err := c.GetConversation(ctx, userID, conversationID)
	if err != nil {
		return 0, err
	}
	return conv.RecvMsgOpt, nil
}

// GetConversations 批量获取多个会话的详细信息
// 参数:
//   - ctx: 上下文
//   - ownerUserID: 会话拥有者用户ID
//   - conversationIDs: 会话ID列表
//
// 返回:
//   - []*pbconversation.Conversation: 会话信息列表
//   - error: 错误信息
//
// 功能:
//   - 并发获取多个会话信息，提高性能
//   - 使用errgroup控制并发数量，避免过多协程
//   - 自动过滤不存在的会话（返回ErrRecordNotFound）
//   - 适用于会话列表页面的批量数据加载
func (c *ConversationLocalCache) GetConversations(ctx context.Context, ownerUserID string, conversationIDs []string) ([]*pbconversation.Conversation, error) {
	var (
		conversations     = make([]*pbconversation.Conversation, 0, len(conversationIDs))
		conversationsChan = make(chan *pbconversation.Conversation, len(conversationIDs))
	)

	// 创建错误组，用于并发控制和错误收集
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(conversationWorkerCount) // 限制并发协程数量

	// 并发获取每个会话的信息
	for _, conversationID := range conversationIDs {
		conversationID := conversationID // 避免闭包变量问题
		g.Go(func() error {
			conversation, err := c.GetConversation(ctx, ownerUserID, conversationID)
			if err != nil {
				// 如果会话不存在，跳过该会话，不返回错误
				if errs.ErrRecordNotFound.Is(err) {
					return nil
				}
				return err
			}
			conversationsChan <- conversation
			return nil
		})
	}

	// 等待所有协程完成
	if err := g.Wait(); err != nil {
		return nil, err
	}

	// 收集结果
	close(conversationsChan)
	for conversation := range conversationsChan {
		conversations = append(conversations, conversation)
	}
	return conversations, nil
}

// getConversationNotReceiveMessageUserIDs 内部方法：获取不接收消息的用户ID列表
// 参数:
//   - ctx: 上下文
//   - conversationID: 会话ID
//
// 返回:
//   - *pbconversation.GetConversationNotReceiveMessageUserIDsResp: 响应对象
//   - error: 错误信息
//
// 功能:
//   - 获取在指定会话中设置了"不接收消息"的用户列表
//   - 用于消息推送时过滤不需要接收消息的用户
//   - 实现缓存机制减少RPC调用
func (c *ConversationLocalCache) getConversationNotReceiveMessageUserIDs(ctx context.Context, conversationID string) (val *pbconversation.GetConversationNotReceiveMessageUserIDsResp, err error) {
	log.ZDebug(ctx, "ConversationLocalCache getConversationNotReceiveMessageUserIDs req", "conversationID", conversationID)
	defer func() {
		if err == nil {
			log.ZDebug(ctx, "ConversationLocalCache getConversationNotReceiveMessageUserIDs return", "conversationID", conversationID, "value", val)
		} else {
			log.ZError(ctx, "ConversationLocalCache getConversationNotReceiveMessageUserIDs return", err, "conversationID", conversationID)
		}
	}()

	var cache cacheProto[pbconversation.GetConversationNotReceiveMessageUserIDsResp]
	return cache.Unmarshal(c.local.Get(ctx, cachekey.GetConversationNotReceiveMessageUserIDsKey(conversationID), func(ctx context.Context) ([]byte, error) {
		log.ZDebug(ctx, "ConversationLocalCache getConversationNotReceiveMessageUserIDs rpc", "conversationID", conversationID)
		return cache.Marshal(c.client.ConversationClient.GetConversationNotReceiveMessageUserIDs(ctx, &pbconversation.GetConversationNotReceiveMessageUserIDsReq{ConversationID: conversationID}))
	}))
}

// GetConversationNotReceiveMessageUserIDs 获取不接收消息的用户ID列表
// 参数:
//   - ctx: 上下文
//   - conversationID: 会话ID
//
// 返回:
//   - []string: 用户ID列表
//   - error: 错误信息
//
// 功能:
//   - 返回在指定会话中设置了消息接收选项为"不接收"的用户ID列表
//   - 主要用于消息推送逻辑，避免向这些用户推送消息
//   - 提取响应对象中的用户ID列表
func (c *ConversationLocalCache) GetConversationNotReceiveMessageUserIDs(ctx context.Context, conversationID string) ([]string, error) {
	res, err := c.getConversationNotReceiveMessageUserIDs(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	return res.UserIDs, nil
}

// GetConversationNotReceiveMessageUserIDMap 获取不接收消息的用户ID映射表
// 参数:
//   - ctx: 上下文
//   - conversationID: 会话ID
//
// 返回:
//   - map[string]struct{}: 用户ID映射表，键为用户ID，值为空结构体
//   - error: 错误信息
//
// 功能:
//   - 将用户ID列表转换为映射表，便于快速查找
//   - 在消息推送时可以通过O(1)时间复杂度判断用户是否在黑名单中
//   - 使用空结构体节省内存空间
func (c *ConversationLocalCache) GetConversationNotReceiveMessageUserIDMap(ctx context.Context, conversationID string) (map[string]struct{}, error) {
	res, err := c.getConversationNotReceiveMessageUserIDs(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	// 使用工具函数将切片转换为集合
	return datautil.SliceSet(res.UserIDs), nil
}
