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

package link

import (
	"hash/fnv"
	"sync"
	"unsafe"
)

// Link 定义了链接关系管理的接口
// 用于管理缓存key之间的关联关系，当一个key被删除时，其关联的key也会被删除
type Link interface {
	// Link 建立key与其他key的关联关系
	// key: 主键
	// link: 与主键关联的键列表
	Link(key string, link ...string)

	// Del 删除指定key及其所有关联关系
	// key: 要删除的键
	// 返回: 与该key关联的所有key集合（包括自身）
	Del(key string) map[string]struct{}
}

// newLinkKey 创建一个新的linkKey实例
// 返回: linkKey指针
func newLinkKey() *linkKey {
	return &linkKey{
		data: make(map[string]map[string]struct{}),
	}
}

// linkKey 管理单个分片的链接关系
// 使用互斥锁保证并发安全
type linkKey struct {
	lock sync.Mutex                     // 互斥锁，保护data字段的并发访问
	data map[string]map[string]struct{} // 存储链接关系：key -> 关联的key集合
}

// link 在当前分片中建立key与其他key的关联关系
// key: 主键
// link: 与主键关联的键列表
func (x *linkKey) link(key string, link ...string) {
	x.lock.Lock()
	defer x.lock.Unlock()

	// 获取或创建当前key的关联集合
	v, ok := x.data[key]
	if !ok {
		v = make(map[string]struct{})
		x.data[key] = v
	}

	// 将所有关联key添加到集合中
	for _, k := range link {
		v[k] = struct{}{}
	}
}

// del 删除指定key的所有关联关系
// key: 要删除的键
// 返回: 与该key关联的所有key集合，如果key不存在则返回nil
func (x *linkKey) del(key string) map[string]struct{} {
	x.lock.Lock()
	defer x.lock.Unlock()

	// 查找key的关联关系
	ks, ok := x.data[key]
	if !ok {
		return nil
	}

	// 删除key的关联关系
	delete(x.data, key)
	return ks
}

// New 创建一个新的Link实例
// n: 分片数量，用于减少锁竞争，必须大于0
// 返回: Link接口实现
func New(n int) Link {
	if n <= 0 {
		panic("must be greater than 0")
	}

	// 创建指定数量的分片
	slots := make([]*linkKey, n)
	for i := 0; i < len(slots); i++ {
		slots[i] = newLinkKey()
	}

	return &slot{
		n:     uint64(n),
		slots: slots,
	}
}

// slot 是Link接口的分片实现
// 通过哈希将key分散到不同的分片中，减少锁竞争
type slot struct {
	n     uint64     // 分片数量
	slots []*linkKey // 分片数组
}

// index 计算字符串s应该分配到哪个分片
// s: 输入字符串
// 返回: 分片索引（0到n-1）
func (x *slot) index(s string) uint64 {
	h := fnv.New64a()
	// 使用unsafe.Pointer避免字符串到字节切片的内存拷贝
	_, _ = h.Write(*(*[]byte)(unsafe.Pointer(&s)))
	return h.Sum64() % x.n
}

// Link 实现Link接口的Link方法
// 建立双向关联关系：key关联到所有link，每个link也关联到key
func (x *slot) Link(key string, link ...string) {
	if len(link) == 0 {
		return
	}

	mk := key
	// 创建link的副本，避免外部修改影响内部逻辑
	lks := make([]string, len(link))
	for i, k := range link {
		lks[i] = k
	}

	// 在主key的分片中建立：主key -> 关联key的关系
	x.slots[x.index(mk)].link(mk, lks...)

	// 在每个关联key的分片中建立：关联key -> 主key的关系
	// 这样形成双向关联，删除任一key都能找到所有相关的key
	for _, lk := range lks {
		x.slots[x.index(lk)].link(lk, mk)
	}
}

// Del 实现Link接口的Del方法
// 删除指定key及其所有关联关系
func (x *slot) Del(key string) map[string]struct{} {
	return x.delKey(key)
}

// delKey 递归删除key及其所有关联的key
// 使用深度优先搜索遍历所有关联关系
// k: 要删除的键
// 返回: 所有被删除的key集合
func (x *slot) delKey(k string) map[string]struct{} {
	del := make(map[string]struct{}) // 记录已删除的key，避免重复处理
	stack := []string{k}             // 使用栈进行深度优先搜索

	// 遍历所有需要删除的key
	for len(stack) > 0 {
		// 弹出栈顶元素
		curr := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		// 如果已经处理过该key，跳过
		if _, ok := del[curr]; ok {
			continue
		}

		// 标记当前key已删除
		del[curr] = struct{}{}

		// 从对应分片中删除当前key，获取其关联的key
		childKeys := x.slots[x.index(curr)].del(curr)

		// 将所有关联的key加入栈中，等待处理
		for ck := range childKeys {
			stack = append(stack, ck)
		}
	}

	return del
}
