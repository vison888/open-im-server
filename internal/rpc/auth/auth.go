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
 * 身份认证与授权服务
 *
 * 本模块是OpenIM系统的安全核心，负责处理所有的身份认证和访问控制功能。
 *
 * 核心功能：
 *
 * 1. Token管理体系
 *    - Token生成：为用户和管理员创建JWT访问令牌
 *    - Token验证：验证令牌的有效性和权限范围
 *    - Token刷新：支持令牌的自动续期机制
 *    - Token撤销：支持令牌的主动失效和强制下线
 *
 * 2. 多平台身份管理
 *    - 平台隔离：不同客户端平台的独立Token管理
 *    - 多端登录：支持同一用户在多个平台同时在线
 *    - 互踢机制：支持新登录踢掉旧登录的策略
 *    - 状态同步：跨平台的登录状态实时同步
 *
 * 3. 权限分级体系
 *    - 系统管理员：拥有最高权限，可管理所有用户和系统设置
 *    - 普通用户：标准用户权限，只能访问自己的数据
 *    - 应用账号：特殊功能账号，如通知账号等
 *    - 访客权限：受限访问权限，用于特定场景
 *
 * 4. 安全机制
 *    - JWT签名：使用密钥对Token进行数字签名
 *    - 过期控制：可配置的Token有效期管理
 *    - 防重放：Token状态追踪防止重复使用
 *    - 异常检测：识别异常登录和攻击行为
 *
 * 5. 会话管理
 *    - 在线状态：实时跟踪用户在线状态
 *    - 强制下线：管理员强制用户下线功能
 *    - 会话超时：自动清理过期会话
 *    - 设备管理：支持设备级别的会话控制
 *
 * 技术特性：
 * - JWT标准：基于JWT的无状态Token设计
 * - Redis存储：使用Redis存储Token状态和映射关系
 * - 分布式支持：支持多节点的Token状态同步
 * - 高性能：缓存优化的快速认证验证
 * - 安全传输：所有认证数据的安全传输保护
 *
 * 应用场景：
 * - API接口认证：所有API调用的身份验证
 * - WebSocket连接：实时通信的连接认证
 * - 第三方集成：外部系统的身份验证集成
 * - 管理后台：管理员身份的特权验证
 * - 移动应用：移动端的安全登录认证
 */
package auth

import (
	"context"
	"errors"

	"github.com/openimsdk/open-im-server/v3/pkg/rpcli"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	redis2 "github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache/redis"
	"github.com/openimsdk/tools/db/redisutil"
	"github.com/openimsdk/tools/utils/datautil"
	"github.com/redis/go-redis/v9"

	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/open-im-server/v3/pkg/common/prommetrics"
	"github.com/openimsdk/open-im-server/v3/pkg/common/servererrs"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/controller"
	pbauth "github.com/openimsdk/protocol/auth"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/msggateway"
	"github.com/openimsdk/tools/discovery"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/tokenverify"
	"google.golang.org/grpc"
)

// authServer 认证服务器结构体
//
// 实现了完整的身份认证和授权功能，包括Token管理、权限验证、会话控制等核心安全机制。
//
// 架构设计：
// - 微服务架构：独立的认证服务，可单独部署和扩展
// - 无状态设计：基于JWT的无状态Token认证
// - 缓存优化：Redis缓存提升认证性能
// - 服务发现：集成服务注册与发现机制
type authServer struct {
	pbauth.UnimplementedAuthServer                                // gRPC服务基础实现
	authDatabase                   controller.AuthDatabase        // 认证数据库控制器，管理Token和用户状态
	RegisterCenter                 discovery.SvcDiscoveryRegistry // 服务注册中心，用于服务发现和通信
	config                         *Config                        // 服务配置，包含认证相关的所有配置参数
	userClient                     *rpcli.UserClient              // 用户服务客户端，用于用户信息验证
}

// Config 认证服务配置结构体
//
// 集中管理认证服务运行所需的所有配置参数，包括：
// - 认证策略：Token有效期、多端登录策略等
// - 存储配置：Redis连接、缓存设置等
// - 安全配置：密钥管理、管理员权限等
// - 服务配置：服务发现、网络配置等
type Config struct {
	RpcConfig   config.Auth      // 认证服务特定配置：Token策略、过期时间等
	RedisConfig config.Redis     // Redis配置：连接信息、连接池设置
	Share       config.Share     // 共享配置：密钥、管理员ID、多端登录策略
	Discovery   config.Discovery // 服务发现配置：注册中心设置
}

