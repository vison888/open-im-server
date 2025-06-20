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

// AnyValue 是一个类型转换工具函数，用于将interface{}类型转换为指定的泛型类型
// 这个函数通常用于处理那些返回interface{}类型的函数，将其转换为具体的类型
// 如果转换过程中出现错误，会返回该类型的零值和错误信息
//
// V: 目标类型，可以是任意类型
// v: 要转换的值，类型为interface{}
// err: 可能的错误信息
//
// 返回值:
// - 如果err不为nil，返回类型V的零值和该错误
// - 如果err为nil，尝试将v转换为类型V并返回，如果转换失败会panic
//
// 使用场景:
// 当你有一个返回(interface{}, error)的函数，但你知道实际类型应该是V时，
// 可以使用这个函数进行安全的类型转换
func AnyValue[V any](v any, err error) (V, error) {
	if err != nil {
		// 如果有错误，返回零值和错误
		var zero V
		return zero, err
	}
	// 没有错误时，将interface{}断言为目标类型V
	// 注意：这里的类型断言如果失败会panic，调用者需要确保类型正确
	return v.(V), nil
}
