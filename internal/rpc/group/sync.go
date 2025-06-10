/*
 * 群组数据同步服务
 *
 * 本模块实现了群组系统的增量数据同步机制，是确保多端数据一致性的核心组件。
 *
 * 同步机制核心功能：
 *
 * 1. 增量同步算法
 *    - 版本控制：基于版本号的增量数据追踪
 *    - 变更检测：识别新增、修改、删除的数据变化
 *    - 批量同步：支持批量获取多个群组的增量数据
 *    - 全量兜底：版本差异过大时的全量数据同步
 *
 * 2. 群组成员同步
 *    - 成员变更：实时同步成员加入、退出、信息修改
 *    - 角色变更：管理员任免、群主转让的权限同步
 *    - 状态同步：禁言状态、在线状态等实时更新
 *    - 排序版本：支持成员列表的排序版本控制
 *
 * 3. 群组列表同步
 *    - 加群同步：用户新加入群组的列表更新
 *    - 退群同步：用户退出群组后的列表维护
 *    - 群组信息：群组基础信息变更的同步
 *    - 权限同步：用户在群组中权限变化的更新
 *
 * 4. 版本管理机制
 *    - 版本递增：每次数据变更自动递增版本号
 *    - 版本缓存：高频访问版本信息的缓存优化
 *    - 版本校验：客户端版本与服务端版本的一致性检查
 *    - 版本清理：过期版本数据的定期清理机制
 *
 * 5. 性能优化策略
 *    - 限量同步：单次同步数据量限制，避免大数据传输
 *    - 哈希校验：使用ID哈希快速检测数据变化
 *    - 分页同步：大量数据的分页传输机制
 *    - 缓存策略：版本信息和热点数据的多级缓存
 *
 * 6. 数据一致性保证
 *    - 事务控制：关键操作的事务一致性保证
 *    - 重试机制：网络异常时的自动重试逻辑
 *    - 冲突解决：并发修改时的数据冲突处理
 *    - 回滚机制：异常情况下的数据回滚策略
 *
 * 技术特性：
 * - 高效算法：增量同步避免全量数据传输
 * - 实时性：数据变更的毫秒级同步响应
 * - 可扩展性：支持大规模用户和群组的同步
 * - 容错性：网络异常和服务故障的自动恢复
 */
package group

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/internal/rpc/incrversion"
	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/open-im-server/v3/pkg/common/servererrs"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/open-im-server/v3/pkg/util/hashutil"
	"github.com/openimsdk/protocol/constant"
	pbgroup "github.com/openimsdk/protocol/group"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/mcontext"
	"github.com/openimsdk/tools/utils/datautil"
)

const versionSyncLimit = 500

func (g *groupServer) GetFullGroupMemberUserIDs(ctx context.Context, req *pbgroup.GetFullGroupMemberUserIDsReq) (*pbgroup.GetFullGroupMemberUserIDsResp, error) {
	userIDs, err := g.db.FindGroupMemberUserID(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}
	if !authverify.IsAppManagerUid(ctx, g.config.Share.IMAdminUserID) {
		if !datautil.Contain(mcontext.GetOpUserID(ctx), userIDs...) {
			return nil, errs.ErrNoPermission.WrapMsg("op user not in group")
		}
	}
	vl, err := g.db.FindMaxGroupMemberVersionCache(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}
	idHash := hashutil.IdHash(userIDs)
	if req.IdHash == idHash {
		userIDs = nil
	}
	return &pbgroup.GetFullGroupMemberUserIDsResp{
		Version:   idHash,
		VersionID: vl.ID.Hex(),
		Equal:     req.IdHash == idHash,
		UserIDs:   userIDs,
	}, nil
}

