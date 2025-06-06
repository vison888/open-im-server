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

package conversation

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/notification"
	"github.com/openimsdk/open-im-server/v3/pkg/rpcli"
	"github.com/openimsdk/protocol/msg"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/sdkws"
)

type ConversationNotificationSender struct {
	*notification.NotificationSender
}

func NewConversationNotificationSender(conf *config.Notification, msgClient *rpcli.MsgClient) *ConversationNotificationSender {
	return &ConversationNotificationSender{notification.NewNotificationSender(conf, notification.WithRpcClient(func(ctx context.Context, req *msg.SendMsgReq) (*msg.SendMsgResp, error) {
		return msgClient.SendMsg(ctx, req)
	}))}
}

// SetPrivate invote.
func (c *ConversationNotificationSender) ConversationSetPrivateNotification(ctx context.Context, sendID, recvID string,
	isPrivateChat bool, conversationID string,
) {
	tips := &sdkws.ConversationSetPrivateTips{
		RecvID:         recvID,
		SendID:         sendID,
		IsPrivate:      isPrivateChat,
		ConversationID: conversationID,
	}

	c.Notification(ctx, sendID, recvID, constant.ConversationPrivateChatNotification, tips)
}

func (c *ConversationNotificationSender) ConversationChangeNotification(ctx context.Context, userID string, conversationIDs []string) {
	tips := &sdkws.ConversationUpdateTips{
		UserID:             userID,
		ConversationIDList: conversationIDs,
	}

	c.Notification(ctx, userID, userID, constant.ConversationChangeNotification, tips)
}

func (c *ConversationNotificationSender) ConversationUnreadChangeNotification(
	ctx context.Context,
	userID, conversationID string,
	unreadCountTime, hasReadSeq int64,
) {
	tips := &sdkws.ConversationHasReadTips{
		UserID:          userID,
		ConversationID:  conversationID,
		HasReadSeq:      hasReadSeq,
		UnreadCountTime: unreadCountTime,
	}

	c.Notification(ctx, userID, userID, constant.ConversationUnreadNotification, tips)
}
