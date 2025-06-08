package redis

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache/cachekey"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/redis/go-redis/v9"
)

// NewUserOnline 创建用户在线状态缓存实例
// 设计思路：
// 1. 使用Redis Sorted Set存储用户在线状态，score为过期时间戳
// 2. 通过Redis发布订阅机制实现状态变更通知
// 3. 支持分布式环境下的状态一致性
// 参数：
//   - rdb: Redis通用客户端接口，支持单机、集群、哨兵模式
//
// 返回值：
//   - cache.OnlineCache: 在线状态缓存接口实现
func NewUserOnline(rdb redis.UniversalClient) cache.OnlineCache {
	return &userOnline{
		rdb:         rdb,
		expire:      cachekey.OnlineExpire,  // 在线状态过期时间
		channelName: cachekey.OnlineChannel, // 状态变更通知频道
	}
}

// userOnline Redis在线状态缓存实现
// 数据结构设计：
// - 使用Sorted Set存储：key为用户在线状态键，member为平台ID，score为过期时间戳
// - 优势：自动按时间排序，支持范围查询，便于清理过期数据
type userOnline struct {
	rdb         redis.UniversalClient // Redis客户端
	expire      time.Duration         // 在线状态过期时间
	channelName string                // 状态变更通知频道名
}

// getUserOnlineKey 生成用户在线状态的Redis键
// 键格式：online:用户ID
// 设计思路：统一键命名规范，便于管理和监控
func (s *userOnline) getUserOnlineKey(userID string) string {
	return cachekey.GetOnlineKey(userID)
}

// GetOnline 获取指定用户当前在线的平台列表
// 设计思路：
// 1. 使用ZRANGEBYSCORE命令查询score大于当前时间戳的成员
// 2. score小于当前时间的成员表示已过期，自动过滤
// 3. 返回仍然有效的平台ID列表
// 参数：
//   - ctx: 上下文信息
//   - userID: 用户唯一标识
//
// 返回值：
//   - []int32: 当前在线的平台ID列表
//   - error: 错误信息
func (s *userOnline) GetOnline(ctx context.Context, userID string) ([]int32, error) {
	// 查询score大于等于当前时间戳的所有成员（未过期的在线状态）
	members, err := s.rdb.ZRangeByScore(ctx, s.getUserOnlineKey(userID), &redis.ZRangeBy{
		Min: strconv.FormatInt(time.Now().Unix(), 10), // 最小值为当前时间戳
		Max: "+inf",                                   // 最大值为正无穷
	}).Result()
	if err != nil {
		return nil, errs.Wrap(err)
	}

	// 将字符串格式的平台ID转换为int32类型
	platformIDs := make([]int32, 0, len(members))
	for _, member := range members {
		val, err := strconv.Atoi(member)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		platformIDs = append(platformIDs, int32(val))
	}
	return platformIDs, nil
}

// GetAllOnlineUsers 分页获取所有在线用户及其平台信息
// 设计思路：
// 1. 使用SCAN命令遍历所有在线状态键，支持分页避免阻塞
// 2. 对每个用户键使用ZRANGE获取完整的平台列表
// 3. 返回用户ID到平台列表的映射关系
// 应用场景：管理后台统计、系统监控、批量操作等
// 参数：
//   - ctx: 上下文信息
//   - cursor: 分页游标，首次查询传0
//
// 返回值：
//   - map[string][]int32: 用户ID到平台ID列表的映射
//   - uint64: 下一页游标，为0表示已遍历完成
//   - error: 错误信息
func (s *userOnline) GetAllOnlineUsers(ctx context.Context, cursor uint64) (map[string][]int32, uint64, error) {
	result := make(map[string][]int32)

	// 使用SCAN命令分页扫描所有在线状态键
	keys, nextCursor, err := s.rdb.Scan(ctx, cursor, fmt.Sprintf("%s*", cachekey.OnlineKey), constant.ParamMaxLength).Result()
	if err != nil {
		return nil, 0, err
	}

	// 遍历每个用户的在线状态键
	for _, key := range keys {
		// 从键中提取用户ID
		userID := cachekey.GetOnlineKeyUserID(key)
		// 获取该用户所有平台的在线状态（包括过期的）
		strValues, err := s.rdb.ZRange(ctx, key, 0, -1).Result()
		if err != nil {
			return nil, 0, err
		}

		// 转换平台ID格式
		values := make([]int32, 0, len(strValues))
		for _, value := range strValues {
			intValue, err := strconv.Atoi(value)
			if err != nil {
				return nil, 0, errs.Wrap(err)
			}
			values = append(values, int32(intValue))
		}

		result[userID] = values
	}

	return result, nextCursor, nil
}

