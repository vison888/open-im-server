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
 * OpenIM配置结构定义模块
 *
 * 本模块定义了OpenIM即时通讯系统中所有服务和组件的配置结构体。
 * 这些结构体通过mapstructure标签与YAML配置文件进行映射，支持环境变量覆盖。
 *
 * 配置分类：
 *
 * 1. 缓存配置 (CacheConfig, LocalCache)
 *    - 本地缓存的配置参数
 *    - 缓存槽位、过期时间等设置
 *
 * 2. 基础设施配置
 *    - 日志配置 (Log)
 *    - 数据库配置 (Mongo, Redis)
 *    - 消息队列配置 (Kafka)
 *    - 对象存储配置 (Minio, Cos, Oss, Kodo, Aws)
 *
 * 3. 服务配置
 *    - API服务 (API)
 *    - 消息网关 (MsgGateway)
 *    - 消息传输 (MsgTransfer)
 *    - 推送服务 (Push)
 *    - 认证服务 (Auth)
 *    - 各种RPC服务 (User, Friend, Group, Msg, Third, Conversation)
 *
 * 4. 业务配置
 *    - 通知配置 (Notification, NotificationConfig)
 *    - Webhook配置 (Webhooks)
 *    - 定时任务配置 (CronTask)
 *    - 共享配置 (Share)
 *
 * 5. 部署配置
 *    - 服务发现配置 (Discovery, Etcd, ZooKeeper)
 *    - 监控配置 (Prometheus)
 *    - TLS配置 (TLSConfig)
 *
 * 设计特点：
 * - 使用mapstructure标签进行YAML映射
 * - 支持嵌套配置结构
 * - 提供配置构建方法
 * - 支持配置验证和默认值
 * - 兼容环境变量覆盖
 */
package config

import (
	"strings"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/kafka"
	"github.com/openimsdk/tools/db/mongoutil"
	"github.com/openimsdk/tools/db/redisutil"
	"github.com/openimsdk/tools/s3/aws"
	"github.com/openimsdk/tools/s3/cos"
	"github.com/openimsdk/tools/s3/kodo"
	"github.com/openimsdk/tools/s3/minio"
	"github.com/openimsdk/tools/s3/oss"
)

// CacheConfig 缓存配置结构
//
// 定义本地缓存的基本配置参数，包括缓存主题、槽位设置和过期时间等。
// 用于配置系统中各种类型数据的本地缓存策略。
//
// 配置说明：
// - Topic: 缓存主题名称，用于区分不同类型的缓存数据
// - SlotNum: 缓存槽位数量，影响缓存的并发性能和内存分布
// - SlotSize: 每个槽位的大小，决定单个槽位能存储的数据量
// - SuccessExpire: 成功缓存的过期时间（秒）
// - FailedExpire: 失败缓存的过期时间（秒）
//
// 使用场景：
// - 用户信息缓存
// - 群组信息缓存
// - 好友关系缓存
// - 会话信息缓存
type CacheConfig struct {
	Topic         string `mapstructure:"topic"`         // 缓存主题名称
	SlotNum       int    `mapstructure:"slotNum"`       // 缓存槽位数量
	SlotSize      int    `mapstructure:"slotSize"`      // 每个槽位大小
	SuccessExpire int    `mapstructure:"successExpire"` // 成功缓存过期时间（秒）
	FailedExpire  int    `mapstructure:"failedExpire"`  // 失败缓存过期时间（秒）
}

// LocalCache 本地缓存配置
//
// 定义系统中各种业务数据的本地缓存配置，通过分类管理不同类型的缓存策略。
// 每种数据类型都可以有独立的缓存参数设置。
//
// 缓存分类：
// - User: 用户相关数据缓存（用户信息、在线状态等）
// - Group: 群组相关数据缓存（群组信息、成员列表等）
// - Friend: 好友关系数据缓存（好友列表、好友信息等）
// - Conversation: 会话数据缓存（会话列表、会话设置等）
//
// 性能优化：
// - 减少数据库查询频率
// - 提高数据访问速度
// - 降低网络延迟
// - 提升用户体验
type LocalCache struct {
	User         CacheConfig `mapstructure:"user"`         // 用户缓存配置
	Group        CacheConfig `mapstructure:"group"`        // 群组缓存配置
	Friend       CacheConfig `mapstructure:"friend"`       // 好友缓存配置
	Conversation CacheConfig `mapstructure:"conversation"` // 会话缓存配置
}

// Log 日志配置结构
//
// 定义系统日志的输出、轮转、级别等配置参数。
// 支持多种日志输出格式和存储策略。
//
// 配置参数：
// - StorageLocation: 日志文件存储位置
// - RotationTime: 日志轮转时间间隔（小时）
// - RemainRotationCount: 保留的日志轮转文件数量
// - RemainLogLevel: 日志级别（1=debug, 2=info, 3=warn, 4=error）
// - IsStdout: 是否输出到标准输出
// - IsJson: 是否使用JSON格式输出
// - IsSimplify: 是否使用简化格式
// - WithStack: 是否包含堆栈信息
//
// 日志级别说明：
// - 1 (Debug): 调试信息，包含详细的程序执行信息
// - 2 (Info): 一般信息，记录程序正常运行状态
// - 3 (Warn): 警告信息，记录潜在问题
// - 4 (Error): 错误信息，记录程序异常和错误
type Log struct {
	StorageLocation     string `mapstructure:"storageLocation"`     // 日志存储位置
	RotationTime        uint   `mapstructure:"rotationTime"`        // 日志轮转时间（小时）
	RemainRotationCount uint   `mapstructure:"remainRotationCount"` // 保留轮转文件数量
	RemainLogLevel      int    `mapstructure:"remainLogLevel"`      // 日志级别
	IsStdout            bool   `mapstructure:"isStdout"`            // 是否输出到标准输出
	IsJson              bool   `mapstructure:"isJson"`              // 是否使用JSON格式
	IsSimplify          bool   `mapstructure:"isSimplify"`          // 是否使用简化格式
	WithStack           bool   `mapstructure:"withStack"`           // 是否包含堆栈信息
}

