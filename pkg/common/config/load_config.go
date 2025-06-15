/*
 * 配置文件加载模块
 *
 * 本模块提供了基于Viper的配置文件加载功能，支持多种配置源和环境变量覆盖。
 * 是OpenIM系统配置管理的核心加载器，提供统一的配置加载接口。
 *
 * 核心功能：
 * - 配置文件读取：支持YAML、JSON等多种格式的配置文件
 * - 环境变量支持：自动读取环境变量并覆盖配置文件中的值
 * - 结构体映射：将配置数据自动映射到Go结构体
 * - 错误处理：提供详细的配置加载错误信息
 *
 * 技术特性：
 * - 基于Viper：使用成熟的配置管理库
 * - 环境变量前缀：支持自定义环境变量前缀
 * - 自动类型转换：自动将配置值转换为目标类型
 * - 标签支持：使用mapstructure标签进行字段映射
 *
 * 使用场景：
 * - 系统启动时的配置加载
 * - 微服务配置的统一管理
 * - 多环境配置的动态切换
 * - 配置热重载和更新
 */
package config

import (
	"strings"

	"github.com/mitchellh/mapstructure"
	"github.com/openimsdk/tools/errs"
	"github.com/spf13/viper"
)

// LoadConfig 加载配置文件
//
// 这是一个通用的配置文件加载函数，使用Viper库来读取配置文件并将其映射到指定的结构体。
// 支持环境变量覆盖配置文件中的值，提供灵活的配置管理方案。
//
// 功能特性：
// 1. 多格式支持：自动识别YAML、JSON、TOML等配置文件格式
// 2. 环境变量覆盖：环境变量可以覆盖配置文件中的对应值
// 3. 自动类型转换：将配置值自动转换为目标结构体的字段类型
// 4. 嵌套配置支持：支持复杂的嵌套配置结构
//
// 环境变量规则：
// - 使用指定的前缀（envPrefix）
// - 点号（.）会被替换为下划线（_）
// - 自动转换为大写
// - 例如：prefix.database.host -> PREFIX_DATABASE_HOST
//
// 参数说明：
// - path: 配置文件的完整路径
// - envPrefix: 环境变量前缀，用于区分不同服务的环境变量
// - config: 目标配置结构体的指针，用于接收解析后的配置数据
//
// 返回值：
// - error: 配置加载过程中的错误信息，nil表示成功
//
// 错误处理：
// - 配置文件不存在或无法读取
// - 配置文件格式错误
// - 结构体映射失败
// - 类型转换错误
//
// 使用示例：
//
//	type Config struct {
//	    Database struct {
//	        Host string `mapstructure:"host"`
//	        Port int    `mapstructure:"port"`
//	    } `mapstructure:"database"`
//	}
//
//	var cfg Config
//	err := LoadConfig("/path/to/config.yaml", "MYAPP", &cfg)
//
// 环境变量示例：
//
//	MYAPP_DATABASE_HOST=localhost
//	MYAPP_DATABASE_PORT=3306
//
// 注意事项：
// - config参数必须是指针类型
// - 结构体字段需要使用mapstructure标签
// - 环境变量会覆盖配置文件中的对应值
// - 配置文件路径必须是绝对路径或相对于工作目录的路径
func LoadConfig(path string, envPrefix string, config any) error {
	// 创建新的Viper实例，避免全局状态污染
	v := viper.New()

	// 设置配置文件路径
	// Viper会根据文件扩展名自动识别配置文件格式
	v.SetConfigFile(path)

	// 设置环境变量前缀
	// 所有相关的环境变量都应该以此前缀开头
	v.SetEnvPrefix(envPrefix)

	// 启用自动环境变量读取
	// Viper会自动查找匹配的环境变量
	v.AutomaticEnv()

	// 设置环境变量键名替换规则
	// 将配置键中的点号（.）替换为下划线（_）以匹配环境变量命名规范
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// 读取配置文件内容
	if err := v.ReadInConfig(); err != nil {
		return errs.WrapMsg(err, "failed to read config file", "path", path, "envPrefix", envPrefix)
	}

	// 将配置数据映射到目标结构体
	// 使用mapstructure进行结构体字段映射
	if err := v.Unmarshal(config, func(config *mapstructure.DecoderConfig) {
		// 指定使用mapstructure标签进行字段映射
		config.TagName = "mapstructure"
	}); err != nil {
		return errs.WrapMsg(err, "failed to unmarshal config", "path", path, "envPrefix", envPrefix)
	}

	return nil
}
