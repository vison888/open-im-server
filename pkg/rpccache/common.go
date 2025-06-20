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

package rpccache

import (
	"github.com/openimsdk/tools/errs"
	"google.golang.org/protobuf/proto"
)

// newListMap 创建列表映射结构体
// 参数:
//   - values: 可比较类型的值列表
//   - err: 可能的错误
//
// 返回:
//   - *listMap[V]: 包含列表和映射的结构体
//   - error: 错误信息
//
// 功能:
//   - 将切片转换为既包含列表又包含映射的数据结构
//   - 列表保持原有顺序，映射提供O(1)查找性能
//   - 常用于需要同时支持遍历和快速查找的场景
//   - 如果输入有错误，直接返回错误不进行处理
func newListMap[V comparable](values []V, err error) (*listMap[V], error) {
	if err != nil {
		return nil, err
	}
	lm := &listMap[V]{
		List: values,
		Map:  make(map[V]struct{}, len(values)), // 预分配容量优化性能
	}
	// 将列表中的每个值添加到映射中，用于快速查找
	for _, value := range values {
		lm.Map[value] = struct{}{} // 使用空结构体节省内存
	}
	return lm, nil
}

// listMap 列表映射结构体
// 泛型类型V必须是可比较的类型（支持==和!=操作）
// 提供同时具备列表和映射特性的数据结构
type listMap[V comparable] struct {
	List []V            // 保持原有顺序的列表，用于遍历
	Map  map[V]struct{} // 映射表，用于O(1)时间复杂度的查找
}

// respProtoMarshal Protocol Buffers响应序列化函数
// 参数:
//   - resp: 实现了proto.Message接口的响应对象
//   - err: 可能存在的错误
//
// 返回:
//   - []byte: 序列化后的字节数组
//   - error: 序列化过程中的错误
//
// 功能:
//   - 将gRPC响应对象序列化为字节数组，用于缓存存储
//   - 如果输入参数包含错误，直接返回错误不进行序列化
//   - 使用Protocol Buffers标准序列化方法，保证跨语言兼容性
//   - 序列化后的数据可以存储在Redis、本地缓存等存储介质中
func respProtoMarshal(resp proto.Message, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	return proto.Marshal(resp)
}

// cacheUnmarshal 缓存反序列化函数（已废弃，使用cacheProto替代）
// 参数:
//   - resp: 序列化后的字节数组
//   - err: 可能存在的错误
//
// 返回:
//   - *V: 反序列化后的对象指针
//   - error: 反序列化过程中的错误
//
// 功能:
//   - 将字节数组反序列化为指定类型的对象
//   - 如果输入参数包含错误，直接返回错误
//   - 使用Protocol Buffers标准反序列化方法
//   - 通过类型断言将泛型对象转换为proto.Message接口
func cacheUnmarshal[V any](resp []byte, err error) (*V, error) {
	if err != nil {
		return nil, err
	}
	var val V
	// 将泛型类型断言为proto.Message接口，然后进行反序列化
	if err := proto.Unmarshal(resp, any(&val).(proto.Message)); err != nil {
		return nil, errs.WrapMsg(err, "local cache proto.Unmarshal error")
	}
	return &val, nil
}

// cacheProto 缓存Protocol Buffers处理器
// 泛型结构体，用于处理特定类型的Protocol Buffers序列化和反序列化
// 提供类型安全的缓存操作接口
type cacheProto[V any] struct{}

// Marshal 序列化方法
// 参数:
//   - resp: 待序列化的响应对象指针
//   - err: 可能存在的错误
//
// 返回:
//   - []byte: 序列化后的字节数组
//   - error: 序列化过程中的错误
//
// 功能:
//   - 将响应对象序列化为字节数组，用于缓存存储
//   - 如果输入参数包含错误，直接返回错误不进行序列化
//   - 通过类型断言确保对象实现了proto.Message接口
//   - 提供类型安全的序列化操作
func (cacheProto[V]) Marshal(resp *V, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	// 将泛型指针转换为proto.Message接口，然后进行序列化
	return proto.Marshal(any(resp).(proto.Message))
}

// Unmarshal 反序列化方法
// 参数:
//   - resp: 序列化后的字节数组
//   - err: 可能存在的错误
//
// 返回:
//   - *V: 反序列化后的对象指针
//   - error: 反序列化过程中的错误
//
// 功能:
//   - 将字节数组反序列化为指定类型的对象
//   - 如果输入参数包含错误，直接返回错误
//   - 创建泛型类型的零值，然后进行反序列化
//   - 提供类型安全的反序列化操作，避免类型转换错误
func (cacheProto[V]) Unmarshal(resp []byte, err error) (*V, error) {
	if err != nil {
		return nil, err
	}
	var val V // 创建泛型类型的零值
	// 将泛型对象转换为proto.Message接口，然后进行反序列化
	if err := proto.Unmarshal(resp, any(&val).(proto.Message)); err != nil {
		return nil, errs.WrapMsg(err, "local cache proto.Unmarshal error")
	}
	return &val, nil
}