// Minio 对象存储配置
//
// 定义MinIO对象存储服务的连接和访问配置。
// MinIO是一个高性能的分布式对象存储服务，兼容Amazon S3 API。
//
// 配置参数：
// - Bucket: 存储桶名称
// - AccessKeyID: 访问密钥ID
// - SecretAccessKey: 访问密钥
// - SessionToken: 会话令牌（可选）
// - InternalAddress: 内部访问地址（服务间通信）
// - ExternalAddress: 外部访问地址（客户端访问）
// - PublicRead: 是否允许公共读取
//
// 地址配置说明：
// - InternalAddress: 用于服务器内部访问，通常是内网地址
// - ExternalAddress: 用于客户端访问，通常是公网地址或域名
//
// 安全考虑：
// - AccessKeyID和SecretAccessKey应该保密
// - 建议使用HTTPS协议
// - 合理设置PublicRead权限
type Minio struct {
	Bucket          string `mapstructure:"bucket"`          // 存储桶名称
	AccessKeyID     string `mapstructure:"accessKeyID"`     // 访问密钥ID
	SecretAccessKey string `mapstructure:"secretAccessKey"` // 访问密钥
	SessionToken    string `mapstructure:"sessionToken"`    // 会话令牌
	InternalAddress string `mapstructure:"internalAddress"` // 内部访问地址
	ExternalAddress string `mapstructure:"externalAddress"` // 外部访问地址
	PublicRead      bool   `mapstructure:"publicRead"`      // 是否允许公共读取
}

// Mongo MongoDB数据库配置
//
// 定义MongoDB数据库的连接参数和性能设置。
// 支持单机部署和集群部署两种模式。
//
// 连接方式：
// 1. URI方式：使用完整的MongoDB连接URI
// 2. 参数方式：分别指定地址、数据库、认证等参数
//
// 配置参数：
// - URI: MongoDB连接URI（优先使用）
// - Address: MongoDB服务器地址列表（集群模式）
// - Database: 数据库名称
// - Username: 用户名
// - Password: 密码
// - AuthSource: 认证数据库
// - MaxPoolSize: 最大连接池大小
// - MaxRetry: 最大重试次数
//
// 性能优化：
// - 合理设置连接池大小
// - 配置适当的重试策略
// - 使用认证提高安全性
type Mongo struct {
	URI         string   `mapstructure:"uri"`         // MongoDB连接URI
	Address     []string `mapstructure:"address"`     // 服务器地址列表
	Database    string   `mapstructure:"database"`    // 数据库名称
	Username    string   `mapstructure:"username"`    // 用户名
	Password    string   `mapstructure:"password"`    // 密码
	AuthSource  string   `mapstructure:"authSource"`  // 认证数据库
	MaxPoolSize int      `mapstructure:"maxPoolSize"` // 最大连接池大小
	MaxRetry    int      `mapstructure:"maxRetry"`    // 最大重试次数
}

// Kafka 消息队列配置
//
// 定义Kafka消息队列的连接参数、主题配置和安全设置。
// Kafka用于系统中的异步消息处理和服务间通信。
//
// 基本配置：
// - Username: 用户名（SASL认证）
// - Password: 密码（SASL认证）
// - ProducerAck: 生产者确认模式
// - CompressType: 消息压缩类型
// - Address: Kafka集群地址列表
//
// 主题配置：
// - ToRedisTopic: 发送到Redis的消息主题
// - ToMongoTopic: 发送到MongoDB的消息主题
// - ToPushTopic: 推送消息主题
// - ToOfflinePushTopic: 离线推送消息主题
//
// 消费者组配置：
// - ToRedisGroupID: Redis消费者组ID
// - ToMongoGroupID: MongoDB消费者组ID
// - ToPushGroupID: 推送消费者组ID
// - ToOfflineGroupID: 离线推送消费者组ID
//
// 安全配置：
// - Tls: TLS/SSL配置
type Kafka struct {
	Username           string    `mapstructure:"username"`             // 用户名
	Password           string    `mapstructure:"password"`             // 密码
	ProducerAck        string    `mapstructure:"producerAck"`          // 生产者确认模式
	CompressType       string    `mapstructure:"compressType"`         // 压缩类型
	Address            []string  `mapstructure:"address"`              // 集群地址
	ToRedisTopic       string    `mapstructure:"toRedisTopic"`         // Redis主题
	ToMongoTopic       string    `mapstructure:"toMongoTopic"`         // MongoDB主题
	ToPushTopic        string    `mapstructure:"toPushTopic"`          // 推送主题
	ToOfflinePushTopic string    `mapstructure:"toOfflinePushTopic"`   // 离线推送主题
	ToRedisGroupID     string    `mapstructure:"toRedisGroupID"`       // Redis消费者组
	ToMongoGroupID     string    `mapstructure:"toMongoGroupID"`       // MongoDB消费者组
	ToPushGroupID      string    `mapstructure:"toPushGroupID"`        // 推送消费者组
	ToOfflineGroupID   string    `mapstructure:"toOfflinePushGroupID"` // 离线推送消费者组
	Tls                TLSConfig `mapstructure:"tls"`                  // TLS配置
}

// TLSConfig TLS/SSL安全配置
//
// 定义TLS/SSL连接的安全参数，用于加密网络通信。
// 支持客户端证书认证和服务器证书验证。
//
// 配置参数：
// - EnableTLS: 是否启用TLS
// - CACrt: CA证书文件路径
// - ClientCrt: 客户端证书文件路径
// - ClientKey: 客户端私钥文件路径
// - ClientKeyPwd: 客户端私钥密码
// - InsecureSkipVerify: 是否跳过证书验证（仅用于测试）
//
// 安全建议：
// - 生产环境必须启用TLS
// - 不要跳过证书验证
// - 妥善保管私钥文件
// - 定期更新证书
type TLSConfig struct {
	EnableTLS          bool   `mapstructure:"enableTLS"`          // 是否启用TLS
	CACrt              string `mapstructure:"caCrt"`              // CA证书路径
	ClientCrt          string `mapstructure:"clientCrt"`          // 客户端证书路径
	ClientKey          string `mapstructure:"clientKey"`          // 客户端私钥路径
	ClientKeyPwd       string `mapstructure:"clientKeyPwd"`       // 私钥密码
	InsecureSkipVerify bool   `mapstructure:"insecureSkipVerify"` // 跳过证书验证
}

