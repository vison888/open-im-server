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

// context.go - 用户连接上下文管理模块
//
// 功能概述:
// 1. 封装HTTP请求的上下文信息，提供统一的参数访问接口
// 2. 实现Go标准库的context.Context接口，支持上下文传递
// 3. 解析和验证WebSocket连接建立时的各种参数
// 4. 提供请求参数的类型转换和默认值处理
// 5. 支持请求头和查询参数的统一访问
//
// 设计思路:
// - 上下文抽象: 将HTTP请求封装为上下文对象，便于参数传递
// - 参数统一: 支持从URL查询参数和HTTP头部获取配置信息
// - 类型安全: 提供类型安全的参数访问方法，避免类型错误
// - 验证集成: 内置参数验证逻辑，确保连接参数的合法性
// - 兼容性: 实现标准context.Context接口，与现有代码兼容
//
// 核心特性:
// - 参数解析: 自动解析URL参数和HTTP头部
// - 类型转换: 字符串到数值类型的安全转换
// - 默认值: 为可选参数提供合理的默认值
// - 错误处理: 完善的参数验证和错误报告

package msggateway

import (
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/servererrs"

	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/tools/utils/encrypt"
	"github.com/openimsdk/tools/utils/stringutil"
	"github.com/openimsdk/tools/utils/timeutil"
)

// UserConnContext 用户连接上下文结构体
// 设计思路：
// 1. 封装HTTP请求的所有上下文信息
// 2. 实现context.Context接口，支持标准上下文操作
// 3. 提供便捷的参数访问方法
// 4. 自动生成连接唯一标识
type UserConnContext struct {
	RespWriter http.ResponseWriter // HTTP响应写入器
	Req        *http.Request       // HTTP请求对象
	Path       string              // 请求路径
	Method     string              // HTTP方法
	RemoteAddr string              // 客户端远程地址（包含代理信息）
	ConnID     string              // 连接唯一标识符
}

// Deadline 实现context.Context接口 - 获取上下文截止时间
// 当前实现返回零值，表示无截止时间限制
func (c *UserConnContext) Deadline() (deadline time.Time, ok bool) {
	return
}

// Done 实现context.Context接口 - 获取上下文完成通道
// 当前实现返回nil，表示上下文不会主动取消
func (c *UserConnContext) Done() <-chan struct{} {
	return nil
}

// Err 实现context.Context接口 - 获取上下文错误
// 当前实现返回nil，表示上下文无错误
func (c *UserConnContext) Err() error {
	return nil
}

// Value 实现context.Context接口 - 根据键获取上下文值
// 设计思路：
// 1. 支持OpenIM框架标准的上下文键
// 2. 提供用户ID、操作ID、连接ID等关键信息
// 3. 自动进行平台ID到平台名称的转换
//
// 参数：
//   - key: 上下文键，支持框架定义的标准键
//
// 返回值：
//   - any: 对应的上下文值，类型根据键而定
func (c *UserConnContext) Value(key any) any {
	switch key {
	case constant.OpUserID:
		// 操作用户ID
		return c.GetUserID()
	case constant.OperationID:
		// 操作ID，用于请求追踪
		return c.GetOperationID()
	case constant.ConnID:
		// 连接ID
		return c.GetConnID()
	case constant.OpUserPlatform:
		// 用户平台名称（从平台ID转换）
		return constant.PlatformIDToName(stringutil.StringToInt(c.GetPlatformID()))
	case constant.RemoteAddr:
		// 远程地址
		return c.RemoteAddr
	default:
		return ""
	}
}

