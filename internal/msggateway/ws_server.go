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

// ws_server.go - WebSocket服务器核心模块
//
// 功能概述:
// 1. 提供WebSocket长连接服务，支持实时消息推送
// 2. 管理客户端连接的完整生命周期（注册、验证、注销）
// 3. 实现多端登录策略和连接冲突处理机制
// 4. 集成服务发现和负载均衡，支持集群部署
// 5. 提供连接状态监控和性能指标统计
//
// 设计思路:
// - 事件驱动: 使用通道机制处理连接事件，支持高并发
// - 对象池优化: 复用客户端对象，减少GC压力
// - 分层架构: 清晰的接口设计，支持组件替换和测试
// - 集群协调: 跨节点的多端登录检查和状态同步
// - 高可用设计: 优雅关闭、错误恢复、监控集成
//
// 核心组件:
// - LongConnServer接口: 长连接服务器的核心抽象
// - WsServer: WebSocket服务器的具体实现
// - 连接管理: 客户端注册、注销、状态跟踪
// - 认证体系: 令牌验证、权限检查、多端冲突处理

package msggateway

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/rpcli"

	"github.com/go-playground/validator/v10"
	"github.com/openimsdk/open-im-server/v3/pkg/common/prommetrics"
	"github.com/openimsdk/open-im-server/v3/pkg/common/servererrs"
	"github.com/openimsdk/open-im-server/v3/pkg/common/webhook"
	"github.com/openimsdk/open-im-server/v3/pkg/rpccache"
	pbAuth "github.com/openimsdk/protocol/auth"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/msggateway"
	"github.com/openimsdk/tools/discovery"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
	"github.com/openimsdk/tools/utils/stringutil"
	"golang.org/x/sync/errgroup"
)

// LongConnServer 长连接服务器接口定义
// 设计思路：
// 1. 定义完整的长连接服务能力抽象
// 2. 支持依赖注入和接口替换
// 3. 集成消息处理、压缩、验证等核心功能
// 4. 提供清晰的服务边界和职责分离
type LongConnServer interface {
	// Run 启动服务器，接收错误通道用于异常处理
	Run(done chan error) error

	// wsHandler WebSocket请求处理器
	wsHandler(w http.ResponseWriter, r *http.Request)

	// GetUserAllCons 获取用户的所有连接
	GetUserAllCons(userID string) ([]*Client, bool)

	// GetUserPlatformCons 获取用户在指定平台的连接
	GetUserPlatformCons(userID string, platform int) ([]*Client, bool, bool)

	// Validate 验证数据结构
	Validate(s any) error

	// SetDiscoveryRegistry 设置服务发现注册器
	SetDiscoveryRegistry(ctx context.Context, client discovery.SvcDiscoveryRegistry, config *Config) error

	// KickUserConn 踢用户下线
	KickUserConn(client *Client) error

	// UnRegister 注销客户端连接
	UnRegister(c *Client)

	// SetKickHandlerInfo 设置踢下线处理信息
	SetKickHandlerInfo(i *kickHandler)

	// SubUserOnlineStatus 订阅用户在线状态
	SubUserOnlineStatus(ctx context.Context, client *Client, data *Req) ([]byte, error)

	// 嵌入压缩器和消息处理器接口
	Compressor
	MessageHandler
}

