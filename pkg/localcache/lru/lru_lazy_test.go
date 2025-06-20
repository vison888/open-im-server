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
	"fmt"
	"hash/fnv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

// cacheTarget 实现了Target接口，用于收集缓存操作的统计信息
// 所有字段都使用int64类型以支持原子操作，确保并发安全
type cacheTarget struct {
	getHit      int64 // 缓存命中次数
	getSuccess  int64 // 获取成功次数
	getFailed   int64 // 获取失败次数
	delHit      int64 // 删除命中次数
	delNotFound int64 // 删除未找到次数
}

// IncrGetHit 原子地增加缓存命中次数
func (r *cacheTarget) IncrGetHit() {
	atomic.AddInt64(&r.getHit, 1)
}

// IncrGetSuccess 原子地增加获取成功次数
func (r *cacheTarget) IncrGetSuccess() {
	atomic.AddInt64(&r.getSuccess, 1)
}

// IncrGetFailed 原子地增加获取失败次数
func (r *cacheTarget) IncrGetFailed() {
	atomic.AddInt64(&r.getFailed, 1)
}

// IncrDelHit 原子地增加删除命中次数
func (r *cacheTarget) IncrDelHit() {
	atomic.AddInt64(&r.delHit, 1)
}

// IncrDelNotFound 原子地增加删除未找到次数
func (r *cacheTarget) IncrDelNotFound() {
	atomic.AddInt64(&r.delNotFound, 1)
}

// String 返回统计信息的字符串表示，用于调试和监控
func (r *cacheTarget) String() string {
	return fmt.Sprintf("getHit: %d, getSuccess: %d, getFailed: %d, delHit: %d, delNotFound: %d",
		r.getHit, r.getSuccess, r.getFailed, r.delHit, r.delNotFound)
}

// TestName LRU缓存的高并发性能测试
// 这个测试验证了分槽LRU缓存在高并发场景下的性能表现
func TestName(t *testing.T) {
	// 创建统计目标，用于收集性能指标
	target := &cacheTarget{}

	// 创建分槽LRU缓存
	// 参数说明：
	// - 100: 槽位数量，通过分片减少锁竞争
	// - 哈希函数: 使用FNV算法将string key映射到uint64
	// - 工厂函数: 创建主动过期LRU实例，容量100，成功TTL60秒，失败TTL1秒
	l := NewSlotLRU[string, string](100, func(k string) uint64 {
		h := fnv.New64a()
		// 使用unsafe.Pointer避免字符串到字节切片的内存拷贝
		h.Write(*(*[]byte)(unsafe.Pointer(&k)))
		return h.Sum64()
	}, func() LRU[string, string] {
		return NewExpirationLRU[string, string](100, time.Second*60, time.Second, target, nil)
	})

	// 注释中的替代实现，可以用于对比测试
	//l := NewInertiaLRU[string, string](1000, time.Second*20, time.Second*5, target)

	// fn 定义了单个key的测试函数
	// key: 要测试的缓存键
	// n: 执行次数
	// fetch: 获取数据的函数
	fn := func(key string, n int, fetch func() (string, error)) {
		for i := 0; i < n; i++ {
			// 执行缓存获取操作
			v, err := l.Get(key, fetch)
			// 这里使用匿名函数消费返回值，避免编译器优化
			func(v ...any) {}(v, err)
		}
	}

	// 用于统计实际使用的key数量
	tmp := make(map[string]struct{})

	var wg sync.WaitGroup

	// 启动10000个goroutine进行并发测试
	for i := 0; i < 10000; i++ {
		wg.Add(1)
		// 生成key，通过取模限制key的数量为200个
		// 这样可以测试缓存在有限key集合上的性能
		key := fmt.Sprintf("key_%d", i%200)
		tmp[key] = struct{}{}

		go func() {
			defer wg.Done()
			// 每个goroutine执行10000次缓存获取操作
			fn(key, 10000, func() (string, error) {
				// 模拟数据获取，返回带前缀的key作为值
				return "value_" + key, nil
			})
		}()

		// 注释掉的删除测试代码
		// 可以启用来测试并发读写场景
		//wg.Add(1)
		//go func() {
		//	defer wg.Done()
		//	for i := 0; i < 10; i++ {
		//		l.Del(key)
		//		time.Sleep(time.Second / 3)
		//	}
		//}()
	}

	// 等待所有goroutine完成
	wg.Wait()

	// 输出测试结果
	t.Log("实际key数量:", len(tmp))
	t.Log("缓存统计信息:", target.String())

	// 性能分析提示：
	// - getHit/getSuccess 的比值反映了缓存命中率
	// - 高命中率说明缓存有效减少了数据获取次数
	// - 在这个测试中，由于key数量有限（200个）而访问次数很多，
	//   预期会有很高的缓存命中率
}
