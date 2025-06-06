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

// client.go - 客户端连接管理模块
//
// 功能概述:
// 1. 管理单个WebSocket客户端连接的完整生命周期
// 2. 处理客户端消息的接收、解析、路由和响应
// 3. 实现心跳机制，维护连接活性
// 4. 支持多种消息协议和编码格式
// 5. 提供连接状态管理和异常处理
//
// 设计思路:
// - 单连接单协程: 每个客户端连接由独立协程处理，避免相互影响
// - 状态管理: 原子操作保证连接状态的线程安全
// - 心跳机制: 自动心跳检测，及时发现断线
// - 错误隔离: 单个连接错误不影响其他连接
// - 对象池优化: 复用对象，减少GC压力
//
// 核心组件:
// - Client: 客户端连接的核心结构体
// - Message Handler: 消息处理器，支持多种业务请求
// - Heartbeat: 心跳机制，保证连接活性
// - Error Handling: 完善的错误处理和恢复机制

package msggateway

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/openimsdk/open-im-server/v3/pkg/msgprocessor"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/apiresp"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
	"github.com/openimsdk/tools/utils/stringutil"
)

// 预定义错误类型，用于连接状态管理和错误处理
var (
	ErrConnClosed                = errs.New("conn has closed")                      // 连接已关闭
	ErrNotSupportMessageProtocol = errs.New("not support message protocol")         // 不支持的消息协议
	ErrClientClosed              = errs.New("client actively close the connection") // 客户端主动关闭连接
	ErrPanic                     = errs.New("panic error")                          // 恐慌错误
)

// WebSocket 消息类型常量定义
// 基于 WebSocket 协议规范定义的消息类型
const (
	// MessageText 文本消息类型，用于 UTF-8 编码的文本消息（如 JSON）
	MessageText = iota + 1
	// MessageBinary 二进制消息类型，用于二进制消息（如 protobuf）
	MessageBinary
	// CloseMessage 关闭控制消息，可选的消息载荷包含数字代码和文本
	CloseMessage = 8

	// PingMessage Ping 控制消息，可选的消息载荷是 UTF-8 编码的文本
	PingMessage = 9

	// PongMessage Pong 控制消息，可选的消息载荷是 UTF-8 编码的文本
	PongMessage = 10
)

// PingPongHandler Ping/Pong 消息处理器类型定义
type PingPongHandler func(string) error

// Client 客户端连接结构体
// 设计思路：
// 1. 封装单个WebSocket连接的所有状态和操作
// 2. 支持多平台并发连接（iOS、Android、Web、PC等）
// 3. 内置心跳机制，自动维护连接活性
// 4. 支持数据压缩和多种编码格式
// 5. 提供订阅功能，支持用户状态变更通知
type Client struct {
	w              *sync.Mutex         // 写操作互斥锁，保证并发写入安全
	conn           LongConn            // 底层长连接接口
	PlatformID     int                 `json:"platformID"`   // 平台ID（iOS=1, Android=2, Windows=3, etc.）
	IsCompress     bool                `json:"isCompress"`   // 是否启用数据压缩
	UserID         string              `json:"userID"`       // 用户唯一标识
	IsBackground   bool                `json:"isBackground"` // 是否为后台模式（主要针对移动端）
	SDKType        string              `json:"sdkType"`      // SDK类型（Go/JS）
	Encoder        Encoder             // 消息编码器（JSON/Gob）
	ctx            *UserConnContext    // 用户连接上下文
	longConnServer LongConnServer      // 长连接服务器引用
	closed         atomic.Bool         // 连接关闭状态（原子操作）
	closedErr      error               // 连接关闭错误信息
	token          string              // 用户认证令牌
	hbCtx          context.Context     // 心跳上下文
	hbCancel       context.CancelFunc  // 心跳取消函数
	subLock        *sync.Mutex         // 订阅操作互斥锁
	subUserIDs     map[string]struct{} // 客户端订阅的用户ID列表
}

