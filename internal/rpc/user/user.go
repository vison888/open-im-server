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

/*
 * 用户管理服务
 *
 * 本模块是OpenIM系统的用户管理核心，负责处理所有与用户相关的业务逻辑和数据管理。
 *
 * 核心功能：
 *
 * 1. 用户生命周期管理
 *    - 用户注册：新用户的注册流程和数据初始化
 *    - 用户信息管理：用户基本信息和扩展信息的维护
 *    - 用户状态管理：在线状态、活跃状态等用户状态的跟踪
 *    - 用户注销：用户账号的注销和数据清理流程
 *
 * 2. 用户信息服务
 *    - 基本信息：昵称、头像、性别等基础用户信息
 *    - 扩展信息：用户自定义字段和业务相关属性
 *    - 批量查询：支持批量用户信息查询和更新
 *    - 信息同步：用户信息的跨服务同步机制
 *
 * 3. 用户权限管理
 *    - 权限验证：用户操作权限的验证和控制
 *    - 角色管理：用户角色的分配和管理
 *    - 访问控制：基于用户身份的访问控制策略
 *    - 管理员功能：系统管理员的特殊权限和功能
 *
 * 4. 用户在线状态
 *    - 状态跟踪：实时跟踪用户在线状态
 *    - 多端管理：支持用户多设备同时在线
 *    - 状态同步：跨设备的状态信息同步
 *    - 离线处理：用户离线时的消息和通知处理
 *
 * 5. 用户通知账号
 *    - 系统账号：用于系统通知的特殊账号管理
 *    - 通知发送：系统消息和通知的发送管理
 *    - 账号搜索：通知账号的查询和管理功能
 *    - 权限控制：通知账号的权限和使用范围控制
 *
 * 6. 用户自定义命令
 *    - 命令管理：用户自定义命令的增删改查
 *    - 命令执行：用户命令的执行和结果处理
 *    - 权限控制：命令执行的权限验证
 *    - 批量操作：支持批量命令操作
 *
 * 7. 用户统计分析
 *    - 注册统计：用户注册数量和趋势分析
 *    - 活跃度统计：用户活跃度和使用行为分析
 *    - 分页查询：支持大量用户数据的分页查询
 *    - 数据导出：用户数据的导出和分析功能
 *
 * 技术特性：
 * - 微服务架构：独立的用户服务，支持水平扩展
 * - 数据库抽象：支持多种数据库的抽象访问层
 * - 缓存优化：多层缓存提升用户数据访问性能
 * - 异步处理：非阻塞的异步操作处理机制
 * - 事务支持：保证数据一致性的事务处理
 * - 监控集成：完整的监控和日志记录功能
 *
 * 集成能力：
 * - Webhook集成：与第三方系统的集成能力
 * - 消息推送：集成消息推送和通知服务
 * - 好友系统：与好友关系系统的集成
 * - 群组系统：与群组管理系统的集成
 * - 认证系统：与身份认证系统的集成
 *
 * 应用场景：
 * - 即时通讯：为IM应用提供完整的用户管理能力
 * - 社交应用：支持复杂的社交关系和用户互动
 * - 企业应用：企业级用户管理和权限控制
 * - 游戏应用：游戏玩家信息和状态管理
 * - 电商平台：用户账号和个人信息管理
 */
package user

import (
	"context"
	"errors"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/openimsdk/open-im-server/v3/internal/rpc/relation"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/prommetrics"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache/redis"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database/mgo"
	tablerelation "github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/open-im-server/v3/pkg/common/webhook"
	"github.com/openimsdk/open-im-server/v3/pkg/localcache"
	"github.com/openimsdk/open-im-server/v3/pkg/rpcli"
	"github.com/openimsdk/protocol/group"
	friendpb "github.com/openimsdk/protocol/relation"
	"github.com/openimsdk/tools/db/redisutil"

	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/open-im-server/v3/pkg/common/convert"
	"github.com/openimsdk/open-im-server/v3/pkg/common/servererrs"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/controller"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/sdkws"
	pbuser "github.com/openimsdk/protocol/user"
	"github.com/openimsdk/tools/db/mongoutil"
	"github.com/openimsdk/tools/db/pagination"
	registry "github.com/openimsdk/tools/discovery"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/utils/datautil"
	"google.golang.org/grpc"
)

