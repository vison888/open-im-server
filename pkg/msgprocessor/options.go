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

// Package msgprocessor 消息处理器包
// options.go 定义了消息处理选项系统，这是OpenIM消息系统的核心配置机制
// 通过灵活的选项配置，可以精确控制消息的行为：存储、推送、同步、会话更新等
//
// 核心设计理念：
// 1. 选项化配置：每个消息行为特性都可以独立开关
// 2. 业务场景适配：不同类型的消息需要不同的处理策略
// 3. 性能优化：精确控制消息处理流程，避免不必要的操作
// 4. 多端同步：细粒度控制消息在不同设备间的同步行为
package msgprocessor

import "github.com/openimsdk/protocol/constant"

// Options 消息选项类型，使用map存储各种开关配置
// 键为选项名称（字符串常量），值为布尔开关
// 这种设计提供了最大的灵活性，可以动态组合各种选项
type Options map[string]bool

// OptionsOpt 选项设置函数类型
// 采用函数式编程模式，允许链式调用设置多个选项
// 例：WithHistory(true), WithOfflinePush(false)
type OptionsOpt func(Options)

// NewOptions 创建带默认值的消息选项配置
//
// 默认配置策略：
// - 大部分选项默认为false，采用保守策略，需要明确开启
// - IsSenderSync默认为true，确保发送者多设备同步
//
// 使用场景：
// 1. 普通聊天消息：NewOptions(WithHistory(true), WithPersistent(true), WithOfflinePush(true))
// 2. 系统通知：NewOptions(WithHistory(false), WithOfflinePush(false))
// 3. 临时消息：NewOptions() // 使用默认配置，不存储不推送
//
// 参数：
//   - opts: 可变参数，允许传入多个选项设置函数
//
// 返回：
//   - Options: 配置好的选项映射
func NewOptions(opts ...OptionsOpt) Options {
	// 初始化默认选项配置
	options := make(map[string]bool, 11)

	// ========== 消息分类和路由选项 ==========

	// IsNotNotification: 标识是否为非通知消息（即普通聊天消息）
	// false = 通知消息，会走通知消息处理流程，会话ID前缀为"n_"
	// true  = 普通消息，会走普通消息处理流程，会话ID前缀为"si_/sg_/g_"
	// 设置时机：消息创建时根据消息类型自动设置
	// 效果：影响会话ID生成、消息存储策略、推送策略
	options[constant.IsNotNotification] = false

	// ========== 消息发送控制选项 ==========

	// IsSendMsg: 通知消息是否需要作为普通消息发送
	// 解决问题：通知消息既要通知又要聊天的场景
	// false = 仅发送通知，不在聊天界面显示
	// true  = 通知 + 聊天消息双重发送，在聊天界面也会显示
	// 设置时机：系统通知配置中设置，如群创建通知、成员变更通知
	// 效果：影响消息是否会被克隆为普通消息发送
	// 典型场景：群成员进入通知，既要通知用户，也要在群聊中显示
	options[constant.IsSendMsg] = false

	// ========== 消息存储和历史记录选项 ==========

	// IsHistory: 是否存储为历史消息记录
	// 核心职责：控制消息是否进入历史记录系统，用于消息检索和会话记录
	//
	// 详细说明：
	// false = 不存储历史记录，消息不会写入MongoDB的msg集合
	//         适用场景：
	//         - 正在输入状态消息 (typing)
	//         - 临时状态通知
	//         - 实时语音/视频状态消息
	//         - 一次性系统通知
	// true  = 存储历史记录，消息会写入MongoDB的msg集合，支持历史查询
	//         适用场景：
	//         - 普通聊天消息（文本/图片/语音等）
	//         - 重要系统通知（需要用户后续查看）
	//         - 群公告、入群通知等
	//
	// 技术影响：
	// - 决定消息是否调用 BatchInsertChat2Cache 写入Redis缓存（24小时TTL）
	// - 决定消息是否发送到ToMongoTopic队列进行持久化
	// - 影响消息的可搜索性和历史同步功能
	// - 在categorizeMessageLists()方法中用于消息分类
	options[constant.IsHistory] = false

	// IsPersistent: 是否持久化存储（专门控制存储层面）
	// 核心职责：控制消息的存储级别和数据保留策略
	//
	// 与IsHistory的核心区别：
	// ┌──────────────────┬────────────────────┬────────────────────┐
	// │   选项组合        │      IsHistory     │   IsPersistent     │
	// ├──────────────────┼────────────────────┼────────────────────┤
	// │ 普通聊天消息      │ true (缓存+持久化)  │ true (永久存储)     │
	// │ 系统重要通知      │ true (缓存+持久化)  │ true (永久存储)     │
	// │ 临时状态消息      │ false (仅推送)     │ false (不存储)     │
	// │ 正在输入状态      │ false (仅推送)     │ false (不存储)     │
	// │ 调试/测试消息     │ true (缓存查看)    │ false (临时存储)   │
	// │ 审计日志消息      │ true (缓存查看)    │ true (合规要求)    │
	// └──────────────────┴────────────────────┴────────────────────┘
	//
	// 详细说明：
	// false = 不持久化存储，消息可能被清理或有TTL限制
	//         适用场景：
	//         - 临时调试消息
	//         - 短期状态消息
	//         - 仅需要短期缓存的消息
	//         - 开发测试环境的临时消息
	// true  = 持久化存储，消息永久保留，不受TTL限制
	//         适用场景：
	//         - 重要聊天消息
	//         - 合规审计要求的消息
	//         - 商业价值的消息记录
	//         - 法律证据类消息
	//
	// 技术影响：
	// - 影响MongoDB存储策略和索引设计
	// - 影响数据备份和归档策略
	// - 影响Redis缓存的TTL设置
	// - 影响消息的存储成本和性能优化
	//
	// 实际应用示例：
	// 1. 普通聊天：WithHistory(true) + WithPersistent() = 完整存储
	// 2. 调试消息：WithHistory(true) + 不调用WithPersistent() = 仅临时缓存
	// 3. 状态消息：WithHistory(false) + 不调用WithPersistent() = 仅推送
	options[constant.IsPersistent] = false

	// ========== 推送和通知选项 ==========

	// IsOfflinePush: 是否支持离线推送
	// 解决问题：用户离线时的消息推送控制
	// false = 不推送，用户上线后通过同步获取
	// true  = 支持推送，用户离线时通过推送服务通知
	// 设置时机：
	//   - 重要聊天消息：true
	//   - 系统维护通知：false
	//   - 仅在线消息：false
	// 效果：影响消息是否进入离线推送队列
	options[constant.IsOfflinePush] = false

	// IsUnreadCount: 是否计入未读消息数统计
	// 解决问题：避免系统消息污染用户的未读数
	// false = 不计入未读数，不影响角标显示
	// true  = 计入未读数，会增加会话角标
	// 设置时机：
	//   - 普通聊天消息：true
	//   - 系统通知消息：false
	//   - 群管理操作：false
	// 效果：影响客户端未读数显示和会话列表排序
	options[constant.IsUnreadCount] = false

	// ========== 会话更新控制选项 ==========

	// IsConversationUpdate: 是否更新会话信息
	// 解决问题：历史消息同步时避免覆盖会话最新状态
	// false = 不更新会话，通常用于历史消息同步
	// true  = 更新会话最新消息、时间戳等
	// 设置时机：
	//   - 新实时消息：true
	//   - 历史同步消息：false
	//   - 离线消息同步：false
	// 效果：影响会话列表的排序和最新消息显示
	options[constant.IsConversationUpdate] = false

	// ========== 多设备同步选项 ==========

	// IsSenderSync: 是否同步给发送者的其他设备
	// 解决问题：多设备消息同步的一致性
	// false = 不同步给发送者其他设备，节省带宽
	// true  = 同步给发送者其他设备，保证多端一致性
	// 设置时机：
	//   - 普通消息：true（默认）
	//   - 用户状态变更：false（避免循环通知）
	//   - 临时消息：false
	// 效果：影响发送者在其他设备上是否能看到自己发送的消息
	options[constant.IsSenderSync] = true // 注意：这是唯一默认为true的选项

	// ========== 隐私和安全选项 ==========

	// IsNotPrivate: 是否为非私密消息
	// 解决问题：区分普通消息和私密消息（如阅后即焚）
	// false = 私密消息，有特殊的显示和处理逻辑
	// true  = 普通消息，正常显示和处理
	// 设置时机：根据会话的隐私设置自动判断
	// 效果：影响消息的显示样式和生命周期管理
	options[constant.IsNotPrivate] = false

	// IsSenderConversationUpdate: 是否更新发送者的会话信息
	// 解决问题：发送者和接收者会话更新逻辑可能不同
	// false = 不更新发送者会话，避免重复更新
	// true  = 更新发送者会话，保证发送者看到最新状态
	// 设置时机：特定业务场景需要差异化处理发送者会话时
	// 效果：影响发送者设备上的会话状态更新
	options[constant.IsSenderConversationUpdate] = false

	// ========== 性能优化选项 ==========

	// IsReactionFromCache: 表情回应是否从缓存获取
	// 解决问题：表情回应的性能优化
	// false = 从数据库获取，保证数据准确性
	// true  = 从缓存获取，提高响应速度
	// 设置时机：表情回应相关消息处理时设置
	// 效果：影响表情回应数据的获取策略
	options[constant.IsReactionFromCache] = false

	// 应用用户传入的选项设置函数
	for _, opt := range opts {
		opt(options)
	}

	return options
}

