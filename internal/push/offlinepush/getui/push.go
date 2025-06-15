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

// Package getui 实现个推推送服务
// 个推是国内主要的推送服务提供商，提供Android和iOS推送服务
package getui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"sync"
	"time"

	"github.com/openimsdk/open-im-server/v3/internal/push/offlinepush/options"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
	"github.com/openimsdk/tools/utils/httputil"
	"github.com/openimsdk/tools/utils/splitter"
	"github.com/redis/go-redis/v9"
)

// 错误定义
var (
	ErrTokenExpire = errs.New("token expire")     // 令牌过期错误
	ErrUserIDEmpty = errs.New("userIDs is empty") // 用户ID列表为空错误
)

// 个推API相关常量定义
const (
	pushURL      = "/push/single/alias" // 单个推送API路径
	authURL      = "/auth"              // 认证API路径
	taskURL      = "/push/list/message" // 创建推送任务API路径
	batchPushURL = "/push/list/alias"   // 批量推送API路径

	// 响应代码
	tokenExpireCode = 10001 // 令牌过期的响应代码

	// 缓存时间设置
	tokenExpireTime = 60 * 60 * 23        // 令牌过期时间：23小时（秒）
	taskIDTTL       = 1000 * 60 * 60 * 24 // 任务ID生存时间：24小时（毫秒）
)

// Client 个推推送客户端
// 封装了个推推送服务的所有功能，包括认证、推送、批量推送等
type Client struct {
	cache           cache.ThirdCache     // 第三方缓存接口，用于存储令牌和任务ID
	tokenExpireTime int64                // 令牌过期时间配置
	taskIDTTL       int64                // 任务ID生存时间配置
	pushConf        *config.Push         // 推送配置
	httpClient      *httputil.HTTPClient // HTTP客户端，用于API请求
}

// NewClient 创建个推推送客户端实例
// 参数:
//   - pushConf: 推送配置，包含个推相关设置
//   - cache: 缓存接口，用于令牌和任务ID管理
//
// 返回:
//   - *Client: 个推推送客户端实例
func NewClient(pushConf *config.Push, cache cache.ThirdCache) *Client {
	return &Client{
		cache:           cache,
		tokenExpireTime: tokenExpireTime,
		taskIDTTL:       taskIDTTL,
		pushConf:        pushConf,
		httpClient:      httputil.NewHTTPClient(httputil.NewClientConfig()),
	}
}

