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

// compressor.go - 数据压缩处理模块
//
// 功能概述:
// 1. 提供统一的数据压缩和解压缩接口
// 2. 基于gzip算法实现高效的数据压缩
// 3. 使用对象池优化性能，减少内存分配和GC压力
// 4. 支持大规模并发压缩操作
//
// 设计思路:
// - 接口抽象: 定义统一的压缩器接口，便于扩展不同的压缩算法
// - 对象池优化: 使用sync.Pool复用gzip.Writer和gzip.Reader对象
// - 双模式支持: 提供普通模式和对象池模式，适应不同性能需求
// - 错误处理: 完善的错误包装和日志记录
//
// 性能优化:
// - 对象复用: 避免频繁创建和销毁压缩器对象
// - 内存优化: 减少内存分配，降低GC开销
// - 并发安全: 支持多协程并发压缩操作
// - 资源管理: 自动的资源获取和释放机制

package msggateway

import (
	"bytes"
	"compress/gzip"
	"io"
	"sync"

	"github.com/openimsdk/tools/errs"
)

// 全局对象池，用于复用gzip压缩器和解压器
// 设计思路：
// 1. 避免频繁创建和销毁gzip.Writer/Reader对象
// 2. 减少内存分配，提高性能
// 3. 支持高并发场景下的对象复用
var (
	// gzipWriterPool gzip写入器对象池
	// 每次压缩操作从池中获取Writer，使用完毕后归还
	gzipWriterPool = sync.Pool{New: func() any { return gzip.NewWriter(nil) }}

	// gzipReaderPool gzip读取器对象池
	// 每次解压操作从池中获取Reader，使用完毕后归还
	gzipReaderPool = sync.Pool{New: func() any { return new(gzip.Reader) }}
)

// Compressor 压缩器接口定义
// 设计思路：
// 1. 提供统一的压缩和解压缩方法
// 2. 支持普通模式和对象池优化模式
// 3. 易于扩展其他压缩算法（如LZ4、Snappy等）
// 4. 标准化的错误处理和返回值
type Compressor interface {
	// Compress 普通压缩方法
	// 适用于偶尔压缩的场景，无对象池优化
	Compress(rawData []byte) ([]byte, error)

	// CompressWithPool 使用对象池的压缩方法
	// 适用于高频压缩场景，性能更优
	CompressWithPool(rawData []byte) ([]byte, error)

	// DeCompress 普通解压方法
	// 适用于偶尔解压的场景，无对象池优化
	DeCompress(compressedData []byte) ([]byte, error)

	// DecompressWithPool 使用对象池的解压方法
	// 适用于高频解压场景，性能更优
	DecompressWithPool(compressedData []byte) ([]byte, error)
}

// GzipCompressor Gzip压缩器实现
// 基于标准库的gzip包实现数据压缩功能
type GzipCompressor struct {
	compressProtocol string // 压缩协议标识，用于日志和调试
}

// NewGzipCompressor 创建新的Gzip压缩器实例
// 返回初始化完成的压缩器，可立即使用
func NewGzipCompressor() *GzipCompressor {
	return &GzipCompressor{compressProtocol: "gzip"}
}

// Compress 普通模式的数据压缩
// 设计思路：
// 1. 每次创建新的gzip.Writer，适用于低频场景
// 2. 使用bytes.Buffer作为输出缓冲区
// 3. 确保Writer正确关闭，避免数据不完整
// 4. 完善的错误处理和包装
//
// 参数：
//   - rawData: 待压缩的原始数据
//
// 返回值：
//   - []byte: 压缩后的数据
//   - error: 错误信息
func (g *GzipCompressor) Compress(rawData []byte) ([]byte, error) {
	gzipBuffer := bytes.Buffer{}
	gz := gzip.NewWriter(&gzipBuffer)

	// 写入原始数据到压缩器
	if _, err := gz.Write(rawData); err != nil {
		return nil, errs.WrapMsg(err, "GzipCompressor.Compress: writing to gzip writer failed")
	}

	// 关闭压缩器，确保所有数据被写入
	if err := gz.Close(); err != nil {
		return nil, errs.WrapMsg(err, "GzipCompressor.Compress: closing gzip writer failed")
	}

	return gzipBuffer.Bytes(), nil
}

