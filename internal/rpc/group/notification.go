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

package group

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/openimsdk/open-im-server/v3/pkg/rpcli"

	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/open-im-server/v3/pkg/common/convert"
	"github.com/openimsdk/open-im-server/v3/pkg/common/servererrs"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/controller"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/versionctx"
	"github.com/openimsdk/open-im-server/v3/pkg/msgprocessor"
	"github.com/openimsdk/open-im-server/v3/pkg/notification"
	"github.com/openimsdk/open-im-server/v3/pkg/notification/common_user"
	"github.com/openimsdk/protocol/constant"
	pbgroup "github.com/openimsdk/protocol/group"
	"github.com/openimsdk/protocol/msg"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
	"github.com/openimsdk/tools/utils/datautil"
	"github.com/openimsdk/tools/utils/stringutil"
	"go.mongodb.org/mongo-driver/mongo"
)

// GroupApplicationReceiver
const (
	applicantReceiver = iota
	adminReceiver
)

func NewNotificationSender(db controller.GroupDatabase, config *Config, userClient *rpcli.UserClient, msgClient *rpcli.MsgClient, conversationClient *rpcli.ConversationClient) *NotificationSender {
	return &NotificationSender{
		NotificationSender: notification.NewNotificationSender(&config.NotificationConfig,
			notification.WithRpcClient(func(ctx context.Context, req *msg.SendMsgReq) (*msg.SendMsgResp, error) {
				return msgClient.SendMsg(ctx, req)
			}),
			notification.WithUserRpcClient(userClient.GetUserInfo),
		),
		getUsersInfo: func(ctx context.Context, userIDs []string) ([]common_user.CommonUser, error) {
			users, err := userClient.GetUsersInfo(ctx, userIDs)
			if err != nil {
				return nil, err
			}
			return datautil.Slice(users, func(e *sdkws.UserInfo) common_user.CommonUser { return e }), nil
		},
		db:                 db,
		config:             config,
		msgClient:          msgClient,
		conversationClient: conversationClient,
	}
}

type NotificationSender struct {
	*notification.NotificationSender
	getUsersInfo       func(ctx context.Context, userIDs []string) ([]common_user.CommonUser, error)
	db                 controller.GroupDatabase
	config             *Config
	msgClient          *rpcli.MsgClient
	conversationClient *rpcli.ConversationClient
}

func (g *NotificationSender) PopulateGroupMember(ctx context.Context, members ...*model.GroupMember) error {
	if len(members) == 0 {
		return nil
	}
	emptyUserIDs := make(map[string]struct{})
	for _, member := range members {
		if member.Nickname == "" || member.FaceURL == "" {
			emptyUserIDs[member.UserID] = struct{}{}
		}
	}
	if len(emptyUserIDs) > 0 {
		users, err := g.getUsersInfo(ctx, datautil.Keys(emptyUserIDs))
		if err != nil {
			return err
		}
		userMap := make(map[string]common_user.CommonUser)
		for i, user := range users {
			userMap[user.GetUserID()] = users[i]
		}
		for i, member := range members {
			user, ok := userMap[member.UserID]
			if !ok {
				continue
			}
			if member.Nickname == "" {
				members[i].Nickname = user.GetNickname()
			}
			if member.FaceURL == "" {
				members[i].FaceURL = user.GetFaceURL()
			}
		}
	}
	return nil
}

func (g *NotificationSender) getUser(ctx context.Context, userID string) (*sdkws.PublicUserInfo, error) {
	users, err := g.getUsersInfo(ctx, []string{userID})
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, servererrs.ErrUserIDNotFound.WrapMsg(fmt.Sprintf("user %s not found", userID))
	}
	return &sdkws.PublicUserInfo{
		UserID:   users[0].GetUserID(),
		Nickname: users[0].GetNickname(),
		FaceURL:  users[0].GetFaceURL(),
		Ex:       users[0].GetEx(),
	}, nil
}

