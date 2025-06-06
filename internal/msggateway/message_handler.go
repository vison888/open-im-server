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

// message_handler.go - 消息处理器模块
//
// 功能概述:
// 1. 处理WebSocket客户端发送的消息请求
// 2. 将WebSocket消息转换为相应的RPC调用
// 3. 管理请求-响应的对象池优化性能
// 4. 提供统一的消息路由和错误处理机制
// 5. 支持异步消息处理和响应管理
//
// 设计思路:
// - 对象池优化: 使用sync.Pool复用Req对象，减少GC压力
// - 统一路由: 所有WebSocket消息通过统一入口处理
// - 错误处理: 统一的错误响应格式和异常处理
// - 性能优化: 异步处理和内存复用提升吞吐量
// - 解耦设计: 消息处理与具体业务逻辑分离
//
// 核心职责:
// - 消息解析: 解析WebSocket原始消息
// - 路由分发: 根据操作ID路由到对应的RPC服务
// - 响应处理: 统一格式化和发送响应消息
// - 性能优化: 对象池管理和异步处理

package msggateway

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/openimsdk/open-im-server/v3/pkg/rpcli"

	"github.com/go-playground/validator/v10"
	"google.golang.org/protobuf/proto"

	"github.com/openimsdk/protocol/msg"
	"github.com/openimsdk/protocol/push"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/utils/jsonutil"
)

const (
	TextPing = "ping"
	TextPong = "pong"
)

type TextMessage struct {
	Type string          `json:"type"`
	Body json.RawMessage `json:"body"`
}

type Req struct {
	ReqIdentifier int32  `json:"reqIdentifier" validate:"required"`
	Token         string `json:"token"`
	SendID        string `json:"sendID"        validate:"required"`
	OperationID   string `json:"operationID"   validate:"required"`
	MsgIncr       string `json:"msgIncr"       validate:"required"`
	Data          []byte `json:"data"`
}

func (r *Req) String() string {
	var tReq Req
	tReq.ReqIdentifier = r.ReqIdentifier
	tReq.Token = r.Token
	tReq.SendID = r.SendID
	tReq.OperationID = r.OperationID
	tReq.MsgIncr = r.MsgIncr
	return jsonutil.StructToJsonString(tReq)
}

var reqPool = sync.Pool{
	New: func() any {
		return new(Req)
	},
}

func getReq() *Req {
	req := reqPool.Get().(*Req)
	req.Data = nil
	req.MsgIncr = ""
	req.OperationID = ""
	req.ReqIdentifier = 0
	req.SendID = ""
	req.Token = ""
	return req
}

func freeReq(req *Req) {
	reqPool.Put(req)
}

type Resp struct {
	ReqIdentifier int32  `json:"reqIdentifier"`
	MsgIncr       string `json:"msgIncr"`
	OperationID   string `json:"operationID"`
	ErrCode       int    `json:"errCode"`
	ErrMsg        string `json:"errMsg"`
	Data          []byte `json:"data"`
}

func (r *Resp) String() string {
	var tResp Resp
	tResp.ReqIdentifier = r.ReqIdentifier
	tResp.MsgIncr = r.MsgIncr
	tResp.OperationID = r.OperationID
	tResp.ErrCode = r.ErrCode
	tResp.ErrMsg = r.ErrMsg
	return jsonutil.StructToJsonString(tResp)
}

type MessageHandler interface {
	GetSeq(ctx context.Context, data *Req) ([]byte, error)
	SendMessage(ctx context.Context, data *Req) ([]byte, error)
	SendSignalMessage(ctx context.Context, data *Req) ([]byte, error)
	PullMessageBySeqList(ctx context.Context, data *Req) ([]byte, error)
	GetConversationsHasReadAndMaxSeq(ctx context.Context, data *Req) ([]byte, error)
	GetSeqMessage(ctx context.Context, data *Req) ([]byte, error)
	UserLogout(ctx context.Context, data *Req) ([]byte, error)
	SetUserDeviceBackground(ctx context.Context, data *Req) ([]byte, bool, error)
	GetLastMessage(ctx context.Context, data *Req) ([]byte, error)
}