// ResetClient 重置客户端状态，用于连接复用和对象池优化
// 设计思路：
// 1. 复用已有的Client对象，避免频繁的内存分配
// 2. 重置所有状态，确保新连接的干净环境
// 3. 根据SDK类型选择合适的编码器
// 4. 初始化心跳机制和订阅管理
//
// 参数：
//   - ctx: 新的用户连接上下文
//   - conn: 新的长连接对象
//   - longConnServer: 长连接服务器实例
func (c *Client) ResetClient(ctx *UserConnContext, conn LongConn, longConnServer LongConnServer) {
	c.w = new(sync.Mutex)
	c.conn = conn
	c.PlatformID = stringutil.StringToInt(ctx.GetPlatformID())
	c.IsCompress = ctx.GetCompression()
	c.IsBackground = ctx.GetBackground()
	c.UserID = ctx.GetUserID()
	c.ctx = ctx
	c.longConnServer = longConnServer
	c.IsBackground = false // 默认为前台模式
	c.closed.Store(false)  // 连接状态为活跃
	c.closedErr = nil      // 清空错误信息
	c.token = ctx.GetToken()
	c.SDKType = ctx.GetSDKType()
	c.hbCtx, c.hbCancel = context.WithCancel(c.ctx) // 创建心跳上下文
	c.subLock = new(sync.Mutex)

	// 清理旧的订阅列表
	if c.subUserIDs != nil {
		clear(c.subUserIDs)
	}

	// 根据SDK类型选择编码器
	if c.SDKType == GoSDK {
		c.Encoder = NewGobEncoder() // Go SDK使用Gob编码
	} else {
		c.Encoder = NewJsonEncoder() // 其他SDK使用JSON编码
	}
	c.subUserIDs = make(map[string]struct{})
}

// pingHandler 处理客户端发送的Ping消息
// 设计思路：
// 1. 更新读取超时时间，保持连接活性
// 2. 自动回复Pong消息，完成心跳握手
// 3. 记录心跳日志，便于连接状态监控
func (c *Client) pingHandler(appData string) error {
	if err := c.conn.SetReadDeadline(pongWait); err != nil {
		return err
	}

	log.ZDebug(c.ctx, "ping Handler Success.", "appData", appData)
	return c.writePongMsg(appData)
}

// pongHandler 处理客户端发送的Pong响应
// 主要用于更新读取超时时间，确认连接活性
func (c *Client) pongHandler(_ string) error {
	if err := c.conn.SetReadDeadline(pongWait); err != nil {
		return err
	}
	return nil
}

// readMessage 持续读取客户端消息的主循环
// 设计思路：
// 1. 独立协程运行，专门处理消息接收
// 2. 支持多种消息类型：二进制、文本、控制消息
// 3. 异常恢复机制，防止panic导致服务崩溃
// 4. 自动心跳处理，维护连接活性
// 5. 优雅关闭，确保资源正确释放
func (c *Client) readMessage() {
	defer func() {
		// 异常恢复机制，防止panic影响其他连接
		if r := recover(); r != nil {
			c.closedErr = ErrPanic
			log.ZPanic(c.ctx, "socket have panic err:", errs.ErrPanic(r))
		}
		c.close()
	}()

	// 设置连接参数
	c.conn.SetReadLimit(maxMessageSize)  // 限制消息大小，防止攻击
	_ = c.conn.SetReadDeadline(pongWait) // 设置读取超时
	c.conn.SetPongHandler(c.pongHandler) // 设置Pong处理器
	c.conn.SetPingHandler(c.pingHandler) // 设置Ping处理器
	c.activeHeartbeat(c.hbCtx)           // 启动主动心跳（针对Web平台）

	// 主消息接收循环
	for {
		log.ZDebug(c.ctx, "readMessage")
		messageType, message, returnErr := c.conn.ReadMessage()
		if returnErr != nil {
			log.ZWarn(c.ctx, "readMessage", returnErr, "messageType", messageType)
			c.closedErr = returnErr
			return
		}

		log.ZDebug(c.ctx, "readMessage", "messageType", messageType)
		if c.closed.Load() {
			// 连接已关闭但协程尚未退出的场景
			c.closedErr = ErrConnClosed
			return
		}

		// 根据消息类型分别处理
		switch messageType {
		case MessageBinary:
			// 二进制消息处理（主要业务消息）
			_ = c.conn.SetReadDeadline(pongWait)
			parseDataErr := c.handleMessage(message)
			if parseDataErr != nil {
				c.closedErr = parseDataErr
				return
			}
		case MessageText:
			// 文本消息处理（心跳和控制消息）
			_ = c.conn.SetReadDeadline(pongWait)
			parseDataErr := c.handlerTextMessage(message)
			if parseDataErr != nil {
				c.closedErr = parseDataErr
				return
			}
		case PingMessage:
			// Ping消息处理
			err := c.writePongMsg("")
			log.ZError(c.ctx, "writePongMsg", err)

		case CloseMessage:
			// 客户端主动关闭
			c.closedErr = ErrClientClosed
			return

		default:
			// 未知消息类型，忽略处理
		}
	}
}

