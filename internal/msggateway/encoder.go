// Copyright © 2023 OpenIM. All rights reserved.
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

// encoder.go - 消息编码器模块
//
// 功能概述:
// 1. 提供统一的消息编码和解码接口
// 2. 支持多种编码格式：Gob（Go专用）和JSON（通用）
// 3. 根据客户端SDK类型自动选择合适的编码器
// 4. 保证消息传输的效率和兼容性
//
// 设计思路:
// - 接口抽象: 定义统一的编码器接口，支持不同的编码实现
// - 多格式支持: Gob用于Go客户端（性能优），JSON用于其他客户端（兼容性好）
// - 自动选择: 根据SDK类型自动选择最优的编码器
// - 错误处理: 完善的编码错误包装和处理
//
// 编码器对比:
// - GobEncoder: Go语言专用，二进制格式，高性能，小体积
// - JsonEncoder: 通用格式，文本格式，跨语言兼容，可读性好
//
// 应用场景:
// - Go SDK客户端: 使用Gob编码，获得最佳性能
// - Web/JS客户端: 使用JSON编码，确保兼容性
// - 调试诊断: JSON格式便于人工阅读和调试

package msggateway

import (
	"bytes"
	"encoding/gob"
	"encoding/json"

	"github.com/openimsdk/tools/errs"
)

// Encoder 编码器接口定义
// 设计思路：
// 1. 提供统一的编码和解码方法
// 2. 支持任意数据类型的编码
// 3. 标准化的错误处理和返回值
// 4. 便于扩展其他编码格式（如protobuf、msgpack等）
type Encoder interface {
	// Encode 编码方法
	// 将任意数据类型编码为字节数组
	//
	// 参数：
	//   - data: 待编码的数据，支持任意可编码类型
	//
	// 返回值：
	//   - []byte: 编码后的字节数组
	//   - error: 编码错误信息
	Encode(data any) ([]byte, error)

	// Decode 解码方法
	// 将字节数组解码为指定的数据类型
	//
	// 参数：
	//   - encodeData: 编码后的字节数组
	//   - decodeData: 解码目标对象的指针
	//
	// 返回值：
	//   - error: 解码错误信息
	Decode(encodeData []byte, decodeData any) error
}

// GobEncoder Gob编码器实现
// 设计思路：
// 1. 基于Go标准库的gob包实现
// 2. 高效的二进制编码格式
// 3. 支持Go语言的所有数据类型
// 4. 体积小，性能高，适合Go客户端
//
// 优势：
// - 性能优异: 二进制格式，编解码速度快
// - 体积紧凑: 比JSON格式节省空间
// - 类型安全: 保持Go语言的类型信息
// - 功能完整: 支持复杂数据结构和指针
//
// 劣势：
// - Go专用: 仅限Go语言使用
// - 不可读: 二进制格式，无法人工阅读
// - 版本敏感: 结构体变更可能导致兼容性问题
type GobEncoder struct{}

// NewGobEncoder 创建新的Gob编码器实例
// 返回初始化完成的Gob编码器
func NewGobEncoder() Encoder {
	return GobEncoder{}
}

// Encode Gob编码实现
// 设计思路：
// 1. 使用bytes.Buffer作为输出缓冲区
// 2. 创建gob.Encoder进行编码
// 3. 完善的错误处理和包装
//
// 参数：
//   - data: 待编码的数据
//
// 返回值：
//   - []byte: 编码后的字节数组
//   - error: 编码错误信息
func (g GobEncoder) Encode(data any) ([]byte, error) {
	var buff bytes.Buffer
	enc := gob.NewEncoder(&buff)
	if err := enc.Encode(data); err != nil {
		return nil, errs.WrapMsg(err, "GobEncoder.Encode failed", "action", "encode")
	}
	return buff.Bytes(), nil
}

// Decode Gob解码实现
// 设计思路：
// 1. 使用bytes.Buffer包装输入数据
// 2. 创建gob.Decoder进行解码
// 3. 直接解码到目标对象
//
// 参数：
//   - encodeData: 编码后的字节数组
//   - decodeData: 解码目标对象的指针
//
// 返回值：
//   - error: 解码错误信息
func (g GobEncoder) Decode(encodeData []byte, decodeData any) error {
	buff := bytes.NewBuffer(encodeData)
	dec := gob.NewDecoder(buff)
	if err := dec.Decode(decodeData); err != nil {
		return errs.WrapMsg(err, "GobEncoder.Decode failed", "action", "decode")
	}
	return nil
}

// JsonEncoder JSON编码器实现
// 设计思路：
// 1. 基于Go标准库的json包实现
// 2. 文本格式，跨语言兼容
// 3. 人类可读，便于调试
// 4. 广泛支持，适合Web和多语言客户端
//
// 优势：
// - 跨语言: 几乎所有编程语言都支持JSON
// - 可读性: 文本格式，便于调试和诊断
// - 标准化: 遵循RFC 7159标准，兼容性好
// - Web友好: 浏览器原生支持，适合Web应用
//
// 劣势：
// - 体积较大: 文本格式比二进制占用更多空间
// - 性能较低: 解析文本比二进制慢
// - 类型限制: 不支持某些Go特有类型（如函数、通道等）
type JsonEncoder struct{}

// NewJsonEncoder 创建新的JSON编码器实例
// 返回初始化完成的JSON编码器
func NewJsonEncoder() Encoder {
	return JsonEncoder{}
}

// Encode JSON编码实现
// 设计思路：
// 1. 使用json.Marshal进行标准JSON编码
// 2. 简洁的错误处理
//
// 参数：
//   - data: 待编码的数据
//
// 返回值：
//   - []byte: 编码后的JSON字节数组
//   - error: 编码错误信息
func (g JsonEncoder) Encode(data any) ([]byte, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return nil, errs.New("JsonEncoder.Encode failed", "action", "encode")
	}
	return b, nil
}

// Decode JSON解码实现
// 设计思路：
// 1. 使用json.Unmarshal进行标准JSON解码
// 2. 直接解码到目标对象
//
// 参数：
//   - encodeData: JSON编码后的字节数组
//   - decodeData: 解码目标对象的指针
//
// 返回值：
//   - error: 解码错误信息
func (g JsonEncoder) Decode(encodeData []byte, decodeData any) error {
	err := json.Unmarshal(encodeData, decodeData)
	if err != nil {
		return errs.New("JsonEncoder.Decode failed", "action", "decode")
	}
	return nil
}
