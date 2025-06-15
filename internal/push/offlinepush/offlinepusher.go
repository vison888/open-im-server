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

// Package offlinepush 实现离线推送功能
// 支持多种第三方推送服务，包括个推(GeTui)、Firebase Cloud Messaging(FCM)、极光推送(JPush)等
package offlinepush

import (
	"context"
	"strings"

	"github.com/openimsdk/open-im-server/v3/internal/push/offlinepush/dummy"
	"github.com/openimsdk/open-im-server/v3/internal/push/offlinepush/fcm"
	"github.com/openimsdk/open-im-server/v3/internal/push/offlinepush/getui"
	"github.com/openimsdk/open-im-server/v3/internal/push/offlinepush/jpush"
	"github.com/openimsdk/open-im-server/v3/internal/push/offlinepush/options"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache"
)

// 支持的推送服务类型常量定义
const (
	geTUI    = "getui" // 个推服务标识
	firebase = "fcm"   // Firebase Cloud Messaging服务标识
	jPush    = "jpush" // 极光推送服务标识
)

// OfflinePusher 离线推送器接口
// 定义了所有离线推送服务必须实现的方法
// 使用策略模式，支持多种推送服务的统一调用
type OfflinePusher interface {
	// Push 执行离线推送
	// 参数:
	//   - ctx: 上下文，包含请求信息和超时控制
	//   - userIDs: 目标用户ID列表
	//   - title: 推送标题
	//   - content: 推送内容
	//   - opts: 推送选项，包含iOS音效、徽章数量、扩展信息等
	// 返回:
	//   - error: 推送失败时的错误信息
	Push(ctx context.Context, userIDs []string, title, content string, opts *options.Opts) error
}

// NewOfflinePusher 离线推送器工厂函数
// 根据配置选择合适的推送服务实现，使用工厂模式创建推送器实例
// 参数:
//   - pushConf: 推送服务配置，包含启用的推送服务类型和相关配置
//   - cache: 第三方缓存接口，用于存储推送令牌等信息
//   - fcmConfigPath: FCM配置文件路径，当使用FCM推送时需要
//
// 返回:
//   - OfflinePusher: 离线推送器实例
//   - error: 创建过程中的错误信息
func NewOfflinePusher(pushConf *config.Push, cache cache.ThirdCache, fcmConfigPath string) (OfflinePusher, error) {
	var offlinePusher OfflinePusher

	// 将推送服务类型转换为小写，确保配置的一致性
	pushConf.Enable = strings.ToLower(pushConf.Enable)

	// 根据配置的推送服务类型选择相应的实现
	switch pushConf.Enable {
	case geTUI:
		// 创建个推推送器
		// 个推是国内主要的推送服务提供商，支持Android和iOS推送
		offlinePusher = getui.NewClient(pushConf, cache)

	case firebase:
		// 创建Firebase Cloud Messaging推送器
		// FCM是Google提供的免费跨平台消息传递解决方案
		// 需要返回创建结果，因为FCM初始化可能失败（需要配置文件）
		return fcm.NewClient(pushConf, cache, fcmConfigPath)

	case jPush:
		// 创建极光推送器
		// 极光推送是国内主要的推送服务提供商之一
		offlinePusher = jpush.NewClient(pushConf)

	default:
		// 如果没有配置或配置了不支持的推送服务，使用空推送器
		// 空推送器不执行实际的推送操作，只记录警告日志
		// 这样可以确保系统在未配置推送服务时仍能正常运行
		offlinePusher = dummy.NewClient()
	}

	return offlinePusher, nil
}