// handleMessage 处理单个业务消息
// 设计思路：
// 1. 支持数据压缩，减少网络传输量
// 2. 统一的消息格式验证
// 3. 用户身份验证，确保安全性
// 4. 基于请求标识符的消息路由
// 5. 统一的响应格式和错误处理
//
// 消息处理流程：
// 解压缩 -> 反序列化 -> 验证 -> 身份检查 -> 业务处理 -> 响应
func (c *Client) handleMessage(message []byte) error {
	// 1. 消息解压缩（如果启用）
	if c.IsCompress {
		var err error
		message, err = c.longConnServer.DecompressWithPool(message)
		if err != nil {
			return errs.Wrap(err)
		}
	}

	// 2. 获取请求对象（使用对象池优化性能）
	var binaryReq = getReq()
	defer freeReq(binaryReq)

	// 3. 消息反序列化
	err := c.Encoder.Decode(message, binaryReq)
	if err != nil {
		return err
	}

	// 4. 请求格式验证
	if err := c.longConnServer.Validate(binaryReq); err != nil {
		return err
	}

	// 5. 用户身份验证
	if binaryReq.SendID != c.UserID {
		return errs.New("exception conn userID not same to req userID", "binaryReq", binaryReq.String())
	}

	// 6. 构建请求上下文
	ctx := mcontext.WithMustInfoCtx(
		[]string{binaryReq.OperationID, binaryReq.SendID, constant.PlatformIDToName(c.PlatformID), c.ctx.GetConnID()},
	)

	log.ZDebug(ctx, "gateway req message", "req", binaryReq.String())

	var (
		resp       []byte
		messageErr error
	)

	// 7. 基于请求标识符的消息路由
	switch binaryReq.ReqIdentifier {
	case WSGetNewestSeq:
		// 获取最新序列号
		resp, messageErr = c.longConnServer.GetSeq(ctx, binaryReq)
	case WSSendMsg:
		// 发送消息
		resp, messageErr = c.longConnServer.SendMessage(ctx, binaryReq)
	case WSSendSignalMsg:
		// 发送信令消息
		resp, messageErr = c.longConnServer.SendSignalMessage(ctx, binaryReq)
	case WSPullMsgBySeqList:
		// 按序列号列表拉取消息
		resp, messageErr = c.longConnServer.PullMessageBySeqList(ctx, binaryReq)
	case WSPullMsg:
		// 拉取消息
		resp, messageErr = c.longConnServer.GetSeqMessage(ctx, binaryReq)
	case WSGetConvMaxReadSeq:
		// 获取会话最大已读序列号
		resp, messageErr = c.longConnServer.GetConversationsHasReadAndMaxSeq(ctx, binaryReq)
	case WsPullConvLastMessage:
		// 拉取会话最新消息
		resp, messageErr = c.longConnServer.GetLastMessage(ctx, binaryReq)
	case WsLogoutMsg:
		// 用户登出
		resp, messageErr = c.longConnServer.UserLogout(ctx, binaryReq)
	case WsSetBackgroundStatus:
		// 设置后台状态
		resp, messageErr = c.setAppBackgroundStatus(ctx, binaryReq)
	case WsSubUserOnlineStatus:
		// 订阅用户在线状态
		resp, messageErr = c.longConnServer.SubUserOnlineStatus(ctx, c, binaryReq)
	default:
		// 未知请求类型
		return fmt.Errorf(
			"ReqIdentifier failed,sendID:%s,msgIncr:%s,reqIdentifier:%d",
			binaryReq.SendID,
			binaryReq.MsgIncr,
			binaryReq.ReqIdentifier,
		)
	}

	// 8. 统一响应处理
	return c.replyMessage(ctx, binaryReq, messageErr, resp)
}

