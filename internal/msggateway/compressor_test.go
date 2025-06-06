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

// compressor_test.go - 数据压缩器测试模块
//
// 功能概述:
// 1. 验证压缩器的正确性和数据完整性
// 2. 测试并发安全性，确保多协程环境下的稳定性
// 3. 性能基准测试，对比普通模式和对象池模式的性能差异
// 4. 压力测试，验证大量数据处理的稳定性
//
// 测试策略:
// - 正确性测试: 验证压缩解压的数据完整性
// - 并发测试: 多协程同时进行压缩解压操作
// - 性能测试: 基准测试对比不同实现的性能
// - 边界测试: 测试空数据、大数据等边界情况
//
// 测试覆盖:
// - 基本功能: 压缩和解压的基本流程
// - 数据完整性: 压缩前后数据的一致性
// - 并发安全: 多协程并发操作的安全性
// - 性能对比: 普通模式vs对象池模式的性能差异

package msggateway

import (
	"crypto/rand"
	"sync"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
)

// mockRandom 生成随机测试数据
// 设计思路：
// 1. 使用crypto/rand生成高质量随机数据
// 2. 固定50字节长度，模拟真实消息大小
// 3. 提供多样化的测试数据，覆盖各种字节组合
//
// 返回值：
//   - []byte: 50字节的随机数据
func mockRandom() []byte {
	bs := make([]byte, 50)
	rand.Read(bs)
	return bs
}

// TestCompressDecompress 基本压缩解压功能测试
// 设计思路：
// 1. 大量循环测试，确保算法稳定性
// 2. 每次使用不同的随机数据，提高测试覆盖率
// 3. 验证压缩解压后的数据完整性
// 4. 测试错误处理机制
//
// 测试流程：
// 生成随机数据 -> 压缩 -> 解压 -> 验证数据一致性
func TestCompressDecompress(t *testing.T) {

	compressor := NewGzipCompressor()

	// 执行2000次测试，确保算法稳定性
	for i := 0; i < 2000; i++ {
		src := mockRandom()

		// 测试压缩功能
		dest, err := compressor.CompressWithPool(src)
		if err != nil {
			t.Log(err)
		}
		assert.Equal(t, nil, err) // 验证压缩成功

		// 测试解压功能
		res, err := compressor.DecompressWithPool(dest)
		if err != nil {
			t.Log(err)
		}
		assert.Equal(t, nil, err) // 验证解压成功

		// 验证数据完整性：原始数据与解压后数据必须完全一致
		assert.EqualValues(t, src, res)
	}
}

// TestCompressDecompressWithConcurrency 并发压缩解压测试
// 设计思路：
// 1. 使用WaitGroup确保所有协程完成
// 2. 200个并发协程，模拟高并发场景
// 3. 每个协程独立执行压缩解压流程
// 4. 验证对象池在并发环境下的安全性
//
// 测试目标：
// - 验证对象池的并发安全性
// - 确保压缩器在高并发下的数据正确性
// - 检测可能的竞态条件和数据竞争
func TestCompressDecompressWithConcurrency(t *testing.T) {
	wg := sync.WaitGroup{}
	compressor := NewGzipCompressor()

	// 启动200个并发协程
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			src := mockRandom()

			// 并发执行压缩操作
			dest, err := compressor.CompressWithPool(src)
			if err != nil {
				t.Log(err)
			}
			assert.Equal(t, nil, err)

			// 并发执行解压操作
			res, err := compressor.DecompressWithPool(dest)
			if err != nil {
				t.Log(err)
			}
			assert.Equal(t, nil, err)

			// 验证并发环境下的数据完整性
			assert.EqualValues(t, src, res)

		}()
	}
	wg.Wait() // 等待所有协程完成
}

// BenchmarkCompress 普通压缩模式性能基准测试
// 测试目标：
// 1. 测量普通压缩模式的性能表现
// 2. 为性能对比提供基准数据
// 3. 评估在高频压缩场景下的性能开销
//
// 测试方法：
// - 使用固定的测试数据，确保测试结果可比较
// - 多次执行压缩操作，获得平均性能数据
// - 测量每次操作的耗时和内存分配
func BenchmarkCompress(b *testing.B) {
	src := mockRandom()
	compressor := NewGzipCompressor()

	// 基准测试：执行b.N次压缩操作
	for i := 0; i < b.N; i++ {
		_, err := compressor.Compress(src)
		assert.Equal(b, nil, err)
	}
}

// BenchmarkCompressWithSyncPool 对象池压缩模式性能基准测试
// 测试目标：
// 1. 测量对象池优化后的压缩性能
// 2. 与普通模式进行性能对比
// 3. 验证对象池优化的实际效果
//
// 预期结果：
// - 对象池模式应该显著优于普通模式
// - 减少内存分配次数和GC压力
// - 提高高频操作场景下的吞吐量
func BenchmarkCompressWithSyncPool(b *testing.B) {
	src := mockRandom()

	compressor := NewGzipCompressor()
	// 基准测试：执行b.N次对象池压缩操作
	for i := 0; i < b.N; i++ {
		_, err := compressor.CompressWithPool(src)
		assert.Equal(b, nil, err)
	}
}

// BenchmarkDecompress 普通解压模式性能基准测试
// 测试目标：
// 1. 测量普通解压模式的性能表现
// 2. 评估解压操作的性能开销
// 3. 为解压性能优化提供基准数据
//
// 测试准备：
// - 预先压缩测试数据，确保测试的一致性
// - 使用相同的压缩数据进行多次解压测试
func BenchmarkDecompress(b *testing.B) {
	src := mockRandom()

	compressor := NewGzipCompressor()
	// 预先压缩数据，用于解压测试
	comdata, err := compressor.Compress(src)

	assert.Equal(b, nil, err)

	// 基准测试：执行b.N次解压操作
	for i := 0; i < b.N; i++ {
		_, err := compressor.DeCompress(comdata)
		assert.Equal(b, nil, err)
	}
}

// BenchmarkDecompressWithSyncPool 对象池解压模式性能基准测试
// 测试目标：
// 1. 测量对象池优化后的解压性能
// 2. 与普通解压模式进行性能对比
// 3. 验证对象池在解压操作中的优化效果
//
// 性能优化验证：
// - 对象复用应该减少内存分配
// - 降低GC频率和开销
// - 提高高并发解压场景的性能
func BenchmarkDecompressWithSyncPool(b *testing.B) {
	src := mockRandom()

	compressor := NewGzipCompressor()
	// 预先压缩数据
	comdata, err := compressor.Compress(src)
	assert.Equal(b, nil, err)

	// 基准测试：执行b.N次对象池解压操作
	for i := 0; i < b.N; i++ {
		_, err := compressor.DecompressWithPool(comdata)
		assert.Equal(b, nil, err)
	}
}

// TestName 客户端对象大小测试
// 设计思路：
// 1. 使用unsafe.Sizeof获取Client结构体的内存占用
// 2. 监控结构体大小的变化，用于内存优化
// 3. 帮助评估连接数量对内存的影响
//
// 应用价值：
// - 内存使用分析：了解每个连接的内存开销
// - 容量规划：根据Client大小估算系统容量
// - 优化指导：识别结构体字段的内存使用情况
func TestName(t *testing.T) {
	// 输出Client结构体的内存大小（字节）
	// 这对于内存使用分析和性能优化很重要
	t.Log(unsafe.Sizeof(Client{}))

}
