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
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/localcache/lru"
)

// defaultOption 返回默认的缓存配置选项
// 这些默认值经过优化，适合大多数使用场景
func defaultOption() *option {
	return &option{
		localSlotNum:    500,                                                    // 本地缓存槽位数量，分散锁竞争
		localSlotSize:   20000,                                                  // 每个槽位的容量
		linkSlotNum:     500,                                                    // 链接关系管理的槽位数量
		expirationEvict: false,                                                  // 默认使用懒惰过期策略
		localSuccessTTL: time.Minute,                                            // 成功数据的存活时间：1分钟
		localFailedTTL:  time.Second * 5,                                        // 失败数据的存活时间：5秒
		delFn:           make([]func(ctx context.Context, key ...string), 0, 2), // 删除回调函数列表
		target:          EmptyTarget{},                                          // 默认的空统计目标
	}
}

// option 定义了缓存的配置选项
// 包含缓存大小、过期策略、TTL等各种配置
type option struct {
	localSlotNum  int // 本地缓存的槽位数量，用于分散锁竞争
	localSlotSize int // 每个槽位的最大缓存项数量
	linkSlotNum   int // 链接关系管理的槽位数量，0表示禁用链接功能

	// expirationEvict 过期策略标志:
	// true: 主动过期，使用定时器主动清理过期数据，内存使用更高效但CPU消耗略高
	// false: 懒惰过期，访问时检查是否过期，CPU消耗低但过期数据可能占用内存较久
	expirationEvict bool

	localSuccessTTL time.Duration // 成功获取数据的缓存存活时间
	localFailedTTL  time.Duration // 获取数据失败的缓存存活时间

	// delFn 删除回调函数列表
	// 当调用Cache.Del方法时，会按顺序调用这些函数
	// 通常用于通知其他服务或清理相关资源
	delFn []func(ctx context.Context, key ...string)

	target lru.Target // 统计指标收集器，用于监控缓存性能
}

// Option 定义了配置选项的函数类型
// 使用函数式选项模式，允许用户灵活配置缓存
type Option func(o *option)

// WithExpirationEvict 启用主动过期策略
// 使用定时器主动清理过期的缓存项，适合内存敏感的场景
func WithExpirationEvict() Option {
	return func(o *option) {
		o.expirationEvict = true
	}
}

// WithLazy 启用懒惰过期策略（默认）
// 在访问时检查数据是否过期，适合CPU敏感的场景
func WithLazy() Option {
	return func(o *option) {
		o.expirationEvict = false
	}
}

// WithLocalDisable 禁用本地缓存
// 实际上是将本地槽位数量设置为0，禁用整个本地缓存功能
func WithLocalDisable() Option {
	return WithLinkSlotNum(0)
}

// WithLinkDisable 禁用链接功能
// 将链接槽位数量设置为0，缓存项之间不会建立关联关系
func WithLinkDisable() Option {
	return WithLinkSlotNum(0)
}

// WithLinkSlotNum 设置链接关系管理的槽位数量
// linkSlotNum: 槽位数量，0表示禁用链接功能
func WithLinkSlotNum(linkSlotNum int) Option {
	return func(o *option) {
		o.linkSlotNum = linkSlotNum
	}
}

// WithLocalSlotNum 设置本地缓存的槽位数量
// localSlotNum: 槽位数量，越多则锁竞争越小，但内存开销略大
// 建议设置为CPU核心数的倍数
func WithLocalSlotNum(localSlotNum int) Option {
	return func(o *option) {
		o.localSlotNum = localSlotNum
	}
}

// WithLocalSlotSize 设置每个槽位的最大缓存项数量
// localSlotSize: 每个槽位的容量，总缓存容量 = localSlotNum * localSlotSize
func WithLocalSlotSize(localSlotSize int) Option {
	return func(o *option) {
		o.localSlotSize = localSlotSize
	}
}

// WithLocalSuccessTTL 设置成功数据的缓存存活时间
// localSuccessTTL: 存活时间，必须大于0
// 成功获取的数据会被缓存这么长时间
func WithLocalSuccessTTL(localSuccessTTL time.Duration) Option {
	if localSuccessTTL < 0 {
		panic("localSuccessTTL should be greater than 0")
	}
	return func(o *option) {
		o.localSuccessTTL = localSuccessTTL
	}
}

// WithLocalFailedTTL 设置失败数据的缓存存活时间
// localFailedTTL: 存活时间，必须大于0
// 获取失败的数据也会被缓存，但时间通常较短，避免频繁重试
func WithLocalFailedTTL(localFailedTTL time.Duration) Option {
	if localFailedTTL < 0 {
		panic("localFailedTTL should be greater than 0")
	}
	return func(o *option) {
		o.localFailedTTL = localFailedTTL
	}
}

// WithTarget 设置统计指标收集器
// target: 统计目标，不能为nil
// 用于收集缓存命中率、成功率等指标，便于监控和调优
func WithTarget(target lru.Target) Option {
	if target == nil {
		panic("target should not be nil")
	}
	return func(o *option) {
		o.target = target
	}
}

// WithDeleteKeyBefore 添加删除前的回调函数
// fn: 回调函数，不能为nil
// 当调用Cache.Del方法时，会先调用这些回调函数，然后再删除缓存
// 可以多次调用此选项来添加多个回调函数
func WithDeleteKeyBefore(fn func(ctx context.Context, key ...string)) Option {
	if fn == nil {
		panic("fn should not be nil")
	}
	return func(o *option) {
		o.delFn = append(o.delFn, fn)
	}
}

// EmptyTarget 是一个空的统计目标实现
// 所有方法都是空操作，用作默认值当用户不需要统计功能时
type EmptyTarget struct{}

// IncrGetHit 增加缓存命中次数（空操作）
func (e EmptyTarget) IncrGetHit() {}

// IncrGetSuccess 增加获取成功次数（空操作）
func (e EmptyTarget) IncrGetSuccess() {}

// IncrGetFailed 增加获取失败次数（空操作）
func (e EmptyTarget) IncrGetFailed() {}

// IncrDelHit 增加删除命中次数（空操作）
func (e EmptyTarget) IncrDelHit() {}

// IncrDelNotFound 增加删除未找到次数（空操作）
func (e EmptyTarget) IncrDelNotFound() {}
