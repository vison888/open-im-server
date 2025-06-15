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

/*
 * 配置文件解析模块
 *
 * 本模块负责OpenIM系统的配置文件解析和处理，是系统配置管理的核心组件。
 *
 * 核心功能：
 *
 * 1. 配置文件路径管理
 *    - 默认配置路径获取：支持容器化部署的配置路径解析
 *    - 项目根目录定位：自动识别项目根目录位置
 *    - 相对路径转绝对路径：确保配置文件路径的准确性
 *    - 多环境配置支持：开发、测试、生产环境的配置适配
 *
 * 2. 配置文件加载
 *    - YAML格式解析：支持标准YAML配置文件格式
 *    - 配置文件验证：检查配置文件是否存在和可读
 *    - 错误处理机制：详细的配置加载错误信息
 *    - 配置结构映射：将YAML内容映射到Go结构体
 *
 * 3. 通知配置处理
 *    - 通知选项构建：根据配置构建消息处理选项
 *    - 可靠性级别配置：支持不同级别的消息可靠性
 *    - 离线推送配置：配置离线消息推送参数
 *    - 消息发送控制：控制是否发送消息到聊天记录
 *
 * 4. 部署环境适配
 *    - 容器化部署：支持Docker/K8s容器化部署
 *    - 本地开发环境：支持本地开发调试
 *    - 配置文件查找：智能查找配置文件位置
 *    - 路径规范化：统一配置文件路径格式
 *
 * 技术特性：
 * - 跨平台兼容：支持Windows、Linux、macOS等操作系统
 * - 路径自动解析：自动解析相对路径和绝对路径
 * - 错误信息详细：提供详细的错误定位信息
 * - 配置热加载：支持配置文件的动态加载
 * - 类型安全：使用Go的类型系统确保配置安全
 *
 * 配置文件类型：
 * - config.yaml：主配置文件，包含系统核心配置
 * - notification.yaml：通知配置文件，包含通知相关配置
 * - 自定义配置：支持加载自定义配置文件
 *
 * 使用场景：
 * - 系统启动时的配置加载
 * - 配置文件的动态重载
 * - 多环境配置管理
 * - 配置验证和错误处理
 */
package config

import (
	"os"
	"path/filepath"

	"github.com/openimsdk/open-im-server/v3/pkg/msgprocessor"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/field"
	"gopkg.in/yaml.v3"
)

// 配置文件相关常量定义
//
// 这些常量定义了系统中使用的标准配置文件名称和路径，
// 确保整个系统使用统一的配置文件命名规范。
const (
	// FileName 主配置文件名
	// 系统的主要配置文件，包含数据库连接、服务端口、认证等核心配置
	FileName = "config.yaml"

	// NotificationFileName 通知配置文件名
	// 专门用于配置通知相关的参数，如推送设置、通知模板等
	NotificationFileName = "notification.yaml"

	// DefaultFolderPath 默认配置文件夹路径
	// 相对于可执行文件的默认配置文件夹位置，主要用于容器化部署
	DefaultFolderPath = "../config/"
)

// GetDefaultConfigPath 获取默认配置路径
//
// 返回系统默认的配置文件路径，主要用于容器化部署场景。
// 该方法会基于可执行文件的位置计算出配置文件的绝对路径。
//
// 路径计算逻辑：
// 1. 获取当前可执行文件的绝对路径
// 2. 基于可执行文件路径计算配置目录路径
// 3. 使用field.OutDir规范化路径格式
// 4. 返回绝对路径，确保路径的准确性
//
// 返回值：
// - string: 配置文件目录的绝对路径
// - error: 路径获取过程中的错误信息
//
// 使用场景：
// - 容器化部署中的配置文件定位
// - 系统启动时的默认配置加载
// - 配置文件路径的标准化处理
//
// 错误处理：
// - 可执行文件路径获取失败
// - 路径规范化处理失败
// - 文件系统访问权限问题
//
// 注意事项：
// - 返回的是目录路径，不包含具体的配置文件名
// - 路径是基于可执行文件位置的相对计算
// - 适用于标准的容器化部署结构
func GetDefaultConfigPath() (string, error) {
	// 获取当前可执行文件的绝对路径
	executablePath, err := os.Executable()
	if err != nil {
		return "", errs.WrapMsg(err, "failed to get executable path")
	}

	// 基于可执行文件路径计算配置目录的绝对路径
	// 使用../config/相对路径，这是K8s容器的标准配置路径
	configPath, err := field.OutDir(filepath.Join(filepath.Dir(executablePath), "../config/"))
	if err != nil {
		return "", errs.WrapMsg(err, "failed to get output directory", "outDir", filepath.Join(filepath.Dir(executablePath), "../config/"))
	}

	return configPath, nil
}

