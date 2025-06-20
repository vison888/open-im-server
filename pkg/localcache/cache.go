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
	"context"
	"hash/fnv"
	"unsafe"

	"github.com/openimsdk/open-im-server/v3/pkg/localcache/link"
	"github.com/openimsdk/open-im-server/v3/pkg/localcache/lru"
)

// Cache 定义了本地缓存的接口
// V any 表示缓存值的类型，支持泛型
type Cache[V any] interface {
	// Get 根据key获取缓存值，如果缓存不存在则调用fetch函数获取
	// ctx: 上下文对象
	// key: 缓存的键
	// fetch: 当缓存不存在时用于获取数据的函数
	Get(ctx context.Context, key string, fetch func(ctx context.Context) (V, error)) (V, error)

	// GetLink 根据key获取缓存值，并建立与其他key的关联关系
	// ctx: 上下文对象
	// key: 缓存的键
	// fetch: 当缓存不存在时用于获取数据的函数
	// link: 与当前key关联的其他key列表，当任一关联key被删除时，当前key也会被删除
	GetLink(ctx context.Context, key string, fetch func(ctx context.Context) (V, error), link ...string) (V, error)

	// Del 删除指定的缓存key，会触发删除回调函数
	// ctx: 上下文对象
	// key: 要删除的缓存键列表
	Del(ctx context.Context, key ...string)

	// DelLocal 仅删除本地缓存，不触发删除回调函数
	// ctx: 上下文对象
	// key: 要删除的缓存键列表
	DelLocal(ctx context.Context, key ...string)

	// Stop 停止缓存服务，清理资源
	Stop()
}

// LRUStringHash 使用FNV哈希算法计算字符串的哈希值
// 这是一个高性能的哈希函数，用于将字符串映射到LRU缓存的不同槽位
// key: 要计算哈希的字符串
// 返回: 64位无符号整数哈希值
func LRUStringHash(key string) uint64 {
	h := fnv.New64a()
	// unsafe.Pointer转换避免了字符串到字节切片的内存拷贝，提高性能
	h.Write(*(*[]byte)(unsafe.Pointer(&key)))
	return h.Sum64()
}

// New 创建一个新的缓存实例
// V any 表示缓存值的类型
// opts: 可变参数选项，用于配置缓存行为
// 返回: Cache接口的实现
func New[V any](opts ...Option) Cache[V] {
	// 使用默认选项初始化
	opt := defaultOption()
	// 应用用户提供的选项
	for _, o := range opts {
		o(opt)
	}

	// 创建缓存实例
	c := cache[V]{opt: opt}

	// 如果配置了本地缓存参数，则创建本地LRU缓存
	if opt.localSlotNum > 0 && opt.localSlotSize > 0 {
		// 创建LRU缓存的工厂函数
		createSimpleLRU := func() lru.LRU[string, V] {
			if opt.expirationEvict {
				// 使用主动过期的LRU缓存（定时器清理过期数据）
				return lru.NewExpirationLRU[string, V](opt.localSlotSize, opt.localSuccessTTL, opt.localFailedTTL, opt.target, c.onEvict)
			} else {
				// 使用懒惰过期的LRU缓存（访问时检查是否过期）
				return lru.NewLayLRU[string, V](opt.localSlotSize, opt.localSuccessTTL, opt.localFailedTTL, opt.target, c.onEvict)
			}
		}

		if opt.localSlotNum == 1 {
			// 单槽LRU缓存
			c.local = createSimpleLRU()
		} else {
			// 多槽LRU缓存，通过哈希分片减少锁竞争
			c.local = lru.NewSlotLRU[string, V](opt.localSlotNum, LRUStringHash, createSimpleLRU)
		}

		// 如果配置了链接功能，则创建链接管理器
		if opt.linkSlotNum > 0 {
			c.link = link.New(opt.linkSlotNum)
		}
	}
	return &c
}

// cache 是Cache接口的具体实现
type cache[V any] struct {
	opt   *option            // 配置选项
	link  link.Link          // 链接关系管理器，用于处理缓存key之间的关联关系
	local lru.LRU[string, V] // 本地LRU缓存
}

// onEvict 是LRU缓存的驱逐回调函数
// 当缓存项被驱逐时，同时删除与之关联的其他缓存项
// key: 被驱逐的缓存键
// value: 被驱逐的缓存值
func (c *cache[V]) onEvict(key string, value V) {
	if c.link != nil {
		// 删除与当前key关联的所有key，并获取关联的key列表
		lks := c.link.Del(key)
		// 删除所有关联的缓存项
		for k := range lks {
			if key != k { // 防止死锁，不删除自己
				c.local.Del(k)
			}
		}
	}
}

// del 内部删除方法，删除指定的缓存key
// key: 要删除的缓存键列表
func (c *cache[V]) del(key ...string) {
	if c.local == nil {
		return
	}

	for _, k := range key {
		// 删除本地缓存
		c.local.Del(k)
		// 如果启用了链接功能，还需要删除关联的缓存
		if c.link != nil {
			lks := c.link.Del(k)
			// 删除所有关联的缓存项
			for k := range lks {
				c.local.Del(k)
			}
		}
	}
}

// Get 实现Cache接口的Get方法
// 这是GetLink方法的简化版本，不建立任何关联关系
func (c *cache[V]) Get(ctx context.Context, key string, fetch func(ctx context.Context) (V, error)) (V, error) {
	return c.GetLink(ctx, key, fetch)
}

// GetLink 实现Cache接口的GetLink方法
// 获取缓存值并可选择性地建立与其他key的关联关系
func (c *cache[V]) GetLink(ctx context.Context, key string, fetch func(ctx context.Context) (V, error), link ...string) (V, error) {
	if c.local != nil {
		// 从本地LRU缓存获取数据
		return c.local.Get(key, func() (V, error) {
			// 如果提供了关联key，建立链接关系
			if len(link) > 0 {
				c.link.Link(key, link...)
			}
			// 调用fetch函数获取实际数据
			return fetch(ctx)
		})
	} else {
		// 如果没有本地缓存，直接调用fetch函数
		return fetch(ctx)
	}
}

// Del 实现Cache接口的Del方法
// 删除缓存并触发删除回调函数
func (c *cache[V]) Del(ctx context.Context, key ...string) {
	// 首先调用所有配置的删除回调函数
	for _, fn := range c.opt.delFn {
		fn(ctx, key...)
	}
	// 然后删除本地缓存
	c.del(key...)
}

// DelLocal 实现Cache接口的DelLocal方法
// 仅删除本地缓存，不触发删除回调函数
func (c *cache[V]) DelLocal(ctx context.Context, key ...string) {
	c.del(key...)
}

// Stop 实现Cache接口的Stop方法
// 停止缓存服务，清理资源
func (c *cache[V]) Stop() {
	c.local.Stop()
}
