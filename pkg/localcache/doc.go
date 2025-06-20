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

// Package localcache 提供了高性能的本地缓存实现
//
// 这个包实现了一个分布式系统中的本地缓存层，具有以下特性：
//
// 1. 基于LRU（Least Recently Used）算法的缓存淘汰策略
// 2. 支持两种过期策略：主动过期（定时清理）和懒惰过期（访问时检查）
// 3. 分槽设计减少锁竞争，提高并发性能
// 4. 缓存项关联功能，支持级联删除
// 5. 完善的统计指标收集，便于监控和调优
// 6. 支持批量操作，提高批量数据处理性能
//
// 主要组件：
// - Cache: 缓存的主要接口，提供Get、Set、Del等基本操作
// - LRU: LRU缓存的接口定义，支持多种实现
// - Link: 缓存项关联关系管理，支持级联删除
//
// 使用示例：
//
//	// 创建一个基本的缓存实例
//	cache := New[string]()
//
//	// 获取缓存，如果不存在则通过fetch函数获取
//	value, err := cache.Get(ctx, "key1", func(ctx context.Context) (string, error) {
//	    return "value1", nil
//	})
//
//	// 建立关联关系的缓存获取
//	value, err := cache.GetLink(ctx, "key1", func(ctx context.Context) (string, error) {
//	    return "value1", nil
//	}, "key2", "key3") // key1与key2、key3建立关联，删除任一会影响其他
//
//	// 删除缓存
//	cache.Del(ctx, "key1")
//
//	// 配置缓存选项
//	cache := New[string](
//	    WithLocalSlotNum(1000),        // 设置1000个槽位
//	    WithLocalSlotSize(10000),      // 每个槽位10000个缓存项
//	    WithExpirationEvict(),         // 使用主动过期策略
//	    WithLocalSuccessTTL(time.Hour), // 成功数据缓存1小时
//	)
package localcache // import "github.com/openimsdk/open-im-server/v3/pkg/localcache"
