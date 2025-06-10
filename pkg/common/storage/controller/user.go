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

// Package controller 用户管理控制器包
// 提供用户基础信息的增删查改、用户命令管理、统计分析等核心功能
// 支持缓存优化、事务安全、分页查询和批量操作
package controller

import (
	"context"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/tools/db/pagination"
	"github.com/openimsdk/tools/db/tx"
	"github.com/openimsdk/tools/utils/datautil"

	"github.com/openimsdk/protocol/user"
	"github.com/openimsdk/tools/errs"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache"
)

// UserDatabase 用户数据库接口
// 定义了用户管理的所有核心操作，包括用户信息管理、命令系统、统计分析等
type UserDatabase interface {
	// FindWithError 获取指定用户信息（严格模式）
	// 获取指定用户的信息，如果任何userID未找到，则返回错误
	// userIDs: 用户ID列表
	// 返回: 用户信息列表、错误信息
	FindWithError(ctx context.Context, userIDs []string) (users []*model.User, err error)

	// Find 获取指定用户信息（宽松模式）
	// 获取指定用户的信息，如果userID未找到，不返回错误，只返回找到的用户
	// userIDs: 用户ID列表
	// 返回: 用户信息列表、错误信息
	Find(ctx context.Context, userIDs []string) (users []*model.User, err error)

	// FindByNickname 根据昵称查找用户
	// 通过昵称搜索用户信息，支持模糊匹配
	// nickname: 用户昵称
	// 返回: 用户信息列表、错误信息
	FindByNickname(ctx context.Context, nickname string) (users []*model.User, err error)

	// FindNotification 查找通知账号
	// 获取指定级别的通知账号用户列表，用于系统消息推送
	// level: 通知级别
	// 返回: 用户信息列表、错误信息
	FindNotification(ctx context.Context, level int64) (users []*model.User, err error)

	// Create 创建用户
	// 批量插入用户，外部保证userID不重复且不存在于存储中
	// users: 用户信息列表
	// 返回: 错误信息
	Create(ctx context.Context, users []*model.User) (err error)

	// UpdateByMap 更新用户信息
	// 通过映射表更新用户信息（支持零值），外部保证userID存在
	// userID: 用户ID
	// args: 要更新的字段映射
	// 返回: 错误信息
	UpdateByMap(ctx context.Context, userID string, args map[string]any) (err error)

	// PageFindUser 分页查找用户
	// 根据用户级别范围分页查询用户列表
	// level1: 最小级别
	// level2: 最大级别
	// pagination: 分页参数
	// 返回: 总数、用户列表、错误信息
	PageFindUser(ctx context.Context, level1 int64, level2 int64, pagination pagination.Pagination) (count int64, users []*model.User, err error)

	// PageFindUserWithKeyword 带关键词的分页用户查询
	// 根据用户级别和关键词（用户ID或昵称）分页查询用户
	// level1: 最小级别
	// level2: 最大级别
	// userID: 用户ID关键词
	// nickName: 昵称关键词
	// pagination: 分页参数
	PageFindUserWithKeyword(ctx context.Context, level1 int64, level2 int64, userID string, nickName string, pagination pagination.Pagination) (count int64, users []*model.User, err error)

	// Page 分页获取用户
	// 不带任何过滤条件的分页用户查询，未找到不返回错误
	// pagination: 分页参数
	// 返回: 总数、用户列表、错误信息
	Page(ctx context.Context, pagination pagination.Pagination) (count int64, users []*model.User, err error)

	// IsExist 检查用户是否存在
	// 检查userIDs是否存在，只要有一个存在就返回true
	// userIDs: 用户ID列表
	// 返回: 是否存在、错误信息
	IsExist(ctx context.Context, userIDs []string) (exist bool, err error)

	// GetAllUserID 获取所有用户ID
	// 分页获取系统中所有用户的ID列表
	// pagination: 分页参数
	// 返回: 总数、用户ID列表、错误信息
	GetAllUserID(ctx context.Context, pagination pagination.Pagination) (int64, []string, error)

	// GetUserByID 根据ID获取用户
	// 通过用户ID获取单个用户信息，优先从缓存获取
	// userID: 用户ID
	// 返回: 用户信息、错误信息
	GetUserByID(ctx context.Context, userID string) (user *model.User, err error)

	// InitOnce 初始化用户（幂等操作）
	// 函数内部先查询存储中是否存在，如果存在则不做任何操作；如果不存在则插入
	// users: 要初始化的用户列表
	// 返回: 错误信息
	InitOnce(ctx context.Context, users []*model.User) (err error)

	// CountTotal 获取用户总数
	// 统计指定时间之前的用户总数，用于数据分析
	// before: 截止时间，nil表示不限制时间
	// 返回: 用户总数、错误信息
	CountTotal(ctx context.Context, before *time.Time) (int64, error)

	// CountRangeEverydayTotal 获取时间范围内的用户增量
	// 统计指定时间范围内每天的用户注册增量
	// start: 开始时间
	// end: 结束时间
	// 返回: 日期到用户数的映射、错误信息
	CountRangeEverydayTotal(ctx context.Context, start time.Time, end time.Time) (map[string]int64, error)

	// SortQuery 排序查询用户
	// 根据用户ID和昵称映射进行排序查询
	// userIDName: 用户ID到昵称的映射
	// asc: 是否升序排列
	// 返回: 排序后的用户列表、错误信息
	SortQuery(ctx context.Context, userIDName map[string]string, asc bool) ([]*model.User, error)

	// CRUD user command
	AddUserCommand(ctx context.Context, userID string, Type int32, UUID string, value string, ex string) error
	DeleteUserCommand(ctx context.Context, userID string, Type int32, UUID string) error
	UpdateUserCommand(ctx context.Context, userID string, Type int32, UUID string, val map[string]any) error
	GetUserCommands(ctx context.Context, userID string, Type int32) ([]*user.CommandInfoResp, error)
	GetAllUserCommands(ctx context.Context, userID string) ([]*user.AllCommandInfoResp, error)
}

