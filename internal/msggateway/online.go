package msggateway

import (
	"context"
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache/cachekey"
	pbuser "github.com/openimsdk/protocol/user"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
	"github.com/openimsdk/tools/utils/datautil"
)

// ChangeOnlineStatus 用户在线状态变更处理器
// 这是整个在线状态系统的核心调度器，负责：
// 1. 定时续约用户在线状态，防止缓存过期
// 2. 实时处理用户状态变更事件
// 3. 批量合并状态更新请求，提高性能
// 4. 并发处理状态更新，避免阻塞
// 5. 触发 webhook 回调通知外部系统
//
// 设计思路：
// - 使用多个并发 goroutine 处理状态更新，避免单点瓶颈
// - 采用批量合并策略，减少网络请求次数
// - 基于用户ID哈希分片，保证同一用户的操作串行化
// - 定时器机制确保状态及时同步和续约
//
// 参数：
//   - concurrent: 并发处理的 goroutine 数量，最小为1
func (ws *WsServer) ChangeOnlineStatus(concurrent int) {
	// 并发数最小为1，确保至少有一个处理协程
	if concurrent < 1 {
		concurrent = 1
	}

	// 续约时间设置为缓存过期时间的1/3，确保及时续约
	// 这样设计可以在缓存过期前有足够的时间进行多次续约尝试
	const renewalTime = cachekey.OnlineExpire / 3
	//const renewalTime = time.Second * 10  // 用于测试的较短间隔
	renewalTicker := time.NewTicker(renewalTime)

	// 为每个并发处理器创建独立的请求通道，避免竞争
	// 通道缓冲大小为64，平衡内存使用和处理延迟
	requestChs := make([]chan *pbuser.SetUserOnlineStatusReq, concurrent)
	// 为每个处理器创建状态变更缓冲区，用于批量合并
	changeStatus := make([][]UserState, concurrent)

	// 初始化每个并发处理器的通道和缓冲区
	for i := 0; i < concurrent; i++ {
		requestChs[i] = make(chan *pbuser.SetUserOnlineStatusReq, 64)
		changeStatus[i] = make([]UserState, 0, 100) // 预分配容量，减少内存重分配
	}

	// 合并定时器：每秒钟强制推送一次积累的状态变更
	// 确保即使未达到批次大小，状态也能及时同步
	mergeTicker := time.NewTicker(time.Second)

	// 本地状态到 protobuf 格式的转换函数
	local2pb := func(u UserState) *pbuser.UserOnlineStatus {
		return &pbuser.UserOnlineStatus{
			UserID:  u.UserID,
			Online:  u.Online,
			Offline: u.Offline,
		}
	}

	// 随机数种子，用于用户ID哈希分片，避免热点
	rNum := rand.Uint64()

	// pushUserState 将用户状态变更推送到对应的处理器
	// 设计思路：
	// 1. 基于用户ID的MD5哈希进行分片，确保同一用户的操作顺序性
	// 2. 使用随机数增加哈希的随机性，避免热点用户集中
	// 3. 当缓冲区满时立即发送，实现动态批量处理
	pushUserState := func(us ...UserState) {
		for _, u := range us {
			// 计算用户ID的MD5哈希，用于分片
			sum := md5.Sum([]byte(u.UserID))
			// 结合随机数和哈希值，计算分片索引
			i := (binary.BigEndian.Uint64(sum[:]) + rNum) % uint64(concurrent)

			// 添加到对应分片的缓冲区
			changeStatus[i] = append(changeStatus[i], u)
			status := changeStatus[i]

			// 当缓冲区达到容量上限时，立即发送批量请求
			if len(status) == cap(status) {
				req := &pbuser.SetUserOnlineStatusReq{
					Status: datautil.Slice(status, local2pb),
				}
				changeStatus[i] = status[:0] // 重置缓冲区，复用底层数组
				select {
				case requestChs[i] <- req:
					// 成功发送到处理通道
				default:
					// 处理通道已满，记录警告日志
					log.ZError(context.Background(), "user online processing is too slow", nil)
				}
			}
		}
	}

	// pushAllUserState 强制推送所有缓冲区中的状态变更
	// 用于定时器触发的批量推送，确保状态及时同步
	pushAllUserState := func() {
		for i, status := range changeStatus {
			if len(status) == 0 {
				continue // 跳过空缓冲区
			}
			req := &pbuser.SetUserOnlineStatusReq{
				Status: datautil.Slice(status, local2pb),
			}
			changeStatus[i] = status[:0] // 重置缓冲区
			select {
			case requestChs[i] <- req:
				// 成功发送
			default:
				// 通道阻塞，记录警告
				log.ZError(context.Background(), "user online processing is too slow", nil)
			}
		}
	}

	// 原子计数器，用于生成唯一的操作ID
	var count atomic.Int64
	// 操作ID前缀，包含进程ID，便于分布式环境下的日志追踪
	operationIDPrefix := fmt.Sprintf("p_%d_", os.Getpid())

	// doRequest 执行具体的状态更新请求
	// 设计思路：
	// 1. 为每个请求生成唯一的操作ID，便于日志追踪
	// 2. 设置超时时间，避免长时间阻塞
	// 3. 调用用户服务更新状态
	// 4. 触发相应的 webhook 回调通知
	doRequest := func(req *pbuser.SetUserOnlineStatusReq) {
		// 生成唯一操作ID，便于日志追踪和问题排查
		opIdCtx := mcontext.SetOperationID(context.Background(), operationIDPrefix+strconv.FormatInt(count.Add(1), 10))
		// 设置5秒超时，避免长时间阻塞
		ctx, cancel := context.WithTimeout(opIdCtx, time.Second*5)
		defer cancel()

		// 调用用户服务更新在线状态
		if err := ws.userClient.SetUserOnlineStatus(ctx, req); err != nil {
			log.ZError(ctx, "update user online status", err)
		}

		// 处理状态变更的 webhook 回调
		for _, ss := range req.Status {
			// 处理上线事件的 webhook
			for _, online := range ss.Online {
				// 获取客户端连接信息，判断是否为后台模式
				client, _, _ := ws.clients.Get(ss.UserID, int(online))
				back := false
				if len(client) > 0 {
					back = client[0].IsBackground
				}
				// 触发用户上线 webhook
				ws.webhookAfterUserOnline(ctx, &ws.msgGatewayConfig.WebhooksConfig.AfterUserOnline, ss.UserID, int(online), back, ss.ConnID)
			}
			// 处理下线事件的 webhook
			for _, offline := range ss.Offline {
				ws.webhookAfterUserOffline(ctx, &ws.msgGatewayConfig.WebhooksConfig.AfterUserOffline, ss.UserID, int(offline), ss.ConnID)
			}
		}
	}

	// 启动并发处理协程
	// 每个协程独立处理一个通道的请求，实现并发处理
	for i := 0; i < concurrent; i++ {
		go func(ch <-chan *pbuser.SetUserOnlineStatusReq) {
			// 持续处理通道中的请求，直到通道关闭
			for req := range ch {
				doRequest(req)
			}
		}(requestChs[i])
	}

	// 主事件循环：处理三种类型的事件
	// 1. 定时合并推送
	// 2. 定时续约
	// 3. 实时状态变更
	for {
		select {
		case <-mergeTicker.C:
			// 定时合并推送：每秒强制推送所有积累的状态变更
			// 确保状态更新的实时性，避免因批次未满而延迟
			pushAllUserState()

		case now := <-renewalTicker.C:
			// 定时续约：定期续约在线用户状态，防止缓存过期
			// 设计思路：
			// - 计算续约截止时间：当前时间减去续约间隔
			// - 获取需要续约的用户列表
			// - 批量更新这些用户的在线状态
			deadline := now.Add(-cachekey.OnlineExpire / 3)
			users := ws.clients.GetAllUserStatus(deadline, now)
			log.ZDebug(context.Background(), "renewal ticker", "deadline", deadline, "nowtime", now, "num", len(users), "users", users)
			pushUserState(users...)

		case state := <-ws.clients.UserState():
			// 实时状态变更：处理来自客户端连接管理器的状态变更事件
			// 这是最重要的实时处理路径，确保用户状态变更能立即同步
			log.ZDebug(context.Background(), "OnlineCache user online change", "userID", state.UserID, "online", state.Online, "offline", state.Offline)
			pushUserState(state)
		}
	}
}
