// Package msg 消息服务包
// filter.go 专门处理消息过滤功能
// 提供基于用户ID、消息类型等维度的灵活过滤机制
// 主要用于webhook回调的条件过滤，减少不必要的回调请求
package msg

import (
	"strconv"
	"strings"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	pbchat "github.com/openimsdk/protocol/msg"
	"github.com/openimsdk/tools/utils/datautil"
)

// 定义分隔符常量，用于解析消息类型范围
const (
	separator = "-" // 用于表示消息类型范围，如 "1-100" 表示类型1到100
)

// filterAfterMsg 过滤发送后回调的消息
// 根据after配置决定哪些消息需要执行发送后回调
// 返回true表示消息通过过滤，需要执行回调
func filterAfterMsg(msg *pbchat.SendMsgReq, after *config.AfterConfig) bool {
	// 调用通用过滤器，传入after配置的过滤条件
	// AttentionIds: 关注的用户ID列表，只有这些用户的消息才会回调
	// AllowedTypes: 允许回调的消息类型列表
	// DeniedTypes: 禁止回调的消息类型列表
	return filterMsg(msg, after.AttentionIds, after.AllowedTypes, after.DeniedTypes)
}

// filterBeforeMsg 过滤发送前回调的消息
// 根据before配置决定哪些消息需要执行发送前回调
// 发送前回调通常不需要AttentionIds过滤，因为所有消息都可能需要预处理
func filterBeforeMsg(msg *pbchat.SendMsgReq, before *config.BeforeConfig) bool {
	// 发送前回调不使用AttentionIds过滤（传入nil）
	// 这是因为发送前的过滤通常是基于内容和类型的，而不是基于用户的
	return filterMsg(msg, nil, before.AllowedTypes, before.DeniedTypes)
}

// filterMsg 通用消息过滤器
// 这是过滤系统的核心实现，支持多种过滤维度的组合
// 过滤条件之间是AND关系，必须全部满足才能通过
func filterMsg(msg *pbchat.SendMsgReq, attentionIds, allowedTypes, deniedTypes []string) bool {
	// 第一层过滤：用户ID过滤
	// 如果配置了关注用户列表，只有发送者或接收者在列表中的消息才能通过
	// 这种设计常用于只关心特定用户的回调场景
	if len(attentionIds) != 0 && !datautil.Contains([]string{msg.MsgData.SendID, msg.MsgData.RecvID}, attentionIds...) {
		return false // 发送者和接收者都不在关注列表中，过滤掉
	}

	// 第二层过滤：允许类型过滤
	// 如果配置了允许类型列表，只有在列表中的消息类型才能通过
	// 这是白名单模式，只处理指定类型的消息
	if len(allowedTypes) != 0 && !isInInterval(msg.MsgData.ContentType, allowedTypes) {
		return false // 消息类型不在允许列表中，过滤掉
	}

	// 第三层过滤：禁止类型过滤
	// 如果配置了禁止类型列表，在列表中的消息类型将被过滤掉
	// 这是黑名单模式，排除不需要处理的消息类型
	if len(deniedTypes) != 0 && isInInterval(msg.MsgData.ContentType, deniedTypes) {
		return false // 消息类型在禁止列表中，过滤掉
	}

	// 通过所有过滤条件
	return true
}

// isInInterval 检查消息类型是否在指定的区间列表中
// 支持两种格式：
// 1. 单个数字：如 "101" 表示类型101
// 2. 范围：如 "100-200" 表示类型100到200的所有类型
// 这种设计使得配置更加灵活，可以方便地指定消息类型范围
func isInInterval(contentType int32, interval []string) bool {
	// 遍历每个配置的区间
	for _, v := range interval {
		if strings.Contains(v, separator) {
			// 处理范围格式：如 "100-200"
			bounds := strings.Split(v, separator)
			if len(bounds) != 2 {
				// 格式错误，跳过这个配置项
				continue
			}

			// 解析下界
			bottom, err := strconv.Atoi(bounds[0])
			if err != nil {
				continue // 解析失败，跳过
			}

			// 解析上界
			top, err := strconv.Atoi(bounds[1])
			if err != nil {
				continue // 解析失败，跳过
			}

			// 检查消息类型是否在范围内（包含边界）
			if datautil.BetweenEq(int(contentType), bottom, top) {
				return true // 在范围内，匹配成功
			}
		} else {
			// 处理单个数字格式：如 "101"
			iv, err := strconv.Atoi(v)
			if err != nil {
				continue // 解析失败，跳过
			}

			// 精确匹配消息类型
			if int(contentType) == iv {
				return true // 匹配成功
			}
		}
	}

	// 没有找到匹配的区间
	return false
}
