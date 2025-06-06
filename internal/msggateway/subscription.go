package msggateway

import (
	"context"
	"sync"

	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/utils/datautil"
	"google.golang.org/protobuf/proto"
)

// subscriberUserOnlineStatusChanges 处理用户在线状态变更的订阅通知
// 设计思路：
// 1. 检查用户状态变更是否影响订阅者
// 2. 如果有订阅者关注该用户，则推送状态变更通知
// 3. 支持多平台状态变更的批量通知
// 参数：
//   - ctx: 上下文信息
//   - userID: 状态发生变更的用户ID
//   - platformIDs: 变更的平台ID列表
func (ws *WsServer) subscriberUserOnlineStatusChanges(ctx context.Context, userID string, platformIDs []int32) {
	// 检查是否有客户端订阅了该用户的状态变更
	if ws.clients.RecvSubChange(userID, platformIDs) {
		log.ZDebug(ctx, "gateway receive subscription message and go back online", "userID", userID, "platformIDs", platformIDs)
	} else {
		log.ZDebug(ctx, "gateway ignore user online status changes", "userID", userID, "platformIDs", platformIDs)
	}

	// 向所有订阅者推送该用户的状态变更通知
	ws.pushUserIDOnlineStatus(ctx, userID, platformIDs)
}

// SubUserOnlineStatus 处理用户在线状态订阅请求
// 设计思路：
// 1. 解析客户端的订阅请求（订阅/取消订阅用户列表）
// 2. 更新订阅关系映射
// 3. 对于新订阅的用户，立即返回其当前在线状态
// 4. 返回订阅结果给客户端
// 参数：
//   - ctx: 上下文信息
//   - client: 发起订阅的客户端
//   - data: 订阅请求数据
//
// 返回值：
//   - []byte: 序列化后的订阅结果
//   - error: 错误信息
func (ws *WsServer) SubUserOnlineStatus(ctx context.Context, client *Client, data *Req) ([]byte, error) {
	var sub sdkws.SubUserOnlineStatus
	// 反序列化订阅请求
	if err := proto.Unmarshal(data.Data, &sub); err != nil {
		return nil, err
	}

	// 更新客户端的订阅关系
	// sub.SubscribeUserID: 要订阅的用户列表
	// sub.UnsubscribeUserID: 要取消订阅的用户列表
	ws.subscription.Sub(client, sub.SubscribeUserID, sub.UnsubscribeUserID)

	// 构建订阅响应
	var resp sdkws.SubUserOnlineStatusTips
	if len(sub.SubscribeUserID) > 0 {
		resp.Subscribers = make([]*sdkws.SubUserOnlineStatusElem, 0, len(sub.SubscribeUserID))

		// 为每个新订阅的用户获取当前在线状态
		for _, userID := range sub.SubscribeUserID {
			platformIDs, err := ws.online.GetUserOnlinePlatform(ctx, userID)
			if err != nil {
				return nil, err
			}
			resp.Subscribers = append(resp.Subscribers, &sdkws.SubUserOnlineStatusElem{
				UserID:            userID,
				OnlinePlatformIDs: platformIDs,
			})
		}
	}
	return proto.Marshal(&resp)
}

// newSubscription 创建新的订阅管理器
func newSubscription() *Subscription {
	return &Subscription{
		userIDs: make(map[string]*subClient),
	}
}

// subClient 订阅某个用户的客户端集合
// 设计思路：使用 map 存储客户端连接，key 为连接地址，便于快速查找和删除
type subClient struct {
	clients map[string]*Client // key: 客户端连接地址, value: 客户端连接
}

// Subscription 订阅管理器
// 设计思路：
// 1. 维护用户ID到订阅客户端的映射关系
// 2. 支持多个客户端同时订阅同一个用户
// 3. 使用读写锁保证并发安全
// 4. 客户端断开时自动清理订阅关系
type Subscription struct {
	lock    sync.RWMutex          // 读写锁，保护并发访问
	userIDs map[string]*subClient // 用户ID -> 订阅该用户的客户端集合
}

// DelClient 删除客户端的所有订阅关系
// 设计思路：
// 1. 获取客户端订阅的所有用户ID
// 2. 从客户端的订阅列表中移除
// 3. 从全局订阅映射中移除该客户端
// 4. 如果某个用户没有任何订阅者，则删除该用户的映射
// 参数：
//   - client: 要删除订阅关系的客户端
func (s *Subscription) DelClient(client *Client) {
	// 获取客户端订阅的所有用户ID
	client.subLock.Lock()
	userIDs := datautil.Keys(client.subUserIDs)
	for _, userID := range userIDs {
		delete(client.subUserIDs, userID)
	}
	client.subLock.Unlock()

	if len(userIDs) == 0 {
		return // 客户端没有订阅任何用户
	}

	// 从全局订阅映射中移除该客户端
	addr := client.ctx.GetRemoteAddr()
	s.lock.Lock()
	defer s.lock.Unlock()

	for _, userID := range userIDs {
		sub, ok := s.userIDs[userID]
		if !ok {
			continue
		}
		delete(sub.clients, addr)

		// 如果该用户没有任何订阅者，删除该用户的映射
		if len(sub.clients) == 0 {
			delete(s.userIDs, userID)
		}
	}
}