// NewMsgOptions 创建普通消息的默认选项配置
// 这是一个简化版本，主要用于普通聊天消息
//
// 注意：当前实现有问题，返回了空map而不是包含默认配置的map
// 建议修复为返回完整的默认配置
//
// 使用场景：
// - 快速创建普通聊天消息的选项配置
// - 不需要复杂配置的简单消息场景
//
// 返回：
//   - Options: 简化的选项配置
func NewMsgOptions() Options {
	options := make(map[string]bool, 11)
	options[constant.IsOfflinePush] = false
	// TODO: 这里应该返回options而不是空map
	return make(map[string]bool)
}

// WithOptions 在现有选项基础上应用新的配置
// 支持动态修改选项配置，提供灵活的配置更新机制
//
// 使用场景：
// - 基于基础配置进行个性化调整
// - 运行时动态修改消息选项
//
// 参数：
//   - options: 现有的选项配置
//   - opts: 要应用的选项修改函数列表
//
// 返回：
//   - Options: 更新后的选项配置
func WithOptions(options Options, opts ...OptionsOpt) Options {
	for _, opt := range opts {
		opt(options)
	}
	return options
}

// ========== 选项设置函数 ==========
// 以下函数采用函数式编程模式，支持链式调用

// WithNotNotification 设置是否为非通知消息
//
// 业务场景：
// - 普通聊天消息：WithNotNotification(true)
// - 系统通知消息：WithNotNotification(false)
//
// 参数：
//   - b: true=普通消息，false=通知消息
//
// 返回：
//   - OptionsOpt: 选项设置函数
func WithNotNotification(b bool) OptionsOpt {
	return func(options Options) {
		options[constant.IsNotNotification] = b
	}
}

