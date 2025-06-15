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

// Package jpush 实现极光推送服务
// 极光推送是国内主要的推送服务提供商之一，提供稳定的Android和iOS推送服务
package jpush

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/openimsdk/open-im-server/v3/internal/push/offlinepush/jpush/body"
	"github.com/openimsdk/open-im-server/v3/internal/push/offlinepush/options"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/tools/utils/httputil"
)

// JPush 极光推送客户端结构体
// 封装了极光推送服务的配置和HTTP客户端
type JPush struct {
	pushConf   *config.Push         // 推送配置，包含极光推送相关设置
	httpClient *httputil.HTTPClient // HTTP客户端，用于发送API请求
}

// NewClient 创建极光推送客户端实例
// 参数:
//   - pushConf: 推送配置，包含极光推送的AppKey、MasterSecret等
//
// 返回:
//   - *JPush: 极光推送客户端实例
func NewClient(pushConf *config.Push) *JPush {
	return &JPush{
		pushConf:   pushConf,
		httpClient: httputil.NewHTTPClient(httputil.NewClientConfig()),
	}
}

// Auth 认证方法（暂未实现）
// 极光推送使用HTTP Basic Auth，暂时未使用此方法
// 参数:
//   - apiKey: API密钥
//   - secretKey: 密钥
//   - timeStamp: 时间戳
//
// 返回:
//   - token: 认证令牌
//   - err: 错误信息
func (j *JPush) Auth(apiKey, secretKey string, timeStamp int64) (token string, err error) {
	return token, nil
}

// SetAlias 设置别名方法（暂未实现）
// 用于为设备设置别名，暂时未使用此方法
// 参数:
//   - cid: 设备ID
//   - alias: 别名
//
// 返回:
//   - resp: 响应结果
//   - err: 错误信息
func (j *JPush) SetAlias(cid, alias string) (resp string, err error) {
	return resp, nil
}

// getAuthorization 生成HTTP Basic认证头
// 极光推送使用HTTP Basic认证方式，需要将AppKey和MasterSecret进行Base64编码
// 参数:
//   - appKey: 应用密钥
//   - masterSecret: 主密钥
//
// 返回:
//   - string: 格式化的Authorization头值
func (j *JPush) getAuthorization(appKey string, masterSecret string) string {
	// 拼接AppKey和MasterSecret
	str := fmt.Sprintf("%s:%s", appKey, masterSecret)
	buf := []byte(str)

	// 进行Base64编码并格式化为Basic认证头
	Authorization := fmt.Sprintf("Basic %s", base64.StdEncoding.EncodeToString(buf))
	return Authorization
}

// Push 执行极光推送
// 构建推送消息并发送给指定用户列表
// 参数:
//   - ctx: 上下文
//   - userIDs: 目标用户ID列表
//   - title: 推送标题
//   - content: 推送内容
//   - opts: 推送选项，包含扩展信息和信号设置
//
// 返回:
//   - error: 推送失败时的错误信息
func (j *JPush) Push(ctx context.Context, userIDs []string, title, content string, opts *options.Opts) error {
	// 1. 设置推送平台：支持所有平台（iOS、Android、WinPhone等）
	var pf body.Platform
	pf.SetAll()

	// 2. 设置推送目标：使用用户ID作为别名
	var au body.Audience
	au.SetAlias(userIDs)

	// 3. 配置通知内容
	var no body.Notification

	// 设置扩展数据
	extras := make(map[string]string)
	extras["ex"] = opts.Ex // 业务扩展信息

	// 如果有客户端消息ID，加入扩展数据
	if opts.Signal.ClientMsgID != "" {
		extras["ClientMsgID"] = opts.Signal.ClientMsgID
	}

	// 启用iOS的可变内容模式，支持通知扩展
	no.IOSEnableMutableContent()

	// 设置通知的扩展数据、内容和平台特定配置
	no.SetExtras(extras)
	no.SetAlert(content, title, opts)
	no.SetAndroidIntent(j.pushConf)

	// 4. 配置自定义消息（透传消息）
	var msg body.Message
	msg.SetMsgContent(content) // 设置消息内容
	msg.SetTitle(title)        // 设置消息标题

	// 为自定义消息添加扩展信息
	if opts.Signal.ClientMsgID != "" {
		msg.SetExtras("ClientMsgID", opts.Signal.ClientMsgID)
	}
	msg.SetExtras("ex", opts.Ex)

	// 5. 配置推送选项
	var opt body.Options
	opt.SetApnsProduction(j.pushConf.IOSPush.Production) // 设置iOS推送环境（生产/开发）

	// 6. 构建完整的推送对象
	var pushObj body.PushObj
	pushObj.SetPlatform(&pf)     // 设置推送平台
	pushObj.SetAudience(&au)     // 设置推送目标
	pushObj.SetNotification(&no) // 设置通知内容
	pushObj.SetMessage(&msg)     // 设置自定义消息
	pushObj.SetOptions(&opt)     // 设置推送选项

	// 7. 发送推送请求
	var resp map[string]any
	return j.request(ctx, pushObj, &resp, 5)
}

// request 发送HTTP推送请求
// 向极光推送服务器发送推送请求并处理响应
// 参数:
//   - ctx: 上下文
//   - po: 推送对象，包含完整的推送信息
//   - resp: 响应结果指针
//   - timeout: 请求超时时间（秒）
//
// 返回:
//   - error: 请求失败时的错误信息
func (j *JPush) request(ctx context.Context, po body.PushObj, resp *map[string]any, timeout int) error {
	// 发送HTTP POST请求到极光推送API
	err := j.httpClient.PostReturn(
		ctx,
		j.pushConf.JPush.PushURL, // 极光推送API地址
		map[string]string{
			// 设置HTTP Basic认证头
			"Authorization": j.getAuthorization(j.pushConf.JPush.AppKey, j.pushConf.JPush.MasterSecret),
		},
		po,      // 推送对象作为请求体
		resp,    // 响应结果
		timeout, // 请求超时时间
	)

	if err != nil {
		return err
	}

	// 检查推送结果
	// 极光推送成功时sendno字段应该为"0"
	if (*resp)["sendno"] != "0" {
		return fmt.Errorf("jpush push failed %v", resp)
	}

	return nil
}