// WsServer WebSocket服务器结构体
// 设计思路：
// 1. 聚合所有服务组件，实现完整的WebSocket服务
// 2. 使用原子操作管理并发统计数据
// 3. 通道机制处理异步事件，避免阻塞
// 4. 对象池优化内存使用，提高性能
// 5. 集成监控、认证、消息处理等核心能力
type WsServer struct {
	msgGatewayConfig  *Config                        // 消息网关配置
	port              int                            // 服务端口
	wsMaxConnNum      int64                          // 最大连接数限制
	registerChan      chan *Client                   // 客户端注册通道
	unregisterChan    chan *Client                   // 客户端注销通道
	kickHandlerChan   chan *kickHandler              // 踢下线处理通道
	clients           UserMap                        // 用户连接映射管理器
	online            *rpccache.OnlineCache          // 在线状态缓存
	subscription      *Subscription                  // 订阅管理器
	clientPool        sync.Pool                      // 客户端对象池
	onlineUserNum     atomic.Int64                   // 在线用户数量（原子操作）
	onlineUserConnNum atomic.Int64                   // 在线连接数量（原子操作）
	handshakeTimeout  time.Duration                  // WebSocket握手超时时间
	writeBufferSize   int                            // 写缓冲区大小
	validate          *validator.Validate            // 数据验证器
	disCov            discovery.SvcDiscoveryRegistry // 服务发现客户端

	// 嵌入的功能组件
	Compressor     // 数据压缩器
	MessageHandler // 消息处理器

	// RPC客户端
	webhookClient *webhook.Client   // Webhook客户端
	userClient    *rpcli.UserClient // 用户服务客户端
	authClient    *rpcli.AuthClient // 认证服务客户端
}

// kickHandler 踢下线处理器结构体
// 设计思路：
// 1. 封装踢下线所需的上下文信息
// 2. 支持新旧连接的冲突处理
// 3. 用于多端登录策略的执行
type kickHandler struct {
	clientOK   bool      // 是否存在同平台客户端
	oldClients []*Client // 旧的客户端连接列表
	newClient  *Client   // 新的客户端连接
}

// SetDiscoveryRegistry 设置服务发现注册器并建立RPC连接
// 设计思路：
// 1. 通过服务发现获取各个微服务的连接
// 2. 初始化RPC客户端，建立服务间通信
// 3. 配置消息处理器，集成业务处理能力
// 4. 缓存服务发现客户端供后续使用
//
// 参数：
//   - ctx: 上下文信息
//   - disCov: 服务发现注册器
//   - config: 配置信息
//
// 返回值：
//   - error: 初始化错误信息
func (ws *WsServer) SetDiscoveryRegistry(ctx context.Context, disCov discovery.SvcDiscoveryRegistry, config *Config) error {
	// 获取用户服务连接
	userConn, err := disCov.GetConn(ctx, config.Share.RpcRegisterName.User)
	if err != nil {
		return err
	}

	// 获取推送服务连接
	pushConn, err := disCov.GetConn(ctx, config.Share.RpcRegisterName.Push)
	if err != nil {
		return err
	}

	// 获取认证服务连接
	authConn, err := disCov.GetConn(ctx, config.Share.RpcRegisterName.Auth)
	if err != nil {
		return err
	}

	// 获取消息服务连接
	msgConn, err := disCov.GetConn(ctx, config.Share.RpcRegisterName.Msg)
	if err != nil {
		return err
	}

	// 初始化RPC客户端
	ws.userClient = rpcli.NewUserClient(userConn)
	ws.authClient = rpcli.NewAuthClient(authConn)

	// 初始化消息处理器，集成多个RPC服务
	ws.MessageHandler = NewGrpcHandler(
		ws.validate,
		rpcli.NewMsgClient(msgConn),
		rpcli.NewPushMsgServiceClient(pushConn),
	)

	// 缓存服务发现客户端
	ws.disCov = disCov
	return nil
}

// UnRegister 注销客户端连接
// 设计思路：
// 1. 通过通道异步处理注销请求
// 2. 避免阻塞调用者线程
// 3. 保证注销操作的线程安全
//
// 参数：
//   - c: 要注销的客户端连接
func (ws *WsServer) UnRegister(c *Client) {
	ws.unregisterChan <- c
}

// Validate 数据验证方法（当前为空实现）
// 设计思路：
// 1. 预留验证接口，支持后续扩展
// 2. 统一的验证入口，便于规则管理
// 3. 符合接口契约，保持系统一致性
//
// 参数：
//   - any: 待验证的数据
//
// 返回值：
//   - error: 验证错误信息
func (ws *WsServer) Validate(_ any) error {
	return nil
}