func (g *NotificationSender) getGroupInfo(ctx context.Context, groupID string) (*sdkws.GroupInfo, error) {
	gm, err := g.db.TakeGroup(ctx, groupID)
	if err != nil {
		return nil, err
	}
	num, err := g.db.FindGroupMemberNum(ctx, groupID)
	if err != nil {
		return nil, err
	}
	ownerUserIDs, err := g.db.GetGroupRoleLevelMemberIDs(ctx, groupID, constant.GroupOwner)
	if err != nil {
		return nil, err
	}
	var ownerUserID string
	if len(ownerUserIDs) > 0 {
		ownerUserID = ownerUserIDs[0]
	}

	return convert.Db2PbGroupInfo(gm, ownerUserID, num), nil
}

func (g *NotificationSender) getGroupMembers(ctx context.Context, groupID string, userIDs []string) ([]*sdkws.GroupMemberFullInfo, error) {
	members, err := g.db.FindGroupMembers(ctx, groupID, userIDs)
	if err != nil {
		return nil, err
	}
	if err := g.PopulateGroupMember(ctx, members...); err != nil {
		return nil, err
	}
	log.ZDebug(ctx, "getGroupMembers", "members", members)
	res := make([]*sdkws.GroupMemberFullInfo, 0, len(members))
	for _, member := range members {
		res = append(res, g.groupMemberDB2PB(member, 0))
	}
	return res, nil
}

func (g *NotificationSender) getGroupMemberMap(ctx context.Context, groupID string, userIDs []string) (map[string]*sdkws.GroupMemberFullInfo, error) {
	members, err := g.getGroupMembers(ctx, groupID, userIDs)
	if err != nil {
		return nil, err
	}
	m := make(map[string]*sdkws.GroupMemberFullInfo)
	for i, member := range members {
		m[member.UserID] = members[i]
	}
	return m, nil
}

func (g *NotificationSender) getGroupMember(ctx context.Context, groupID string, userID string) (*sdkws.GroupMemberFullInfo, error) {
	members, err := g.getGroupMembers(ctx, groupID, []string{userID})
	if err != nil {
		return nil, err
	}
	if len(members) == 0 {
		return nil, errs.ErrInternalServer.WrapMsg(fmt.Sprintf("group %s member %s not found", groupID, userID))
	}
	return members[0], nil
}

func (g *NotificationSender) getGroupOwnerAndAdminUserID(ctx context.Context, groupID string) ([]string, error) {
	members, err := g.db.FindGroupMemberRoleLevels(ctx, groupID, []int32{constant.GroupOwner, constant.GroupAdmin})
	if err != nil {
		return nil, err
	}
	if err := g.PopulateGroupMember(ctx, members...); err != nil {
		return nil, err
	}
	fn := func(e *model.GroupMember) string { return e.UserID }
	return datautil.Slice(members, fn), nil
}

func (g *NotificationSender) groupMemberDB2PB(member *model.GroupMember, appMangerLevel int32) *sdkws.GroupMemberFullInfo {
	return &sdkws.GroupMemberFullInfo{
		GroupID:        member.GroupID,
		UserID:         member.UserID,
		RoleLevel:      member.RoleLevel,
		JoinTime:       member.JoinTime.UnixMilli(),
		Nickname:       member.Nickname,
		FaceURL:        member.FaceURL,
		AppMangerLevel: appMangerLevel,
		JoinSource:     member.JoinSource,
		OperatorUserID: member.OperatorUserID,
		Ex:             member.Ex,
		MuteEndTime:    member.MuteEndTime.UnixMilli(),
		InviterUserID:  member.InviterUserID,
	}
}

/* func (g *NotificationSender) getUsersInfoMap(ctx context.Context, userIDs []string) (map[string]*sdkws.UserInfo, error) {
	users, err := g.getUsersInfo(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[string]*sdkws.UserInfo)
	for _, user := range users {
		result[user.GetUserID()] = user.(*sdkws.UserInfo)
	}
	return result, nil
} */

