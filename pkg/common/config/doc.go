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

/*
Package config 提供OpenIM系统的配置管理功能

本包是OpenIM即时通讯系统的核心配置管理模块，负责处理系统中所有配置相关的功能，
包括配置文件的加载、解析、环境变量处理和配置结构定义等。

# 核心功能

## 1. 配置文件管理
- 支持多种配置文件格式（YAML、JSON、TOML等）
- 提供统一的配置文件加载接口
- 支持配置文件的热重载和动态更新
- 配置文件路径的自动发现和解析

## 2. 环境变量支持
- 自动环境变量映射和覆盖
- 统一的环境变量命名规范
- 支持多环境配置（开发、测试、生产）
- 容器化部署的环境变量注入

## 3. 配置结构定义
- 完整的配置结构体定义
- 支持嵌套配置和复杂数据类型
- 配置验证和类型转换
- 默认值设置和配置继承

## 4. 服务配置管理
- 微服务配置的统一管理
- RPC服务配置
- 中间件配置（Redis、MongoDB、Kafka等）
- 第三方服务集成配置

# 主要组件

## 配置加载器 (load_config.go)
提供基于Viper的配置文件加载功能：
  - LoadConfig: 通用配置加载函数
  - 支持环境变量覆盖
  - 自动类型转换和结构体映射

## 配置结构 (config.go)
定义系统中所有的配置结构体：
  - 服务配置：API、RPC、Gateway等
  - 中间件配置：数据库、缓存、消息队列
  - 业务配置：通知、Webhook、推送等

## 环境变量管理 (env.go)
管理环境变量前缀和映射：
  - EnvPrefixMap: 配置文件到环境变量前缀的映射
  - 统一的环境变量命名规范
  - 命令行参数定义

## 配置解析 (parse.go)
提供配置文件路径解析和选项构建：
  - GetDefaultConfigPath: 获取默认配置路径
  - GetProjectRoot: 项目根目录定位
  - GetOptionsByNotification: 通知配置选项构建

## 常量定义 (constant.go)
定义系统常量和权限设置：
  - 环境变量名称常量
  - 部署类型标识符
  - 文件和目录权限常量

# 使用示例

## 基本配置加载

	import "github.com/openimsdk/open-im-server/v3/pkg/common/config"

	// 定义配置结构
	type MyConfig struct {
	    Database struct {
	        Host string `mapstructure:"host"`
	        Port int    `mapstructure:"port"`
	    } `mapstructure:"database"`
	}

	// 加载配置
	var cfg MyConfig
	err := config.LoadConfig("/path/to/config.yaml", "MYAPP", &cfg)

## 环境变量覆盖

	# 配置文件中的值可以通过环境变量覆盖
	export MYAPP_DATABASE_HOST=localhost
	export MYAPP_DATABASE_PORT=3306

## 获取配置路径

	// 获取默认配置路径
	configPath := config.GetDefaultConfigPath()

	// 获取项目根目录
	projectRoot := config.GetProjectRoot()

# 配置文件类型

系统支持以下配置文件：

## 核心配置
- config.yaml: 主配置文件
- notification.yaml: 通知配置
- share.yml: 共享配置
- webhooks.yml: Webhook配置

## 中间件配置
- kafka.yml: Kafka消息队列配置
- redis.yml: Redis缓存配置
- mongodb.yml: MongoDB数据库配置
- minio.yml: MinIO对象存储配置

## 服务配置
- openim-api.yml: API网关配置
- openim-msggateway.yml: 消息网关配置
- openim-push.yml: 推送服务配置
- openim-rpc-*.yml: 各种RPC服务配置

# 部署支持

## 容器化部署
- Docker容器配置注入
- Kubernetes ConfigMap集成
- 环境变量动态配置

## 服务发现
- ETCD服务注册与发现
- Kubernetes原生服务发现
- 多种部署模式支持

# 安全特性

## 权限管理
- 基于最小权限原则的文件权限设置
- 敏感文件的严格权限控制
- 不同类型文件的分类权限管理

## 配置安全
- 敏感配置的加密存储
- 配置文件的访问控制
- 环境变量的安全注入

# 扩展性

## 自定义配置
- 支持自定义配置结构
- 灵活的配置继承机制
- 插件化的配置扩展

## 多环境支持
- 开发、测试、生产环境配置
- 环境特定的配置覆盖
- 配置模板和变量替换

本包为OpenIM系统提供了完整、安全、灵活的配置管理解决方案，
支持各种部署场景和配置需求。
*/
package config // import "github.com/openimsdk/open-im-server/v3/pkg/common/config"