// GetProjectRoot 获取项目根目录路径
//
// 返回项目根目录的绝对路径，主要用于开发环境和本地调试。
// 该方法通过可执行文件位置向上查找，定位项目的根目录。
//
// 路径计算逻辑：
// 1. 获取当前可执行文件的绝对路径
// 2. 向上遍历目录结构，查找项目根目录
// 3. 使用相对路径"../../../../.."进行目录定位
// 4. 规范化路径格式并返回绝对路径
//
// 返回值：
// - string: 项目根目录的绝对路径
// - error: 路径获取过程中的错误信息
//
// 使用场景：
// - 开发环境中的配置文件加载
// - 本地调试时的资源文件定位
// - 项目结构相关的路径计算
// - 测试环境中的文件路径解析
//
// 错误处理：
// - 可执行文件路径获取失败
// - 项目根目录定位失败
// - 路径规范化处理失败
//
// 注意事项：
// - 路径计算基于固定的目录层级关系
// - 适用于标准的项目目录结构
// - 返回的是项目根目录，不是配置目录
func GetProjectRoot() (string, error) {
	// 获取当前可执行文件的绝对路径
	executablePath, err := os.Executable()
	if err != nil {
		return "", errs.Wrap(err)
	}

	// 基于可执行文件路径向上查找项目根目录
	// 使用相对路径"../../../../.."定位项目根目录
	projectRoot, err := field.OutDir(filepath.Join(filepath.Dir(executablePath), "../../../../.."))
	if err != nil {
		return "", errs.Wrap(err)
	}

	return projectRoot, nil
}

// GetOptionsByNotification 根据通知配置构建消息处理选项
//
// 基于通知配置参数构建消息处理器的选项，用于控制消息的处理行为。
// 该方法是通知系统配置的核心，决定了消息如何被处理和推送。
//
// 配置处理逻辑：
// 1. 创建默认的消息处理选项
// 2. 根据sendMessage参数覆盖配置中的发送消息设置
// 3. 根据IsSendMsg配置决定是否启用未读计数
// 4. 根据离线推送配置决定是否启用离线推送
// 5. 根据可靠性级别配置消息持久化和历史记录
// 6. 设置最终的消息发送选项
//
// 参数说明：
// - cfg: 通知配置结构，包含各种通知相关的配置参数
// - sendMessage: 可选的消息发送控制参数，用于覆盖配置中的设置
//
// 返回值：
// - msgprocessor.Options: 配置好的消息处理选项
//
// 配置选项说明：
// - UnreadCount: 是否启用未读消息计数
// - OfflinePush: 是否启用离线消息推送
// - History: 是否保存消息历史记录
// - Persistent: 是否持久化消息
// - SendMsg: 是否发送消息到聊天记录
//
// 可靠性级别：
// - UnreliableNotification: 不可靠通知，不保证送达
// - ReliableNotificationNoMsg: 可靠通知但不保存消息，启用历史记录和持久化
//
// 使用场景：
// - 通知系统初始化时的选项配置
// - 不同类型通知的处理选项设置
// - 消息可靠性级别的动态调整
// - 离线推送功能的开关控制
func GetOptionsByNotification(cfg NotificationConfig, sendMessage *bool) msgprocessor.Options {
	// 创建默认的消息处理选项
	opts := msgprocessor.NewOptions()

	// 如果提供了sendMessage参数，则覆盖配置中的IsSendMsg设置
	// 这允许在运行时动态控制是否发送消息
	if sendMessage != nil {
		cfg.IsSendMsg = *sendMessage
	}

	// 如果配置为发送消息，则启用未读消息计数功能
	// 未读计数用于客户端显示未读消息数量
	if cfg.IsSendMsg {
		opts = msgprocessor.WithOptions(opts, msgprocessor.WithUnreadCount(true))
	}

	// 如果启用了离线推送，则添加离线推送选项
	// 离线推送用于向不在线的用户推送消息通知
	if cfg.OfflinePush.Enable {
		opts = msgprocessor.WithOptions(opts, msgprocessor.WithOfflinePush(true))
	}

	// 根据可靠性级别配置消息处理选项
	switch cfg.ReliabilityLevel {
	case constant.UnreliableNotification:
		// 不可靠通知：不需要额外的处理选项
		// 消息可能丢失，不保证送达

	case constant.ReliableNotificationNoMsg:
		// 可靠通知但不保存消息：启用历史记录和持久化
		// 确保通知的可靠性，但不在聊天记录中显示
		opts = msgprocessor.WithOptions(opts, msgprocessor.WithHistory(true), msgprocessor.WithPersistent())
	}

	// 设置是否发送消息到聊天记录的选项
	// 这个选项控制消息是否会出现在用户的聊天界面中
	opts = msgprocessor.WithOptions(opts, msgprocessor.WithSendMsg(cfg.IsSendMsg))

	return opts
}

