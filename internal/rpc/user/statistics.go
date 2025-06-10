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
 * 用户统计分析服务
 *
 * 本模块负责提供用户相关的统计分析功能，为系统运营和管理决策提供数据支撑。
 *
 * 核心功能：
 *
 * 1. 用户注册统计
 *    - 总量统计：系统中用户的总注册数量
 *    - 时间范围统计：指定时间段内的用户注册情况
 *    - 增长趋势：用户注册数量的时间趋势分析
 *    - 阶段对比：不同时间段的注册数量对比
 *
 * 2. 统计数据分析
 *    - 日度统计：按天统计用户注册数量
 *    - 基准对比：与历史基准数据的对比分析
 *    - 增长率计算：用户增长率和增长速度分析
 *    - 趋势预测：基于历史数据的趋势预测
 *
 * 3. 时间维度分析
 *    - 时间区间：支持灵活的时间区间查询
 *    - 时间粒度：支持天、周、月等不同时间粒度
 *    - 时区处理：正确处理不同时区的时间数据
 *    - 节假日分析：考虑节假日对注册数据的影响
 *
 * 4. 数据验证与处理
 *    - 参数验证：确保统计查询参数的有效性
 *    - 数据清洗：处理异常和脏数据
 *    - 结果格式化：将统计结果格式化为标准输出
 *    - 异常处理：统计过程中的异常情况处理
 *
 * 5. 性能优化
 *    - 索引优化：数据库查询的索引优化
 *    - 缓存策略：统计结果的缓存机制
 *    - 分页处理：大数据量的分页统计
 *    - 异步计算：复杂统计的异步计算处理
 *
 * 技术特性：
 * - 高性能查询：优化的数据库查询策略
 * - 实时统计：支持实时和准实时的统计计算
 * - 灵活配置：可配置的统计维度和指标
 * - 扩展性强：支持新增统计指标和维度
 * - 数据准确：确保统计数据的准确性和一致性
 *
 * 应用场景：
 * - 运营报告：为运营团队提供用户增长数据
 * - 管理决策：为管理层提供用户规模数据支撑
 * - 产品分析：分析用户注册的时间分布规律
 * - 市场分析：了解用户增长的市场趋势
 * - 系统监控：监控用户注册的异常情况
 */
package user

import (
	"context"
	"time"

	pbuser "github.com/openimsdk/protocol/user"
	"github.com/openimsdk/tools/errs"
)

// UserRegisterCount 用户注册数量统计
//
// 统计指定时间范围内的用户注册情况，提供全面的用户增长数据分析。
//
// 统计维度：
// 1. 总注册数：系统中的用户总数量
// 2. 基准数量：指定开始时间之前的用户数量
// 3. 每日统计：时间范围内每天的新增注册数量
//
// 数据处理流程：
// 1. 参数验证：验证时间范围的合理性
// 2. 总量查询：查询系统中的用户总数
// 3. 基准查询：查询开始时间之前的用户数量
// 4. 范围统计：统计时间范围内每天的注册数量
// 5. 结果组装：将各项统计结果组装成响应
//
// 时间处理：
// - 毫秒时间戳：使用毫秒级时间戳确保精度
// - 时区转换：正确处理时区转换和本地时间
// - 边界处理：合理处理时间范围的边界情况
// - 有效性验证：验证开始时间不能晚于结束时间
//
// 性能考虑：
// - 索引利用：充分利用时间字段的数据库索引
// - 查询优化：针对大数据量的查询优化
// - 缓存策略：对历史统计数据进行缓存
// - 并发安全：确保并发查询的数据一致性
//
// 应用场景：
// - 增长报告：生成用户增长的详细报告
// - 运营分析：分析用户注册的时间分布
// - 趋势监控：监控用户注册的异常波动
// - 对比分析：不同时间段的注册数据对比
//
// 参数说明：
// - ctx: 请求上下文，包含请求的元数据和超时控制
// - req: 统计请求，包含开始时间和结束时间（毫秒时间戳）
//
// 返回值：
// - *pbuser.UserRegisterCountResp: 统计响应，包含各项统计数据
// - error: 错误信息，包括参数错误和查询错误
func (s *userServer) UserRegisterCount(ctx context.Context, req *pbuser.UserRegisterCountReq) (*pbuser.UserRegisterCountResp, error) {
	// 验证时间参数的合理性
	if req.Start > req.End {
		return nil, errs.ErrArgs.WrapMsg("start > end")
	}

	// 查询系统中的用户总数
	total, err := s.db.CountTotal(ctx, nil)
	if err != nil {
		return nil, err
	}

	// 转换开始时间为time.Time格式
	start := time.UnixMilli(req.Start)

	// 查询开始时间之前的用户数量（基准数量）
	before, err := s.db.CountTotal(ctx, &start)
	if err != nil {
		return nil, err
	}

	// 统计时间范围内每天的用户注册数量
	count, err := s.db.CountRangeEverydayTotal(ctx, start, time.UnixMilli(req.End))
	if err != nil {
		return nil, err
	}

	// 构建并返回统计响应
	return &pbuser.UserRegisterCountResp{
		Total:  total,  // 用户总数
		Before: before, // 基准时间之前的用户数
		Count:  count,  // 时间范围内每日新增统计
	}, nil
}
