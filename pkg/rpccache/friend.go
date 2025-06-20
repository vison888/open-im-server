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

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache/cachekey"
	"github.com/openimsdk/open-im-server/v3/pkg/rpcli"
	"github.com/openimsdk/protocol/relation"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/localcache"
	"github.com/openimsdk/tools/log"
	"github.com/redis/go-redis/v9"
)

// NewFriendLocalCache 创建好友关系本地缓存实例
// 参数:
//   - client: 关系RPC客户端，用于调用远程好友关系服务
//   - localCache: 本地缓存配置，包含缓存槽数量、大小、TTL等配置
//   - cli: Redis客户端，用于订阅缓存失效通知
//
// 返回:
//   - *FriendLocalCache: 好友关系本地缓存实例
//
// 功能:
//   - 初始化好友关系数据的本地缓存
//   - 配置缓存参数（槽数量、槽大小、成功/失败TTL）
//   - 如果启用缓存，启动Redis订阅协程监听缓存失效事件
//   - 提供好友关系、黑名单等数据的高效缓存访问
func NewFriendLocalCache(client *rpcli.RelationClient, localCache *config.LocalCache, cli redis.UniversalClient) *FriendLocalCache {
	lc := localCache.Friend
	// 输出好友关系缓存配置信息用于调试
	log.ZDebug(context.Background(), "FriendLocalCache", "topic", lc.Topic, "slotNum", lc.SlotNum, "slotSize", lc.SlotSize, "enable", lc.Enable())

	x := &FriendLocalCache{
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

// FriendLocalCache 好友关系本地缓存结构体
// 提供好友关系相关数据的本地缓存功能，包括好友关系验证、黑名单检查等
// 减少对远程RPC服务的调用频率，提升系统性能
type FriendLocalCache struct {
	client *rpcli.RelationClient    // 关系RPC客户端，用于获取远程数据
	local  localcache.Cache[[]byte] // 本地缓存实例，存储序列化后的关系数据
}

// IsFriend 检查两个用户是否为好友关系
// 参数:
//   - ctx: 上下文
//   - possibleFriendUserID: 可能的好友用户ID
//   - userID: 当前用户ID
//
// 返回:
//   - bool: 是否为好友关系
//   - error: 错误信息
//
// 功能:
//   - 检查possibleFriendUserID是否在userID的好友列表中
//   - 优先从本地缓存获取结果，缓存未命中时调用RPC
//   - 适用于消息发送权限验证、好友状态显示等场景
//   - 提取响应对象中的好友关系标识
func (f *FriendLocalCache) IsFriend(ctx context.Context, possibleFriendUserID, userID string) (val bool, err error) {
	res, err := f.isFriend(ctx, possibleFriendUserID, userID)
	if err != nil {
		return false, err
	}
	return res.InUser1Friends, nil
}

// isFriend 内部方法：获取好友关系检查响应对象
// 参数:
//   - ctx: 上下文
//   - possibleFriendUserID: 可能的好友用户ID
//   - userID: 当前用户ID
//
// 返回:
//   - *relation.IsFriendResp: 好友关系检查响应对象
//   - error: 错误信息
//
// 功能:
//   - 实现好友关系检查的缓存逻辑
//   - 使用GetLink方法实现关联缓存，当好友列表变更时自动失效
//   - 记录详细的调试和错误日志
//   - 缓存键关联到好友ID列表，保证数据一致性
func (f *FriendLocalCache) isFriend(ctx context.Context, possibleFriendUserID, userID string) (val *relation.IsFriendResp, err error) {
	log.ZDebug(ctx, "FriendLocalCache isFriend req", "possibleFriendUserID", possibleFriendUserID, "userID", userID)
	defer func() {
		if err == nil {
			log.ZDebug(ctx, "FriendLocalCache isFriend return", "possibleFriendUserID", possibleFriendUserID, "userID", userID, "value", val)
		} else {
			log.ZError(ctx, "FriendLocalCache isFriend return", err, "possibleFriendUserID", possibleFriendUserID, "userID", userID)
		}
	}()

	var cache cacheProto[relation.IsFriendResp]
	// 使用GetLink方法实现关联缓存，当好友列表变更时会自动失效此缓存
	return cache.Unmarshal(f.local.GetLink(ctx, cachekey.GetIsFriendKey(possibleFriendUserID, userID), func(ctx context.Context) ([]byte, error) {
		log.ZDebug(ctx, "FriendLocalCache isFriend rpc", "possibleFriendUserID", possibleFriendUserID, "userID", userID)
		// 调用RPC检查好友关系，注意参数顺序：userID1是要检查的用户，userID2是目标用户
		return cache.Marshal(f.client.FriendClient.IsFriend(ctx, &relation.IsFriendReq{UserID1: userID, UserID2: possibleFriendUserID}))
	}, cachekey.GetFriendIDsKey(possibleFriendUserID))) // 关联到好友ID列表缓存键
}

// IsBlack 检查用户是否在黑名单中
// 参数:
//   - ctx: 上下文
//   - possibleBlackUserID: 可能被拉黑的用户ID
//   - userID: 当前用户ID（黑名单拥有者）
//
// 返回:
//   - bool: 是否在黑名单中
//   - error: 错误信息
//
// 功能:
//   - 检查possibleBlackUserID是否在userID的黑名单中
//   - 优先从本地缓存获取结果，缓存未命中时调用RPC
//   - 适用于消息发送权限验证、用户交互限制等场景
//   - 提取响应对象中的黑名单状态标识
func (f *FriendLocalCache) IsBlack(ctx context.Context, possibleBlackUserID, userID string) (val bool, err error) {
	res, err := f.isBlack(ctx, possibleBlackUserID, userID)
	if err != nil {
		return false, err
	}
	return res.InUser2Blacks, nil
}

// isBlack 内部方法：获取黑名单检查响应对象
// 参数:
//   - ctx: 上下文
//   - possibleBlackUserID: 可能被拉黑的用户ID
//   - userID: 当前用户ID（黑名单拥有者）
//
// 返回:
//   - *relation.IsBlackResp: 黑名单检查响应对象
//   - error: 错误信息
//
// 功能:
//   - 实现黑名单检查的缓存逻辑
//   - 使用GetLink方法实现关联缓存，当黑名单变更时自动失效
//   - 记录详细的调试和错误日志
//   - 缓存键关联到黑名单ID列表，保证数据一致性
func (f *FriendLocalCache) isBlack(ctx context.Context, possibleBlackUserID, userID string) (val *relation.IsBlackResp, err error) {
	log.ZDebug(ctx, "FriendLocalCache isBlack req", "possibleBlackUserID", possibleBlackUserID, "userID", userID)
	defer func() {
		if err == nil {
			log.ZDebug(ctx, "FriendLocalCache isBlack return", "possibleBlackUserID", possibleBlackUserID, "userID", userID, "value", val)
		} else {
			log.ZError(ctx, "FriendLocalCache isBlack return", err, "possibleBlackUserID", possibleBlackUserID, "userID", userID)
		}
	}()

	var cache cacheProto[relation.IsBlackResp]
	// 使用GetLink方法实现关联缓存，当黑名单变更时会自动失效此缓存
	return cache.Unmarshal(f.local.GetLink(ctx, cachekey.GetIsBlackIDsKey(possibleBlackUserID, userID), func(ctx context.Context) ([]byte, error) {
		log.ZDebug(ctx, "FriendLocalCache IsBlack rpc", "possibleBlackUserID", possibleBlackUserID, "userID", userID)
		// 调用RPC检查黑名单状态，注意参数顺序：userID1是被检查的用户，userID2是黑名单拥有者
		return cache.Marshal(f.client.FriendClient.IsBlack(ctx, &relation.IsBlackReq{UserID1: possibleBlackUserID, UserID2: userID}))
	}, cachekey.GetBlackIDsKey(userID))) // 关联到黑名单ID列表缓存键
}
