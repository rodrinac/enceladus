// Package store mirrors src/storage.py: the Redis-backed processing timestamps.
package store

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

const dateFormat = "02/01/2006 15:04:05"

// DataStore is the storage boundary; RedisStore is the production
// implementation and tests may fake it.
type DataStore interface {
	Save(relatorioID, nomeRelatorio string) error
	Dates() (map[string]string, error)
}

type RedisStore struct {
	Client *redis.Client
	DB     int
}

func NewRedisStore(client *redis.Client) *RedisStore {
	return &RedisStore{Client: client}
}

func (s *RedisStore) key(id, nome string) string {
	return "dataProcessamento." + id + "." + nome
}

func (s *RedisStore) Save(relatorioID, nomeRelatorio string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.Client.Set(ctx, s.key(relatorioID, nomeRelatorio), time.Now().Format(dateFormat), 0).Err()
}

func (s *RedisStore) Dates() (map[string]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	keys, err := s.Client.Keys(ctx, "dataProcessamento.*").Result()
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		value, getErr := s.Client.Get(ctx, key).Result()
		if getErr != nil {
			continue
		}
		out[key] = value
	}
	return out, nil
}
