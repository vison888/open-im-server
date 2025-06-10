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
 * 用户相关的Webhook回调服务
 *
 * 本模块负责处理用户相关操作的Webhook回调机制，为第三方系统提供数据同步和业务集成的能力。
 *
 * 核心功能：
 *
 * 1. 操作前置回调（Before Callbacks）
 *    - 数据预处理：在实际操作执行前对数据进行验证和修改
 *    - 权限控制：第三方系统可以在回调中实现额外的权限验证
 *    - 数据增强：通过回调添加额外的业务数据或字段
 *    - 操作拦截：根据业务规则决定是否允许操作继续执行
 *
 * 2. 操作后置回调（After Callbacks）
 *    - 数据同步：将操作结果同步到第三方系统
 *    - 业务触发：触发下游业务流程和处理逻辑
 *    - 日志记录：记录详细的操作日志和审计信息
 *    - 通知推送：向相关方发送操作完成通知
 *
 * 3. 支持的用户操作
 *    - 用户信息更新：包括昵称、头像等基本信息的变更
 *    - 用户扩展信息更新：用户自定义字段的变更
 *    - 用户注册：新用户的注册流程回调
 *    - 其他用户生命周期事件
 *
 * 4. 回调机制特性
 *    - 同步回调：前置回调采用同步方式，确保数据一致性
 *    - 异步回调：后置回调采用异步方式，提高系统性能
 *    - 错误处理：完善的错误处理和重试机制
 *    - 条件触发：支持基于条件的回调触发控制
 *
 * 5. 数据处理能力
 *    - 字段映射：支持数据字段的映射和转换
 *    - 数据验证：对回调数据进行有效性验证
 *    - 数据过滤：支持敏感数据的过滤和脱敏
 *    - 批量处理：支持批量操作的回调处理
 *
 * 技术特性：
 * - HTTP Webhook：基于HTTP协议的标准Webhook实现
 * - 重试机制：支持失败重试和指数退避算法
 * - 超时控制：可配置的请求超时时间
 * - 签名验证：支持请求签名验证确保安全性
 * - 负载均衡：支持多个回调端点的负载均衡
 *
 * 应用场景：
 * - 企业系统集成：与企业现有用户系统进行数据同步
 * - 第三方认证：集成第三方身份认证和授权系统
 * - 业务流程触发：用户操作触发相关业务流程
 * - 数据仓库同步：将用户数据同步到数据仓库
 * - 实时监控：实时监控用户操作和行为数据
 */
package user

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/common/webhook"
	"github.com/openimsdk/tools/utils/datautil"

	cbapi "github.com/openimsdk/open-im-server/v3/pkg/callbackstruct"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	pbuser "github.com/openimsdk/protocol/user"
)

// webhookBeforeUpdateUserInfo 用户信息更新前置回调
//
// 在用户更新基本信息（昵称、头像等）之前触发的回调，允许第三方系统：
// 1. 验证更新数据的合法性和完整性
// 2. 对用户输入的数据进行预处理和标准化
// 3. 添加额外的业务字段或计算字段
// 4. 根据业务规则决定是否允许更新操作
//
// 回调数据处理：
// - 数据验证：验证昵称格式、头像URL有效性等
// - 内容过滤：过滤敏感词汇和不当内容
// - 数据规范化：统一数据格式和编码标准
// - 权限检查：验证用户是否有权限进行此操作
//
// 同步执行：前置回调采用同步方式执行，确保：
// - 数据一致性：回调结果直接影响后续操作
// - 错误处理：回调失败会阻止原操作执行
// - 性能考虑：回调超时时间需要合理设置
//
// 参数说明：
// - ctx: 请求上下文，包含请求ID、用户身份等信息
// - before: 前置回调配置，包含回调URL、超时设置等
// - req: 用户信息更新请求，包含要更新的用户数据
//
// 返回值：
// - error: 回调执行错误，非nil时会阻止后续操作
func (s *userServer) webhookBeforeUpdateUserInfo(ctx context.Context, before *config.BeforeConfig, req *pbuser.UpdateUserInfoReq) error {
	return webhook.WithCondition(ctx, before, func(ctx context.Context) error {
		// 构建回调请求数据
		cbReq := &cbapi.CallbackBeforeUpdateUserInfoReq{
			CallbackCommand: cbapi.CallbackBeforeUpdateUserInfoCommand,
			UserID:          req.UserInfo.UserID,
			FaceURL:         &req.UserInfo.FaceURL,
			Nickname:        &req.UserInfo.Nickname,
		}

		// 准备回调响应数据结构
		resp := &cbapi.CallbackBeforeUpdateUserInfoResp{}

		// 执行同步回调请求
		if err := s.webhookClient.SyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, resp, before); err != nil {
			return err
		}

		// 将回调返回的数据应用到原始请求中
		// 支持第三方系统修改和增强原始数据
		datautil.NotNilReplace(&req.UserInfo.FaceURL, resp.FaceURL)   // 更新头像URL
		datautil.NotNilReplace(&req.UserInfo.Ex, resp.Ex)             // 更新扩展字段
		datautil.NotNilReplace(&req.UserInfo.Nickname, resp.Nickname) // 更新昵称
		return nil
	})
}

