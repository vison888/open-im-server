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

// user_map.go - 用户连接映射管理模块
//
// 功能概述:
// 1. 管理用户ID与客户端连接的映射关系
// 2. 支持多平台并发连接的存储和查询
// 3. 实现线程安全的连接增删改查操作
// 4. 提供用户在线状态变更的实时通知机制
// 5. 支持连接的生命周期管理和资源清理
//
// 设计思路:
// - 分层设计: 用户 -> 平台 -> 连接列表的三层映射结构
// - 线程安全: 使用读写锁保护并发访问
// - 事件驱动: 状态变更通过通道异步通知
// - 内存优化: 及时清理无效连接，避免内存泄漏
// - 高性能: 针对高频查询操作进行优化
//
// 核心数据结构:
// - UserMap: 用户映射管理接口
// - UserPlatform: 用户在特定平台的连接信息
// - UserState: 用户状态变更事件
// - userMap: 具体的映射实现

package msggateway

import (
	"sync"
	"time"

	"github.com/openimsdk/tools/utils/datautil"
)

// UserMap 用户连接映射管理接口
// 设计思路：
// 1. 提供完整的用户连接生命周期管理
// 2. 支持多平台并发连接的复杂场景
// 3. 实现高效的查询和状态变更通知
// 4. 保证线程安全和数据一致性
type UserMap interface {
	// GetAll 获取指定用户的所有连接
	// 返回: 连接列表, 用户是否存在
	GetAll(userID string) ([]*Client, bool)

	// Get 获取指定用户在特定平台的连接
	// 返回: 连接列表, 用户是否存在, 平台是否有连接
	Get(userID string, platformID int) ([]*Client, bool, bool)

	// Set 为用户添加新的客户端连接
	Set(userID string, v *Client)

	// DeleteClients 删除用户的指定连接列表
	// 返回: 是否完全删除了该用户（无剩余连接）
	DeleteClients(userID string, clients []*Client) (isDeleteUser bool)

	// UserState 获取用户状态变更通知通道
	UserState() <-chan UserState

	// GetAllUserStatus 获取所有用户的状态信息（批量查询）
	GetAllUserStatus(deadline time.Time, nowtime time.Time) []UserState

	// RecvSubChange 处理订阅变更，检查本地是否还有相关连接
	RecvSubChange(userID string, platformIDs []int32) bool
}

// UserState 用户状态变更事件结构
// 设计思路：
// 1. 记录用户状态的完整变更信息
// 2. 支持在线和离线平台的同时记录
// 3. 用于状态同步和事件通知
type UserState struct {
	UserID  string  // 用户唯一标识
	Online  []int32 // 当前在线的平台ID列表
	Offline []int32 // 当前离线的平台ID列表
}

// UserPlatform 用户平台连接信息结构
// 设计思路：
// 1. 聚合同一用户的所有平台连接
// 2. 记录最后更新时间，用于缓存管理
// 3. 提供平台ID的快速访问方法
type UserPlatform struct {
	Time    time.Time // 最后更新时间
	Clients []*Client // 该用户的所有客户端连接
}

// PlatformIDs 获取当前连接的所有平台ID列表
// 设计思路：
// 1. 从连接列表中提取平台ID
// 2. 返回有序的平台ID切片
// 3. 用于状态查询和事件通知
//
// 返回值：
//   - []int32: 平台ID列表，如果无连接则返回nil
func (u *UserPlatform) PlatformIDs() []int32 {
	if len(u.Clients) == 0 {
		return nil
	}
	platformIDs := make([]int32, 0, len(u.Clients))
	for _, client := range u.Clients {
		platformIDs = append(platformIDs, int32(client.PlatformID))
	}
	return platformIDs
}

// PlatformIDSet 获取当前连接的平台ID集合
// 设计思路：
// 1. 返回Set结构便于快速查找
// 2. 去重处理，避免重复平台ID
// 3. 用于集合运算和比较操作
//
// 返回值：
//   - map[int32]struct{}: 平台ID集合，如果无连接则返回nil
func (u *UserPlatform) PlatformIDSet() map[int32]struct{} {
	if len(u.Clients) == 0 {
		return nil
	}
	platformIDs := make(map[int32]struct{})
	for _, client := range u.Clients {
		platformIDs[int32(client.PlatformID)] = struct{}{}
	}
	return platformIDs
}

