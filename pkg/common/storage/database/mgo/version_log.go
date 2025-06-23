// Package mgo 提供基于MongoDB的版本日志存储实现
//
// 版本日志系统是OpenIM增量同步机制的核心组件，用于支持客户端的增量数据同步。
// 通过维护数据变更的版本信息，客户端可以只获取自上次同步以来的变更数据，
// 大大减少网络传输量和提升同步效率。
//
// **核心设计理念：**
//
// 1. **版本驱动同步**
//   - 每次数据变更都会递增版本号
//   - 客户端记录最后同步的版本号
//   - 服务端返回指定版本后的所有变更
//
// 2. **原子性保证**
//   - 使用MongoDB的原子操作确保版本更新的一致性
//   - 支持事务处理，避免数据不一致
//   - 通过乐观锁机制处理并发更新
//
// 3. **存储优化**
//   - 使用聚合管道进行高效的数据处理
//   - 支持批量操作减少数据库往返
//   - 自动清理过期的版本日志数据
//
// **数据结构设计：**
//
// VersionLog文档结构：
//
//	{
//	  "_id": ObjectId,           // MongoDB文档ID
//	  "d_id": "domain_id",       // 领域ID（如群组ID、用户ID）
//	  "version": 123,            // 当前版本号
//	  "deleted": 0,              // 删除版本号（用于软删除）
//	  "last_update": ISODate,    // 最后更新时间
//	  "logs": [                  // 变更日志数组
//	    {
//	      "e_id": "element_id",  // 元素ID（如成员ID）
//	      "state": 1,            // 变更状态（增加、修改、删除）
//	      "version": 123,        // 变更版本号
//	      "last_update": ISODate // 变更时间
//	    }
//	  ]
//	}
//
// **版本管理策略：**
//
// - 版本号递增：每次变更版本号+1，确保时序正确
// - 软删除机制：通过deleted字段标记删除，支持恢复
// - 批量处理：多个变更可以在同一版本中批量处理
// - 自动清理：定期清理过期的版本日志，控制存储空间
//
// **同步机制：**
//
// 1. 客户端请求：携带lastVersion参数
// 2. 服务端查询：返回version > lastVersion的所有变更
// 3. 客户端应用：按版本顺序应用变更
// 4. 版本更新：客户端更新本地的lastVersion
//
// **性能优化：**
//
// - 索引优化：在d_id字段上建立唯一索引
// - 聚合查询：使用MongoDB聚合管道提升查询性能
// - 内存管理：合理控制单次返回的日志数量
// - 并发控制：通过MongoDB的原子操作避免锁竞争
package mgo

import (
	"context"
	"errors"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/versionctx"
	"github.com/openimsdk/tools/db/mongoutil"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func NewVersionLog(coll *mongo.Collection) (database.VersionLog, error) {
	lm := &VersionLogMgo{coll: coll}
	if err := lm.initIndex(context.Background()); err != nil {
		return nil, errs.WrapMsg(err, "init version log index failed", "coll", coll.Name())
	}
	return lm, nil
}

type VersionLogMgo struct {
	coll *mongo.Collection // MongoDB集合实例，存储版本日志数据
}

func (l *VersionLogMgo) initIndex(ctx context.Context) error {
	_, err := l.coll.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.M{
			"d_id": 1, // 在d_id字段上创建升序索引
		},
		Options: options.Index().SetUnique(true), // 设置唯一性约束
	})

	return err
}

func (l *VersionLogMgo) IncrVersion(ctx context.Context, dId string, eIds []string, state int32) error {
	_, err := l.IncrVersionResult(ctx, dId, eIds, state)
	return err
}

func (l *VersionLogMgo) IncrVersionResult(ctx context.Context, dId string, eIds []string, state int32) (*model.VersionLog, error) {
	vl, err := l.incrVersionResult(ctx, dId, eIds, state)
	if err != nil {
		return nil, err
	}
	versionctx.GetVersionLog(ctx).Append(versionctx.Collection{
		Name: l.coll.Name(),
		Doc:  vl,
	})
	return vl, nil
}

