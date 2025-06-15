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

// Package push 在线推送模块
// 实现向在线用户推送消息的功能，支持多种服务发现机制
package push

import (
	"context"
	"sync"

	"github.com/openimsdk/protocol/msggateway"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/discovery"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/utils/datautil"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
)

// OnlinePusher 在线推送器接口
// 定义了在线推送的核心方法，支持不同的推送策略实现
type OnlinePusher interface {
	// GetConnsAndOnlinePush 获取连接并执行在线推送
	// 向指定用户列表推送消息，并返回推送结果
	// 参数:
	//   - ctx: 上下文
	//   - msg: 要推送的消息数据
	//   - pushToUserIDs: 目标用户ID列表
	// 返回:
	//   - wsResults: WebSocket推送结果列表
	//   - err: 推送失败时的错误信息
	GetConnsAndOnlinePush(ctx context.Context, msg *sdkws.MsgData,
		pushToUserIDs []string) (wsResults []*msggateway.SingleMsgToUserResults, err error)

	// GetOnlinePushFailedUserIDs 获取在线推送失败的用户ID列表
	// 分析推送结果，返回需要进行离线推送的用户列表
	// 参数:
	//   - ctx: 上下文
	//   - msg: 消息数据
	//   - wsResults: WebSocket推送结果列表
	//   - pushToUserIDs: 原始推送用户列表指针
	// 返回:
	//   - []string: 需要离线推送的用户ID列表
	GetOnlinePushFailedUserIDs(ctx context.Context, msg *sdkws.MsgData, wsResults []*msggateway.SingleMsgToUserResults,
		pushToUserIDs *[]string) []string
}

// emptyOnlinePusher 空在线推送器
// 当服务发现未启用时使用，不执行实际的推送操作
type emptyOnlinePusher struct{}

// newEmptyOnlinePusher 创建空在线推送器实例
func newEmptyOnlinePusher() *emptyOnlinePusher {
	return &emptyOnlinePusher{}
}

// GetConnsAndOnlinePush 空实现的在线推送
// 不执行实际推送，只记录日志
func (emptyOnlinePusher) GetConnsAndOnlinePush(ctx context.Context, msg *sdkws.MsgData,
	pushToUserIDs []string) (wsResults []*msggateway.SingleMsgToUserResults, err error) {
	log.ZInfo(ctx, "emptyOnlinePusher GetConnsAndOnlinePush", nil)
	return nil, nil
}

// GetOnlinePushFailedUserIDs 空实现的失败用户获取
// 不返回任何需要离线推送的用户
func (u emptyOnlinePusher) GetOnlinePushFailedUserIDs(ctx context.Context, msg *sdkws.MsgData,
	wsResults []*msggateway.SingleMsgToUserResults, pushToUserIDs *[]string) []string {
	log.ZInfo(ctx, "emptyOnlinePusher GetOnlinePushFailedUserIDs", nil)
	return nil
}

// NewOnlinePusher 在线推送器工厂函数
// 根据服务发现配置选择合适的在线推送器实现
// 参数:
//   - disCov: 服务发现注册中心
//   - config: 推送服务配置
//
// 返回:
//   - OnlinePusher: 在线推送器实例
func NewOnlinePusher(disCov discovery.SvcDiscoveryRegistry, config *Config) OnlinePusher {
	switch config.Discovery.Enable {
	case "k8s":
		// Kubernetes环境：使用一致性哈希策略
		return NewK8sStaticConsistentHash(disCov, config)
	case "zookeeper":
		// ZooKeeper环境：使用全节点推送策略
		return NewDefaultAllNode(disCov, config)
	case "etcd":
		// etcd环境：使用全节点推送策略
		return NewDefaultAllNode(disCov, config)
	default:
		// 未配置服务发现：使用空推送器
		return newEmptyOnlinePusher()
	}
}