// API API服务配置
//
// 定义OpenIM API网关服务的配置参数，包括网络监听、性能设置和监控配置。
// API服务是系统的统一入口，负责处理客户端请求和路由转发。
//
// 网络配置：
// - ListenIP: 监听IP地址
// - Ports: 监听端口列表（支持多端口）
// - CompressionLevel: HTTP响应压缩级别
//
// 监控配置：
// - Enable: 是否启用Prometheus监控
// - AutoSetPorts: 是否自动设置监控端口
// - Ports: 监控端口列表
// - GrafanaURL: Grafana仪表板URL
//
// 性能优化：
// - 合理设置压缩级别
// - 配置多端口负载均衡
// - 启用监控和指标收集
type API struct {
	Api struct {
		ListenIP         string `mapstructure:"listenIP"`         // 监听IP地址
		Ports            []int  `mapstructure:"ports"`            // 监听端口列表
		CompressionLevel int    `mapstructure:"compressionLevel"` // 压缩级别
	} `mapstructure:"api"`
	Prometheus struct {
		Enable       bool   `mapstructure:"enable"`       // 启用监控
		AutoSetPorts bool   `mapstructure:"autoSetPorts"` // 自动设置端口
		Ports        []int  `mapstructure:"ports"`        // 监控端口
		GrafanaURL   string `mapstructure:"grafanaURL"`   // Grafana URL
	} `mapstructure:"prometheus"`
}

// CronTask 定时任务配置
//
// 定义系统中定时任务的执行参数和清理策略。
// 主要用于数据清理、文件管理和系统维护等定时操作。
//
// 配置参数：
// - CronExecuteTime: Cron表达式，定义任务执行时间
// - RetainChatRecords: 保留聊天记录的天数
// - FileExpireTime: 文件过期时间（天）
// - DeleteObjectType: 需要删除的对象类型列表
//
// Cron表达式格式：
// - 秒 分 时 日 月 周
// - 例如："0 0 2 * * *" 表示每天凌晨2点执行
//
// 清理策略：
// - 定期清理过期的聊天记录
// - 删除过期的文件和媒体资源
// - 清理临时数据和缓存
type CronTask struct {
	CronExecuteTime   string   `mapstructure:"cronExecuteTime"`   // Cron执行时间
	RetainChatRecords int      `mapstructure:"retainChatRecords"` // 保留聊天记录天数
	FileExpireTime    int      `mapstructure:"fileExpireTime"`    // 文件过期时间
	DeleteObjectType  []string `mapstructure:"deleteObjectType"`  // 删除对象类型
}

// OfflinePushConfig 离线推送配置
//
// 定义离线推送消息的基本参数和内容设置。
// 当用户离线时，系统会通过第三方推送服务发送通知。
//
// 配置参数：
// - Enable: 是否启用离线推送
// - Title: 推送消息标题
// - Desc: 推送消息描述
// - Ext: 推送扩展数据（JSON格式）
//
// 推送场景：
// - 用户离线时收到新消息
// - 群组@消息通知
// - 好友申请通知
// - 系统重要通知
type OfflinePushConfig struct {
	Enable bool   `mapstructure:"enable"` // 是否启用
	Title  string `mapstructure:"title"`  // 推送标题
	Desc   string `mapstructure:"desc"`   // 推送描述
	Ext    string `mapstructure:"ext"`    // 扩展数据
}

// NotificationConfig 通知配置
//
// 定义单个通知类型的配置参数，包括发送策略、可靠性级别和离线推送设置。
// 每种通知类型都可以有独立的配置策略。
//
// 配置参数：
// - IsSendMsg: 是否发送消息通知
// - ReliabilityLevel: 可靠性级别（1-3，数字越大可靠性越高）
// - UnreadCount: 是否计入未读数
// - OfflinePush: 离线推送配置
//
// 可靠性级别说明：
// - 1: 普通可靠性，允许消息丢失
// - 2: 中等可靠性，重要消息保证送达
// - 3: 高可靠性，关键消息必须送达
//
// 使用场景：
// - 群组通知
// - 好友通知
// - 系统通知
// - 会话通知
type NotificationConfig struct {
	IsSendMsg        bool              `mapstructure:"isSendMsg"`        // 是否发送消息
	ReliabilityLevel int               `mapstructure:"reliabilityLevel"` // 可靠性级别
	UnreadCount      bool              `mapstructure:"unreadCount"`      // 是否计入未读
	OfflinePush      OfflinePushConfig `mapstructure:"offlinePush"`      // 离线推送配置
}

// Notification 通知系统配置
//
// 定义OpenIM系统中所有类型通知的配置参数。
// 每种通知类型都可以独立配置发送策略、可靠性级别和离线推送设置。
//
// 通知分类：
//
// 1. 群组相关通知
//   - 群组创建、信息修改、解散等
//   - 成员加入、退出、被踢等
//   - 群组权限和状态变更
//
// 2. 好友相关通知
//   - 好友申请、同意、拒绝
//   - 好友添加、删除
//   - 好友信息更新
//
// 3. 用户相关通知
//   - 用户信息更新
//   - 用户状态变更
//   - 黑名单操作
//
// 4. 会话相关通知
//   - 会话状态变更
//   - 会话隐私设置
//
// 配置策略：
// - 每种通知可以独立开启/关闭
// - 可设置不同的可靠性级别
// - 支持离线推送配置
// - 可控制是否计入未读数
type Notification struct {
	// 群组相关通知
	GroupCreated             NotificationConfig `mapstructure:"groupCreated"`             // 群组创建通知
	GroupInfoSet             NotificationConfig `mapstructure:"groupInfoSet"`             // 群组信息设置通知
	JoinGroupApplication     NotificationConfig `mapstructure:"joinGroupApplication"`     // 加入群组申请通知
	MemberQuit               NotificationConfig `mapstructure:"memberQuit"`               // 成员退出群组通知
	GroupApplicationAccepted NotificationConfig `mapstructure:"groupApplicationAccepted"` // 群组申请被接受通知
	GroupApplicationRejected NotificationConfig `mapstructure:"groupApplicationRejected"` // 群组申请被拒绝通知
	GroupOwnerTransferred    NotificationConfig `mapstructure:"groupOwnerTransferred"`    // 群主转让通知
	MemberKicked             NotificationConfig `mapstructure:"memberKicked"`             // 成员被踢出群组通知
	MemberInvited            NotificationConfig `mapstructure:"memberInvited"`            // 成员被邀请入群通知
	MemberEnter              NotificationConfig `mapstructure:"memberEnter"`              // 成员进入群组通知
	GroupDismissed           NotificationConfig `mapstructure:"groupDismissed"`           // 群组解散通知
	GroupMuted               NotificationConfig `mapstructure:"groupMuted"`               // 群组禁言通知
	GroupCancelMuted         NotificationConfig `mapstructure:"groupCancelMuted"`         // 群组取消禁言通知
	GroupMemberMuted         NotificationConfig `mapstructure:"groupMemberMuted"`         // 群成员禁言通知
	GroupMemberCancelMuted   NotificationConfig `mapstructure:"groupMemberCancelMuted"`   // 群成员取消禁言通知
	GroupMemberInfoSet       NotificationConfig `mapstructure:"groupMemberInfoSet"`       // 群成员信息设置通知
	GroupMemberSetToAdmin    NotificationConfig `yaml:"groupMemberSetToAdmin"`            // 群成员设为管理员通知
	GroupMemberSetToOrdinary NotificationConfig `yaml:"groupMemberSetToOrdinaryUser"`     // 群成员设为普通用户通知
	GroupInfoSetAnnouncement NotificationConfig `mapstructure:"groupInfoSetAnnouncement"` // 群公告设置通知
	GroupInfoSetName         NotificationConfig `mapstructure:"groupInfoSetName"`         // 群名称设置通知

	// 好友相关通知
	FriendApplicationAdded    NotificationConfig `mapstructure:"friendApplicationAdded"`    // 好友申请添加通知
	FriendApplicationApproved NotificationConfig `mapstructure:"friendApplicationApproved"` // 好友申请通过通知
	FriendApplicationRejected NotificationConfig `mapstructure:"friendApplicationRejected"` // 好友申请拒绝通知
	FriendAdded               NotificationConfig `mapstructure:"friendAdded"`               // 好友添加通知
	FriendDeleted             NotificationConfig `mapstructure:"friendDeleted"`             // 好友删除通知
	FriendRemarkSet           NotificationConfig `mapstructure:"friendRemarkSet"`           // 好友备注设置通知
	FriendInfoUpdated         NotificationConfig `mapstructure:"friendInfoUpdated"`         // 好友信息更新通知

	// 黑名单相关通知
	BlackAdded   NotificationConfig `mapstructure:"blackAdded"`   // 添加黑名单通知
	BlackDeleted NotificationConfig `mapstructure:"blackDeleted"` // 删除黑名单通知

	// 用户相关通知
	UserInfoUpdated   NotificationConfig `mapstructure:"userInfoUpdated"`   // 用户信息更新通知
	UserStatusChanged NotificationConfig `mapstructure:"userStatusChanged"` // 用户状态变更通知

	// 会话相关通知
	ConversationChanged    NotificationConfig `mapstructure:"conversationChanged"`    // 会话变更通知
	ConversationSetPrivate NotificationConfig `mapstructure:"conversationSetPrivate"` // 会话隐私设置通知
}