// WithSendMsg 设置通知消息是否同时作为聊天消息发送
//
// 业务场景：
// - 群成员变更：WithSendMsg(true) - 既通知又在群里显示
// - 系统维护通知：WithSendMsg(false) - 仅通知不显示在聊天中
//
// 参数：
//   - b: true=同时发送聊天消息，false=仅发送通知
//
// 返回：
//   - OptionsOpt: 选项设置函数
func WithSendMsg(b bool) OptionsOpt {
	return func(options Options) {
		options[constant.IsSendMsg] = b
	}
}

// WithHistory 设置是否存储为历史消息
//
// 业务场景：
// - 重要聊天消息：WithHistory(true)
// - 正在输入状态：WithHistory(false)
// - 临时通知：WithHistory(false)
//
// 参数：
//   - b: true=存储历史，false=不存储历史
//
// 返回：
//   - OptionsOpt: 选项设置函数
func WithHistory(b bool) OptionsOpt {
	return func(options Options) {
		options[constant.IsHistory] = b
	}
}

// WithPersistent 设置消息持久化存储
// 注意：这个函数固定设置为true，不接受参数
//
// 业务场景：
// - 重要消息必须持久化存储时调用
// - 需要保证消息不丢失的场景
//
// 返回：
//   - OptionsOpt: 选项设置函数
func WithPersistent() OptionsOpt {
	return func(options Options) {
		options[constant.IsPersistent] = true
	}
}

// WithOfflinePush 设置是否支持离线推送
//
// 业务场景：
// - 重要聊天消息：WithOfflinePush(true)
// - 系统状态更新：WithOfflinePush(false)
// - 仅在线消息：WithOfflinePush(false)
//
// 参数：
//   - b: true=支持离线推送，false=不推送
//
// 返回：
//   - OptionsOpt: 选项设置函数
func WithOfflinePush(b bool) OptionsOpt {
	return func(options Options) {
		options[constant.IsOfflinePush] = b
	}
}

// WithUnreadCount 设置是否计入未读消息数
//
// 业务场景：
// - 普通聊天消息：WithUnreadCount(true)
// - 系统通知：WithUnreadCount(false)
// - 群管理操作：WithUnreadCount(false)
//
// 参数：
//   - b: true=计入未读数，false=不计入未读数
//
// 返回：
//   - OptionsOpt: 选项设置函数
func WithUnreadCount(b bool) OptionsOpt {
	return func(options Options) {
		options[constant.IsUnreadCount] = b
	}
}

// WithConversationUpdate 设置是否更新会话信息
// 注意：这个函数固定设置为true，不接受参数
//
// 业务场景：
// - 新实时消息需要更新会话最新状态时调用
// - 需要更新会话时间戳和最新消息时
//
// 返回：
//   - OptionsOpt: 选项设置函数
func WithConversationUpdate() OptionsOpt {
	return func(options Options) {
		options[constant.IsConversationUpdate] = true
	}
}

