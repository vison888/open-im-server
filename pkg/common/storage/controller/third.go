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

// Package controller 第三方服务数据库控制器实现
//
// 本文件实现第三方服务相关的数据库控制器，主要负责推送令牌和日志管理
//
// **功能概述：**
// ┌─────────────────────────────────────────────────────────┐
// │              第三方服务系统架构                           │
// ├─────────────────────────────────────────────────────────┤
// │ 1. 推送服务：FCM令牌管理和应用角标设置                    │
// │ 2. 日志服务：调试日志的上传、查询和管理                   │
// │ 3. 缓存管理：设备令牌和用户状态的缓存                     │
// │ 4. 数据持久化：日志记录的持久化存储                       │
// └─────────────────────────────────────────────────────────┘
//
// **服务分类：**
// ┌─────────────────┬─────────────────────────────────────┐
// │   推送相关服务   │           日志相关服务              │
// ├─────────────────┼─────────────────────────────────────┤
// │ • FCM令牌更新    │ • 调试日志上传                      │
// │ • 应用角标设置   │ • 日志搜索查询                      │
// │ • 设备状态管理   │ • 日志批量删除                      │
// │ • 缓存失效      │ • 日志统计分析                      │
// └─────────────────┴─────────────────────────────────────┘
//
// **设计特点：**
// - 服务聚合：整合多个第三方服务的管理功能
// - 缓存优先：推送服务主要使用缓存存储
// - 数据持久化：日志服务需要可靠的持久化存储
// - 接口简洁：提供简化的第三方服务操作接口
package controller

import (
	"context"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache"
	"github.com/openimsdk/tools/db/pagination"
)