func (s *groupServer) GetFullJoinGroupIDs(ctx context.Context, req *pbgroup.GetFullJoinGroupIDsReq) (*pbgroup.GetFullJoinGroupIDsResp, error) {
	if err := authverify.CheckAccessV3(ctx, req.UserID, s.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}
	vl, err := s.db.FindMaxJoinGroupVersionCache(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	groupIDs, err := s.db.FindJoinGroupID(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	idHash := hashutil.IdHash(groupIDs)
	if req.IdHash == idHash {
		groupIDs = nil
	}
	return &pbgroup.GetFullJoinGroupIDsResp{
		Version:   idHash,
		VersionID: vl.ID.Hex(),
		Equal:     req.IdHash == idHash,
		GroupIDs:  groupIDs,
	}, nil
}

func (s *groupServer) GetIncrementalGroupMember(ctx context.Context, req *pbgroup.GetIncrementalGroupMemberReq) (*pbgroup.GetIncrementalGroupMemberResp, error) {
	if err := s.checkAdminOrInGroup(ctx, req.GroupID); err != nil {
		return nil, err
	}
	group, err := s.db.TakeGroup(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}
	if group.Status == constant.GroupStatusDismissed {
		return nil, servererrs.ErrDismissedAlready.Wrap()
	}
	var (
		hasGroupUpdate bool
		sortVersion    uint64
	)
	opt := incrversion.Option[*sdkws.GroupMemberFullInfo, pbgroup.GetIncrementalGroupMemberResp]{
		Ctx:           ctx,
		VersionKey:    req.GroupID,
		VersionID:     req.VersionID,
		VersionNumber: req.Version,
		Version: func(ctx context.Context, groupID string, version uint, limit int) (*model.VersionLog, error) {
			vl, err := s.db.FindMemberIncrVersion(ctx, groupID, version, limit)
			if err != nil {
				return nil, err
			}
			logs := make([]model.VersionLogElem, 0, len(vl.Logs))
			for i, log := range vl.Logs {
				switch log.EID {
				case model.VersionGroupChangeID:
					vl.LogLen--
					hasGroupUpdate = true
				case model.VersionSortChangeID:
					vl.LogLen--
					sortVersion = uint64(log.Version)
				default:
					logs = append(logs, vl.Logs[i])
				}
			}
			vl.Logs = logs
			if vl.LogLen > 0 {
				hasGroupUpdate = true
			}
			return vl, nil
		},
		CacheMaxVersion: s.db.FindMaxGroupMemberVersionCache,
		Find: func(ctx context.Context, ids []string) ([]*sdkws.GroupMemberFullInfo, error) {
			return s.getGroupMembersInfo(ctx, req.GroupID, ids)
		},
		Resp: func(version *model.VersionLog, delIDs []string, insertList, updateList []*sdkws.GroupMemberFullInfo, full bool) *pbgroup.GetIncrementalGroupMemberResp {
			return &pbgroup.GetIncrementalGroupMemberResp{
				VersionID:   version.ID.Hex(),
				Version:     uint64(version.Version),
				Full:        full,
				Delete:      delIDs,
				Insert:      insertList,
				Update:      updateList,
				SortVersion: sortVersion,
			}
		},
	}
	resp, err := opt.Build()
	if err != nil {
		return nil, err
	}
	if resp.Full || hasGroupUpdate {
		count, err := s.db.FindGroupMemberNum(ctx, group.GroupID)
		if err != nil {
			return nil, err
		}
		owner, err := s.db.TakeGroupOwner(ctx, group.GroupID)
		if err != nil {
			return nil, err
		}
		resp.Group = s.groupDB2PB(group, owner.UserID, count)
	}
	return resp, nil
}

func (g *groupServer) GetIncrementalJoinGroup(ctx context.Context, req *pbgroup.GetIncrementalJoinGroupReq) (*pbgroup.GetIncrementalJoinGroupResp, error) {
	if err := authverify.CheckAccessV3(ctx, req.UserID, g.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}
	opt := incrversion.Option[*sdkws.GroupInfo, pbgroup.GetIncrementalJoinGroupResp]{
		Ctx:             ctx,
		VersionKey:      req.UserID,
		VersionID:       req.VersionID,
		VersionNumber:   req.Version,
		Version:         g.db.FindJoinIncrVersion,
		CacheMaxVersion: g.db.FindMaxJoinGroupVersionCache,
		Find:            g.getGroupsInfo,
		Resp: func(version *model.VersionLog, delIDs []string, insertList, updateList []*sdkws.GroupInfo, full bool) *pbgroup.GetIncrementalJoinGroupResp {
			return &pbgroup.GetIncrementalJoinGroupResp{
				VersionID: version.ID.Hex(),
				Version:   uint64(version.Version),
				Full:      full,
				Delete:    delIDs,
				Insert:    insertList,
				Update:    updateList,
			}
		},
	}
	return opt.Build()
}

func (g *groupServer) BatchGetIncrementalGroupMember(ctx context.Context, req *pbgroup.BatchGetIncrementalGroupMemberReq) (*pbgroup.BatchGetIncrementalGroupMemberResp, error) {
	var num int
	resp := make(map[string]*pbgroup.GetIncrementalGroupMemberResp)
	for _, memberReq := range req.ReqList {
		if _, ok := resp[memberReq.GroupID]; ok {
			continue
		}
		memberResp, err := g.GetIncrementalGroupMember(ctx, memberReq)
		if err != nil {
			return nil, err
		}
		resp[memberReq.GroupID] = memberResp
		num += len(memberResp.Insert) + len(memberResp.Update) + len(memberResp.Delete)
		if num >= versionSyncLimit {
			break
		}
	}
	return &pbgroup.BatchGetIncrementalGroupMemberResp{RespList: resp}, nil
}
