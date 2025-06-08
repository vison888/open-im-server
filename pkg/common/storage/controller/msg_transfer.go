// Package controller 消息传输控制器包
// 提供消息存储、缓存、队列的统一管理接口
// 核心功能：批量消息插入、缓存管理、消息队列发送、已读状态管理
package controller

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/convert"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/kafka"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/protocol/constant"
	pbmsg "github.com/openimsdk/protocol/msg"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/utils/datautil"
	"go.mongodb.org/mongo-driver/mongo"
)

// MsgTransferDatabase 消息传输数据库接口
// 定义了消息存储、缓存、队列操作的核心接口，支持批量操作和异步处理
type MsgTransferDatabase interface {
	// BatchInsertChat2DB 批量插入消息到数据库
	// 将一批消息持久化存储到MongoDB，支持消息分片和索引优化
	// ctx: 上下文，用于超时控制和链路追踪
	// conversationID: 会话ID，用于消息分片和路由
	// msgs: 待插入的消息列表
	// currentMaxSeq: 当前最大序列号，用于数据一致性检查
	// 返回: 错误信息
	BatchInsertChat2DB(ctx context.Context, conversationID string, msgs []*sdkws.MsgData, currentMaxSeq int64) error

	// DeleteMessagesFromCache 从缓存中删除指定序列号的消息
	// 用于消息撤回、删除等场景的缓存清理
	// ctx: 上下文
	// conversationID: 会话ID
	// seqs: 要删除的消息序列号列表
	// 返回: 错误信息
	DeleteMessagesFromCache(ctx context.Context, conversationID string, seqs []int64) error

	// BatchInsertChat2Cache 批量插入消息到缓存
	// 分配序列号并将消息批量插入Redis缓存，提供快速读取能力
	// ctx: 上下文
	// conversationID: 会话ID
	// msgs: 待缓存的消息列表
	// 返回: 最后分配的序列号、是否为新会话、用户已读映射、错误信息
	BatchInsertChat2Cache(ctx context.Context, conversationID string, msgs []*sdkws.MsgData) (seq int64, isNewConversation bool, userHasReadMap map[string]int64, err error)

	// SetHasReadSeqs 设置用户已读序列号到缓存
	// 更新用户在指定会话中的已读状态，用于消息已读回执
	// ctx: 上下文
	// conversationID: 会话ID
	// userSeqMap: 用户ID到已读序列号的映射
	// 返回: 错误信息
	SetHasReadSeqs(ctx context.Context, conversationID string, userSeqMap map[string]int64) error

	// SetHasReadSeqToDB 设置用户已读序列号到数据库
	// 将已读状态持久化到数据库，保证数据可靠性
	// ctx: 上下文
	// conversationID: 会话ID
	// userSeqMap: 用户ID到已读序列号的映射
	// 返回: 错误信息
	SetHasReadSeqToDB(ctx context.Context, conversationID string, userSeqMap map[string]int64) error

	// MsgToPushMQ 发送消息到推送队列
	// 将消息发送到Kafka推送主题，用于实时消息推送
	// ctx: 上下文
	// key: Kafka分区键，用于消息路由和负载均衡
	// conversationID: 会话ID
	// msg2mq: 待推送的消息数据
	// 返回: Kafka分区号、偏移量、错误信息
	MsgToPushMQ(ctx context.Context, key, conversationID string, msg2mq *sdkws.MsgData) (int32, int64, error)

	// MsgToMongoMQ 发送消息到MongoDB持久化队列
	// 将消息发送到Kafka MongoDB主题，用于异步持久化存储
	// ctx: 上下文
	// key: Kafka分区键
	// conversationID: 会话ID
	// msgs: 待持久化的消息列表
	// lastSeq: 最后一条消息的序列号
	// 返回: 错误信息
	MsgToMongoMQ(ctx context.Context, key, conversationID string, msgs []*sdkws.MsgData, lastSeq int64) error
}

