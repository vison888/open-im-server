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
	"encoding/json"

	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/redis/go-redis/v9"
)

// subscriberRedisDeleteCache Redis缓存失效订阅器
// 参数:
//   - ctx: 上下文，用于控制协程生命周期
//   - client: Redis客户端，用于订阅消息
//   - channel: 订阅的频道名称，用于接收缓存失效通知
//   - del: 删除缓存的回调函数，接收需要删除的缓存键列表
//
// 功能:
//   - 订阅Redis指定频道的消息
//   - 解析接收到的JSON格式的缓存键列表
//   - 调用删除回调函数清理本地缓存
//   - 实现分布式缓存一致性，当某个服务更新数据时，通知其他服务删除相关缓存
//   - 包含panic恢复机制，确保订阅协程的稳定性
//
// 使用场景:
//   - 分布式部署环境下的缓存一致性保证
//   - 数据更新时的主动缓存失效通知
//   - 多实例服务间的缓存同步
func subscriberRedisDeleteCache(ctx context.Context, client redis.UniversalClient, channel string, del func(ctx context.Context, key ...string)) {
	// 使用defer捕获可能的panic，确保订阅协程不会因为异常而退出
	defer func() {
		if r := recover(); r != nil {
			log.ZPanic(ctx, "subscriberRedisDeleteCache Panic", errs.ErrPanic(r))
		}
	}()

	// 订阅Redis频道，持续监听缓存失效消息
	// client.Subscribe(ctx, channel).Channel() 返回一个消息通道
	// 通过range循环持续接收消息，直到上下文取消或连接断开
	for message := range client.Subscribe(ctx, channel).Channel() {
		// 记录接收到的缓存失效消息，用于调试和监控
		log.ZDebug(ctx, "subscriberRedisDeleteCache", "channel", channel, "payload", message.Payload)

		var keys []string
		// 解析JSON格式的消息载荷，获取需要删除的缓存键列表
		// 消息格式应为：["key1", "key2", "key3"]
		if err := json.Unmarshal([]byte(message.Payload), &keys); err != nil {
			// 如果JSON解析失败，记录错误日志但不中断订阅
			log.ZError(ctx, "subscriberRedisDeleteCache json.Unmarshal error", err)
			continue
		}

		// 如果缓存键列表为空，跳过本次处理
		if len(keys) == 0 {
			continue
		}

		// 调用删除回调函数，清理本地缓存中的指定键
		// del函数通常是本地缓存实例的DelLocal方法
		del(ctx, keys...)
	}
}
