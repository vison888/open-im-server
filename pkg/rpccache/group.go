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
	"github.com/openimsdk/protocol/group"
	"github.com/openimsdk/tools/utils/datautil"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/localcache"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/redis/go-redis/v9"
)

// NewGroupLocalCache 创建群组本地缓存实例
// 参数:
//   - client: 群组RPC客户端，用于调用远程群组服务
//   - localCache: 本地缓存配置，包含缓存槽数量、大小、TTL等配置
//   - cli: Redis客户端，用于订阅缓存失效通知
//
// 返回:
//   - *GroupLocalCache: 群组本地缓存实例
//
// 功能:
//   - 初始化群组相关数据的本地缓存
//   - 配置缓存参数（槽数量、槽大小、成功/失败TTL）
//   - 如果启用缓存，启动Redis订阅协程监听缓存失效事件
//   - 提供群组信息、成员信息等数据的高效缓存访问
func NewGroupLocalCache(client *rpcli.GroupClient, localCache *config.LocalCache, cli redis.UniversalClient) *GroupLocalCache {
	lc := localCache.Group
	log.ZDebug(context.Background(), "GroupLocalCache", "topic", lc.Topic, "slotNum", lc.SlotNum, "slotSize", lc.SlotSize, "enable", lc.Enable())
	x := &GroupLocalCache{
		client: client,
		local: localcache.New[[]byte](
			localcache.WithLocalSlotNum(lc.SlotNum),
			localcache.WithLocalSlotSize(lc.SlotSize),
			localcache.WithLinkSlotNum(lc.SlotNum),
			localcache.WithLocalSuccessTTL(lc.Success()),
			localcache.WithLocalFailedTTL(lc.Failed()),
		),
	}
	if lc.Enable() {
		go subscriberRedisDeleteCache(context.Background(), cli, lc.Topic, x.local.DelLocal)
	}
	return x
}

// GroupLocalCache 群组本地缓存结构体
// 提供群组相关数据的本地缓存功能，包括群组信息、成员信息等
// 减少对远程RPC服务的调用频率，提升系统性能
type GroupLocalCache struct {
	client *rpcli.GroupClient
	local  localcache.Cache[[]byte]
}

// getGroupMemberIDs 内部方法：获取群组成员ID列表响应对象
// 参数:
//   - ctx: 上下文
//   - groupID: 群组ID
//
// 返回:
//   - *group.GetGroupMemberUserIDsResp: 包含成员ID列表的响应对象
//   - error: 错误信息
//
// 功能:
//   - 获取指定群组的所有成员用户ID列表
//   - 实现缓存机制，减少RPC调用
//   - 用于群消息推送、权限验证等场景
//   - 记录详细的调试和错误日志
func (g *GroupLocalCache) getGroupMemberIDs(ctx context.Context, groupID string) (val *group.GetGroupMemberUserIDsResp, err error) {
	log.ZDebug(ctx, "GroupLocalCache getGroupMemberIDs req", "groupID", groupID)
	defer func() {
		if err == nil {
			log.ZDebug(ctx, "GroupLocalCache getGroupMemberIDs return", "groupID", groupID, "value", val)
		} else {
			log.ZError(ctx, "GroupLocalCache getGroupMemberIDs return", err, "groupID", groupID)
		}
	}()
	var cache cacheProto[group.GetGroupMemberUserIDsResp]
	return cache.Unmarshal(g.local.Get(ctx, cachekey.GetGroupMemberIDsKey(groupID), func(ctx context.Context) ([]byte, error) {
		log.ZDebug(ctx, "GroupLocalCache getGroupMemberIDs rpc", "groupID", groupID)
		return cache.Marshal(g.client.GroupClient.GetGroupMemberUserIDs(ctx, &group.GetGroupMemberUserIDsReq{GroupID: groupID}))
	}))
}

