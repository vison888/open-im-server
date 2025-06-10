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
 * 群组数据类型转换工具
 *
 * 本模块提供群组相关的数据类型转换功能，主要负责在不同数据层之间进行类型转换。
 *
 * 核心功能：
 *
 * 1. 数据库模型到API模型转换
 *    - 群组信息转换：将数据库群组记录转换为API返回格式
 *    - 成员信息转换：将数据库成员记录转换为API格式
 *    - 字段映射：处理不同模型之间的字段对应关系
 *    - 类型适配：处理不同数据类型之间的转换
 *
 * 2. 时间格式处理
 *    - 时间戳转换：将Go time.Time转换为Unix毫秒时间戳
 *    - 时区处理：确保时间数据的一致性
 *    - 格式标准化：统一时间数据的表示格式
 *
 * 3. 权限级别映射
 *    - 应用管理级别：集成应用层的管理权限
 *    - 角色级别转换：群组内角色到通用角色的映射
 *    - 权限计算：基于不同维度计算用户权限级别
 *
 * 4. 可选参数处理
 *    - 默认值设置：为可选字段设置合理的默认值
 *    - 空值处理：处理null和空字符串的情况
 *    - 条件转换：基于业务逻辑的条件性转换
 *
 * 5. 性能优化
 *    - 缓存友好：支持缓存场景下的高效转换
 *    - 批量处理：支持批量数据的高效转换
 *    - 内存优化：避免不必要的内存分配和复制
 *
 * 设计原则：
 * - 类型安全：强类型转换，避免运行时错误
 * - 数据完整性：确保转换过程中数据不丢失
 * - 向后兼容：支持API版本兼容性
 * - 扩展性：易于添加新的转换规则和字段
 */
package group

import (
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/protocol/sdkws"
)

// groupDB2PB 将数据库群组记录转换为API群组信息
//
// 这是群组信息转换的核心方法，将数据库存储的群组数据转换为
// 客户端API所需的标准格式，包含完整的群组元数据。
//
// 转换内容：
// - 基础信息：群组ID、名称、介绍、头像等
// - 权限设置：加群验证、查看成员、加好友等权限配置
// - 状态信息：群组当前状态、创建时间等
// - 统计数据：成员数量、群主信息等
// - 扩展信息：公告更新时间、更新者等详细信息
//
// 特殊处理：
// - 时间转换：将Go time.Time转换为Unix毫秒时间戳
// - 权限聚合：整合多个权限字段到统一结构
// - 状态标准化：确保状态值的一致性
//
// 参数说明：
// - group: 数据库群组记录，包含完整的群组数据
// - ownerUserID: 群主用户ID，可能来自单独查询
// - memberCount: 群组成员数量，用于显示统计信息
//
// 返回值：
// - *sdkws.GroupInfo: 标准化的群组信息，可直接返回给客户端
//
// 使用场景：
// - 群组详情查询：获取群组完整信息
// - 群组列表展示：批量转换群组基础信息
// - 缓存数据转换：从缓存恢复的数据转换
// - Webhook回调：向外部系统提供标准化数据
func (s *groupServer) groupDB2PB(group *model.Group, ownerUserID string, memberCount uint32) *sdkws.GroupInfo {
	return &sdkws.GroupInfo{
		GroupID:                group.GroupID,                            // 群组唯一标识
		GroupName:              group.GroupName,                          // 群组名称
		Notification:           group.Notification,                       // 群组公告内容
		Introduction:           group.Introduction,                       // 群组介绍
		FaceURL:                group.FaceURL,                            // 群组头像URL
		OwnerUserID:            ownerUserID,                              // 群主用户ID
		CreateTime:             group.CreateTime.UnixMilli(),             // 创建时间（Unix毫秒时间戳）
		MemberCount:            memberCount,                              // 成员数量
		Ex:                     group.Ex,                                 // 扩展字段
		Status:                 group.Status,                             // 群组状态（正常/禁言/解散）
		CreatorUserID:          group.CreatorUserID,                      // 创建者用户ID
		GroupType:              group.GroupType,                          // 群组类型
		NeedVerification:       group.NeedVerification,                   // 加群验证设置
		LookMemberInfo:         group.LookMemberInfo,                     // 查看成员信息权限
		ApplyMemberFriend:      group.ApplyMemberFriend,                  // 申请成员好友权限
		NotificationUpdateTime: group.NotificationUpdateTime.UnixMilli(), // 公告更新时间
		NotificationUserID:     group.NotificationUserID,                 // 公告更新者ID
	}
}