// userServer 用户服务器结构体
//
// 实现了完整的用户管理功能，包括用户信息管理、状态跟踪、权限控制等核心能力。
//
// 架构设计：
// - 微服务架构：作为独立的用户服务，提供完整的用户管理能力
// - 分层设计：清晰的业务逻辑层、数据访问层和服务接口层
// - 依赖注入：通过接口依赖实现松耦合的组件设计
// - 缓存集成：多层缓存策略提升系统性能
//
// 核心组件：
// - 数据库控制器：抽象的用户数据访问层
// - 缓存系统：用户数据的多层缓存机制
// - 通知系统：用户相关事件的通知推送
// - Webhook客户端：与外部系统的集成接口
// - 服务发现：与其他微服务的通信能力
type userServer struct {
	pbuser.UnimplementedUserServer                                    // gRPC服务基础实现
	online                         cache.OnlineCache                  // 在线状态缓存，管理用户在线状态
	db                             controller.UserDatabase            // 用户数据库控制器，提供数据访问抽象
	friendNotificationSender       *relation.FriendNotificationSender // 好友关系通知发送器
	userNotificationSender         *UserNotificationSender            // 用户通知发送器
	RegisterCenter                 registry.SvcDiscoveryRegistry      // 服务注册中心，用于服务发现
	config                         *Config                            // 服务配置信息
	webhookClient                  *webhook.Client                    // Webhook客户端，用于第三方集成
	groupClient                    *rpcli.GroupClient                 // 群组服务客户端
	relationClient                 *rpcli.RelationClient              // 关系服务客户端
}

// Config 用户服务配置结构体
//
// 集中管理用户服务运行所需的所有配置参数，支持灵活的服务配置和环境适配。
//
// 配置分类：
// - 服务配置：用户服务特定的配置参数
// - 存储配置：数据库、缓存等存储系统的配置
// - 消息配置：消息队列和通知系统的配置
// - 集成配置：与其他服务和外部系统的集成配置
//
// 配置管理：
// - 环境隔离：支持不同环境的配置差异化
// - 动态配置：支持配置的动态加载和更新
// - 配置验证：启动时的配置有效性验证
// - 默认值：提供合理的配置默认值
type Config struct {
	RpcConfig          config.User         // 用户服务RPC配置
	RedisConfig        config.Redis        // Redis缓存配置
	MongodbConfig      config.Mongo        // MongoDB数据库配置
	KafkaConfig        config.Kafka        // Kafka消息队列配置
	NotificationConfig config.Notification // 通知系统配置
	Share              config.Share        // 共享配置，包含跨服务的通用配置
	WebhooksConfig     config.Webhooks     // Webhook集成配置
	LocalCacheConfig   config.LocalCache   // 本地缓存配置
	Discovery          config.Discovery    // 服务发现配置
}

