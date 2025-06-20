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

// Package link 提供了缓存key之间的关联关系管理功能
//
// 这个包实现了缓存项之间的双向关联机制，当一个缓存项被删除时，
// 所有与之关联的缓存项也会被连带删除。这种机制在以下场景中非常有用：
//
// 1. 数据一致性保证：
//   - 当用户信息更新时，需要清理所有与该用户相关的缓存
//   - 当群组信息变更时，需要清理群组成员、群组信息等相关缓存
//
// 2. 级联清理：
//   - 避免缓存中存在过期或不一致的数据
//   - 简化缓存管理，自动化清理流程
//
// 主要特性：
// - 双向关联：A关联B时，B也会自动关联A
// - 分片存储：使用哈希分片减少锁竞争，提高并发性能
// - 递归删除：使用深度优先搜索遍历所有关联关系
// - 并发安全：每个分片独立加锁，支持多goroutine访问
//
// 工作原理：
// 1. 建立关联：当调用Link(key, links...)时，会建立双向关联关系
// 2. 存储分片：根据key的哈希值将关联关系存储到不同的分片中
// 3. 删除遍历：当删除某个key时，使用栈结构进行深度优先搜索
// 4. 避免重复：使用集合记录已处理的key，避免重复删除和死循环
//
// 使用示例：
//
//	// 创建链接管理器，使用10个分片
//	linkMgr := New(10)
//
//	// 建立关联关系
//	linkMgr.Link("user:123", "user_info:123", "user_friends:123", "user_groups:123")
//
//	// 删除用户相关的所有缓存
//	deletedKeys := linkMgr.Del("user:123")
//	// deletedKeys 将包含所有与 user:123 关联的key
//
// 注意事项：
// - 关联关系是双向的，删除任一关联key都会影响其他key
// - 建立关联时会复制key列表，避免外部修改影响内部状态
// - 删除操作是原子的，要么全部删除，要么全部保留
package link // import "github.com/openimsdk/open-im-server/v3/pkg/localcache/link"