// Prometheus 监控配置
//
// 定义Prometheus监控系统的基本配置参数。
// Prometheus用于收集和存储系统的性能指标和监控数据。
//
// 配置参数：
// - Enable: 是否启用Prometheus监控
// - Ports: Prometheus指标暴露端口列表
//
// 监控指标包括：
// - 系统性能指标（CPU、内存、网络等）
// - 业务指标（消息数量、用户在线数等）
// - 服务健康状态
// - 错误率和响应时间
//
// 使用场景：
// - 系统性能监控
// - 服务健康检查
// - 告警和通知
// - 容量规划
type Prometheus struct {
	Enable bool  `mapstructure:"enable"` // 是否启用监控
	Ports  []int `mapstructure:"ports"`  // 监控端口列表
}

// MsgGateway 消息网关配置
//
// 定义OpenIM消息网关服务的配置参数。
// 消息网关是客户端连接的入口，负责维护长连接和消息路由。
//
// 主要功能：
// - 维护客户端WebSocket长连接
// - 消息路由和转发
// - 连接状态管理
// - 负载均衡和故障转移
//
// 配置组件：
// 1. RPC配置：服务注册和RPC通信设置
// 2. 监控配置：Prometheus监控设置
// 3. 长连接服务：WebSocket连接管理
//
// 性能参数：
// - 最大连接数限制
// - 消息长度限制
// - 连接超时设置
type MsgGateway struct {
	RPC struct {
		RegisterIP   string `mapstructure:"registerIP"`   // RPC注册IP地址
		AutoSetPorts bool   `mapstructure:"autoSetPorts"` // 是否自动设置端口
		Ports        []int  `mapstructure:"ports"`        // RPC服务端口列表
	} `mapstructure:"rpc"`
	Prometheus  Prometheus `mapstructure:"prometheus"` // 监控配置
	ListenIP    string     `mapstructure:"listenIP"`   // 监听IP地址
	LongConnSvr struct {
		Ports               []int `mapstructure:"ports"`               // 长连接服务端口
		WebsocketMaxConnNum int   `mapstructure:"websocketMaxConnNum"` // WebSocket最大连接数
		WebsocketMaxMsgLen  int   `mapstructure:"websocketMaxMsgLen"`  // WebSocket最大消息长度
		WebsocketTimeout    int   `mapstructure:"websocketTimeout"`    // WebSocket连接超时时间
	} `mapstructure:"longConnSvr"`
}

// MsgTransfer 消息传输配置
//
// 定义OpenIM消息传输服务的配置参数。
// 消息传输服务负责处理消息的持久化、转发和分发。
//
// 主要功能：
// - 消息持久化到数据库
// - 消息转发到目标用户
// - 离线消息处理
// - 消息状态更新
//
// 工作流程：
// 1. 从Kafka消费消息
// 2. 处理消息业务逻辑
// 3. 持久化到MongoDB
// 4. 更新Redis缓存
// 5. 推送给在线用户
//
// 监控指标：
// - 消息处理速度
// - 消息积压情况
// - 错误率统计
type MsgTransfer struct {
	Prometheus struct {
		Enable       bool  `mapstructure:"enable"`       // 是否启用监控
		AutoSetPorts bool  `mapstructure:"autoSetPorts"` // 是否自动设置端口
		Ports        []int `mapstructure:"ports"`        // 监控端口列表
	} `mapstructure:"prometheus"`
}

