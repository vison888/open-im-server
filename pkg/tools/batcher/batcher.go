// Package batcher 批处理器包
// 提供高性能的泛型批处理器，支持数据聚合、分片处理和并发执行
// 主要用于优化大量数据的处理性能，通过批量聚合减少系统调用开销
package batcher

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/utils/idutil"
)

// 默认配置常量
var (
	DefaultDataChanSize = 1000        // 默认数据通道缓冲区大小
	DefaultSize         = 100         // 默认批处理大小（聚合多少条消息后触发处理）
	DefaultBuffer       = 100         // 默认工作协程的通道缓冲区大小
	DefaultWorker       = 5           // 默认并发工作协程数量
	DefaultInterval     = time.Second // 默认定时触发间隔（即使未达到批处理大小也会触发）
)

// Config 批处理器配置结构体
type Config struct {
	size       int           // 批处理聚合大小：达到此数量的消息时触发批处理
	buffer     int           // 单个工作协程的通道缓冲区大小：每个worker处理通道的缓冲容量
	dataBuffer int           // 主数据通道大小：接收待处理数据的通道容量
	worker     int           // 并行处理的协程数量：控制并发处理能力
	interval   time.Duration // 定时聚合触发间隔：即使未达到size也会定时触发处理
	syncWait   bool          // 是否等待消息分发完成后同步等待：控制是否同步等待所有消息处理完毕
}

// Option 配置选项函数类型，用于链式配置批处理器参数
type Option func(c *Config)

// WithSize 设置批处理聚合大小的选项函数
// 参数s: 批处理大小，达到此数量的消息时触发批处理
func WithSize(s int) Option {
	return func(c *Config) {
		c.size = s
	}
}

// WithBuffer 设置工作协程通道缓冲区大小的选项函数
// 参数b: 每个工作协程处理通道的缓冲区大小
func WithBuffer(b int) Option {
	return func(c *Config) {
		c.buffer = b
	}
}

// WithWorker 设置并发工作协程数量的选项函数
// 参数w: 并行处理的协程数量，影响处理并发能力
func WithWorker(w int) Option {
	return func(c *Config) {
		c.worker = w
	}
}

// WithInterval 设置定时触发间隔的选项函数
// 参数i: 定时器间隔，即使未达到批处理大小也会定时触发处理
func WithInterval(i time.Duration) Option {
	return func(c *Config) {
		c.interval = i
	}
}

// WithSyncWait 设置是否同步等待的选项函数
// 参数wait: 是否等待所有消息处理完毕后再继续，影响处理流程的同步性
func WithSyncWait(wait bool) Option {
	return func(c *Config) {
		c.syncWait = wait
	}
}

// WithDataBuffer 设置主数据通道大小的选项函数
// 参数size: 主数据通道缓冲区大小，影响数据接收能力
func WithDataBuffer(size int) Option {
	return func(c *Config) {
		c.dataBuffer = size
	}
}

// Batcher 泛型批处理器结构体
// T: 处理的数据类型，支持任意类型的数据批处理
type Batcher[T any] struct {
	config *Config // 批处理器配置

	// 上下文控制和取消
	globalCtx context.Context    // 全局上下文，用于控制整个批处理器的生命周期
	cancel    context.CancelFunc // 取消函数，用于停止批处理器

	// 核心处理函数（必须设置）
	Do       func(ctx context.Context, channelID int, val *Msg[T]) // 批处理执行函数：处理具体的批量数据
	Sharding func(key string) int                                  // 分片函数：根据key决定数据分配到哪个工作协程
	Key      func(data *T) string                                  // 键提取函数：从数据中提取用于分组和分片的key

	// 回调函数（可选）
	OnComplete func(lastMessage *T, totalCount int)                                             // 完成回调：批处理完成后的回调函数
	HookFunc   func(triggerID string, messages map[string][]*T, totalCount int, lastMessage *T) // 钩子函数：批处理触发时的钩子函数

	// 内部通道和同步控制
	data     chan *T        // 主数据接收通道：接收待处理的数据
	chArrays []chan *Msg[T] // 工作协程通道数组：每个工作协程对应一个通道
	wait     sync.WaitGroup // 等待组：用于等待所有工作协程结束
	counter  sync.WaitGroup // 计数器：用于同步等待模式下的消息处理计数
}

// emptyOnComplete 空的完成回调函数，用作默认值
func emptyOnComplete[T any](*T, int) {}

// emptyHookFunc 空的钩子函数，用作默认值
func emptyHookFunc[T any](string, map[string][]*T, int, *T) {
}