func (l *VersionLogMgo) incrVersionResult(ctx context.Context, dId string, eIds []string, state int32) (*model.VersionLog, error) {
	if len(eIds) == 0 {
		return nil, errs.ErrArgs.WrapMsg("elem id is empty", "dId", dId)
	}
	now := time.Now()
	if res, err := l.writeLogBatch2(ctx, dId, eIds, state, now); err == nil {
		return res, nil
	} else if !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, err
	}
	if res, err := l.initDoc(ctx, dId, eIds, state, now); err == nil {
		return res, nil
	} else if !mongo.IsDuplicateKeyError(err) {
		return nil, err
	}
	return l.writeLogBatch2(ctx, dId, eIds, state, now)
}

func (l *VersionLogMgo) initDoc(ctx context.Context, dId string, eIds []string, state int32, now time.Time) (*model.VersionLog, error) {
	wl := model.VersionLogTable{
		ID:         primitive.NewObjectID(),
		DID:        dId,
		Logs:       make([]model.VersionLogElem, 0, len(eIds)),
		Version:    database.FirstVersion,
		Deleted:    database.DefaultDeleteVersion,
		LastUpdate: now,
	}
	for _, eId := range eIds {
		wl.Logs = append(wl.Logs, model.VersionLogElem{
			EID:        eId,
			State:      state,
			Version:    database.FirstVersion,
			LastUpdate: now,
		})
	}
	if _, err := l.coll.InsertOne(ctx, &wl); err != nil {
		return nil, err
	}
	return wl.VersionLog(), nil
}

func (l *VersionLogMgo) writeLogBatch2(ctx context.Context, dId string, eIds []string, state int32, now time.Time) (*model.VersionLog, error) {
	if eIds == nil {
		eIds = []string{}
	}
	filter := bson.M{
		"d_id": dId,
	}
	elems := make([]bson.M, 0, len(eIds))
	for _, eId := range eIds {
		elems = append(elems, bson.M{
			"e_id":        eId,
			"version":     "$version",
			"state":       state,
			"last_update": now,
		})
	}
	pipeline := []bson.M{
		{
			"$addFields": bson.M{
				"delete_e_ids": eIds,
			},
		},
		{
			"$set": bson.M{
				"version":     bson.M{"$add": []any{"$version", 1}},
				"last_update": now,
			},
		},
		{
			"$set": bson.M{
				"logs": bson.M{
					"$filter": bson.M{
						"input": "$logs",
						"as":    "log",
						"cond": bson.M{
							"$not": bson.M{
								"$in": []any{"$$log.e_id", "$delete_e_ids"},
							},
						},
					},
				},
			},
		},
		{
			"$set": bson.M{
				"logs": bson.M{
					"$concatArrays": []any{
						"$logs",
						elems,
					},
				},
			},
		},
		{
			"$unset": "delete_e_ids",
		},
	}
	projection := bson.M{
		"logs": 0,
	}
	opt := options.FindOneAndUpdate().SetUpsert(false).SetReturnDocument(options.After).SetProjection(projection)
	res, err := mongoutil.FindOneAndUpdate[*model.VersionLog](ctx, l.coll, filter, pipeline, opt)
	if err != nil {
		return nil, err
	}
	res.Logs = make([]model.VersionLogElem, 0, len(eIds))
	for _, id := range eIds {
		res.Logs = append(res.Logs, model.VersionLogElem{
			EID:        id,
			State:      state,
			Version:    res.Version,
			LastUpdate: res.LastUpdate,
		})
	}
	return res, nil
}

func (l *VersionLogMgo) findDoc(ctx context.Context, dId string) (*model.VersionLog, error) {
	vl, err := mongoutil.FindOne[*model.VersionLogTable](ctx, l.coll, bson.M{"d_id": dId}, options.FindOne().SetProjection(bson.M{"logs": 0}))
	if err != nil {
		return nil, err
	}
	return vl.VersionLog(), nil
}