// DefaultAllNode 默认全节点推送器
// 向所有消息网关节点广播推送消息，适用于传统服务发现环境
type DefaultAllNode struct {
	disCov discovery.SvcDiscoveryRegistry // 服务发现注册中心
	config *Config                        // 推送服务配置
}

// NewDefaultAllNode 创建默认全节点推送器实例
func NewDefaultAllNode(disCov discovery.SvcDiscoveryRegistry, config *Config) *DefaultAllNode {
	return &DefaultAllNode{disCov: disCov, config: config}
}

// GetConnsAndOnlinePush 执行全节点在线推送
// 向所有消息网关节点发送推送请求，由网关节点负责向连接的用户推送
func (d *DefaultAllNode) GetConnsAndOnlinePush(ctx context.Context, msg *sdkws.MsgData,
	pushToUserIDs []string) (wsResults []*msggateway.SingleMsgToUserResults, err error) {
	// 获取所有消息网关连接
	conns, err := d.disCov.GetConns(ctx, d.config.Share.RpcRegisterName.MessageGateway)
	if len(conns) == 0 {
		log.ZWarn(ctx, "get gateway conn 0 ", nil)
	} else {
		log.ZDebug(ctx, "get gateway conn", "conn length", len(conns))
	}

	if err != nil {
		return nil, err
	}

	var (
		mu         sync.Mutex                                                                         // 互斥锁，保护并发写入结果
		wg         = errgroup.Group{}                                                                 // 错误组，管理并发协程
		input      = &msggateway.OnlineBatchPushOneMsgReq{MsgData: msg, PushToUserIDs: pushToUserIDs} // 推送请求
		maxWorkers = d.config.RpcConfig.MaxConcurrentWorkers                                          // 最大并发工作者数量
	)

	// 设置最小并发数
	if maxWorkers < 3 {
		maxWorkers = 3
	}

	wg.SetLimit(maxWorkers)

	// 向每个消息网关节点并发发送推送请求
	for _, conn := range conns {
		conn := conn // 循环变量安全
		ctx := ctx
		wg.Go(func() error {
			msgClient := msggateway.NewMsgGatewayClient(conn)
			reply, err := msgClient.SuperGroupOnlineBatchPushOneMsg(ctx, input)
			if err != nil {
				log.ZError(ctx, "SuperGroupOnlineBatchPushOneMsg ", err, "req:", input.String())
				return nil // 不返回错误，避免影响其他节点的推送
			}

			log.ZDebug(ctx, "push result", "reply", reply)
			// 收集推送结果
			if reply != nil && reply.SinglePushResult != nil {
				mu.Lock()
				wsResults = append(wsResults, reply.SinglePushResult...)
				mu.Unlock()
			}

			return nil
		})
	}

	_ = wg.Wait()

	// 总是返回nil错误，即使部分节点推送失败
	return wsResults, nil
}

// GetOnlinePushFailedUserIDs 获取全节点推送中失败的用户ID
// 分析推送结果，排除发送者和推送成功的用户，返回需要离线推送的用户列表
func (d *DefaultAllNode) GetOnlinePushFailedUserIDs(_ context.Context, msg *sdkws.MsgData,
	wsResults []*msggateway.SingleMsgToUserResults, pushToUserIDs *[]string) []string {

	// 初始化成功推送的用户列表，包含消息发送者（发送者不需要离线推送）
	onlineSuccessUserIDs := []string{msg.SendID}
	for _, v := range wsResults {
		// 排除消息发送者
		if msg.SendID == v.UserID {
			continue
		}
		// 如果在线推送成功，加入成功列表
		if v.OnlinePush {
			onlineSuccessUserIDs = append(onlineSuccessUserIDs, v.UserID)
		}
	}

	// 计算需要离线推送的用户：原始推送列表 - 成功推送列表
	return datautil.SliceSub(*pushToUserIDs, onlineSuccessUserIDs)
}