// userDatabase 用户数据库实现
// 整合了用户数据库操作、缓存管理和事务控制
type userDatabase struct {
	tx     tx.Tx           // 事务管理接口，确保数据一致性
	userDB database.User   // 用户数据库接口，提供持久化存储
	cache  cache.UserCache // 用户缓存接口，提供高性能查询
}

// NewUserDatabase 创建用户数据库实例
// 初始化用户管理所需的所有组件：数据库、缓存、事务管理
func NewUserDatabase(userDB database.User, cache cache.UserCache, tx tx.Tx) UserDatabase {
	return &userDatabase{userDB: userDB, cache: cache, tx: tx}
}

// InitOnce 初始化用户（幂等操作）
// 检查用户是否已存在，只创建不存在的用户，确保操作幂等性
func (u *userDatabase) InitOnce(ctx context.Context, users []*model.User) error {
	// 提取用户模型中的用户ID
	userIDs := datautil.Slice(users, func(e *model.User) string {
		return e.UserID
	})

	// 查找数据库中已存在的用户
	existingUsers, err := u.userDB.Find(ctx, userIDs)
	if err != nil {
		return err
	}

	// 计算数据库中缺失的用户
	missingUsers := datautil.SliceAnySub(users, existingUsers, func(e *model.User) string {
		return e.UserID
	})

	// 创建缺失用户的记录
	if len(missingUsers) > 0 {
		if err := u.userDB.Create(ctx, missingUsers); err != nil {
			return err
		}
	}

	return nil
}

// FindWithError 获取指定用户信息（严格模式）
// 如果任何userID未找到，返回错误
func (u *userDatabase) FindWithError(ctx context.Context, userIDs []string) (users []*model.User, err error) {
	// 去重用户ID列表
	userIDs = datautil.Distinct(userIDs)

	// TODO: 添加逻辑来识别哪些用户ID是重复的，哪些用户ID未找到

	// 从缓存获取用户信息
	users, err = u.cache.GetUsersInfo(ctx, userIDs)
	if err != nil {
		return
	}

	// 检查是否所有用户都找到了
	if len(users) != len(userIDs) {
		err = errs.ErrRecordNotFound.WrapMsg("userID not found")
	}
	return
}

// Find 获取指定用户信息（宽松模式）
// 如果userID未找到，不返回错误，只返回找到的用户
func (u *userDatabase) Find(ctx context.Context, userIDs []string) (users []*model.User, err error) {
	return u.cache.GetUsersInfo(ctx, userIDs)
}

// FindByNickname 根据昵称查找用户
// 通过昵称搜索用户信息，支持精确匹配
func (u *userDatabase) FindByNickname(ctx context.Context, nickname string) (users []*model.User, err error) {
	return u.userDB.TakeByNickname(ctx, nickname)
}

// FindNotification 查找通知账号
// 获取指定级别的通知账号，用于系统消息和公告推送
func (u *userDatabase) FindNotification(ctx context.Context, level int64) (users []*model.User, err error) {
	return u.userDB.TakeNotification(ctx, level)
}

// Create 创建用户
// 批量插入用户，使用事务确保数据一致性，并清理相关缓存
func (u *userDatabase) Create(ctx context.Context, users []*model.User) (err error) {
	return u.tx.Transaction(ctx, func(ctx context.Context) error {
		// 先插入数据库
		if err = u.userDB.Create(ctx, users); err != nil {
			return err
		}
		// 再清理缓存，确保缓存与数据库一致性
		return u.cache.DelUsersInfo(datautil.Slice(users, func(e *model.User) string {
			return e.UserID
		})...).ChainExecDel(ctx)
	})
}

// UpdateByMap 更新用户信息
// 通过字段映射更新用户信息，支持零值更新，使用事务保证一致性
func (u *userDatabase) UpdateByMap(ctx context.Context, userID string, args map[string]any) (err error) {
	return u.tx.Transaction(ctx, func(ctx context.Context) error {
		// 先更新数据库
		if err := u.userDB.UpdateByMap(ctx, userID, args); err != nil {
			return err
		}
		// 再清理缓存
		return u.cache.DelUsersInfo(userID).ChainExecDel(ctx)
	})
}

