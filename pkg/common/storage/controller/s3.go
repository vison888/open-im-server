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

// Package controller S3对象存储数据库控制器实现
//
// 本文件实现S3对象存储相关的数据库控制器，提供统一的对象存储管理接口
//
// **功能概述：**
// ┌─────────────────────────────────────────────────────────┐
// │                S3存储系统架构                            │
// ├─────────────────────────────────────────────────────────┤
// │ 1. 分片上传：支持大文件的分片并发上传                      │
// │ 2. 签名认证：提供预签名URL和表单直传                      │
// │ 3. 缓存优化：Redis缓存元数据，提高访问性能                 │
// │ 4. 生命周期：自动管理过期文件的清理                       │
// │ 5. 多引擎：支持多种对象存储后端（MinIO、阿里云、AWS等）     │
// └─────────────────────────────────────────────────────────┘
//
// **上传流程：**
// 客户端 → 预签名URL → 直传S3 → 确认上传 → 元数据入库 → 缓存更新
//
// **架构特点：**
// - 分层设计：控制器→缓存→存储引擎
// - 高性能：Redis缓存 + 并发上传
// - 高可用：多副本存储 + 故障转移
// - 可扩展：插件化的存储引擎支持
package controller

import (
	"context"
	"path/filepath"
	"time"

	redisCache "github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache/redis"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache"
	"github.com/openimsdk/tools/s3"
	"github.com/openimsdk/tools/s3/cont"
	"github.com/redis/go-redis/v9"
)