// webhookAfterUpdateUserInfo 用户信息更新后置回调
//
// 在用户信息更新成功后触发的回调，用于：
// 1. 将更新结果同步到第三方系统
// 2. 触发相关的业务流程和处理逻辑
// 3. 记录详细的操作日志和审计信息
// 4. 发送更新完成的通知消息
//
// 异步执行：后置回调采用异步方式执行，特点：
// - 性能优化：不阻塞主流程，提高系统响应速度
// - 容错性强：回调失败不影响已完成的操作
// - 重试机制：支持失败重试和错误恢复
//
// 应用场景：
// - 数据同步：同步用户信息到CRM、ERP等系统
// - 消息推送：通知相关用户或管理员
// - 业务触发：触发积分、等级等业务逻辑更新
// - 统计分析：更新用户行为分析数据
//
// 参数说明：
// - ctx: 请求上下文
// - after: 后置回调配置
// - req: 已完成的用户信息更新请求
func (s *userServer) webhookAfterUpdateUserInfo(ctx context.Context, after *config.AfterConfig, req *pbuser.UpdateUserInfoReq) {
	// 构建回调通知数据
	cbReq := &cbapi.CallbackAfterUpdateUserInfoReq{
		CallbackCommand: cbapi.CallbackAfterUpdateUserInfoCommand,
		UserID:          req.UserInfo.UserID,
		FaceURL:         req.UserInfo.FaceURL,
		Nickname:        req.UserInfo.Nickname,
	}

	// 执行异步回调通知
	s.webhookClient.AsyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, &cbapi.CallbackAfterUpdateUserInfoResp{}, after)
}

// webhookBeforeUpdateUserInfoEx 用户扩展信息更新前置回调
//
// 处理用户扩展信息更新的前置回调，扩展信息通常包括：
// 1. 用户自定义字段和属性
// 2. 业务相关的特殊标识和配置
// 3. 第三方系统的集成数据
// 4. 用户偏好设置和个性化配置
//
// 与基本信息更新的区别：
// - 字段灵活性：扩展信息字段更加灵活多样
// - 业务复杂性：可能涉及更复杂的业务逻辑验证
// - 权限差异：可能需要不同的权限验证策略
// - 影响范围：扩展信息变更可能影响更多业务模块
//
// 验证重点：
// - 数据格式：验证JSON、XML等复杂数据格式
// - 业务规则：验证业务特定的约束条件
// - 系统限制：检查字段长度、数据大小等限制
// - 安全性：防止恶意数据注入和攻击
func (s *userServer) webhookBeforeUpdateUserInfoEx(ctx context.Context, before *config.BeforeConfig, req *pbuser.UpdateUserInfoExReq) error {
	return webhook.WithCondition(ctx, before, func(ctx context.Context) error {
		// 构建扩展信息更新回调请求
		cbReq := &cbapi.CallbackBeforeUpdateUserInfoExReq{
			CallbackCommand: cbapi.CallbackBeforeUpdateUserInfoExCommand,
			UserID:          req.UserInfo.UserID,
			FaceURL:         req.UserInfo.FaceURL,
			Nickname:        req.UserInfo.Nickname,
		}

		resp := &cbapi.CallbackBeforeUpdateUserInfoExResp{}

		// 执行同步回调验证
		if err := s.webhookClient.SyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, resp, before); err != nil {
			return err
		}

		// 应用回调结果到原始请求
		// 注意：扩展信息更新使用指针传递方式
		datautil.NotNilReplace(req.UserInfo.FaceURL, resp.FaceURL)
		datautil.NotNilReplace(req.UserInfo.Ex, resp.Ex)
		datautil.NotNilReplace(req.UserInfo.Nickname, resp.Nickname)
		return nil
	})
}