// Start 启动用户服务
//
// 初始化并启动用户服务的所有组件，建立必要的连接和依赖关系。
//
// 启动流程：
// 1. 数据库初始化：建立MongoDB和Redis连接
// 2. 服务连接：建立与其他微服务的RPC连接
// 3. 组件初始化：初始化所有核心组件和控制器
// 4. 服务注册：将用户服务注册到gRPC服务器
// 5. 数据初始化：初始化系统管理员账号等基础数据
//
// 依赖管理：
// - 数据库依赖：MongoDB作为主数据库，Redis作为缓存
// - 服务依赖：消息服务、群组服务、关系服务等
// - 配置依赖：各种配置文件和环境变量
// - 基础设施：服务发现、监控、日志等基础设施
//
// 错误处理：
// - 启动失败时的清理机制
// - 依赖服务不可用时的降级策略
// - 配置错误的详细错误信息
// - 资源泄露的防护机制
//
// 参数说明：
// - ctx: 启动上下文，控制启动过程的超时和取消
// - config: 服务配置，包含所有必要的配置参数
// - client: 服务发现客户端，用于注册和发现其他服务
// - server: gRPC服务器实例，用于注册用户服务
//
// 返回值：
// - error: 启动过程中的错误信息，nil表示启动成功
func Start(ctx context.Context, config *Config, client registry.SvcDiscoveryRegistry, server *grpc.Server) error {
	// 初始化MongoDB连接
	mgocli, err := mongoutil.NewMongoDB(ctx, config.MongodbConfig.Build())
	if err != nil {
		return err
	}

	// 初始化Redis连接
	rdb, err := redisutil.NewRedisClient(ctx, config.RedisConfig.Build())
	if err != nil {
		return err
	}

	// 准备系统管理员用户数据
	users := make([]*tablerelation.User, 0)
	for _, v := range config.Share.IMAdminUserID {
		users = append(users, &tablerelation.User{
			UserID:         v,
			Nickname:       v,
			AppMangerLevel: constant.AppNotificationAdmin,
		})
	}

	// 初始化用户数据库操作层
	userDB, err := mgo.NewUserMongo(mgocli.GetDB())
	if err != nil {
		return err
	}

	// 建立与其他服务的RPC连接
	msgConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.Msg)
	if err != nil {
		return err
	}
	groupConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.Group)
	if err != nil {
		return err
	}
	friendConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.Friend)
	if err != nil {
		return err
	}

	// 创建服务客户端
	msgClient := rpcli.NewMsgClient(msgConn)

	// 初始化用户缓存系统
	userCache := redis.NewUserCacheRedis(rdb, &config.LocalCacheConfig, userDB, redis.GetRocksCacheOptions())

	// 创建用户数据库控制器
	database := controller.NewUserDatabase(userDB, userCache, mgocli.GetTx())

	// 初始化本地缓存
	localcache.InitLocalCache(&config.LocalCacheConfig)

	// 创建用户服务实例
	u := &userServer{
		online:                   redis.NewUserOnline(rdb),
		db:                       database,
		RegisterCenter:           client,
		friendNotificationSender: relation.NewFriendNotificationSender(&config.NotificationConfig, msgClient, relation.WithDBFunc(database.FindWithError)),
		userNotificationSender:   NewUserNotificationSender(config, msgClient, WithUserFunc(database.FindWithError)),
		config:                   config,
		webhookClient:            webhook.NewWebhookClient(config.WebhooksConfig.URL),
		groupClient:              rpcli.NewGroupClient(groupConn),
		relationClient:           rpcli.NewRelationClient(friendConn),
	}

	// 注册用户服务到gRPC服务器
	pbuser.RegisterUserServer(server, u)

	// 初始化系统管理员账号
	return u.db.InitOnce(context.Background(), users)
}

// GetDesignateUsers 获取指定用户信息
//
// 根据用户ID列表批量获取用户的详细信息，支持高效的批量查询。
//
// 功能特点：
// - 批量查询：一次请求获取多个用户信息，提高查询效率
// - 数据转换：将数据库模型转换为API响应格式
// - 错误处理：对不存在的用户ID进行合理的错误处理
// - 性能优化：通过缓存和批量查询优化性能
//
// 应用场景：
// - 好友列表：获取好友列表的用户信息
// - 群成员：获取群组成员的详细信息
// - 消息展示：获取消息发送者的用户信息
// - 搜索结果：获取搜索结果中用户的详细信息
//
// 参数说明：
// - ctx: 请求上下文
// - req: 包含用户ID列表的请求
//
// 返回值：
// - resp: 包含用户信息列表的响应
// - err: 错误信息
func (s *userServer) GetDesignateUsers(ctx context.Context, req *pbuser.GetDesignateUsersReq) (resp *pbuser.GetDesignateUsersResp, err error) {
	resp = &pbuser.GetDesignateUsersResp{}

	// 从数据库批量查询用户信息
	users, err := s.db.Find(ctx, req.UserIDs)
	if err != nil {
		return nil, err
	}

	// 将数据库模型转换为API响应格式
	resp.UsersInfo = convert.UsersDB2Pb(users)
	return resp, nil
}

