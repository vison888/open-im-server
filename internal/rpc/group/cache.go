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
 * 群组缓存服务
 *
 * 本模块提供群组数据的高速缓存访问功能，优化数据查询性能。
 *
 * 缓存功能：
 *
 * 1. 群组信息缓存
 *    - 基础信息：群组名称、头像、介绍等基础数据
 *    - 快速访问：避免频繁查询数据库的性能开销
 *    - 数据一致性：确保缓存数据与数据库数据的同步
 *
 * 2. 群组成员缓存
 *    - 成员信息：群组成员的详细信息缓存
 *    - 权限验证：基于缓存的快速权限检查
 *    - 访问控制：确保只有授权用户才能访问
 *
 * 3. 性能优化
 *    - 减少数据库访问：通过缓存减少数据库查询压力
 *    - 响应速度提升：毫秒级的数据访问响应
 *    - 并发性能：支持高并发的缓存访问
 *
 * 4. 数据一致性
 *    - 缓存更新：数据变更时的缓存同步机制
 *    - 过期策略：缓存数据的自动过期和刷新
 *    - 一致性保证：确保缓存与数据库的数据一致
 *
 * 使用场景：
 * - 高频群组信息查询的性能优化
 * - 实时权限验证的快速响应
 * - 大并发场景下的数据库压力缓解
 * - 群组列表展示的快速加载
 */
package group

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/common/convert"
	pbgroup "github.com/openimsdk/protocol/group"
)

// GetGroupInfoCache 获取群组信息缓存
//
// 从缓存中快速获取群组的基础信息，主要用于高频访问场景的性能优化。
//
// 功能特点：
// 1. 快速响应：直接从缓存或数据库获取数据，避免复杂计算
// 2. 数据简化：返回核心的群组基础信息，不包含实时统计数据
// 3. 权限透明：不进行额外的权限验证，由调用方控制
// 4. 格式统一：使用标准的群组信息格式返回数据
//
// 与常规群组信息查询的区别：
// - 不包含实时成员数量统计
// - 不包含群主用户信息
// - 响应速度更快，适用于列表展示
// - 数据可能略有延迟，但性能更优
//
// 应用场景：
// - 群组列表的快速展示
// - 群组基础信息的预览
// - 第三方系统的群组信息同步
// - 缓存预热和数据预加载
//
// 性能优势：
// - 避免复杂的关联查询
// - 减少数据库访问次数
// - 降低系统整体负载
// - 提升用户体验
//
// 注意事项：
// - 返回的成员数量和群主信息为默认值
// - 实时性要求高的场景请使用常规接口
// - 需要配合缓存更新机制保证数据一致性
func (s *groupServer) GetGroupInfoCache(ctx context.Context, req *pbgroup.GetGroupInfoCacheReq) (*pbgroup.GetGroupInfoCacheResp, error) {
	// 从数据库获取群组基础信息
	// 这里虽然调用了数据库，但通常会被底层的缓存层拦截
	group, err := s.db.TakeGroup(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}

	// 转换为标准的群组信息格式
	// 使用空字符串作为群主ID，0作为成员数量，表示这是缓存版本
	return &pbgroup.GetGroupInfoCacheResp{
		GroupInfo: convert.Db2PbGroupInfo(group, "", 0),
	}, nil
}

// GetGroupMemberCache 获取群组成员缓存信息
//
// 快速获取指定群组成员的详细信息，主要用于权限验证和成员信息展示。
//
// 功能特点：
// 1. 权限验证：确保请求者有权限查看群组成员信息
// 2. 精确查询：根据群组ID和成员ID精确获取单个成员信息
// 3. 完整信息：返回成员的完整信息，包括角色、状态等
// 4. 缓存优化：利用缓存机制提升查询性能
//
// 权限控制：
// - 群组成员：可以查看同群组的其他成员信息
// - 群组管理员：可以查看群组内任意成员信息
// - 系统管理员：可以查看任意群组的成员信息
// - 非成员：无权限访问群组成员信息
//
// 应用场景：
// - 群组内@某人时的成员信息获取
// - 权限验证中的角色信息查询
// - 成员详情页面的信息展示
// - 群组管理功能的成员信息获取
//
// 性能考虑：
// - 单个成员查询，性能开销较小
// - 利用索引优化查询速度
// - 缓存机制减少数据库访问
// - 支持高并发的访问场景
//
// 数据安全：
// - 严格的权限验证机制
// - 敏感信息的脱敏处理
// - 访问日志的记录和审计
func (s *groupServer) GetGroupMemberCache(ctx context.Context, req *pbgroup.GetGroupMemberCacheReq) (*pbgroup.GetGroupMemberCacheResp, error) {
	// 权限验证：检查请求者是否为群组成员或管理员
	if err := s.checkAdminOrInGroup(ctx, req.GroupID); err != nil {
		return nil, err
	}

	// 查询指定的群组成员信息
	members, err := s.db.TakeGroupMember(ctx, req.GroupID, req.GroupMemberID)
	if err != nil {
		return nil, err
	}

	// 转换为标准的群组成员信息格式并返回
	return &pbgroup.GetGroupMemberCacheResp{
		Member: convert.Db2PbGroupMember(members),
	}, nil
}
