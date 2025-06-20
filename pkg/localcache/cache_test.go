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
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestName 缓存系统的并发性能测试
// 这个测试模拟了高并发场景下的缓存读写操作，用于验证缓存的性能和正确性
func TestName(t *testing.T) {
	// 创建缓存实例，使用主动过期策略
	// 也可以使用 New[string]() 来使用默认的懒惰过期策略
	c := New[string](WithExpirationEvict())
	//c := New[string]()

	ctx := context.Background()

	// 测试参数配置
	const (
		num  = 10000  // 每个goroutine的操作次数
		tNum = 10000  // 总的goroutine数量（包括读和写）
		kNum = 100000 // key的总数量，用于生成不同的key
		pNum = 100    // 打印间隔，每pNum次操作打印一次统计
	)

	// getKey 根据数值生成key字符串
	// 通过取模操作限制key的数量，模拟实际场景中的key重复访问
	getKey := func(v uint64) string {
		return fmt.Sprintf("key_%d", v%kNum)
	}

	// 记录测试开始时间
	start := time.Now()
	t.Log("测试开始时间:", start)

	// 使用原子操作统计读写次数，确保并发安全
	var (
		get atomic.Int64 // 读操作计数器
		del atomic.Int64 // 删除操作计数器
	)

	// incrGet 增加读操作计数，并定期打印统计信息
	incrGet := func() {
		if v := get.Add(1); v%pNum == 0 {
			//t.Log("#读操作计数:", v/pNum)
		}
	}

	// incrDel 增加删除操作计数，并定期打印统计信息
	incrDel := func() {
		if v := del.Add(1); v%pNum == 0 {
			//t.Log("@删除操作计数:", v/pNum)
		}
	}

	var wg sync.WaitGroup

	// 启动并发测试
	// 每次循环创建2个goroutine：1个读操作，1个删除操作
	for i := 0; i < tNum; i++ {
		// 读操作goroutine
		wg.Add(1)
		go func() {
			defer wg.Done()
			// 每个goroutine执行num次读操作
			for i := 0; i < num; i++ {
				// 使用随机key进行缓存读取
				// 如果缓存不存在，通过fetch函数获取数据
				c.Get(ctx, getKey(rand.Uint64()), func(ctx context.Context) (string, error) {
					// 模拟数据获取过程，返回格式化的字符串
					return fmt.Sprintf("index_%d", i), nil
				})
				incrGet()
			}
		}()

		// 删除操作goroutine
		wg.Add(1)
		go func() {
			defer wg.Done()
			// 稍微延迟启动删除操作，让缓存先积累一些数据
			time.Sleep(time.Second / 10)
			// 每个goroutine执行num次删除操作
			for i := 0; i < num; i++ {
				// 随机删除缓存key
				c.Del(ctx, getKey(rand.Uint64()))
				incrDel()
			}
		}()
	}

	// 等待所有goroutine完成
	wg.Wait()

	// 记录测试结束时间并计算耗时
	end := time.Now()
	t.Log("测试结束时间:", end)
	t.Log("总耗时:", end.Sub(start))
	t.Log("总读操作次数:", get.Load())
	t.Log("总删除操作次数:", del.Load())

	// 性能分析注释
	// 原代码注释显示某次测试耗时137.35s
	// 实际性能会受到以下因素影响：
	// 1. 硬件配置（CPU核心数、内存大小）
	// 2. 并发goroutine数量
	// 3. 缓存配置（槽位数量、容量大小）
	// 4. 过期策略（主动vs懒惰）
	// 5. key的分布情况（是否均匀分布到各个槽位）
}