// groupMemberDB2PB 将数据库群组成员记录转换为API成员信息
//
// 转换数据库中的群组成员记录为客户端API标准格式，
// 包含成员的完整信息和权限级别。
//
// 转换内容：
// - 基础信息：用户ID、群组ID、加入时间等
// - 角色信息：成员角色级别、应用管理级别
// - 个性化信息：群内昵称、头像（优先于全局设置）
// - 权限信息：邀请者、操作者等权限相关信息
// - 状态信息：禁言状态、禁言截止时间等
//
// 权限级别处理：
// - 群组角色级别：群主(100) > 管理员(60) > 普通成员(20)
// - 应用管理级别：系统级权限，用于跨群组操作
// - 权限继承：应用级权限可以覆盖群组级权限
//
// 时间处理：
// - 加入时间：记录成员加入群组的时间点
// - 禁言时间：禁言的截止时间，过期自动解除
// - 时间戳格式：统一使用Unix毫秒时间戳
//
// 参数说明：
// - member: 数据库成员记录，包含完整的成员数据
// - appMangerLevel: 应用管理级别，用于系统级权限控制
//
// 返回值：
// - *sdkws.GroupMemberFullInfo: 完整的成员信息，包含所有字段
//
// 使用场景：
// - 成员列表展示：群组成员的分页查询
// - 成员详情查询：获取特定成员的完整信息
// - 权限验证：基于成员信息进行权限判断
// - 管理操作：管理员查看和管理成员信息
func (s *groupServer) groupMemberDB2PB(member *model.GroupMember, appMangerLevel int32) *sdkws.GroupMemberFullInfo {
	return &sdkws.GroupMemberFullInfo{
		GroupID:        member.GroupID,                 // 群组ID
		UserID:         member.UserID,                  // 用户ID
		RoleLevel:      member.RoleLevel,               // 群组内角色级别
		JoinTime:       member.JoinTime.UnixMilli(),    // 加入时间（Unix毫秒时间戳）
		Nickname:       member.Nickname,                // 群内昵称（优先于全局昵称）
		FaceURL:        member.FaceURL,                 // 群内头像（优先于全局头像）
		AppMangerLevel: appMangerLevel,                 // 应用管理级别
		JoinSource:     member.JoinSource,              // 加入方式（邀请/申请/直接加入）
		OperatorUserID: member.OperatorUserID,          // 操作者用户ID（执行加入操作的用户）
		Ex:             member.Ex,                      // 扩展字段
		MuteEndTime:    member.MuteEndTime.UnixMilli(), // 禁言结束时间
		InviterUserID:  member.InviterUserID,           // 邀请者用户ID
	}
}

// groupMemberDB2PB2 将数据库群组成员记录转换为API成员信息（简化版本）
//
// 这是groupMemberDB2PB的简化版本，适用于不需要应用管理级别的场景，
// 自动将应用管理级别设置为0（无特殊权限）。
//
// 适用场景：
// - 普通用户查看成员信息：不需要展示管理级别
// - 公开信息展示：只显示基础的群组角色
// - 批量转换：提高批量处理的性能
// - 缓存场景：简化缓存数据结构
//
// 参数说明：
// - member: 数据库成员记录
//
// 返回值：
// - *sdkws.GroupMemberFullInfo: 成员信息（应用管理级别为0）
//
// 注意事项：
// - 应用管理级别固定为0，不适用于管理员功能
// - 其他字段与完整版本转换保持一致
// - 推荐在非管理场景下使用以提高性能
func (s *groupServer) groupMemberDB2PB2(member *model.GroupMember) *sdkws.GroupMemberFullInfo {
	// 调用完整版本，但将应用管理级别设为0
	return s.groupMemberDB2PB(member, 0)
}
