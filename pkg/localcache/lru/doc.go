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

// Package lru 提供了多种LRU（Least Recently Used）缓存实现
//
// LRU是一种缓存淘汰策略，当缓存满时会优先淘汰最近最少使用的数据。
// 这个包提供了多种LRU实现，适用于不同的使用场景：
//
// 1. ExpirationLRU（主动过期LRU）：
//   - 使用定时器主动清理过期数据
//   - 内存使用更高效，但CPU消耗略高
//   - 适合内存敏感的场景
//
// 2. LayLRU（懒惰过期LRU）：
//   - 在访问时检查数据是否过期
//   - CPU消耗低，但过期数据可能占用内存较久
//   - 适合CPU敏感的场景
//
// 3. SlotLRU（分槽LRU）：
//   - 通过分片减少锁竞争
//   - 提高并发性能
//   - 适合高并发场景
//
// 主要特性：
// - 泛型支持，类型安全
// - 并发安全，支持多goroutine访问
// - 支持批量操作，提高批量数据处理性能
// - 完善的统计指标，便于监控和调优
// - TTL支持，区分成功和失败数据的存活时间
// - 驱逐回调，支持清理相关资源
//
// 使用示例：
//
//	// 创建一个懒惰过期LRU缓存
//	cache := NewLayLRU[string, string](
//	    1000,              // 最大容量
//	    time.Hour,         // 成功数据TTL
//	    time.Minute,       // 失败数据TTL
//	    target,            // 统计目标
//	    onEvict,           // 驱逐回调
//	)
//
//	// 获取数据
//	value, err := cache.Get("key", func() (string, error) {
//	    return "value", nil
//	})
//
//	// 批量获取数据
//	values, err := cache.GetBatch([]string{"key1", "key2"}, func(keys []string) (map[string]string, error) {
//	    return map[string]string{"key1": "value1", "key2": "value2"}, nil
//	})
package lru // import "github.com/openimsdk/open-im-server/v3/pkg/localcache/lru"
