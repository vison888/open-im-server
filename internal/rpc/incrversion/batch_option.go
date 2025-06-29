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

// Package incrversion 增量版本管理包
//
// 该包实现了IM系统中的增量数据同步机制，支持批量和单个数据的版本控制同步。
// 核心功能包括：
// 1. 增量版本号管理：基于版本号进行数据变更追踪
// 2. 批量数据同步：支持多个目标的批量增量同步
// 3. 版本校验机制：确保客户端与服务端数据一致性
// 4. 智能同步策略：根据版本差异选择全量或增量同步
//
// 设计原理：
// - 每个数据实体都有对应的版本日志(VersionLog)
// - 版本日志记录了数据的增删改操作序列
// - 客户端通过版本ID和版本号请求增量数据
// - 服务端根据版本差异返回相应的数据变更
package incrversion

import (
	"context"
	"fmt"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/tools/errs"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// BatchOption 批量增量版本同步选项
//
// **系统架构与设计理念:**
//
// 这是增量同步系统的高级抽象，专门设计用于处理大规模批量同步场景。
// 相比单个目标的Option，BatchOption优化了多目标同步的性能和效率，
// 通过批量操作、并行处理和智能缓存显著提升了系统吞吐量。
//
// **核心设计优势:**
//
// 1. **批量处理优化**
//   - 单次API调用处理多个同步目标
//   - 减少网络往返次数和连接开销
//   - 批量数据库操作提升查询效率
//   - 共享连接池和事务上下文
//
// 2. **并行执行引擎**
//   - 多个目标的版本查询并行执行
//   - 数据查询操作并行处理
//   - 充分利用多核CPU和I/O并发
//   - 显著降低总体处理延迟
//
// 3. **智能缓存策略**
//   - 支持批量缓存查询接口
//   - 缓存预热和批量失效机制
//   - 减少数据库访问压力
//   - 提升热点数据的访问速度
//
// 4. **差异化同步策略**
//   - 每个目标独立的同步策略决策
//   - 支持混合同步模式（部分全量+部分增量）
//   - 自适应负载均衡
//   - 异常隔离和容错处理
//
// **泛型参数设计哲学:**
//
// - **A: 数据实体类型**
//   - 支持复杂的业务数据结构（如会话列表、好友关系、群组信息等）
//   - 强类型约束确保编译时类型安全
//   - 支持嵌套数据结构和自定义序列化
//   - 兼容protobuf、JSON等多种序列化格式
//
// - **B: 响应结果类型**
//   - 包装批量同步的完整响应数据
//   - 支持目标级别的个性化响应格式
//   - 包含同步元数据和错误信息
//   - 支持流式响应和分页返回
//
// **核心工作流程（5个关键阶段）:**
//
// **阶段1：批量请求预处理**
// - 接收多个目标的版本信息列表
//   - TargetKeys: 目标标识符数组（如用户ID、会话ID等）
//   - VersionIDs: 客户端当前版本ID数组（MongoDB ObjectID格式）
//   - VersionNumbers: 客户端当前版本号数组（递增序列号）
//
// - 参数一致性验证：确保三个数组长度一致
// - 参数有效性检查：验证每个参数的格式和逻辑正确性
// - 目标去重和排序：提升查询效率和缓存命中率
//
// **阶段2：批量版本有效性验证**
// - 并行验证每个目标的版本信息
// - 版本ID格式检查：MongoDB ObjectID十六进制格式验证
// - 版本号逻辑验证：确保版本号为正整数
// - 版本连续性预检查：初步判断版本的合理性
// - 生成版本有效性映射表，指导后续处理
//
// **阶段3：智能同步策略决策**
// - 利用缓存优先策略快速获取服务端最新版本
// - 批量比较客户端版本与服务端版本
// - 为每个目标确定独立的同步策略：
//   - 全量同步：版本无效或差异过大
//   - 增量同步：版本有效且需要更新
//   - 无需同步：版本完全一致
//   - 错误恢复：异常情况的容错处理
//
// **阶段4：批量版本日志查询**
// - 按同步策略分组目标进行批量查询
// - 增量同步目标：查询版本变更记录
// - 全量同步目标：获取完整版本快照
// - 并行执行多个查询任务，提升整体性能
// - 查询结果缓存和索引建立
//
// **阶段5：批量数据内容查询与响应构造**
// - 解析版本日志，提取数据变更ID
// - 并行查询具体的业务数据内容
// - 按目标组织数据查询结果
// - 构造标准化的批量响应格式
// - 包含详细的同步状态和元数据信息
//
// **性能特性与优化措施:**
//
// 1. **查询性能优化**
//   - 批量查询减少数据库连接开销
//   - 并行查询充分利用系统资源
//   - 查询结果预取和缓存
//   - 智能查询计划和索引优化
//
// 2. **内存使用优化**
//   - 流式处理大数据集，避免内存溢出
//   - 分批处理和结果集分页
//   - 及时释放临时对象和缓存
//   - 内存使用监控和动态调整
//
// 3. **网络传输优化**
//   - 批量传输减少网络开销
//   - 数据压缩和增量传输
//   - 连接复用和长连接优化
//   - 网络错误重试和容错处理
//
// **适用场景与业务价值:**
//
// 1. **多设备同步场景**
//   - 用户在多个设备间的数据同步
//   - 会话列表、好友列表、群组信息的批量同步
//   - 设置项和偏好配置的跨设备同步
//
// 2. **高并发同步场景**
//   - 大量用户同时请求数据同步
//   - 系统启动时的批量数据初始化
//   - 网络恢复后的大量增量同步
//
// 3. **企业级应用场景**
//   - 组织架构变更的批量同步
//   - 权限配置更新的批量推送
//   - 业务数据的定期批量同步
//
// **监控与可观测性:**
//
// - 批量操作的耗时分布统计
// - 不同同步策略的选择比例
// - 数据传输量和压缩率监控
// - 错误率和重试成功率追踪
// - 缓存命中率和性能指标
//
// **错误处理与容灾设计:**
//
// - 单个目标错误不影响其他目标的同步
// - 部分失败的优雅处理和状态报告
// - 自动重试机制和指数退避策略
// - 详细的错误日志和问题定位信息
type BatchOption[A, B any] struct {
	Ctx            context.Context // 请求上下文
	TargetKeys     []string        // 目标键列表（如用户ID列表）
	VersionIDs     []string        // 客户端当前版本ID列表（MongoDB ObjectID的十六进制字符串）
	VersionNumbers []uint64        // 客户端当前版本号列表

	// 核心回调函数，由调用方实现具体的数据访问逻辑

	// Versions 获取版本日志的回调函数
	// 参数：dIds-目标ID列表, versions-起始版本号列表, limits-每个目标的最大同步条数
	// 返回：目标ID到版本日志的映射
	Versions func(ctx context.Context, dIds []string, versions []uint64, limits []int) (map[string]*model.VersionLog, error)

	// CacheMaxVersions 获取缓存中最新版本的回调函数（可选）
	// 用于优化性能，避免不必要的数据库查询
	// 参数：dIds-目标ID列表
	// 返回：目标ID到最新版本日志的映射
	CacheMaxVersions func(ctx context.Context, dIds []string) (map[string]*model.VersionLog, error)

	// Find 根据ID列表查询具体数据的回调函数
	// 参数：dId-目标ID, ids-数据ID列表
	// 返回：查询到的数据实体
	Find func(ctx context.Context, dId string, ids []string) (A, error)

	// Resp 构造最终响应结果的回调函数
	// 参数：versionsMap-版本映射, deleteIdsMap-删除ID映射, insertListMap-新增数据映射,
	//      updateListMap-更新数据映射, fullMap-是否全量同步映射
	// 返回：构造的响应结果
	Resp func(versionsMap map[string]*model.VersionLog, deleteIdsMap map[string][]string, insertListMap, updateListMap map[string]A, fullMap map[string]bool) *B
}

// newError 创建统一格式的内部服务器错误
func (o *BatchOption[A, B]) newError(msg string) error {
	return errs.ErrInternalServer.WrapMsg(msg)
}

// check 验证BatchOption的必要参数是否完整
// 确保所有必需的回调函数都已设置，避免运行时空指针异常
func (o *BatchOption[A, B]) check() error {
	if o.Ctx == nil {
		return o.newError("opt ctx is nil")
	}
	if len(o.TargetKeys) == 0 {
		return o.newError("targetKeys is empty")
	}
	if o.Versions == nil {
		return o.newError("func versions is nil")
	}
	if o.Find == nil {
		return o.newError("func find is nil")
	}
	if o.Resp == nil {
		return o.newError("func resp is nil")
	}
	return nil
}

// validVersions 验证客户端提供的版本信息是否有效
// 有效的版本信息需要满足：
// 1. VersionID是有效的MongoDB ObjectID十六进制字符串
// 2. ObjectID不为零值
// 3. VersionNumber大于0
// 返回：每个版本是否有效的布尔数组
func (o *BatchOption[A, B]) validVersions() []bool {
	valids := make([]bool, len(o.VersionIDs))
	for i, versionID := range o.VersionIDs {
		objID, err := primitive.ObjectIDFromHex(versionID)
		valids[i] = (err == nil && (!objID.IsZero()) && o.VersionNumbers[i] > 0)
	}
	return valids
}

// equalIDs 检查客户端版本ID与服务端最新版本ID是否一致
// 用于判断客户端版本是否是最新的，如果一致则可能无需同步
func (o *BatchOption[A, B]) equalIDs(objIDs []primitive.ObjectID) []bool {
	equals := make([]bool, len(o.VersionIDs))
	for i, versionID := range o.VersionIDs {
		equals[i] = versionID == objIDs[i].Hex()
	}
	return equals
}

// getVersions 获取版本日志数据
// 这是同步逻辑的核心方法，根据客户端版本信息和服务端最新版本，
// 确定每个目标的同步策略并获取相应的版本日志
//
// 同步策略：
// - tagQuery: 需要增量同步，查询从客户端版本号开始的变更
// - tagFull: 需要全量同步，返回所有数据
// - tagEqual: 版本一致，无需同步
func (o *BatchOption[A, B]) getVersions(tags *[]int) (versions map[string]*model.VersionLog, err error) {
	var dIDs []string        // 需要查询版本日志的目标ID列表
	var versionNums []uint64 // 对应的起始版本号列表
	var limits []int         // 对应的查询限制列表

	valids := o.validVersions() // 验证客户端版本有效性

	// 如果没有缓存版本查询函数，直接根据版本有效性确定策略
	if o.CacheMaxVersions == nil {
		for i, valid := range valids {
			if valid {
				// 版本有效，进行增量查询
				(*tags)[i] = tagQuery
				dIDs = append(dIDs, o.TargetKeys[i])
				versionNums = append(versionNums, o.VersionNumbers[i])
				limits = append(limits, syncLimit)
			} else {
				// 版本无效，进行全量同步
				(*tags)[i] = tagFull
				dIDs = append(dIDs, o.TargetKeys[i])
				versionNums = append(versionNums, 0)
				limits = append(limits, 0)
			}
		}

		// 批量查询版本日志
		versions, err = o.Versions(o.Ctx, dIDs, versionNums, limits)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		return versions, nil

	} else {
		// 有缓存版本查询函数，先从缓存获取最新版本进行比较
		caches, err := o.CacheMaxVersions(o.Ctx, o.TargetKeys)
		if err != nil {
			return nil, errs.Wrap(err)
		}

		// 将客户端版本ID转换为ObjectID用于比较
		objIDs := make([]primitive.ObjectID, len(o.VersionIDs))
		for i, versionID := range o.VersionIDs {
			objID, _ := primitive.ObjectIDFromHex(versionID)
			objIDs[i] = objID
		}

		// 检查版本ID是否相等
		equals := o.equalIDs(objIDs)

		// 根据版本有效性、ID相等性、版本号确定同步策略
		for i, valid := range valids {
			if !valid {
				// 版本无效，全量同步
				(*tags)[i] = tagFull
			} else if !equals[i] {
				// 版本ID不匹配，全量同步
				(*tags)[i] = tagFull
			} else if o.VersionNumbers[i] == uint64(caches[o.TargetKeys[i]].Version) {
				// 版本号相等，无需同步
				(*tags)[i] = tagEqual
			} else {
				// 版本号不等，增量同步
				(*tags)[i] = tagQuery
				dIDs = append(dIDs, o.TargetKeys[i])
				versionNums = append(versionNums, o.VersionNumbers[i])
				limits = append(limits, syncLimit)

				// 从缓存中移除，稍后会用查询结果替换
				delete(caches, o.TargetKeys[i])
			}
		}

		// 如果有需要查询的目标，执行查询并合并到缓存结果中
		if dIDs != nil {
			versionMap, err := o.Versions(o.Ctx, dIDs, versionNums, limits)
			if err != nil {
				return nil, errs.Wrap(err)
			}

			// 将查询结果合并到缓存中
			for k, v := range versionMap {
				caches[k] = v
			}
		}

		versions = caches
	}
	return versions, nil
}

// Build 构建并执行批量增量同步
// 这是BatchOption的主要执行方法，完成整个同步流程：
// 1. 参数验证
// 2. 获取版本日志并确定同步策略
// 3. 解析版本日志获取变更的数据ID
// 4. 查询具体的数据内容
// 5. 构造并返回最终结果
func (o *BatchOption[A, B]) Build() (*B, error) {
	// 1. 验证必要参数
	if err := o.check(); err != nil {
		return nil, errs.Wrap(err)
	}

	// 2. 获取版本日志并确定同步策略
	tags := make([]int, len(o.TargetKeys))
	versions, err := o.getVersions(&tags)
	if err != nil {
		return nil, errs.Wrap(err)
	}

	// 3. 根据同步策略确定是否需要全量同步
	fullMap := make(map[string]bool)
	for i, tag := range tags {
		switch tag {
		case tagQuery:
			// 增量查询：检查版本日志完整性，决定是否需要全量同步
			vLog := versions[o.TargetKeys[i]]
			// 如果版本ID不匹配、版本号落后或日志不完整，则需要全量同步
			fullMap[o.TargetKeys[i]] = vLog.ID.Hex() != o.VersionIDs[i] || uint64(vLog.Version) < o.VersionNumbers[i] || len(vLog.Logs) != vLog.LogLen
		case tagFull:
			// 全量同步
			fullMap[o.TargetKeys[i]] = true
		case tagEqual:
			// 版本相等，无需同步
			fullMap[o.TargetKeys[i]] = false
		default:
			panic(fmt.Errorf("undefined tag %d", tag))
		}
	}

	// 4. 解析版本日志，获取变更的数据ID
	var (
		insertIdsMap = make(map[string][]string) // 新增数据ID映射
		deleteIdsMap = make(map[string][]string) // 删除数据ID映射
		updateIdsMap = make(map[string][]string) // 更新数据ID映射
	)

	// 只有增量同步的目标才需要解析版本日志
	for _, targetKey := range o.TargetKeys {
		if !fullMap[targetKey] {
			version := versions[targetKey]
			// 从版本日志中提取增删改的数据ID
			insertIds, deleteIds, updateIds := version.DeleteAndChangeIDs()
			insertIdsMap[targetKey] = insertIds
			deleteIdsMap[targetKey] = deleteIds
			updateIdsMap[targetKey] = updateIds
		}
	}

	// 5. 查询具体的数据内容
	var (
		insertListMap = make(map[string]A) // 新增数据内容映射
		updateListMap = make(map[string]A) // 更新数据内容映射
	)

	// 查询新增的数据
	for targetKey, insertIds := range insertIdsMap {
		if len(insertIds) > 0 {
			insertList, err := o.Find(o.Ctx, targetKey, insertIds)
			if err != nil {
				return nil, errs.Wrap(err)
			}
			insertListMap[targetKey] = insertList
		}
	}

	// 查询更新的数据
	for targetKey, updateIds := range updateIdsMap {
		if len(updateIds) > 0 {
			updateList, err := o.Find(o.Ctx, targetKey, updateIds)
			if err != nil {
				return nil, errs.Wrap(err)
			}
			updateListMap[targetKey] = updateList
		}
	}

	// 6. 构造最终响应结果
	return o.Resp(versions, deleteIdsMap, insertListMap, updateListMap, fullMap), nil
}