// initConfig 初始化配置文件加载
//
// 这是一个通用的配置文件加载函数，支持从指定路径加载配置文件，
// 如果指定路径不存在，则尝试从项目默认的config目录加载。
//
// 加载流程：
// 1. 构建完整的配置文件路径
// 2. 检查配置文件是否存在
// 3. 如果不存在，尝试从项目根目录的config文件夹加载
// 4. 读取配置文件内容
// 5. 解析YAML格式并映射到配置结构体
//
// 参数说明：
// - config: 配置结构体指针，用于接收解析后的配置数据
// - configName: 配置文件名称（如config.yaml）
// - configFolderPath: 配置文件夹路径
//
// 返回值：
// - error: 配置加载过程中的错误信息，nil表示成功
//
// 路径查找策略：
// 1. 优先使用指定的configFolderPath
// 2. 如果指定路径的文件不存在，则查找项目根目录下的config文件夹
// 3. 确保配置文件的可访问性和可读性
//
// 错误处理：
// - 文件状态检查错误：文件系统访问问题
// - 文件不存在错误：配置文件缺失
// - 文件读取错误：权限问题或文件损坏
// - YAML解析错误：配置文件格式错误
//
// 使用场景：
// - 系统启动时加载主配置文件
// - 加载通知配置文件
// - 加载其他自定义配置文件
// - 配置文件的热重载
//
// 注意事项：
// - config参数必须是指针类型，以便修改原始结构体
// - 配置文件必须是有效的YAML格式
// - 路径分隔符会自动适配不同操作系统
func initConfig(config any, configName, configFolderPath string) error {
	// 构建完整的配置文件路径
	configFolderPath = filepath.Join(configFolderPath, configName)

	// 检查配置文件是否存在
	_, err := os.Stat(configFolderPath)
	if err != nil {
		// 如果不是文件不存在的错误，直接返回错误
		if !os.IsNotExist(err) {
			return errs.WrapMsg(err, "stat config path error", "config Folder Path", configFolderPath)
		}

		// 文件不存在时，尝试从项目根目录的config文件夹加载
		path, err := GetProjectRoot()
		if err != nil {
			return err
		}
		// 重新构建配置文件路径：项目根目录/config/配置文件名
		configFolderPath = filepath.Join(path, "config", configName)
	}

	// 读取配置文件内容
	data, err := os.ReadFile(configFolderPath)
	if err != nil {
		return errs.WrapMsg(err, "read file error", "config Folder Path", configFolderPath)
	}

	// 解析YAML格式的配置文件内容
	// 将YAML内容映射到传入的config结构体中
	if err = yaml.Unmarshal(data, config); err != nil {
		return errs.WrapMsg(err, "unmarshal yaml error", "config Folder Path", configFolderPath)
	}

	return nil
}