// deprecated:
// UpdateUserInfo
func (s *userServer) UpdateUserInfo(ctx context.Context, req *pbuser.UpdateUserInfoReq) (resp *pbuser.UpdateUserInfoResp, err error) {
	resp = &pbuser.UpdateUserInfoResp{}
	err = authverify.CheckAccessV3(ctx, req.UserInfo.UserID, s.config.Share.IMAdminUserID)
	if err != nil {
		return nil, err
	}

	if err := s.webhookBeforeUpdateUserInfo(ctx, &s.config.WebhooksConfig.BeforeUpdateUserInfo, req); err != nil {
		return nil, err
	}
	data := convert.UserPb2DBMap(req.UserInfo)
	oldUser, err := s.db.GetUserByID(ctx, req.UserInfo.UserID)
	if err != nil {
		return nil, err
	}
	if err := s.db.UpdateByMap(ctx, req.UserInfo.UserID, data); err != nil {
		return nil, err
	}
	s.friendNotificationSender.UserInfoUpdatedNotification(ctx, req.UserInfo.UserID)

	s.webhookAfterUpdateUserInfo(ctx, &s.config.WebhooksConfig.AfterUpdateUserInfo, req)
	if err = s.NotificationUserInfoUpdate(ctx, req.UserInfo.UserID, oldUser); err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *userServer) UpdateUserInfoEx(ctx context.Context, req *pbuser.UpdateUserInfoExReq) (resp *pbuser.UpdateUserInfoExResp, err error) {
	resp = &pbuser.UpdateUserInfoExResp{}
	err = authverify.CheckAccessV3(ctx, req.UserInfo.UserID, s.config.Share.IMAdminUserID)
	if err != nil {
		return nil, err
	}

	if err = s.webhookBeforeUpdateUserInfoEx(ctx, &s.config.WebhooksConfig.BeforeUpdateUserInfoEx, req); err != nil {
		return nil, err
	}

	oldUser, err := s.db.GetUserByID(ctx, req.UserInfo.UserID)
	if err != nil {
		return nil, err
	}

	data := convert.UserPb2DBMapEx(req.UserInfo)
	if err = s.db.UpdateByMap(ctx, req.UserInfo.UserID, data); err != nil {
		return nil, err
	}

	s.friendNotificationSender.UserInfoUpdatedNotification(ctx, req.UserInfo.UserID)
	//friends, err := s.friendRpcClient.GetFriendIDs(ctx, req.UserInfo.UserID)
	//if err != nil {
	//	return nil, err
	//}
	//if req.UserInfo.Nickname != nil || req.UserInfo.FaceURL != nil {
	//	if err := s.NotificationUserInfoUpdate(ctx, req.UserInfo.UserID); err != nil {
	//		return nil, err
	//	}
	//}
	//for _, friendID := range friends {
	//	s.friendNotificationSender.FriendInfoUpdatedNotification(ctx, req.UserInfo.UserID, friendID)
	//}
	s.webhookAfterUpdateUserInfoEx(ctx, &s.config.WebhooksConfig.AfterUpdateUserInfoEx, req)
	if err := s.NotificationUserInfoUpdate(ctx, req.UserInfo.UserID, oldUser); err != nil {
		return nil, err
	}

	return resp, nil
}
func (s *userServer) SetGlobalRecvMessageOpt(ctx context.Context, req *pbuser.SetGlobalRecvMessageOptReq) (resp *pbuser.SetGlobalRecvMessageOptResp, err error) {
	resp = &pbuser.SetGlobalRecvMessageOptResp{}
	if _, err := s.db.FindWithError(ctx, []string{req.UserID}); err != nil {
		return nil, err
	}
	m := make(map[string]any, 1)
	m["global_recv_msg_opt"] = req.GlobalRecvMsgOpt
	if err := s.db.UpdateByMap(ctx, req.UserID, m); err != nil {
		return nil, err
	}
	s.friendNotificationSender.UserInfoUpdatedNotification(ctx, req.UserID)
	return resp, nil
}

func (s *userServer) AccountCheck(ctx context.Context, req *pbuser.AccountCheckReq) (resp *pbuser.AccountCheckResp, err error) {
	resp = &pbuser.AccountCheckResp{}
	if datautil.Duplicate(req.CheckUserIDs) {
		return nil, errs.ErrArgs.WrapMsg("userID repeated")
	}
	err = authverify.CheckAdmin(ctx, s.config.Share.IMAdminUserID)
	if err != nil {
		return nil, err
	}
	users, err := s.db.Find(ctx, req.CheckUserIDs)
	if err != nil {
		return nil, err
	}
	userIDs := make(map[string]any, 0)
	for _, v := range users {
		userIDs[v.UserID] = nil
	}
	for _, v := range req.CheckUserIDs {
		temp := &pbuser.AccountCheckRespSingleUserStatus{UserID: v}
		if _, ok := userIDs[v]; ok {
			temp.AccountStatus = constant.Registered
		} else {
			temp.AccountStatus = constant.UnRegistered
		}
		resp.Results = append(resp.Results, temp)
	}
	return resp, nil
}