// WithSenderSync 设置是否同步给发送者其他设备
// 注意：这个函数固定设置为true，不接受参数
//
// 业务场景：
// - 需要保证多设备同步一致性时调用
// - 重要消息需要在发送者所有设备上显示时
//
// 返回：
//   - OptionsOpt: 选项设置函数
func WithSenderSync() OptionsOpt {
	return func(options Options) {
		options[constant.IsSenderSync] = true
	}
}

// WithNotPrivate 设置为非私密消息
// 注意：这个函数固定设置为true，不接受参数
//
// 业务场景：
// - 确保消息按普通模式处理，不应用私密消息逻辑
// - 退出阅后即焚模式时使用
//
// 返回：
//   - OptionsOpt: 选项设置函数
func WithNotPrivate() OptionsOpt {
	return func(options Options) {
		options[constant.IsNotPrivate] = true
	}
}

// WithSenderConversationUpdate 设置是否更新发送者会话
// 注意：这个函数固定设置为true，不接受参数
//
// 业务场景：
// - 发送者需要看到会话状态更新时调用
// - 确保发送者会话信息与接收者一致时
//
// 返回：
//   - OptionsOpt: 选项设置函数
func WithSenderConversationUpdate() OptionsOpt {
	return func(options Options) {
		options[constant.IsSenderConversationUpdate] = true
	}
}

// WithReactionFromCache 设置表情回应从缓存获取
// 注意：这个函数固定设置为true，不接受参数
//
// 业务场景：
// - 表情回应功能需要快速响应时调用
// - 优化表情回应性能时使用
//
// 返回：
//   - OptionsOpt: 选项设置函数
func WithReactionFromCache() OptionsOpt {
	return func(options Options) {
		options[constant.IsReactionFromCache] = true
	}
}

// ========== 选项查询方法 ==========
// 以下方法用于查询选项配置状态

// Is 通用选项状态查询方法
//
// 查询逻辑：
// - 如果选项不存在或值为true，返回true
// - 如果选项存在且值为false，返回false
// 这种设计使得默认行为偏向于"启用"
//
// 参数：
//   - notification: 选项名称
//
// 返回：
//   - bool: 选项状态
func (o Options) Is(notification string) bool {
	v, ok := o[notification]
	if !ok || v {
		return true
	}
	return false
}

// ========== 具体选项查询方法 ==========
// 为每个选项提供专门的查询方法，提高代码可读性

// IsNotNotification 查询是否为非通知消息
// 返回true表示这是普通聊天消息，false表示这是系统通知消息
func (o Options) IsNotNotification() bool {
	return o.Is(constant.IsNotNotification)
}

// IsSendMsg 查询通知消息是否需要同时作为聊天消息发送
// 返回true表示需要双重发送，false表示仅发送通知
func (o Options) IsSendMsg() bool {
	return o.Is(constant.IsSendMsg)
}

// IsHistory 查询是否存储为历史消息
// 返回true表示需要存储，false表示临时消息
func (o Options) IsHistory() bool {
	return o.Is(constant.IsHistory)
}

// IsPersistent 查询是否持久化存储
// 返回true表示需要持久化，false表示临时存储
func (o Options) IsPersistent() bool {
	return o.Is(constant.IsPersistent)
}

// IsOfflinePush 查询是否支持离线推送
// 返回true表示支持推送，false表示不推送
func (o Options) IsOfflinePush() bool {
	return o.Is(constant.IsOfflinePush)
}

// IsUnreadCount 查询是否计入未读消息数
// 返回true表示计入未读数，false表示不影响未读数
func (o Options) IsUnreadCount() bool {
	return o.Is(constant.IsUnreadCount)
}

// IsConversationUpdate 查询是否更新会话信息
// 返回true表示需要更新会话，false表示不更新会话
func (o Options) IsConversationUpdate() bool {
	return o.Is(constant.IsConversationUpdate)
}

// IsSenderSync 查询是否同步给发送者其他设备
// 返回true表示需要同步，false表示不同步
func (o Options) IsSenderSync() bool {
	return o.Is(constant.IsSenderSync)
}

// IsNotPrivate 查询是否为非私密消息
// 返回true表示普通消息，false表示私密消息
func (o Options) IsNotPrivate() bool {
	return o.Is(constant.IsNotPrivate)
}

// IsSenderConversationUpdate 查询是否更新发送者会话
// 返回true表示需要更新发送者会话，false表示不更新
func (o Options) IsSenderConversationUpdate() bool {
	return o.Is(constant.IsSenderConversationUpdate)
}

// IsReactionFromCache 查询表情回应是否从缓存获取
// 返回true表示从缓存获取，false表示从数据库获取
func (o Options) IsReactionFromCache() bool {
	return o.Is(constant.IsReactionFromCache)
}