// SetUserOnline 原子性设置用户在线状态
// 设计思路：
// 1. 使用Lua脚本确保操作的原子性，避免并发问题
// 2. 清理过期的平台状态，移除指定的离线平台，添加新的在线平台
// 3. 只有状态发生实际变更时才发布通知，减少不必要的消息
// 4. 通过Redis发布订阅机制通知其他服务实例状态变更
//
// Lua脚本逻辑解析：
// 1. 记录操作前的成员数量
// 2. 清理过期成员（score小于当前时间）
// 3. 移除指定的离线平台
// 4. 添加新的在线平台（score为过期时间戳）
// 5. 设置键的过期时间
// 6. 检查成员数量是否发生变化，决定是否发布通知
//
// 参数：
//   - ctx: 上下文信息
//   - userID: 用户唯一标识
//   - online: 要设置为在线的平台ID列表
//   - offline: 要设置为离线的平台ID列表
//
// 返回值：
//   - error: 错误信息
func (s *userOnline) SetUserOnline(ctx context.Context, userID string, online, offline []int32) error {
	// Lua脚本：原子性执行用户在线状态更新
	// 脚本参数说明：
	// KEYS[1]: 用户在线状态键
	// ARGV[1]: 键过期时间（秒）
	// ARGV[2]: 当前时间戳（用于清理过期数据）
	// ARGV[3]: 未来过期时间戳（作为新在线状态的score）
	// ARGV[4]: 离线平台数量
	// ARGV[5...]: 离线平台ID列表 + 在线平台ID列表
	script := `
	local key = KEYS[1]
	local score = ARGV[3]
	
	-- 记录操作前的成员数量
	local num1 = redis.call("ZCARD", key)
	
	-- 清理过期的成员（score小于当前时间戳）
	redis.call("ZREMRANGEBYSCORE", key, "-inf", ARGV[2])
	
	-- 移除指定的离线平台
	for i = 5, tonumber(ARGV[4])+4 do
		redis.call("ZREM", key, ARGV[i])
	end
	
	-- 记录移除操作后的成员数量
	local num2 = redis.call("ZCARD", key)
	
	-- 添加新的在线平台，score为未来过期时间戳
	for i = 5+tonumber(ARGV[4]), #ARGV do
		redis.call("ZADD", key, score, ARGV[i])
	end
	
	-- 设置键的过期时间，防止内存泄漏
	redis.call("EXPIRE", key, ARGV[1])
	
	-- 记录最终的成员数量
	local num3 = redis.call("ZCARD", key)
	
	-- 判断是否发生了实际变更
	local change = (num1 ~= num2) or (num2 ~= num3)
	
	if change then
		-- 状态发生变更，返回当前所有在线平台 + 变更标志
		local members = redis.call("ZRANGE", key, 0, -1)
		table.insert(members, "1")  -- 添加变更标志
		return members
	else
		-- 状态未变更，返回无变更标志
		return {"0"}
	end
`
	// 构建脚本参数
	now := time.Now()
	argv := make([]any, 0, 2+len(online)+len(offline))
	argv = append(argv,
		int32(s.expire/time.Second), // 键过期时间（秒）
		now.Unix(),                  // 当前时间戳
		now.Add(s.expire).Unix(),    // 未来过期时间戳
		int32(len(offline)),         // 离线平台数量
	)

	// 添加离线平台ID列表
	for _, platformID := range offline {
		argv = append(argv, platformID)
	}
	// 添加在线平台ID列表
	for _, platformID := range online {
		argv = append(argv, platformID)
	}

	// 执行Lua脚本
	keys := []string{s.getUserOnlineKey(userID)}
	platformIDs, err := s.rdb.Eval(ctx, script, keys, argv).StringSlice()
	if err != nil {
		log.ZError(ctx, "redis SetUserOnline", err, "userID", userID, "online", online, "offline", offline)
		return err
	}

	// 检查返回值有效性
	if len(platformIDs) == 0 {
		return errs.ErrInternalServer.WrapMsg("SetUserOnline redis lua invalid return value")
	}

	// 检查是否需要发布状态变更通知
	if platformIDs[len(platformIDs)-1] != "0" {
		// 状态发生了变更，发布通知消息
		log.ZDebug(ctx, "redis SetUserOnline push", "userID", userID, "online", online, "offline", offline, "platformIDs", platformIDs[:len(platformIDs)-1])

		// 构建通知消息：平台ID列表 + 用户ID，用冒号分隔
		platformIDs[len(platformIDs)-1] = userID // 替换变更标志为用户ID
		msg := strings.Join(platformIDs, ":")

		// 发布到状态变更通知频道
		if err := s.rdb.Publish(ctx, s.channelName, msg).Err(); err != nil {
			return errs.Wrap(err)
		}
	} else {
		// 状态未发生变更，记录调试日志
		log.ZDebug(ctx, "redis SetUserOnline not push", "userID", userID, "online", online, "offline", offline)
	}
	return nil
}