func (g *NotificationSender) fillOpUser(ctx context.Context, targetUser **sdkws.GroupMemberFullInfo, groupID string) (err error) {
	return g.fillUserByUserID(ctx, mcontext.GetOpUserID(ctx), targetUser, groupID)
}

func (g *NotificationSender) fillUserByUserID(ctx context.Context, userID string, targetUser **sdkws.GroupMemberFullInfo, groupID string) error {
	if targetUser == nil {
		return errs.ErrInternalServer.WrapMsg("**sdkws.GroupMemberFullInfo is nil")
	}
	if groupID != "" {
		if authverify.IsManagerUserID(userID, g.config.Share.IMAdminUserID) {
			*targetUser = &sdkws.GroupMemberFullInfo{
				GroupID:        groupID,
				UserID:         userID,
				RoleLevel:      constant.GroupAdmin,
				AppMangerLevel: constant.AppAdmin,
			}
		} else {
			member, err := g.db.TakeGroupMember(ctx, groupID, userID)
			if err == nil {
				*targetUser = g.groupMemberDB2PB(member, 0)
			} else if !(errors.Is(err, mongo.ErrNoDocuments) || errs.ErrRecordNotFound.Is(err)) {
				return err
			}
		}
	}
	user, err := g.getUser(ctx, userID)
	if err != nil {
		return err
	}
	if *targetUser == nil {
		*targetUser = &sdkws.GroupMemberFullInfo{
			GroupID:        groupID,
			UserID:         userID,
			Nickname:       user.Nickname,
			FaceURL:        user.FaceURL,
			OperatorUserID: userID,
		}
	} else {
		if (*targetUser).Nickname == "" {
			(*targetUser).Nickname = user.Nickname
		}
		if (*targetUser).FaceURL == "" {
			(*targetUser).FaceURL = user.FaceURL
		}
	}
	return nil
}

func (g *NotificationSender) setVersion(ctx context.Context, version *uint64, versionID *string, collName string, id string) {
	versions := versionctx.GetVersionLog(ctx).Get()
	for i := len(versions) - 1; i >= 0; i-- {
		coll := versions[i]
		if coll.Name == collName && coll.Doc.DID == id {
			*version = uint64(coll.Doc.Version)
			*versionID = coll.Doc.ID.Hex()
			return
		}
	}
}

func (g *NotificationSender) setSortVersion(ctx context.Context, version *uint64, versionID *string, collName string, id string, sortVersion *uint64) {
	versions := versionctx.GetVersionLog(ctx).Get()
	for _, coll := range versions {
		if coll.Name == collName && coll.Doc.DID == id {
			*version = uint64(coll.Doc.Version)
			*versionID = coll.Doc.ID.Hex()
			for _, elem := range coll.Doc.Logs {
				if elem.EID == model.VersionSortChangeID {
					*sortVersion = uint64(elem.Version)
				}
			}
		}
	}
}

func (g *NotificationSender) GroupCreatedNotification(ctx context.Context, tips *sdkws.GroupCreatedTips, SendMessage *bool) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), tips.Group.GroupID, constant.GroupCreatedNotification, tips, notification.WithSendMessage(SendMessage))
}

func (g *NotificationSender) GroupInfoSetNotification(ctx context.Context, tips *sdkws.GroupInfoSetTips) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), tips.Group.GroupID, constant.GroupInfoSetNotification, tips, notification.WithRpcGetUserName())
}

func (g *NotificationSender) GroupInfoSetNameNotification(ctx context.Context, tips *sdkws.GroupInfoSetNameTips) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), tips.Group.GroupID, constant.GroupInfoSetNameNotification, tips)
}

