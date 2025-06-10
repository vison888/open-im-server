// Copyright © 2024 OpenIM. All rights reserved.
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

// Package controller OpenIM存储控制器包
//
// 本包提供了OpenIM系统所有存储相关的控制器实现，作为数据访问层的核心组件。
// 控制器层位于业务逻辑层和数据存储层之间，负责：
//
// 核心功能：
//   - 统一数据访问接口：为上层业务提供统一的数据操作API
//   - 缓存管理：整合Redis缓存和数据库存储，提供高性能数据访问
//   - 事务控制：确保复杂业务操作的数据一致性
//   - 数据模型转换：处理业务对象和存储模型之间的转换
//
// 主要控制器：
//   - AuthDatabase: 用户认证和Token管理
//   - UserDatabase: 用户基础信息管理
//   - FriendDatabase: 好友关系管理
//   - GroupDatabase: 群组管理
//   - BlackDatabase: 黑名单管理
//   - ConversationDatabase: 会话管理
//   - CommonMsgDatabase: 消息存储和查询
//   - MsgTransferDatabase: 消息传输和队列
//   - PushDatabase: 消息推送
//   - S3Database: 对象存储管理
//   - ThirdDatabase: 第三方服务集成
//
// 设计原则：
//   - 接口分离：每个控制器都定义了清晰的接口
//   - 依赖注入：通过构造函数注入依赖组件
//   - 缓存优先：优先使用缓存，必要时回源到数据库
//   - 事务安全：复杂操作使用事务确保数据一致性
//   - 错误处理：统一的错误处理和日志记录
//
// 使用示例：
//
//	// 创建用户控制器
//	userCtrl := NewUserDatabase(userDB, cache, tx)
//
//	// 查找用户
//	users, err := userCtrl.Find(ctx, userIDs)
//
//	// 创建好友控制器
//	friendCtrl := NewFriendDatabase(friendDB, friendRequestDB, cache, tx)
//
//	// 检查好友关系
//	inFriends, _, err := friendCtrl.CheckIn(ctx, userID1, userID2)
package controller // import "github.com/openimsdk/open-im-server/v3/pkg/common/storage/controller"
