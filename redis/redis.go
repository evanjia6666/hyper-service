package wredis

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

const (
	ZSET_KEY  = "wallet_eoa_cache"
	PAGE_SIZE = 1000
)

func NewRedis(address string) *redis.Client {
	rdb := redis.NewClient(&redis.Options{
		Addr: "address",
	})
	return rdb
}

func QueryAddressFromZset(ctx context.Context, rdb *redis.Client, minScore, maxScore string) ([]string, string, error) {
	var allMembers []string

	min := minScore
	for {
		// 分页获取数据
		members, err := rdb.ZRangeByScore(ctx, ZSET_KEY, &redis.ZRangeBy{
			Min:    min,
			Max:    maxScore,
			Offset: 0,
			Count:  PAGE_SIZE,
		}).Result()
		if err != nil {
			return nil, "", err
		}
		if len(members) == 0 {
			break
		}
		allMembers = append(allMembers, members...)
		lastScore, err := rdb.ZScore(ctx, ZSET_KEY, members[len(members)-1]).Result()
		if err != nil {
			return nil, "", err
		}
		// 下次查询分数大于 lastScore 的数据
		min = fmt.Sprintf("(%f", lastScore)
	}

	return allMembers, min, nil
}

// ReadString from redis
func ReadString(ctx context.Context, rdb *redis.Client, key string) (string, error) {
	return rdb.Get(ctx, key).Result()
}

func WriteString(ctx context.Context, rdb *redis.Client, key, value string) error {
	return rdb.Set(ctx, key, value, 0).Err()
}