// NewMsgTransferDatabase 创建消息传输数据库实例
// 初始化消息传输所需的所有组件：数据库、缓存、序列号管理、Kafka生产者
// msgDocModel: MongoDB消息文档模型，用于消息持久化存储
// msg: 消息缓存接口，提供Redis缓存操作
// seqUser: 用户序列号缓存，管理用户已读状态
// seqConversation: 会话序列号缓存，管理消息序列号分配
// kafkaConf: Kafka配置，用于创建消息队列生产者
// 返回: MsgTransferDatabase接口实例和错误信息
func NewMsgTransferDatabase(msgDocModel database.Msg, msg cache.MsgCache, seqUser cache.SeqUser, seqConversation cache.SeqConversationCache, kafkaConf *config.Kafka) (MsgTransferDatabase, error) {
	// 构建Kafka生产者配置
	conf, err := kafka.BuildProducerConfig(*kafkaConf.Build())
	if err != nil {
		return nil, err
	}

	// 创建MongoDB持久化队列生产者
	producerToMongo, err := kafka.NewKafkaProducer(conf, kafkaConf.Address, kafkaConf.ToMongoTopic)
	if err != nil {
		return nil, err
	}

	// 创建消息推送队列生产者
	producerToPush, err := kafka.NewKafkaProducer(conf, kafkaConf.Address, kafkaConf.ToPushTopic)
	if err != nil {
		return nil, err
	}

	return &msgTransferDatabase{
		msgDocDatabase:  msgDocModel,     // MongoDB消息存储
		msgCache:        msg,             // Redis消息缓存
		seqUser:         seqUser,         // 用户序列号管理
		seqConversation: seqConversation, // 会话序列号管理
		producerToMongo: producerToMongo, // MongoDB队列生产者
		producerToPush:  producerToPush,  // 推送队列生产者
	}, nil
}

// msgTransferDatabase 消息传输数据库实现
// 整合了消息存储、缓存、序列号管理和消息队列的完整实现
type msgTransferDatabase struct {
	msgDocDatabase  database.Msg               // MongoDB消息文档数据库接口
	msgTable        model.MsgDocModel          // 消息文档模型，提供分片和索引策略
	msgCache        cache.MsgCache             // Redis消息缓存接口
	seqConversation cache.SeqConversationCache // 会话序列号缓存，管理消息序列号分配
	seqUser         cache.SeqUser              // 用户序列号缓存，管理用户已读状态
	producerToMongo *kafka.Producer            // MongoDB持久化队列生产者
	producerToPush  *kafka.Producer            // 消息推送队列生产者
}

// BatchInsertChat2DB 批量插入聊天消息到数据库
// 将消息列表转换为数据库模型并批量插入MongoDB，支持离线推送信息和消息状态管理
// ctx: 上下文
// conversationID: 会话ID，用于消息分片
// msgList: 待插入的消息列表
// currentMaxSeq: 当前最大序列号，用于数据一致性验证
// 返回: 错误信息
func (db *msgTransferDatabase) BatchInsertChat2DB(ctx context.Context, conversationID string, msgList []*sdkws.MsgData, currentMaxSeq int64) error {
	if len(msgList) == 0 {
		return errs.ErrArgs.WrapMsg("msgList is empty")
	}

	// 转换消息格式：从protobuf格式转换为数据库模型
	msgs := make([]any, len(msgList))
	seqs := make([]int64, len(msgList))
	for i, msg := range msgList {
		if msg == nil {
			continue
		}
		seqs[i] = msg.Seq

		// 处理离线推送信息
		var offlinePushModel *model.OfflinePushModel
		if msg.OfflinePushInfo != nil {
			offlinePushModel = &model.OfflinePushModel{
				Title:         msg.OfflinePushInfo.Title,         // 推送标题
				Desc:          msg.OfflinePushInfo.Desc,          // 推送描述
				Ex:            msg.OfflinePushInfo.Ex,            // 扩展信息
				IOSPushSound:  msg.OfflinePushInfo.IOSPushSound,  // iOS推送声音
				IOSBadgeCount: msg.OfflinePushInfo.IOSBadgeCount, // iOS角标数量
			}
		}

		// 更新消息状态：发送中状态改为发送成功
		if msg.Status == constant.MsgStatusSending {
			msg.Status = constant.MsgStatusSendSuccess
		}

		// 构建数据库消息模型
		msgs[i] = &model.MsgDataModel{
			SendID:           msg.SendID,           // 发送者ID
			RecvID:           msg.RecvID,           // 接收者ID
			GroupID:          msg.GroupID,          // 群组ID
			ClientMsgID:      msg.ClientMsgID,      // 客户端消息ID
			ServerMsgID:      msg.ServerMsgID,      // 服务端消息ID
			SenderPlatformID: msg.SenderPlatformID, // 发送者平台ID
			SenderNickname:   msg.SenderNickname,   // 发送者昵称
			SenderFaceURL:    msg.SenderFaceURL,    // 发送者头像URL
			SessionType:      msg.SessionType,      // 会话类型（单聊/群聊）
			MsgFrom:          msg.MsgFrom,          // 消息来源
			ContentType:      msg.ContentType,      // 内容类型
			Content:          string(msg.Content),  // 消息内容
			Seq:              msg.Seq,              // 消息序列号
			SendTime:         msg.SendTime,         // 发送时间
			CreateTime:       msg.CreateTime,       // 创建时间
			Status:           msg.Status,           // 消息状态
			Options:          msg.Options,          // 消息选项
			OfflinePush:      offlinePushModel,     // 离线推送信息
			AtUserIDList:     msg.AtUserIDList,     // @用户列表
			AttachedInfo:     msg.AttachedInfo,     // 附加信息
			Ex:               msg.Ex,               // 扩展字段
		}
	}

	// 批量插入到数据库，使用分块插入策略优化性能
	if err := db.BatchInsertBlock(ctx, conversationID, msgs, updateKeyMsg, msgList[0].Seq); err != nil {
		return err
	}

	// 注释：暂时不删除缓存，由其他机制处理缓存一致性
	//return db.msgCache.DelMessageBySeqs(ctx, conversationID, seqs)
	return nil
}