// ThirdDatabase 第三方服务数据库操作接口
// 定义第三方服务相关的数据操作方法，整合推送和日志功能
//
// **接口职责：**
// 1. 推送服务管理：FCM令牌和应用角标
// 2. 日志服务管理：调试日志的完整生命周期
//
// **设计原则：**
// - 功能聚合：将相关的第三方服务功能集中管理
// - 简化接口：对外提供简洁易用的操作接口
// - 职责分离：推送和日志功能相对独立
// - 扩展性：便于添加新的第三方服务支持
//
// **应用场景：**
// - 移动应用的推送通知管理
// - 应用调试和问题排查
// - 用户行为和系统状态监控
type ThirdDatabase interface {
	// ==================== 推送服务管理 ====================

	// FcmUpdateToken 更新FCM推送令牌
	// 功能：更新或新增用户设备的FCM推送令牌
	//
	// **应用场景：**
	// - 用户首次登录时注册推送令牌
	// - 应用重新安装后令牌更新
	// - 令牌过期后的自动更新
	// - 用户切换设备时的令牌管理
	//
	// **参数说明：**
	// - account: 用户账号（通常是用户ID）
	// - platformID: 平台标识（iOS=1, Android=2等）
	// - fcmToken: FCM推送令牌字符串
	// - expireTime: 令牌过期时间戳
	//
	// **存储特点：**
	// - 使用缓存存储，提高访问效率
	// - 支持过期时间管理
	// - 按平台分别存储令牌
	//
	// **安全考虑：**
	// - 令牌具有时效性，定期更新
	// - 平台隔离，避免跨平台令牌混用
	FcmUpdateToken(ctx context.Context, account string, platformID int, fcmToken string, expireTime int64) error

	// SetAppBadge 设置应用角标数量
	// 功能：设置用户应用图标上显示的未读消息数量
	//
	// **功能说明：**
	// - 用于iOS应用图标角标显示
	// - 统计用户未读消息总数
	// - 提供直观的消息提醒
	//
	// **应用场景：**
	// - 用户收到新消息时更新角标
	// - 用户阅读消息后减少角标数量
	// - 用户登录时同步角标状态
	// - 清空所有未读时重置角标
	//
	// **参数说明：**
	// - userID: 用户唯一标识
	// - value: 角标数值（0表示清空角标）
	//
	// **实现细节：**
	// - 存储在缓存中，快速访问
	// - 支持实时更新
	// - 与推送服务集成
	SetAppBadge(ctx context.Context, userID string, value int) error

	// ==================== 日志服务管理 ====================

	// UploadLogs 上传调试日志
	// 功能：批量上传客户端调试日志到服务器
	//
	// **应用场景：**
	// - 应用崩溃时上传错误日志
	// - 用户反馈问题时主动上传日志
	// - 定期上传运行日志进行分析
	// - 开发阶段的调试信息收集
	//
	// **日志内容：**
	// - 应用运行日志
	// - 错误和异常信息
	// - 用户操作轨迹
	// - 系统状态信息
	//
	// **存储特点：**
	// - 持久化存储到数据库
	// - 支持大批量日志处理
	// - 按时间和用户组织数据
	//
	// **隐私保护：**
	// - 过滤敏感信息
	// - 用户授权上传
	// - 数据加密传输
	//
	// **参数说明：**
	// - logs: 日志记录列表
	UploadLogs(ctx context.Context, logs []*model.Log) error

	// DeleteLogs 删除指定日志
	// 功能：根据日志ID批量删除用户的日志记录
	//
	// **删除策略：**
	// - 按用户隔离，确保数据安全
	// - 支持批量删除，提高效率
	// - 验证用户权限，防止误删
	//
	// **应用场景：**
	// - 用户主动清理历史日志
	// - 隐私保护需求的数据清理
	// - 存储空间管理
	// - 数据生命周期管理
	//
	// **参数说明：**
	// - logID: 要删除的日志ID列表
	// - userID: 日志所属用户ID（权限验证）
	DeleteLogs(ctx context.Context, logID []string, userID string) error

	// SearchLogs 搜索日志记录
	// 功能：根据关键词和时间范围搜索日志，支持分页
	//
	// **搜索功能：**
	// - 关键词模糊匹配
	// - 时间范围筛选
	// - 分页结果返回
	// - 多字段组合搜索
	//
	// **应用场景：**
	// - 问题排查和调试
	// - 日志分析和统计
	// - 用户行为追踪
	// - 系统监控和告警
	//
	// **性能优化：**
	// - 数据库索引优化
	// - 分页限制结果集大小
	// - 缓存热点查询结果
	//
	// **参数说明：**
	// - keyword: 搜索关键词
	// - start: 开始时间
	// - end: 结束时间
	// - pagination: 分页参数
	//
	// **返回值：**
	// - int64: 符合条件的总记录数
	// - []*model.Log: 当前页的日志列表
	SearchLogs(ctx context.Context, keyword string, start time.Time, end time.Time, pagination pagination.Pagination) (int64, []*model.Log, error)

	// GetLogs 获取指定日志详情
	// 功能：根据日志ID列表获取详细的日志信息
	//
	// **功能特点：**
	// - 批量获取，提高效率
	// - 用户权限验证
	// - 完整日志内容返回
	//
	// **应用场景：**
	// - 查看日志详细内容
	// - 日志内容分析
	// - 问题诊断和排查
	// - 日志导出功能
	//
	// **安全控制：**
	// - 验证日志所有权
	// - 防止越权访问
	// - 敏感信息过滤
	//
	// **参数说明：**
	// - LogIDs: 日志ID列表
	// - userID: 请求用户ID（权限验证）
	//
	// **返回值：**
	// - []*model.Log: 日志详情列表
	GetLogs(ctx context.Context, LogIDs []string, userID string) ([]*model.Log, error)
}

// thirdDatabase 第三方服务数据库控制器实现
//
// **架构组成：**
// ┌─────────────────────────────────────────────────────────┐
// │                 thirdDatabase                          │
// ├─────────────────┬───────────────────────────────────────┤
// │   缓存组件       │            数据库组件                 │
// │ ┌─────────────┐ │ ┌───────────────────────────────────┐ │
// │ │ ThirdCache  │ │ │            Log DB                │ │
// │ │ - FCM令牌   │ │ │ - 日志持久化存储                   │ │
// │ │ - 应用角标   │ │ │ - 日志搜索查询                     │ │
// │ │ - 设备状态   │ │ │ - 日志批量操作                     │ │
// │ │ - 快速访问   │ │ │ - 数据生命周期管理                 │ │
// │ └─────────────┘ │ └───────────────────────────────────┘ │
// └─────────────────┴───────────────────────────────────────┘
//
// **职责分离：**
// - cache: 处理推送相关的缓存操作（FCM令牌、应用角标）
// - logdb: 处理日志相关的数据库操作（CRUD、搜索、统计）
//
// **数据流向：**
// - 推送服务：数据主要存储在缓存中，快速读写
// - 日志服务：数据持久化到数据库，支持复杂查询
type thirdDatabase struct {
	cache cache.ThirdCache // 第三方服务缓存接口（推送令牌、角标等）
	logdb database.Log     // 日志数据库操作接口
}

