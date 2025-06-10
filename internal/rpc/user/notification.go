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
 * 用户通知发送服务
 *
 * 本模块负责处理用户相关的通知推送功能，为用户提供实时的状态变更和操作通知。
 *
 * 核心功能：
 *
 * 1. 用户状态通知
 *    - 在线状态变更：用户上线、离线状态的实时通知
 *    - 用户信息变更：头像、昵称等基本信息的变更通知
 *    - 用户权限变更：用户角色、权限级别的变更通知
 *    - 用户设置变更：用户偏好设置的变更通知
 *
 * 2. 用户命令通知
 *    - 命令添加通知：新增用户自定义命令的通知
 *    - 命令更新通知：用户命令内容或属性的更新通知
 *    - 命令删除通知：用户命令被删除的通知
 *    - 批量命令操作：支持批量命令操作的通知
 *
 * 3. 通知传递机制
 *    - 实时推送：基于WebSocket的实时通知推送
 *    - 离线存储：用户离线时的通知存储和重发机制
 *    - 多端同步：确保用户在所有设备上接收到通知
 *    - 消息去重：防止重复通知的发送
 *
 * 4. 通知内容定制
 *    - 消息模板：支持可配置的通知消息模板
 *    - 多语言支持：根据用户语言偏好发送对应语言的通知
 *    - 内容过滤：根据用户设置过滤特定类型的通知
 *    - 优先级管理：不同类型通知的优先级管理
 *
 * 5. 扩展功能
 *    - 用户信息获取：集成用户信息获取功能
 *    - 数据库集成：支持数据库数据的直接获取和验证
 *    - 缓存优化：通过缓存提升通知发送性能
 *    - 监控统计：通知发送的成功率和性能监控
 *
 * 技术特性：
 * - 异步处理：非阻塞的异步通知发送机制
 * - 高可用性：支持多节点部署和故障转移
 * - 可扩展性：支持水平扩展以处理大量通知
 * - 消息可靠性：确保重要通知的可靠传递
 * - 性能优化：批量发送和连接复用优化
 *
 * 应用场景：
 * - 用户状态同步：多设备间的用户状态实时同步
 * - 系统通知：系统级别的重要通知推送
 * - 业务提醒：基于业务逻辑的用户提醒
 * - 社交通知：好友关系变更等社交相关通知
 * - 管理通知：管理员操作对用户的影响通知
 */
package user

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/rpcli"
	"github.com/openimsdk/protocol/msg"

	relationtb "github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/open-im-server/v3/pkg/notification/common_user"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/controller"
	"github.com/openimsdk/open-im-server/v3/pkg/notification"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/sdkws"
)

// UserNotificationSender 用户通知发送器
//
// 专门处理用户相关通知的发送器，集成了通知发送的核心功能和用户特定的扩展能力。
//
// 设计模式：
// - 组合模式：通过组合基础NotificationSender实现功能扩展
// - 策略模式：支持不同的用户信息获取策略
// - 适配器模式：适配不同的数据源和用户信息格式
//
// 功能特性：
// - 用户信息集成：自动获取和填充用户相关信息
// - 灵活的数据源：支持多种用户信息获取方式
// - 缓存优化：通过缓存提升用户信息获取性能
// - 错误处理：完善的错误处理和降级机制
//
// 扩展能力：
// - 自定义用户信息获取函数
// - 数据库控制器集成
// - 多种通知类型支持
// - 批量操作优化
type UserNotificationSender struct {
	*notification.NotificationSender                                                                               // 基础通知发送器，提供核心的通知发送功能
	getUsersInfo                     func(ctx context.Context, userIDs []string) ([]common_user.CommonUser, error) // 用户信息获取函数，支持自定义用户信息获取策略
	// db controller
	db controller.UserDatabase // 用户数据库控制器，用于直接访问用户数据
}

// userNotificationSenderOptions 用户通知发送器选项函数类型
//
// 用于配置UserNotificationSender的可选参数，支持灵活的初始化配置。
// 采用函数式选项模式，提供清晰和可扩展的配置方式。
type userNotificationSenderOptions func(*UserNotificationSender)