// BatchInsertBlock 批量分块插入数据到MongoDB
// 采用分块策略优化大批量数据插入性能，支持消息和撤回两种数据类型
// 优先尝试更新现有文档，失败时创建新文档，处理并发插入冲突
// ctx: 上下文
// conversationID: 会话ID，用于文档分片
// fields: 待插入的数据字段列表
// key: 操作类型（updateKeyMsg=消息，updateKeyRevoke=撤回）
// firstSeq: 第一条数据的序列号，用于序列号连续性验证
// 返回: 错误信息
func (db *msgTransferDatabase) BatchInsertBlock(ctx context.Context, conversationID string, fields []any, key int8, firstSeq int64) error {
	if len(fields) == 0 {
		return nil
	}

	// 获取单个文档最大消息数量（通常为100）
	num := db.msgTable.GetSingleGocMsgNum()

	// 验证数据类型和序列号连续性
	for i, field := range fields {
		var ok bool
		switch key {
		case updateKeyMsg:
			var msg *model.MsgDataModel
			msg, ok = field.(*model.MsgDataModel)
			// 验证序列号连续性，确保数据一致性
			if msg != nil && msg.Seq != firstSeq+int64(i) {
				return errs.ErrInternalServer.WrapMsg("seq is invalid")
			}
		case updateKeyRevoke:
			_, ok = field.(*model.RevokeModel)
		default:
			return errs.ErrInternalServer.WrapMsg("key is invalid")
		}
		if !ok {
			return errs.ErrInternalServer.WrapMsg("field type is invalid")
		}
	}
	// updateMsgModel 更新消息模型的内部函数
	// 返回true表示文档存在并更新成功，false表示文档不存在需要创建
	updateMsgModel := func(seq int64, i int) (bool, error) {
		var (
			res *mongo.UpdateResult
			err error
		)
		docID := db.msgTable.GetDocID(conversationID, seq) // 根据会话ID和序列号计算文档ID
		index := db.msgTable.GetMsgIndex(seq)              // 计算消息在文档中的索引位置
		field := fields[i]
		switch key {
		case updateKeyMsg:
			// 更新消息数据到指定文档的指定索引位置
			res, err = db.msgDocDatabase.UpdateMsg(ctx, docID, index, "msg", field)
		case updateKeyRevoke:
			// 更新撤回数据到指定文档的指定索引位置
			res, err = db.msgDocDatabase.UpdateMsg(ctx, docID, index, "revoke", field)
		}
		if err != nil {
			return false, err
		}
		return res.MatchedCount > 0, nil // 返回是否匹配到文档
	}

	// 插入策略：优先尝试更新，失败时创建新文档
	tryUpdate := true
	for i := 0; i < len(fields); i++ {
		seq := firstSeq + int64(i) // 计算当前序列号

		// 尝试更新现有文档
		if tryUpdate {
			matched, err := updateMsgModel(seq, i)
			if err != nil {
				return err
			}
			if matched {
				continue // 更新成功，跳过当前数据
			}
		}
		// 创建新文档：需要更新但文档不存在时
		doc := model.MsgDocModel{
			DocID: db.msgTable.GetDocID(conversationID, seq), // 计算文档ID
			Msg:   make([]*model.MsgInfoModel, num),          // 初始化消息数组
		}

		var insert int // 记录本次插入的数据数量
		// 将属于同一文档的连续数据批量插入
		for j := i; j < len(fields); j++ {
			seq = firstSeq + int64(j)
			// 检查是否还属于同一文档
			if db.msgTable.GetDocID(conversationID, seq) != doc.DocID {
				break
			}
			insert++

			// 根据操作类型设置消息数据
			switch key {
			case updateKeyMsg:
				doc.Msg[db.msgTable.GetMsgIndex(seq)] = &model.MsgInfoModel{
					Msg: fields[j].(*model.MsgDataModel),
				}
			case updateKeyRevoke:
				doc.Msg[db.msgTable.GetMsgIndex(seq)] = &model.MsgInfoModel{
					Revoke: fields[j].(*model.RevokeModel),
				}
			}
		}

		// 初始化空的消息槽位，确保文档结构完整
		for i, msgInfo := range doc.Msg {
			if msgInfo == nil {
				msgInfo = &model.MsgInfoModel{}
				doc.Msg[i] = msgInfo
			}
			if msgInfo.DelList == nil {
				doc.Msg[i].DelList = []string{} // 初始化删除列表
			}
		}

		// 创建文档到数据库
		if err := db.msgDocDatabase.Create(ctx, &doc); err != nil {
			if mongo.IsDuplicateKeyError(err) {
				// 并发插入冲突：其他协程已创建该文档
				i--              // 回退索引，重新处理当前数据
				tryUpdate = true // 下一轮使用更新模式
				continue
			}
			return err
		}

		tryUpdate = false // 当前块插入成功，下一块优先使用插入模式
		i += insert - 1   // 跳过已插入的数据
	}
	return nil
}