// S3Database S3对象存储数据库操作接口
// 定义了对象存储相关的所有操作方法，是对象存储功能的统一访问层
//
// **接口设计原则：**
// 1. 统一抽象：屏蔽不同存储厂商的差异
// 2. 高性能：内置缓存和并发优化
// 3. 易用性：简化复杂的存储操作
// 4. 可扩展：支持多种存储引擎
//
// **功能分类：**
// - 分片上传：大文件分片上传管理
// - 访问控制：URL签名和权限管理
// - 元数据管理：对象信息的CRUD操作
// - 生命周期：过期文件清理和统计
//
// **性能优化：**
// - Redis缓存热点数据
// - 分片并发上传
// - 预签名URL减少服务器负载
// - 批量操作提高效率
type S3Database interface {
	// ==================== 分片上传管理 ====================

	// PartLimit 获取分片上传限制
	// 功能：返回分片上传的配置限制（最大分片数、分片大小等）
	// 应用：客户端分片策略制定
	PartLimit() (*s3.PartLimit, error)

	// PartSize 计算最优分片大小
	// 功能：根据文件总大小计算最优的分片大小
	// 算法：平衡上传效率和内存占用
	// 参数：size - 文件总大小
	// 返回：建议的分片大小
	PartSize(ctx context.Context, size int64) (int64, error)

	// AuthSign 生成分片上传签名
	// 功能：为指定的分片号生成上传认证签名
	// 安全：基于临时凭证，限制上传权限和时间
	// 参数：
	//   - uploadID: 分片上传的唯一标识
	//   - partNumbers: 需要签名的分片号列表
	// 返回：包含所有分片签名信息的结构
	AuthSign(ctx context.Context, uploadID string, partNumbers []int) (*s3.AuthSignResult, error)

	// InitiateMultipartUpload 初始化分片上传
	// 功能：创建分片上传会话，返回uploadID和相关配置
	// 流程：
	//   1. 根据文件hash检查是否已存在
	//   2. 创建上传会话
	//   3. 分配uploadID
	//   4. 设置过期时间
	// 参数：
	//   - hash: 文件内容hash（去重）
	//   - size: 文件大小
	//   - expire: 上传会话过期时间
	//   - maxParts: 最大分片数量
	InitiateMultipartUpload(ctx context.Context, hash string, size int64, expire time.Duration, maxParts int) (*cont.InitiateUploadResult, error)

	// CompleteMultipartUpload 完成分片上传
	// 功能：合并所有分片，完成文件上传
	// 流程：
	//   1. 验证所有分片完整性
	//   2. 合并分片为完整文件
	//   3. 更新文件元数据
	//   4. 清理临时数据
	// 参数：
	//   - uploadID: 分片上传标识
	//   - parts: 分片ETag列表（按顺序）
	CompleteMultipartUpload(ctx context.Context, uploadID string, parts []string) (*cont.UploadResult, error)

	// ==================== 访问控制管理 ====================

	// AccessURL 生成文件访问URL
	// 功能：生成带签名的文件访问URL
	// 特性：
	//   - 支持临时访问链接
	//   - 可自定义下载文件名
	//   - 可设置Content-Type
	// 参数：
	//   - name: 文件名称
	//   - expire: URL过期时间
	//   - opt: 访问选项（文件名、类型等）
	// 返回：过期时间、访问URL
	AccessURL(ctx context.Context, name string, expire time.Duration, opt *s3.AccessURLOption) (time.Time, string, error)

	// FormData 生成表单直传数据
	// 功能：生成客户端直传到S3的表单数据
	// 优势：减少服务器中转，提高上传效率
	// 安全：通过策略限制上传权限
	// 参数：
	//   - name: 文件名
	//   - size: 文件大小
	//   - contentType: 文件类型
	//   - duration: 表单有效期
	FormData(ctx context.Context, name string, size int64, contentType string, duration time.Duration) (*s3.FormData, error)

	// ==================== 对象信息管理 ====================

	// SetObject 设置对象元数据
	// 功能：将对象信息保存到数据库，并更新缓存
	// 流程：
	//   1. 自动设置存储引擎标识
	//   2. 保存到数据库
	//   3. 删除相关缓存
	// 特性：事务保证、缓存一致性
	SetObject(ctx context.Context, info *model.Object) error

	// StatObject 获取对象统计信息
	// 功能：获取对象的详细统计信息（大小、修改时间等）
	// 来源：直接从存储引擎获取最新信息
	// 应用：文件管理、统计分析
	StatObject(ctx context.Context, name string) (*s3.ObjectInfo, error)

	// ==================== 生命周期管理 ====================

	// FindExpirationObject 查找过期对象
	// 功能：查找指定条件下的过期对象
	// 应用：定时清理任务、存储成本优化
	// 参数：
	//   - engine: 存储引擎标识
	//   - expiration: 过期时间阈值
	//   - needDelType: 需要删除的对象类型
	//   - count: 返回数量限制
	FindExpirationObject(ctx context.Context, engine string, expiration time.Time, needDelType []string, count int64) ([]*model.Object, error)

	// DeleteSpecifiedData 删除指定数据
	// 功能：批量删除指定的对象数据
	// 范围：仅删除数据库记录，不删除存储文件
	// 应用：元数据清理、批量管理
	DeleteSpecifiedData(ctx context.Context, engine string, name []string) error

	// DelS3Key 删除S3存储对象
	// 功能：从S3存储中删除实际的文件对象
	// 范围：仅删除存储文件，不删除数据库记录
	// 应用：存储空间清理
	DelS3Key(ctx context.Context, engine string, keys ...string) error

	// GetKeyCount 获取对象数量统计
	// 功能：统计指定引擎和前缀下的对象数量
	// 应用：存储统计、容量规划
	GetKeyCount(ctx context.Context, engine string, key string) (int64, error)
}