// GetClient 获取订阅指定用户的所有客户端
// 参数：
//   - userID: 用户ID
//
// 返回值：
//   - []*Client: 订阅该用户的客户端列表
func (s *Subscription) GetClient(userID string) []*Client {
	s.lock.RLock()
	defer s.lock.RUnlock()

	cs, ok := s.userIDs[userID]
	if !ok {
		return nil
	}

	// 复制客户端列表，避免返回内部引用
	clients := make([]*Client, 0, len(cs.clients))
	for _, client := range cs.clients {
		clients = append(clients, client)
	}
	return clients
}

// Sub 更新客户端的订阅关系
// 设计思路：
// 1. 处理取消订阅：从客户端和全局映射中移除
// 2. 处理新增订阅：添加到客户端和全局映射中
// 3. 优化：如果同时取消和订阅同一个用户，则忽略操作
// 4. 使用客户端连接地址作为唯一标识
// 参数：
//   - client: 要更新订阅关系的客户端
//   - addUserIDs: 要新增订阅的用户ID列表
//   - delUserIDs: 要取消订阅的用户ID列表
func (s *Subscription) Sub(client *Client, addUserIDs, delUserIDs []string) {
	if len(addUserIDs)+len(delUserIDs) == 0 {
		return // 没有订阅变更
	}

	var (
		del = make(map[string]struct{}) // 要删除的订阅
		add = make(map[string]struct{}) // 要添加的订阅
	)

	// 更新客户端的订阅列表
	client.subLock.Lock()

	// 处理取消订阅
	for _, userID := range delUserIDs {
		if _, ok := client.subUserIDs[userID]; !ok {
			continue // 客户端未订阅该用户
		}
		del[userID] = struct{}{}
		delete(client.subUserIDs, userID)
	}

	// 处理新增订阅
	for _, userID := range addUserIDs {
		delete(del, userID) // 如果同时取消和订阅同一用户，则忽略取消操作
		if _, ok := client.subUserIDs[userID]; ok {
			continue // 客户端已订阅该用户
		}
		client.subUserIDs[userID] = struct{}{}
		add[userID] = struct{}{}
	}

	client.subLock.Unlock()

	if len(del)+len(add) == 0 {
		return // 没有实际的订阅变更
	}

	// 更新全局订阅映射
	addr := client.ctx.GetRemoteAddr()
	s.lock.Lock()
	defer s.lock.Unlock()

	// 处理取消订阅
	for userID := range del {
		sub, ok := s.userIDs[userID]
		if !ok {
			continue
		}
		delete(sub.clients, addr)

		// 如果该用户没有任何订阅者，删除该用户的映射
		if len(sub.clients) == 0 {
			delete(s.userIDs, userID)
		}
	}

	// 处理新增订阅
	for userID := range add {
		sub, ok := s.userIDs[userID]
		if !ok {
			// 创建新的订阅客户端集合
			sub = &subClient{clients: make(map[string]*Client)}
			s.userIDs[userID] = sub
		}
		sub.clients[addr] = client
	}
}

// pushUserIDOnlineStatus 向订阅者推送用户在线状态变更
// 设计思路：
// 1. 获取订阅该用户的所有客户端
// 2. 构建状态变更通知消息
// 3. 向每个订阅客户端推送通知
// 4. 记录推送失败的情况
// 参数：
//   - ctx: 上下文信息
//   - userID: 状态发生变更的用户ID
//   - platformIDs: 变更的平台ID列表
func (ws *WsServer) pushUserIDOnlineStatus(ctx context.Context, userID string, platformIDs []int32) {
	// 获取订阅该用户的所有客户端
	clients := ws.subscription.GetClient(userID)
	if len(clients) == 0 {
		return // 没有订阅者
	}

	// 构建状态变更通知消息
	onlineStatus, err := proto.Marshal(&sdkws.SubUserOnlineStatusTips{
		Subscribers: []*sdkws.SubUserOnlineStatusElem{{
			UserID:            userID,
			OnlinePlatformIDs: platformIDs,
		}},
	})
	if err != nil {
		log.ZError(ctx, "pushUserIDOnlineStatus json.Marshal", err)
		return
	}

	// 向每个订阅客户端推送状态变更通知
	for _, client := range clients {
		if err := client.PushUserOnlineStatus(onlineStatus); err != nil {
			log.ZError(ctx, "UserSubscribeOnlineStatusNotification push failed", err,
				"userID", client.UserID,
				"platformID", client.PlatformID,
				"changeUserID", userID,
				"changePlatformID", platformIDs)
		}
	}
}
