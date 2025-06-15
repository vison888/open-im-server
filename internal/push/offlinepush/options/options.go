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

// Package options 定义离线推送的选项和配置
// 包含推送时需要的各种参数，如iOS推送音效、徽章数量、扩展信息等
package options

// Opts 推送选项结构体
// 包含执行离线推送时需要的所有可选参数
// 不同的推送服务可能使用不同的字段
type Opts struct {
	Signal        *Signal // 信号消息配置，包含客户端消息ID等标识信息
	IOSPushSound  string  // iOS推送音效文件名，用于指定推送通知的提示音
	IOSBadgeCount bool    // iOS徽章数量控制，true表示增加徽章数量，false表示设置为固定值
	Ex            string  // 扩展信息，用于传递额外的自定义数据给客户端
}

// Signal 信号消息结构体
// 用于标识和追踪特定的消息推送
type Signal struct {
	ClientMsgID string // 客户端消息ID，用于消息去重和状态跟踪
}
