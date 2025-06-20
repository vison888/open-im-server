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

// NewSlotLRU 创建一个新的分槽LRU缓存实例
// 分槽LRU通过将缓存分散到多个独立的LRU实例中来减少锁竞争，提高并发性能
// K: 缓存键类型，必须可比较
// V: 缓存值类型，任意类型
// slotNum: 分槽数量，建议设置为CPU核心数的倍数
// hash: 哈希函数，用于将key映射到具体的槽位
// create: 创建单个LRU实例的工厂函数
// 返回: LRU接口实现
func NewSlotLRU[K comparable, V any](slotNum int, hash func(K) uint64, create func() LRU[K, V]) LRU[K, V] {
	x := &slotLRU[K, V]{
		n:     uint64(slotNum),
		slots: make([]LRU[K, V], slotNum),
		hash:  hash,
	}
	// 为每个槽位创建独立的LRU实例
	for i := 0; i < slotNum; i++ {
		x.slots[i] = create()
	}
	return x
}

// slotLRU 是分槽LRU缓存的实现
// 通过哈希将不同的key分散到不同的槽位，每个槽位使用独立的LRU实例和锁
type slotLRU[K comparable, V any] struct {
	n     uint64           // 槽位数量
	slots []LRU[K, V]      // 槽位数组，每个槽位是一个独立的LRU实例
	hash  func(k K) uint64 // 哈希函数，用于计算key应该分配到哪个槽位
}

// GetBatch 批量获取缓存项
// 根据key的哈希值将请求分散到不同的槽位，然后并行处理
func (x *slotLRU[K, V]) GetBatch(keys []K, fetch func(keys []K) (map[K]V, error)) (map[K]V, error) {
	var (
		slotKeys = make(map[uint64][]K) // 按槽位分组的key
		vs       = make(map[K]V)        // 最终结果
	)

	// 将keys按槽位分组
	for _, k := range keys {
		index := x.getIndex(k)
		slotKeys[index] = append(slotKeys[index], k)
	}

	// 为每个槽位并行执行批量获取
	for k, v := range slotKeys {
		batches, err := x.slots[k].GetBatch(v, fetch)
		if err != nil {
			return nil, err
		}
		// 合并结果
		for key, value := range batches {
			vs[key] = value
		}
	}
	return vs, nil
}

// getIndex 计算key应该分配到哪个槽位
// k: 要计算的key
// 返回: 槽位索引（0到n-1）
func (x *slotLRU[K, V]) getIndex(k K) uint64 {
	return x.hash(k) % x.n
}

// Get 获取单个缓存项
// 根据key的哈希值确定槽位，然后委托给对应槽位的LRU实例处理
func (x *slotLRU[K, V]) Get(key K, fetch func() (V, error)) (V, error) {
	return x.slots[x.getIndex(key)].Get(key, fetch)
}

// Set 设置缓存项
// 根据key的哈希值确定槽位，然后委托给对应槽位的LRU实例处理
func (x *slotLRU[K, V]) Set(key K, value V) {
	x.slots[x.getIndex(key)].Set(key, value)
}

// SetHas 如果key存在则更新其值
// 根据key的哈希值确定槽位，然后委托给对应槽位的LRU实例处理
func (x *slotLRU[K, V]) SetHas(key K, value V) bool {
	return x.slots[x.getIndex(key)].SetHas(key, value)
}

// Del 删除缓存项
// 根据key的哈希值确定槽位，然后委托给对应槽位的LRU实例处理
func (x *slotLRU[K, V]) Del(key K) bool {
	return x.slots[x.getIndex(key)].Del(key)
}

// Stop 停止所有槽位的LRU实例
// 遍历所有槽位，调用每个LRU实例的Stop方法
func (x *slotLRU[K, V]) Stop() {
	for _, slot := range x.slots {
		slot.Stop()
	}
}