// DeleteMessagesFromCache 从缓存中删除指定序列号的消息
// 用于消息撤回、删除等场景的缓存清理，保证缓存数据一致性
// ctx: 上下文
// conversationID: 会话ID
// seqs: 要删除的消息序列号列表
// 返回: 错误信息
func (db *msgTransferDatabase) DeleteMessagesFromCache(ctx context.Context, conversationID string, seqs []int64) error {
	return db.msgCache.DelMessageBySeqs(ctx, conversationID, seqs)
}

// BatchInsertChat2Cache 批量插入聊天消息到缓存
// 分配序列号并将消息批量插入Redis缓存，提供快速读取能力
// 支持新会话检测和用户已读状态管理
// ctx: 上下文
// conversationID: 会话ID
// msgs: 待缓存的消息列表
// 返回: 最后分配的序列号、是否为新会话、用户已读映射、错误信息
func (db *msgTransferDatabase) BatchInsertChat2Cache(ctx context.Context, conversationID string, msgs []*sdkws.MsgData) (seq int64, isNew bool, userHasReadMap map[string]int64, err error) {
	lenList := len(msgs)

	// 验证消息数量限制
	if int64(lenList) > db.msgTable.GetSingleGocMsgNum() {
		return 0, false, nil, errs.New("message count exceeds limit", "limit", db.msgTable.GetSingleGocMsgNum()).Wrap()
	}
	if lenList < 1 {
		return 0, false, nil, errs.New("no messages to insert", "minCount", 1).Wrap()
	}

	// 从序列号分配器获取连续的序列号
	currentMaxSeq, err := db.seqConversation.Malloc(ctx, conversationID, int64(len(msgs)))
	if err != nil {
		log.ZError(ctx, "storage.seq.Malloc", err)
		return 0, false, nil, err
	}

	// 判断是否为新会话（序列号为0表示新会话）
	isNew = currentMaxSeq == 0
	lastMaxSeq := currentMaxSeq

	// 为每条消息分配序列号并记录用户已读状态
	userSeqMap := make(map[string]int64)
	seqs := make([]int64, 0, lenList)
	for _, m := range msgs {
		currentMaxSeq++
		m.Seq = currentMaxSeq        // 设置消息序列号
		userSeqMap[m.SendID] = m.Seq // 记录发送者的已读序列号
		seqs = append(seqs, m.Seq)
	}

	// 消息格式转换：protobuf -> 数据库模型
	msgToDB := func(msg *sdkws.MsgData) *model.MsgInfoModel {
		return &model.MsgInfoModel{
			Msg: convert.MsgPb2DB(msg),
		}
	}

	// 批量插入消息到Redis缓存
	if err := db.msgCache.SetMessageBySeqs(ctx, conversationID, datautil.Slice(msgs, msgToDB)); err != nil {
		return 0, false, nil, err
	}

	return lastMaxSeq, isNew, userSeqMap, nil
}