func (g *NotificationSender) GroupInfoSetAnnouncementNotification(ctx context.Context, tips *sdkws.GroupInfoSetAnnouncementTips, sendMessage *bool) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), tips.Group.GroupID, constant.GroupInfoSetAnnouncementNotification, tips, notification.WithRpcGetUserName(), notification.WithSendMessage(sendMessage))
}

func (g *NotificationSender) uuid() string {
	return uuid.New().String()
}

func (g *NotificationSender) getGroupRequest(ctx context.Context, groupID string, userID string) (*sdkws.GroupRequest, error) {
	request, err := g.db.TakeGroupRequest(ctx, groupID, userID)
	if err != nil {
		return nil, err
	}
	users, err := g.getUsersInfo(ctx, []string{userID})
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, servererrs.ErrUserIDNotFound.WrapMsg(fmt.Sprintf("user %s not found", userID))
	}
	info, ok := users[0].(*sdkws.UserInfo)
	if !ok {
		info = &sdkws.UserInfo{
			UserID:   users[0].GetUserID(),
			Nickname: users[0].GetNickname(),
			FaceURL:  users[0].GetFaceURL(),
			Ex:       users[0].GetEx(),
		}
	}
	return convert.Db2PbGroupRequest(request, info, nil), nil
}

func (g *NotificationSender) JoinGroupApplicationNotification(ctx context.Context, req *pbgroup.JoinGroupReq, dbReq *model.GroupRequest) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	request, err := g.getGroupRequest(ctx, dbReq.GroupID, dbReq.UserID)
	if err != nil {
		log.ZError(ctx, "JoinGroupApplicationNotification getGroupRequest", err, "dbReq", dbReq)
		return
	}
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, req.GroupID)
	if err != nil {
		return
	}
	var user *sdkws.PublicUserInfo
	user, err = g.getUser(ctx, req.InviterUserID)
	if err != nil {
		return
	}
	userIDs, err := g.getGroupOwnerAndAdminUserID(ctx, req.GroupID)
	if err != nil {
		return
	}
	userIDs = append(userIDs, req.InviterUserID, mcontext.GetOpUserID(ctx))
	tips := &sdkws.JoinGroupApplicationTips{
		Group:     group,
		Applicant: user,
		ReqMsg:    req.ReqMessage,
		Uuid:      g.uuid(),
		Request:   request,
	}
	for _, userID := range datautil.Distinct(userIDs) {
		g.Notification(ctx, mcontext.GetOpUserID(ctx), userID, constant.JoinGroupApplicationNotification, tips)
	}
}

func (g *NotificationSender) MemberQuitNotification(ctx context.Context, member *sdkws.GroupMemberFullInfo) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, member.GroupID)
	if err != nil {
		return
	}
	tips := &sdkws.MemberQuitTips{Group: group, QuitUser: member}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, member.GroupID)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), member.GroupID, constant.MemberQuitNotification, tips)
}

func (g *NotificationSender) GroupApplicationAcceptedNotification(ctx context.Context, req *pbgroup.GroupApplicationResponseReq) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	request, err := g.getGroupRequest(ctx, req.GroupID, req.FromUserID)
	if err != nil {
		log.ZError(ctx, "GroupApplicationAcceptedNotification getGroupRequest", err, "req", req)
		return
	}
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, req.GroupID)
	if err != nil {
		return
	}
	var userIDs []string
	userIDs, err = g.getGroupOwnerAndAdminUserID(ctx, req.GroupID)
	if err != nil {
		return
	}

	var opUser *sdkws.GroupMemberFullInfo
	if err = g.fillOpUser(ctx, &opUser, group.GroupID); err != nil {
		return
	}
	tips := &sdkws.GroupApplicationAcceptedTips{
		Group:     group,
		OpUser:    opUser,
		HandleMsg: req.HandledMsg,
		Uuid:      g.uuid(),
		Request:   request,
	}
	for _, userID := range append(userIDs, req.FromUserID) {
		if userID == req.FromUserID {
			tips.ReceiverAs = applicantReceiver
		} else {
			tips.ReceiverAs = adminReceiver
		}
		g.Notification(ctx, mcontext.GetOpUserID(ctx), userID, constant.GroupApplicationAcceptedNotification, tips)
	}
}