// K8sStaticConsistentHash K8s静态一致性哈希推送器
// 在Kubernetes环境中使用一致性哈希算法，将用户分配到特定的网关节点
type K8sStaticConsistentHash struct {
	disCov discovery.SvcDiscoveryRegistry // 服务发现注册中心
	config *Config                        // 推送服务配置
}

// NewK8sStaticConsistentHash 创建K8s一致性哈希推送器实例
func NewK8sStaticConsistentHash(disCov discovery.SvcDiscoveryRegistry, config *Config) *K8sStaticConsistentHash {
	return &K8sStaticConsistentHash{disCov: disCov, config: config}
}

// GetConnsAndOnlinePush 执行一致性哈希在线推送
// 使用一致性哈希算法将用户分配到特定的网关节点，减少网络开销
func (k *K8sStaticConsistentHash) GetConnsAndOnlinePush(ctx context.Context, msg *sdkws.MsgData,
	pushToUserIDs []string) (wsResults []*msggateway.SingleMsgToUserResults, err error) {

	// 将用户按网关主机分组
	var usersHost = make(map[string][]string)
	for _, v := range pushToUserIDs {
		// 通过一致性哈希获取用户对应的网关主机
		tHost, err := k.disCov.GetUserIdHashGatewayHost(ctx, v)
		if err != nil {
			log.ZError(ctx, "get msg gateway hash error", err)
			return nil, err
		}
		// 将用户分组到对应的主机
		tUsers, tbl := usersHost[tHost]
		if tbl {
			tUsers = append(tUsers, v)
			usersHost[tHost] = tUsers
		} else {
			usersHost[tHost] = []string{v}
		}
	}
	log.ZDebug(ctx, "genUsers send hosts struct:", "usersHost", usersHost)

	// 将主机映射为连接对象
	var usersConns = make(map[grpc.ClientConnInterface][]string)
	for host, userIds := range usersHost {
		tconn, _ := k.disCov.GetConn(ctx, host)
		usersConns[tconn] = userIds
	}

	var (
		mu         sync.Mutex                                // 互斥锁，保护并发写入
		wg         = errgroup.Group{}                        // 错误组，管理并发
		maxWorkers = k.config.RpcConfig.MaxConcurrentWorkers // 最大并发数
	)

	if maxWorkers < 3 {
		maxWorkers = 3
	}
	wg.SetLimit(maxWorkers)

	// 向每个网关连接并发发送推送请求
	for conn, userIds := range usersConns {
		tcon := conn
		tuserIds := userIds
		wg.Go(func() error {
			input := &msggateway.OnlineBatchPushOneMsgReq{MsgData: msg, PushToUserIDs: tuserIds}
			msgClient := msggateway.NewMsgGatewayClient(tcon)
			reply, err := msgClient.SuperGroupOnlineBatchPushOneMsg(ctx, input)
			if err != nil {
				return nil // 不返回错误，避免影响其他连接
			}
			log.ZDebug(ctx, "push result", "reply", reply)
			// 收集推送结果
			if reply != nil && reply.SinglePushResult != nil {
				mu.Lock()
				wsResults = append(wsResults, reply.SinglePushResult...)
				mu.Unlock()
			}
			return nil
		})
	}
	_ = wg.Wait()
	return wsResults, nil
}

// GetOnlinePushFailedUserIDs 获取一致性哈希推送中失败的用户ID
// 直接从推送结果中筛选出推送失败的用户
func (k *K8sStaticConsistentHash) GetOnlinePushFailedUserIDs(_ context.Context, _ *sdkws.MsgData,
	wsResults []*msggateway.SingleMsgToUserResults, _ *[]string) []string {
	var needOfflinePushUserIDs []string
	for _, v := range wsResults {
		// 如果在线推送失败，需要进行离线推送
		if !v.OnlinePush {
			needOfflinePushUserIDs = append(needOfflinePushUserIDs, v.UserID)
		}
	}
	return needOfflinePushUserIDs
}