// newContext 创建新的用户连接上下文
// 设计思路：
// 1. 从HTTP请求中提取基本信息
// 2. 处理代理服务器的X-Forwarded-For头部
// 3. 生成唯一的连接ID，用于调试和监控
//
// 参数：
//   - respWriter: HTTP响应写入器
//   - req: HTTP请求对象
//
// 返回值：
//   - *UserConnContext: 初始化完成的上下文对象
func newContext(respWriter http.ResponseWriter, req *http.Request) *UserConnContext {
	// 获取客户端远程地址
	remoteAddr := req.RemoteAddr
	// 处理代理服务器的真实IP头部
	if forwarded := req.Header.Get("X-Forwarded-For"); forwarded != "" {
		remoteAddr += "_" + forwarded
	}
	return &UserConnContext{
		RespWriter: respWriter,
		Req:        req,
		Path:       req.URL.Path,
		Method:     req.Method,
		RemoteAddr: remoteAddr,
		// 生成连接ID：MD5(远程地址 + 时间戳)
		ConnID: encrypt.Md5(req.RemoteAddr + "_" + strconv.Itoa(int(timeutil.GetCurrentTimestampByMill()))),
	}
}

// newTempContext 创建临时上下文对象
// 主要用于测试和内部逻辑，不依赖真实的HTTP请求
func newTempContext() *UserConnContext {
	return &UserConnContext{
		Req: &http.Request{URL: &url.URL{}},
	}
}

// GetRemoteAddr 获取客户端远程地址
// 返回包含代理信息的完整远程地址
func (c *UserConnContext) GetRemoteAddr() string {
	return c.RemoteAddr
}

// Query 从URL查询参数中获取值
// 设计思路：
// 1. 提供统一的查询参数访问接口
// 2. 返回值存在性标识，便于区分空值和不存在
//
// 参数：
//   - key: 查询参数键名
//
// 返回值：
//   - string: 参数值
//   - bool: 参数是否存在
func (c *UserConnContext) Query(key string) (string, bool) {
	var value string
	if value = c.Req.URL.Query().Get(key); value == "" {
		return value, false
	}
	return value, true
}

// GetHeader 从HTTP头部获取值
// 设计思路：
// 1. 提供统一的HTTP头部访问接口
// 2. 返回值存在性标识，便于区分空值和不存在
//
// 参数：
//   - key: HTTP头部键名
//
// 返回值：
//   - string: 头部值
//   - bool: 头部是否存在
func (c *UserConnContext) GetHeader(key string) (string, bool) {
	var value string
	if value = c.Req.Header.Get(key); value == "" {
		return value, false
	}
	return value, true
}

// SetHeader 设置HTTP响应头部
// 用于在响应中添加自定义头部信息
//
// 参数：
//   - key: 头部键名
//   - value: 头部值
func (c *UserConnContext) SetHeader(key, value string) {
	c.RespWriter.Header().Set(key, value)
}

// ErrReturn 返回HTTP错误响应
// 用于在连接建立失败时返回错误信息
//
// 参数：
//   - error: 错误信息字符串
//   - code: HTTP状态码
func (c *UserConnContext) ErrReturn(error string, code int) {
	http.Error(c.RespWriter, error, code)
}

// GetConnID 获取连接唯一标识符
// 每个WebSocket连接都有唯一的ID，便于调试和监控
func (c *UserConnContext) GetConnID() string {
	return c.ConnID
}

// GetUserID 获取用户ID
// 从URL查询参数中提取用户唯一标识符
func (c *UserConnContext) GetUserID() string {
	return c.Req.URL.Query().Get(WsUserID)
}

// GetPlatformID 获取平台ID
// 从URL查询参数中提取平台标识符
func (c *UserConnContext) GetPlatformID() string {
	return c.Req.URL.Query().Get(PlatformID)
}

// GetOperationID 获取操作ID
// 从URL查询参数中提取操作追踪标识符
func (c *UserConnContext) GetOperationID() string {
	return c.Req.URL.Query().Get(OperationID)
}

// SetOperationID 设置操作ID
// 用于动态修改操作追踪标识符
//
// 参数：
//   - operationID: 新的操作ID
func (c *UserConnContext) SetOperationID(operationID string) {
	values := c.Req.URL.Query()
	values.Set(OperationID, operationID)
	c.Req.URL.RawQuery = values.Encode()
}

// GetToken 获取认证令牌
// 从URL查询参数中提取JWT认证令牌
func (c *UserConnContext) GetToken() string {
	return c.Req.URL.Query().Get(Token)
}

