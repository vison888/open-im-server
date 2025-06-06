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

// hub_server.go - 消息分发中心模块
//
// 功能概述:
// 1. 作为消息网关的核心服务器，处理gRPC接口调用
// 2. 管理在线用户状态查询和消息批量推送
// 3. 实现多端登录策略和用户踢下线机制
// 4. 提供集群间的多终端登录检查服务
// 5. 支持大规模并发消息推送和状态管理
//
// 设计思路:
// - 中心化管理: 作为消息分发的核心枢纽
// - 批量处理: 支持批量消息推送，提高性能
// - 状态管理: 实时维护用户在线状态
// - 集群协调: 支持多节点间的状态同步
// - 性能优化: 使用内存队列和并发处理提升吞吐量
//
// 核心服务:
// - 在线状态查询: 获取用户详细的在线状态信息
// - 批量消息推送: 高效的群组消息分发
// - 用户踢下线: 多端登录策略执行
// - 多终端检查: 跨节点的登录状态验证

package msggateway

import (
	"context"
	"sync/atomic"

	"github.com/openimsdk/open-im-server/v3/pkg/rpcli"

	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/open-im-server/v3/pkg/common/servererrs"
	"github.com/openimsdk/open-im-server/v3/pkg/common/startrpc"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/msggateway"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/discovery"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
	"github.com/openimsdk/tools/mq/memamq"
	"github.com/openimsdk/tools/utils/datautil"
	"google.golang.org/grpc"
)

// InitServer 初始化服务器组件和依赖
// 设计思路：
// 1. 建立与用户服务的RPC连接
// 2. 设置服务发现和注册
// 3. 注册gRPC服务处理器
// 4. 执行自定义的就绪回调
//
// 参数：
//   - ctx: 上下文信息
//   - config: 配置信息
//   - disCov: 服务发现注册器
//   - server: gRPC服务器实例
//
// 返回值：
//   - error: 初始化错误信息
func (s *Server) InitServer(ctx context.Context, config *Config, disCov discovery.SvcDiscoveryRegistry, server *grpc.Server) error {
	// 建立与用户服务的连接
	userConn, err := disCov.GetConn(ctx, config.Share.RpcRegisterName.User)
	if err != nil {
		return err
	}
	s.userClient = rpcli.NewUserClient(userConn)

	// 配置长连接服务器的服务发现
	if err := s.LongConnServer.SetDiscoveryRegistry(ctx, disCov, config); err != nil {
		return err
	}

	// 注册消息网关gRPC服务
	msggateway.RegisterMsgGatewayServer(server, s)

	// 执行就绪回调（如果已设置）
	if s.ready != nil {
		return s.ready(s)
	}
	return nil
}

// Start 启动消息网关服务器
// 设计思路：
// 1. 使用通用的RPC启动框架
// 2. 支持自动端口分配和服务注册
// 3. 集成Prometheus监控
// 4. 支持多实例部署
//
// 参数：
//   - ctx: 上下文信息
//   - index: 服务实例索引
//   - conf: 完整配置信息
//
// 返回值：
//   - error: 启动错误信息
func (s *Server) Start(ctx context.Context, index int, conf *Config) error {
	return startrpc.Start(ctx, &conf.Discovery, &conf.MsgGateway.Prometheus, conf.MsgGateway.ListenIP,
		conf.MsgGateway.RPC.RegisterIP,
		conf.MsgGateway.RPC.AutoSetPorts,
		conf.MsgGateway.RPC.Ports, index,
		conf.Share.RpcRegisterName.MessageGateway,
		&conf.Share,
		conf,
		[]string{
			conf.Share.RpcRegisterName.MessageGateway,
		},
		s.InitServer,
	)
}

// Server 消息网关服务器结构体
// 设计思路：
// 1. 实现msggateway.MsgGatewayServer接口
// 2. 集成长连接服务器管理
// 3. 支持推送终端配置和就绪回调
// 4. 使用内存队列优化并发性能
type Server struct {
	msggateway.UnimplementedMsgGatewayServer                         // gRPC服务器接口实现
	rpcPort                                  int                     // RPC服务端口
	LongConnServer                           LongConnServer          // 长连接服务器接口
	config                                   *Config                 // 服务器配置
	pushTerminal                             map[int]struct{}        // 支持推送的终端类型集合
	ready                                    func(srv *Server) error // 服务器就绪回调函数
	queue                                    *memamq.MemoryQueue     // 内存消息队列，用于异步处理
	userClient                               *rpcli.UserClient       // 用户服务RPC客户端
}

