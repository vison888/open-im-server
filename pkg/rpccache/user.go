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

	"github.com/openimsdk/open-im-server/v3/pkg/rpcli"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache/cachekey"
	"github.com/openimsdk/open-im-server/v3/pkg/localcache"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/protocol/user"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/redis/go-redis/v9"
)

// NewUserLocalCache 创建用户信息本地缓存实例
// 参数:
//   - client: 用户RPC客户端，用于调用远程用户服务
//   - localCache: 本地缓存配置，包含缓存槽数量、大小、TTL等配置
//   - cli: Redis客户端，用于订阅缓存失效通知
//
// 返回:
//   - *UserLocalCache: 用户信息本地缓存实例
//
// 功能:
//   - 初始化用户相关数据的本地缓存
//   - 配置缓存参数（槽数量、槽大小、成功/失败TTL）
//   - 如果启用缓存，启动Redis订阅协程监听缓存失效事件
//   - 提供用户基本信息、全局消息接收设置等数据的高效缓存访问
func NewUserLocalCache(client *rpcli.UserClient, localCache *config.LocalCache, cli redis.UniversalClient) *UserLocalCache {
	lc := localCache.User
	// 输出用户缓存配置信息用于调试
	log.ZDebug(context.Background(), "UserLocalCache", "topic", lc.Topic, "slotNum", lc.SlotNum, "slotSize", lc.SlotSize, "enable", lc.Enable())

	x := &UserLocalCache{
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

// UserLocalCache 用户信息本地缓存结构体
// 提供用户相关数据的本地缓存功能，包括用户基本信息、全局设置等
// 减少对远程RPC服务的调用频率，提升系统性能
type UserLocalCache struct {
	client *rpcli.UserClient        // 用户RPC客户端，用于获取远程数据
	local  localcache.Cache[[]byte] // 本地缓存实例，存储序列化后的用户数据
}

// GetUserInfo 获取用户基本信息
// 参数:
//   - ctx: 上下文
//   - userID: 用户ID
//
// 返回:
//   - *sdkws.UserInfo: 用户基本信息
//   - error: 错误信息
//
// 功能:
//   - 获取用户的基本信息（昵称、头像、性别等）
//   - 优先从本地缓存获取，缓存未命中时调用RPC
//   - 适用于消息发送者信息显示、用户列表等场景
//   - 高频访问数据，缓存效果显著
func (u *UserLocalCache) GetUserInfo(ctx context.Context, userID string) (val *sdkws.UserInfo, err error) {
	log.ZDebug(ctx, "UserLocalCache GetUserInfo req", "userID", userID)
	defer func() {
		if err == nil {
			log.ZDebug(ctx, "UserLocalCache GetUserInfo return", "value", val)
		} else {
			log.ZError(ctx, "UserLocalCache GetUserInfo return", err)
		}
	}()

	var cache cacheProto[sdkws.UserInfo]
	// 从本地缓存获取数据，如果未命中则执行回调函数获取数据
	return cache.Unmarshal(u.local.Get(ctx, cachekey.GetUserInfoKey(userID), func(ctx context.Context) ([]byte, error) {
		log.ZDebug(ctx, "UserLocalCache GetUserInfo rpc", "userID", userID)
		// 调用RPC获取用户信息并序列化
		return cache.Marshal(u.client.GetUserInfo(ctx, userID))
	}))
}

// GetUserGlobalMsgRecvOpt 获取用户全局消息接收选项
// 参数:
//   - ctx: 上下文
//   - userID: 用户ID
//
// 返回:
//   - int32: 全局消息接收选项 (0=正常接收, 1=不接收消息, 2=接收但不提醒)
//   - error: 错误信息
//
// 功能:
//   - 获取用户的全局消息接收设置
//   - 在消息推送时用于判断是否需要推送给用户
//   - 提取响应对象中的全局接收选项值
//   - 复用getUserGlobalMsgRecvOpt的缓存机制
func (u *UserLocalCache) GetUserGlobalMsgRecvOpt(ctx context.Context, userID string) (val int32, err error) {
	resp, err := u.getUserGlobalMsgRecvOpt(ctx, userID)
	if err != nil {
		return 0, err
	}
	return resp.GlobalRecvMsgOpt, nil
}

// getUserGlobalMsgRecvOpt 内部方法：获取用户全局消息接收选项响应对象
// 参数:
//   - ctx: 上下文
//   - userID: 用户ID
//
// 返回:
//   - *user.GetGlobalRecvMessageOptResp: 全局消息接收选项响应对象
//   - error: 错误信息
//
// 功能:
//   - 实现用户全局消息接收选项的缓存逻辑
//   - 优先从本地缓存获取，缓存未命中时调用RPC
//   - 记录详细的调试和错误日志
//   - 用于消息推送逻辑中的全局权限控制
func (u *UserLocalCache) getUserGlobalMsgRecvOpt(ctx context.Context, userID string) (val *user.GetGlobalRecvMessageOptResp, err error) {
	log.ZDebug(ctx, "UserLocalCache getUserGlobalMsgRecvOpt req", "userID", userID)
	defer func() {
		if err == nil {
			log.ZDebug(ctx, "UserLocalCache getUserGlobalMsgRecvOpt return", "value", val)
		} else {
			log.ZError(ctx, "UserLocalCache getUserGlobalMsgRecvOpt return", err)
		}
	}()

	var cache cacheProto[user.GetGlobalRecvMessageOptResp]
	// 使用用户ID生成全局消息接收选项的缓存键
	return cache.Unmarshal(u.local.Get(ctx, cachekey.GetUserGlobalRecvMsgOptKey(userID), func(ctx context.Context) ([]byte, error) {
		log.ZDebug(ctx, "UserLocalCache GetUserGlobalMsgRecvOpt rpc", "userID", userID)
		// 调用RPC获取用户全局消息接收选项
		return cache.Marshal(u.client.UserClient.GetGlobalRecvMessageOpt(ctx, &user.GetGlobalRecvMessageOptReq{UserID: userID}))
	}))
}

// GetUsersInfo 批量获取多个用户的基本信息
// 参数:
//   - ctx: 上下文
//   - userIDs: 用户ID列表
//
// 返回:
//   - []*sdkws.UserInfo: 用户信息列表
//   - error: 错误信息
//
// 功能:
//   - 批量获取多个用户的基本信息
//   - 自动过滤不存在的用户（返回ErrRecordNotFound）
//   - 适用于群成员信息显示、消息发送者信息批量获取等场景
//   - 每个用户信息都会利用本地缓存机制
//   - 记录用户信息未找到的警告日志
func (u *UserLocalCache) GetUsersInfo(ctx context.Context, userIDs []string) ([]*sdkws.UserInfo, error) {
	users := make([]*sdkws.UserInfo, 0, len(userIDs))
	// 逐个获取用户信息
	for _, userID := range userIDs {
		user, err := u.GetUserInfo(ctx, userID)
		if err != nil {
			// 如果用户不存在，记录警告日志但不返回错误
			if errs.ErrRecordNotFound.Is(err) {
				log.ZWarn(ctx, "User info notFound", err, "userID", userID)
				continue
			}
			return nil, err
		}
		users = append(users, user)
	}
	return users, nil
}

// GetUsersInfoMap 获取用户信息映射表
// 参数:
//   - ctx: 上下文
//   - userIDs: 用户ID列表
//
// 返回:
//   - map[string]*sdkws.UserInfo: 用户信息映射表，键为用户ID，值为用户信息
//   - error: 错误信息
//
// 功能:
//   - 批量获取用户信息并组织为映射表结构
//   - 便于根据用户ID快速查找对应的用户信息
//   - 自动过滤不存在的用户（返回ErrRecordNotFound）
//   - 适用于需要频繁根据用户ID查找用户信息的场景
//   - 每个用户信息都会利用本地缓存机制
func (u *UserLocalCache) GetUsersInfoMap(ctx context.Context, userIDs []string) (map[string]*sdkws.UserInfo, error) {
	users := make(map[string]*sdkws.UserInfo, len(userIDs))
	// 逐个获取用户信息并构建映射表
	for _, userID := range userIDs {
		user, err := u.GetUserInfo(ctx, userID)
		if err != nil {
			// 如果用户不存在，跳过该用户，不返回错误
			if errs.ErrRecordNotFound.Is(err) {
				continue
			}
			return nil, err
		}
		users[userID] = user
	}
	return users, nil
}