// GetGroupMember 获取群组成员的详细信息
// 参数:
//   - ctx: 上下文
//   - groupID: 群组ID
//   - userID: 用户ID
//
// 返回:
//   - *sdkws.GroupMemberFullInfo: 群组成员完整信息
//   - error: 错误信息
//
// 功能:
//   - 获取指定用户在指定群组中的详细信息
//   - 包含成员角色、加入时间、昵称等信息
//   - 优先从本地缓存获取，缓存未命中时调用RPC
//   - 适用于群成员信息展示、权限验证等场景
func (g *GroupLocalCache) GetGroupMember(ctx context.Context, groupID, userID string) (val *sdkws.GroupMemberFullInfo, err error) {
	log.ZDebug(ctx, "GroupLocalCache GetGroupInfo req", "groupID", groupID, "userID", userID)
	defer func() {
		if err == nil {
			log.ZDebug(ctx, "GroupLocalCache GetGroupInfo return", "groupID", groupID, "userID", userID, "value", val)
		} else {
			log.ZError(ctx, "GroupLocalCache GetGroupInfo return", err, "groupID", groupID, "userID", userID)
		}
	}()
	var cache cacheProto[sdkws.GroupMemberFullInfo]
	return cache.Unmarshal(g.local.Get(ctx, cachekey.GetGroupMemberInfoKey(groupID, userID), func(ctx context.Context) ([]byte, error) {
		log.ZDebug(ctx, "GroupLocalCache GetGroupInfo rpc", "groupID", groupID, "userID", userID)
		return cache.Marshal(g.client.GetGroupMemberCache(ctx, groupID, userID))
	}))
}

// GetGroupInfo 获取群组的基本信息
// 参数:
//   - ctx: 上下文
//   - groupID: 群组ID
//
// 返回:
//   - *sdkws.GroupInfo: 群组基本信息
//   - error: 错误信息
//
// 功能:
//   - 获取群组的基本信息（群名、头像、创建时间等）
//   - 优先从本地缓存获取，缓存未命中时调用RPC
//   - 适用于群聊界面、群组列表等场景
//   - 高频访问数据，缓存效果显著
func (g *GroupLocalCache) GetGroupInfo(ctx context.Context, groupID string) (val *sdkws.GroupInfo, err error) {
	log.ZDebug(ctx, "GroupLocalCache GetGroupInfo req", "groupID", groupID)
	defer func() {
		if err == nil {
			log.ZDebug(ctx, "GroupLocalCache GetGroupInfo return", "groupID", groupID, "value", val)
		} else {
			log.ZError(ctx, "GroupLocalCache GetGroupInfo return", err, "groupID", groupID)
		}
	}()
	var cache cacheProto[sdkws.GroupInfo]
	return cache.Unmarshal(g.local.Get(ctx, cachekey.GetGroupInfoKey(groupID), func(ctx context.Context) ([]byte, error) {
		log.ZDebug(ctx, "GroupLocalCache GetGroupInfo rpc", "groupID", groupID)
		return cache.Marshal(g.client.GetGroupInfoCache(ctx, groupID))
	}))
}

// GetGroupMemberIDs 获取群组成员ID列表
// 参数:
//   - ctx: 上下文
//   - groupID: 群组ID
//
// 返回:
//   - []string: 成员用户ID列表
//   - error: 错误信息
//
// 功能:
//   - 获取指定群组的所有成员用户ID列表
//   - 用于群消息推送时确定接收者列表
//   - 提取响应对象中的用户ID列表
//   - 复用getGroupMemberIDs的缓存机制
func (g *GroupLocalCache) GetGroupMemberIDs(ctx context.Context, groupID string) ([]string, error) {
	res, err := g.getGroupMemberIDs(ctx, groupID)
	if err != nil {
		return nil, err
	}
	return res.UserIDs, nil
}