// GetUserAllCons 获取用户的所有连接
// 设计思路：
// 1. 委托给用户映射管理器处理
// 2. 提供统一的查询接口
// 3. 支持高频查询操作
//
// 参数：
//   - userID: 用户ID
//
// 返回值：
//   - []*Client: 用户的所有连接
//   - bool: 用户是否存在
func (ws *WsServer) GetUserAllCons(userID string) ([]*Client, bool) {
	return ws.clients.GetAll(userID)
}

// GetUserPlatformCons 获取用户在指定平台的连接
// 设计思路：
// 1. 支持平台级别的精确查询
// 2. 用于多端登录策略的实现
// 3. 提供详细的查询结果状态
//
// 参数：
//   - userID: 用户ID
//   - platform: 平台ID
//
// 返回值：
//   - []*Client: 平台连接列表
//   - bool: 用户是否存在
//   - bool: 平台是否有连接
func (ws *WsServer) GetUserPlatformCons(userID string, platform int) ([]*Client, bool, bool) {
	return ws.clients.Get(userID, platform)
}

// NewWsServer 创建新的WebSocket服务器实例
// 设计思路：
// 1. 使用选项模式配置服务器参数
// 2. 初始化所有必要的组件和通道
// 3. 配置合理的默认值和缓冲区大小
// 4. 集成监控、压缩、验证等功能
//
// 参数：
//   - msgGatewayConfig: 消息网关配置
//   - opts: 可选配置选项
//
// 返回值：
//   - *WsServer: 初始化完成的服务器实例
func NewWsServer(msgGatewayConfig *Config, opts ...Option) *WsServer {
	var config configs
	for _, o := range opts {
		o(&config)
	}

	// 创建数据验证器
	v := validator.New()

	return &WsServer{
		msgGatewayConfig: msgGatewayConfig,
		port:             config.port,
		wsMaxConnNum:     config.maxConnNum,
		writeBufferSize:  config.writeBufferSize,
		handshakeTimeout: config.handshakeTimeout,

		// 配置客户端对象池
		clientPool: sync.Pool{
			New: func() any {
				return new(Client)
			},
		},

		// 初始化事件处理通道（缓冲容量1000，支持高并发）
		registerChan:    make(chan *Client, 1000),
		unregisterChan:  make(chan *Client, 1000),
		kickHandlerChan: make(chan *kickHandler, 1000),

		// 初始化核心组件
		validate:      v,
		clients:       newUserMap(),                                                  // 用户连接映射管理器
		subscription:  newSubscription(),                                             // 订阅管理器
		Compressor:    NewGzipCompressor(),                                           // Gzip压缩器
		webhookClient: webhook.NewWebhookClient(msgGatewayConfig.WebhooksConfig.URL), // Webhook客户端
	}
}

// Run 启动WebSocket服务器
// 设计思路：
// 1. 启动多个独立的事件处理协程
// 2. 使用HTTP服务器承载WebSocket连接
// 3. 支持优雅关闭和错误处理
// 4. 集成超时控制和资源清理
//
// 处理流程：
// 启动事件循环 -> 启动HTTP服务 -> 等待关闭信号 -> 优雅关闭
//
// 参数：
//   - done: 关闭信号通道
//
// 返回值：
//   - error: 启动或运行错误
func (ws *WsServer) Run(done chan error) error {
	var (
		client       *Client
		netErr       error
		shutdownDone = make(chan struct{}, 1)
	)

	// 创建HTTP服务器
	server := http.Server{
		Addr:    ":" + stringutil.IntToString(ws.port),
		Handler: nil,
	}

	// 启动事件处理协程
	go func() {
		for {
			select {
			case <-shutdownDone:
				return
			case client = <-ws.registerChan:
				// 处理客户端注册事件
				ws.registerClient(client)
			case client = <-ws.unregisterChan:
				// 处理客户端注销事件
				ws.unregisterClient(client)
			case onlineInfo := <-ws.kickHandlerChan:
				// 处理踢下线事件
				ws.multiTerminalLoginChecker(onlineInfo.clientOK, onlineInfo.oldClients, onlineInfo.newClient)
			}
		}
	}()

	// 启动HTTP服务协程
	netDone := make(chan struct{}, 1)
	go func() {
		// 注册WebSocket处理器
		http.HandleFunc("/", ws.wsHandler)
		err := server.ListenAndServe()
		defer close(netDone)
		if err != nil && err != http.ErrServerClosed {
			netErr = errs.WrapMsg(err, "ws start err", server.Addr)
		}
	}()

	// 等待关闭信号
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var err error
	select {
	case err = <-done:
		// 收到关闭信号，优雅关闭HTTP服务器
		sErr := server.Shutdown(ctx)
		if sErr != nil {
			return errs.WrapMsg(sErr, "shutdown err")
		}
		close(shutdownDone)
		if err != nil {
			return err
		}
	case <-netDone:
		// HTTP服务器自然结束
	}
	return netErr
}

