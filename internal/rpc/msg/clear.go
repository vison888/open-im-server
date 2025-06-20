// Package msg 消息服务包
// clear.go 专门处理消息清理和销毁功能
// 包含硬删除消息、根据时间戳获取消息序列号等管理员级别的操作
// 这些功能主要用于数据清理、存储优化和合规性要求
package msg

import (
	"context"
	"strings"

	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/protocol/msg"
	"github.com/openimsdk/tools/log"
)

// DestructMsgs 硬删除消息（物理删除）
// 这是一个危险的管理员操作，会从数据库中彻底删除消息记录
// 主要用于数据清理、存储空间回收和合规性要求（如GDPR数据删除）
func (m *msgServer) DestructMsgs(ctx context.Context, req *msg.DestructMsgsReq) (*msg.DestructMsgsResp, error) {
	// 权限验证：只有系统管理员才能执行硬删除操作
	// 这是重要的安全检查，防止普通用户误操作删除重要数据
	if err := authverify.CheckAdmin(ctx, m.config.Share.IMAdminUserID); err != nil {
		return nil, err
	}

	// 查找指定时间戳之前的随机消息文档
	// 使用随机查找可以避免每次都删除相同的文档，提高清理效率
	// Limit参数控制每次删除的文档数量，避免一次性删除过多数据影响性能
	docs, err := m.MsgDatabase.GetRandBeforeMsg(ctx, req.Timestamp, int(req.Limit))
	if err != nil {
		return nil, err
	}

	// 逐个处理每个文档
	for i, doc := range docs {
		// 从数据库中物理删除文档
		// 这个操作是不可逆的，删除后无法恢复
		if err := m.MsgDatabase.DeleteDoc(ctx, doc.DocID); err != nil {
			return nil, err
		}

		log.ZDebug(ctx, "DestructMsgs delete doc", "index", i, "docID", doc.DocID)

		// 解析文档ID以提取会话ID
		// 文档ID格式通常为 "conversationID:xxx"，需要提取前半部分
		index := strings.LastIndex(doc.DocID, ":")
		if index < 0 {
			// 如果文档ID格式不正确，跳过这个文档
			continue
		}

		// 查找文档中最大的消息序列号
		// 这将用于更新会话的最小序列号，确保已删除的消息不会再被查询到
		var minSeq int64
		for _, model := range doc.Msg {
			// 跳过空消息
			if model.Msg == nil {
				continue
			}
			// 记录最大的序列号
			if model.Msg.Seq > minSeq {
				minSeq = model.Msg.Seq
			}
		}

		// 如果没有找到有效的序列号，跳过
		if minSeq <= 0 {
			continue
		}

		// 提取会话ID
		conversationID := doc.DocID[:index]
		if conversationID == "" {
			continue
		}

		// 设置新的最小序列号为最大序列号+1
		// 这样可以确保所有小于等于minSeq的消息都不会被查询到
		// 即使它们在其他文档中仍然存在，也会被逻辑上"删除"
		minSeq++
		if err := m.MsgDatabase.SetMinSeq(ctx, conversationID, minSeq); err != nil {
			return nil, err
		}

		log.ZDebug(ctx, "DestructMsgs delete doc set min seq", "index", i, "docID", doc.DocID,
			"conversationID", conversationID, "setMinSeq", minSeq)
	}

	// 返回实际删除的文档数量
	return &msg.DestructMsgsResp{Count: int32(len(docs))}, nil
}

// GetLastMessageSeqByTime 根据时间戳获取最后一条消息的序列号
// 这个接口用于根据时间点查找对应的消息序列号
// 常用于消息同步、数据分析和时间范围查询等场景
func (m *msgServer) GetLastMessageSeqByTime(ctx context.Context, req *msg.GetLastMessageSeqByTimeReq) (*msg.GetLastMessageSeqByTimeResp, error) {
	// 从数据库查询指定时间点之前的最后一条消息序列号
	// 这个查询通常使用索引优化，可以快速定位到时间点对应的序列号
	seq, err := m.MsgDatabase.GetLastMessageSeqByTime(ctx, req.ConversationID, req.Time)
	if err != nil {
		return nil, err
	}

	// 返回查询到的序列号
	// 如果指定时间点没有消息，通常返回0或最小序列号
	return &msg.GetLastMessageSeqByTimeResp{Seq: seq}, nil
}