// GetCompression 获取压缩配置
// 设计思路：
// 1. 优先检查URL查询参数中的压缩配置
// 2. 如果查询参数中没有，则检查HTTP头部
// 3. 支持gzip压缩协议
//
// 返回值：
//   - bool: 是否启用压缩
func (c *UserConnContext) GetCompression() bool {
	// 首先检查查询参数
	compression, exists := c.Query(Compression)
	if exists && compression == GzipCompressionProtocol {
		return true
	} else {
		// 再检查HTTP头部
		compression, exists := c.GetHeader(Compression)
		if exists && compression == GzipCompressionProtocol {
			return true
		}
	}
	return false
}

// GetSDKType 获取SDK类型
// 设计思路：
// 1. 从URL查询参数中获取SDK类型
// 2. 如果未指定，默认使用Go SDK
//
// 返回值：
//   - string: SDK类型标识（go/js）
func (c *UserConnContext) GetSDKType() string {
	sdkType := c.Req.URL.Query().Get(SDKType)
	if sdkType == "" {
		sdkType = GoSDK // 默认使用Go SDK
	}
	return sdkType
}

// ShouldSendResp 判断是否需要发送响应
// 设计思路：
// 1. 从查询参数中获取响应控制标识
// 2. 进行布尔值解析，错误时返回false
//
// 返回值：
//   - bool: 是否需要发送响应
func (c *UserConnContext) ShouldSendResp() bool {
	errResp, exists := c.Query(SendResponse)
	if exists {
		b, err := strconv.ParseBool(errResp)
		if err != nil {
			return false
		} else {
			return b
		}
	}
	return false
}

// SetToken 设置认证令牌
// 用于动态修改认证令牌（主要用于测试）
//
// 参数：
//   - token: 新的认证令牌
func (c *UserConnContext) SetToken(token string) {
	c.Req.URL.RawQuery = Token + "=" + token
}

// GetBackground 获取后台状态
// 设计思路：
// 1. 从URL查询参数中获取后台状态标识
// 2. 进行布尔值解析，错误时返回false
//
// 返回值：
//   - bool: 是否为后台模式
func (c *UserConnContext) GetBackground() bool {
	b, err := strconv.ParseBool(c.Req.URL.Query().Get(BackgroundStatus))
	if err != nil {
		return false
	}
	return b
}

// ParseEssentialArgs 解析和验证必需的连接参数
// 设计思路：
// 1. 验证所有必需的连接参数是否存在
// 2. 进行参数格式验证（如平台ID必须为整数）
// 3. 验证SDK类型的合法性
// 4. 确保连接参数的完整性和正确性
//
// 验证项目：
// - Token: 认证令牌必须存在
// - SendID: 用户ID必须存在
// - PlatformID: 平台ID必须存在且为有效整数
// - SDKType: SDK类型必须为空、go或js之一
//
// 返回值：
//   - error: 验证失败时返回具体错误信息
func (c *UserConnContext) ParseEssentialArgs() error {
	// 验证Token参数
	_, exists := c.Query(Token)
	if !exists {
		return servererrs.ErrConnArgsErr.WrapMsg("token is empty")
	}

	// 验证用户ID参数
	_, exists = c.Query(WsUserID)
	if !exists {
		return servererrs.ErrConnArgsErr.WrapMsg("sendID is empty")
	}

	// 验证平台ID参数
	platformIDStr, exists := c.Query(PlatformID)
	if !exists {
		return servererrs.ErrConnArgsErr.WrapMsg("platformID is empty")
	}
	// 验证平台ID格式（必须为整数）
	_, err := strconv.Atoi(platformIDStr)
	if err != nil {
		return servererrs.ErrConnArgsErr.WrapMsg("platformID is not int")
	}

	// 验证SDK类型参数
	switch sdkType, _ := c.Query(SDKType); sdkType {
	case "", GoSDK, JsSDK:
		// 合法的SDK类型：空（使用默认）、go、js
	default:
		return servererrs.ErrConnArgsErr.WrapMsg("sdkType is not go or js")
	}
	return nil
}