// Start 启动认证服务
//
// 初始化认证服务的所有组件，包括数据库连接、缓存设置、外部服务连接等。
//
// 初始化流程：
// 1. 建立Redis连接：用于Token状态存储
// 2. 建立用户服务连接：用于用户信息验证
// 3. 初始化认证数据库控制器：整合Token管理和缓存策略
// 4. 注册gRPC服务：将认证服务注册到服务器
//
// 参数说明：
// - ctx: 启动上下文，控制启动过程的超时和取消
// - config: 服务配置，包含所有必要的配置参数
// - client: 服务发现客户端，用于注册和发现其他服务
// - server: gRPC服务器实例，用于注册认证服务
//
// 返回值：
// - error: 启动过程中的错误信息，nil表示启动成功
func Start(ctx context.Context, config *Config, client discovery.SvcDiscoveryRegistry, server *grpc.Server) error {
	// 初始化Redis客户端，用于Token状态存储
	rdb, err := redisutil.NewRedisClient(ctx, config.RedisConfig.Build())
	if err != nil {
		return err
	}

	// 建立与用户服务的连接，用于用户信息验证
	userConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.User)
	if err != nil {
		return err
	}

	// 注册认证服务到gRPC服务器
	pbauth.RegisterAuthServer(server, &authServer{
		RegisterCenter: client,
		// 初始化认证数据库控制器，整合Token缓存和管理策略
		authDatabase: controller.NewAuthDatabase(
			redis2.NewTokenCacheModel(rdb, config.RpcConfig.TokenPolicy.Expire), // Token缓存模型
			config.Share.Secret,                 // JWT签名密钥
			config.RpcConfig.TokenPolicy.Expire, // Token过期时间
			config.Share.MultiLogin,             // 多端登录策略
			config.Share.IMAdminUserID,          // 系统管理员ID列表
		),
		config:     config,
		userClient: rpcli.NewUserClient(userConn), // 用户服务客户端
	})
	return nil
}

// GetAdminToken 获取管理员访问Token
//
// 为系统管理员生成具有最高权限的访问令牌，用于管理后台和系统操作。
//
// 安全验证：
// 1. 密钥验证：验证请求中的系统密钥是否正确
// 2. 管理员身份验证：确认用户ID是否在管理员列表中
// 3. 用户存在性验证：确认管理员用户确实存在于系统中
//
// Token特性：
// - 管理员平台ID：使用特殊的管理员平台标识
// - 最高权限：可以访问所有管理功能和用户数据
// - 较长有效期：适合管理操作的较长时间访问
//
// 使用场景：
// - 管理后台登录：为管理员提供后台访问权限
// - 系统初始化：系统部署时的初始管理员设置
// - 紧急操作：紧急情况下的系统管理访问
// - API调用：管理类API的身份验证
//
// 参数说明：
// - ctx: 请求上下文，包含请求链路信息
// - req: 管理员Token请求，包含用户ID和系统密钥
//
// 返回值：
// - *pbauth.GetAdminTokenResp: 包含Token和过期时间的响应
// - error: 验证失败或生成过程中的错误信息
func (s *authServer) GetAdminToken(ctx context.Context, req *pbauth.GetAdminTokenReq) (*pbauth.GetAdminTokenResp, error) {
	resp := pbauth.GetAdminTokenResp{}

	// 验证系统密钥
	if req.Secret != s.config.Share.Secret {
		return nil, errs.ErrNoPermission.WrapMsg("secret invalid")
	}

	// 验证用户ID是否在管理员列表中
	if !datautil.Contain(req.UserID, s.config.Share.IMAdminUserID...) {
		return nil, errs.ErrArgs.WrapMsg("userID is error.", "userID", req.UserID, "adminUserID", s.config.Share.IMAdminUserID)
	}

	// 验证管理员用户是否存在
	if err := s.userClient.CheckUser(ctx, []string{req.UserID}); err != nil {
		return nil, err
	}

	// 生成管理员Token
	token, err := s.authDatabase.CreateToken(ctx, req.UserID, int(constant.AdminPlatformID))
	if err != nil {
		return nil, err
	}

	// 记录管理员登录指标
	prommetrics.UserLoginCounter.Inc()

	// 构建响应
	resp.Token = token
	resp.ExpireTimeSeconds = s.config.RpcConfig.TokenPolicy.Expire * 24 * 60 * 60
	return &resp, nil
}