// Push 推送服务配置
//
// 定义OpenIM推送服务的配置参数，包括各种第三方推送平台的集成配置。
// 推送服务负责向离线用户发送消息通知。
//
// 支持的推送平台：
// - 个推(GeTui): 国内主流推送平台
// - FCM(Firebase Cloud Messaging): Google推送服务
// - JPush(极光推送): 国内推送服务
// - iOS Push: Apple推送通知服务
//
// 主要功能：
// - 离线消息推送
// - 多平台推送支持
// - 推送模板管理
// - 推送统计和监控
//
// 性能优化：
// - 并发工作者数量控制
// - 用户缓存策略
// - 推送去重和合并
type Push struct {
	RPC struct {
		RegisterIP   string `mapstructure:"registerIP"`   // RPC注册IP地址
		ListenIP     string `mapstructure:"listenIP"`     // RPC监听IP地址
		AutoSetPorts bool   `mapstructure:"autoSetPorts"` // 是否自动设置端口
		Ports        []int  `mapstructure:"ports"`        // RPC服务端口列表
	} `mapstructure:"rpc"`
	Prometheus           Prometheus `mapstructure:"prometheus"`           // 监控配置
	MaxConcurrentWorkers int        `mapstructure:"maxConcurrentWorkers"` // 最大并发工作者数量
	Enable               string     `mapstructure:"enable"`               // 启用的推送平台

	// 个推配置
	GeTui struct {
		PushUrl      string `mapstructure:"pushUrl"`      // 推送URL
		MasterSecret string `mapstructure:"masterSecret"` // 主密钥
		AppKey       string `mapstructure:"appKey"`       // 应用密钥
		Intent       string `mapstructure:"intent"`       // 意图动作
		ChannelID    string `mapstructure:"channelID"`    // 通道ID
		ChannelName  string `mapstructure:"channelName"`  // 通道名称
	} `mapstructure:"geTui"`

	// Firebase云消息配置
	FCM struct {
		FilePath string `mapstructure:"filePath"` // 服务账号密钥文件路径
		AuthURL  string `mapstructure:"authURL"`  // 认证URL
	} `mapstructure:"fcm"`

	// 极光推送配置
	JPush struct {
		AppKey       string `mapstructure:"appKey"`       // 应用密钥
		MasterSecret string `mapstructure:"masterSecret"` // 主密钥
		PushURL      string `mapstructure:"pushURL"`      // 推送URL
		PushIntent   string `mapstructure:"pushIntent"`   // 推送意图
	} `mapstructure:"jpush"`

	// iOS推送配置
	IOSPush struct {
		PushSound  string `mapstructure:"pushSound"`  // 推送声音
		BadgeCount bool   `mapstructure:"badgeCount"` // 是否显示角标
		Production bool   `mapstructure:"production"` // 是否生产环境
	} `mapstructure:"iosPush"`

	FullUserCache bool `mapstructure:"fullUserCache"` // 是否启用完整用户缓存
}

type Auth struct {
	RPC struct {
		RegisterIP   string `mapstructure:"registerIP"`
		ListenIP     string `mapstructure:"listenIP"`
		AutoSetPorts bool   `mapstructure:"autoSetPorts"`
		Ports        []int  `mapstructure:"ports"`
	} `mapstructure:"rpc"`
	Prometheus  Prometheus `mapstructure:"prometheus"`
	TokenPolicy struct {
		Expire int64 `mapstructure:"expire"`
	} `mapstructure:"tokenPolicy"`
}

type Conversation struct {
	RPC struct {
		RegisterIP   string `mapstructure:"registerIP"`
		ListenIP     string `mapstructure:"listenIP"`
		AutoSetPorts bool   `mapstructure:"autoSetPorts"`
		Ports        []int  `mapstructure:"ports"`
	} `mapstructure:"rpc"`
	Prometheus Prometheus `mapstructure:"prometheus"`
}

type Friend struct {
	RPC struct {
		RegisterIP   string `mapstructure:"registerIP"`
		ListenIP     string `mapstructure:"listenIP"`
		AutoSetPorts bool   `mapstructure:"autoSetPorts"`
		Ports        []int  `mapstructure:"ports"`
	} `mapstructure:"rpc"`
	Prometheus Prometheus `mapstructure:"prometheus"`
}

type Group struct {
	RPC struct {
		RegisterIP   string `mapstructure:"registerIP"`
		ListenIP     string `mapstructure:"listenIP"`
		AutoSetPorts bool   `mapstructure:"autoSetPorts"`
		Ports        []int  `mapstructure:"ports"`
	} `mapstructure:"rpc"`
	Prometheus                 Prometheus `mapstructure:"prometheus"`
	EnableHistoryForNewMembers bool       `mapstructure:"enableHistoryForNewMembers"`
}

type Msg struct {
	RPC struct {
		RegisterIP   string `mapstructure:"registerIP"`
		ListenIP     string `mapstructure:"listenIP"`
		AutoSetPorts bool   `mapstructure:"autoSetPorts"`
		Ports        []int  `mapstructure:"ports"`
	} `mapstructure:"rpc"`
	Prometheus   Prometheus `mapstructure:"prometheus"`
	FriendVerify bool       `mapstructure:"friendVerify"`
}

type Third struct {
	RPC struct {
		RegisterIP   string `mapstructure:"registerIP"`
		ListenIP     string `mapstructure:"listenIP"`
		AutoSetPorts bool   `mapstructure:"autoSetPorts"`
		Ports        []int  `mapstructure:"ports"`
	} `mapstructure:"rpc"`
	Prometheus Prometheus `mapstructure:"prometheus"`
	Object     struct {
		Enable string `mapstructure:"enable"`
		Cos    Cos    `mapstructure:"cos"`
		Oss    Oss    `mapstructure:"oss"`
		Kodo   Kodo   `mapstructure:"kodo"`
		Aws    Aws    `mapstructure:"aws"`
	} `mapstructure:"object"`
}
type Cos struct {
	BucketURL    string `mapstructure:"bucketURL"`
	SecretID     string `mapstructure:"secretID"`
	SecretKey    string `mapstructure:"secretKey"`
	SessionToken string `mapstructure:"sessionToken"`
	PublicRead   bool   `mapstructure:"publicRead"`
}
type Oss struct {
	Endpoint        string `mapstructure:"endpoint"`
	Bucket          string `mapstructure:"bucket"`
	BucketURL       string `mapstructure:"bucketURL"`
	AccessKeyID     string `mapstructure:"accessKeyID"`
	AccessKeySecret string `mapstructure:"accessKeySecret"`
	SessionToken    string `mapstructure:"sessionToken"`
	PublicRead      bool   `mapstructure:"publicRead"`
}

type Kodo struct {
	Endpoint        string `mapstructure:"endpoint"`
	Bucket          string `mapstructure:"bucket"`
	BucketURL       string `mapstructure:"bucketURL"`
	AccessKeyID     string `mapstructure:"accessKeyID"`
	AccessKeySecret string `mapstructure:"accessKeySecret"`
	SessionToken    string `mapstructure:"sessionToken"`
	PublicRead      bool   `mapstructure:"publicRead"`
}

