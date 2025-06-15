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

// Package getui 个推推送服务数据结构定义
// 包含个推API请求和响应的所有数据结构
package getui

import (
	"fmt"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
)

// Resp 个推API通用响应结构体
// 所有个推API的响应都包含这些基础字段
type Resp struct {
	Code int    `json:"code"` // 响应代码，0表示成功
	Msg  string `json:"msg"`  // 响应消息
	Data any    `json:"data"` // 具体的响应数据，类型根据不同API而定
}

// parseError 解析响应错误
// 根据响应代码判断请求是否成功，并返回相应的错误
// 返回:
//   - err: 解析后的错误信息，成功时为nil
func (r *Resp) parseError() (err error) {
	switch r.Code {
	case tokenExpireCode:
		// 令牌过期错误
		err = ErrTokenExpire
	case 0:
		// 成功响应
		err = nil
	default:
		// 其他错误，返回详细的错误信息
		err = fmt.Errorf("code %d, msg %s", r.Code, r.Msg)
	}
	return err
}

// RespI 响应接口
// 定义了所有响应结构体必须实现的方法
type RespI interface {
	parseError() error
}

// AuthReq 认证请求结构体
// 用于向个推服务器请求认证令牌
type AuthReq struct {
	Sign      string `json:"sign"`      // HMAC-SHA256签名
	Timestamp string `json:"timestamp"` // 时间戳字符串
	AppKey    string `json:"appkey"`    // 应用密钥
}

// AuthResp 认证响应结构体
// 包含认证成功后返回的令牌信息
type AuthResp struct {
	ExpireTime string `json:"expire_time"` // 令牌过期时间（秒）
	Token      string `json:"token"`       // 认证令牌
}

// TaskResp 任务响应结构体
// 创建推送任务后返回的任务ID
type TaskResp struct {
	TaskID string `json:"taskID"` // 推送任务ID
}

// Settings 推送设置结构体
// 包含推送消息的各种设置选项
type Settings struct {
	TTL *int64 `json:"ttl"` // 消息生存时间（毫秒），超时后消息失效
}

// Audience 推送目标结构体
// 定义推送消息的目标用户
type Audience struct {
	Alias []string `json:"alias"` // 目标用户别名列表（通常是用户ID）
}

// PushMessage 推送消息结构体
// 定义推送消息的内容格式
type PushMessage struct {
	Notification *Notification `json:"notification,omitempty"` // 通知消息，用于显示在通知栏
	Transmission *string       `json:"transmission,omitempty"` // 透传消息，直接传递给应用处理
}

// PushChannel 推送渠道结构体
// 定义不同平台的推送配置
type PushChannel struct {
	Ios     *Ios     `json:"ios"`     // iOS平台推送配置
	Android *Android `json:"android"` // Android平台推送配置
}

// PushReq 推送请求结构体
// 个推推送API的完整请求结构
type PushReq struct {
	RequestID   *string      `json:"request_id"`   // 请求ID，用于追踪和去重
	Settings    *Settings    `json:"settings"`     // 推送设置
	Audience    *Audience    `json:"audience"`     // 推送目标
	PushMessage *PushMessage `json:"push_message"` // 推送消息内容
	PushChannel *PushChannel `json:"push_channel"` // 推送渠道配置
	IsAsync     *bool        `json:"is_async"`     // 是否异步推送
	TaskID      *string      `json:"taskid"`       // 任务ID（批量推送时使用）
}

// Ios iOS平台推送配置结构体
// 包含iOS特有的推送参数
type Ios struct {
	NotificationType *string `json:"type"`       // 通知类型
	AutoBadge        *string `json:"auto_badge"` // 自动徽章设置
	Aps              struct {
		Sound string `json:"sound"` // 推送音效
		Alert Alert  `json:"alert"` // 推送内容
	} `json:"aps"` // Apple Push Service配置
}

// Alert iOS推送内容结构体
// 定义iOS推送通知的标题和内容
type Alert struct {
	Title string `json:"title"` // 推送标题
	Body  string `json:"body"`  // 推送内容
}

// Android Android平台推送配置结构体
// 包含Android和各厂商推送的配置
type Android struct {
	Ups struct {
		Notification Notification `json:"notification"` // 通知配置
		Options      Options      `json:"options"`      // 厂商推送选项
	} `json:"ups"` // 统一推送服务配置
}

// Notification 通知结构体
// 定义推送通知的基本信息
type Notification struct {
	Title       string `json:"title"`       // 通知标题
	Body        string `json:"body"`        // 通知内容
	ChannelID   string `json:"channelID"`   // 通知渠道ID
	ChannelName string `json:"ChannelName"` // 通知渠道名称
	ClickType   string `json:"click_type"`  // 点击类型（如启动应用）
}