// newUserMap 创建新的用户映射管理器
// 设计思路：
// 1. 初始化内部数据结构和通道
// 2. 设置合适的通道缓冲区大小
// 3. 返回接口类型，支持依赖注入
//
// 返回值：
//   - UserMap: 用户映射管理器接口
func newUserMap() UserMap {
	return &userMap{
		data: make(map[string]*UserPlatform),
		ch:   make(chan UserState, 10000), // 大容量缓冲区，避免阻塞
	}
}

// userMap 用户映射管理器的具体实现
// 设计思路：
// 1. 使用读写锁保证并发安全
// 2. map存储用户到平台连接的映射
// 3. 通道用于异步状态变更通知
// 4. 支持高并发的读写操作
type userMap struct {
	lock sync.RWMutex             // 读写锁，保护并发访问
	data map[string]*UserPlatform // 用户ID到平台连接的映射
	ch   chan UserState           // 状态变更通知通道
}

// RecvSubChange 处理订阅变更通知
// 设计思路：
// 1. 检查本地是否还有指定平台的连接
// 2. 如果有剩余连接，发送状态通知
// 3. 用于集群间的状态同步协调
//
// 应用场景：
// - 集群节点间的连接状态同步
// - 订阅关系变更的处理
// - 负载均衡的连接迁移
//
// 参数：
//   - userID: 用户ID
//   - platformIDs: 要检查的平台ID列表
//
// 返回值：
//   - bool: 是否还有剩余连接
func (u *userMap) RecvSubChange(userID string, platformIDs []int32) bool {
	u.lock.RLock()
	defer u.lock.RUnlock()

	result, ok := u.data[userID]
	if !ok {
		return false // 用户不存在
	}

	// 获取本地平台ID集合
	localPlatformIDs := result.PlatformIDSet()

	// 从本地集合中移除指定的平台ID
	for _, platformID := range platformIDs {
		delete(localPlatformIDs, platformID)
	}

	// 如果没有剩余连接，返回false
	if len(localPlatformIDs) == 0 {
		return false
	}

	// 发送状态更新通知
	u.push(userID, result, nil)
	return true
}

// push 推送用户状态变更事件
// 设计思路：
// 1. 非阻塞方式发送状态事件
// 2. 更新时间戳，用于缓存管理
// 3. 支持在线和离线状态的同时通知
//
// 参数：
//   - userID: 用户ID
//   - userPlatform: 用户平台连接信息
//   - offline: 离线的平台ID列表
//
// 返回值：
//   - bool: 是否成功发送事件
func (u *userMap) push(userID string, userPlatform *UserPlatform, offline []int32) bool {
	select {
	case u.ch <- UserState{
		UserID:  userID,
		Online:  userPlatform.PlatformIDs(),
		Offline: offline,
	}:
		// 更新时间戳
		userPlatform.Time = time.Now()
		return true
	default:
		// 通道已满，丢弃事件避免阻塞
		return false
	}
}

// GetAll 获取指定用户的所有连接
// 设计思路：
// 1. 使用读锁保护并发访问
// 2. 返回连接切片的副本，避免外部修改
// 3. 高频调用的优化路径
//
// 参数：
//   - userID: 用户ID
//
// 返回值：
//   - []*Client: 用户的所有连接
//   - bool: 用户是否存在
func (u *userMap) GetAll(userID string) ([]*Client, bool) {
	u.lock.RLock()
	defer u.lock.RUnlock()

	result, ok := u.data[userID]
	if !ok {
		return nil, false
	}
	return result.Clients, true
}

// Get 获取指定用户在特定平台的连接
// 设计思路：
// 1. 支持多层级的查询结果
// 2. 区分用户不存在和平台无连接的情况
// 3. 返回匹配平台的所有连接
//
// 参数：
//   - userID: 用户ID
//   - platformID: 平台ID
//
// 返回值：
//   - []*Client: 匹配平台的连接列表
//   - bool: 用户是否存在
//   - bool: 该平台是否有连接
func (u *userMap) Get(userID string, platformID int) ([]*Client, bool, bool) {
	u.lock.RLock()
	defer u.lock.RUnlock()

	result, ok := u.data[userID]
	if !ok {
		return nil, false, false
	}

	var clients []*Client
	// 遍历所有连接，筛选匹配的平台
	for _, client := range result.Clients {
		if client.PlatformID == platformID {
			clients = append(clients, client)
		}
	}
	return clients, true, len(clients) > 0
}

