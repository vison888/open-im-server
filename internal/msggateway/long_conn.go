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

package msggateway

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/openimsdk/tools/apiresp"

	"github.com/gorilla/websocket"
	"github.com/openimsdk/tools/errs"
)

// LongConn 长连接接口抽象
// 设计思路：
// 1. 提供统一的长连接接口，支持不同的底层实现（WebSocket、TCP等）
// 2. 封装连接的基本操作：读写、超时、心跳等
// 3. 支持连接升级和错误处理
// 4. 为上层业务提供简单易用的连接抽象
type LongConn interface {
	// Close 关闭连接
	// 优雅关闭当前连接，释放相关资源
	Close() error

	// WriteMessage 向连接写入消息
	// messageType: 消息类型，支持二进制(2)和文本(1)
	// message: 要发送的消息内容
	WriteMessage(messageType int, message []byte) error

	// ReadMessage 从连接读取消息
	// 返回值: 消息类型、消息内容、错误信息
	ReadMessage() (int, []byte, error)

	// SetReadDeadline 设置读取超时时间
	// timeout: 超时时长，超时后读取操作将返回错误
	SetReadDeadline(timeout time.Duration) error

	// SetWriteDeadline 设置写入超时时间
	// timeout: 超时时长，超时后写入操作将返回错误
	SetWriteDeadline(timeout time.Duration) error

	// Dial 建立客户端连接
	// urlStr: 连接地址，必须包含认证参数
	// requestHeader: 请求头，可用于控制数据压缩等
	Dial(urlStr string, requestHeader http.Header) (*http.Response, error)

	// IsNil 检查连接是否为空
	// 用于判断连接是否已经建立
	IsNil() bool

	// SetConnNil 将连接设置为空
	// 用于连接断开后的清理操作
	SetConnNil()

	// SetReadLimit 设置消息读取的最大字节数
	// limit: 最大字节数限制，防止超大消息攻击
	SetReadLimit(limit int64)

	// SetPongHandler 设置 Pong 消息处理器
	// handler: Pong 消息的处理函数
	SetPongHandler(handler PingPongHandler)

	// SetPingHandler 设置 Ping 消息处理器
	// handler: Ping 消息的处理函数
	SetPingHandler(handler PingPongHandler)

	// GenerateLongConn 升级 HTTP 连接为长连接
	// 从 HTTP 请求升级为 WebSocket 连接
	GenerateLongConn(w http.ResponseWriter, r *http.Request) error
}

// GWebSocket WebSocket 连接的具体实现
// 基于 gorilla/websocket 库实现的 WebSocket 长连接
type GWebSocket struct {
	protocolType     int             // 协议类型
	conn             *websocket.Conn // 底层 WebSocket 连接
	handshakeTimeout time.Duration   // 握手超时时间
	writeBufferSize  int             // 写缓冲区大小
}

// newGWebSocket 创建新的 WebSocket 连接实例
// 参数：
//   - protocolType: 协议类型标识
//   - handshakeTimeout: 握手超时时间
//   - wbs: 写缓冲区大小
func newGWebSocket(protocolType int, handshakeTimeout time.Duration, wbs int) *GWebSocket {
	return &GWebSocket{protocolType: protocolType, handshakeTimeout: handshakeTimeout, writeBufferSize: wbs}
}

// Close 关闭 WebSocket 连接
func (d *GWebSocket) Close() error {
	return d.conn.Close()
}

// GenerateLongConn 将 HTTP 请求升级为 WebSocket 连接
// 设计思路：
// 1. 配置 WebSocket 升级器，设置握手超时和跨域检查
// 2. 可选配置写缓冲区大小，默认为 4KB
// 3. 执行协议升级，建立 WebSocket 连接
// 4. 错误处理：升级失败时返回详细错误信息
func (d *GWebSocket) GenerateLongConn(w http.ResponseWriter, r *http.Request) error {
	// 配置 WebSocket 升级器
	upgrader := &websocket.Upgrader{
		HandshakeTimeout: d.handshakeTimeout,
		CheckOrigin:      func(r *http.Request) bool { return true }, // 允许所有跨域请求
	}

	// 设置写缓冲区大小（如果指定）
	if d.writeBufferSize > 0 { // 默认是 4KB
		upgrader.WriteBufferSize = d.writeBufferSize
	}

	// 执行 HTTP 到 WebSocket 的协议升级
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		// upgrader.Upgrade 方法通常返回足够的错误信息来诊断升级过程中可能出现的问题
		return errs.WrapMsg(err, "GenerateLongConn: WebSocket upgrade failed")
	}
	d.conn = conn
	return nil
}