type Aws struct {
	Endpoint        string `mapstructure:"endpoint"`
	Region          string `mapstructure:"region"`
	Bucket          string `mapstructure:"bucket"`
	AccessKeyID     string `mapstructure:"accessKeyID"`
	SecretAccessKey string `mapstructure:"secretAccessKey"`
	SessionToken    string `mapstructure:"sessionToken"`
}

type User struct {
	RPC struct {
		RegisterIP   string `mapstructure:"registerIP"`
		ListenIP     string `mapstructure:"listenIP"`
		AutoSetPorts bool   `mapstructure:"autoSetPorts"`
		Ports        []int  `mapstructure:"ports"`
	} `mapstructure:"rpc"`
	Prometheus Prometheus `mapstructure:"prometheus"`
}

// Redis 缓存数据库配置
//
// 定义Redis缓存数据库的连接参数和性能设置。
// Redis用于系统的高速缓存、会话存储和实时数据处理。
//
// 部署模式：
// - 单机模式：单个Redis实例
// - 集群模式：Redis Cluster集群部署
//
// 主要用途：
// - 用户会话缓存
// - 消息缓存和队列
// - 计数器和统计数据
// - 分布式锁
// - 实时数据存储
//
// 性能优化：
// - 连接池大小配置
// - 重试策略设置
// - 集群模式负载均衡
type Redis struct {
	Address     []string `mapstructure:"address"`     // Redis服务器地址列表
	Username    string   `mapstructure:"username"`    // 用户名（Redis 6.0+）
	Password    string   `mapstructure:"password"`    // 密码
	ClusterMode bool     `mapstructure:"clusterMode"` // 是否集群模式
	DB          int      `mapstructure:"storage"`     // 数据库编号（单机模式）
	MaxRetry    int      `mapstructure:"maxRetry"`    // 最大重试次数
	PoolSize    int      `mapstructure:"poolSize"`    // 连接池大小
}

type BeforeConfig struct {
	Enable         bool     `mapstructure:"enable"`
	Timeout        int      `mapstructure:"timeout"`
	FailedContinue bool     `mapstructure:"failedContinue"`
	AllowedTypes   []string `mapstructure:"allowedTypes"`
	DeniedTypes    []string `mapstructure:"deniedTypes"`
}

type AfterConfig struct {
	Enable       bool     `mapstructure:"enable"`
	Timeout      int      `mapstructure:"timeout"`
	AttentionIds []string `mapstructure:"attentionIds"`
	AllowedTypes []string `mapstructure:"allowedTypes"`
	DeniedTypes  []string `mapstructure:"deniedTypes"`
}

type Share struct {
	Secret          string          `mapstructure:"secret"`
	RpcRegisterName RpcRegisterName `mapstructure:"rpcRegisterName"`
	IMAdminUserID   []string        `mapstructure:"imAdminUserID"`
	MultiLogin      MultiLogin      `mapstructure:"multiLogin"`
	RPCMaxBodySize  MaxRequestBody  `mapstructure:"rpcMaxBodySize"`
}

type MaxRequestBody struct {
	RequestMaxBodySize  int `mapstructure:"requestMaxBodySize"`
	ResponseMaxBodySize int `mapstructure:"responseMaxBodySize"`
}

type MultiLogin struct {
	Policy       int `mapstructure:"policy"`
	MaxNumOneEnd int `mapstructure:"maxNumOneEnd"`
}

type RpcRegisterName struct {
	User           string `mapstructure:"user"`
	Friend         string `mapstructure:"friend"`
	Msg            string `mapstructure:"msg"`
	Push           string `mapstructure:"push"`
	MessageGateway string `mapstructure:"messageGateway"`
	Group          string `mapstructure:"group"`
	Auth           string `mapstructure:"auth"`
	Conversation   string `mapstructure:"conversation"`
	Third          string `mapstructure:"third"`
}

func (r *RpcRegisterName) GetServiceNames() []string {
	return []string{
		r.User,
		r.Friend,
		r.Msg,
		r.Push,
		r.MessageGateway,
		r.Group,
		r.Auth,
		r.Conversation,
		r.Third,
	}
}

// FullConfig stores all configurations for before and after events
type Webhooks struct {
	URL                      string       `mapstructure:"url"`
	BeforeSendSingleMsg      BeforeConfig `mapstructure:"beforeSendSingleMsg"`
	BeforeUpdateUserInfoEx   BeforeConfig `mapstructure:"beforeUpdateUserInfoEx"`
	AfterUpdateUserInfoEx    AfterConfig  `mapstructure:"afterUpdateUserInfoEx"`
	AfterSendSingleMsg       AfterConfig  `mapstructure:"afterSendSingleMsg"`
	BeforeSendGroupMsg       BeforeConfig `mapstructure:"beforeSendGroupMsg"`
	BeforeMsgModify          BeforeConfig `mapstructure:"beforeMsgModify"`
	AfterSendGroupMsg        AfterConfig  `mapstructure:"afterSendGroupMsg"`
	AfterUserOnline          AfterConfig  `mapstructure:"afterUserOnline"`
	AfterUserOffline         AfterConfig  `mapstructure:"afterUserOffline"`
	AfterUserKickOff         AfterConfig  `mapstructure:"afterUserKickOff"`
	BeforeOfflinePush        BeforeConfig `mapstructure:"beforeOfflinePush"`
	BeforeOnlinePush         BeforeConfig `mapstructure:"beforeOnlinePush"`
	BeforeGroupOnlinePush    BeforeConfig `mapstructure:"beforeGroupOnlinePush"`
	BeforeAddFriend          BeforeConfig `mapstructure:"beforeAddFriend"`
	BeforeUpdateUserInfo     BeforeConfig `mapstructure:"beforeUpdateUserInfo"`
	AfterUpdateUserInfo      AfterConfig  `mapstructure:"afterUpdateUserInfo"`
	BeforeCreateGroup        BeforeConfig `mapstructure:"beforeCreateGroup"`
	AfterCreateGroup         AfterConfig  `mapstructure:"afterCreateGroup"`
	BeforeMemberJoinGroup    BeforeConfig `mapstructure:"beforeMemberJoinGroup"`
	BeforeSetGroupMemberInfo BeforeConfig `mapstructure:"beforeSetGroupMemberInfo"`
	AfterSetGroupMemberInfo  AfterConfig  `mapstructure:"afterSetGroupMemberInfo"`
	AfterQuitGroup           AfterConfig  `mapstructure:"afterQuitGroup"`
	AfterKickGroupMember     AfterConfig  `mapstructure:"afterKickGroupMember"`
	AfterDismissGroup        AfterConfig  `mapstructure:"afterDismissGroup"`
	BeforeApplyJoinGroup     BeforeConfig `mapstructure:"beforeApplyJoinGroup"`
	AfterGroupMsgRead        AfterConfig  `mapstructure:"afterGroupMsgRead"`
	AfterSingleMsgRead       AfterConfig  `mapstructure:"afterSingleMsgRead"`
	BeforeUserRegister       BeforeConfig `mapstructure:"beforeUserRegister"`
	AfterUserRegister        AfterConfig  `mapstructure:"afterUserRegister"`
	AfterTransferGroupOwner  AfterConfig  `mapstructure:"afterTransferGroupOwner"`
	BeforeSetFriendRemark    BeforeConfig `mapstructure:"beforeSetFriendRemark"`
	AfterSetFriendRemark     AfterConfig  `mapstructure:"afterSetFriendRemark"`
	AfterGroupMsgRevoke      AfterConfig  `mapstructure:"afterGroupMsgRevoke"`
	AfterJoinGroup           AfterConfig  `mapstructure:"afterJoinGroup"`
	BeforeInviteUserToGroup  BeforeConfig `mapstructure:"beforeInviteUserToGroup"`
	AfterSetGroupInfo        AfterConfig  `mapstructure:"afterSetGroupInfo"`
	BeforeSetGroupInfo       BeforeConfig `mapstructure:"beforeSetGroupInfo"`
	AfterSetGroupInfoEx      AfterConfig  `mapstructure:"afterSetGroupInfoEx"`
	BeforeSetGroupInfoEx     BeforeConfig `mapstructure:"beforeSetGroupInfoEx"`
	AfterRevokeMsg           AfterConfig  `mapstructure:"afterRevokeMsg"`
	BeforeAddBlack           BeforeConfig `mapstructure:"beforeAddBlack"`
	AfterAddFriend           AfterConfig  `mapstructure:"afterAddFriend"`
	BeforeAddFriendAgree     BeforeConfig `mapstructure:"beforeAddFriendAgree"`
	AfterAddFriendAgree      AfterConfig  `mapstructure:"afterAddFriendAgree"`
	AfterDeleteFriend        AfterConfig  `mapstructure:"afterDeleteFriend"`
	BeforeImportFriends      BeforeConfig `mapstructure:"beforeImportFriends"`
	AfterImportFriends       AfterConfig  `mapstructure:"afterImportFriends"`
	AfterRemoveBlack         AfterConfig  `mapstructure:"afterRemoveBlack"`
}

