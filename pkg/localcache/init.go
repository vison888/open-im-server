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

package localcache

import (
	"strings"
	"sync"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache/cachekey"
)

var (
	once      sync.Once           // 确保初始化只执行一次
	subscribe map[string][]string // 订阅映射：topic -> 缓存key前缀列表
)

// InitLocalCache 初始化本地缓存的订阅配置
// 根据配置文件中的设置，建立topic与缓存key前缀的映射关系
// 当某个topic的消息到达时，可以根据此映射找到需要清理的缓存key
// localCache: 本地缓存配置，包含各种缓存的开关和topic设置
func InitLocalCache(localCache *config.LocalCache) {
	once.Do(func() {
		// 定义缓存配置与其对应的缓存key前缀的映射关系
		list := []struct {
			Local config.CacheConfig // 缓存配置
			Keys  []string           // 对应的缓存key前缀列表
		}{
			{
				// 用户相关缓存
				Local: localCache.User,
				Keys:  []string{cachekey.UserInfoKey, cachekey.UserGlobalRecvMsgOptKey},
			},
			{
				// 群组相关缓存
				Local: localCache.Group,
				Keys:  []string{cachekey.GroupMemberIDsKey, cachekey.GroupInfoKey, cachekey.GroupMemberInfoKey},
			},
			{
				// 好友关系相关缓存
				Local: localCache.Friend,
				Keys:  []string{cachekey.FriendIDsKey, cachekey.BlackIDsKey},
			},
			{
				// 会话相关缓存
				Local: localCache.Conversation,
				Keys:  []string{cachekey.ConversationKey, cachekey.ConversationIDsKey, cachekey.ConversationNotReceiveMessageUserIDsKey},
			},
		}

		// 初始化订阅映射
		subscribe = make(map[string][]string)

		// 遍历所有缓存配置，建立topic到缓存key前缀的映射
		for _, v := range list {
			if v.Local.Enable() {
				// 如果该缓存类型启用了，则将其topic和对应的key前缀加入订阅映射
				subscribe[v.Local.Topic] = v.Keys
			}
		}
	})
}

// GetPublishKeysByTopic 根据topic列表和key列表，返回每个topic对应的key列表
// 这个函数用于确定哪些缓存key需要根据特定的topic进行清理
// topics: 需要处理的topic列表
// keys: 需要检查的缓存key列表
// 返回: 每个topic对应的需要清理的key列表
func GetPublishKeysByTopic(topics []string, keys []string) map[string][]string {
	// 初始化结果映射，为每个topic创建空的key列表
	keysByTopic := make(map[string][]string)
	for _, topic := range topics {
		keysByTopic[topic] = []string{}
	}

	// 检查每个key是否属于某个topic的管理范围
	for _, key := range keys {
		for _, topic := range topics {
			// 获取该topic对应的缓存key前缀列表
			prefixes, ok := subscribe[topic]
			if !ok {
				// 如果topic没有对应的订阅配置，跳过
				continue
			}

			// 检查当前key是否匹配该topic的任何一个前缀
			for _, prefix := range prefixes {
				if strings.HasPrefix(key, prefix) {
					// 如果匹配，将该key加入对应topic的列表中
					keysByTopic[topic] = append(keysByTopic[topic], key)
					break // 找到匹配的前缀后，不需要继续检查其他前缀
				}
			}
		}
	}

	return keysByTopic
}
