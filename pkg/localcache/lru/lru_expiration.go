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

import (
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
)

// NewExpirationLRU 创建一个新的主动过期LRU缓存实例
// 主动过期意味着使用定时器主动清理过期的缓存项，而不是等到访问时才检查
// K: 缓存键类型，必须可比较
// V: 缓存值类型，任意类型
// size: 缓存最大容量
// successTTL: 成功获取数据的缓存存活时间
// failedTTL: 获取数据失败的缓存存活时间（通常较短，避免频繁重试失败的数据）
// target: 统计指标收集器
// onEvict: 缓存项被驱逐时的回调函数
// 返回: LRU接口实现
func NewExpirationLRU[K comparable, V any](size int, successTTL, failedTTL time.Duration, target Target, onEvict EvictCallback[K, V]) LRU[K, V] {
	var cb expirable.EvictCallback[K, *expirationLruItem[V]]
	if onEvict != nil {
		// 包装用户提供的回调函数，从内部结构中提取实际值
		cb = func(key K, value *expirationLruItem[V]) {
			onEvict(key, value.value)
		}
	}

	// 创建带过期功能的LRU缓存，使用successTTL作为默认TTL
	core := expirable.NewLRU[K, *expirationLruItem[V]](size, cb, successTTL)

	return &ExpirationLRU[K, V]{
		core:       core,
		successTTL: successTTL,
		failedTTL:  failedTTL,
		target:     target,
	}
}

// expirationLruItem 表示主动过期LRU缓存中的一个缓存项
// 包含读写锁保护并发访问，以及错误信息和实际值
type expirationLruItem[V any] struct {
	lock  sync.RWMutex // 读写锁，保护value和err字段的并发访问
	err   error        // 获取数据时的错误信息
	value V            // 实际缓存的值
}

// ExpirationLRU 是主动过期LRU缓存的实现
// 使用hashicorp的expirable.LRU作为底层实现，该实现会主动清理过期数据
type ExpirationLRU[K comparable, V any] struct {
	lock       sync.Mutex                               // 保护core字段的并发访问
	core       *expirable.LRU[K, *expirationLruItem[V]] // 底层的可过期LRU缓存
	successTTL time.Duration                            // 成功数据的存活时间
	failedTTL  time.Duration                            // 失败数据的存活时间
	target     Target                                   // 统计指标收集器
}

// GetBatch 批量获取缓存项（当前未实现）
// 此方法在主动过期LRU中尚未实现，调用会导致panic
func (x *ExpirationLRU[K, V]) GetBatch(keys []K, fetch func(keys []K) (map[K]V, error)) (map[K]V, error) {
	//TODO implement me
	panic("implement me")
}

// Get 获取缓存项，如果不存在则调用fetch函数获取
// 这是LRU缓存的核心方法，实现了缓存的主要逻辑
func (x *ExpirationLRU[K, V]) Get(key K, fetch func() (V, error)) (V, error) {
	x.lock.Lock()
	// 尝试从缓存中获取数据
	v, ok := x.core.Get(key)
	if ok {
		// 缓存命中，释放外层锁
		x.lock.Unlock()
		x.target.IncrGetSuccess()

		// 获取读锁访问缓存项数据
		v.lock.RLock()
		defer v.lock.RUnlock()
		return v.value, v.err
	} else {
		// 缓存未命中，创建新的缓存项并加锁
		v = &expirationLruItem[V]{}
		x.core.Add(key, v)
		v.lock.Lock()
		x.lock.Unlock()
		defer v.lock.Unlock()

		// 调用fetch函数获取数据
		v.value, v.err = fetch()
		if v.err == nil {
			x.target.IncrGetSuccess()
		} else {
			x.target.IncrGetFailed()
			// 如果获取失败，立即从缓存中移除，避免缓存错误数据
			x.core.Remove(key)
		}
		return v.value, v.err
	}
}

// Del 删除指定key的缓存项
func (x *ExpirationLRU[K, V]) Del(key K) bool {
	x.lock.Lock()
	ok := x.core.Remove(key)
	x.lock.Unlock()

	// 更新统计指标
	if ok {
		x.target.IncrDelHit()
	} else {
		x.target.IncrDelNotFound()
	}
	return ok
}

// SetHas 如果key已存在则更新其值
// 只有当key在缓存中存在时才会更新，否则不做任何操作
func (x *ExpirationLRU[K, V]) SetHas(key K, value V) bool {
	x.lock.Lock()
	defer x.lock.Unlock()

	if x.core.Contains(key) {
		// key存在，更新其值
		x.core.Add(key, &expirationLruItem[V]{value: value})
		return true
	}
	return false
}

// Set 设置缓存项
// 无论key是否存在都会设置其值
func (x *ExpirationLRU[K, V]) Set(key K, value V) {
	x.lock.Lock()
	defer x.lock.Unlock()
	x.core.Add(key, &expirationLruItem[V]{value: value})
}

// Stop 停止缓存服务
// 对于主动过期LRU，底层的expirable.LRU会自动管理定时器资源
func (x *ExpirationLRU[K, V]) Stop() {
	// expirable.LRU会自动管理其内部的定时器资源
}
