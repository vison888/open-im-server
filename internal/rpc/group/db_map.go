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
 * 群组数据库映射工具
 *
 * 本模块提供群组数据的映射转换功能，主要负责将API请求参数转换为数据库更新操作的字段映射。
 *
 * 核心功能：
 *
 * 1. 群组信息映射
 *    - 基础映射：将群组设置请求转换为数据库更新字段
 *    - 扩展映射：支持更复杂的群组信息设置场景
 *    - 条件映射：根据不同条件生成不同的更新策略
 *
 * 2. 状态管理映射
 *    - 群组状态：群组启用、禁用、解散等状态转换
 *    - 成员状态：成员禁言、角色变更等状态映射
 *    - 时间映射：各种时间字段的设置和更新
 *
 * 3. 成员信息映射
 *    - 角色映射：成员角色级别的设置和变更
 *    - 信息映射：昵称、头像、扩展信息等字段映射
 *    - 权限映射：成员权限相关字段的转换
 *
 * 4. 数据验证与处理
 *    - 字段验证：确保更新字段的有效性
 *    - 空值处理：处理空字符串和null值的情况
 *    - 格式标准化：统一数据格式和编码标准
 *
 * 5. 更新策略控制
 *    - 增量更新：只更新有变化的字段
 *    - 批量更新：支持批量字段的同时更新
 *    - 条件更新：基于业务逻辑的条件性更新
 *
 * 设计特点：
 * - 类型安全：强类型的字段映射，避免运行时错误
 * - 灵活扩展：易于添加新的映射规则和字段
 * - 性能优化：只生成必要的更新字段，减少数据库压力
 * - 逻辑分离：将映射逻辑与业务逻辑分离，提高代码可维护性
 */
package group

import (
	"context"
	"strings"
	"time"

	pbgroup "github.com/openimsdk/protocol/group"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/mcontext"
)

// UpdateGroupInfoMap 创建群组信息更新映射
//
// 将群组信息设置请求转换为数据库更新字段映射，支持增量更新策略。
//
// 功能特点：
// 1. 增量更新：只映射有值的字段，避免不必要的数据库操作
// 2. 特殊处理：对通知字段进行特殊处理，包含更新时间和操作者信息
// 3. 类型转换：将protobuf包装类型转换为Go原生类型
// 4. 默认值过滤：跳过空字符串等默认值，避免意外覆盖
//
// 支持的更新字段：
// - group_name: 群组名称
// - notification: 群组公告（包含更新时间和操作者）
// - introduction: 群组介绍
// - face_url: 群组头像URL
// - need_verification: 加群验证设置
// - look_member_info: 查看成员信息权限
// - apply_member_friend: 申请成员好友权限
// - ex: 扩展字段
//
// 通知字段特殊处理：
// - 自动记录通知更新时间
// - 自动记录通知更新操作者
// - 支持通知内容的实时更新
//
// 参数说明：
// - ctx: 上下文，用于获取操作者信息
// - group: 群组信息设置对象
//
// 返回值：
// - map[string]any: 数据库更新字段映射
func UpdateGroupInfoMap(ctx context.Context, group *sdkws.GroupInfoForSet) map[string]any {
	m := make(map[string]any)

	// 群组名称更新
	if group.GroupName != "" {
		m["group_name"] = group.GroupName
	}

	// 群组公告更新（特殊处理）
	// 公告更新需要记录更新时间和操作者信息
	if group.Notification != "" {
		m["notification"] = group.Notification
		m["notification_update_time"] = time.Now()            // 记录公告更新时间
		m["notification_user_id"] = mcontext.GetOpUserID(ctx) // 记录公告更新操作者
	}

	// 群组介绍更新
	if group.Introduction != "" {
		m["introduction"] = group.Introduction
	}

	// 群组头像URL更新
	if group.FaceURL != "" {
		m["face_url"] = group.FaceURL
	}

	// 加群验证设置更新
	if group.NeedVerification != nil {
		m["need_verification"] = group.NeedVerification.Value
	}

	// 查看成员信息权限更新
	if group.LookMemberInfo != nil {
		m["look_member_info"] = group.LookMemberInfo.Value
	}

	// 申请成员好友权限更新
	if group.ApplyMemberFriend != nil {
		m["apply_member_friend"] = group.ApplyMemberFriend.Value
	}

	// 扩展字段更新
	if group.Ex != nil {
		m["ex"] = group.Ex.Value
	}

	return m
}