// Options 厂商推送选项结构体
// 包含华为、小米、vivo等厂商的特殊推送配置
type Options struct {
	HW struct {
		DefaultSound bool   `json:"/message/android/notification/default_sound"` // 华为：使用默认声音
		ChannelID    string `json:"/message/android/notification/channel_id"`    // 华为：通知渠道ID
		Sound        string `json:"/message/android/notification/sound"`         // 华为：自定义声音
		Importance   string `json:"/message/android/notification/importance"`    // 华为：重要性级别
	} `json:"HW"` // 华为推送配置

	XM struct {
		ChannelID string `json:"/extra.channel_id"` // 小米：通知渠道ID
	} `json:"XM"` // 小米推送配置

	VV struct {
		Classification int `json:"/classification"` // vivo：消息分类
	} `json:"VV"` // vivo推送配置
}

// Payload 推送负载结构体
// 用于传递额外的推送信息
type Payload struct {
	IsSignal bool `json:"isSignal"` // 是否为信号消息
}

// newPushReq 创建基础推送请求
// 根据推送配置和内容创建推送请求结构体
// 参数:
//   - pushConf: 推送配置
//   - title: 推送标题
//   - content: 推送内容
//
// 返回:
//   - PushReq: 构建好的推送请求
func newPushReq(pushConf *config.Push, title, content string) PushReq {
	pushReq := PushReq{
		PushMessage: &PushMessage{
			Notification: &Notification{
				Title:       title,                      // 设置推送标题
				Body:        content,                    // 设置推送内容
				ClickType:   "startapp",                 // 点击后启动应用
				ChannelID:   pushConf.GeTui.ChannelID,   // 使用配置的渠道ID
				ChannelName: pushConf.GeTui.ChannelName, // 使用配置的渠道名称
			},
		},
	}
	return pushReq
}

// newBatchPushReq 创建批量推送请求
// 用于批量推送的请求结构体，需要指定用户列表和任务ID
// 参数:
//   - userIDs: 目标用户ID列表
//   - taskID: 推送任务ID
//
// 返回:
//   - PushReq: 构建好的批量推送请求
func newBatchPushReq(userIDs []string, taskID string) PushReq {
	IsAsync := true // 批量推送使用异步模式
	return PushReq{
		Audience: &Audience{Alias: userIDs}, // 设置推送目标用户列表
		IsAsync:  &IsAsync,                  // 启用异步推送
		TaskID:   &taskID,                   // 设置任务ID
	}
}

// setPushChannel 设置推送渠道配置
// 为推送请求配置iOS和Android平台的推送参数
// 参数:
//   - title: 推送标题
//   - body: 推送内容
func (pushReq *PushReq) setPushChannel(title string, body string) {
	pushReq.PushChannel = &PushChannel{}

	// 配置iOS推送
	pushReq.PushChannel.Ios = &Ios{}
	notify := "notify"
	pushReq.PushChannel.Ios.NotificationType = &notify                  // 设置为通知类型
	pushReq.PushChannel.Ios.Aps.Sound = "default"                       // 使用默认声音
	pushReq.PushChannel.Ios.Aps.Alert = Alert{Title: title, Body: body} // 设置推送内容

	// 配置Android推送
	pushReq.PushChannel.Android = &Android{}
	pushReq.PushChannel.Android.Ups.Notification = Notification{
		Title:     title,      // 设置推送标题
		Body:      body,       // 设置推送内容
		ClickType: "startapp", // 点击后启动应用
	}

	// 配置各厂商的推送选项
	pushReq.PushChannel.Android.Ups.Options = Options{
		// 华为推送配置
		HW: struct {
			DefaultSound bool   `json:"/message/android/notification/default_sound"`
			ChannelID    string `json:"/message/android/notification/channel_id"`
			Sound        string `json:"/message/android/notification/sound"`
			Importance   string `json:"/message/android/notification/importance"`
		}{
			ChannelID:  "RingRing4",    // 华为通知渠道ID
			Sound:      "/raw/ring001", // 华为自定义声音
			Importance: "NORMAL",       // 华为重要性级别
		},

		// 小米推送配置
		XM: struct {
			ChannelID string `json:"/extra.channel_id"`
		}{
			ChannelID: "high_system", // 小米高优先级系统渠道
		},

		// vivo推送配置
		VV: struct {
			Classification int "json:\"/classification\""
		}{
			Classification: 1, // vivo消息分类：重要消息
		},
	}
}