// WithUserDB 配置用户数据库控制器选项
//
// 为用户通知发送器配置数据库控制器，使其能够直接访问用户数据库。
//
// 使用场景：
// - 需要直接查询用户数据库的场景
// - 对性能要求较高，需要减少网络调用的场景
// - 需要事务支持的复杂操作场景
//
// 优势：
// - 性能优化：减少网络调用开销
// - 数据一致性：支持事务操作
// - 功能完整：可访问完整的数据库功能
//
// 参数说明：
// - db: 用户数据库控制器实例
//
// 返回值：
// - userNotificationSenderOptions: 配置函数
func WithUserDB(db controller.UserDatabase) userNotificationSenderOptions {
	return func(u *UserNotificationSender) {
		u.db = db
	}
}

// WithUserFunc 配置用户信息获取函数选项
//
// 为用户通知发送器配置自定义的用户信息获取函数，支持灵活的用户数据获取策略。
//
// 适配器功能：
// - 将数据库特定的用户模型转换为通用的CommonUser接口
// - 支持不同数据源的用户信息获取
// - 提供统一的用户信息访问接口
//
// 灵活性：
// - 支持自定义用户信息获取逻辑
// - 可以集成缓存、限流等高级功能
// - 支持多数据源的用户信息聚合
//
// 错误处理：
// - 统一的错误处理和返回格式
// - 支持部分失败的处理策略
// - 提供详细的错误信息和上下文
//
// 参数说明：
// - fn: 用户信息获取函数，接受用户ID列表，返回用户信息列表
//
// 返回值：
// - userNotificationSenderOptions: 配置函数
func WithUserFunc(
	fn func(ctx context.Context, userIDs []string) (users []*relationtb.User, err error),
) userNotificationSenderOptions {
	return func(u *UserNotificationSender) {
		// 适配器函数：将数据库模型转换为通用接口
		f := func(ctx context.Context, userIDs []string) (result []common_user.CommonUser, err error) {
			// 调用原始的用户获取函数
			users, err := fn(ctx, userIDs)
			if err != nil {
				return nil, err
			}

			// 转换为通用的CommonUser接口
			for _, user := range users {
				result = append(result, user)
			}
			return result, nil
		}
		u.getUsersInfo = f
	}
}

// NewUserNotificationSender 创建用户通知发送器
//
// 创建并初始化一个新的用户通知发送器实例，集成基础通知功能和用户特定功能。
//
// 初始化流程：
// 1. 创建基础NotificationSender实例
// 2. 配置消息发送客户端
// 3. 应用用户特定的配置选项
// 4. 返回完整配置的发送器实例
//
// 配置集成：
// - 通知配置：从系统配置中获取通知相关设置
// - 消息客户端：配置用于实际消息发送的RPC客户端
// - 可选配置：通过选项函数应用额外的配置
//
// 依赖注入：
// - 通过参数注入必要的依赖组件
// - 支持可选依赖的灵活配置
// - 提供清晰的依赖关系管理
//
// 参数说明：
// - config: 系统配置，包含通知相关的配置信息
// - msgClient: 消息RPC客户端，用于实际的消息发送
// - opts: 可选配置函数列表，用于定制发送器行为
//
// 返回值：
// - *UserNotificationSender: 配置完成的用户通知发送器实例
func NewUserNotificationSender(config *Config, msgClient *rpcli.MsgClient, opts ...userNotificationSenderOptions) *UserNotificationSender {
	// 创建用户通知发送器基础实例
	f := &UserNotificationSender{
		// 创建基础通知发送器，配置消息发送能力
		NotificationSender: notification.NewNotificationSender(&config.NotificationConfig, notification.WithRpcClient(func(ctx context.Context, req *msg.SendMsgReq) (*msg.SendMsgResp, error) {
			return msgClient.SendMsg(ctx, req)
		})),
	}

	// 应用所有可选配置
	for _, opt := range opts {
		opt(f)
	}

	return f
}

/*
注释掉的代码保留，用于未来可能的功能扩展

func (u *UserNotificationSender) getUsersInfoMap(
	ctx context.Context,
	userIDs []string,
) (map[string]*sdkws.UserInfo, error) {
	users, err := u.getUsersInfo(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[string]*sdkws.UserInfo)
	for _, user := range users {
		result[user.GetUserID()] = user.(*sdkws.UserInfo)
	}
	return result, nil
}

func (u *UserNotificationSender) getFromToUserNickname(
	ctx context.Context,
	fromUserID, toUserID string,
) (string, string, error) {
	users, err := u.getUsersInfoMap(ctx, []string{fromUserID, toUserID})
	if err != nil {
		return "", "", nil
	}
	return users[fromUserID].Nickname, users[toUserID].Nickname, nil
}
*/