// Push 执行个推推送
// 支持单个推送和批量推送，自动处理令牌获取和刷新
// 参数:
//   - ctx: 上下文
//   - userIDs: 目标用户ID列表
//   - title: 推送标题
//   - content: 推送内容
//   - opts: 推送选项
//
// 返回:
//   - error: 推送失败时的错误信息
func (g *Client) Push(ctx context.Context, userIDs []string, title, content string, opts *options.Opts) error {
	// 从缓存获取认证令牌
	token, err := g.cache.GetGetuiToken(ctx)
	if err != nil {
		if errs.Unwrap(err) == redis.Nil {
			// 令牌不存在，需要重新获取
			log.ZDebug(ctx, "getui token not exist in redis")
			token, err = g.getTokenAndSave2Redis(ctx)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}

	// 构建推送请求
	pushReq := newPushReq(g.pushConf, title, content)
	pushReq.setPushChannel(title, content)

	// 根据用户数量选择推送方式
	if len(userIDs) > 1 {
		// 批量推送：多个用户
		maxNum := 999 // 个推批量推送的最大用户数限制
		if len(userIDs) > maxNum {
			// 如果用户数量超过限制，进行分批处理
			s := splitter.NewSplitter(maxNum, userIDs)
			wg := sync.WaitGroup{}
			wg.Add(len(s.GetSplitResult()))

			// 并发处理每个分批
			for i, v := range s.GetSplitResult() {
				go func(index int, userIDs []string) {
					defer wg.Done()
					// 对每个分批再次分割，确保不超过限制
					for i := 0; i < len(userIDs); i += maxNum {
						end := i + maxNum
						if end > len(userIDs) {
							end = len(userIDs)
						}
						if err = g.batchPush(ctx, token, userIDs[i:end], pushReq); err != nil {
							log.ZError(ctx, "batchPush failed", err, "index", index, "token", token, "req", pushReq)
						}
					}
					// 处理完整的分批
					if err = g.batchPush(ctx, token, userIDs, pushReq); err != nil {
						log.ZError(ctx, "batchPush failed", err, "index", index, "token", token, "req", pushReq)
					}
				}(i, v.Item)
			}
			wg.Wait()
		} else {
			// 用户数量在限制范围内，直接批量推送
			err = g.batchPush(ctx, token, userIDs, pushReq)
		}
	} else if len(userIDs) == 1 {
		// 单个推送：只有一个用户
		err = g.singlePush(ctx, token, userIDs[0], pushReq)
	} else {
		// 用户列表为空
		return ErrUserIDEmpty
	}

	// 处理令牌过期情况
	switch err {
	case ErrTokenExpire:
		// 令牌过期，重新获取令牌
		token, err = g.getTokenAndSave2Redis(ctx)
	}

	return err
}

// Auth 获取认证令牌
// 使用HMAC-SHA256算法生成签名进行认证
// 参数:
//   - ctx: 上下文
//   - timeStamp: 时间戳（毫秒）
//
// 返回:
//   - token: 认证令牌
//   - expireTime: 令牌过期时间
//   - err: 认证失败时的错误信息
func (g *Client) Auth(ctx context.Context, timeStamp int64) (token string, expireTime int64, err error) {
	// 生成HMAC-SHA256签名
	// 签名内容：AppKey + 时间戳 + MasterSecret
	h := sha256.New()
	h.Write(
		[]byte(g.pushConf.GeTui.AppKey + strconv.Itoa(int(timeStamp)) + g.pushConf.GeTui.MasterSecret),
	)
	sign := hex.EncodeToString(h.Sum(nil))

	// 构建认证请求
	reqAuth := AuthReq{
		Sign:      sign,
		Timestamp: strconv.Itoa(int(timeStamp)),
		AppKey:    g.pushConf.GeTui.AppKey,
	}

	// 发送认证请求
	respAuth := AuthResp{}
	err = g.request(ctx, authURL, reqAuth, "", &respAuth)
	if err != nil {
		return "", 0, err
	}

	// 解析过期时间
	expire, err := strconv.Atoi(respAuth.ExpireTime)
	return respAuth.Token, int64(expire), err
}

// GetTaskID 获取推送任务ID
// 批量推送需要先创建任务，然后使用任务ID进行推送
// 参数:
//   - ctx: 上下文
//   - token: 认证令牌
//   - pushReq: 推送请求
//
// 返回:
//   - string: 任务ID
//   - error: 获取失败时的错误信息
func (g *Client) GetTaskID(ctx context.Context, token string, pushReq PushReq) (string, error) {
	respTask := TaskResp{}
	// 设置任务生存时间（5分钟）
	ttl := int64(1000 * 60 * 5)
	pushReq.Settings = &Settings{TTL: &ttl}

	err := g.request(ctx, taskURL, pushReq, token, &respTask)
	if err != nil {
		return "", errs.Wrap(err)
	}

	return respTask.TaskID, nil
}

// batchPush 批量推送
// 最大支持999个用户的批量推送
// 参数:
//   - ctx: 上下文
//   - token: 认证令牌
//   - userIDs: 用户ID列表（最多999个）
//   - pushReq: 推送请求
//
// 返回:
//   - error: 推送失败时的错误信息
func (g *Client) batchPush(ctx context.Context, token string, userIDs []string, pushReq PushReq) error {
	// 先获取任务ID
	taskID, err := g.GetTaskID(ctx, token, pushReq)
	if err != nil {
		return err
	}

	// 构建批量推送请求
	pushReq = newBatchPushReq(userIDs, taskID)
	return g.request(ctx, batchPushURL, pushReq, token, nil)
}

// singlePush 单个推送
// 向单个用户发送推送消息
// 参数:
//   - ctx: 上下文
//   - token: 认证令牌
//   - userID: 目标用户ID
//   - pushReq: 推送请求
//
// 返回:
//   - error: 推送失败时的错误信息
func (g *Client) singlePush(ctx context.Context, token, userID string, pushReq PushReq) error {
	// 设置请求ID和目标用户
	operationID := mcontext.GetOperationID(ctx)
	pushReq.RequestID = &operationID
	pushReq.Audience = &Audience{Alias: []string{userID}}

	return g.request(ctx, pushURL, pushReq, token, nil)
}

// request 发送HTTP请求
// 统一的HTTP请求处理方法
// 参数:
//   - ctx: 上下文
//   - url: 请求路径
//   - input: 请求体
//   - token: 认证令牌
//   - output: 响应结果
//
// 返回:
//   - error: 请求失败时的错误信息
func (g *Client) request(ctx context.Context, url string, input any, token string, output any) error {
	// 设置请求头
	header := map[string]string{"token": token}

	// 封装响应结构
	resp := &Resp{}
	resp.Data = output

	// 发送HTTP请求
	return g.postReturn(ctx, g.pushConf.GeTui.PushUrl+url, header, input, resp, 3)
}

// postReturn 发送POST请求并处理响应
// 参数:
//   - ctx: 上下文
//   - url: 完整的请求URL
//   - header: 请求头
//   - input: 请求体
//   - output: 响应结果
//   - timeout: 超时时间（秒）
//
// 返回:
//   - error: 请求失败时的错误信息
func (g *Client) postReturn(
	ctx context.Context,
	url string,
	header map[string]string,
	input any,
	output RespI,
	timeout int,
) error {
	// 发送HTTP POST请求
	err := g.httpClient.PostReturn(ctx, url, header, input, output, timeout)
	if err != nil {
		return err
	}

	// 解析响应错误
	return output.parseError()
}

// getTokenAndSave2Redis 获取令牌并保存到Redis
// 获取新的认证令牌并缓存到Redis中
// 参数:
//   - ctx: 上下文
//
// 返回:
//   - token: 认证令牌
//   - err: 获取失败时的错误信息
func (g *Client) getTokenAndSave2Redis(ctx context.Context) (token string, err error) {
	// 获取当前时间戳并进行认证
	token, _, err = g.Auth(ctx, time.Now().UnixNano()/1e6)
	if err != nil {
		return
	}

	// 将令牌保存到Redis，过期时间23小时
	err = g.cache.SetGetuiToken(ctx, token, 60*60*23)
	if err != nil {
		return
	}

	return token, nil
}

// GetTaskIDAndSave2Redis 获取任务ID并保存到Redis
// 获取推送任务ID并缓存到Redis中（当前未使用）
// 参数:
//   - ctx: 上下文
//   - token: 认证令牌
//   - pushReq: 推送请求
//
// 返回:
//   - taskID: 任务ID
//   - err: 获取失败时的错误信息
func (g *Client) GetTaskIDAndSave2Redis(ctx context.Context, token string, pushReq PushReq) (taskID string, err error) {
	// 设置任务生存时间
	pushReq.Settings = &Settings{TTL: &g.taskIDTTL}

	// 获取任务ID
	taskID, err = g.GetTaskID(ctx, token, pushReq)
	if err != nil {
		return
	}

	// 保存任务ID到Redis
	err = g.cache.SetGetuiTaskID(ctx, taskID, g.tokenExpireTime)
	if err != nil {
		return
	}

	return token, nil
}
