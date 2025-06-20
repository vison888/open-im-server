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

	"github.com/hashicorp/golang-lru/v2/simplelru"
)

// layLruItem 表示懒惰过期LRU缓存中的一个缓存项
// 懒惰过期意味着不使用定时器主动清理，而是在访问时检查是否过期
type layLruItem[V any] struct {
	lock    sync.Mutex // 互斥锁，保护expires、err、value字段的并发访问
	expires int64      // 过期时间戳（毫秒），0表示永不过期
	err     error      // 获取数据时的错误信息
	value   V          // 实际缓存的值
}

// NewLayLRU 创建一个新的懒惰过期LRU缓存实例
// 懒惰过期的优势是不需要定时器，减少了系统资源消耗
// 缺点是过期数据会一直占用内存直到被访问或被LRU算法驱逐
// K: 缓存键类型，必须可比较
// V: 缓存值类型，任意类型
// size: 缓存最大容量
// successTTL: 成功获取数据的缓存存活时间
// failedTTL: 获取数据失败的缓存存活时间
// target: 统计指标收集器
// onEvict: 缓存项被驱逐时的回调函数
// 返回: LayLRU实例指针
func NewLayLRU[K comparable, V any](size int, successTTL, failedTTL time.Duration, target Target, onEvict EvictCallback[K, V]) *LayLRU[K, V] {
	var cb simplelru.EvictCallback[K, *layLruItem[V]]
	if onEvict != nil {
		// 包装用户提供的回调函数，从内部结构中提取实际值
		cb = func(key K, value *layLruItem[V]) {
			onEvict(key, value.value)
		}
	}

	// 创建底层的简单LRU缓存
	core, err := simplelru.NewLRU[K, *layLruItem[V]](size, cb)
	if err != nil {
		panic(err)
	}

	return &LayLRU[K, V]{
		core:       core,
		successTTL: successTTL,
		failedTTL:  failedTTL,
		target:     target,
	}
}

// LayLRU 是懒惰过期LRU缓存的实现
// 使用hashicorp的simplelru.LRU作为底层实现，在访问时检查数据是否过期
type LayLRU[K comparable, V any] struct {
	lock       sync.Mutex                        // 保护core字段的并发访问
	core       *simplelru.LRU[K, *layLruItem[V]] // 底层的简单LRU缓存
	successTTL time.Duration                     // 成功数据的存活时间
	failedTTL  time.Duration                     // 失败数据的存活时间
	target     Target                            // 统计指标收集器
}

// Get 获取缓存项，如果不存在或已过期则调用fetch函数获取
// 这是懒惰过期LRU缓存的核心方法
func (x *LayLRU[K, V]) Get(key K, fetch func() (V, error)) (V, error) {
	x.lock.Lock()
	// 尝试从缓存中获取数据
	v, ok := x.core.Get(key)
	if ok {
		x.lock.Unlock()
		v.lock.Lock()
		expires, value, err := v.expires, v.value, v.err

		// 检查数据是否过期
		if expires != 0 && expires > time.Now().UnixMilli() {
			// 数据未过期，直接返回缓存的数据
			v.lock.Unlock()
			x.target.IncrGetHit()
			return value, err
		}
	} else {
		// 缓存未命中，创建新的缓存项
		v = &layLruItem[V]{}
		x.core.Add(key, v)
		v.lock.Lock()
		x.lock.Unlock()
	}

	defer v.lock.Unlock()

	// 双重检查，确保在获取锁的过程中数据没有被其他goroutine更新
	if v.expires > time.Now().UnixMilli() {
		return v.value, v.err
	}

	// 数据过期或不存在，调用fetch函数获取新数据
	v.value, v.err = fetch()
	if v.err == nil {
		// 获取成功，设置成功TTL
		v.expires = time.Now().Add(x.successTTL).UnixMilli()
		x.target.IncrGetSuccess()
	} else {
		// 获取失败，设置失败TTL（通常较短）
		v.expires = time.Now().Add(x.failedTTL).UnixMilli()
		x.target.IncrGetFailed()
	}
	return v.value, v.err
}

// GetBatch 批量获取缓存项
// 对于每个key，如果缓存中存在且未过期则直接返回，否则加入查询列表
// 最后批量调用fetch函数获取缺失的数据
func (x *LayLRU[K, V]) GetBatch(keys []K, fetch func(keys []K) (map[K]V, error)) (map[K]V, error) {
	var (
		err  error     // 用于记录第一个遇到的错误
		once sync.Once // 确保err只被设置一次
	)

	res := make(map[K]V)                // 结果映射
	queries := make([]K, 0)             // 需要查询的key列表
	setVs := make(map[K]*layLruItem[V]) // 需要设置到缓存的项

	// 检查每个key的缓存状态
	for _, key := range keys {
		x.lock.Lock()
		v, ok := x.core.Get(key)
		x.lock.Unlock()

		if ok {
			v.lock.Lock()
			expires, value, err1 := v.expires, v.value, v.err
			v.lock.Unlock()

			// 检查是否过期
			if expires != 0 && expires > time.Now().UnixMilli() {
				// 未过期，使用缓存数据
				x.target.IncrGetHit()
				res[key] = value
				if err1 != nil {
					// 记录第一个错误
					once.Do(func() {
						err = err1
					})
				}
				continue
			}
		}
		// 缓存不存在或已过期，加入查询列表
		queries = append(queries, key)
	}

	// 批量获取缺失的数据
	values, err1 := fetch(queries)
	if err1 != nil {
		once.Do(func() {
			err = err1
		})
	}

	// 将获取到的数据设置到缓存中
	for key, val := range values {
		v := &layLruItem[V]{}
		v.value = val

		if err == nil {
			// 获取成功，设置成功TTL
			v.expires = time.Now().Add(x.successTTL).UnixMilli()
			x.target.IncrGetSuccess()
		} else {
			// 获取失败，设置失败TTL
			v.expires = time.Now().Add(x.failedTTL).UnixMilli()
			x.target.IncrGetFailed()
		}

		setVs[key] = v
		x.lock.Lock()
		x.core.Add(key, v)
		x.lock.Unlock()
		res[key] = val
	}

	return res, err
}

//func (x *LayLRU[K, V]) Has(key K) bool {
//	x.lock.Lock()
//	defer x.lock.Unlock()
//	return x.core.Contains(key)
//}

// Set 设置缓存项
// 无论key是否存在都会设置其值，并设置为成功TTL
func (x *LayLRU[K, V]) Set(key K, value V) {
	x.lock.Lock()
	defer x.lock.Unlock()
	x.core.Add(key, &layLruItem[V]{
		value:   value,
		expires: time.Now().Add(x.successTTL).UnixMilli(),
	})
}

// SetHas 如果key已存在则更新其值
// 只有当key在缓存中存在时才会更新，否则不做任何操作
func (x *LayLRU[K, V]) SetHas(key K, value V) bool {
	x.lock.Lock()
	defer x.lock.Unlock()

	if x.core.Contains(key) {
		// key存在，更新其值
		x.core.Add(key, &layLruItem[V]{
			value:   value,
			expires: time.Now().Add(x.successTTL).UnixMilli(),
		})
		return true
	}
	return false
}

// Del 删除指定key的缓存项
func (x *LayLRU[K, V]) Del(key K) bool {
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

// Stop 停止缓存服务
// 对于懒惰过期LRU，没有需要清理的资源
func (x *LayLRU[K, V]) Stop() {
	// 懒惰过期LRU不使用定时器，无需清理资源
}