var _ MessageHandler = (*GrpcHandler)(nil)

type GrpcHandler struct {
	validate   *validator.Validate
	msgClient  *rpcli.MsgClient
	pushClient *rpcli.PushMsgServiceClient
}

func NewGrpcHandler(validate *validator.Validate, msgClient *rpcli.MsgClient, pushClient *rpcli.PushMsgServiceClient) *GrpcHandler {
	return &GrpcHandler{
		validate:   validate,
		msgClient:  msgClient,
		pushClient: pushClient,
	}
}

func (g *GrpcHandler) GetSeq(ctx context.Context, data *Req) ([]byte, error) {
	req := sdkws.GetMaxSeqReq{}
	if err := proto.Unmarshal(data.Data, &req); err != nil {
		return nil, errs.WrapMsg(err, "GetSeq: error unmarshaling request", "action", "unmarshal", "dataType", "GetMaxSeqReq")
	}
	if err := g.validate.Struct(&req); err != nil {
		return nil, errs.WrapMsg(err, "GetSeq: validation failed", "action", "validate", "dataType", "GetMaxSeqReq")
	}
	resp, err := g.msgClient.MsgClient.GetMaxSeq(ctx, &req)
	if err != nil {
		return nil, err
	}
	c, err := proto.Marshal(resp)
	if err != nil {
		return nil, errs.WrapMsg(err, "GetSeq: error marshaling response", "action", "marshal", "dataType", "GetMaxSeqResp")
	}
	return c, nil
}

// SendMessage handles the sending of messages through gRPC. It unmarshals the request data,
// validates the message, and then sends it using the message RPC client.
func (g *GrpcHandler) SendMessage(ctx context.Context, data *Req) ([]byte, error) {
	var msgData sdkws.MsgData
	if err := proto.Unmarshal(data.Data, &msgData); err != nil {
		return nil, errs.WrapMsg(err, "SendMessage: error unmarshaling message data", "action", "unmarshal", "dataType", "MsgData")
	}

	if err := g.validate.Struct(&msgData); err != nil {
		return nil, errs.WrapMsg(err, "SendMessage: message data validation failed", "action", "validate", "dataType", "MsgData")
	}

	req := msg.SendMsgReq{MsgData: &msgData}
	resp, err := g.msgClient.MsgClient.SendMsg(ctx, &req)
	if err != nil {
		return nil, err
	}

	c, err := proto.Marshal(resp)
	if err != nil {
		return nil, errs.WrapMsg(err, "SendMessage: error marshaling response", "action", "marshal", "dataType", "SendMsgResp")
	}

	return c, nil
}

func (g *GrpcHandler) SendSignalMessage(ctx context.Context, data *Req) ([]byte, error) {
	resp, err := g.msgClient.MsgClient.SendMsg(ctx, nil)
	if err != nil {
		return nil, err
	}
	c, err := proto.Marshal(resp)
	if err != nil {
		return nil, errs.WrapMsg(err, "error marshaling response", "action", "marshal", "dataType", "SendMsgResp")
	}
	return c, nil
}

func (g *GrpcHandler) PullMessageBySeqList(ctx context.Context, data *Req) ([]byte, error) {
	req := sdkws.PullMessageBySeqsReq{}
	if err := proto.Unmarshal(data.Data, &req); err != nil {
		return nil, errs.WrapMsg(err, "err proto unmarshal", "action", "unmarshal", "dataType", "PullMessageBySeqsReq")
	}
	if err := g.validate.Struct(data); err != nil {
		return nil, errs.WrapMsg(err, "validation failed", "action", "validate", "dataType", "PullMessageBySeqsReq")
	}
	resp, err := g.msgClient.MsgClient.PullMessageBySeqs(ctx, &req)
	if err != nil {
		return nil, err
	}
	c, err := proto.Marshal(resp)
	if err != nil {
		return nil, errs.WrapMsg(err, "error marshaling response", "action", "marshal", "dataType", "PullMessageBySeqsResp")
	}
	return c, nil
}

