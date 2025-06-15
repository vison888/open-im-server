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
 * 配置常量定义模块
 *
 * 本模块定义了OpenIM系统中使用的各种配置常量，包括环境变量名称、
 * 部署类型标识符和文件权限设置等。这些常量确保了系统配置的一致性和安全性。
 *
 * 常量分类：
 *
 * 1. 环境变量常量
 *    - 配置文件路径：用于指定配置文件的挂载路径
 *    - 部署类型：标识当前的部署环境类型
 *    - 服务发现：指定使用的服务发现机制
 *
 * 2. 文件权限常量
 *    - 目录权限：不同用途目录的权限设置
 *    - 文件权限：不同类型文件的权限设置
 *    - 安全考虑：基于最小权限原则的权限分配
 *
 * 权限设计原则：
 * - 最小权限：只授予必要的权限
 * - 分类管理：不同类型的文件使用不同权限
 * - 安全优先：敏感文件使用严格的权限控制
 * - 兼容性：确保跨平台的权限兼容性
 *
 * 使用场景：
 * - 容器化部署中的环境变量配置
 * - 文件系统操作中的权限设置
 * - 服务发现机制的选择
 * - 安全策略的实施
 */
package config

// 环境变量和部署相关常量
//
// 这些常量定义了系统运行时需要的环境变量名称和部署类型标识符，
// 用于在不同的部署环境中进行配置和服务发现。
const (
	// MountConfigFilePath 配置文件路径环境变量名
	// 用于在容器化部署中指定配置文件的挂载路径
	//
	// 使用场景：
	// - Docker容器中通过环境变量指定配置文件位置
	// - Kubernetes部署中通过ConfigMap挂载配置
	// - 多环境部署中的配置路径动态指定
	//
	// 示例：
	//   export CONFIG_PATH=/app/config
	//   docker run -e CONFIG_PATH=/app/config openim:latest
	MountConfigFilePath = "CONFIG_PATH"

	// DeploymentType 部署类型环境变量名
	// 用于标识当前系统的部署类型，影响配置加载和服务发现策略
	//
	// 支持的部署类型：
	// - kubernetes: Kubernetes集群部署
	// - docker: Docker容器部署
	// - standalone: 独立部署
	//
	// 使用场景：
	// - 根据部署类型选择不同的配置策略
	// - 服务发现机制的自动选择
	// - 监控和日志收集策略的调整
	//
	// 示例：
	//   export DEPLOYMENT_TYPE=kubernetes
	DeploymentType = "DEPLOYMENT_TYPE"

	// KUBERNETES Kubernetes部署类型标识符
	// 当DeploymentType设置为此值时，系统将使用Kubernetes原生的服务发现机制
	//
	// 特性：
	// - 使用Kubernetes Service进行服务发现
	// - 支持Kubernetes的健康检查机制
	// - 集成Kubernetes的配置管理（ConfigMap/Secret）
	// - 支持Kubernetes的负载均衡和故障转移
	KUBERNETES = "kubernetes"

	// ETCD 服务发现类型标识符
	// 指定使用ETCD作为服务注册和发现的后端存储
	//
	// 特性：
	// - 分布式键值存储
	// - 强一致性保证
	// - 支持服务健康检查
	// - 提供服务变更通知机制
	//
	// 使用场景：
	// - 微服务架构中的服务注册与发现
	// - 配置中心的实现
	// - 分布式锁的实现
	ETCD = "etcd"
)

// 文件和目录权限常量
//
// 这些常量定义了系统中不同类型文件和目录的权限设置，
// 基于最小权限原则和安全最佳实践进行设计。
//
// 权限表示法：
// - 使用八进制数表示Unix/Linux文件权限
// - 格式：0xyz，其中x=所有者权限，y=组权限，z=其他用户权限
// - 权限位：4=读(r)，2=写(w)，1=执行(x)
const (
	// DefaultDirPerm 默认目录权限 (0755)
	// 用于创建一般用途的目录，提供标准的目录访问权限
	//
	// 权限分解：
	// - 所有者(7): 读(4) + 写(2) + 执行(1) = 完全控制
	// - 组用户(5): 读(4) + 执行(1) = 读取和进入目录
	// - 其他用户(5): 读(4) + 执行(1) = 读取和进入目录
	//
	// 使用场景：
	// - 日志目录
	// - 临时文件目录
	// - 一般数据目录
	// - 公共资源目录
	DefaultDirPerm = 0755

	// PrivateFilePerm 私有文件权限 (0600)
	// 用于敏感文件，只允许文件所有者读写，其他用户无任何权限
	//
	// 权限分解：
	// - 所有者(6): 读(4) + 写(2) = 读写权限
	// - 组用户(0): 无权限
	// - 其他用户(0): 无权限
	//
	// 使用场景：
	// - 密钥文件（私钥、证书）
	// - 密码文件
	// - 敏感配置文件
	// - 用户凭证文件
	PrivateFilePerm = 0600

	// ExecFilePerm 可执行文件权限 (0754)
	// 用于可执行文件，所有者有完全权限，组和其他用户只能读取
	//
	// 权限分解：
	// - 所有者(7): 读(4) + 写(2) + 执行(1) = 完全控制
	// - 组用户(5): 读(4) + 执行(1) = 读取和执行
	// - 其他用户(4): 读(4) = 只读权限
	//
	// 使用场景：
	// - 应用程序二进制文件
	// - 脚本文件
	// - 工具程序
	// - 启动脚本
	ExecFilePerm = 0754

	// SharedDirPerm 共享目录权限 (0770)
	// 用于需要组内共享的目录，所有者和组用户有完全权限，其他用户无权限
	//
	// 权限分解：
	// - 所有者(7): 读(4) + 写(2) + 执行(1) = 完全控制
	// - 组用户(7): 读(4) + 写(2) + 执行(1) = 完全控制
	// - 其他用户(0): 无权限
	//
	// 使用场景：
	// - 团队协作目录
	// - 共享数据目录
	// - 组内配置目录
	// - 协作工作空间
	SharedDirPerm = 0770

	// ReadOnlyDirPerm 只读目录权限 (0555)
	// 用于只读目录，所有用户只能读取和进入，不能修改
	//
	// 权限分解：
	// - 所有者(5): 读(4) + 执行(1) = 读取和进入
	// - 组用户(5): 读(4) + 执行(1) = 读取和进入
	// - 其他用户(5): 读(4) + 执行(1) = 读取和进入
	//
	// 使用场景：
	// - 静态资源目录
	// - 只读配置目录
	// - 文档目录
	// - 归档目录
	ReadOnlyDirPerm = 0555
)