func (g *NotificationSender) GroupApplicationRejectedNotification(ctx context.Context, req *pbgroup.GroupApplicationResponseReq) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	request, err := g.getGroupRequest(ctx, req.GroupID, req.FromUserID)
	if err != nil {
		log.ZError(ctx, "GroupApplicationAcceptedNotification getGroupRequest", err, "req", req)
		return
	}
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, req.GroupID)
	if err != nil {
		return
	}
	var userIDs []string
	userIDs, err = g.getGroupOwnerAndAdminUserID(ctx, req.GroupID)
	if err != nil {
		return
	}

	var opUser *sdkws.GroupMemberFullInfo
	if err = g.fillOpUser(ctx, &opUser, group.GroupID); err != nil {
		return
	}
	tips := &sdkws.GroupApplicationRejectedTips{
		Group:     group,
		OpUser:    opUser,
		HandleMsg: req.HandledMsg,
		Uuid:      g.uuid(),
		Request:   request,
	}
	for _, userID := range append(userIDs, req.FromUserID) {
		if userID == req.FromUserID {
			tips.ReceiverAs = applicantReceiver
		} else {
			tips.ReceiverAs = adminReceiver
		}
		g.Notification(ctx, mcontext.GetOpUserID(ctx), userID, constant.GroupApplicationRejectedNotification, tips)
	}
}

func (g *NotificationSender) GroupOwnerTransferredNotification(ctx context.Context, req *pbgroup.TransferGroupOwnerReq) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, req.GroupID)
	if err != nil {
		return
	}
	opUserID := mcontext.GetOpUserID(ctx)
	var member map[string]*sdkws.GroupMemberFullInfo
	member, err = g.getGroupMemberMap(ctx, req.GroupID, []string{opUserID, req.NewOwnerUserID, req.OldOwnerUserID})
	if err != nil {
		return
	}
	tips := &sdkws.GroupOwnerTransferredTips{
		Group:             group,
		OpUser:            member[opUserID],
		NewGroupOwner:     member[req.NewOwnerUserID],
		OldGroupOwnerInfo: member[req.OldOwnerUserID],
	}
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, req.GroupID)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), group.GroupID, constant.GroupOwnerTransferredNotification, tips)
}

func (g *NotificationSender) MemberKickedNotification(ctx context.Context, tips *sdkws.MemberKickedTips, SendMessage *bool) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), tips.Group.GroupID, constant.MemberKickedNotification, tips, notification.WithSendMessage(SendMessage))
}

func (g *NotificationSender) GroupApplicationAgreeMemberEnterNotification(ctx context.Context, groupID string, SendMessage *bool, invitedOpUserID string, entrantUserID ...string) error {
	return g.groupApplicationAgreeMemberEnterNotification(ctx, groupID, SendMessage, invitedOpUserID, entrantUserID...)
}