// CompressWithPool 使用对象池优化的数据压缩
// 设计思路：
// 1. 从对象池获取复用的gzip.Writer，提高性能
// 2. 使用Reset方法重置Writer状态，确保干净环境
// 3. 使用defer确保Writer正确归还到对象池
// 4. 适用于高频压缩场景，大幅提升性能
//
// 性能优势：
// - 避免重复创建和销毁gzip.Writer对象
// - 减少内存分配和垃圾回收开销
// - 支持高并发压缩操作
//
// 参数：
//   - rawData: 待压缩的原始数据
//
// 返回值：
//   - []byte: 压缩后的数据
//   - error: 错误信息
func (g *GzipCompressor) CompressWithPool(rawData []byte) ([]byte, error) {
	// 从对象池获取Writer
	gz := gzipWriterPool.Get().(*gzip.Writer)
	defer gzipWriterPool.Put(gz) // 确保归还到对象池

	gzipBuffer := bytes.Buffer{}
	gz.Reset(&gzipBuffer) // 重置Writer，绑定新的输出缓冲区

	// 写入数据并关闭
	if _, err := gz.Write(rawData); err != nil {
		return nil, errs.WrapMsg(err, "GzipCompressor.CompressWithPool: error writing data")
	}
	if err := gz.Close(); err != nil {
		return nil, errs.WrapMsg(err, "GzipCompressor.CompressWithPool: error closing gzip writer")
	}
	return gzipBuffer.Bytes(), nil
}

// DeCompress 普通模式的数据解压
// 设计思路：
// 1. 每次创建新的gzip.Reader，适用于低频场景
// 2. 使用io.ReadAll一次性读取所有解压数据
// 3. 确保Reader正确关闭，释放系统资源
// 4. 即使关闭失败也返回已解压的数据
//
// 参数：
//   - compressedData: 压缩后的数据
//
// 返回值：
//   - []byte: 解压后的原始数据
//   - error: 错误信息
func (g *GzipCompressor) DeCompress(compressedData []byte) ([]byte, error) {
	buff := bytes.NewBuffer(compressedData)
	reader, err := gzip.NewReader(buff)
	if err != nil {
		return nil, errs.WrapMsg(err, "GzipCompressor.DeCompress: NewReader creation failed")
	}

	// 读取所有解压数据
	decompressedData, err := io.ReadAll(reader)
	if err != nil {
		return nil, errs.WrapMsg(err, "GzipCompressor.DeCompress: reading from gzip reader failed")
	}

	// 关闭Reader
	if err = reader.Close(); err != nil {
		// 即使关闭失败，我们已经成功读取了数据
		// 返回解压数据和关闭失败的错误信息
		return decompressedData, errs.WrapMsg(err, "GzipCompressor.DeCompress: closing gzip reader failed")
	}
	return decompressedData, nil
}

// DecompressWithPool 使用对象池优化的数据解压
// 设计思路：
// 1. 从对象池获取复用的gzip.Reader，提高性能
// 2. 使用Reset方法重置Reader状态和输入源
// 3. 使用defer确保Reader正确归还到对象池
// 4. 适用于高频解压场景，大幅提升性能
//
// 性能优势：
// - 避免重复创建和销毁gzip.Reader对象
// - 减少内存分配和垃圾回收开销
// - 支持高并发解压操作
//
// 参数：
//   - compressedData: 压缩后的数据
//
// 返回值：
//   - []byte: 解压后的原始数据
//   - error: 错误信息
func (g *GzipCompressor) DecompressWithPool(compressedData []byte) ([]byte, error) {
	// 从对象池获取Reader
	reader := gzipReaderPool.Get().(*gzip.Reader)
	defer gzipReaderPool.Put(reader) // 确保归还到对象池

	// 重置Reader，绑定新的输入源
	err := reader.Reset(bytes.NewReader(compressedData))
	if err != nil {
		return nil, errs.WrapMsg(err, "GzipCompressor.DecompressWithPool: resetting gzip reader failed")
	}

	// 读取所有解压数据
	decompressedData, err := io.ReadAll(reader)
	if err != nil {
		return nil, errs.WrapMsg(err, "GzipCompressor.DecompressWithPool: reading from pooled gzip reader failed")
	}

	// 关闭Reader
	if err = reader.Close(); err != nil {
		// 类似DeCompress方法，返回数据和关闭失败的错误
		return decompressedData, errs.WrapMsg(err, "GzipCompressor.DecompressWithPool: closing pooled gzip reader failed")
	}
	return decompressedData, nil
}
