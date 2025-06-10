// Package relation 用户关系同步服务模块
//
// 本文件实现了OpenIM用户关系数据的增量同步机制，是保障多端数据一致性的核心组件。
// 当用户的好友关系发生变化时，系统通过版本控制和增量同步技术，
// 确保用户在不同设备上的好友数据保持一致。
//
// 核心功能：
// 1. 用户信息更新通知：当用户信息变更时，通知所有相关好友
// 2. 完整好友列表同步：获取用户的完整好友ID列表，支持哈希校验
// 3. 增量好友数据同步：基于版本号的增量同步，减少数据传输量
//
// 同步策略：
// - 版本化管理：每次数据变更都会产生新的版本号
// - 哈希校验：通过ID哈希快速判断数据是否变更
// - 增量传输：只传输变更的数据，提高同步效率
// - 多端一致性：确保用户在所有设备上的数据一致
//
// 技术特点：
// - 高性能：基于版本号的快速比较和增量传输
// - 可靠性：完整的错误处理和日志记录
// - 扩展性：支持大量用户的并发同步
// - 实时性：结合通知机制实现准实时同步
//
// 适用场景：
// - 用户多端登录场景
// - 好友关系频繁变更的场景
// - 网络不稳定环境下的数据同步
// - 大型IM系统的性能优化
package relation

import (
	"context"
	"slices"

	"github.com/openimsdk/open-im-server/v3/pkg/util/hashutil"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/log"

	"github.com/openimsdk/open-im-server/v3/internal/rpc/incrversion"
	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/protocol/relation"
)

// NotificationUserInfoUpdate 用户信息更新通知处理
//
// 当用户的基本信息（如昵称、头像等）发生变更时，此方法负责通知所有相关的好友，
// 确保好友端能够及时更新该用户的显示信息。这是一个异步处理的关键节点。
//
// 业务流程：
// 1. 查询该用户的所有好友ID列表
// 2. 为每个好友更新版本信息（用于增量同步）
// 3. 向每个好友发送用户信息更新通知
//
// 异步处理策略：
// - 使用内存队列进行异步处理，避免阻塞主流程
// - 使用context.WithoutCancel确保通知发送完成
// - 错误处理：记录日志但不影响主流程
//
// 参数说明：
// - req.UserID: 信息发生变更的用户ID
//
// 性能考虑：
// - 批量处理好友列表，减少数据库访问次数
// - 异步通知发送，提高响应速度
// - 错误隔离，单个通知失败不影响其他通知
//
// 同步机制：
// - 更新版本信息支持增量同步
// - 通知机制保证实时性
// - 多端数据一致性保障
func (s *friendServer) NotificationUserInfoUpdate(ctx context.Context, req *relation.NotificationUserInfoUpdateReq) (*relation.NotificationUserInfoUpdateResp, error) {
	// 1. 查询该用户的所有好友ID列表
	// FindFriendUserIDs方法获取与该用户有好友关系的所有用户ID
	// 这些用户需要接收到用户信息更新的通知
	userIDs, err := s.db.FindFriendUserIDs(ctx, req.UserID)
	if err != nil {
		return nil, err
	}

	// 2. 如果该用户有好友，则进行通知处理
	if len(userIDs) > 0 {
		// 构造好友ID列表，用于版本更新
		friendUserIDs := []string{req.UserID}

		// 创建一个不会被取消的上下文，确保异步操作能够完成
		// 即使客户端断开连接，通知处理也会继续执行
		noCancelCtx := context.WithoutCancel(ctx)

		// 3. 将通知处理任务加入异步队列
		// 使用内存队列进行异步处理，提高接口响应速度
		err := s.queue.PushCtx(ctx, func() {
			// 3.1 为每个好友更新版本信息
			// 这是增量同步机制的关键步骤
			for _, userID := range userIDs {
				// OwnerIncrVersion方法为指定用户的好友列表更新版本信息
				// model.VersionStateUpdate 标识这是一个更新操作
				if err := s.db.OwnerIncrVersion(noCancelCtx, userID, friendUserIDs, model.VersionStateUpdate); err != nil {
					// 如果版本更新失败，记录错误日志但继续处理其他用户
					// 这样确保部分失败不会影响整体通知流程
					log.ZError(ctx, "OwnerIncrVersion", err, "userID", userID, "friendUserIDs", friendUserIDs)
				}
			}

			// 3.2 向每个好友发送用户信息更新通知
			for _, userID := range userIDs {
				// FriendInfoUpdatedNotification方法发送实时通知
				// 第一个参数是信息变更的用户ID，第二个参数是要通知的好友ID
				s.notificationSender.FriendInfoUpdatedNotification(noCancelCtx, req.UserID, userID)
			}
		})

		// 4. 检查队列推送是否成功
		if err != nil {
			// 如果队列推送失败（如队列满了），记录错误日志
			// 但依然返回成功响应，因为这不是致命错误
			log.ZError(ctx, "NotificationUserInfoUpdate timeout", err, "userID", req.UserID)
		}
	}

	// 5. 返回成功响应
	return &relation.NotificationUserInfoUpdateResp{}, nil
}

