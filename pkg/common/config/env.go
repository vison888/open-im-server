/*
 * 环境变量配置模块
 *
 * 本模块负责管理OpenIM系统中各种配置文件对应的环境变量前缀映射。
 * 提供统一的环境变量命名规范和配置文件标识符管理。
 *
 * 核心功能：
 * - 环境变量前缀映射：为每个配置文件生成对应的环境变量前缀
 * - 命名规范统一：确保所有环境变量遵循统一的命名规范
 * - 配置文件标识：定义系统中使用的各种配置文件标识符
 * - 自动化映射：自动生成配置文件名到环境变量前缀的映射关系
 *
 * 环境变量命名规则：
 * - 前缀：IMENV_
 * - 文件名转换：移除.yml/.yaml后缀
 * - 字符替换：连字符(-)替换为下划线(_)
 * - 大小写：全部转换为大写
 * - 示例：openim-api.yml -> IMENV_OPENIM_API
 *
 * 使用场景：
 * - 容器化部署中的环境变量配置
 * - 多环境配置管理（开发、测试、生产）
 * - CI/CD流水线中的配置注入
 * - 配置文件的动态覆盖
 */
package config

import "strings"

// EnvPrefixMap 环境变量前缀映射表
//
// 这个映射表存储了每个配置文件名对应的环境变量前缀。
// 用于在配置加载时确定应该使用哪个环境变量前缀来覆盖配置文件中的值。
//
// 映射规则：
// - 键：配置文件名（如 "config.yaml"）
// - 值：对应的环境变量前缀（如 "IMENV_CONFIG"）
//
// 使用方式：
// - 在配置加载时查找对应的环境变量前缀
// - 支持通过环境变量覆盖配置文件中的任意值
// - 确保不同配置文件的环境变量不会冲突
//
// 示例：
//
//	prefix := EnvPrefixMap["openim-api.yml"]  // 返回 "IMENV_OPENIM_API"
//	// 然后可以使用 IMENV_OPENIM_API_API_PORTS 等环境变量
var EnvPrefixMap map[string]string

// init 初始化环境变量前缀映射
//
// 在包加载时自动执行，为所有已知的配置文件生成对应的环境变量前缀。
// 这确保了系统中所有配置文件都有统一的环境变量命名规范。
//
// 处理流程：
// 1. 初始化映射表
// 2. 遍历所有配置文件名
// 3. 为每个文件名生成对应的环境变量前缀
// 4. 存储到映射表中
//
// 命名转换规则：
// - 移除文件扩展名（.yml, .yaml）
// - 添加统一前缀 "IMENV_"
// - 将连字符替换为下划线
// - 转换为大写字母
func init() {
	// 初始化环境变量前缀映射表
	EnvPrefixMap = make(map[string]string)

	// 定义系统中所有的配置文件名
	// 包括核心配置、服务配置、中间件配置等
	fileNames := []string{
		// 核心配置文件
		FileName,               // config.yaml - 主配置文件
		NotificationFileName,   // notification.yaml - 通知配置
		ShareFileName,          // share.yml - 共享配置
		WebhooksConfigFileName, // webhooks.yml - Webhook配置

		// 中间件配置文件
		KafkaConfigFileName,   // kafka.yml - Kafka消息队列配置
		RedisConfigFileName,   // redis.yml - Redis缓存配置
		MongodbConfigFileName, // mongodb.yml - MongoDB数据库配置
		MinioConfigFileName,   // minio.yml - MinIO对象存储配置
		LogConfigFileName,     // log.yml - 日志配置

		// 服务配置文件
		OpenIMAPICfgFileName,         // openim-api.yml - API网关配置
		OpenIMCronTaskCfgFileName,    // openim-crontask.yml - 定时任务配置
		OpenIMMsgGatewayCfgFileName,  // openim-msggateway.yml - 消息网关配置
		OpenIMMsgTransferCfgFileName, // openim-msgtransfer.yml - 消息传输配置
		OpenIMPushCfgFileName,        // openim-push.yml - 推送服务配置

		// RPC服务配置文件
		OpenIMRPCAuthCfgFileName,         // openim-rpc-auth.yml - 认证服务配置
		OpenIMRPCConversationCfgFileName, // openim-rpc-conversation.yml - 会话服务配置
		OpenIMRPCFriendCfgFileName,       // openim-rpc-friend.yml - 好友服务配置
		OpenIMRPCGroupCfgFileName,        // openim-rpc-group.yml - 群组服务配置
		OpenIMRPCMsgCfgFileName,          // openim-rpc-msg.yml - 消息服务配置
		OpenIMRPCThirdCfgFileName,        // openim-rpc-third.yml - 第三方服务配置
		OpenIMRPCUserCfgFileName,         // openim-rpc-user.yml - 用户服务配置

		// 其他配置文件
		DiscoveryConfigFilename, // discovery.yml - 服务发现配置
	}

	// 为每个配置文件生成对应的环境变量前缀
	for _, fileName := range fileNames {
		// 移除文件扩展名（.yml 或 .yaml）
		envKey := strings.TrimSuffix(strings.TrimSuffix(fileName, ".yml"), ".yaml")

		// 添加统一的环境变量前缀
		envKey = "IMENV_" + envKey

		// 转换为大写并将连字符替换为下划线
		envKey = strings.ToUpper(strings.ReplaceAll(envKey, "-", "_"))

		// 存储映射关系
		EnvPrefixMap[fileName] = envKey
	}
}

// 命令行参数和配置相关常量
//
// 这些常量定义了系统中使用的标准命令行参数名称和配置标识符，
// 确保整个系统使用统一的参数命名规范。
const (
	// FlagConf 配置文件夹路径参数名
	// 用于命令行参数中指定配置文件夹的路径
	// 示例：--config_folder_path=/path/to/config
	FlagConf = "config_folder_path"

	// FlagTransferIndex 传输索引参数名
	// 用于消息传输服务中指定处理器索引
	// 主要用于多实例部署时的负载均衡和任务分配
	// 示例：--index=0
	FlagTransferIndex = "index"
)