// New 创建新的批处理器实例
// T: 数据类型泛型参数
// opts: 可变配置选项，用于自定义批处理器行为
// 返回: 配置好的批处理器实例
func New[T any](opts ...Option) *Batcher[T] {
	// 初始化批处理器，设置默认的空回调函数
	b := &Batcher[T]{
		OnComplete: emptyOnComplete[T], // 设置默认的完成回调
		HookFunc:   emptyHookFunc[T],   // 设置默认的钩子函数
	}

	// 创建默认配置
	config := &Config{
		size:     DefaultSize,     // 默认批处理大小
		buffer:   DefaultBuffer,   // 默认缓冲区大小
		worker:   DefaultWorker,   // 默认工作协程数
		interval: DefaultInterval, // 默认触发间隔
	}

	// 应用配置选项
	for _, opt := range opts {
		opt(config)
	}

	// 设置配置并初始化通道
	b.config = config
	b.data = make(chan *T, DefaultDataChanSize)                      // 创建主数据通道
	b.globalCtx, b.cancel = context.WithCancel(context.Background()) // 创建上下文控制

	// 为每个工作协程创建独立的通道
	b.chArrays = make([]chan *Msg[T], b.config.worker)
	for i := 0; i < b.config.worker; i++ {
		b.chArrays[i] = make(chan *Msg[T], b.config.buffer)
	}
	return b
}

// Worker 获取工作协程数量
// 返回: 当前配置的并发工作协程数量
func (b *Batcher[T]) Worker() int {
	return b.config.worker
}

// Start 启动批处理器
// 开始运行调度器和工作协程，执行以下步骤：
// 1. 验证必需的函数是否已设置
// 2. 启动所有工作协程
// 3. 启动调度器协程
// 返回: 如果必需函数未设置则返回错误，否则返回nil
func (b *Batcher[T]) Start() error {
	// 验证必需的函数是否已设置
	if b.Sharding == nil {
		return errs.New("Sharding function is required").Wrap()
	}
	if b.Do == nil {
		return errs.New("Do function is required").Wrap()
	}
	if b.Key == nil {
		return errs.New("Key function is required").Wrap()
	}

	// 启动所有工作协程
	b.wait.Add(b.config.worker)
	for i := 0; i < b.config.worker; i++ {
		go b.run(i, b.chArrays[i]) // 每个工作协程处理自己的通道
	}

	// 启动调度器协程
	b.wait.Add(1)
	go b.scheduler()
	return nil
}

// Put 向批处理器投入数据
// ctx: 上下文，用于控制投入操作的超时和取消
// data: 待处理的数据指针
// 返回: 如果数据为nil、通道已关闭或上下文取消则返回错误，否则返回nil
func (b *Batcher[T]) Put(ctx context.Context, data *T) error {
	if data == nil {
		return errs.New("data can not be nil").Wrap()
	}

	// 通过select实现非阻塞的数据投入，支持上下文取消
	select {
	case <-b.globalCtx.Done():
		return errs.New("data channel is closed").Wrap() // 批处理器已关闭
	case <-ctx.Done():
		return ctx.Err() // 上下文取消或超时
	case b.data <- data:
		return nil // 成功投入数据
	}
}

// scheduler 调度器函数，负责数据聚合和批处理触发
// 主要职责：
// 1. 监听数据通道，收集待处理数据
// 2. 按key对数据进行分组聚合
// 3. 当达到批处理大小或定时器触发时，分发消息给工作协程
// 4. 处理优雅关闭逻辑
func (b *Batcher[T]) scheduler() {
	ticker := time.NewTicker(b.config.interval) // 创建定时器
	defer func() {
		// 清理资源
		ticker.Stop()                   // 停止定时器
		for _, ch := range b.chArrays { // 关闭所有工作协程通道
			close(ch)
		}
		close(b.data) // 关闭主数据通道
		b.wait.Done() // 通知等待组调度器已结束
	}()

	// 数据聚合状态
	vals := make(map[string][]*T) // 按key分组的数据映射
	count := 0                    // 当前聚合的消息总数
	var lastAny *T                // 最后一条消息，用于回调

	for {
		select {
		case data, ok := <-b.data:
			if !ok {
				// 数据通道意外关闭
				return
			}
			if data == nil {
				// 收到nil数据表示关闭信号
				if count > 0 {
					b.distributeMessage(vals, count, lastAny) // 处理剩余数据
				}
				return
			}

			// 数据聚合逻辑
			key := b.Key(data)                  // 提取数据的key
			vals[key] = append(vals[key], data) // 按key分组存储
			lastAny = data                      // 记录最后一条消息
			count++                             // 增加计数

			// 检查是否达到批处理大小
			if count >= b.config.size {
				b.distributeMessage(vals, count, lastAny) // 分发消息
				vals = make(map[string][]*T)              // 重置聚合状态
				count = 0
			}

		case <-ticker.C:
			// 定时器触发，处理未达到批处理大小的数据
			if count > 0 {
				b.distributeMessage(vals, count, lastAny) // 分发消息
				vals = make(map[string][]*T)              // 重置聚合状态
				count = 0
			}
		}
	}
}