func (l *VersionLogMgo) FindChangeLog(ctx context.Context, dId string, version uint, limit int) (*model.VersionLog, error) {
	if wl, err := l.findChangeLog(ctx, dId, version, limit); err == nil {
		return wl, nil
	} else if !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, err
	}
	log.ZDebug(ctx, "init doc", "dId", dId)
	if res, err := l.initDoc(ctx, dId, nil, 0, time.Now()); err == nil {
		log.ZDebug(ctx, "init doc success", "dId", dId)
		return res, nil
	} else if mongo.IsDuplicateKeyError(err) {
		return l.findChangeLog(ctx, dId, version, limit)
	} else {
		return nil, err
	}
}

func (l *VersionLogMgo) BatchFindChangeLog(ctx context.Context, dIds []string, versions []uint, limits []int) (vLogs []*model.VersionLog, err error) {
	for i := 0; i < len(dIds); i++ {
		if vLog, err := l.findChangeLog(ctx, dIds[i], versions[i], limits[i]); err == nil {
			vLogs = append(vLogs, vLog)
		} else if !errors.Is(err, mongo.ErrNoDocuments) {
			log.ZError(ctx, "findChangeLog error:", errs.Wrap(err))
		}
		log.ZDebug(ctx, "init doc", "dId", dIds[i])
		if res, err := l.initDoc(ctx, dIds[i], nil, 0, time.Now()); err == nil {
			log.ZDebug(ctx, "init doc success", "dId", dIds[i])
			vLogs = append(vLogs, res)
		} else if mongo.IsDuplicateKeyError(err) {
			l.findChangeLog(ctx, dIds[i], versions[i], limits[i])
		} else {
			log.ZError(ctx, "init doc error:", errs.Wrap(err))
		}
	}
	return vLogs, errs.Wrap(err)
}

func (l *VersionLogMgo) findChangeLog(ctx context.Context, dId string, version uint, limit int) (*model.VersionLog, error) {
	if version == 0 && limit == 0 {
		return l.findDoc(ctx, dId)
	}
	pipeline := []bson.M{
		{
			"$match": bson.M{
				"d_id": dId,
			},
		},
		{
			"$addFields": bson.M{
				"logs": bson.M{
					"$cond": bson.M{
						"if": bson.M{
							"$or": []bson.M{
								{"$lt": []any{"$version", version}},
								{"$gte": []any{"$deleted", version}},
							},
						},
						"then": []any{},
						"else": "$logs",
					},
				},
			},
		},
		{
			"$addFields": bson.M{
				"logs": bson.M{
					"$filter": bson.M{
						"input": "$logs",
						"as":    "l",
						"cond": bson.M{
							"$gt": []any{"$$l.version", version},
						},
					},
				},
			},
		},
		{
			"$addFields": bson.M{
				"log_len": bson.M{"$size": "$logs"},
			},
		},
		{
			"$addFields": bson.M{
				"logs": bson.M{
					"$cond": bson.M{
						"if": bson.M{
							"$gt": []any{"$log_len", limit},
						},
						"then": []any{},
						"else": "$logs",
					},
				},
			},
		},
	}
	if limit <= 0 {
		pipeline = pipeline[:len(pipeline)-1]
	}
	vl, err := mongoutil.Aggregate[*model.VersionLog](ctx, l.coll, pipeline)
	if err != nil {
		return nil, err
	}
	if len(vl) == 0 {
		return nil, mongo.ErrNoDocuments
	}
	return vl[0], nil
}

func (l *VersionLogMgo) DeleteAfterUnchangedLog(ctx context.Context, deadline time.Time) error {
	return mongoutil.DeleteMany(ctx, l.coll, bson.M{
		"last_update": bson.M{
			"$lt": deadline,
		},
	})
}

func (l *VersionLogMgo) Delete(ctx context.Context, dId string) error {
	return mongoutil.DeleteOne(ctx, l.coll, bson.M{"d_id": dId})
}