// concurrentRequest 并发请求的限制数量
// 用于控制集群间通信的并发度，避免资源过度消耗
var concurrentRequest = 3

// sendUserOnlineInfoToOtherNode 向其他节点发送用户上线信息
// 设计思路：
// 1. 获取集群中的所有节点连接
// 2. 并发向其他节点发送多端登录检查请求
// 3. 使用错误组控制并发数量
// 4. 过滤掉当前节点，避免自调用
//
// 应用场景：
// - 新用户连接时的集群状态同步
// - 多端登录策略的跨节点协调
// - 负载均衡场景下的状态一致性
//
// 参数：
//   - ctx: 上下文信息
//   - client: 新连接的客户端
//
// 返回值：
//   - error: 发送错误信息
func (ws *WsServer) sendUserOnlineInfoToOtherNode(ctx context.Context, client *Client) error {
	// 获取集群中所有节点的连接
	conns, err := ws.disCov.GetConns(ctx, ws.msgGatewayConfig.Share.RpcRegisterName.MessageGateway)
	if err != nil {
		return err
	}

	// 如果只有当前节点或无其他节点，直接返回
	if len(conns) == 0 || (len(conns) == 1 && ws.disCov.IsSelfNode(conns[0])) {
		return nil
	}

	// 使用错误组控制并发数量
	wg := errgroup.Group{}
	wg.SetLimit(concurrentRequest)

	// 向每个其他节点发送在线信息
	for _, v := range conns {
		v := v
		log.ZDebug(ctx, "sendUserOnlineInfoToOtherNode conn")

		// 过滤掉当前节点
		if ws.disCov.IsSelfNode(v) {
			log.ZDebug(ctx, "Filter out this node")
			continue
		}

		wg.Go(func() error {
			// 创建消息网关客户端
			msgClient := msggateway.NewMsgGatewayClient(v)

			// 发送多端登录检查请求
			_, err := msgClient.MultiTerminalLoginCheck(ctx, &msggateway.MultiTerminalLoginCheckReq{
				UserID:     client.UserID,
				PlatformID: int32(client.PlatformID),
				Token:      client.token,
			})
			if err != nil {
				log.ZWarn(ctx, "MultiTerminalLoginCheck err", err)
			}
			return nil
		})
	}

	// 等待所有请求完成
	_ = wg.Wait()
	return nil
}

// SetKickHandlerInfo 设置踢下线处理信息
// 设计思路：
// 1. 通过通道异步处理踢下线请求
// 2. 避免阻塞调用者线程
// 3. 支持批量踢下线处理
//
// 参数：
//   - i: 踢下线处理器信息
func (ws *WsServer) SetKickHandlerInfo(i *kickHandler) {
	ws.kickHandlerChan <- i
}

