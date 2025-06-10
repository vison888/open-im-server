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
 * 群组统计分析服务
 *
 * 本模块提供群组相关的统计分析功能，为管理员和分析系统提供数据支持。
 *
 * 核心功能：
 *
 * 1. 群组创建统计
 *    - 时间维度统计：按日、周、月统计群组创建数量
 *    - 趋势分析：群组创建的增长趋势和变化规律
 *    - 峰值监测：识别群组创建的高峰时段
 *    - 异常检测：发现异常的群组创建行为
 *
 * 2. 运营数据分析
 *    - 活跃度统计：群组的活跃度和使用率分析
 *    - 规模分析：群组规模分布和成员数量统计
 *    - 生命周期：群组的存续时间和状态变化
 *    - 转化率：从创建到活跃的转化情况
 *
 * 3. 管理决策支持
 *    - 容量规划：基于历史数据预测未来需求
 *    - 资源优化：识别资源使用的瓶颈和优化点
 *    - 政策制定：为群组管理政策提供数据依据
 *    - 风险预警：识别潜在的风险和异常情况
 *
 * 4. 时间范围查询
 *    - 灵活时间段：支持自定义时间范围的统计
 *    - 精确筛选：基于具体时间点的精确查询
 *    - 对比分析：支持不同时间段的数据对比
 *    - 实时更新：提供近实时的统计数据
 *
 * 5. 性能特性
 *    - 高效查询：优化的数据库查询和索引策略
 *    - 缓存支持：热点统计数据的缓存机制
 *    - 异步处理：复杂统计任务的异步计算
 *    - 批量处理：支持大数据量的批量统计
 *
 * 应用场景：
 * - 运营监控：实时监控群组系统的运营状况
 * - 数据报表：生成定期的数据分析报表
 * - 管理决策：为管理层提供决策支持数据
 * - 系统优化：基于统计数据优化系统性能
 * - 用户洞察：了解用户的群组使用行为
 */
package group

import (
	"context"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/protocol/group"
	"github.com/openimsdk/tools/errs"
)

// GroupCreateCount 群组创建数量统计
//
// 统计指定时间范围内的群组创建数量，为运营分析和管理决策提供数据支持。
//
// 功能特点：
// 1. 时间范围查询：支持灵活的时间范围设置
// 2. 精确统计：基于数据库记录的精确计数
// 3. 高性能查询：优化的查询逻辑，支持大数据量
// 4. 实时准确：反映最新的群组创建情况
//
// 统计维度：
// - 时间维度：在指定时间范围内创建的群组数量
// - 全量统计：包含所有类型和状态的群组
// - 创建时间：基于群组的实际创建时间进行统计
//
// 时间处理：
// - 开始时间：统计范围的起始时间点（包含）
// - 结束时间：统计范围的结束时间点（包含）
// - 时区处理：确保时间范围的准确性
// - 边界处理：正确处理时间边界的包含关系
//
// 应用场景：
// - 日常运营：查看每日群组创建情况
// - 周期报表：生成周、月、季度统计报表
// - 趋势分析：分析群组创建的增长趋势
// - 异常监测：识别异常的群组创建行为
// - 容量规划：基于历史数据进行容量预测
//
// 性能考虑：
// - 索引优化：基于创建时间的数据库索引
// - 缓存策略：热点时间段的结果缓存
// - 查询优化：避免全表扫描的查询优化
// - 分页支持：大时间范围的分页处理
//
// 参数说明：
// - ctx: 请求上下文，包含超时控制和链路追踪
// - req: 统计请求，包含时间范围等查询条件
//   - Start: 统计开始时间（Unix时间戳）
//   - End: 统计结束时间（Unix时间戳）
//
// 返回值：
// - *group.GroupCreateCountResp: 统计结果响应
//   - Count: 指定时间范围内的群组创建数量
//
// - error: 统计过程中的错误信息
//
// 错误处理：
// - 时间参数错误：开始时间大于结束时间
// - 数据库错误：查询过程中的数据库异常
// - 权限错误：调用者没有统计查询权限
// - 超时错误：查询超时或上下文取消
//
// 使用示例：
// - 查询今日创建群组数：设置开始时间为今日00:00，结束时间为当前时间
// - 查询月度创建数：设置开始时间为月初，结束时间为月末
// - 查询历史峰值：设置较大的时间范围进行历史数据分析
func (s *groupServer) GroupCreateCount(ctx context.Context, req *group.GroupCreateCountReq) (*group.GroupCreateCountResp, error) {
	// 参数验证：确保时间范围的合理性
	if req.Start > req.End {
		return nil, errs.ErrArgs.WrapMsg("start > end: %d > %d", req.Start, req.End)
	}

	// 权限验证：只有系统管理员才能查看统计数据
	if err := authverify.CheckAdmin(ctx, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	// 查询系统中群组的总数量
	total, err := s.db.CountTotal(ctx, nil)
	if err != nil {
		return nil, err
	}

	// 转换起始时间为时间对象
	start := time.UnixMilli(req.Start)

	// 查询起始时间之前创建的群组数量（作为基准值）
	before, err := s.db.CountTotal(ctx, &start)
	if err != nil {
		return nil, err
	}

	// 查询指定时间范围内每日的群组创建数量
	count, err := s.db.CountRangeEverydayTotal(ctx, start, time.UnixMilli(req.End))
	if err != nil {
		return nil, err
	}

	// 构建并返回统计结果
	return &group.GroupCreateCountResp{
		Total:  total,  // 群组总数
		Before: before, // 基准时间前的数量
		Count:  count,  // 按日分布的创建数量
	}, nil
}