// setAppBackgroundStatus 设置应用后台状态
// 主要用于移动端应用进入后台时的状态管理
func (c *Client) setAppBackgroundStatus(ctx context.Context, req *Req) ([]byte, error) {
	resp, isBackground, messageErr := c.longConnServer.SetUserDeviceBackground(ctx, req)
	if messageErr != nil {
		return nil, messageErr
	}

	c.IsBackground = isBackground
	// TODO: callback - 预留回调处理
	return resp, nil
}

// close 关闭客户端连接
// 设计思路：
// 1. 使用互斥锁防止重复关闭
// 2. 原子操作标记关闭状态
// 3. 取消心跳协程
// 4. 从服务器注销连接
func (c *Client) close() {
	c.w.Lock()
	defer c.w.Unlock()
	if c.closed.Load() {
		return // 已经关闭，避免重复操作
	}
	c.closed.Store(true)           // 标记为已关闭
	c.conn.Close()                 // 关闭底层连接
	c.hbCancel()                   // 取消服务器主动心跳
	c.longConnServer.UnRegister(c) // 从服务器注销
}

// replyMessage 统一的消息响应处理
// 设计思路：
// 1. 标准化的错误处理和响应格式
// 2. 统一的响应序列化和发送
// 3. 特殊请求的后置处理（如登出）
func (c *Client) replyMessage(ctx context.Context, binaryReq *Req, err error, resp []byte) error {
	// 解析错误信息
	errResp := apiresp.ParseError(err)

	// 构建响应消息
	mReply := Resp{
		ReqIdentifier: binaryReq.ReqIdentifier,
		MsgIncr:       binaryReq.MsgIncr,
		OperationID:   binaryReq.OperationID,
		ErrCode:       errResp.ErrCode,
		ErrMsg:        errResp.ErrMsg,
		Data:          resp,
	}

	t := time.Now()
	log.ZDebug(ctx, "gateway reply message", "resp", mReply.String())

	// 发送响应
	err = c.writeBinaryMsg(mReply)
	if err != nil {
		log.ZWarn(ctx, "wireBinaryMsg replyMessage", err, "resp", mReply.String())
	}
	log.ZDebug(ctx, "wireBinaryMsg end", "time cost", time.Since(t))

	// 特殊处理：用户登出后关闭连接
	if binaryReq.ReqIdentifier == WsLogoutMsg {
		return errs.New("user logout", "operationID", binaryReq.OperationID).Wrap()
	}
	return nil
}

// PushMessage 向客户端推送消息
// 设计思路：
// 1. 支持消息和通知的分类推送
// 2. 使用protobuf序列化，保证性能
// 3. 统一的推送响应格式
func (c *Client) PushMessage(ctx context.Context, msgData *sdkws.MsgData) error {
	var msg sdkws.PushMessages
	conversationID := msgprocessor.GetConversationIDByMsg(msgData)
	m := map[string]*sdkws.PullMsgs{conversationID: {Msgs: []*sdkws.MsgData{msgData}}}

	// 根据会话类型分类处理
	if msgprocessor.IsNotification(conversationID) {
		msg.NotificationMsgs = m
	} else {
		msg.Msgs = m
	}

	log.ZDebug(ctx, "PushMessage", "msg", &msg)
	data, err := proto.Marshal(&msg)
	if err != nil {
		return err
	}

	resp := Resp{
		ReqIdentifier: WSPushMsg,
		OperationID:   mcontext.GetOperationID(ctx),
		Data:          data,
	}
	return c.writeBinaryMsg(resp)
}

// KickOnlineMessage 发送踢下线消息
// 用于多端登录策略执行时强制下线
func (c *Client) KickOnlineMessage() error {
	resp := Resp{
		ReqIdentifier: WSKickOnlineMsg,
	}
	log.ZDebug(c.ctx, "KickOnlineMessage debug ")
	err := c.writeBinaryMsg(resp)
	c.close() // 发送完成后立即关闭连接
	return err
}

// PushUserOnlineStatus 推送用户在线状态变更
// 用于订阅功能，向客户端推送关注用户的状态变更
func (c *Client) PushUserOnlineStatus(data []byte) error {
	resp := Resp{
		ReqIdentifier: WsSubUserOnlineStatus,
		Data:          data,
	}
	return c.writeBinaryMsg(resp)
}