// registerClient 注册新的客户端连接
// 设计思路：
// 1. 检查用户和平台的连接状态
// 2. 处理多端登录冲突和策略执行
// 3. 更新在线统计数据和监控指标
// 4. 异步执行跨节点状态同步
//
// 处理逻辑：
// 检查连接状态 -> 执行多端策略 -> 更新统计 -> 跨节点同步
//
// 参数：
//   - client: 要注册的客户端连接
func (ws *WsServer) registerClient(client *Client) {
	var (
		userOK     bool      // 用户是否已存在
		clientOK   bool      // 同平台是否有连接
		oldClients []*Client // 同平台的旧连接
	)

	// 检查用户在指定平台的连接状态
	oldClients, userOK, clientOK = ws.clients.Get(client.UserID, client.PlatformID)

	if !userOK {
		// 新用户首次连接
		ws.clients.Set(client.UserID, client)
		log.ZDebug(client.ctx, "user not exist", "userID", client.UserID, "platformID", client.PlatformID)

		// 更新在线用户数量统计
		prommetrics.OnlineUserGauge.Add(1)
		ws.onlineUserNum.Add(1)
		ws.onlineUserConnNum.Add(1)
	} else {
		// 用户已存在，处理多端登录
		ws.multiTerminalLoginChecker(clientOK, oldClients, client)
		log.ZDebug(client.ctx, "user exist", "userID", client.UserID, "platformID", client.PlatformID)

		if clientOK {
			// 同平台重复登录
			ws.clients.Set(client.UserID, client)
			log.ZDebug(client.ctx, "repeat login", "userID", client.UserID, "platformID",
				client.PlatformID, "old remote addr", getRemoteAdders(oldClients))
			ws.onlineUserConnNum.Add(1)
		} else {
			// 新平台连接
			ws.clients.Set(client.UserID, client)
			ws.onlineUserConnNum.Add(1)
		}
	}

	// 异步执行后续处理
	wg := sync.WaitGroup{}
	log.ZDebug(client.ctx, "ws.msgGatewayConfig.Discovery.Enable", "discoveryEnable", ws.msgGatewayConfig.Discovery.Enable)

	// 如果不是k8s环境，执行跨节点状态同步
	if ws.msgGatewayConfig.Discovery.Enable != "k8s" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = ws.sendUserOnlineInfoToOtherNode(client.ctx, client)
		}()
	}

	wg.Wait()

	log.ZDebug(client.ctx, "user online", "online user Num", ws.onlineUserNum.Load(), "online user conn Num", ws.onlineUserConnNum.Load())
}

// getRemoteAdders 获取客户端连接的远程地址列表
// 设计思路：
// 1. 拼接多个连接的地址信息
// 2. 用于日志记录和调试信息
// 3. 支持连接溯源和问题排查
//
// 参数：
//   - client: 客户端连接列表
//
// 返回值：
//   - string: 拼接后的地址字符串
func getRemoteAdders(client []*Client) string {
	var ret string
	for i, c := range client {
		if i == 0 {
			ret = c.ctx.GetRemoteAddr()
		} else {
			ret += "@" + c.ctx.GetRemoteAddr()
		}
	}
	return ret
}

// KickUserConn 踢用户连接下线
// 设计思路：
// 1. 从用户映射中删除指定连接
// 2. 发送踢下线消息通知客户端
// 3. 处理连接清理和资源释放
//
// 参数：
//   - client: 要踢下线的客户端
//
// 返回值：
//   - error: 踢下线错误信息
func (ws *WsServer) KickUserConn(client *Client) error {
	ws.clients.DeleteClients(client.UserID, []*Client{client})
	return client.KickOnlineMessage()
}