func (g *NotificationSender) groupApplicationAgreeMemberEnterNotification(ctx context.Context, groupID string, SendMessage *bool, invitedOpUserID string, entrantUserID ...string) error {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()

	if !g.config.RpcConfig.EnableHistoryForNewMembers {
		conversationID := msgprocessor.GetConversationIDBySessionType(constant.ReadGroupChatType, groupID)
		maxSeq, err := g.msgClient.GetConversationMaxSeq(ctx, conversationID)
		if err != nil {
			return err
		}
		if err := g.msgClient.SetUserConversationsMinSeq(ctx, conversationID, entrantUserID, maxSeq+1); err != nil {
			return err
		}
	}
	if err := g.conversationClient.CreateGroupChatConversations(ctx, groupID, entrantUserID); err != nil {
		return err
	}

	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, groupID)
	if err != nil {
		return err
	}
	users, err := g.getGroupMembers(ctx, groupID, entrantUserID)
	if err != nil {
		return err
	}

	tips := &sdkws.MemberInvitedTips{
		Group:           group,
		InvitedUserList: users,
	}
	opUserID := mcontext.GetOpUserID(ctx)
	if err = g.fillUserByUserID(ctx, opUserID, &tips.OpUser, tips.Group.GroupID); err != nil {
		return nil
	}
	if invitedOpUserID == opUserID {
		tips.InviterUser = tips.OpUser
	} else {
		if err = g.fillUserByUserID(ctx, invitedOpUserID, &tips.InviterUser, tips.Group.GroupID); err != nil {
			return err
		}
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), group.GroupID, constant.MemberInvitedNotification, tips, notification.WithSendMessage(SendMessage))
	return nil
}

func (g *NotificationSender) MemberEnterNotification(ctx context.Context, groupID string, entrantUserID string) error {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()

	if !g.config.RpcConfig.EnableHistoryForNewMembers {
		conversationID := msgprocessor.GetConversationIDBySessionType(constant.ReadGroupChatType, groupID)
		maxSeq, err := g.msgClient.GetConversationMaxSeq(ctx, conversationID)
		if err != nil {
			return err
		}
		if err := g.msgClient.SetUserConversationsMinSeq(ctx, conversationID, []string{entrantUserID}, maxSeq+1); err != nil {
			return err
		}
	}
	if err := g.conversationClient.CreateGroupChatConversations(ctx, groupID, []string{entrantUserID}); err != nil {
		return err
	}
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, groupID)
	if err != nil {
		return err
	}
	user, err := g.getGroupMember(ctx, groupID, entrantUserID)
	if err != nil {
		return err
	}

	tips := &sdkws.MemberEnterTips{
		Group:         group,
		EntrantUser:   user,
		OperationTime: time.Now().UnixMilli(),
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), group.GroupID, constant.MemberEnterNotification, tips)
	return nil
}

func (g *NotificationSender) GroupDismissedNotification(ctx context.Context, tips *sdkws.GroupDismissedTips, SendMessage *bool) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.Notification(ctx, mcontext.GetOpUserID(ctx), tips.Group.GroupID, constant.GroupDismissedNotification, tips, notification.WithSendMessage(SendMessage))
}

func (g *NotificationSender) GroupMemberMutedNotification(ctx context.Context, groupID, groupMemberUserID string, mutedSeconds uint32) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, groupID)
	if err != nil {
		return
	}
	var user map[string]*sdkws.GroupMemberFullInfo
	user, err = g.getGroupMemberMap(ctx, groupID, []string{mcontext.GetOpUserID(ctx), groupMemberUserID})
	if err != nil {
		return
	}
	tips := &sdkws.GroupMemberMutedTips{
		Group: group, MutedSeconds: mutedSeconds,
		OpUser: user[mcontext.GetOpUserID(ctx)], MutedUser: user[groupMemberUserID],
	}
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), group.GroupID, constant.GroupMemberMutedNotification, tips)
}

func (g *NotificationSender) GroupMemberCancelMutedNotification(ctx context.Context, groupID, groupMemberUserID string) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, groupID)
	if err != nil {
		return
	}
	var user map[string]*sdkws.GroupMemberFullInfo
	user, err = g.getGroupMemberMap(ctx, groupID, []string{mcontext.GetOpUserID(ctx), groupMemberUserID})
	if err != nil {
		return
	}
	tips := &sdkws.GroupMemberCancelMutedTips{Group: group, OpUser: user[mcontext.GetOpUserID(ctx)], MutedUser: user[groupMemberUserID]}
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), group.GroupID, constant.GroupMemberCancelMutedNotification, tips)
}