// SetHasReadSeqs 设置用户已读序列号到缓存
// 更新用户在指定会话中的已读状态，用于消息已读回执和未读计数
// ctx: 上下文
// conversationID: 会话ID
// userSeqMap: 用户ID到已读序列号的映射
// 返回: 错误信息
func (db *msgTransferDatabase) SetHasReadSeqs(ctx context.Context, conversationID string, userSeqMap map[string]int64) error {
	for userID, seq := range userSeqMap {
		if err := db.seqUser.SetUserReadSeq(ctx, conversationID, userID, seq); err != nil {
			return err
		}
	}
	return nil
}

// SetHasReadSeqToDB 设置用户已读序列号到数据库
// 将已读状态持久化到数据库，保证数据可靠性和跨设备同步
// ctx: 上下文
// conversationID: 会话ID
// userSeqMap: 用户ID到已读序列号的映射
// 返回: 错误信息
func (db *msgTransferDatabase) SetHasReadSeqToDB(ctx context.Context, conversationID string, userSeqMap map[string]int64) error {
	for userID, seq := range userSeqMap {
		if err := db.seqUser.SetUserReadSeqToDB(ctx, conversationID, userID, seq); err != nil {
			return err
		}
	}
	return nil
}

// MsgToPushMQ 发送消息到推送队列
// 将消息发送到Kafka推送主题，用于实时消息推送到客户端
// 支持负载均衡和消息路由，保证推送的可靠性和性能
// ctx: 上下文
// key: Kafka分区键，用于消息路由和负载均衡
// conversationID: 会话ID
// msg2mq: 待推送的消息数据
// 返回: Kafka分区号、偏移量、错误信息
func (db *msgTransferDatabase) MsgToPushMQ(ctx context.Context, key, conversationID string, msg2mq *sdkws.MsgData) (int32, int64, error) {
	// 发送消息到推送队列，包装消息数据和会话ID
	partition, offset, err := db.producerToPush.SendMessage(ctx, key, &pbmsg.PushMsgDataToMQ{
		MsgData:        msg2mq,
		ConversationID: conversationID,
	})
	if err != nil {
		log.ZError(ctx, "MsgToPushMQ", err, "key", key, "msg2mq", msg2mq)
		return 0, 0, err
	}
	return partition, offset, nil
}

// MsgToMongoMQ 发送消息到MongoDB持久化队列
// 将消息发送到Kafka MongoDB主题，用于异步持久化存储
// 支持批量消息处理，提高存储效率
// ctx: 上下文
// key: Kafka分区键
// conversationID: 会话ID
// messages: 待持久化的消息列表
// lastSeq: 最后一条消息的序列号，用于数据一致性检查
// 返回: 错误信息
func (db *msgTransferDatabase) MsgToMongoMQ(ctx context.Context, key, conversationID string, messages []*sdkws.MsgData, lastSeq int64) error {
	if len(messages) > 0 {
		// 发送批量消息到MongoDB持久化队列
		_, _, err := db.producerToMongo.SendMessage(ctx, key, &pbmsg.MsgDataToMongoByMQ{
			LastSeq:        lastSeq,
			ConversationID: conversationID,
			MsgData:        messages,
		})
		if err != nil {
			log.ZError(ctx, "MsgToMongoMQ", err, "key", key, "conversationID", conversationID, "lastSeq", lastSeq)
			return err
		}
	}
	return nil
}