// WriteMessage 向 WebSocket 连接写入消息
func (d *GWebSocket) WriteMessage(messageType int, message []byte) error {
	// d.setSendConn(d.conn)  // 预留的连接设置方法
	return d.conn.WriteMessage(messageType, message)
}

// setSendConn 设置发送连接（预留方法）
// func (d *GWebSocket) setSendConn(sendConn *websocket.Conn) {
//	d.sendConn = sendConn
// }

// ReadMessage 从 WebSocket 连接读取消息
func (d *GWebSocket) ReadMessage() (int, []byte, error) {
	return d.conn.ReadMessage()
}

// SetReadDeadline 设置读取超时时间
func (d *GWebSocket) SetReadDeadline(timeout time.Duration) error {
	return d.conn.SetReadDeadline(time.Now().Add(timeout))
}

// SetWriteDeadline 设置写入超时时间
// 设计思路：
// 1. 验证超时时间必须大于0
// 2. 设置绝对超时时间（当前时间 + 超时时长）
// 3. 错误处理和包装
func (d *GWebSocket) SetWriteDeadline(timeout time.Duration) error {
	if timeout <= 0 {
		return errs.New("timeout must be greater than 0")
	}

	// TODO SetWriteDeadline 未来添加错误处理
	if err := d.conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return errs.WrapMsg(err, "GWebSocket.SetWriteDeadline failed")
	}
	return nil
}

// Dial 建立客户端 WebSocket 连接
// 用于客户端主动连接到服务器
func (d *GWebSocket) Dial(urlStr string, requestHeader http.Header) (*http.Response, error) {
	conn, httpResp, err := websocket.DefaultDialer.Dial(urlStr, requestHeader)
	if err != nil {
		return httpResp, errs.WrapMsg(err, "GWebSocket.Dial failed", "url", urlStr)
	}
	d.conn = conn
	return httpResp, nil
}

// IsNil 检查连接是否为空
func (d *GWebSocket) IsNil() bool {
	return d.conn == nil
	//
	// if d.conn != nil {
	// 	return false
	// }
	// return true
}

// SetConnNil 将连接设置为空
// 用于连接断开后的清理操作
func (d *GWebSocket) SetConnNil() {
	d.conn = nil
}

// SetReadLimit 设置消息读取的最大字节数
// 防止超大消息导致的内存耗尽攻击
func (d *GWebSocket) SetReadLimit(limit int64) {
	d.conn.SetReadLimit(limit)
}

// SetPongHandler 设置 Pong 消息处理器
// 用于处理客户端发送的 Pong 响应
func (d *GWebSocket) SetPongHandler(handler PingPongHandler) {
	d.conn.SetPongHandler(handler)
}

// SetPingHandler 设置 Ping 消息处理器
// 用于处理客户端发送的 Ping 请求
func (d *GWebSocket) SetPingHandler(handler PingPongHandler) {
	d.conn.SetPingHandler(handler)
}

// RespondWithError 响应错误信息
// 设计思路：
// 1. 升级连接为 WebSocket
// 2. 将错误信息序列化为 JSON
// 3. 发送错误响应
// 4. 关闭连接
func (d *GWebSocket) RespondWithError(err error, w http.ResponseWriter, r *http.Request) error {
	if err := d.GenerateLongConn(w, r); err != nil {
		return err
	}

	// 将错误转换为标准响应格式
	data, err := json.Marshal(apiresp.ParseError(err))
	if err != nil {
		_ = d.Close()
		return errs.WrapMsg(err, "json marshal failed")
	}

	// 发送错误消息
	if err := d.WriteMessage(MessageText, data); err != nil {
		_ = d.Close()
		return errs.WrapMsg(err, "WriteMessage failed")
	}
	_ = d.Close()
	return nil
}

// RespondWithSuccess 响应成功信息
// 发送成功响应，通常用于连接建立确认
func (d *GWebSocket) RespondWithSuccess() error {
	// 创建成功响应（错误为 nil）
	data, err := json.Marshal(apiresp.ParseError(nil))
	if err != nil {
		_ = d.Close()
		return errs.WrapMsg(err, "json marshal failed")
	}

	// 发送成功消息
	if err := d.WriteMessage(MessageText, data); err != nil {
		_ = d.Close()
		return errs.WrapMsg(err, "WriteMessage failed")
	}
	return nil
}