// SetLongConnServer 设置长连接服务器
// 用于依赖注入，将具体的长连接实现注入到服务器中
func (s *Server) SetLongConnServer(LongConnServer LongConnServer) {
	s.LongConnServer = LongConnServer
}

// NewServer 创建新的消息网关服务器实例
// 设计思路：
// 1. 初始化服务器的基本配置
// 2. 设置推送终端支持（iOS和Android）
// 3. 创建内存队列用于异步处理
// 4. 配置就绪回调机制
//
// 参数：
//   - longConnServer: 长连接服务器实例
//   - conf: 服务器配置
//   - ready: 就绪回调函数
//
// 返回值：
//   - *Server: 初始化完成的服务器实例
func NewServer(longConnServer LongConnServer, conf *Config, ready func(srv *Server) error) *Server {
	s := &Server{
		LongConnServer: longConnServer,
		pushTerminal:   make(map[int]struct{}),
		config:         conf,
		ready:          ready,
		queue:          memamq.NewMemoryQueue(512, 1024*16), // 512个worker，16KB缓冲区
	}
	// 配置支持推送的终端类型
	s.pushTerminal[constant.IOSPlatformID] = struct{}{}     // iOS终端
	s.pushTerminal[constant.AndroidPlatformID] = struct{}{} // Android终端
	return s
}

// GetUsersOnlineStatus 获取用户在线状态详细信息
// 设计思路：
// 1. 权限验证：仅允许管理员调用
// 2. 遍历指定用户列表，获取连接信息
// 3. 构建详细的在线状态响应
// 4. 包含平台、连接ID、令牌等详细信息
//
// 参数：
//   - ctx: 上下文信息
//   - req: 包含用户ID列表的请求
//
// 返回值：
//   - *msggateway.GetUsersOnlineStatusResp: 详细的在线状态响应
//   - error: 错误信息
func (s *Server) GetUsersOnlineStatus(ctx context.Context, req *msggateway.GetUsersOnlineStatusReq) (*msggateway.GetUsersOnlineStatusResp, error) {
	// 权限验证：仅管理员可调用
	if !authverify.IsAppManagerUid(ctx, s.config.Share.IMAdminUserID) {
		return nil, errs.ErrNoPermission.WrapMsg("only app manager")
	}

	var resp msggateway.GetUsersOnlineStatusResp

	// 遍历每个用户ID，获取其在线状态
	for _, userID := range req.UserIDs {
		clients, ok := s.LongConnServer.GetUserAllCons(userID)
		if !ok {
			continue // 用户无在线连接，跳过
		}

		// 构建用户在线状态详情
		uresp := new(msggateway.GetUsersOnlineStatusResp_SuccessResult)
		uresp.UserID = userID

		// 遍历用户的所有连接，收集详细信息
		for _, client := range clients {
			if client == nil {
				continue
			}

			// 构建平台详细状态信息
			ps := new(msggateway.GetUsersOnlineStatusResp_SuccessDetail)
			ps.PlatformID = int32(client.PlatformID) // 平台ID
			ps.ConnID = client.ctx.GetConnID()       // 连接ID
			ps.Token = client.token                  // 认证令牌
			ps.IsBackground = client.IsBackground    // 是否后台模式
			uresp.Status = constant.Online           // 设置为在线状态
			uresp.DetailPlatformStatus = append(uresp.DetailPlatformStatus, ps)
		}

		// 只有在线用户才添加到响应中
		if uresp.Status == constant.Online {
			resp.SuccessResult = append(resp.SuccessResult, uresp)
		}
	}
	return &resp, nil
}