// GetGroupMemberIDMap 获取群组成员ID映射表
// 参数:
//   - ctx: 上下文
//   - groupID: 群组ID
//
// 返回:
//   - map[string]struct{}: 成员ID映射表，键为用户ID，值为空结构体
//   - error: 错误信息
//
// 功能:
//   - 将成员ID列表转换为映射表，便于快速查找
//   - 在群消息权限验证时可以通过O(1)时间复杂度判断用户是否为群成员
//   - 使用空结构体节省内存空间
//   - 适用于需要频繁检查成员身份的场景
func (g *GroupLocalCache) GetGroupMemberIDMap(ctx context.Context, groupID string) (map[string]struct{}, error) {
	res, err := g.getGroupMemberIDs(ctx, groupID)
	if err != nil {
		return nil, err
	}
	return datautil.SliceSet(res.UserIDs), nil
}

// GetGroupInfos 批量获取多个群组的基本信息
// 参数:
//   - ctx: 上下文
//   - groupIDs: 群组ID列表
//
// 返回:
//   - []*sdkws.GroupInfo: 群组信息列表
//   - error: 错误信息
//
// 功能:
//   - 批量获取多个群组的基本信息
//   - 自动过滤不存在的群组（返回ErrRecordNotFound）
//   - 适用于用户群组列表页面的批量数据加载
//   - 每个群组信息都会利用本地缓存机制
func (g *GroupLocalCache) GetGroupInfos(ctx context.Context, groupIDs []string) ([]*sdkws.GroupInfo, error) {
	groupInfos := make([]*sdkws.GroupInfo, 0, len(groupIDs))
	for _, groupID := range groupIDs {
		groupInfo, err := g.GetGroupInfo(ctx, groupID)
		if err != nil {
			if errs.ErrRecordNotFound.Is(err) {
				continue
			}
			return nil, err
		}
		groupInfos = append(groupInfos, groupInfo)
	}
	return groupInfos, nil
}

// GetGroupMembers 批量获取群组成员的详细信息
// 参数:
//   - ctx: 上下文
//   - groupID: 群组ID
//   - userIDs: 用户ID列表
//
// 返回:
//   - []*sdkws.GroupMemberFullInfo: 群组成员信息列表
//   - error: 错误信息
//
// 功能:
//   - 批量获取指定用户在指定群组中的详细信息
//   - 自动过滤不存在的成员（返回ErrRecordNotFound）
//   - 适用于群成员列表页面、@功能等场景
//   - 每个成员信息都会利用本地缓存机制
func (g *GroupLocalCache) GetGroupMembers(ctx context.Context, groupID string, userIDs []string) ([]*sdkws.GroupMemberFullInfo, error) {
	members := make([]*sdkws.GroupMemberFullInfo, 0, len(userIDs))
	for _, userID := range userIDs {
		member, err := g.GetGroupMember(ctx, groupID, userID)
		if err != nil {
			if errs.ErrRecordNotFound.Is(err) {
				continue
			}
			return nil, err
		}
		members = append(members, member)
	}
	return members, nil
}

// GetGroupMemberInfoMap 获取群组成员信息映射表
// 参数:
//   - ctx: 上下文
//   - groupID: 群组ID
//   - userIDs: 用户ID列表
//
// 返回:
//   - map[string]*sdkws.GroupMemberFullInfo: 成员信息映射表，键为用户ID，值为成员信息
//   - error: 错误信息
//
// 功能:
//   - 批量获取群组成员信息并组织为映射表结构
//   - 便于根据用户ID快速查找对应的成员信息
//   - 自动过滤不存在的成员（返回ErrRecordNotFound）
//   - 适用于需要频繁根据用户ID查找成员信息的场景
//   - 每个成员信息都会利用本地缓存机制
func (g *GroupLocalCache) GetGroupMemberInfoMap(ctx context.Context, groupID string, userIDs []string) (map[string]*sdkws.GroupMemberFullInfo, error) {
	members := make(map[string]*sdkws.GroupMemberFullInfo)
	for _, userID := range userIDs {
		member, err := g.GetGroupMember(ctx, groupID, userID)
		if err != nil {
			if errs.ErrRecordNotFound.Is(err) {
				continue
			}
			return nil, err
		}
		members[userID] = member
	}
	return members, nil
}