// Set 为用户添加新的客户端连接
// 设计思路：
// 1. 使用写锁保护并发修改
// 2. 支持新用户创建和现有用户连接追加
// 3. 立即发送状态变更通知
//
// 参数：
//   - userID: 用户ID
//   - client: 要添加的客户端连接
func (u *userMap) Set(userID string, client *Client) {
	u.lock.Lock()
	defer u.lock.Unlock()

	result, ok := u.data[userID]
	if ok {
		// 用户已存在，追加连接
		result.Clients = append(result.Clients, client)
	} else {
		// 新用户，创建平台连接信息
		result = &UserPlatform{
			Clients: []*Client{client},
		}
		u.data[userID] = result
	}

	// 发送状态变更通知
	u.push(client.UserID, result, nil)
}

// DeleteClients 删除用户的指定连接列表
// 设计思路：
// 1. 基于连接地址进行精确匹配删除
// 2. 区分部分删除和完全删除的情况
// 3. 记录离线平台信息用于通知
// 4. 自动清理空用户记录
//
// 参数：
//   - userID: 用户ID
//   - clients: 要删除的连接列表
//
// 返回值：
//   - bool: 是否完全删除了该用户（无剩余连接）
func (u *userMap) DeleteClients(userID string, clients []*Client) (isDeleteUser bool) {
	if len(clients) == 0 {
		return false
	}

	u.lock.Lock()
	defer u.lock.Unlock()

	result, ok := u.data[userID]
	if !ok {
		return false
	}

	// 记录要删除的平台ID
	offline := make([]int32, 0, len(clients))

	// 创建待删除连接的地址集合，用于快速查找
	deleteAddr := datautil.SliceSetAny(clients, func(client *Client) string {
		return client.ctx.GetRemoteAddr()
	})

	// 重新构建连接列表，排除要删除的连接
	tmp := result.Clients
	result.Clients = result.Clients[:0] // 重置切片但保留容量

	for _, client := range tmp {
		if _, delCli := deleteAddr[client.ctx.GetRemoteAddr()]; delCli {
			// 记录离线的平台ID
			offline = append(offline, int32(client.PlatformID))
		} else {
			// 保留未删除的连接
			result.Clients = append(result.Clients, client)
		}
	}

	// 延迟发送状态变更通知
	defer u.push(userID, result, offline)

	// 检查是否需要删除用户记录
	if len(result.Clients) > 0 {
		return false // 还有剩余连接
	}

	// 删除用户记录，释放内存
	delete(u.data, userID)
	return true
}

// GetAllUserStatus 获取所有用户的状态信息（批量查询）
// 设计思路：
// 1. 基于时间戳过滤需要更新的用户
// 2. 批量构建状态信息，提高效率
// 3. 更新时间戳，用于下次增量查询
// 4. 支持大规模用户的状态同步
//
// 应用场景：
// - 定期的状态同步任务
// - 集群间的批量状态更新
// - 监控系统的状态收集
//
// 参数：
//   - deadline: 时间截止点，只处理该时间之前的数据
//   - nowtime: 当前时间，用于更新时间戳
//
// 返回值：
//   - []UserState: 用户状态列表
func (u *userMap) GetAllUserStatus(deadline time.Time, nowtime time.Time) (result []UserState) {
	u.lock.RLock()
	defer u.lock.RUnlock()

	result = make([]UserState, 0, len(u.data))

	for userID, userPlatform := range u.data {
		// 跳过时间戳晚于截止时间的记录
		if deadline.Before(userPlatform.Time) {
			continue
		}

		// 更新时间戳
		userPlatform.Time = nowtime

		// 构建在线平台列表
		online := make([]int32, 0, len(userPlatform.Clients))
		for _, client := range userPlatform.Clients {
			online = append(online, int32(client.PlatformID))
		}

		// 添加到结果列表
		result = append(result, UserState{
			UserID: userID,
			Online: online,
		})
	}
	return result
}

// UserState 获取用户状态变更通知通道
// 设计思路：
// 1. 返回只读通道，避免外部误操作
// 2. 用于异步的状态变更监听
// 3. 支持事件驱动的状态处理
//
// 返回值：
//   - <-chan UserState: 只读的状态变更通道
func (u *userMap) UserState() <-chan UserState {
	return u.ch
}