// pushToUser 向指定用户推送消息
// 设计思路：
// 1. 获取用户的所有在线连接
// 2. 遍历每个连接进行消息推送
// 3. 处理后台模式和iOS特殊逻辑
// 4. 记录推送结果和错误信息
//
// 参数：
//   - ctx: 上下文信息
//   - userID: 目标用户ID
//   - msgData: 要推送的消息数据
//
// 返回值：
//   - *msggateway.SingleMsgToUserResults: 推送结果详情
func (s *Server) pushToUser(ctx context.Context, userID string, msgData *sdkws.MsgData) *msggateway.SingleMsgToUserResults {
	// 获取用户所有在线连接
	clients, ok := s.LongConnServer.GetUserAllCons(userID)
	if !ok {
		log.ZDebug(ctx, "push user not online", "userID", userID)
		return &msggateway.SingleMsgToUserResults{
			UserID: userID,
		}
	}

	log.ZDebug(ctx, "push user online", "clients", clients, "userID", userID)
	result := &msggateway.SingleMsgToUserResults{
		UserID: userID,
		Resp:   make([]*msggateway.SingleMsgToUserPlatform, 0, len(clients)),
	}

	// 遍历用户的每个连接进行推送
	for _, client := range clients {
		if client == nil {
			continue
		}

		userPlatform := &msggateway.SingleMsgToUserPlatform{
			RecvPlatFormID: int32(client.PlatformID),
		}

		// 推送逻辑：非后台模式或非iOS后台模式才推送
		if !client.IsBackground ||
			(client.IsBackground && client.PlatformID != constant.IOSPlatformID) {
			err := client.PushMessage(ctx, msgData)
			if err != nil {
				log.ZWarn(ctx, "online push msg failed", err, "userID", userID, "platformID", client.PlatformID)
				userPlatform.ResultCode = int64(servererrs.ErrPushMsgErr.Code())
			} else {
				// 检查是否为支持推送的终端类型
				if _, ok := s.pushTerminal[client.PlatformID]; ok {
					result.OnlinePush = true
				}
			}
		} else {
			// iOS后台模式不推送
			userPlatform.ResultCode = int64(servererrs.ErrIOSBackgroundPushErr.Code())
		}
		result.Resp = append(result.Resp, userPlatform)
	}
	return result
}

// SuperGroupOnlineBatchPushOneMsg 超级群组在线批量推送消息
// 设计思路：
// 1. 使用原子计数器跟踪推送进度
// 2. 内存队列异步处理，提高并发性能
// 3. 通道机制收集推送结果
// 4. 支持上下文取消和超时处理
//
// 参数：
//   - ctx: 上下文信息
//   - req: 批量推送请求，包含用户列表和消息
//
// 返回值：
//   - *msggateway.OnlineBatchPushOneMsgResp: 批量推送结果
//   - error: 错误信息
func (s *Server) SuperGroupOnlineBatchPushOneMsg(ctx context.Context, req *msggateway.OnlineBatchPushOneMsgReq) (*msggateway.OnlineBatchPushOneMsgResp, error) {
	if len(req.PushToUserIDs) == 0 {
		return &msggateway.OnlineBatchPushOneMsgResp{}, nil
	}

	// 创建结果收集通道
	ch := make(chan *msggateway.SingleMsgToUserResults, len(req.PushToUserIDs))
	var count atomic.Int64
	count.Add(int64(len(req.PushToUserIDs)))

	// 为每个用户创建推送任务
	for i := range req.PushToUserIDs {
		userID := req.PushToUserIDs[i]
		err := s.queue.PushCtx(ctx, func() {
			// 执行推送并发送结果
			ch <- s.pushToUser(ctx, userID, req.MsgData)
			if count.Add(-1) == 0 {
				close(ch) // 所有任务完成，关闭通道
			}
		})
		if err != nil {
			// 任务入队失败，直接返回失败结果
			if count.Add(-1) == 0 {
				close(ch)
			}
			log.ZError(ctx, "pushToUser MemoryQueue failed", err, "userID", userID)
			ch <- &msggateway.SingleMsgToUserResults{
				UserID: userID,
			}
		}
	}

	// 收集推送结果
	resp := &msggateway.OnlineBatchPushOneMsgResp{
		SinglePushResult: make([]*msggateway.SingleMsgToUserResults, 0, len(req.PushToUserIDs)),
	}

	for {
		select {
		case <-ctx.Done():
			// 上下文取消，处理未完成的用户
			log.ZError(ctx, "SuperGroupOnlineBatchPushOneMsg ctx done", context.Cause(ctx))
			userIDSet := datautil.SliceSet(req.PushToUserIDs)
			for _, results := range resp.SinglePushResult {
				delete(userIDSet, results.UserID)
			}
			// 为未处理的用户添加默认结果
			for userID := range userIDSet {
				resp.SinglePushResult = append(resp.SinglePushResult, &msggateway.SingleMsgToUserResults{
					UserID: userID,
				})
			}
			return resp, nil
		case res, ok := <-ch:
			if !ok {
				// 通道关闭，所有结果已收集完成
				return resp, nil
			}
			resp.SinglePushResult = append(resp.SinglePushResult, res)
		}
	}
}