type ZooKeeper struct {
	Schema   string   `mapstructure:"schema"`
	Address  []string `mapstructure:"address"`
	Username string   `mapstructure:"username"`
	Password string   `mapstructure:"password"`
}

type Discovery struct {
	Enable    string    `mapstructure:"enable"`
	Etcd      Etcd      `mapstructure:"etcd"`
	ZooKeeper ZooKeeper `mapstructure:"zooKeeper"`
}

type Etcd struct {
	RootDirectory string   `mapstructure:"rootDirectory"`
	Address       []string `mapstructure:"address"`
	Username      string   `mapstructure:"username"`
	Password      string   `mapstructure:"password"`
}

func (m *Mongo) Build() *mongoutil.Config {
	return &mongoutil.Config{
		Uri:         m.URI,
		Address:     m.Address,
		Database:    m.Database,
		Username:    m.Username,
		Password:    m.Password,
		AuthSource:  m.AuthSource,
		MaxPoolSize: m.MaxPoolSize,
		MaxRetry:    m.MaxRetry,
	}
}

func (r *Redis) Build() *redisutil.Config {
	return &redisutil.Config{
		ClusterMode: r.ClusterMode,
		Address:     r.Address,
		Username:    r.Username,
		Password:    r.Password,
		DB:          r.DB,
		MaxRetry:    r.MaxRetry,
		PoolSize:    r.PoolSize,
	}
}

func (k *Kafka) Build() *kafka.Config {
	return &kafka.Config{
		Username:     k.Username,
		Password:     k.Password,
		ProducerAck:  k.ProducerAck,
		CompressType: k.CompressType,
		Addr:         k.Address,
		TLS: kafka.TLSConfig{
			EnableTLS:          k.Tls.EnableTLS,
			CACrt:              k.Tls.CACrt,
			ClientCrt:          k.Tls.ClientCrt,
			ClientKey:          k.Tls.ClientKey,
			ClientKeyPwd:       k.Tls.ClientKeyPwd,
			InsecureSkipVerify: k.Tls.InsecureSkipVerify,
		},
	}
}

func (m *Minio) Build() *minio.Config {
	formatEndpoint := func(address string) string {
		if strings.HasPrefix(address, "http://") || strings.HasPrefix(address, "https://") {
			return address
		}
		return "http://" + address
	}
	return &minio.Config{
		Bucket:          m.Bucket,
		AccessKeyID:     m.AccessKeyID,
		SecretAccessKey: m.SecretAccessKey,
		SessionToken:    m.SessionToken,
		PublicRead:      m.PublicRead,
		Endpoint:        formatEndpoint(m.InternalAddress),
		SignEndpoint:    formatEndpoint(m.ExternalAddress),
	}
}
func (c *Cos) Build() *cos.Config {
	return &cos.Config{
		BucketURL:    c.BucketURL,
		SecretID:     c.SecretID,
		SecretKey:    c.SecretKey,
		SessionToken: c.SessionToken,
		PublicRead:   c.PublicRead,
	}
}

func (o *Oss) Build() *oss.Config {
	return &oss.Config{
		Endpoint:        o.Endpoint,
		Bucket:          o.Bucket,
		BucketURL:       o.BucketURL,
		AccessKeyID:     o.AccessKeyID,
		AccessKeySecret: o.AccessKeySecret,
		SessionToken:    o.SessionToken,
		PublicRead:      o.PublicRead,
	}
}

func (o *Kodo) Build() *kodo.Config {
	return &kodo.Config{
		Endpoint:        o.Endpoint,
		Bucket:          o.Bucket,
		BucketURL:       o.BucketURL,
		AccessKeyID:     o.AccessKeyID,
		AccessKeySecret: o.AccessKeySecret,
		SessionToken:    o.SessionToken,
		PublicRead:      o.PublicRead,
	}
}

func (o *Aws) Build() *aws.Config {
	return &aws.Config{
		Region:          o.Region,
		Bucket:          o.Bucket,
		AccessKeyID:     o.AccessKeyID,
		SecretAccessKey: o.SecretAccessKey,
		SessionToken:    o.SessionToken,
	}
}

