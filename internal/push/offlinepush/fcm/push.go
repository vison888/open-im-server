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

// Package fcm 实现Firebase Cloud Messaging推送服务
// FCM是Google提供的免费跨平台消息传递解决方案，支持Android、iOS和Web推送
package fcm

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/openimsdk/open-im-server/v3/internal/push/offlinepush/options"
	"github.com/openimsdk/tools/utils/httputil"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/tools/errs"
	"github.com/redis/go-redis/v9"
	"google.golang.org/api/option"
)

// 单次推送数量限制，防止单次请求过大导致超时或失败
const SinglePushCountLimit = 400

// 支持的终端平台列表，定义了FCM推送支持的设备类型
var Terminal = []int{constant.IOSPlatformID, constant.AndroidPlatformID, constant.WebPlatformID}

// Fcm FCM推送器结构体
// 封装了Firebase Cloud Messaging客户端和缓存接口
type Fcm struct {
	fcmMsgCli *messaging.Client // Firebase消息客户端，用于发送推送消息
	cache     cache.ThirdCache  // 第三方缓存接口，用于存储和获取推送令牌
}

// NewClient 创建并初始化FCM客户端
// 支持两种认证方式：服务账户密钥文件和认证URL
// 参数:
//   - pushConf: 推送配置，包含FCM相关设置
//   - cache: 缓存接口，用于令牌管理
//   - fcmConfigPath: FCM配置文件基础路径
//
// 返回:
//   - *Fcm: FCM推送器实例
//   - error: 初始化失败时的错误信息
func NewClient(pushConf *config.Push, cache cache.ThirdCache, fcmConfigPath string) (*Fcm, error) {
	var opt option.ClientOption

	// 根据配置选择认证方式
	switch {
	case len(pushConf.FCM.FilePath) != 0:
		// 方式1: 使用服务账户密钥文件进行认证
		// 拼接完整的配置文件路径
		credentialsFilePath := filepath.Join(fcmConfigPath, pushConf.FCM.FilePath)
		opt = option.WithCredentialsFile(credentialsFilePath)

	case len(pushConf.FCM.AuthURL) != 0:
		// 方式2: 通过HTTP URL获取认证信息
		// 从指定URL下载服务账户密钥内容
		client := httputil.NewHTTPClient(httputil.NewClientConfig())
		resp, err := client.Get(pushConf.FCM.AuthURL)
		if err != nil {
			return nil, err
		}
		opt = option.WithCredentialsJSON(resp)

	default:
		// 如果两种认证方式都未配置，返回错误
		return nil, errs.New("no FCM config").Wrap()
	}

	// 初始化Firebase应用实例
	fcmApp, err := firebase.NewApp(context.Background(), nil, opt)
	if err != nil {
		return nil, errs.Wrap(err)
	}

	// 获取消息客户端
	ctx := context.Background()
	fcmMsgClient, err := fcmApp.Messaging(ctx)
	if err != nil {
		return nil, errs.Wrap(err)
	}

	return &Fcm{fcmMsgCli: fcmMsgClient, cache: cache}, nil
}

// Push 执行FCM推送
// 支持批量推送，自动处理推送令牌获取、消息构建、批量发送等逻辑
// 参数:
//   - ctx: 上下文
//   - userIDs: 目标用户ID列表
//   - title: 推送标题
//   - content: 推送内容
//   - opts: 推送选项，包含iOS音效、徽章设置等
//
// 返回:
//   - error: 推送失败时的错误信息
func (f *Fcm) Push(ctx context.Context, userIDs []string, title, content string, opts *options.Opts) error {
	// 获取所有用户的推送令牌
	// allTokens: map[用户ID][]推送令牌
	allTokens := make(map[string][]string, 0)
	for _, account := range userIDs {
		var personTokens []string

		// 遍历支持的平台，获取每个平台的推送令牌
		for _, v := range Terminal {
			Token, err := f.cache.GetFcmToken(ctx, account, v)
			if err == nil {
				personTokens = append(personTokens, Token)
			}
		}
		allTokens[account] = personTokens
	}

	// 推送结果统计
	Success := 0 // 成功推送数量
	Fail := 0    // 失败推送数量

	// 构建通知内容
	notification := &messaging.Notification{}
	notification.Body = content
	notification.Title = title

	// 消息列表，用于批量推送
	var messages []*messaging.Message

	// 错误信息构建器
	var sendErrBuilder strings.Builder // 发送错误信息
	var msgErrBuilder strings.Builder  // 消息错误信息

	// 为每个用户构建推送消息
	for userID, personTokens := range allTokens {
		// 构建APNS配置（iOS推送专用）
		apns := &messaging.APNSConfig{Payload: &messaging.APNSPayload{Aps: &messaging.Aps{Sound: opts.IOSPushSound}}}

		// 检查是否达到单次推送限制，如果达到则先发送已构建的消息
		messageCount := len(messages)
		if messageCount >= SinglePushCountLimit {
			response, err := f.fcmMsgCli.SendEach(ctx, messages)
			if err != nil {
				// 记录发送失败
				Fail = Fail + messageCount
				sendErrBuilder.WriteString(err.Error())
				sendErrBuilder.WriteByte('.')
			} else {
				// 统计发送结果
				Success = Success + response.SuccessCount
				Fail = Fail + response.FailureCount

				// 记录具体的消息发送失败信息
				if response.FailureCount != 0 {
					for i := range response.Responses {
						if !response.Responses[i].Success {
							msgErrBuilder.WriteString(response.Responses[i].Error.Error())
							msgErrBuilder.WriteByte('.')
						}
					}
				}
			}
			// 清空消息列表，准备下一批推送
			messages = messages[0:0]
		}

		// 处理iOS徽章数量
		if opts.IOSBadgeCount {
			// 增加用户徽章未读数量
			unreadCountSum, err := f.cache.IncrUserBadgeUnreadCountSum(ctx, userID)
			if err == nil {
				apns.Payload.Aps.Badge = &unreadCountSum
			} else {
				// 徽章处理失败，跳过该用户
				Fail++
				continue
			}
		} else {
			// 获取当前徽章数量
			unreadCountSum, err := f.cache.GetUserBadgeUnreadCountSum(ctx, userID)
			if err == nil && unreadCountSum != 0 {
				apns.Payload.Aps.Badge = &unreadCountSum
			} else if err == redis.Nil || unreadCountSum == 0 {
				// 如果没有未读消息，设置徽章为1（表示有新消息）
				zero := 1
				apns.Payload.Aps.Badge = &zero
			} else {
				// 获取徽章数量失败，跳过该用户
				Fail++
				continue
			}
		}

		// 为用户的每个设备令牌创建推送消息
		for _, token := range personTokens {
			temp := &messaging.Message{
				Data:         map[string]string{"ex": opts.Ex}, // 扩展数据
				Token:        token,                            // 设备推送令牌
				Notification: notification,                     // 通知内容
				APNS:         apns,                             // iOS专用配置
			}
			messages = append(messages, temp)
		}
	}

	// 发送剩余的消息
	messageCount := len(messages)
	if messageCount > 0 {
		response, err := f.fcmMsgCli.SendEach(ctx, messages)
		if err != nil {
			Fail = Fail + messageCount
		} else {
			Success = Success + response.SuccessCount
			Fail = Fail + response.FailureCount
		}
	}

	// 如果有失败的推送，返回详细的错误信息
	if Fail != 0 {
		return errs.New(fmt.Sprintf("%d message send failed;send err:%s;message err:%s",
			Fail, sendErrBuilder.String(), msgErrBuilder.String())).Wrap()
	}

	return nil
}