// NewS3Database 创建S3数据库控制器实例
//
// **依赖组件：**
// - rdb: Redis客户端（缓存）
// - s3: S3存储接口（存储引擎）
// - obj: 对象信息数据库接口（元数据）
//
// **初始化架构：**
// ┌─────────────────────────────────────────────────────────┐
// │                  S3Database                            │
// ├─────────────────┬─────────────────┬───────────────────┤
// │   控制器层       │     缓存层       │    数据库层        │
// │ ┌─────────────┐ │ ┌─────────────┐ │ ┌───────────────┐ │
// │ │ Controller  │ │ │ ObjectCache │ │ │ ObjectInfo DB │ │
// │ │ - 业务逻辑   │ │ │ - Redis缓存  │ │ │ - 元数据存储   │ │
// │ │ - 参数验证   │ │ │ - S3缓存    │ │ │ - 查询统计     │ │
// │ └─────────────┘ │ └─────────────┘ │ └───────────────┘ │
// └─────────────────┴─────────────────┴───────────────────┘
//
//	             ↓
//	┌─────────────────────────────────┐
//	│         S3存储引擎              │
//	│ - MinIO / AWS S3 / 阿里云OSS    │
//	│ - 对象存储 / 分片上传           │
//	└─────────────────────────────────┘
func NewS3Database(rdb redis.UniversalClient, s3 s3.Interface, obj database.ObjectInfo) S3Database {
	return &s3Database{
		// 创建S3控制器，集成缓存和存储引擎
		s3: cont.New(redisCache.NewS3Cache(rdb, s3), s3),
		// 对象元数据缓存
		cache: redisCache.NewObjectCacheRedis(rdb, obj),
		// S3操作缓存
		s3cache: redisCache.NewS3Cache(rdb, s3),
		// 数据库操作接口
		db: obj,
	}
}

// s3Database S3数据库控制器实现
//
// **组件职责：**
// ┌─────────────────────────────────────────────────────────┐
// │                  s3Database                            │
// ├─────────────────────────────────────────────────────────┤
// │ s3: S3控制器                                            │
// │   - 分片上传管理                                         │
// │   - 预签名URL生成                                        │
// │   - 存储引擎抽象                                         │
// ├─────────────────────────────────────────────────────────┤
// │ cache: 对象缓存                                         │
// │   - 对象元数据缓存                                       │
// │   - 访问URL缓存                                          │
// │   - 缓存失效管理                                         │
// ├─────────────────────────────────────────────────────────┤
// │ s3cache: S3操作缓存                                     │
// │   - 存储操作缓存                                         │
// │   - 上传状态缓存                                         │
// │   - 临时数据管理                                         │
// ├─────────────────────────────────────────────────────────┤
// │ db: 数据库接口                                           │
// │   - 对象元数据持久化                                     │
// │   - 查询统计功能                                         │
// │   - 生命周期管理                                         │
// └─────────────────────────────────────────────────────────┘
type s3Database struct {
	s3      *cont.Controller    // S3控制器，处理存储引擎交互
	cache   cache.ObjectCache   // 对象缓存，提高元数据访问性能
	s3cache cont.S3Cache        // S3缓存，优化存储操作
	db      database.ObjectInfo // 数据库接口，持久化元数据
}

// ==================== 分片上传管理实现 ====================

// PartSize 计算最优分片大小实现
// 委托给S3控制器处理，基于文件大小智能计算
func (s *s3Database) PartSize(ctx context.Context, size int64) (int64, error) {
	return s.s3.PartSize(ctx, size)
}

// PartLimit 获取分片限制实现
// 返回当前S3配置的分片上传限制
func (s *s3Database) PartLimit() (*s3.PartLimit, error) {
	return s.s3.PartLimit()
}

// AuthSign 生成分片签名实现
// 为指定分片生成安全的上传签名
func (s *s3Database) AuthSign(ctx context.Context, uploadID string, partNumbers []int) (*s3.AuthSignResult, error) {
	return s.s3.AuthSign(ctx, uploadID, partNumbers)
}

// InitiateMultipartUpload 初始化分片上传实现
// 创建分片上传会话，支持文件去重和过期管理
func (s *s3Database) InitiateMultipartUpload(ctx context.Context, hash string, size int64, expire time.Duration, maxParts int) (*cont.InitiateUploadResult, error) {
	return s.s3.InitiateUpload(ctx, hash, size, expire, maxParts)
}

// CompleteMultipartUpload 完成分片上传实现
// 合并分片并完成上传流程
func (s *s3Database) CompleteMultipartUpload(ctx context.Context, uploadID string, parts []string) (*cont.UploadResult, error) {
	return s.s3.CompleteUpload(ctx, uploadID, parts)
}

// ==================== 对象信息管理实现 ====================

