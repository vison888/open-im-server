// Package controller 认证控制器包
// 提供用户认证、Token管理、多端登录控制等核心功能
// 支持JWT Token生成与验证、多设备登录策略、Token缓存管理
package controller

import (
	"context"

	"github.com/golang-jwt/jwt/v4"
	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache/cachekey"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/tokenverify"
)

// AuthDatabase 认证数据库接口
// 定义了用户认证、Token管理的核心操作接口
type AuthDatabase interface {
	// GetTokensWithoutError 获取用户在指定平台的所有Token（不返回错误）
	// 如果结果为空，不返回错误，用于兼容性处理
	// userID: 用户ID
	// platformID: 平台ID（iOS、Android、Web等）
	// 返回: Token映射表和错误信息
	GetTokensWithoutError(ctx context.Context, userID string, platformID int) (map[string]int, error)

	// CreateToken 创建用户Token
	// 核心认证方法，支持多端登录控制、Token踢下线等策略
	// userID: 用户ID
	// platformID: 平台ID
	// 返回: 生成的Token字符串和错误信息
	CreateToken(ctx context.Context, userID string, platformID int) (string, error)

	// BatchSetTokenMapByUidPid 批量设置Token映射表
	// 用于批量Token状态更新，如批量踢下线等场景
	// tokens: Token列表
	// 返回: 错误信息
	BatchSetTokenMapByUidPid(ctx context.Context, tokens []string) error

	// SetTokenMapByUidPid 设置用户平台的Token映射表
	// 更新指定用户在指定平台的Token状态映射
	// userID: 用户ID
	// platformID: 平台ID
	// m: Token状态映射（Token -> 状态）
	// 返回: 错误信息
	SetTokenMapByUidPid(ctx context.Context, userID string, platformID int, m map[string]int) error
}

// multiLoginConfig 多端登录配置
// 定义多设备登录的策略和限制
type multiLoginConfig struct {
	Policy       int // 多端登录策略（互踢、共存等）
	MaxNumOneEnd int // 单端最大登录数量
}

// authDatabase 认证数据库实现
// 整合了Token缓存、JWT签名、多端登录控制等功能
type authDatabase struct {
	cache        cache.TokenModel // Token缓存接口，提供Redis缓存操作
	accessSecret string           // JWT签名密钥，用于Token加密和验证
	accessExpire int64            // Token过期时间（秒）
	multiLogin   multiLoginConfig // 多端登录配置
	adminUserIDs []string         // 管理员用户ID列表，管理员不受多端登录限制
}

// NewAuthDatabase 创建认证数据库实例
// 初始化认证相关的所有组件：缓存、JWT配置、多端登录策略
// cache: Token缓存模型
// accessSecret: JWT签名密钥
// accessExpire: Token过期时间
// multiLogin: 多端登录配置
// adminUserIDs: 管理员用户ID列表
// 返回: AuthDatabase接口实例
func NewAuthDatabase(cache cache.TokenModel, accessSecret string, accessExpire int64, multiLogin config.MultiLogin, adminUserIDs []string) AuthDatabase {
	return &authDatabase{cache: cache, accessSecret: accessSecret, accessExpire: accessExpire, multiLogin: multiLoginConfig{
		Policy:       multiLogin.Policy,
		MaxNumOneEnd: multiLogin.MaxNumOneEnd,
	}, adminUserIDs: adminUserIDs,
	}
}

// GetTokensWithoutError 获取用户Token映射（兼容版本）
// 如果结果为空，不返回错误，主要用于兼容性处理
func (a *authDatabase) GetTokensWithoutError(ctx context.Context, userID string, platformID int) (map[string]int, error) {
	return a.cache.GetTokensWithoutError(ctx, userID, platformID)
}

// SetTokenMapByUidPid 设置Token映射表
// 更新用户在指定平台的Token状态映射到缓存
func (a *authDatabase) SetTokenMapByUidPid(ctx context.Context, userID string, platformID int, m map[string]int) error {
	return a.cache.SetTokenMapByUidPid(ctx, userID, platformID, m)
}