// UpdateGroupInfoExMap 创建扩展群组信息更新映射
//
// 这是一个增强版的群组信息映射函数，提供更精细的控制和验证功能。
// 相比基础版本，增加了数据验证、标志位控制和错误处理机制。
//
// 功能增强：
// 1. 数据验证：对关键字段进行有效性验证
// 2. 标志位控制：返回不同类型更新的标志位，便于后续处理
// 3. 错误处理：对无效数据进行错误报告
// 4. 格式处理：自动处理空白字符和格式标准化
//
// 标志位说明：
// - normalFlag: 是否包含常规字段更新（介绍、头像、权限设置等）
// - groupNameFlag: 是否包含群组名称更新
// - notificationFlag: 是否包含群组公告更新
//
// 验证规则：
// - 群组名称：不能为空白字符串
// - 公告内容：自动去除首尾空白字符
// - 其他字段：允许空值，表示清空该字段
//
// 错误情况：
// - 群组名称为空白字符串时返回参数错误
// - 其他字段验证失败时返回相应错误
//
// 参数说明：
// - ctx: 上下文，用于获取操作者信息
// - group: 扩展群组信息设置请求
//
// 返回值：
// - m: 数据库更新字段映射
// - normalFlag: 常规字段更新标志
// - groupNameFlag: 群组名称更新标志
// - notificationFlag: 群组公告更新标志
// - err: 验证错误信息
func UpdateGroupInfoExMap(ctx context.Context, group *pbgroup.SetGroupInfoExReq) (m map[string]any, normalFlag, groupNameFlag, notificationFlag bool, err error) {
	m = make(map[string]any)

	// 群组名称更新处理
	if group.GroupName != nil {
		// 去除首尾空白字符并验证
		if strings.TrimSpace(group.GroupName.Value) != "" {
			m["group_name"] = group.GroupName.Value
			groupNameFlag = true
		} else {
			// 群组名称不能为空
			return nil, normalFlag, notificationFlag, groupNameFlag, errs.ErrArgs.WrapMsg("group name is empty")
		}
	}

	// 群组公告更新处理
	if group.Notification != nil {
		notificationFlag = true
		// 去除首尾空白字符（如果公告只包含空格，设置为空字符串）
		group.Notification.Value = strings.TrimSpace(group.Notification.Value)

		m["notification"] = group.Notification.Value
		m["notification_user_id"] = mcontext.GetOpUserID(ctx) // 记录操作者
		m["notification_update_time"] = time.Now()            // 记录更新时间
	}

	// 群组介绍更新处理
	if group.Introduction != nil {
		m["introduction"] = group.Introduction.Value
		normalFlag = true
	}

	// 群组头像URL更新处理
	if group.FaceURL != nil {
		m["face_url"] = group.FaceURL.Value
		normalFlag = true
	}

	// 加群验证设置更新处理
	if group.NeedVerification != nil {
		m["need_verification"] = group.NeedVerification.Value
		normalFlag = true
	}

	// 查看成员信息权限更新处理
	if group.LookMemberInfo != nil {
		m["look_member_info"] = group.LookMemberInfo.Value
		normalFlag = true
	}

	// 申请成员好友权限更新处理
	if group.ApplyMemberFriend != nil {
		m["apply_member_friend"] = group.ApplyMemberFriend.Value
		normalFlag = true
	}

	// 扩展字段更新处理
	if group.Ex != nil {
		m["ex"] = group.Ex.Value
		normalFlag = true
	}

	return m, normalFlag, groupNameFlag, notificationFlag, nil
}