// NewThirdDatabase 创建第三方服务数据库控制器实例
//
// **参数说明：**
// - cache: 第三方服务缓存实例，处理推送相关数据
// - logdb: 日志数据库实例，处理日志相关数据
//
// **设计考虑：**
// - 依赖注入：通过构造函数注入依赖组件
// - 职责分离：缓存和数据库分别处理不同类型的数据
// - 接口抽象：基于接口编程，便于测试和扩展
func NewThirdDatabase(cache cache.ThirdCache, logdb database.Log) ThirdDatabase {
	return &thirdDatabase{
		cache: cache,
		logdb: logdb,
	}
}

// ==================== 日志服务实现 ====================

// DeleteLogs 删除日志实现
//
// **安全机制：**
// - 按用户隔离删除，确保数据安全
// - 验证日志所有权，防止越权操作
//
// **操作特点：**
// - 直接委托给数据库层处理
// - 支持批量删除操作
// - 原子性操作保证
func (t *thirdDatabase) DeleteLogs(ctx context.Context, logID []string, userID string) error {
	return t.logdb.Delete(ctx, logID, userID)
}

// GetLogs 获取日志实现
//
// **数据获取：**
// - 根据日志ID批量获取
// - 用户权限验证
// - 完整数据返回
//
// **性能优化：**
// - 批量查询减少数据库访问
// - 索引优化提高查询效率
func (t *thirdDatabase) GetLogs(ctx context.Context, LogIDs []string, userID string) ([]*model.Log, error) {
	return t.logdb.Get(ctx, LogIDs, userID)
}

// SearchLogs 搜索日志实现
//
// **搜索能力：**
// - 关键词模糊搜索
// - 时间范围过滤
// - 分页结果处理
//
// **查询优化：**
// - 数据库索引支持
// - 查询条件优化
// - 结果集限制
//
// **返回信息：**
// - 总记录数：用于分页计算
// - 日志列表：当前页的具体数据
func (t *thirdDatabase) SearchLogs(ctx context.Context, keyword string, start time.Time, end time.Time, pagination pagination.Pagination) (int64, []*model.Log, error) {
	return t.logdb.Search(ctx, keyword, start, end, pagination)
}

// UploadLogs 上传日志实现
//
// **上传处理：**
// - 批量创建日志记录
// - 数据持久化存储
// - 原子性操作保证
//
// **数据处理：**
// - 自动添加时间戳
// - 数据格式验证
// - 存储优化处理
func (t *thirdDatabase) UploadLogs(ctx context.Context, logs []*model.Log) error {
	return t.logdb.Create(ctx, logs)
}

// ==================== 推送服务实现 ====================

// FcmUpdateToken FCM令牌更新实现
//
// **令牌管理：**
// - 更新或新增FCM推送令牌
// - 设置令牌过期时间
// - 按平台分别管理
//
// **缓存操作：**
// - 直接写入缓存存储
// - 快速读写访问
// - 自动过期管理
//
// **参数处理：**
// - account: 用户账号标识
// - platformID: 平台类型（iOS/Android等）
// - fcmToken: FCM推送令牌
// - expireTime: 令牌过期时间
func (t *thirdDatabase) FcmUpdateToken(ctx context.Context, account string, platformID int, fcmToken string, expireTime int64) error {
	return t.cache.SetFcmToken(ctx, account, platformID, fcmToken, expireTime)
}

// SetAppBadge 设置应用角标实现
//
// **角标管理：**
// - 设置用户应用角标数量
// - 实时更新角标状态
// - 支持角标清零操作
//
// **缓存特性：**
// - 快速读写访问
// - 实时状态更新
// - 与推送服务联动
//
// **应用场景：**
// - 新消息到达时增加角标
// - 消息已读时减少角标
// - 全部已读时清零角标
func (t *thirdDatabase) SetAppBadge(ctx context.Context, userID string, value int) error {
	return t.cache.SetUserBadgeUnreadCountSum(ctx, userID, value)
}