// BatchSetTokenMapByUidPid 批量设置Token映射表
// 解析Token列表，批量更新Token状态，用于批量踢下线等场景
func (a *authDatabase) BatchSetTokenMapByUidPid(ctx context.Context, tokens []string) error {
	setMap := make(map[string]map[string]any)
	for _, token := range tokens {
		// 解析Token获取用户信息
		claims, err := tokenverify.GetClaimFromToken(token, authverify.Secret(a.accessSecret))
		if err != nil {
			continue // 跳过无效Token
		}
		// 构建缓存键
		key := cachekey.GetTokenKey(claims.UserID, claims.PlatformID)
		if v, ok := setMap[key]; ok {
			v[token] = constant.KickedToken // 设置为被踢下线状态
		} else {
			setMap[key] = map[string]any{
				token: constant.KickedToken,
			}
		}
	}
	if err := a.cache.BatchSetTokenMapByUidPid(ctx, setMap); err != nil {
		return err
	}
	return nil
}

// CreateToken 创建用户认证Token
// 这是认证系统的核心方法，处理Token生成、多端登录控制、Token踢下线等复杂逻辑
//
// 主要功能：
// 1. 多端登录策略检查：根据配置决定是否踢掉其他设备的Token
// 2. Token生成：使用JWT生成包含用户信息的Token
// 3. 缓存管理：将新Token状态保存到Redis缓存
// 4. 管理员特权：管理员用户不受多端登录限制
//
// 多端登录策略说明：
// - DefalutNotKick: 默认不踢人，但有数量限制
// - AllLoginButSameTermKick: 允许多端登录但同类型设备互踢
// - PCAndOther: PC和其他设备可以共存
// - AllLoginButSameClassKick: 同类设备互踢
//
// userID: 用户唯一标识
// platformID: 平台ID（iOS=1, Android=2, Windows=3, Web=4等）
// 返回: 生成的JWT Token字符串和错误信息
func (a *authDatabase) CreateToken(ctx context.Context, userID string, platformID int) (string, error) {
	// 检查是否为管理员用户
	// 管理员用户不受多端登录策略限制，可以无限制登录
	isAdmin := authverify.IsManagerUserID(userID, a.adminUserIDs)

	if !isAdmin {
		// 非管理员用户需要进行多端登录控制
		// 获取用户所有平台的现有Token
		tokens, err := a.cache.GetAllTokensWithoutError(ctx, userID)
		if err != nil {
			return "", err
		}

		// 执行多端登录策略检查
		// deleteTokenKey: 需要删除的无效Token
		// kickedTokenKey: 需要踢下线的Token
		deleteTokenKey, kickedTokenKey, err := a.checkToken(ctx, tokens, platformID)
		if err != nil {
			return "", err
		}

		// 删除无效Token（过期、格式错误等）
		if len(deleteTokenKey) != 0 {
			err = a.cache.DeleteTokenByUidPid(ctx, userID, platformID, deleteTokenKey)
			if err != nil {
				return "", err
			}
		}

		// 踢下线冲突Token（根据多端登录策略）
		if len(kickedTokenKey) != 0 {
			for _, k := range kickedTokenKey {
				err := a.cache.SetTokenFlagEx(ctx, userID, platformID, k, constant.KickedToken)
				if err != nil {
					return "", err
				}
				log.ZDebug(ctx, "kicked token in create token", "token", k)
			}
		}
	}

	// 生成新的JWT Token
	// 创建Token声明，包含用户ID、平台ID、过期时间等信息
	claims := tokenverify.BuildClaims(userID, platformID, a.accessExpire)

	// 使用HS256算法签名Token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(a.accessSecret))
	if err != nil {
		return "", errs.WrapMsg(err, "token.SignedString")
	}

	// 非管理员用户需要将Token状态保存到缓存
	if !isAdmin {
		if err = a.cache.SetTokenFlagEx(ctx, userID, platformID, tokenString, constant.NormalToken); err != nil {
			return "", err
		}
	}

	return tokenString, nil
}