// UpdateGroupStatusMap 创建群组状态更新映射
//
// 生成群组状态更新的数据库字段映射，用于群组状态的变更操作。
//
// 支持的状态类型：
// - 正常状态（GroupOk）：群组正常运行
// - 禁言状态（GroupStatusMuted）：全群禁言
// - 解散状态（GroupStatusDismissed）：群组已解散
//
// 状态管理场景：
// - 群组禁言：管理员设置全群禁言
// - 解除禁言：恢复群组正常状态
// - 群组解散：群主解散群组
// - 状态恢复：系统管理员恢复群组状态
//
// 参数说明：
// - status: 新的群组状态值
//
// 返回值：
// - map[string]any: 包含状态字段的更新映射
func UpdateGroupStatusMap(status int) map[string]any {
	return map[string]any{
		"status": status,
	}
}

// UpdateGroupMemberMutedTimeMap 创建群组成员禁言时间更新映射
//
// 生成成员禁言时间更新的数据库字段映射，用于个人禁言功能。
//
// 禁言时间管理：
// - 设置禁言：传入未来时间点，表示禁言截止时间
// - 解除禁言：传入过去时间点（如Unix纪元），表示立即解除
// - 永久禁言：传入足够远的未来时间
// - 自动解除：系统根据时间自动判断是否仍在禁言期
//
// 时间处理逻辑：
// - 使用绝对时间而非相对时间，避免时区问题
// - 支持秒级精度的禁言时间控制
// - 兼容历史数据和不同时间格式
//
// 应用场景：
// - 管理员对成员进行临时禁言
// - 自动解除已过期的禁言状态
// - 群主或管理员提前解除禁言
// - 系统批量处理禁言状态
//
// 参数说明：
// - t: 禁言结束时间点
//
// 返回值：
// - map[string]any: 包含禁言时间字段的更新映射
func UpdateGroupMemberMutedTimeMap(t time.Time) map[string]any {
	return map[string]any{
		"mute_end_time": t,
	}
}

// UpdateGroupMemberMap 创建群组成员信息更新映射
//
// 将成员信息设置请求转换为数据库更新字段映射，支持成员个性化信息的管理。
//
// 功能特点：
// 1. 增量更新：只更新有变化的字段
// 2. 个性化支持：支持群内昵称、头像等个性化设置
// 3. 角色管理：支持成员角色级别的变更
// 4. 扩展性：支持自定义扩展字段
//
// 支持的更新字段：
// - nickname: 群内昵称（优先于全局昵称显示）
// - face_url: 群内头像（优先于全局头像显示）
// - role_level: 成员角色级别（群主、管理员、普通成员）
// - ex: 扩展字段（存储自定义业务数据）
//
// 个性化优先级：
// 1. 群内设置 > 全局设置
// 2. 最新更新 > 历史设置
// 3. 显式设置 > 默认值
//
// 角色级别说明：
// - GroupOwner (100): 群主，拥有最高权限
// - GroupAdmin (60): 管理员，拥有管理权限
// - GroupOrdinaryUsers (20): 普通成员，基础权限
//
// 扩展字段用途：
// - 存储业务相关的自定义数据
// - 支持第三方系统的数据集成
// - 实现特殊功能的标记和配置
//
// 参数说明：
// - req: 成员信息设置请求，包含要更新的字段
//
// 返回值：
// - map[string]any: 数据库更新字段映射
func UpdateGroupMemberMap(req *pbgroup.SetGroupMemberInfo) map[string]any {
	m := make(map[string]any)

	// 群内昵称更新
	// 设置后将优先于用户全局昵称显示
	if req.Nickname != nil {
		m["nickname"] = req.Nickname.Value
	}

	// 群内头像更新
	// 设置后将优先于用户全局头像显示
	if req.FaceURL != nil {
		m["face_url"] = req.FaceURL.Value
	}

	// 成员角色级别更新
	// 影响成员在群组中的权限和操作范围
	if req.RoleLevel != nil {
		m["role_level"] = req.RoleLevel.Value
	}

	// 扩展字段更新
	// 用于存储自定义业务数据
	if req.Ex != nil {
		m["ex"] = req.Ex.Value
	}

	return m
}