// Page 分页获取用户
// 不带过滤条件的分页查询，未找到记录不返回错误
func (u *userDatabase) Page(ctx context.Context, pagination pagination.Pagination) (count int64, users []*model.User, err error) {
	return u.userDB.Page(ctx, pagination)
}

// PageFindUser 分页查找用户
// 根据用户级别范围进行分页查询，用于管理员界面的用户管理
func (u *userDatabase) PageFindUser(ctx context.Context, level1 int64, level2 int64, pagination pagination.Pagination) (count int64, users []*model.User, err error) {
	return u.userDB.PageFindUser(ctx, level1, level2, pagination)
}

// PageFindUserWithKeyword 带关键词的分页用户查询
// 支持按用户ID或昵称关键词搜索，结合级别过滤
func (u *userDatabase) PageFindUserWithKeyword(ctx context.Context, level1 int64, level2 int64, userID, nickName string, pagination pagination.Pagination) (count int64, users []*model.User, err error) {
	return u.userDB.PageFindUserWithKeyword(ctx, level1, level2, userID, nickName, pagination)
}

// IsExist 检查用户是否存在
// 检查userIDs中是否有任何一个用户存在，有一个存在即返回true
func (u *userDatabase) IsExist(ctx context.Context, userIDs []string) (exist bool, err error) {
	users, err := u.userDB.Find(ctx, userIDs)
	if err != nil {
		return false, err
	}
	if len(users) > 0 {
		return true, nil
	}
	return false, nil
}

// GetAllUserID 获取所有用户ID
// 分页获取系统中所有用户的ID列表，用于数据统计和批量操作
func (u *userDatabase) GetAllUserID(ctx context.Context, pagination pagination.Pagination) (total int64, userIDs []string, err error) {
	return u.userDB.GetAllUserID(ctx, pagination)
}

// GetUserByID 根据ID获取用户
// 优先从缓存获取单个用户信息，提供最高性能的单用户查询
func (u *userDatabase) GetUserByID(ctx context.Context, userID string) (user *model.User, err error) {
	return u.cache.GetUserInfo(ctx, userID)
}

// CountTotal 获取用户总数
// 统计指定时间之前的用户总数，用于运营数据分析
func (u *userDatabase) CountTotal(ctx context.Context, before *time.Time) (count int64, err error) {
	return u.userDB.CountTotal(ctx, before)
}

// CountRangeEverydayTotal 获取时间范围内的用户增量
// 统计指定时间范围内每天的用户注册增量，用于增长趋势分析
func (u *userDatabase) CountRangeEverydayTotal(ctx context.Context, start time.Time, end time.Time) (map[string]int64, error) {
	return u.userDB.CountRangeEverydayTotal(ctx, start, end)
}

// SortQuery 排序查询用户
// 根据用户ID和昵称映射进行自定义排序查询
func (u *userDatabase) SortQuery(ctx context.Context, userIDName map[string]string, asc bool) ([]*model.User, error) {
	return u.userDB.SortQuery(ctx, userIDName, asc)
}

// AddUserCommand 添加用户命令
// 为用户添加特定类型的命令记录，用于系统状态控制和业务逻辑管理
func (u *userDatabase) AddUserCommand(ctx context.Context, userID string, Type int32, UUID string, value string, ex string) error {
	return u.userDB.AddUserCommand(ctx, userID, Type, UUID, value, ex)
}

// DeleteUserCommand 删除用户命令
// 删除指定用户的特定命令记录，用于命令状态清理
func (u *userDatabase) DeleteUserCommand(ctx context.Context, userID string, Type int32, UUID string) error {
	return u.userDB.DeleteUserCommand(ctx, userID, Type, UUID)
}

// UpdateUserCommand 更新用户命令
// 更新指定用户命令的属性信息，支持部分字段更新
func (u *userDatabase) UpdateUserCommand(ctx context.Context, userID string, Type int32, UUID string, val map[string]any) error {
	return u.userDB.UpdateUserCommand(ctx, userID, Type, UUID, val)
}

// GetUserCommands 获取用户命令列表
// 获取指定用户和类型的所有命令记录，用于命令状态查询
func (u *userDatabase) GetUserCommands(ctx context.Context, userID string, Type int32) ([]*user.CommandInfoResp, error) {
	commands, err := u.userDB.GetUserCommand(ctx, userID, Type)
	return commands, err
}

// GetAllUserCommands 获取用户所有命令
// 获取指定用户的所有类型命令记录，提供完整的命令视图
func (u *userDatabase) GetAllUserCommands(ctx context.Context, userID string) ([]*user.AllCommandInfoResp, error) {
	commands, err := u.userDB.GetAllUserCommand(ctx, userID)
	return commands, err
}