func (g *NotificationSender) GroupMutedNotification(ctx context.Context, groupID string) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, groupID)
	if err != nil {
		return
	}
	var users []*sdkws.GroupMemberFullInfo
	users, err = g.getGroupMembers(ctx, groupID, []string{mcontext.GetOpUserID(ctx)})
	if err != nil {
		return
	}
	tips := &sdkws.GroupMutedTips{Group: group}
	if len(users) > 0 {
		tips.OpUser = users[0]
	}
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, groupID)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), group.GroupID, constant.GroupMutedNotification, tips)
}

func (g *NotificationSender) GroupCancelMutedNotification(ctx context.Context, groupID string) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, groupID)
	if err != nil {
		return
	}
	var users []*sdkws.GroupMemberFullInfo
	users, err = g.getGroupMembers(ctx, groupID, []string{mcontext.GetOpUserID(ctx)})
	if err != nil {
		return
	}
	tips := &sdkws.GroupCancelMutedTips{Group: group}
	if len(users) > 0 {
		tips.OpUser = users[0]
	}
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, groupID)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), group.GroupID, constant.GroupCancelMutedNotification, tips)
}

func (g *NotificationSender) GroupMemberInfoSetNotification(ctx context.Context, groupID, groupMemberUserID string) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, groupID)
	if err != nil {
		return
	}
	var user map[string]*sdkws.GroupMemberFullInfo
	user, err = g.getGroupMemberMap(ctx, groupID, []string{groupMemberUserID})
	if err != nil {
		return
	}
	tips := &sdkws.GroupMemberInfoSetTips{Group: group, OpUser: user[mcontext.GetOpUserID(ctx)], ChangedUser: user[groupMemberUserID]}
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setSortVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID, &tips.GroupSortVersion)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), group.GroupID, constant.GroupMemberInfoSetNotification, tips)
}

func (g *NotificationSender) GroupMemberSetToAdminNotification(ctx context.Context, groupID, groupMemberUserID string) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, groupID)
	if err != nil {
		return
	}
	user, err := g.getGroupMemberMap(ctx, groupID, []string{mcontext.GetOpUserID(ctx), groupMemberUserID})
	if err != nil {
		return
	}
	tips := &sdkws.GroupMemberInfoSetTips{Group: group, OpUser: user[mcontext.GetOpUserID(ctx)], ChangedUser: user[groupMemberUserID]}
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setSortVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID, &tips.GroupSortVersion)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), group.GroupID, constant.GroupMemberSetToAdminNotification, tips)
}

func (g *NotificationSender) GroupMemberSetToOrdinaryUserNotification(ctx context.Context, groupID, groupMemberUserID string) {
	var err error
	defer func() {
		if err != nil {
			log.ZError(ctx, stringutil.GetFuncName(1)+" failed", err)
		}
	}()
	var group *sdkws.GroupInfo
	group, err = g.getGroupInfo(ctx, groupID)
	if err != nil {
		return
	}
	var user map[string]*sdkws.GroupMemberFullInfo
	user, err = g.getGroupMemberMap(ctx, groupID, []string{mcontext.GetOpUserID(ctx), groupMemberUserID})
	if err != nil {
		return
	}
	tips := &sdkws.GroupMemberInfoSetTips{Group: group, OpUser: user[mcontext.GetOpUserID(ctx)], ChangedUser: user[groupMemberUserID]}
	if err = g.fillOpUser(ctx, &tips.OpUser, tips.Group.GroupID); err != nil {
		return
	}
	g.setSortVersion(ctx, &tips.GroupMemberVersion, &tips.GroupMemberVersionID, database.GroupMemberVersionName, tips.Group.GroupID, &tips.GroupSortVersion)
	g.Notification(ctx, mcontext.GetOpUserID(ctx), group.GroupID, constant.GroupMemberSetToOrdinaryUserNotification, tips)
}
