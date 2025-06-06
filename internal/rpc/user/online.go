package user

import (
	"context"

	"github.com/openimsdk/tools/utils/datautil"

	"github.com/openimsdk/protocol/constant"
	pbuser "github.com/openimsdk/protocol/user"
)

// getUserOnlineStatus 获取指定用户的在线状态
// 设计思路：
// 1. 通过底层存储接口获取用户在各个平台的在线状态
// 2. 如果用户在任何平台在线，则整体状态为在线(Online)
// 3. 如果用户在所有平台都离线，则整体状态为离线(Offline)
// 参数：
//   - ctx: 上下文信息
//   - userID: 用户唯一标识
//
// 返回值：
//   - *pbuser.OnlineStatus: 包含用户ID、整体状态和平台列表的在线状态对象
//   - error: 错误信息
func (s *userServer) getUserOnlineStatus(ctx context.Context, userID string) (*pbuser.OnlineStatus, error) {
	// 从底层存储获取用户在线的平台ID列表
	platformIDs, err := s.online.GetOnline(ctx, userID)
	if err != nil {
		return nil, err
	}

	// 构建在线状态响应对象
	status := pbuser.OnlineStatus{
		UserID:      userID,
		PlatformIDs: platformIDs,
	}

	// 状态判断逻辑：只要有任何平台在线，整体状态就为在线
	if len(platformIDs) > 0 {
		status.Status = constant.Online
	} else {
		status.Status = constant.Offline
	}
	return &status, nil
}

// getUsersOnlineStatus 批量获取多个用户的在线状态
// 设计思路：
// 1. 遍历用户ID列表，逐个调用getUserOnlineStatus获取状态
// 2. 将所有用户的状态聚合到一个列表中返回
// 参数：
//   - ctx: 上下文信息
//   - userIDs: 用户ID列表
//
// 返回值：
//   - []*pbuser.OnlineStatus: 用户在线状态列表
//   - error: 错误信息
func (s *userServer) getUsersOnlineStatus(ctx context.Context, userIDs []string) ([]*pbuser.OnlineStatus, error) {
	res := make([]*pbuser.OnlineStatus, 0, len(userIDs))
	for _, userID := range userIDs {
		status, err := s.getUserOnlineStatus(ctx, userID)
		if err != nil {
			return nil, err
		}
		res = append(res, status)
	}
	return res, nil
}

// SubscribeOrCancelUsersStatus 订阅或取消订阅用户状态变更通知
// 设计思路：
// 1. 目前为空实现，预留接口用于后续实现用户状态变更的订阅机制
// 2. 可用于实现实时状态推送、好友在线提醒等功能
// 参数：
//   - ctx: 上下文信息
//   - req: 订阅请求，包含要订阅的用户列表和操作类型
//
// 返回值：
//   - *pbuser.SubscribeOrCancelUsersStatusResp: 订阅响应
//   - error: 错误信息
func (s *userServer) SubscribeOrCancelUsersStatus(ctx context.Context, req *pbuser.SubscribeOrCancelUsersStatusReq) (*pbuser.SubscribeOrCancelUsersStatusResp, error) {
	return &pbuser.SubscribeOrCancelUsersStatusResp{}, nil
}

// GetUserStatus 对外提供的获取用户状态接口
// 设计思路：
// 1. 封装getUsersOnlineStatus方法，提供标准的RPC接口
// 2. 支持批量查询，提高接口效率
// 参数：
//   - ctx: 上下文信息
//   - req: 包含用户ID列表的请求
//
// 返回值：
//   - *pbuser.GetUserStatusResp: 包含状态列表的响应
//   - error: 错误信息
func (s *userServer) GetUserStatus(ctx context.Context, req *pbuser.GetUserStatusReq) (*pbuser.GetUserStatusResp, error) {
	res, err := s.getUsersOnlineStatus(ctx, req.UserIDs)
	if err != nil {
		return nil, err
	}
	return &pbuser.GetUserStatusResp{StatusList: res}, nil
}