// Failed 获取失败缓存过期时间
//
// 将配置中的失败过期时间（秒）转换为time.Duration类型。
// 用于设置缓存失败情况下的过期时间。
//
// 返回值：
// - time.Duration: 失败缓存的过期时间间隔
//
// 使用场景：
// - 缓存操作失败时的过期时间设置
// - 避免频繁重试失败的操作
// - 提供合理的错误恢复时间
func (l *CacheConfig) Failed() time.Duration {
	return time.Second * time.Duration(l.FailedExpire)
}

// Success 获取成功缓存过期时间
//
// 将配置中的成功过期时间（秒）转换为time.Duration类型。
// 用于设置缓存成功情况下的过期时间。
//
// 返回值：
// - time.Duration: 成功缓存的过期时间间隔
//
// 使用场景：
// - 正常缓存数据的过期时间设置
// - 控制缓存数据的生命周期
// - 平衡缓存命中率和数据新鲜度
func (l *CacheConfig) Success() time.Duration {
	return time.Second * time.Duration(l.SuccessExpire)
}

// Enable 检查缓存配置是否启用
//
// 通过检查关键配置参数来判断缓存是否应该启用。
// 只有当主题名称不为空且槽位配置合理时才启用缓存。
//
// 检查条件：
// - Topic不为空：确保有明确的缓存主题
// - SlotNum大于0：确保有足够的缓存槽位
// - SlotSize大于0：确保每个槽位有合理的大小
//
// 返回值：
// - bool: true表示缓存配置有效且应该启用，false表示禁用
//
// 使用场景：
// - 系统启动时的缓存初始化检查
// - 动态配置更新时的有效性验证
// - 缓存服务的开关控制
func (l *CacheConfig) Enable() bool {
	return l.Topic != "" && l.SlotNum > 0 && l.SlotSize > 0
}

// 配置文件名常量定义
//
// 定义OpenIM系统中所有配置文件的标准文件名。
// 这些常量确保整个系统使用统一的配置文件命名规范。
//
// 文件分类：
// - 基础设施配置：数据库、缓存、消息队列等
// - 服务配置：各种微服务的配置文件
// - 业务配置：通知、Webhook、共享配置等
//
// 命名规范：
// - 基础组件：直接使用组件名（如 redis.yml）
// - OpenIM服务：使用 openim- 前缀
// - RPC服务：使用 openim-rpc- 前缀
var (
	// 基础设施配置文件
	DiscoveryConfigFilename  = "discovery.yml"   // 服务发现配置
	KafkaConfigFileName      = "kafka.yml"       // Kafka消息队列配置
	LocalCacheConfigFileName = "local-cache.yml" // 本地缓存配置
	LogConfigFileName        = "log.yml"         // 日志配置
	MinioConfigFileName      = "minio.yml"       // MinIO对象存储配置
	MongodbConfigFileName    = "mongodb.yml"     // MongoDB数据库配置
	RedisConfigFileName      = "redis.yml"       // Redis缓存配置

	// 核心服务配置文件
	OpenIMAPICfgFileName         = "openim-api.yml"         // API网关配置
	OpenIMCronTaskCfgFileName    = "openim-crontask.yml"    // 定时任务配置
	OpenIMMsgGatewayCfgFileName  = "openim-msggateway.yml"  // 消息网关配置
	OpenIMMsgTransferCfgFileName = "openim-msgtransfer.yml" // 消息传输配置
	OpenIMPushCfgFileName        = "openim-push.yml"        // 推送服务配置

	// RPC服务配置文件
	OpenIMRPCAuthCfgFileName         = "openim-rpc-auth.yml"         // 认证服务配置
	OpenIMRPCConversationCfgFileName = "openim-rpc-conversation.yml" // 会话服务配置
	OpenIMRPCFriendCfgFileName       = "openim-rpc-friend.yml"       // 好友服务配置
	OpenIMRPCGroupCfgFileName        = "openim-rpc-group.yml"        // 群组服务配置
	OpenIMRPCMsgCfgFileName          = "openim-rpc-msg.yml"          // 消息服务配置
	OpenIMRPCThirdCfgFileName        = "openim-rpc-third.yml"        // 第三方服务配置
	OpenIMRPCUserCfgFileName         = "openim-rpc-user.yml"         // 用户服务配置

	// 业务配置文件
	ShareFileName          = "share.yml"    // 共享配置
	WebhooksConfigFileName = "webhooks.yml" // Webhook配置
)

func (d *Discovery) GetConfigFileName() string {
	return DiscoveryConfigFilename
}

func (k *Kafka) GetConfigFileName() string {
	return KafkaConfigFileName
}

func (lc *LocalCache) GetConfigFileName() string {
	return LocalCacheConfigFileName
}

func (l *Log) GetConfigFileName() string {
	return LogConfigFileName
}

func (m *Minio) GetConfigFileName() string {
	return MinioConfigFileName
}

func (m *Mongo) GetConfigFileName() string {
	return MongodbConfigFileName
}

func (n *Notification) GetConfigFileName() string {
	return NotificationFileName
}

func (a *API) GetConfigFileName() string {
	return OpenIMAPICfgFileName
}

func (ct *CronTask) GetConfigFileName() string {
	return OpenIMCronTaskCfgFileName
}

func (mg *MsgGateway) GetConfigFileName() string {
	return OpenIMMsgGatewayCfgFileName
}

func (mt *MsgTransfer) GetConfigFileName() string {
	return OpenIMMsgTransferCfgFileName
}

func (p *Push) GetConfigFileName() string {
	return OpenIMPushCfgFileName
}

func (a *Auth) GetConfigFileName() string {
	return OpenIMRPCAuthCfgFileName
}

func (c *Conversation) GetConfigFileName() string {
	return OpenIMRPCConversationCfgFileName
}

func (f *Friend) GetConfigFileName() string {
	return OpenIMRPCFriendCfgFileName
}

func (g *Group) GetConfigFileName() string {
	return OpenIMRPCGroupCfgFileName
}

func (m *Msg) GetConfigFileName() string {
	return OpenIMRPCMsgCfgFileName
}

func (t *Third) GetConfigFileName() string {
	return OpenIMRPCThirdCfgFileName
}

func (u *User) GetConfigFileName() string {
	return OpenIMRPCUserCfgFileName
}

func (r *Redis) GetConfigFileName() string {
	return RedisConfigFileName
}

func (s *Share) GetConfigFileName() string {
	return ShareFileName
}

func (w *Webhooks) GetConfigFileName() string {
	return WebhooksConfigFileName
}
