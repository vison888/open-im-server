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

package lru

import "github.com/hashicorp/golang-lru/v2/simplelru"

// EvictCallback 定义了缓存项被驱逐时的回调函数类型
// 当LRU缓存满了需要驱逐旧项时，或者手动删除缓存项时会调用此回调
// K: 缓存键的类型，必须是可比较的类型
// V: 缓存值的类型，可以是任意类型
type EvictCallback[K comparable, V any] simplelru.EvictCallback[K, V]

// LRU 定义了LRU（Least Recently Used）缓存的接口
// LRU是一种缓存淘汰策略，当缓存满时会优先淘汰最近最少使用的数据
// K: 缓存键的类型，必须是可比较的类型（如string、int等）
// V: 缓存值的类型，可以是任意类型
type LRU[K comparable, V any] interface {
	// Get 根据key获取缓存值，如果缓存不存在则调用fetch函数获取
	// key: 缓存的键
	// fetch: 当缓存不存在时用于获取数据的函数
	// 返回: 缓存值和可能的错误
	Get(key K, fetch func() (V, error)) (V, error)

	// Set 设置缓存项
	// key: 缓存的键
	// value: 要缓存的值
	Set(key K, value V)

	// SetHas 如果key已存在则更新其值，否则不做任何操作
	// key: 缓存的键
	// value: 要设置的值
	// 返回: 如果key存在并成功更新则返回true，否则返回false
	SetHas(key K, value V) bool

	// GetBatch 批量获取多个key的缓存值
	// keys: 要获取的key列表
	// fetch: 当部分key的缓存不存在时，用于批量获取数据的函数
	// 返回: key到value的映射和可能的错误
	GetBatch(keys []K, fetch func(keys []K) (map[K]V, error)) (map[K]V, error)

	// Del 删除指定key的缓存项
	// key: 要删除的缓存键
	// 返回: 如果key存在并成功删除则返回true，否则返回false
	Del(key K) bool

	// Stop 停止LRU缓存服务，清理相关资源
	// 主要用于清理定时器、goroutine等资源
	Stop()
}

// Target 定义了缓存统计指标的接口
// 用于收集缓存的命中率、成功率等统计信息，便于监控和调优
type Target interface {
	// IncrGetHit 增加缓存命中次数
	// 当Get操作从缓存中直接获取到数据时调用
	IncrGetHit()

	// IncrGetSuccess 增加获取成功次数
	// 当Get操作成功返回数据时调用（无论是从缓存还是从fetch函数）
	IncrGetSuccess()

	// IncrGetFailed 增加获取失败次数
	// 当Get操作失败时调用（通常是fetch函数返回错误）
	IncrGetFailed()

	// IncrDelHit 增加删除命中次数
	// 当Del操作成功删除存在的缓存项时调用
	IncrDelHit()

	// IncrDelNotFound 增加删除未找到次数
	// 当Del操作尝试删除不存在的缓存项时调用
	IncrDelNotFound()
}
