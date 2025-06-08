// Package batcher_test 批处理器测试包
// 包含对批处理器功能的综合测试，验证批处理、并发处理、同步等待等核心功能
package batcher

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/openimsdk/tools/utils/stringutil"
)

// TestBatcher 批处理器综合测试函数
// 测试内容：
// 1. 批处理器的创建和配置
// 2. 大量数据的批处理能力（10000条消息）
// 3. 多工作协程的并发处理
// 4. 同步等待机制
// 5. 分片和负载均衡
// 6. 优雅关闭和资源清理
func TestBatcher(t *testing.T) {
	// 定义测试配置
	config := Config{
		size:     1000,                 // 批处理大小：1000条消息触发一次处理
		buffer:   10,                   // 工作协程缓冲区：每个协程通道缓冲10条消息
		worker:   10,                   // 工作协程数：10个并发处理协程
		interval: 5 * time.Millisecond, // 定时触发间隔：5毫秒
	}

	// 创建字符串类型的批处理器实例，启用同步等待
	b := New[string](
		WithSize(config.size),         // 设置批处理大小
		WithBuffer(config.buffer),     // 设置缓冲区大小
		WithWorker(config.worker),     // 设置工作协程数
		WithInterval(config.interval), // 设置定时触发间隔
		WithSyncWait(true),            // 启用同步等待模式
	)

	// 设置批处理执行函数：模拟处理逻辑，记录处理信息
	b.Do = func(ctx context.Context, channelID int, vals *Msg[string]) {
		t.Logf("Channel %d Processed batch: %v", channelID, vals)
	}

	// 设置完成回调函数：批处理完成时的回调
	b.OnComplete = func(lastMessage *string, totalCount int) {
		t.Logf("Completed processing with last message: %v, total count: %d", *lastMessage, totalCount)
	}

	// 设置分片函数：根据key的哈希值分配到不同工作协程
	b.Sharding = func(key string) int {
		hashCode := stringutil.GetHashCode(key) // 计算字符串哈希值
		return int(hashCode) % config.worker    // 取模分配到对应协程
	}

	// 设置键提取函数：直接使用字符串值作为key
	b.Key = func(data *string) string {
		return *data
	}

	// 启动批处理器
	err := b.Start()
	if err != nil {
		t.Fatal(err)
	}

	// 测试大量数据的批处理：投入10000条测试数据
	for i := 0; i < 10000; i++ {
		data := "data" + fmt.Sprintf("%d", i) // 生成测试数据
		if err := b.Put(context.Background(), &data); err != nil {
			t.Fatal(err)
		}
	}

	// 等待1秒确保所有数据处理完成
	time.Sleep(time.Duration(1) * time.Second)

	// 测试优雅关闭性能
	start := time.Now()
	b.Close() // 关闭批处理器，等待所有协程结束
	elapsed := time.Since(start)
	t.Logf("Close took %s", elapsed)

	// 验证资源清理：数据通道应该为空
	if len(b.data) != 0 {
		t.Error("Data channel should be empty after closing")
	}
}