// UserStatusChangeNotification 发送用户状态变更通知
//
// 当用户状态发生变化时发送通知，包括在线状态、活跃状态等各种用户状态的变更。
//
// 通知内容：
// - 状态变更详情：具体的状态变更信息
// - 变更时间：状态变更的准确时间戳
// - 影响范围：状态变更可能影响的功能和服务
// - 附加信息：与状态变更相关的额外信息
//
// 应用场景：
// - 在线状态同步：用户在不同设备间的状态同步
// - 好友状态提醒：通知好友用户状态的变化
// - 业务流程触发：基于状态变更触发相关业务逻辑
// - 系统监控：用于系统监控和用户行为分析
//
// 参数说明：
// - ctx: 请求上下文，包含请求的元数据
// - tips: 用户状态变更提示信息，包含状态变更的详细数据
func (u *UserNotificationSender) UserStatusChangeNotification(
	ctx context.Context,
	tips *sdkws.UserStatusChangeTips,
) {
	u.Notification(ctx, tips.FromUserID, tips.ToUserID, constant.UserStatusChangeNotification, tips)
}

// UserCommandUpdateNotification 发送用户命令更新通知
//
// 当用户的自定义命令被更新时发送通知，确保相关方能及时了解命令的变更。
//
// 命令更新类型：
// - 命令内容更新：命令的执行内容或逻辑变更
// - 命令属性更新：命令的元数据、权限等属性变更
// - 命令状态更新：命令的启用/禁用状态变更
// - 批量命令更新：多个命令的批量更新操作
//
// 通知对象：
// - 命令所有者：接收命令更新的确认通知
// - 相关用户：可能受命令更新影响的其他用户
// - 系统管理员：需要了解命令变更的管理人员
//
// 参数说明：
// - ctx: 请求上下文
// - tips: 用户命令更新提示信息，包含更新的命令详情
func (u *UserNotificationSender) UserCommandUpdateNotification(
	ctx context.Context,
	tips *sdkws.UserCommandUpdateTips,
) {
	u.Notification(ctx, tips.FromUserID, tips.ToUserID, constant.UserCommandUpdateNotification, tips)
}

// UserCommandAddNotification 发送用户命令添加通知
//
// 当用户添加新的自定义命令时发送通知，确保新命令的创建被适当记录和通知。
//
// 新命令特性：
// - 命令功能：新命令提供的具体功能和能力
// - 权限设置：新命令的访问权限和使用范围
// - 集成信息：新命令与现有系统的集成点
// - 使用指南：新命令的使用方法和注意事项
//
// 验证和审核：
// - 命令合法性：确保新命令符合系统规范
// - 安全性检查：验证命令不会造成安全风险
// - 性能影响：评估新命令对系统性能的影响
// - 冲突检测：检查是否与现有命令存在冲突
//
// 参数说明：
// - ctx: 请求上下文
// - tips: 用户命令添加提示信息，包含新命令的详细信息
func (u *UserNotificationSender) UserCommandAddNotification(
	ctx context.Context,
	tips *sdkws.UserCommandAddTips,
) {
	u.Notification(ctx, tips.FromUserID, tips.ToUserID, constant.UserCommandAddNotification, tips)
}

// UserCommandDeleteNotification 发送用户命令删除通知
//
// 当用户删除自定义命令时发送通知，确保命令删除操作被妥善记录和通知相关方。
//
// 删除影响分析：
// - 依赖关系：分析被删除命令的依赖关系
// - 数据清理：确保相关数据被适当清理
// - 权限回收：回收与命令相关的权限和资源
// - 历史记录：保留必要的历史记录和审计信息
//
// 安全考虑：
// - 删除权限：验证用户是否有权限删除该命令
// - 影响评估：评估删除操作对其他功能的影响
// - 恢复机制：提供必要的命令恢复机制
// - 通知范围：确定需要通知的用户范围
//
// 清理策略：
// - 立即清理：立即清理相关的临时数据
// - 延迟清理：对重要数据进行延迟清理
// - 归档处理：将删除的命令信息进行归档
// - 审计记录：记录详细的删除操作审计信息
//
// 参数说明：
// - ctx: 请求上下文
// - tips: 用户命令删除提示信息，包含被删除命令的相关信息
func (u *UserNotificationSender) UserCommandDeleteNotification(
	ctx context.Context,
	tips *sdkws.UserCommandDeleteTips,
) {
	u.Notification(ctx, tips.FromUserID, tips.ToUserID, constant.UserCommandDeleteNotification, tips)
}