// multiTerminalLoginChecker 多端登录策略检查器
// 设计思路：
// 1. 根据配置的多端登录策略执行相应逻辑
// 2. 支持多种策略：不踢、PC和其他、同终端踢、同类别踢
// 3. 处理令牌失效和连接清理
// 4. 集成认证服务完成令牌管理
//
// 支持的策略：
// - DefalutNotKick: 默认不踢下线
// - PCAndOther: PC端不踢，其他端按终端踢
// - AllLoginButSameTermKick: 同终端踢下线
// - AllLoginButSameClassKick: 同类别踢下线
//
// 参数：
//   - clientOK: 是否存在同平台客户端
//   - oldClients: 旧的客户端连接列表
//   - newClient: 新的客户端连接
func (ws *WsServer) multiTerminalLoginChecker(clientOK bool, oldClients []*Client, newClient *Client) {
	// 踢下线令牌处理函数
	kickTokenFunc := func(kickClients []*Client) {
		var kickTokens []string

		// 删除被踢的连接
		ws.clients.DeleteClients(newClient.UserID, kickClients)

		// 发送踢下线消息并收集令牌
		for _, c := range kickClients {
			kickTokens = append(kickTokens, c.token)
			err := c.KickOnlineMessage()
			if err != nil {
				log.ZWarn(c.ctx, "KickOnlineMessage", err)
			}
		}

		// 创建上下文信息
		ctx := mcontext.WithMustInfoCtx(
			[]string{newClient.ctx.GetOperationID(), newClient.ctx.GetUserID(),
				constant.PlatformIDToName(newClient.PlatformID), newClient.ctx.GetConnID()},
		)

		// 调用认证服务踢令牌
		if err := ws.authClient.KickTokens(ctx, kickTokens); err != nil {
			log.ZWarn(newClient.ctx, "kickTokens err", err)
		}
	}

	// 根据多端登录策略执行相应逻辑
	switch ws.msgGatewayConfig.Share.MultiLogin.Policy {
	case constant.DefalutNotKick:
		// 默认策略：不踢任何连接

	case constant.PCAndOther:
		// PC和其他策略：PC端不踢，其他端按终端处理
		if constant.PlatformIDToClass(newClient.PlatformID) == constant.TerminalPC {
			return
		}
		fallthrough

	case constant.AllLoginButSameTermKick:
		// 同终端踢下线策略
		if !clientOK {
			return
		}

		// 删除旧连接
		ws.clients.DeleteClients(newClient.UserID, oldClients)
		for _, c := range oldClients {
			err := c.KickOnlineMessage()
			if err != nil {
				log.ZWarn(c.ctx, "KickOnlineMessage", err)
			}
		}

		// 失效旧令牌，保留新令牌
		ctx := mcontext.WithMustInfoCtx(
			[]string{newClient.ctx.GetOperationID(), newClient.ctx.GetUserID(),
				constant.PlatformIDToName(newClient.PlatformID), newClient.ctx.GetConnID()},
		)
		req := &pbAuth.InvalidateTokenReq{
			PreservedToken: newClient.token,
			UserID:         newClient.UserID,
			PlatformID:     int32(newClient.PlatformID),
		}
		if err := ws.authClient.InvalidateToken(ctx, req); err != nil {
			log.ZWarn(newClient.ctx, "InvalidateToken err", err, "userID", newClient.UserID,
				"platformID", newClient.PlatformID)
		}

	case constant.AllLoginButSameClassKick:
		// 同类别踢下线策略
		clients, ok := ws.clients.GetAll(newClient.UserID)
		if !ok {
			return
		}

		var kickClients []*Client
		// 查找同类别的连接
		for _, client := range clients {
			if constant.PlatformIDToClass(client.PlatformID) == constant.PlatformIDToClass(newClient.PlatformID) {
				kickClients = append(kickClients, client)
			}
		}
		kickTokenFunc(kickClients)
	}
}

// unregisterClient 注销客户端连接
// 设计思路：
// 1. 清理用户连接映射关系
// 2. 更新在线统计数据和监控指标
// 3. 清理订阅关系和资源
// 4. 回收客户端对象到对象池
//
// 参数：
//   - client: 要注销的客户端连接
func (ws *WsServer) unregisterClient(client *Client) {
	// 将客户端对象放回对象池
	defer ws.clientPool.Put(client)

	// 从用户映射中删除连接
	isDeleteUser := ws.clients.DeleteClients(client.UserID, []*Client{client})

	// 更新统计数据
	if isDeleteUser {
		ws.onlineUserNum.Add(-1)
		prommetrics.OnlineUserGauge.Dec()
	}
	ws.onlineUserConnNum.Add(-1)

	// 清理订阅关系
	ws.subscription.DelClient(client)

	log.ZDebug(client.ctx, "user offline", "close reason", client.closedErr, "online user Num",
		ws.onlineUserNum.Load(), "online user conn Num",
		ws.onlineUserConnNum.Load(),
	)
}