// SetObject 设置对象元数据实现
//
// **完整流程：**
// 1. 自动设置存储引擎标识
// 2. 保存元数据到数据库
// 3. 删除相关缓存确保一致性
//
// **缓存策略：**
// - 写入后立即失效相关缓存
// - 确保下次读取获取最新数据
// - 避免缓存不一致问题
func (s *s3Database) SetObject(ctx context.Context, info *model.Object) error {
	// **第一步：设置存储引擎标识**
	// 自动标记对象属于当前S3存储引擎
	info.Engine = s.s3.Engine()

	// **第二步：保存到数据库**
	// 持久化对象元数据信息
	if err := s.db.SetObject(ctx, info); err != nil {
		return err
	}

	// **第三步：删除相关缓存**
	// 使用链式调用删除对象名称相关的缓存
	// 确保缓存和数据库的一致性
	return s.cache.DelObjectName(info.Engine, info.Name).ChainExecDel(ctx)
}

// AccessURL 生成访问URL实现
//
// **访问流程：**
// 1. 从缓存获取对象元数据
// 2. 设置访问选项默认值
// 3. 生成预签名访问URL
//
// **性能优化：**
// - 优先从缓存获取对象信息
// - 智能设置Content-Type和文件名
// - 减少存储引擎的直接调用
//
// **安全特性：**
// - 基于对象实际信息生成URL
// - 支持临时访问控制
// - 可自定义下载行为
func (s *s3Database) AccessURL(ctx context.Context, name string, expire time.Duration, opt *s3.AccessURLOption) (time.Time, string, error) {
	// **第一步：获取对象元数据**
	// 优先从缓存获取，提高性能
	obj, err := s.cache.GetName(ctx, s.s3.Engine(), name)
	if err != nil {
		return time.Time{}, "", err
	}

	// **第二步：设置访问选项**
	// 提供合理的默认值，提升用户体验
	if opt == nil {
		opt = &s3.AccessURLOption{}
	}
	if opt.ContentType == "" {
		opt.ContentType = obj.ContentType // 使用对象原始类型
	}
	if opt.Filename == "" {
		opt.Filename = filepath.Base(obj.Name) // 使用文件名作为下载名
	}

	// **第三步：生成预签名URL**
	expireTime := time.Now().Add(expire)
	rawURL, err := s.s3.AccessURL(ctx, obj.Key, expire, opt)
	if err != nil {
		return time.Time{}, "", err
	}

	return expireTime, rawURL, nil
}

// ==================== 存储引擎操作实现 ====================

// StatObject 获取对象统计信息实现
// 直接从存储引擎获取最新的对象信息
func (s *s3Database) StatObject(ctx context.Context, name string) (*s3.ObjectInfo, error) {
	return s.s3.StatObject(ctx, name)
}

// FormData 生成表单数据实现
// 创建客户端直传S3的表单数据
func (s *s3Database) FormData(ctx context.Context, name string, size int64, contentType string, duration time.Duration) (*s3.FormData, error) {
	return s.s3.FormData(ctx, name, size, contentType, duration)
}

// ==================== 生命周期管理实现 ====================

// FindExpirationObject 查找过期对象实现
// 委托给数据库层处理复杂的过期查询逻辑
func (s *s3Database) FindExpirationObject(ctx context.Context, engine string, expiration time.Time, needDelType []string, count int64) ([]*model.Object, error) {
	return s.db.FindExpirationObject(ctx, engine, expiration, needDelType, count)
}

// GetKeyCount 获取对象统计实现
// 统计指定条件下的对象数量
func (s *s3Database) GetKeyCount(ctx context.Context, engine string, key string) (int64, error) {
	return s.db.GetKeyCount(ctx, engine, key)
}

// DeleteSpecifiedData 删除指定数据实现
// 仅删除数据库中的元数据记录
func (s *s3Database) DeleteSpecifiedData(ctx context.Context, engine string, name []string) error {
	return s.db.Delete(ctx, engine, name)
}

// DelS3Key 删除S3对象实现
// 从实际的S3存储中删除文件对象
func (s *s3Database) DelS3Key(ctx context.Context, engine string, keys ...string) error {
	return s.s3cache.DelS3Key(ctx, engine, keys...)
}