// KickUserOffline 踢用户下线
// 设计思路：
// 1. 根据用户ID和平台ID精确定位连接
// 2. 遍历匹配的连接执行踢下线操作
// 3. 记录踢下线操作的日志信息
// 4. 支持批量踢下线处理
//
// 参数：
//   - ctx: 上下文信息
//   - req: 踢下线请求，包含用户列表和平台ID
//
// 返回值：
//   - *msggateway.KickUserOfflineResp: 踢下线响应
//   - error: 错误信息
func (s *Server) KickUserOffline(ctx context.Context, req *msggateway.KickUserOfflineReq) (*msggateway.KickUserOfflineResp, error) {
	for _, v := range req.KickUserIDList {
		// 获取指定用户在指定平台的连接
		clients, _, ok := s.LongConnServer.GetUserPlatformCons(v, int(req.PlatformID))
		if !ok {
			log.ZDebug(ctx, "conn not exist", "userID", v, "platformID", req.PlatformID)
			continue
		}

		// 踢下线每个匹配的连接
		for _, client := range clients {
			log.ZDebug(ctx, "kick user offline", "userID", v, "platformID", req.PlatformID, "client", client)
			if err := client.longConnServer.KickUserConn(client); err != nil {
				log.ZWarn(ctx, "kick user offline failed", err, "userID", v, "platformID", req.PlatformID)
			}
		}
		continue
	}

	return &msggateway.KickUserOfflineResp{}, nil
}

// MultiTerminalLoginCheck 多终端登录检查
// 设计思路：
// 1. 检查用户在指定平台是否已有连接
// 2. 创建虚拟客户端用于踢下线处理
// 3. 设置踢下线处理器信息
// 4. 支持集群间的多终端登录策略协调
//
// 应用场景：
// - 新连接建立时的多端登录检查
// - 集群节点间的登录状态同步
// - 多端登录策略的执行协调
//
// 参数：
//   - ctx: 上下文信息
//   - req: 多终端登录检查请求
//
// 返回值：
//   - *msggateway.MultiTerminalLoginCheckResp: 检查响应
//   - error: 错误信息
func (s *Server) MultiTerminalLoginCheck(ctx context.Context, req *msggateway.MultiTerminalLoginCheckReq) (*msggateway.MultiTerminalLoginCheckResp, error) {
	// 检查用户在指定平台是否已有连接
	if oldClients, userOK, clientOK := s.LongConnServer.GetUserPlatformCons(req.UserID, int(req.PlatformID)); userOK {
		// 创建临时上下文和虚拟客户端
		tempUserCtx := newTempContext()
		tempUserCtx.SetToken(req.Token)
		tempUserCtx.SetOperationID(mcontext.GetOperationID(ctx))

		client := &Client{}
		client.ctx = tempUserCtx
		client.UserID = req.UserID
		client.PlatformID = int(req.PlatformID)

		// 设置踢下线处理器信息
		i := &kickHandler{
			clientOK:   clientOK,
			oldClients: oldClients,
			newClient:  client,
		}
		s.LongConnServer.SetKickHandlerInfo(i)
	}
	return &msggateway.MultiTerminalLoginCheckResp{}, nil
}