// validateRespWithRequest 验证认证响应与请求的匹配性
// 设计思路：
// 1. 确保令牌中的用户ID与请求一致
// 2. 确保令牌中的平台ID与请求一致
// 3. 防止令牌伪造和攻击
//
// 参数：
//   - ctx: 用户连接上下文
//   - resp: 认证响应
//
// 返回值：
//   - error: 验证错误信息
func (ws *WsServer) validateRespWithRequest(ctx *UserConnContext, resp *pbAuth.ParseTokenResp) error {
	userID := ctx.GetUserID()
	platformID := stringutil.StringToInt32(ctx.GetPlatformID())

	// 验证用户ID匹配
	if resp.UserID != userID {
		return servererrs.ErrTokenInvalid.WrapMsg(fmt.Sprintf("token uid %s != userID %s", resp.UserID, userID))
	}

	// 验证平台ID匹配
	if resp.PlatformID != platformID {
		return servererrs.ErrTokenInvalid.WrapMsg(fmt.Sprintf("token platform %d != platformID %d", resp.PlatformID, platformID))
	}
	return nil
}

// wsHandler WebSocket请求处理器
// 设计思路：
// 1. 完整的连接建立流程：验证 -> 认证 -> 握手 -> 注册
// 2. 分层的错误处理：HTTP错误 vs WebSocket错误
// 3. 连接数限制和资源保护
// 4. 对象池优化和生命周期管理
//
// 处理流程：
// 连接数检查 -> 参数解析 -> 令牌验证 -> WebSocket握手 -> 客户端注册 -> 消息循环
//
// 参数：
//   - w: HTTP响应写入器
//   - r: HTTP请求对象
func (ws *WsServer) wsHandler(w http.ResponseWriter, r *http.Request) {
	// 创建连接上下文
	connContext := newContext(w, r)

	// 检查连接数限制
	if ws.onlineUserConnNum.Load() >= ws.wsMaxConnNum {
		httpError(connContext, servererrs.ErrConnOverMaxNumLimit.WrapMsg("over max conn num limit"))
		return
	}

	// 解析必要参数（用户ID、令牌等）
	err := connContext.ParseEssentialArgs()
	if err != nil {
		httpError(connContext, err)
		return
	}

	// 调用认证服务解析令牌
	resp, err := ws.authClient.ParseToken(connContext, connContext.GetToken())
	if err != nil {
		// 根据上下文判断是否需要通过WebSocket发送错误
		shouldSendError := connContext.ShouldSendResp()
		if shouldSendError {
			// 尝试建立WebSocket连接发送错误
			wsLongConn := newGWebSocket(WebSocket, ws.handshakeTimeout, ws.writeBufferSize)
			if err := wsLongConn.RespondWithError(err, w, r); err == nil {
				return
			}
		}
		// 通过HTTP返回错误
		httpError(connContext, err)
		return
	}

	// 验证认证响应的匹配性
	err = ws.validateRespWithRequest(connContext, resp)
	if err != nil {
		httpError(connContext, err)
		return
	}

	log.ZDebug(connContext, "new conn", "token", connContext.GetToken())

	// 创建WebSocket长连接
	wsLongConn := newGWebSocket(WebSocket, ws.handshakeTimeout, ws.writeBufferSize)
	if err := wsLongConn.GenerateLongConn(w, r); err != nil {
		log.ZWarn(connContext, "long connection fails", err)
		return
	} else {
		// 检查是否需要发送成功响应
		shouldSendSuccessResp := connContext.ShouldSendResp()
		if shouldSendSuccessResp {
			if err := wsLongConn.RespondWithSuccess(); err != nil {
				return
			}
		}
	}

	// 从对象池获取客户端对象并重置状态
	client := ws.clientPool.Get().(*Client)
	client.ResetClient(connContext, wsLongConn, ws)

	// 注册客户端并启动消息处理循环
	ws.registerChan <- client
	go client.readMessage()
}