// Msg 批处理消息结构体，包装了一组相同key的数据
// T: 数据类型泛型参数
type Msg[T any] struct {
	key       string // 消息的分组key，用于数据分类
	triggerID string // 触发ID，用于追踪批处理的触发来源
	val       []*T   // 同一key下的批量数据切片
}

// Key 获取消息的分组key
// 返回: 用于数据分组和分片的key
func (m Msg[T]) Key() string {
	return m.key
}

// TriggerID 获取消息的触发ID
// 返回: 用于追踪批处理触发来源的ID
func (m Msg[T]) TriggerID() string {
	return m.triggerID
}

// Val 获取消息的批量数据
// 返回: 同一key下的所有数据指针切片
func (m Msg[T]) Val() []*T {
	return m.val
}

// String 实现Stringer接口，提供消息的字符串表示
// 返回: 包含key和所有数据值的格式化字符串
func (m Msg[T]) String() string {
	var sb strings.Builder
	sb.WriteString("Key: ")
	sb.WriteString(m.key)
	sb.WriteString(", Values: [")
	for i, v := range m.val {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(fmt.Sprintf("%v", *v))
	}
	sb.WriteString("]")
	return sb.String()
}

// distributeMessage 分发聚合好的消息到工作协程
// messages: 按key分组的消息映射
// totalCount: 总消息数量
// lastMessage: 最后一条消息，用于回调
// 执行流程：
// 1. 生成触发ID用于追踪
// 2. 调用钩子函数
// 3. 按分片规则分发消息到不同工作协程
// 4. 根据配置决定是否同步等待
// 5. 调用完成回调
func (b *Batcher[T]) distributeMessage(messages map[string][]*T, totalCount int, lastMessage *T) {
	triggerID := idutil.OperationIDGenerator()               // 生成唯一的触发ID
	b.HookFunc(triggerID, messages, totalCount, lastMessage) // 调用钩子函数

	// 遍历所有分组消息，分发到对应的工作协程
	for key, data := range messages {
		if b.config.syncWait {
			b.counter.Add(1) // 如果启用同步等待，增加计数器
		}
		channelID := b.Sharding(key)                                                // 根据key计算分片ID
		b.chArrays[channelID] <- &Msg[T]{key: key, triggerID: triggerID, val: data} // 发送到对应的工作协程通道
	}

	if b.config.syncWait {
		b.counter.Wait() // 等待所有消息处理完成
	}

	b.OnComplete(lastMessage, totalCount) // 调用完成回调
}

// run 工作协程的运行函数
// channelID: 工作协程的唯一标识ID
// ch: 该工作协程监听的消息通道
// 主要职责：
// 1. 持续监听分配给该协程的消息通道
// 2. 接收到消息后调用Do函数进行处理
// 3. 处理完成后通知计数器（如果启用同步等待）
// 4. 通道关闭时优雅退出
func (b *Batcher[T]) run(channelID int, ch <-chan *Msg[T]) {
	defer b.wait.Done() // 协程结束时通知等待组
	for {
		select {
		case messages, ok := <-ch:
			if !ok {
				// 通道已关闭，退出协程
				return
			}
			// 调用用户定义的处理函数
			b.Do(context.Background(), channelID, messages)
			if b.config.syncWait {
				b.counter.Done() // 如果启用同步等待，通知计数器处理完成
			}
		}
	}
}

// Close 关闭批处理器，执行优雅关闭流程
// 执行步骤：
// 1. 取消全局上下文，停止接收新数据
// 2. 向数据通道发送nil信号，通知调度器关闭
// 3. 等待所有协程（调度器和工作协程）退出
// 注意：调用此方法后批处理器将无法再使用
func (b *Batcher[T]) Close() {
	b.cancel()    // 发送取消信号，停止接收新数据
	b.data <- nil // 向调度器发送关闭信号
	b.wait.Wait() // 等待所有协程退出
}
