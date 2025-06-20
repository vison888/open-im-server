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

// Package msg 消息服务包
// msg_status.go 专门处理消息发送状态管理功能
// 提供消息发送状态的设置和查询接口，用于跟踪消息的发送进度
// 主要用于客户端和服务器之间的消息状态同步
package msg

import (
	"context"

	"github.com/openimsdk/protocol/constant"
	pbmsg "github.com/openimsdk/protocol/msg"
	"github.com/openimsdk/tools/mcontext"
)

// SetSendMsgStatus 设置消息发送状态
// 用于更新特定操作ID对应的消息发送状态
// 通常在消息发送过程中的关键节点调用，记录发送进度
func (m *msgServer) SetSendMsgStatus(ctx context.Context, req *pbmsg.SetSendMsgStatusReq) (*pbmsg.SetSendMsgStatusResp, error) {
	resp := &pbmsg.SetSendMsgStatusResp{}

	// 从上下文中获取操作ID
	// 操作ID是每个请求的唯一标识符，用于跟踪整个请求链路
	operationID := mcontext.GetOperationID(ctx)

	// 将消息发送状态存储到数据库
	// 状态值的含义：
	// - constant.MsgStatusSending: 消息发送中
	// - constant.MsgStatusSendSuccess: 消息发送成功
	// - constant.MsgStatusSendFailed: 消息发送失败
	// - 其他自定义状态值
	if err := m.MsgDatabase.SetSendMsgStatus(ctx, operationID, req.Status); err != nil {
		return nil, err
	}

	return resp, nil
}

// GetSendMsgStatus 获取消息发送状态
// 根据操作ID查询对应的消息发送状态
// 客户端可以使用此接口轮询消息发送进度
func (m *msgServer) GetSendMsgStatus(ctx context.Context, req *pbmsg.GetSendMsgStatusReq) (*pbmsg.GetSendMsgStatusResp, error) {
	resp := &pbmsg.GetSendMsgStatusResp{}

	// 从上下文中获取操作ID
	operationID := mcontext.GetOperationID(ctx)

	// 从数据库查询操作ID对应的发送状态
	status, err := m.MsgDatabase.GetSendMsgStatus(ctx, operationID)

	// 处理记录不存在的情况
	// IsNotFound检查是否为"记录不存在"错误
	if IsNotFound(err) {
		// 如果没有找到状态记录，返回"不存在"状态
		// 这通常发生在操作ID无效或状态尚未设置的情况下
		resp.Status = constant.MsgStatusNotExist
		return resp, nil
	} else if err != nil {
		// 其他数据库错误直接返回
		return nil, err
	}

	// 返回查询到的状态
	resp.Status = status
	return resp, nil
}
