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
	"testing"
)

// TestName 测试链接功能的基本用法
// 演示了如何建立缓存key之间的关联关系以及删除操作的级联效果
func TestName(t *testing.T) {
	// 创建一个使用单个分片的链接管理器
	// 在生产环境中，建议使用更多分片以提高并发性能
	v := New(1)

	// 建立第一组关联关系
	// 将 "a:1" 与 "b:1", "c:1" 建立关联
	// 这意味着删除任何一个key都会导致其他相关key被删除
	v.Link("a:1", "b:1", "c:1")

	// 建立第二组关联关系
	// 将 "z:1" 与 "b:1" 建立关联
	// 注意：这里 "b:1" 已经与 "a:1", "c:1" 关联了
	// 所以现在 "z:1" 间接地与 "a:1", "c:1" 也有关联
	v.Link("z:1", "b:1")

	// 删除 "z:1"
	// 由于关联关系，这个操作可能会影响到其他key
	// Del方法会返回所有被删除的key集合
	deletedKeys := v.Del("z:1")

	// 打印链接管理器的状态，用于调试
	t.Log("链接管理器状态:", v)

	// 打印被删除的key集合
	t.Log("被删除的key:", deletedKeys)

	// 预期的删除结果分析：
	// 1. "z:1" 直接删除
	// 2. "b:1" 与 "z:1" 关联，所以也会被删除
	// 3. "a:1" 与 "b:1" 关联，所以也会被删除
	// 4. "c:1" 与 "b:1" 关联，所以也会被删除
	// 最终所有key都会被删除，这展示了级联删除的威力
}