// writeBinaryMsg 写入二进制消息
// 设计思路：
// 1. 并发写入保护，使用互斥锁
// 2. 连接状态检查，避免向已关闭连接写入
// 3. 可选的数据压缩，减少网络传输
// 4. 统一的超时处理
func (c *Client) writeBinaryMsg(resp Resp) error {
	if c.closed.Load() {
		return nil // 连接已关闭，忽略写入
	}

	// 消息编码
	encodedBuf, err := c.Encoder.Encode(resp)
	if err != nil {
		return err
	}

	// 并发写入保护
	c.w.Lock()
	defer c.w.Unlock()

	// 设置写入超时
	err = c.conn.SetWriteDeadline(writeWait)
	if err != nil {
		return err
	}

	// 可选压缩处理
	if c.IsCompress {
		resultBuf, compressErr := c.longConnServer.CompressWithPool(encodedBuf)
		if compressErr != nil {
			return compressErr
		}
		return c.conn.WriteMessage(MessageBinary, resultBuf)
	}

	return c.conn.WriteMessage(MessageBinary, encodedBuf)
}

// activeHeartbeat Web平台主动心跳
// 设计思路：
// 1. 仅针对Web平台，因为浏览器环境特殊
// 2. 独立协程运行，不阻塞主流程
// 3. 定时发送Ping消息，维护连接活性
// 4. 支持优雅取消，避免协程泄露
func (c *Client) activeHeartbeat(ctx context.Context) {
	if c.PlatformID == constant.WebPlatformID {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.ZPanic(ctx, "activeHeartbeat Panic", errs.ErrPanic(r))
				}
			}()
			log.ZDebug(ctx, "server initiative send heartbeat start.")
			ticker := time.NewTicker(pingPeriod)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					if err := c.writePingMsg(); err != nil {
						log.ZWarn(c.ctx, "send Ping Message error.", err)
						return
					}
				case <-c.hbCtx.Done():
					return
				}
			}
		}()
	}
}

// writePingMsg 发送Ping消息
func (c *Client) writePingMsg() error {
	if c.closed.Load() {
		return nil
	}

	c.w.Lock()
	defer c.w.Unlock()

	err := c.conn.SetWriteDeadline(writeWait)
	if err != nil {
		return err
	}

	return c.conn.WriteMessage(PingMessage, nil)
}

// writePongMsg 发送Pong响应消息
func (c *Client) writePongMsg(appData string) error {
	log.ZDebug(c.ctx, "write Pong Msg in Server", "appData", appData)
	if c.closed.Load() {
		log.ZWarn(c.ctx, "is closed in server", nil, "appdata", appData, "closed err", c.closedErr)
		return nil
	}

	c.w.Lock()
	defer c.w.Unlock()

	err := c.conn.SetWriteDeadline(writeWait)
	if err != nil {
		log.ZWarn(c.ctx, "SetWriteDeadline in Server have error", errs.Wrap(err), "writeWait", writeWait, "appData", appData)
		return errs.Wrap(err)
	}
	err = c.conn.WriteMessage(PongMessage, []byte(appData))
	if err != nil {
		log.ZWarn(c.ctx, "Write Message have error", errs.Wrap(err), "Pong msg", PongMessage)
	}

	return errs.Wrap(err)
}

// handlerTextMessage 处理文本消息（主要是心跳消息）
// 设计思路：
// 1. 支持JSON格式的文本消息
// 2. 实现Ping/Pong心跳机制
// 3. 可扩展的文本消息处理框架
func (c *Client) handlerTextMessage(b []byte) error {
	var msg TextMessage
	if err := json.Unmarshal(b, &msg); err != nil {
		return err
	}

	switch msg.Type {
	case TextPong:
		// Pong消息，直接忽略
		return nil
	case TextPing:
		// Ping消息，回复Pong
		msg.Type = TextPong
		msgData, err := json.Marshal(msg)
		if err != nil {
			return err
		}
		c.w.Lock()
		defer c.w.Unlock()
		if err := c.conn.SetWriteDeadline(writeWait); err != nil {
			return err
		}
		return c.conn.WriteMessage(MessageText, msgData)
	default:
		return fmt.Errorf("not support message type %s", msg.Type)
	}
}