// GetFullFriendUserIDs 获取完整的好友用户ID列表
//
// 返回指定用户的完整好友ID列表，支持哈希校验机制以提高同步效率。
// 客户端可以通过比较哈希值来判断好友列表是否发生变更，避免不必要的数据传输。
//
// 同步策略：
// 1. 获取用户的最新版本信息
// 2. 获取完整的好友ID列表
// 3. 计算好友ID列表的哈希值
// 4. 与客户端提供的哈希值进行比较
// 5. 如果哈希相同，返回空列表；否则返回完整列表
//
// 参数说明：
// - req.UserID: 要查询好友列表的用户ID
// - req.IdHash: 客户端当前的好友ID列表哈希值（可选）
//
// 返回数据：
// - Version: 当前好友列表的哈希值（版本标识）
// - VersionID: 版本记录的ID（用于增量同步）
// - Equal: 客户端哈希是否与服务端相同
// - UserIDs: 好友ID列表（如果哈希不同才返回）
//
// 性能优化：
// - 哈希比较：快速判断数据是否变更
// - 条件返回：相同数据不重复传输
// - 版本控制：支持后续的增量同步
//
// 安全控制：
// - 权限验证：确保用户只能查询自己的好友列表
// - 数据隔离：不同用户的数据完全隔离
func (s *friendServer) GetFullFriendUserIDs(ctx context.Context, req *relation.GetFullFriendUserIDsReq) (*relation.GetFullFriendUserIDsResp, error) {
	// 1. 权限验证：检查用户是否有权限查询指定用户的好友列表
	// CheckAccessV3确保用户只能查询自己的数据，或者管理员可以查询任何用户的数据
	if err := authverify.CheckAccessV3(ctx, req.UserID, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	// 2. 获取用户好友关系的最大版本信息
	// FindMaxFriendVersionCache方法从缓存中获取用户好友关系的最新版本信息
	// 版本信息用于支持增量同步机制
	vl, err := s.db.FindMaxFriendVersionCache(ctx, req.UserID)
	if err != nil {
		return nil, err
	}

	// 3. 获取用户的完整好友ID列表
	// FindFriendUserIDs方法查询该用户所有好友的用户ID
	userIDs, err := s.db.FindFriendUserIDs(ctx, req.UserID)
	if err != nil {
		return nil, err
	}

	// 4. 计算好友ID列表的哈希值
	// hashutil.IdHash方法基于用户ID列表计算哈希值
	// 哈希值用作数据版本的唯一标识
	idHash := hashutil.IdHash(userIDs)

	// 5. 比较客户端和服务端的哈希值
	// 如果哈希值相同，说明数据没有变更，客户端不需要重新获取数据
	if req.IdHash == idHash {
		userIDs = nil // 将用户ID列表设为空，减少数据传输
	}

	// 6. 构造并返回响应
	return &relation.GetFullFriendUserIDsResp{
		Version:   idHash,               // 当前好友列表的哈希值（版本标识）
		VersionID: vl.ID.Hex(),          // 版本记录的ID（MongoDB ObjectID转字符串）
		Equal:     req.IdHash == idHash, // 客户端哈希是否与服务端相同
		UserIDs:   userIDs,              // 好友ID列表（哈希不同时才返回）
	}, nil
}

// GetIncrementalFriends 获取增量好友数据
//
// 基于版本号获取用户好友关系的增量变更数据。这是实现高效数据同步的核心方法，
// 通过增量传输大大减少了网络传输量和客户端处理负担。
//
// 增量同步原理：
// 1. 客户端提供上次同步的版本号和版本ID
// 2. 服务端查询该版本之后的所有变更记录
// 3. 将变更记录分类为：删除、新增、更新
// 4. 获取变更好友的详细信息
// 5. 返回分类后的增量数据
//
// 版本管理：
// - 每次好友关系变更都会产生新的版本记录
// - 版本记录包含变更类型、用户ID、时间戳等信息
// - 支持版本回溯和数据恢复
//
// 参数说明：
// - req.UserID: 要同步数据的用户ID
// - req.VersionID: 客户端当前的版本ID
// - req.Version: 客户端当前的版本号
//
// 返回数据：
// - VersionID: 最新的版本ID
// - Version: 最新的版本号
// - Full: 是否为全量数据（首次同步或版本差异太大时）
// - Delete: 已删除的好友ID列表
// - Insert: 新增的好友信息列表
// - Update: 更新的好友信息列表
// - SortVersion: 排序版本号（用于好友列表排序）
//
// 性能优化：
// - 增量传输：只传输变更数据
// - 缓存利用：优先使用缓存数据
// - 批量处理：一次性处理多个变更
// - 排序优化：支持好友列表排序同步
//
// 容错机制：
// - 版本检查：防止版本冲突和数据不一致
// - 降级策略：异常情况下降级为全量同步
// - 错误处理：完善的错误信息和日志记录
func (s *friendServer) GetIncrementalFriends(ctx context.Context, req *relation.GetIncrementalFriendsReq) (*relation.GetIncrementalFriendsResp, error) {
	// 1. 权限验证：检查用户是否有权限获取指定用户的增量好友数据
	if err := authverify.CheckAccessV3(ctx, req.UserID, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	// 2. 初始化排序版本号
	// 排序版本号用于处理好友列表的排序变更，如置顶好友的顺序调整
	var sortVersion uint64

	// 3. 构造增量同步选项
	// incrversion.Option 是增量同步的核心配置，定义了同步的各种策略和回调函数
	opt := incrversion.Option[*sdkws.FriendInfo, relation.GetIncrementalFriendsResp]{
		Ctx:           ctx,           // 请求上下文
		VersionKey:    req.UserID,    // 版本键（用户ID）
		VersionID:     req.VersionID, // 客户端版本ID
		VersionNumber: req.Version,   // 客户端版本号

		// Version 回调函数：获取指定版本之后的变更记录
		Version: func(ctx context.Context, ownerUserID string, version uint, limit int) (*model.VersionLog, error) {
			// 3.1 查询用户好友关系的增量版本记录
			// FindFriendIncrVersion方法获取指定版本之后的所有变更记录
			vl, err := s.db.FindFriendIncrVersion(ctx, ownerUserID, version, limit)
			if err != nil {
				return nil, err
			}

			// 3.2 处理排序变更记录
			// 从版本日志中分离出排序变更记录，单独处理
			vl.Logs = slices.DeleteFunc(vl.Logs, func(elem model.VersionLogElem) bool {
				// 如果是排序变更记录（EID为VersionSortChangeID）
				if elem.EID == model.VersionSortChangeID {
					vl.LogLen--                        // 减少日志计数
					sortVersion = uint64(elem.Version) // 记录排序版本号
					return true                        // 从日志中删除此记录
				}
				return false
			})

			return vl, nil
		},

		// CacheMaxVersion 回调函数：获取最大版本号（通常从缓存获取）
		CacheMaxVersion: s.db.FindMaxFriendVersionCache,

		// Find 回调函数：根据用户ID列表获取好友详细信息
		Find: func(ctx context.Context, ids []string) ([]*sdkws.FriendInfo, error) {
			// 调用 getFriend 方法获取指定好友的详细信息
			// 包括好友的基本信息、备注、添加时间等
			return s.getFriend(ctx, req.UserID, ids)
		},

		// Resp 回调函数：构造最终的响应数据
		Resp: func(version *model.VersionLog, deleteIds []string, insertList, updateList []*sdkws.FriendInfo, full bool) *relation.GetIncrementalFriendsResp {
			return &relation.GetIncrementalFriendsResp{
				VersionID:   version.ID.Hex(),        // 最新版本ID（MongoDB ObjectID转字符串）
				Version:     uint64(version.Version), // 最新版本号
				Full:        full,                    // 是否为全量数据
				Delete:      deleteIds,               // 已删除的好友ID列表
				Insert:      insertList,              // 新增的好友信息列表
				Update:      updateList,              // 更新的好友信息列表
				SortVersion: sortVersion,             // 排序版本号
			}
		},
	}

	// 4. 执行增量同步构建
	// opt.Build() 方法执行完整的增量同步流程：
	// - 版本比较和验证
	// - 变更记录查询和分析
	// - 数据获取和转换
	// - 响应构造和返回
	return opt.Build()
}
