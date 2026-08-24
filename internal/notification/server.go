package notification

import (
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// NewAsynqServer builds an asynq.Server sharing the app's existing Redis
// client, and NewAsynqMux wires it to the Worker's handler for TaskTypeSend.
func NewAsynqServer(rdb redis.UniversalClient, cfg asynq.Config) *asynq.Server {
	return asynq.NewServerFromRedisClient(rdb, cfg)
}

func NewAsynqClient(rdb redis.UniversalClient) *asynq.Client {
	return asynq.NewClientFromRedisClient(rdb)
}

func NewAsynqMux(worker *Worker) *asynq.ServeMux {
	mux := asynq.NewServeMux()
	mux.HandleFunc(TaskTypeSend, worker.HandleSend)
	return mux
}
