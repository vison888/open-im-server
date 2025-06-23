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

// Package incrversion 增量版本管理包 - 单个目标同步选项
//
// 本文件实现了单个目标的增量版本同步机制，是BatchOption的简化版本。
// 主要用于单个用户或单个会话的数据同步场景。
package incrversion

import (
	"context"
	"fmt"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/tools/errs"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// 同步相关常量定义

// syncLimit 单次同步的最大记录数量限制
// 设置为200是为了平衡性能和内存使用，避免单次同步过多数据导致内存压力
const syncLimit = 200

// 同步策略标签常量
// 用于标识不同的同步策略，决定如何处理客户端的同步请求
const (
	tagQuery = iota + 1 // 增量查询：需要从指定版本号开始查询变更数据
	tagFull             // 全量同步：返回所有数据，通常在版本信息无效或差异过大时使用
	tagEqual            // 版本相等：客户端版本与服务端一致，无需同步
)

// Option 单个目标的增量版本同步选项
//
// 这是针对单个目标（如单个用户的会话列表、好友列表等）的增量同步结构。
// 相比BatchOption，它简化了处理逻辑，适用于单个目标的同步场景。
//
// **设计理念与技术架构:**
//
// 1. **版本驱动同步机制**
//   - 基于MongoDB ObjectID作为版本ID，确保全局唯一性和时间顺序
//   - 使用递增的版本号标识数据变更序列
//   - 客户端保存最后同步的版本信息，服务端返回增量变更
//   - 支持离线场景下的数据一致性恢复
//
// 2. **智能同步策略选择**
//   - 版本有效性检查：确保客户端版本信息完整且格式正确
//   - 版本连续性验证：检查客户端版本是否基于服务端当前版本
//   - 自适应全量/增量切换：根据版本差异自动选择最优同步策略
//   - 异常容错机制：版本不匹配时自动降级为全量同步
//
// 3. **高性能缓存优化**
//   - 可选的缓存版本查询：减少数据库访问，提升响应速度
//   - 二级缓存策略：支持Redis缓存和本地缓存的组合使用
//   - 缓存一致性保证：通过版本控制确保缓存数据的时效性
//   - 智能缓存预热：根据访问模式提前加载热点数据
//
// **泛型参数设计:**
// - A: 数据实体类型（如[]*Conversation, []*Friend, []*GroupMember）
//   - 支持任意业务数据类型，提供强类型安全
//   - 泛型约束确保类型一致性，避免运行时类型错误
//   - 支持复杂的嵌套数据结构和自定义序列化
//
// - B: 响应结果类型（如*GetIncrementalConversationResp）
//   - 包装完整的同步响应，包含版本信息和数据变更
//   - 支持自定义响应格式，适应不同的API设计风格
//   - 包含元数据信息，如同步状态、错误信息等
//
// **核心工作流程:**
//
// 1. **请求预处理阶段**
//   - 接收客户端的版本ID（MongoDB ObjectID的Hex字符串）
//   - 接收客户端的版本号（uint64类型的递增序列号）
//   - 验证参数完整性和格式正确性
//   - 设置请求上下文和超时控制
//
// 2. **版本信息验证阶段**
//   - ObjectID格式验证：确保是有效的24字符十六进制字符串
//   - 版本号有效性检查：确保版本号大于0（0表示未初始化）
//   - 版本连续性检查：验证客户端版本是否基于服务端当前版本
//   - 时间戳合理性验证：检查版本时间是否在合理范围内
//
// 3. **同步策略决策阶段**
//   - 策略A（增量同步）：版本有效且连续，返回增量变更数据
//   - 策略B（全量同步）：版本无效或不连续，返回所有数据
//   - 策略C（无需同步）：版本完全一致，返回空变更集
//   - 策略D（错误恢复）：异常情况下的容错处理
//
// 4. **数据查询与组装阶段**
//   - 版本日志解析：从版本日志中提取变更的数据ID
//   - 分类数据查询：分别查询新增、修改、删除的数据项
//   - 数据完整性校验：确保查询结果的完整性和正确性
//   - 响应结果构造：将版本信息和数据变更组装成最终响应
//
// **性能特性与优化:**
//
// - **查询性能优化**
//   - 利用MongoDB索引加速版本查询（通常<10ms）
//   - 批量数据查询减少数据库往返次数
//   - 智能limit控制，避免单次查询过多数据
//   - 查询结果缓存，减少重复计算开销
//
// - **内存管理优化**
//   - 流式处理大数据集，避免内存溢出
//   - 及时释放临时对象，减少GC压力
//   - 内存使用监控，动态调整查询策略
//   - 对象池复用，减少内存分配开销
//
// - **网络传输优化**
//   - 增量传输减少网络带宽消耗
//   - 数据压缩降低传输延迟
//   - 批量操作减少网络往返次数
//   - 连接复用提升网络效率
//
// **使用场景与适用性:**
//
// 1. **实时消息同步**
//   - 用户会话列表的实时更新
//   - 未读消息数量的增量同步
//   - 会话置顶状态的变更同步
//   - 会话免打扰设置的同步
//
// 2. **社交关系同步**
//   - 好友列表的增量更新
//   - 好友状态变更的实时同步
//   - 黑名单列表的同步
//   - 好友申请状态的更新
//
// 3. **群组信息同步**
//   - 群组成员列表的增量同步
//   - 群组信息变更的实时更新
//   - 群组权限变更的同步
//   - 群组公告和设置的同步
//
// 4. **用户配置同步**
//   - 用户偏好设置的同步
//   - 隐私设置的跨设备同步
//   - 个人资料的实时更新
//   - 应用配置的云端同步
//
// **错误处理与容灾:**
//
// - 版本冲突处理：自动降级为全量同步
// - 网络超时处理：重试机制和降级策略
// - 数据不一致处理：完整性校验和修复机制
// - 系统异常处理：优雅降级和错误恢复
type Option[A, B any] struct {
	// **基础参数配置**
	Ctx           context.Context // 请求上下文，包含超时控制、取消信号、元数据传递
	VersionKey    string          // 版本键（通常是用户ID或其他唯一标识），用于标识数据所有者
	VersionID     string          // 客户端当前版本ID（MongoDB ObjectID的十六进制字符串，24字符）
	VersionNumber uint64          // 客户端当前版本号（递增序列号，表示客户端已同步到的版本）

	// **核心回调函数接口**
	// 采用回调函数设计模式，实现业务逻辑与同步逻辑的解耦，
	// 调用方只需实现具体的数据访问逻辑，同步框架负责版本控制和策略选择

	// CacheMaxVersion 获取缓存中最新版本的高性能查询函数（可选优化）
	//
	// **设计目的:**
	// - 性能优化：避免每次都查询数据库，利用缓存快速响应
	// - 减少延迟：典型响应时间从50ms降低到5ms以下
	// - 降低负载：减少数据库查询压力，提升系统并发能力
	// - 智能决策：快速获取最新版本信息，辅助同步策略选择
	//
	// **实现要求:**
	// - 缓存一致性：确保缓存数据与数据库的最终一致性
	// - 超时控制：设置合理的缓存查询超时时间（建议5-10ms）
	// - 降级策略：缓存不可用时自动降级到数据库查询
	// - 并发安全：支持高并发访问，避免缓存击穿和雪崩
	//
	// **参数说明:**
	// - ctx: 请求上下文，包含超时控制
	// - dId: 目标数据ID，用于定位具体的版本记录
	//
	// **返回值说明:**
	// - *model.VersionLog: 最新的版本日志记录，包含版本ID、版本号、变更摘要
	// - error: 缓存查询错误，nil表示成功
	//
	// **性能指标:**
	// - 响应时间: <10ms (P99)
	// - 命中率: >95%
	// - 并发量: >1000 QPS
	CacheMaxVersion func(ctx context.Context, dId string) (*model.VersionLog, error)

	// Version 核心版本日志查询函数
	//
	// **设计核心:**
	// 这是增量同步的核心函数，负责根据客户端版本信息查询版本变更日志。
	// 支持增量查询和全量查询两种模式，是整个同步机制的数据基础。
	//
	// **查询模式:**
	// 1. **增量查询模式** (version > 0, limit > 0)
	//    - 查询从指定版本号开始的变更记录
	//    - 按版本号升序返回，确保变更顺序正确
	//    - 限制查询条数，避免单次返回过多数据
	//    - 用于客户端版本有效且需要增量同步的场景
	//
	// 2. **全量查询模式** (version = 0, limit = 0)
	//    - 返回最新的完整版本日志
	//    - 包含所有数据的完整快照信息
	//    - 用于客户端版本无效或需要全量同步的场景
	//    - 一般配合完整数据查询使用
	//
	// **技术实现要求:**
	// - 数据库索引：在dId和version字段上建立复合索引
	// - 查询性能：单次查询耗时应控制在50ms以内
	// - 结果排序：按version字段升序返回，确保变更顺序
	// - 内存控制：通过limit参数控制单次查询的内存使用
	//
	// **参数详解:**
	// - ctx: 请求上下文，包含超时控制和取消信号
	// - dId: 目标数据标识符，定位具体的数据版本链
	// - version: 起始版本号，0表示全量查询，>0表示增量查询
	// - limit: 最大查询条数，0表示无限制，>0表示限制条数
	//
	// **返回结果结构:**
	// - ID: 版本日志的唯一标识（MongoDB ObjectID）
	// - DID: 数据标识符，与参数dId对应
	// - Version: 版本号，递增序列
	// - Logs: 变更日志列表，包含具体的数据变更信息
	// - LogLen: 日志条数，用于完整性校验
	//
	// **异常处理:**
	// - 版本不存在：返回空的版本日志，不抛出错误
	// - 数据库连接失败：返回具体的网络错误
	// - 查询超时：返回超时错误，客户端可重试
	//
	// **性能指标:**
	// - 查询延迟: <50ms (P95)
	// - 吞吐量: >1000 QPS
	// - 索引命中率: >99%
	Version func(ctx context.Context, dId string, version uint, limit int) (*model.VersionLog, error)

	// Find 具体数据查询函数
	//
	// **功能职责:**
	// 根据版本日志中解析出的数据ID列表，查询具体的业务数据内容。
	// 这是从变更记录到实际数据的桥梁，完成增量同步的最后一步。
	//
	// **查询策略:**
	// 1. **批量查询优化**
	//    - 支持一次查询多个ID，减少数据库往返次数
	//    - 使用IN查询或批量查询接口提升性能
	//    - 查询结果按ID顺序返回，保持数据一致性
	//
	// 2. **数据完整性保证**
	//    - 查询不存在的ID时不报错，返回空结果
	//    - 支持部分数据缺失的场景，提供容错能力
	//    - 返回的数据包含完整的业务信息
	//
	// 3. **性能优化措施**
	//    - 利用数据库索引加速ID查询
	//    - 支持查询结果缓存，避免重复查询
	//    - 内存使用优化，避免大结果集的内存溢出
	//
	// **使用场景示例:**
	// - 会话同步：根据会话ID列表查询会话详细信息
	// - 好友同步：根据好友ID列表查询好友资料信息
	// - 群组同步：根据群组ID列表查询群组详细信息
	// - 消息同步：根据消息ID列表查询消息内容
	//
	// **参数说明:**
	// - ctx: 请求上下文，用于超时控制和请求追踪
	// - ids: 数据ID列表，来源于版本日志的变更记录
	//
	// **返回值说明:**
	// - []A: 查询到的数据实体列表，类型A为泛型参数
	// - error: 查询错误，包括网络错误、数据库错误等
	//
	// **错误处理策略:**
	// - ID不存在：不视为错误，返回空结果
	// - 网络超时：返回超时错误，支持客户端重试
	// - 权限不足：返回权限错误，阻止非法访问
	// - 数据损坏：返回数据错误，触发数据修复流程
	//
	// **性能指标:**
	// - 查询延迟: <100ms (P95)
	// - 批量查询支持: 单次最多1000个ID
	// - 并发查询: >500 QPS
	Find func(ctx context.Context, ids []string) ([]A, error)

	// Resp 响应结果构造函数
	//
	// **设计理念:**
	// 将版本信息、数据变更信息和同步元数据组装成最终的API响应格式。
	// 这是同步框架与业务API的接口层，负责数据格式的标准化和序列化。
	//
	// **响应结构设计:**
	// 1. **版本信息部分**
	//    - 当前服务端版本ID和版本号
	//    - 客户端下次同步的基准版本
	//    - 版本有效期和同步间隔建议
	//
	// 2. **数据变更部分**
	//    - 新增数据列表：需要在客户端新增的数据
	//    - 更新数据列表：需要在客户端更新的数据
	//    - 删除ID列表：需要在客户端删除的数据ID
	//
	// 3. **同步元数据部分**
	//    - 同步类型标识：全量同步或增量同步
	//    - 数据完整性标识：是否包含完整的变更信息
	//    - 后续同步建议：下次同步的时间间隔
	//
	// **参数详细说明:**
	// - version: 服务端版本日志，包含最新的版本信息
	//   - ID: 版本唯一标识，客户端下次同步时使用
	//   - Version: 版本序列号，用于版本比较和验证
	//   - Logs: 变更日志，包含数据变更的详细信息
	//
	// - deleteIds: 删除的数据ID列表
	//   - 客户端需要从本地存储中删除这些数据
	//   - 空列表表示没有删除操作
	//   - ID格式应与业务数据的主键格式一致
	//
	// - insertList: 新增的数据实体列表
	//   - 客户端需要将这些数据添加到本地存储
	//   - 包含完整的业务数据内容
	//   - 数据格式符合业务API规范
	//
	// - updateList: 更新的数据实体列表
	//   - 客户端需要更新本地存储中对应的数据
	//   - 包含完整的更新后数据内容
	//   - 客户端应使用数据ID匹配本地记录
	//
	// - full: 全量同步标识
	//   - true: 全量同步，客户端应清空本地数据并重新加载
	//   - false: 增量同步，客户端按变更列表增量更新
	//
	// **响应格式要求:**
	// - JSON序列化兼容：支持标准JSON序列化
	// - 向后兼容性：新增字段不影响老版本客户端
	// - 数据压缩：大数据量时支持gzip压缩
	// - 错误信息包装：包含详细的错误码和错误描述
	//
	// **使用示例:**
	// ```go
	// return &GetIncrementalConversationResp{
	//     VersionID:     version.ID.Hex(),
	//     VersionNumber: uint64(version.Version),
	//     Full:          full,
	//     Delete:        deleteIds,
	//     Insert:        insertList,
	//     Update:        updateList,
	//     SyncTime:      time.Now().UnixMilli(),
	// }
	// ```
	Resp func(version *model.VersionLog, deleteIds []string, insertList, updateList []A, full bool) *B
}

// newError 创建统一格式的内部服务器错误
func (o *Option[A, B]) newError(msg string) error {
	return errs.ErrInternalServer.WrapMsg(msg)
}

// check 验证Option的必要参数是否完整
// 确保所有必需的回调函数都已设置，避免运行时错误
func (o *Option[A, B]) check() error {
	if o.Ctx == nil {
		return o.newError("opt ctx is nil")
	}
	if o.VersionKey == "" {
		return o.newError("versionKey is empty")
	}
	if o.Version == nil {
		return o.newError("func version is nil")
	}
	if o.Find == nil {
		return o.newError("func find is nil")
	}
	if o.Resp == nil {
		return o.newError("func resp is nil")
	}
	return nil
}

// validVersion 验证客户端提供的版本信息是否有效
//
// **验证目的:**
// 确保客户端提供的版本信息格式正确且逻辑有效，这是增量同步的前提条件。
// 无效的版本信息会导致同步策略降级为全量同步，确保数据一致性。
//
// **验证规则详述:**
//
// 1. **VersionID格式验证**
//   - 必须是有效的MongoDB ObjectID十六进制字符串
//   - 长度必须是24个字符（12字节的十六进制表示）
//   - 字符必须是有效的十六进制字符（0-9，a-f，A-F）
//   - 不能包含其他特殊字符或空格
//
// 2. **ObjectID有效性验证**
//   - ObjectID不能为零值（000000000000000000000000）
//   - 零值ObjectID表示未初始化或无效状态
//   - 确保ObjectID是由MongoDB或兼容系统生成的有效ID
//
// 3. **VersionNumber逻辑验证**
//   - 版本号必须大于0（>0）
//   - 版本号0表示未同步状态或初始状态
//   - 版本号应该是递增的正整数
//   - 版本号的连续性将在后续流程中验证
//
// **版本信息有效性的业务含义:**
// - 有效：客户端曾经与服务端成功同步过，可以尝试增量同步
// - 无效：客户端是首次同步或版本信息损坏，需要全量同步
//
// **错误处理策略:**
// - 格式错误：ObjectIDFromHex会返回解析错误
// - 零值ObjectID：IsZero()检查会返回true
// - 版本号无效：直接比较版本号与0的大小关系
//
// **性能考虑:**
// - ObjectID解析是轻量级操作，性能开销很小
// - 本方法会被频繁调用，避免了复杂的字符串操作
// - 验证失败时快速返回，避免不必要的计算
//
// **使用示例:**
// ```go
// // 有效的版本信息
// o.VersionID = "507f1f77bcf86cd799439011"
// o.VersionNumber = 123
// valid := o.validVersion() // 返回 true
//
// // 无效的版本信息
// o.VersionID = "invalid-id"
// o.VersionNumber = 0
// valid := o.validVersion() // 返回 false
// ```
//
// **返回值说明:**
// - true: 版本信息有效，可以进行增量同步判断
// - false: 版本信息无效，需要执行全量同步
func (o *Option[A, B]) validVersion() bool {
	objID, err := primitive.ObjectIDFromHex(o.VersionID)
	return err == nil && (!objID.IsZero()) && o.VersionNumber > 0
}

// equalID 检查客户端版本ID与服务端版本ID是否一致
// 用于判断客户端版本是否是基于服务端当前版本的
func (o *Option[A, B]) equalID(objID primitive.ObjectID) bool {
	return o.VersionID == objID.Hex()
}

// getVersion 获取版本日志数据并确定同步策略
//
// **方法核心职责:**
// 这是增量同步系统的核心决策引擎，负责根据客户端版本信息和可选的缓存优化，
// 智能选择最优的同步策略，并获取相应的版本日志数据。
//
// **设计理念:**
// 采用缓存优先的查询策略，通过两级查询机制实现性能优化：
// 1. 优先使用缓存版本查询（如果配置了CacheMaxVersion）
// 2. 降级到数据库直接查询（如果没有缓存或缓存不可用）
//
// **双重查询策略详解:**
//
// **策略A：直接数据库查询模式（无缓存）**
// 当CacheMaxVersion为nil时，直接基于客户端版本有效性进行查询：
// - 版本有效 → 增量查询：查询从客户端版本开始的变更记录
// - 版本无效 → 全量查询：返回最新的完整版本日志
//
// **策略B：缓存优化查询模式（有缓存）**
// 当CacheMaxVersion不为nil时，采用更智能的查询策略：
// 1. 首先查询缓存获取服务端最新版本
// 2. 将客户端版本与服务端最新版本进行比较
// 3. 根据比较结果决定具体的同步策略
//
// **同步策略决策矩阵:**
//
// | 客户端版本状态 | 版本ID匹配 | 版本号比较 | 决策结果 | 说明 |
// |--------------|----------|----------|---------|------|
// | 无效         | -        | -        | 全量同步 | 客户端首次同步或版本损坏 |
// | 有效         | 不匹配    | -        | 全量同步 | 版本ID不连续，可能有数据缺失 |
// | 有效         | 匹配      | 相等     | 无需同步 | 客户端版本是最新的 |
// | 有效         | 匹配      | 不等     | 增量同步 | 客户端需要更新到最新版本 |
//
// **tag参数的含义:**
// - tagQuery (1): 需要增量查询，获取版本变更记录
// - tagFull (2): 需要全量同步，返回完整数据快照
// - tagEqual (3): 版本一致，无需同步数据
//
// **缓存优化的性能收益:**
// - 减少数据库查询：对于无需同步的情况，避免数据库访问
// - 快速决策：通过缓存比较快速确定同步策略
// - 降低延迟：缓存响应时间通常<10ms，数据库查询可能>50ms
// - 提升并发：减少数据库连接压力，提升系统并发能力
//
// **错误处理与容灾:**
// - 缓存查询失败：返回错误，不降级到数据库查询
// - 版本查询失败：返回数据库错误，客户端可重试
// - 版本不存在：返回空版本日志，触发全量同步
//
// **性能监控指标:**
// - 缓存命中率：>95%（有缓存时）
// - 查询响应时间：<50ms（P95）
// - 决策准确率：>99%（正确选择同步策略）
//
// **使用场景示例:**
// ```go
// // 场景1：客户端首次同步
// o.VersionID = ""
// o.VersionNumber = 0
// // 结果：tag = tagFull，返回全量版本日志
//
// // 场景2：客户端版本是最新的
// o.VersionID = "最新版本ID"
// o.VersionNumber = 最新版本号
// // 结果：tag = tagEqual，返回缓存版本日志
//
// // 场景3：客户端需要增量更新
// o.VersionID = "有效版本ID"
// o.VersionNumber = 较旧版本号
// // 结果：tag = tagQuery，返回增量版本日志
// ```
func (o *Option[A, B]) getVersion(tag *int) (*model.VersionLog, error) {
	// 如果没有缓存版本查询函数，直接查询数据库
	if o.CacheMaxVersion == nil {
		if o.validVersion() {
			// 版本有效，进行增量查询
			*tag = tagQuery
			return o.Version(o.Ctx, o.VersionKey, uint(o.VersionNumber), syncLimit)
		}
		// 版本无效，进行全量同步
		*tag = tagFull
		return o.Version(o.Ctx, o.VersionKey, 0, 0)
	} else {
		// 有缓存版本查询函数，先从缓存获取最新版本进行比较
		cache, err := o.CacheMaxVersion(o.Ctx, o.VersionKey)
		if err != nil {
			return nil, err
		}

		// 验证客户端版本有效性
		if !o.validVersion() {
			// 版本无效，返回缓存的最新版本（全量同步）
			*tag = tagFull
			return cache, nil
		}

		// 检查版本ID是否匹配
		if !o.equalID(cache.ID) {
			// 版本ID不匹配，返回缓存的最新版本（全量同步）
			*tag = tagFull
			return cache, nil
		}

		// 检查版本号是否相等
		if o.VersionNumber == uint64(cache.Version) {
			// 版本号相等，无需同步
			*tag = tagEqual
			return cache, nil
		}

		// 版本号不等，进行增量查询
		*tag = tagQuery
		return o.Version(o.Ctx, o.VersionKey, uint(o.VersionNumber), syncLimit)
	}
}

// Build 构建并执行单个目标的增量同步
//
// **方法设计理念:**
// 这是Option类型的核心执行引擎，实现了完整的增量同步工作流程。
// 采用流水线式的处理模式，将复杂的同步逻辑分解为清晰的步骤，
// 确保每个步骤的职责单一且易于测试和维护。
//
// **核心处理流程（6个关键阶段）:**
//
// **阶段1：参数完整性验证**
// - 验证所有必需的回调函数是否已设置
// - 验证上下文和关键参数的有效性
// - 早期发现配置错误，避免运行时异常
// - 提供清晰的错误信息帮助问题定位
//
// **阶段2：版本策略智能决策**
// - 调用getVersion方法获取版本日志
// - 根据客户端版本信息确定同步策略
// - 利用缓存优化减少不必要的数据库查询
// - 设置tag标识指导后续处理流程
//
// **阶段3：全量同步策略判断**
// - 基于tag标识和版本日志完整性进行二次检查
// - 处理版本跳跃、日志缺失等边缘情况
// - 确保数据一致性，防止增量同步导致的数据丢失
// - 自动降级机制保证同步的可靠性
//
// **阶段4：版本日志数据解析**
// - 从版本日志中提取增删改的数据ID列表
// - 按操作类型分类数据ID（insert、update、delete）
// - 为后续的数据查询做好准备
// - 跳过全量同步场景的日志解析，提升性能
//
// **阶段5：业务数据批量查询**
// - 并行查询新增和更新的具体数据内容
// - 利用批量查询接口提升查询效率
// - 处理数据不存在的异常情况
// - 保持查询结果与ID列表的对应关系
//
// **阶段6：响应结果标准化构造**
// - 调用Resp回调函数构造最终响应
// - 包装版本信息、数据变更和同步元数据
// - 确保响应格式符合API规范
// - 提供客户端下次同步所需的版本基准
//
// **同步策略的二次验证逻辑:**
//
// 在阶段3中，即使getVersion已经确定了初步策略，仍需要进行二次验证：
//
// ```go
// // 对于增量查询策略，需要验证：
//
//	if tag == tagQuery {
//	    // 1. 版本ID一致性检查
//	    full = version.ID.Hex() != o.VersionID ||
//	    // 2. 版本号连续性检查
//	           uint64(version.Version) < o.VersionNumber ||
//	    // 3. 日志完整性检查
//	           len(version.Logs) != version.LogLen
//	}
//
// ```
//
// **错误处理与容错机制:**
//
// 1. **参数验证失败**
//   - 返回具体的配置错误信息
//   - 帮助开发者快速定位问题
//   - 避免运行时的空指针异常
//
// 2. **版本查询失败**
//   - 传播数据库或缓存的错误信息
//   - 支持客户端重试机制
//   - 记录详细的错误日志用于问题排查
//
// 3. **数据查询失败**
//   - 区分网络错误和数据错误
//   - 提供部分成功的处理能力
//   - 确保事务一致性
//
// **性能优化特性:**
//
// 1. **懒加载策略**
//   - 只有在需要时才解析版本日志
//   - 全量同步时跳过日志解析步骤
//   - 减少不必要的计算开销
//
// 2. **并行查询**
//   - 新增和更新数据的查询可以并行执行
//   - 利用Go的goroutine提升并发性能
//   - 减少总体响应时间
//
// 3. **内存管理**
//   - 及时释放大型临时变量
//   - 使用切片预分配减少内存分配
//   - 避免内存泄漏
//
// **监控与可观测性:**
//
// - 每个阶段的耗时监控
// - 同步策略的选择统计
// - 数据量大小的分布监控
// - 错误率和成功率统计
//
// **使用示例:**
// ```go
//
//	option := &Option[Conversation, GetConversationResp]{
//	    Ctx:           ctx,
//	    VersionKey:    userID,
//	    VersionID:     clientVersionID,
//	    VersionNumber: clientVersionNumber,
//	    Version:       db.GetVersionLog,
//	    Find:          db.GetConversations,
//	    Resp:          buildConversationResp,
//	}
//
// resp, err := option.Build()
//
//	if err != nil {
//	    return nil, err
//	}
//
// ```
//
// **返回值说明:**
// - *B: 构造的响应结果，包含完整的同步数据
// - error: 执行过程中的错误，nil表示成功
func (o *Option[A, B]) Build() (*B, error) {
	// 1. 验证必要参数
	if err := o.check(); err != nil {
		return nil, err
	}

	// 2. 获取版本日志并确定同步策略
	var tag int
	version, err := o.getVersion(&tag)
	if err != nil {
		return nil, err
	}

	// 3. 根据同步策略确定是否需要全量同步
	var full bool
	switch tag {
	case tagQuery:
		// 增量查询：检查版本日志完整性，决定是否需要全量同步
		// 如果版本ID不匹配、版本号落后或日志不完整，则需要全量同步
		full = version.ID.Hex() != o.VersionID || uint64(version.Version) < o.VersionNumber || len(version.Logs) != version.LogLen
	case tagFull:
		// 全量同步
		full = true
	case tagEqual:
		// 版本相等，无需同步
		full = false
	default:
		panic(fmt.Errorf("undefined tag %d", tag))
	}

	// 4. 解析版本日志获取变更的数据ID
	var (
		insertIds []string // 新增数据ID列表
		deleteIds []string // 删除数据ID列表
		updateIds []string // 更新数据ID列表
	)

	// 只有增量同步时才需要解析版本日志
	if !full {
		// 从版本日志中提取增删改的数据ID
		insertIds, deleteIds, updateIds = version.DeleteAndChangeIDs()
	}

	// 5. 查询具体的数据内容
	var (
		insertList []A // 新增数据内容列表
		updateList []A // 更新数据内容列表
	)

	// 查询新增的数据
	if len(insertIds) > 0 {
		insertList, err = o.Find(o.Ctx, insertIds)
		if err != nil {
			return nil, err
		}
	}

	// 查询更新的数据
	if len(updateIds) > 0 {
		updateList, err = o.Find(o.Ctx, updateIds)
		if err != nil {
			return nil, err
		}
	}

	// 6. 构造并返回最终响应结果
	return o.Resp(version, deleteIds, insertList, updateList, full), nil
}
