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

// Package dummy 实现空推送器
// 当系统未配置任何推送服务时使用，确保系统正常运行的同时提供配置提示
package dummy

import (
	"context"
	"sync/atomic"

	"github.com/openimsdk/open-im-server/v3/internal/push/offlinepush/options"
	"github.com/openimsdk/tools/log"
)

// NewClient 创建空推送器实例
// 返回一个不执行实际推送操作的推送器，用于未配置推送服务的场景
func NewClient() *Dummy {
	return &Dummy{}
}

// Dummy 空推送器结构体
// 实现了OfflinePusher接口，但不执行实际的推送操作
// 主要用于系统未配置推送服务时的占位符
type Dummy struct {
	v atomic.Bool // 原子布尔值，用于确保警告日志只输出一次
}

// Push 空推送实现
// 不执行实际的推送操作，只在第一次调用时输出警告日志
// 使用原子操作确保多并发环境下警告日志只输出一次
// 参数:
//   - ctx: 上下文，包含请求信息
//   - userIDs: 目标用户ID列表（此实现中未使用）
//   - title: 推送标题（此实现中未使用）
//   - content: 推送内容（此实现中未使用）
//   - opts: 推送选项（此实现中未使用）
//
// 返回:
//   - error: 始终返回nil，表示操作成功
func (d *Dummy) Push(ctx context.Context, userIDs []string, title, content string, opts *options.Opts) error {
	// 使用原子操作确保警告日志只输出一次
	// CompareAndSwap原子地比较v的值是否为false，如果是则设置为true并返回true
	if d.v.CompareAndSwap(false, true) {
		// 输出警告日志，提示用户配置推送服务
		// 日志包含配置文件路径信息，方便用户定位配置位置
		log.ZWarn(ctx, "dummy push", nil, "ps", "the offline push is not configured. to configure it, please go to config/openim-push.yml")
	}
	// 返回nil表示操作成功，不影响系统正常运行
	return nil
}