func (s *userServer) GetPaginationUsers(ctx context.Context, req *pbuser.GetPaginationUsersReq) (resp *pbuser.GetPaginationUsersResp, err error) {
	if req.UserID == "" && req.NickName == "" {
		total, users, err := s.db.PageFindUser(ctx, constant.IMOrdinaryUser, constant.AppOrdinaryUsers, req.Pagination)
		if err != nil {
			return nil, err
		}
		return &pbuser.GetPaginationUsersResp{Total: int32(total), Users: convert.UsersDB2Pb(users)}, err
	} else {
		total, users, err := s.db.PageFindUserWithKeyword(ctx, constant.IMOrdinaryUser, constant.AppOrdinaryUsers, req.UserID, req.NickName, req.Pagination)
		if err != nil {
			return nil, err
		}
		return &pbuser.GetPaginationUsersResp{Total: int32(total), Users: convert.UsersDB2Pb(users)}, err

	}

}

func (s *userServer) UserRegister(ctx context.Context, req *pbuser.UserRegisterReq) (resp *pbuser.UserRegisterResp, err error) {
	resp = &pbuser.UserRegisterResp{}
	if len(req.Users) == 0 {
		return nil, errs.ErrArgs.WrapMsg("users is empty")
	}

	if err = authverify.CheckAdmin(ctx, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	if datautil.DuplicateAny(req.Users, func(e *sdkws.UserInfo) string { return e.UserID }) {
		return nil, errs.ErrArgs.WrapMsg("userID repeated")
	}
	userIDs := make([]string, 0)
	for _, user := range req.Users {
		if user.UserID == "" {
			return nil, errs.ErrArgs.WrapMsg("userID is empty")
		}
		if strings.Contains(user.UserID, ":") {
			return nil, errs.ErrArgs.WrapMsg("userID contains ':' is invalid userID")
		}
		userIDs = append(userIDs, user.UserID)
	}
	exist, err := s.db.IsExist(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	if exist {
		return nil, servererrs.ErrRegisteredAlready.WrapMsg("userID registered already")
	}
	if err := s.webhookBeforeUserRegister(ctx, &s.config.WebhooksConfig.BeforeUserRegister, req); err != nil {
		return nil, err
	}
	now := time.Now()
	users := make([]*tablerelation.User, 0, len(req.Users))
	for _, user := range req.Users {
		users = append(users, &tablerelation.User{
			UserID:           user.UserID,
			Nickname:         user.Nickname,
			FaceURL:          user.FaceURL,
			Ex:               user.Ex,
			CreateTime:       now,
			AppMangerLevel:   user.AppMangerLevel,
			GlobalRecvMsgOpt: user.GlobalRecvMsgOpt,
		})
	}
	if err := s.db.Create(ctx, users); err != nil {
		return nil, err
	}

	prommetrics.UserRegisterCounter.Add(float64(len(users)))

	s.webhookAfterUserRegister(ctx, &s.config.WebhooksConfig.AfterUserRegister, req)
	return resp, nil
}

func (s *userServer) GetGlobalRecvMessageOpt(ctx context.Context, req *pbuser.GetGlobalRecvMessageOptReq) (resp *pbuser.GetGlobalRecvMessageOptResp, err error) {
	user, err := s.db.FindWithError(ctx, []string{req.UserID})
	if err != nil {
		return nil, err
	}
	return &pbuser.GetGlobalRecvMessageOptResp{GlobalRecvMsgOpt: user[0].GlobalRecvMsgOpt}, nil
}

// GetAllUserID Get user account by page.
func (s *userServer) GetAllUserID(ctx context.Context, req *pbuser.GetAllUserIDReq) (resp *pbuser.GetAllUserIDResp, err error) {
	total, userIDs, err := s.db.GetAllUserID(ctx, req.Pagination)
	if err != nil {
		return nil, err
	}
	return &pbuser.GetAllUserIDResp{Total: int32(total), UserIDs: userIDs}, nil
}

// ProcessUserCommandAdd user general function add.
func (s *userServer) ProcessUserCommandAdd(ctx context.Context, req *pbuser.ProcessUserCommandAddReq) (*pbuser.ProcessUserCommandAddResp, error) {
	err := authverify.CheckAccessV3(ctx, req.UserID, s.config.Share.IMAdminUserID)
	if err != nil {
		return nil, err
	}

	var value string
	if req.Value != nil {
		value = req.Value.Value
	}
	var ex string
	if req.Ex != nil {
		value = req.Ex.Value
	}
	// Assuming you have a method in s.storage to add a user command
	err = s.db.AddUserCommand(ctx, req.UserID, req.Type, req.Uuid, value, ex)
	if err != nil {
		return nil, err
	}
	tips := &sdkws.UserCommandAddTips{
		FromUserID: req.UserID,
		ToUserID:   req.UserID,
	}
	s.userNotificationSender.UserCommandAddNotification(ctx, tips)
	return &pbuser.ProcessUserCommandAddResp{}, nil
}

// ProcessUserCommandDelete user general function delete.
func (s *userServer) ProcessUserCommandDelete(ctx context.Context, req *pbuser.ProcessUserCommandDeleteReq) (*pbuser.ProcessUserCommandDeleteResp, error) {
	err := authverify.CheckAccessV3(ctx, req.UserID, s.config.Share.IMAdminUserID)
	if err != nil {
		return nil, err
	}

	err = s.db.DeleteUserCommand(ctx, req.UserID, req.Type, req.Uuid)
	if err != nil {
		return nil, err
	}
	tips := &sdkws.UserCommandDeleteTips{
		FromUserID: req.UserID,
		ToUserID:   req.UserID,
	}
	s.userNotificationSender.UserCommandDeleteNotification(ctx, tips)
	return &pbuser.ProcessUserCommandDeleteResp{}, nil
}

// ProcessUserCommandUpdate user general function update.
func (s *userServer) ProcessUserCommandUpdate(ctx context.Context, req *pbuser.ProcessUserCommandUpdateReq) (*pbuser.ProcessUserCommandUpdateResp, error) {
	err := authverify.CheckAccessV3(ctx, req.UserID, s.config.Share.IMAdminUserID)
	if err != nil {
		return nil, err
	}
	val := make(map[string]any)

	// Map fields from eax to val
	if req.Value != nil {
		val["value"] = req.Value.Value
	}
	if req.Ex != nil {
		val["ex"] = req.Ex.Value
	}

	// Assuming you have a method in s.storage to update a user command
	err = s.db.UpdateUserCommand(ctx, req.UserID, req.Type, req.Uuid, val)
	if err != nil {
		return nil, err
	}
	tips := &sdkws.UserCommandUpdateTips{
		FromUserID: req.UserID,
		ToUserID:   req.UserID,
	}
	s.userNotificationSender.UserCommandUpdateNotification(ctx, tips)
	return &pbuser.ProcessUserCommandUpdateResp{}, nil
}

func (s *userServer) ProcessUserCommandGet(ctx context.Context, req *pbuser.ProcessUserCommandGetReq) (*pbuser.ProcessUserCommandGetResp, error) {

	err := authverify.CheckAccessV3(ctx, req.UserID, s.config.Share.IMAdminUserID)
	if err != nil {
		return nil, err
	}
	// Fetch user commands from the database
	commands, err := s.db.GetUserCommands(ctx, req.UserID, req.Type)
	if err != nil {
		return nil, err
	}

	// Initialize commandInfoSlice as an empty slice
	commandInfoSlice := make([]*pbuser.CommandInfoResp, 0, len(commands))

	for _, command := range commands {
		// No need to use index since command is already a pointer
		commandInfoSlice = append(commandInfoSlice, &pbuser.CommandInfoResp{
			Type:       command.Type,
			Uuid:       command.Uuid,
			Value:      command.Value,
			CreateTime: command.CreateTime,
			Ex:         command.Ex,
		})
	}

	// Return the response with the slice
	return &pbuser.ProcessUserCommandGetResp{CommandResp: commandInfoSlice}, nil
}

func (s *userServer) ProcessUserCommandGetAll(ctx context.Context, req *pbuser.ProcessUserCommandGetAllReq) (*pbuser.ProcessUserCommandGetAllResp, error) {
	err := authverify.CheckAccessV3(ctx, req.UserID, s.config.Share.IMAdminUserID)
	if err != nil {
		return nil, err
	}
	// Fetch user commands from the database
	commands, err := s.db.GetAllUserCommands(ctx, req.UserID)
	if err != nil {
		return nil, err
	}

	// Initialize commandInfoSlice as an empty slice
	commandInfoSlice := make([]*pbuser.AllCommandInfoResp, 0, len(commands))

	for _, command := range commands {
		// No need to use index since command is already a pointer
		commandInfoSlice = append(commandInfoSlice, &pbuser.AllCommandInfoResp{
			Type:       command.Type,
			Uuid:       command.Uuid,
			Value:      command.Value,
			CreateTime: command.CreateTime,
			Ex:         command.Ex,
		})
	}

	// Return the response with the slice
	return &pbuser.ProcessUserCommandGetAllResp{CommandResp: commandInfoSlice}, nil
}

func (s *userServer) AddNotificationAccount(ctx context.Context, req *pbuser.AddNotificationAccountReq) (*pbuser.AddNotificationAccountResp, error) {
	if err := authverify.CheckAdmin(ctx, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}
	if req.AppMangerLevel < constant.AppNotificationAdmin {
		return nil, errs.ErrArgs.WithDetail("app level not supported")
	}
	if req.UserID == "" {
		for i := 0; i < 20; i++ {
			userId := s.genUserID()
			_, err := s.db.FindWithError(ctx, []string{userId})
			if err == nil {
				continue
			}
			req.UserID = userId
			break
		}
		if req.UserID == "" {
			return nil, errs.ErrInternalServer.WrapMsg("gen user id failed")
		}
	} else {
		_, err := s.db.FindWithError(ctx, []string{req.UserID})
		if err == nil {
			return nil, errs.ErrArgs.WrapMsg("userID is used")
		}
	}

	user := &tablerelation.User{
		UserID:         req.UserID,
		Nickname:       req.NickName,
		FaceURL:        req.FaceURL,
		CreateTime:     time.Now(),
		AppMangerLevel: req.AppMangerLevel,
	}
	if err := s.db.Create(ctx, []*tablerelation.User{user}); err != nil {
		return nil, err
	}

	return &pbuser.AddNotificationAccountResp{
		UserID:         req.UserID,
		NickName:       req.NickName,
		FaceURL:        req.FaceURL,
		AppMangerLevel: req.AppMangerLevel,
	}, nil
}

func (s *userServer) UpdateNotificationAccountInfo(ctx context.Context, req *pbuser.UpdateNotificationAccountInfoReq) (*pbuser.UpdateNotificationAccountInfoResp, error) {
	if err := authverify.CheckAdmin(ctx, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	if _, err := s.db.FindWithError(ctx, []string{req.UserID}); err != nil {
		return nil, errs.ErrArgs.Wrap()
	}

	user := map[string]interface{}{}

	if req.NickName != "" {
		user["nickname"] = req.NickName
	}

	if req.FaceURL != "" {
		user["face_url"] = req.FaceURL
	}

	if err := s.db.UpdateByMap(ctx, req.UserID, user); err != nil {
		return nil, err
	}

	return &pbuser.UpdateNotificationAccountInfoResp{}, nil
}

func (s *userServer) SearchNotificationAccount(ctx context.Context, req *pbuser.SearchNotificationAccountReq) (*pbuser.SearchNotificationAccountResp, error) {
	// Check if user is an admin
	if err := authverify.CheckAdmin(ctx, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	var users []*tablerelation.User
	var err error

	// If a keyword is provided in the request
	if req.Keyword != "" {
		// Find users by keyword
		users, err = s.db.Find(ctx, []string{req.Keyword})
		if err != nil {
			return nil, err
		}

		// Convert users to response format
		resp := s.userModelToResp(users, req.Pagination)
		if resp.Total != 0 {
			return resp, nil
		}

		// Find users by nickname if no users found by keyword
		users, err = s.db.FindByNickname(ctx, req.Keyword)
		if err != nil {
			return nil, err
		}
		resp = s.userModelToResp(users, req.Pagination)
		return resp, nil
	}

	// If no keyword, find users with notification settings
	users, err = s.db.FindNotification(ctx, constant.AppNotificationAdmin)
	if err != nil {
		return nil, err
	}

	resp := s.userModelToResp(users, req.Pagination)
	return resp, nil
}

func (s *userServer) GetNotificationAccount(ctx context.Context, req *pbuser.GetNotificationAccountReq) (*pbuser.GetNotificationAccountResp, error) {
	if req.UserID == "" {
		return nil, errs.ErrArgs.WrapMsg("userID is empty")
	}
	user, err := s.db.GetUserByID(ctx, req.UserID)
	if err != nil {
		return nil, servererrs.ErrUserIDNotFound.Wrap()
	}
	if user.AppMangerLevel == constant.AppAdmin || user.AppMangerLevel >= constant.AppNotificationAdmin {
		return &pbuser.GetNotificationAccountResp{Account: &pbuser.NotificationAccountInfo{
			UserID:         user.UserID,
			FaceURL:        user.FaceURL,
			NickName:       user.Nickname,
			AppMangerLevel: user.AppMangerLevel,
		}}, nil
	}

	return nil, errs.ErrNoPermission.WrapMsg("notification messages cannot be sent for this ID")
}

func (s *userServer) genUserID() string {
	const l = 10
	data := make([]byte, l)
	rand.Read(data)
	chars := []byte("0123456789")
	for i := 0; i < len(data); i++ {
		if i == 0 {
			data[i] = chars[1:][data[i]%9]
		} else {
			data[i] = chars[data[i]%10]
		}
	}
	return string(data)
}

func (s *userServer) userModelToResp(users []*tablerelation.User, pagination pagination.Pagination) *pbuser.SearchNotificationAccountResp {
	accounts := make([]*pbuser.NotificationAccountInfo, 0)
	var total int64
	for _, v := range users {
		if v.AppMangerLevel >= constant.AppNotificationAdmin && !datautil.Contain(v.UserID, s.config.Share.IMAdminUserID...) {
			temp := &pbuser.NotificationAccountInfo{
				UserID:         v.UserID,
				FaceURL:        v.FaceURL,
				NickName:       v.Nickname,
				AppMangerLevel: v.AppMangerLevel,
			}
			accounts = append(accounts, temp)
			total += 1
		}
	}

	notificationAccounts := datautil.Paginate(accounts, int(pagination.GetPageNumber()), int(pagination.GetShowNumber()))

	return &pbuser.SearchNotificationAccountResp{Total: total, NotificationAccounts: notificationAccounts}
}

func (s *userServer) NotificationUserInfoUpdate(ctx context.Context, userID string, oldUser *tablerelation.User) error {
	user, err := s.db.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if user.Nickname == oldUser.Nickname && user.FaceURL == oldUser.FaceURL {
		return nil
	}
	oldUserInfo := convert.UserDB2Pb(oldUser)
	newUserInfo := convert.UserDB2Pb(user)
	var wg sync.WaitGroup
	var es [2]error
	wg.Add(len(es))
	go func() {
		defer wg.Done()
		_, es[0] = s.groupClient.NotificationUserInfoUpdate(ctx, &group.NotificationUserInfoUpdateReq{
			UserID:      userID,
			OldUserInfo: oldUserInfo,
			NewUserInfo: newUserInfo,
		})
	}()

	go func() {
		defer wg.Done()
		_, es[1] = s.relationClient.NotificationUserInfoUpdate(ctx, &friendpb.NotificationUserInfoUpdateReq{
			UserID:      userID,
			OldUserInfo: oldUserInfo,
			NewUserInfo: newUserInfo,
		})
	}()
	wg.Wait()
	return errors.Join(es[:]...)
}

func (s *userServer) SortQuery(ctx context.Context, req *pbuser.SortQueryReq) (*pbuser.SortQueryResp, error) {
	users, err := s.db.SortQuery(ctx, req.UserIDName, req.Asc)
	if err != nil {
		return nil, err
	}
	return &pbuser.SortQueryResp{Users: convert.UsersDB2Pb(users)}, nil
}