// SetUserStatus 设置用户在指定平台的在线状态
// 设计思路：
// 1. 根据状态类型(Online/Offline)分别处理在线和离线平台列表
// 2. 调用底层存储接口更新用户状态
// 3. 支持多平台并发在线的场景
// 参数：
//   - ctx: 上下文信息
//   - req: 包含用户ID、平台ID和状态的请求
//
// 返回值：
//   - *pbuser.SetUserStatusResp: 设置状态响应
//   - error: 错误信息
func (s *userServer) SetUserStatus(ctx context.Context, req *pbuser.SetUserStatusReq) (*pbuser.SetUserStatusResp, error) {
	var (
		online  []int32 // 在线平台列表
		offline []int32 // 离线平台列表
	)

	// 根据状态类型构建平台列表
	switch req.Status {
	case constant.Online:
		online = []int32{req.PlatformID}
	case constant.Offline:
		offline = []int32{req.PlatformID}
	}

	// 调用底层接口更新用户在线状态
	if err := s.online.SetUserOnline(ctx, req.UserID, online, offline); err != nil {
		return nil, err
	}
	return &pbuser.SetUserStatusResp{}, nil
}

// GetSubscribeUsersStatus 获取已订阅用户的状态信息
// 设计思路：
// 1. 目前为空实现，预留接口用于获取订阅用户的状态
// 2. 配合SubscribeOrCancelUsersStatus使用，实现状态订阅机制
// 参数：
//   - ctx: 上下文信息
//   - req: 获取订阅状态请求
//
// 返回值：
//   - *pbuser.GetSubscribeUsersStatusResp: 订阅用户状态响应
//   - error: 错误信息
func (s *userServer) GetSubscribeUsersStatus(ctx context.Context, req *pbuser.GetSubscribeUsersStatusReq) (*pbuser.GetSubscribeUsersStatusResp, error) {
	return &pbuser.GetSubscribeUsersStatusResp{}, nil
}

// SetUserOnlineStatus 批量设置多个用户的在线状态
// 设计思路：
// 1. 支持批量更新多个用户的状态，提高操作效率
// 2. 每个用户可以同时设置多个平台的在线和离线状态
// 3. 适用于系统维护、批量状态同步等场景
// 参数：
//   - ctx: 上下文信息
//   - req: 包含多个用户状态信息的请求
//
// 返回值：
//   - *pbuser.SetUserOnlineStatusResp: 批量设置响应
//   - error: 错误信息
func (s *userServer) SetUserOnlineStatus(ctx context.Context, req *pbuser.SetUserOnlineStatusReq) (*pbuser.SetUserOnlineStatusResp, error) {
	// 遍历每个用户的状态信息进行更新
	for _, status := range req.Status {
		if err := s.online.SetUserOnline(ctx, status.UserID, status.Online, status.Offline); err != nil {
			return nil, err
		}
	}
	return &pbuser.SetUserOnlineStatusResp{}, nil
}

// GetAllOnlineUsers 获取所有在线用户列表（支持分页）
// 设计思路：
// 1. 支持游标分页，避免一次性返回大量数据造成性能问题
// 2. 返回用户ID、状态和平台信息的完整映射
// 3. 适用于管理后台统计、系统监控等场景
// 参数：
//   - ctx: 上下文信息
//   - req: 包含游标信息的分页请求
//
// 返回值：
//   - *pbuser.GetAllOnlineUsersResp: 包含在线用户列表和下一页游标的响应
//   - error: 错误信息
func (s *userServer) GetAllOnlineUsers(ctx context.Context, req *pbuser.GetAllOnlineUsersReq) (*pbuser.GetAllOnlineUsersResp, error) {
	// 从底层存储获取在线用户映射和下一页游标
	resMap, nextCursor, err := s.online.GetAllOnlineUsers(ctx, req.Cursor)
	if err != nil {
		return nil, err
	}

	// 构建响应对象
	resp := &pbuser.GetAllOnlineUsersResp{
		StatusList: make([]*pbuser.OnlineStatus, 0, len(resMap)),
		NextCursor: nextCursor,
	}

	// 将映射转换为状态列表
	for userID, plats := range resMap {
		resp.StatusList = append(resp.StatusList, &pbuser.OnlineStatus{
			UserID: userID,
			// 使用工具函数判断状态：有平台在线则为在线，否则为离线
			Status:      int32(datautil.If(len(plats) > 0, constant.Online, constant.Offline)),
			PlatformIDs: plats,
		})
	}
	return resp, nil
}