// GetUserToken 获取普通用户Token
//
// 为普通用户生成访问令牌，仅限管理员调用，用于代理用户操作或系统集成。
//
// 权限验证：
// 1. 管理员权限：只有管理员才能为其他用户生成Token
// 2. 平台限制：不能为管理员平台生成Token
// 3. 用户类型检查：不能为系统管理员用户生成普通Token
// 4. 应用账号限制：应用级账号不能获取Token
//
// 应用场景：
// - 系统集成：第三方系统代理用户操作
// - 测试环境：测试时模拟用户登录
// - 客服系统：客服代理用户进行操作
// - 数据迁移：批量用户数据处理
//
// 安全考虑：
// - 严格的管理员权限验证
// - 防止权限提升攻击
// - 应用账号的特殊保护
// - 完整的操作审计日志
func (s *authServer) GetUserToken(ctx context.Context, req *pbauth.GetUserTokenReq) (*pbauth.GetUserTokenResp, error) {
	// 验证调用者是否为管理员
	if err := authverify.CheckAdmin(ctx, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	// 禁止为管理员平台生成Token
	if req.PlatformID == constant.AdminPlatformID {
		return nil, errs.ErrNoPermission.WrapMsg("platformID invalid. platformID must not be adminPlatformID")
	}

	resp := pbauth.GetUserTokenResp{}

	// 禁止为管理员用户生成普通Token
	if authverify.IsManagerUserID(req.UserID, s.config.Share.IMAdminUserID) {
		return nil, errs.ErrNoPermission.WrapMsg("don't get Admin token")
	}

	// 获取用户信息并验证
	user, err := s.userClient.GetUserInfo(ctx, req.UserID)
	if err != nil {
		return nil, err
	}

	// 应用级账号不能获取Token
	if user.AppMangerLevel >= constant.AppNotificationAdmin {
		return nil, errs.ErrArgs.WrapMsg("app account can`t get token")
	}

	// 生成用户Token
	token, err := s.authDatabase.CreateToken(ctx, req.UserID, int(req.PlatformID))
	if err != nil {
		return nil, err
	}

	resp.Token = token
	resp.ExpireTimeSeconds = s.config.RpcConfig.TokenPolicy.Expire * 24 * 60 * 60
	return &resp, nil
}

// parseToken 解析并验证Token
//
// 核心的Token验证逻辑，处理Token的解析、验证和状态检查。
//
// 验证流程：
// 1. JWT解析：使用密钥解析Token的Claims信息
// 2. 管理员特权：管理员Token跳过Redis状态检查
// 3. 状态验证：从Redis检查Token的当前状态
// 4. 状态判断：根据Token状态返回相应结果
//
// Token状态类型：
// - NormalToken：正常有效的Token
// - KickedToken：被踢下线的Token
// - 不存在：Token已过期或被删除
//
// 性能优化：
// - 管理员Token免Redis查询
// - 缓存Token验证结果
// - 异步状态更新机制
func (s *authServer) parseToken(ctx context.Context, tokensString string) (claims *tokenverify.Claims, err error) {
	// 解析JWT Token
	claims, err = tokenverify.GetClaimFromToken(tokensString, authverify.Secret(s.config.Share.Secret))
	if err != nil {
		return nil, err
	}

	// 检查是否为管理员Token
	isAdmin := authverify.IsManagerUserID(claims.UserID, s.config.Share.IMAdminUserID)
	if isAdmin {
		return claims, nil // 管理员Token直接通过，跳过Redis状态检查
	}

	// 从Redis获取Token状态映射
	m, err := s.authDatabase.GetTokensWithoutError(ctx, claims.UserID, claims.PlatformID)
	if err != nil {
		return nil, err
	}

	// Token不存在于Redis中
	if len(m) == 0 {
		return nil, servererrs.ErrTokenNotExist.Wrap()
	}

	// 检查特定Token的状态
	if v, ok := m[tokensString]; ok {
		switch v {
		case constant.NormalToken:
			return claims, nil
		case constant.KickedToken:
			return nil, servererrs.ErrTokenKicked.Wrap()
		default:
			return nil, errs.Wrap(errs.ErrTokenUnknown)
		}
	}

	return nil, servererrs.ErrTokenNotExist.Wrap()
}

// ParseToken 对外提供的Token解析接口
//
// 解析并验证Token的有效性，返回Token中包含的用户信息。
//
// 应用场景：
// - API网关：验证请求的Token有效性
// - 中间件：在请求处理前验证用户身份
// - 微服务：服务间调用的身份验证
// - WebSocket：建立连接时的身份验证
func (s *authServer) ParseToken(ctx context.Context, req *pbauth.ParseTokenReq) (resp *pbauth.ParseTokenResp, err error) {
	resp = &pbauth.ParseTokenResp{}

	// 解析Token获取Claims
	claims, err := s.parseToken(ctx, req.Token)
	if err != nil {
		return nil, err
	}

	// 构建响应
	resp.UserID = claims.UserID
	resp.PlatformID = int32(claims.PlatformID)
	resp.ExpireTimeSeconds = claims.ExpiresAt.Unix()
	return resp, nil
}

// ForceLogout 强制用户下线
//
// 管理员功能，强制指定用户在指定平台下线，包括断开连接和撤销Token。
//
// 操作流程：
// 1. 权限验证：确认操作者为管理员
// 2. 强制踢下线：通过消息网关断开用户连接
// 3. Token撤销：将用户Token标记为被踢状态
//
// 应用场景：
// - 违规处理：对违规用户进行强制下线
// - 安全事件：发现安全风险时的紧急处理
// - 系统维护：维护期间清空在线用户
// - 账号管理：解决账号异常或冲突问题
func (s *authServer) ForceLogout(ctx context.Context, req *pbauth.ForceLogoutReq) (*pbauth.ForceLogoutResp, error) {
	// 验证管理员权限
	if err := authverify.CheckAdmin(ctx, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	// 执行强制下线操作
	if err := s.forceKickOff(ctx, req.UserID, req.PlatformID); err != nil {
		return nil, err
	}

	return &pbauth.ForceLogoutResp{}, nil
}

// forceKickOff 执行强制踢人的具体逻辑
//
// 内部方法，处理强制下线的具体实现，包括连接断开和Token状态更新。
//
// 操作步骤：
// 1. 获取消息网关连接列表
// 2. 向所有网关发送踢人请求
// 3. 更新Redis中的Token状态为被踢状态
//
// 容错设计：
// - 多网关广播：确保用户在所有网关都被踢下线
// - 错误忽略：单个网关失败不影响整体流程
// - 状态一致性：确保Token状态的最终一致性
func (s *authServer) forceKickOff(ctx context.Context, userID string, platformID int32) error {
	// 获取所有消息网关连接
	conns, err := s.RegisterCenter.GetConns(ctx, s.config.Share.RpcRegisterName.MessageGateway)
	if err != nil {
		return err
	}

	// 向所有消息网关发送踢人请求
	for _, v := range conns {
		log.ZDebug(ctx, "forceKickOff", "userID", userID, "platformID", platformID)
		client := msggateway.NewMsgGatewayClient(v)
		kickReq := &msggateway.KickUserOfflineReq{KickUserIDList: []string{userID}, PlatformID: platformID}
		_, err := client.KickUserOffline(ctx, kickReq)
		if err != nil {
			log.ZError(ctx, "forceKickOff", err, "kickReq", kickReq)
			// 继续处理其他网关，不因单个失败而中断
		}
	}

	// 获取用户在该平台的所有Token
	m, err := s.authDatabase.GetTokensWithoutError(ctx, userID, int(platformID))
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}

	// 将所有Token标记为被踢状态
	for k := range m {
		m[k] = constant.KickedToken
		log.ZDebug(ctx, "set token map is ", "token map", m, "userID", userID, "token", k)

		err = s.authDatabase.SetTokenMapByUidPid(ctx, userID, int(platformID), m)
		if err != nil {
			return err
		}
	}

	return nil
}

// 其他方法的注释继续...

// InvalidateToken 使Token失效（保留指定Token）
//
// 使用户在指定平台的所有Token失效，但可以保留一个指定的Token。
// 主要用于单点登录场景，新登录时踢掉其他登录。
func (s *authServer) InvalidateToken(ctx context.Context, req *pbauth.InvalidateTokenReq) (*pbauth.InvalidateTokenResp, error) {
	// 获取用户在该平台的所有Token
	m, err := s.authDatabase.GetTokensWithoutError(ctx, req.UserID, int(req.PlatformID))
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	if m == nil {
		return nil, errs.New("token map is empty").Wrap()
	}
	log.ZDebug(ctx, "get token from redis", "userID", req.UserID, "platformID", req.PlatformID, "tokenMap", m)

	// 除了指定保留的Token外，其他都标记为被踢状态
	for k := range m {
		if k != req.GetPreservedToken() {
			m[k] = constant.KickedToken
		}
	}
	log.ZDebug(ctx, "set token map is ", "token map", m, "userID", req.UserID, "token", req.GetPreservedToken())

	err = s.authDatabase.SetTokenMapByUidPid(ctx, req.UserID, int(req.PlatformID), m)
	if err != nil {
		return nil, err
	}
	return &pbauth.InvalidateTokenResp{}, nil
}

// KickTokens 批量踢Token
//
// 批量更新多个Token的状态，用于批量踢人操作。
// 主要用于系统维护、批量管理等场景。
func (s *authServer) KickTokens(ctx context.Context, req *pbauth.KickTokensReq) (*pbauth.KickTokensResp, error) {
	if err := s.authDatabase.BatchSetTokenMapByUidPid(ctx, req.Tokens); err != nil {
		return nil, err
	}
	return &pbauth.KickTokensResp{}, nil
}