// webhookAfterUpdateUserInfoEx 用户扩展信息更新后置回调
//
// 用户扩展信息更新完成后的回调通知，主要用于：
// 1. 同步扩展数据到外部系统
// 2. 触发基于扩展信息的业务流程
// 3. 更新相关的索引和缓存数据
// 4. 生成扩展信息变更的统计报告
//
// 特殊处理：
// - 数据敏感性：扩展信息可能包含敏感数据，需要选择性同步
// - 格式转换：可能需要将内部格式转换为外部系统格式
// - 版本控制：支持扩展信息的版本管理和历史追踪
// - 关联更新：可能需要更新关联的业务数据
func (s *userServer) webhookAfterUpdateUserInfoEx(ctx context.Context, after *config.AfterConfig, req *pbuser.UpdateUserInfoExReq) {
	// 构建扩展信息更新完成通知
	cbReq := &cbapi.CallbackAfterUpdateUserInfoExReq{
		CallbackCommand: cbapi.CallbackAfterUpdateUserInfoExCommand,
		UserID:          req.UserInfo.UserID,
		FaceURL:         req.UserInfo.FaceURL,
		Nickname:        req.UserInfo.Nickname,
	}

	// 异步通知外部系统
	s.webhookClient.AsyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, &cbapi.CallbackAfterUpdateUserInfoExResp{}, after)
}

// webhookBeforeUserRegister 用户注册前置回调
//
// 用户注册流程的前置回调，是用户生命周期管理的关键环节，负责：
// 1. 用户数据预验证和标准化处理
// 2. 重复注册检查和防欺诈验证
// 3. 注册资格验证和白名单控制
// 4. 自动分配用户等级、角色等属性
//
// 批量注册支持：
// - 支持单个和批量用户注册的回调处理
// - 提供批量数据验证和处理能力
// - 支持部分成功的批量注册策略
// - 提供详细的批量操作结果反馈
//
// 安全考虑：
// - 防止恶意注册和刷号行为
// - 验证邮箱、手机号等关键信息的真实性
// - 检查用户名、昵称的合规性
// - 实施注册频率限制和IP限制
//
// 数据增强：
// - 自动生成用户唯一标识和编号
// - 分配默认的用户配置和权限
// - 设置用户的初始状态和属性
// - 关联用户到相应的组织和部门
//
// 参数说明：
// - ctx: 注册请求上下文
// - before: 前置回调配置，包含验证策略
// - req: 用户注册请求，包含待注册的用户列表
//
// 返回值：
// - error: 验证失败错误，会阻止注册流程继续
func (s *userServer) webhookBeforeUserRegister(ctx context.Context, before *config.BeforeConfig, req *pbuser.UserRegisterReq) error {
	return webhook.WithCondition(ctx, before, func(ctx context.Context) error {
		// 构建用户注册验证请求
		cbReq := &cbapi.CallbackBeforeUserRegisterReq{
			CallbackCommand: cbapi.CallbackBeforeUserRegisterCommand,
			Users:           req.Users, // 传递完整的用户列表进行批量验证
		}

		resp := &cbapi.CallbackBeforeUserRegisterResp{}

		// 执行同步回调验证
		if err := s.webhookClient.SyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, resp, before); err != nil {
			return err
		}

		// 如果回调返回了修改后的用户数据，则使用回调结果
		// 这允许第三方系统对注册数据进行预处理和增强
		if len(resp.Users) != 0 {
			req.Users = resp.Users
		}
		return nil
	})
}

// webhookAfterUserRegister 用户注册后置回调
//
// 用户注册成功后的回调通知，完成用户注册流程的后续处理：
// 1. 将新注册用户同步到各个业务系统
// 2. 触发欢迎流程和新手引导逻辑
// 3. 发送注册成功通知和确认邮件
// 4. 更新用户注册统计和分析数据
//
// 后续业务触发：
// - 创建用户默认的聊天设置和偏好
// - 分配新用户优惠券和奖励
// - 添加用户到默认群组和频道
// - 初始化用户的业务数据和配置
//
// 数据同步：
// - 同步用户信息到CRM系统
// - 更新用户画像和标签系统
// - 同步到数据仓库和分析平台
// - 更新搜索索引和推荐系统
//
// 通知推送：
// - 发送欢迎短信和邮件
// - 推送新用户注册通知给管理员
// - 触发相关用户的好友推荐
// - 生成注册成功的系统日志
//
// 异步执行优势：
// - 不影响注册流程的用户体验
// - 支持重试机制确保数据最终一致性
// - 可以并行执行多个后续处理任务
// - 降低注册接口的响应时间
func (s *userServer) webhookAfterUserRegister(ctx context.Context, after *config.AfterConfig, req *pbuser.UserRegisterReq) {
	// 构建用户注册完成通知
	cbReq := &cbapi.CallbackAfterUserRegisterReq{
		CallbackCommand: cbapi.CallbackAfterUserRegisterCommand,
		Users:           req.Users, // 传递已注册成功的用户列表
	}

	// 异步通知外部系统用户注册完成
	s.webhookClient.AsyncPost(ctx, cbReq.GetCallbackCommand(), cbReq, &cbapi.CallbackAfterUserRegisterResp{}, after)
}
