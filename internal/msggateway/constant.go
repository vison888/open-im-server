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

// constant.go - 常量定义模块
//
// 功能概述:
// 1. 定义WebSocket连接建立时的参数名常量
// 2. 定义SDK类型常量，区分不同的客户端类型
// 3. 定义WebSocket协议相关的请求标识符
// 4. 定义连接超时和限制相关的常量
//
// 设计思路:
// - 集中管理: 将所有常量集中定义，便于维护和修改
// - 语义化命名: 使用有意义的常量名，提高代码可读性
// - 分类组织: 按功能分组定义常量，便于查找和理解
// - 兼容性: 保持常量值的稳定性，确保客户端兼容
//
// 使用场景:
// - WebSocket握手参数解析
// - 客户端类型判断和处理
// - 消息类型路由和分发
// - 连接超时和限制控制

package msggateway

import "time"

// WebSocket连接建立时的URL参数名常量
// 这些参数在WebSocket握手时通过URL查询字符串传递
// 用于身份验证、连接标识和功能配置
const (
	// WsUserID 用户ID参数名（兼容旧版本）
	// 在URL中传递用户的唯一标识符
	// 示例: ws://host/ws?sendID=user123
	WsUserID = "sendID"

	// CommonUserID 通用用户ID参数名
	// 标准化的用户ID参数名，用于新版本
	CommonUserID = "userID"

	// PlatformID 平台ID参数名
	// 标识客户端运行的平台类型
	// 值范围: 1=iOS, 2=Android, 3=Windows, 4=OSX, 5=Web, 6=Linux等
	PlatformID = "platformID"

	// ConnID 连接ID参数名
	// 用于标识具体的连接实例，便于调试和监控
	ConnID = "connID"

	// Token 认证令牌参数名
	// 用户身份认证的JWT令牌
	Token = "token"

	// OperationID 操作ID参数名
	// 用于追踪和关联相关的操作，便于日志分析
	OperationID = "operationID"

	// Compression 压缩协议参数名
	// 指定是否启用数据压缩以及压缩类型
	Compression = "compression"

	// GzipCompressionProtocol Gzip压缩协议标识
	// 当compression参数值为此常量时，启用gzip压缩
	GzipCompressionProtocol = "gzip"

	// BackgroundStatus 后台状态参数名
	// 标识客户端是否处于后台模式（主要用于移动端）
	BackgroundStatus = "isBackground"

	// SendResponse 响应发送控制参数名
	// 控制是否需要发送响应消息
	SendResponse = "isMsgResp"

	// SDKType SDK类型参数名
	// 标识客户端使用的SDK类型，影响编码方式
	SDKType = "sdkType"
)

// SDK类型常量定义
// 用于区分不同的客户端SDK，选择对应的编码器
const (
	// GoSDK Go语言SDK标识
	// Go客户端使用gob编码，性能更优
	GoSDK = "go"

	// JsSDK JavaScript SDK标识
	// Web和Node.js客户端使用JSON编码，兼容性更好
	JsSDK = "js"
)

// 连接类型常量定义
// 当前仅支持WebSocket协议，预留扩展其他协议的可能
const (
	// WebSocket WebSocket协议标识
	// 目前消息网关主要基于WebSocket协议实现
	WebSocket = iota + 1
)

// WebSocket协议请求标识符常量定义
// 这些常量用于区分不同类型的客户端请求和服务器推送
// 分为三个范围：
// 1xxx: 客户端请求类型
// 2xxx: 服务器推送类型
// 3xxx: 错误和异常类型
const (
	// === 客户端请求类型 (1xxx) ===

	// WSGetNewestSeq 获取最新序列号请求
	// 客户端获取当前会话的最新消息序列号
	WSGetNewestSeq = 1001

	// WSPullMsgBySeqList 按序列号列表拉取消息请求
	// 客户端根据指定的序列号列表批量拉取消息
	WSPullMsgBySeqList = 1002

	// WSSendMsg 发送消息请求
	// 客户端发送普通聊天消息
	WSSendMsg = 1003

	// WSSendSignalMsg 发送信令消息请求
	// 客户端发送信令类消息（如通话邀请、系统通知等）
	WSSendSignalMsg = 1004

	// WSPullMsg 拉取消息请求
	// 客户端主动拉取消息，通常用于离线消息同步
	WSPullMsg = 1005

	// WSGetConvMaxReadSeq 获取会话最大已读序列号请求
	// 客户端获取会话中已读消息的最大序列号
	WSGetConvMaxReadSeq = 1006

	// WsPullConvLastMessage 拉取会话最新消息请求
	// 客户端获取指定会话的最新一条消息
	WsPullConvLastMessage = 1007

	// === 服务器推送类型 (2xxx) ===

	// WSPushMsg 服务器推送消息
	// 服务器主动向客户端推送新消息
	WSPushMsg = 2001

	// WSKickOnlineMsg 踢下线消息
	// 服务器通知客户端被踢下线（多端登录策略）
	WSKickOnlineMsg = 2002

	// WsLogoutMsg 登出消息
	// 客户端主动登出请求
	WsLogoutMsg = 2003

	// WsSetBackgroundStatus 设置后台状态
	// 客户端设置应用后台/前台状态
	WsSetBackgroundStatus = 2004

	// WsSubUserOnlineStatus 订阅用户在线状态
	// 客户端订阅指定用户的在线状态变更通知
	WsSubUserOnlineStatus = 2005

	// === 错误类型 (3xxx) ===

	// WSDataError 数据错误
	// WebSocket数据处理错误的通用标识
	WSDataError = 3001
)

// 连接超时和限制常量定义
// 这些常量控制WebSocket连接的各种超时行为和限制
// 基于生产环境的最佳实践经验设定
const (
	// writeWait 写入消息的最大等待时间
	// 设置为10秒，足够大多数网络环境下的消息发送
	// 超过此时间未完成写入操作将返回超时错误
	writeWait = 10 * time.Second

	// pongWait 等待Pong响应的最大时间
	// 设置为30秒，用于检测客户端连接是否仍然活跃
	// 如果在此时间内未收到Pong响应，将认为连接已断开
	pongWait = 30 * time.Second

	// pingPeriod 发送Ping消息的周期
	// 设置为pongWait的90%，确保在超时前有足够的时间检测连接
	// 只有Web平台会主动发送Ping消息
	pingPeriod = (pongWait * 9) / 10

	// maxMessageSize 单个消息的最大字节数限制
	// 设置为50KB，防止超大消息导致的内存耗尽攻击
	// 超过此大小的消息将被拒绝处理
	maxMessageSize = 51200
)