// checkToken 多端登录策略检查
// 根据配置的多端登录策略，决定哪些Token需要删除或踢下线
//
// 策略类型：
// 1. DefalutNotKick: 默认不踢人策略，但有数量限制
// 2. AllLoginButSameTermKick: 允许多端登录，但同终端类型互踢
// 3. PCAndOther: PC端和其他端可以共存
// 4. AllLoginButSameClassKick: 同类设备互踢
//
// tokens: 用户现有的所有Token映射 map[platformID]map[token]status
// platformID: 当前登录的平台ID
// 返回: 需要删除的Token列表、需要踢下线的Token列表、错误信息
func (a *authDatabase) checkToken(ctx context.Context, tokens map[int]map[string]int, platformID int) ([]string, []string, error) {
	// todo: 将处理旧数据的逻辑移动到其他位置
	var (
		loginTokenMap  = make(map[int][]string) // 有效登录Token映射 map[platformID][]token
		deleteToken    = make([]string, 0)      // 需要删除的无效Token列表
		kickToken      = make([]string, 0)      // 需要踢下线的Token列表
		adminToken     = make([]string, 0)      // 管理员Token列表
		unkickTerminal = ""                     // 不踢下线的终端类型
	)

	// 遍历所有平台的Token，分类处理
	for plfID, tks := range tokens {
		for k, v := range tks {
			// 验证Token有效性
			_, err := tokenverify.GetClaimFromToken(k, authverify.Secret(a.accessSecret))
			if err != nil || v != constant.NormalToken {
				// Token无效或状态异常，加入删除列表
				deleteToken = append(deleteToken, k)
			} else {
				// Token有效，按平台分类
				if plfID != constant.AdminPlatformID {
					loginTokenMap[plfID] = append(loginTokenMap[plfID], k)
				} else {
					adminToken = append(adminToken, k)
				}
			}
		}
	}

	// 根据多端登录策略执行相应逻辑
	switch a.multiLogin.Policy {
	case constant.DefalutNotKick:
		// 默认不踢人策略：每个平台有数量限制
		for plt, ts := range loginTokenMap {
			l := len(ts)
			if platformID == plt {
				l++ // 当前登录会增加一个Token
			}
			limit := a.multiLogin.MaxNumOneEnd
			if l > limit {
				// 超出限制，踢掉最旧的Token
				kickToken = append(kickToken, ts[:l-limit]...)
			}
		}

	case constant.AllLoginButSameTermKick:
		// 同终端类型互踢策略：不同终端可以共存，相同终端只保留最新
		for plt, ts := range loginTokenMap {
			// 保留最新Token，踢掉其他
			kickToken = append(kickToken, ts[:len(ts)-1]...)
			if plt == platformID {
				// 当前平台的最新Token也要被踢掉
				kickToken = append(kickToken, ts[len(ts)-1])
			}
		}

	case constant.PCAndOther:
		// PC和其他设备共存策略
		unkickTerminal = constant.TerminalPC
		if constant.PlatformIDToClass(platformID) != unkickTerminal {
			// 非PC端登录，踢掉其他非PC端Token
			for plt, ts := range loginTokenMap {
				if constant.PlatformIDToClass(plt) != unkickTerminal {
					kickToken = append(kickToken, ts...)
				}
			}
		} else {
			// PC端登录，复杂的共存逻辑
			var (
				preKick   []string
				isReserve = true
			)
			for plt, ts := range loginTokenMap {
				if constant.PlatformIDToClass(plt) != unkickTerminal {
					// 保留一个其他端Token
					if isReserve {
						isReserve = false
						kickToken = append(kickToken, ts[:len(ts)-1]...)
						preKick = append(preKick, ts[len(ts)-1])
						continue
					} else {
						// 优先保留Android端
						if plt == constant.AndroidPlatformID {
							kickToken = append(kickToken, preKick...)
							kickToken = append(kickToken, ts[:len(ts)-1]...)
						} else {
							kickToken = append(kickToken, ts...)
						}
					}
				}
			}
		}

	case constant.AllLoginButSameClassKick:
		// 同类设备互踢策略：每个设备类别只保留一个
		var (
			reserved = make(map[string]struct{})
		)

		for plt, ts := range loginTokenMap {
			if constant.PlatformIDToClass(plt) == constant.PlatformIDToClass(platformID) {
				// 相同类别设备，全部踢掉
				kickToken = append(kickToken, ts...)
			} else {
				// 不同类别设备，每类保留一个
				if _, ok := reserved[constant.PlatformIDToClass(plt)]; !ok {
					reserved[constant.PlatformIDToClass(plt)] = struct{}{}
					kickToken = append(kickToken, ts[:len(ts)-1]...)
					continue
				} else {
					kickToken = append(kickToken, ts...)
				}
			}
		}

	default:
		return nil, nil, errs.New("unknown multiLogin policy").Wrap()
	}

	// 注释：管理员Token数量限制逻辑（当前已禁用）
	//var adminTokenMaxNum = a.multiLogin.MaxNumOneEnd
	//if a.multiLogin.Policy == constant.Customize {
	//	adminTokenMaxNum = a.multiLogin.CustomizeLoginNum[constant.AdminPlatformID]
	//}
	//l := len(adminToken)
	//if platformID == constant.AdminPlatformID {
	//	l++
	//}
	//if l > adminTokenMaxNum {
	//	kickToken = append(kickToken, adminToken[:l-adminTokenMaxNum]...)
	//}

	return deleteToken, kickToken, nil
}