func (g *GrpcHandler) GetConversationsHasReadAndMaxSeq(ctx context.Context, data *Req) ([]byte, error) {
	req := msg.GetConversationsHasReadAndMaxSeqReq{}
	if err := proto.Unmarshal(data.Data, &req); err != nil {
		return nil, errs.WrapMsg(err, "err proto unmarshal", "action", "unmarshal", "dataType", "GetConversationsHasReadAndMaxSeq")
	}
	if err := g.validate.Struct(data); err != nil {
		return nil, errs.WrapMsg(err, "validation failed", "action", "validate", "dataType", "GetConversationsHasReadAndMaxSeq")
	}
	resp, err := g.msgClient.MsgClient.GetConversationsHasReadAndMaxSeq(ctx, &req)
	if err != nil {
		return nil, err
	}
	c, err := proto.Marshal(resp)
	if err != nil {
		return nil, errs.WrapMsg(err, "error marshaling response", "action", "marshal", "dataType", "GetConversationsHasReadAndMaxSeq")
	}
	return c, nil
}

func (g *GrpcHandler) GetSeqMessage(ctx context.Context, data *Req) ([]byte, error) {
	req := msg.GetSeqMessageReq{}
	if err := proto.Unmarshal(data.Data, &req); err != nil {
		return nil, errs.WrapMsg(err, "error unmarshaling request", "action", "unmarshal", "dataType", "GetSeqMessage")
	}
	if err := g.validate.Struct(data); err != nil {
		return nil, errs.WrapMsg(err, "validation failed", "action", "validate", "dataType", "GetSeqMessage")
	}
	resp, err := g.msgClient.MsgClient.GetSeqMessage(ctx, &req)
	if err != nil {
		return nil, err
	}
	c, err := proto.Marshal(resp)
	if err != nil {
		return nil, errs.WrapMsg(err, "error marshaling response", "action", "marshal", "dataType", "GetSeqMessage")
	}
	return c, nil
}

func (g *GrpcHandler) UserLogout(ctx context.Context, data *Req) ([]byte, error) {
	req := push.DelUserPushTokenReq{}
	if err := proto.Unmarshal(data.Data, &req); err != nil {
		return nil, errs.WrapMsg(err, "error unmarshaling request", "action", "unmarshal", "dataType", "DelUserPushTokenReq")
	}
	resp, err := g.pushClient.PushMsgServiceClient.DelUserPushToken(ctx, &req)
	if err != nil {
		return nil, err
	}
	c, err := proto.Marshal(resp)
	if err != nil {
		return nil, errs.WrapMsg(err, "error marshaling response", "action", "marshal", "dataType", "DelUserPushTokenResp")
	}
	return c, nil
}

func (g *GrpcHandler) SetUserDeviceBackground(ctx context.Context, data *Req) ([]byte, bool, error) {
	req := sdkws.SetAppBackgroundStatusReq{}
	if err := proto.Unmarshal(data.Data, &req); err != nil {
		return nil, false, errs.WrapMsg(err, "error unmarshaling request", "action", "unmarshal", "dataType", "SetAppBackgroundStatusReq")
	}
	if err := g.validate.Struct(data); err != nil {
		return nil, false, errs.WrapMsg(err, "validation failed", "action", "validate", "dataType", "SetAppBackgroundStatusReq")
	}
	return nil, req.IsBackground, nil
}

func (g *GrpcHandler) GetLastMessage(ctx context.Context, data *Req) ([]byte, error) {
	var req msg.GetLastMessageReq
	if err := proto.Unmarshal(data.Data, &req); err != nil {
		return nil, err
	}
	resp, err := g.msgClient.GetLastMessage(ctx, &req)
	if err != nil {
		return nil, err
	}
	return proto.Marshal(resp)
}
