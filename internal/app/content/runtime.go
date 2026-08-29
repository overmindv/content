package content

import (
	"context"
	"log/slog"
	"net"

	"github.com/overmindv/content/internal/config"
	contentpb "github.com/overmindv/content/internal/pkg/api/content"
	"github.com/overmindv/content/internal/pkg/kafka"
	"github.com/overmindv/content/internal/pkg/service"
	"github.com/overmindv/content/internal/pkg/store/postgres"
	"github.com/overmindv/parker"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// Build выполняет wiring бизнес-зависимостей content на каркас parker:
// открывает базу, настраивает Kafka-producer и gRPC-сервер (как Runnable),
// регистрирует health-чеки. HTTP/middleware/метрики/миграции берёт на себя parker.
func Build(app *parker.App) error {
	cfg := config.Load()

	pool, err := app.Postgres() // добавляет health-чек "postgres" в /ready
	if err != nil {
		return err
	}

	store := postgres.NewContentItemStore(pool)

	// Включённый Kafka-producer регистрируется как зависимость /ready.
	var producer *parker.Producer
	if cfg.Kafka.Enabled {
		producer, err = app.NewProducer()
		if err != nil {
			return err
		}
		app.AddHealthCheck("kafka", parker.HealthCheckFunc(producer.Ping))
	}

	publisher := kafka.NewPublisher(kafka.Config{
		Enabled: cfg.Kafka.Enabled,
		Topic:   cfg.Kafka.Topic,
	}, producer, app.Logger())

	itemService := service.NewContentItemService(store, publisher)

	// gRPC — бизнес-транспорт content, вне скоупа parker, поэтому как Runnable.
	app.AddRunnable("grpc", func(ctx context.Context) error {
		return runGRPC(ctx, cfg.GRPC.Addr, itemService, app.Logger())
	})

	return nil
}

func runGRPC(ctx context.Context, addr string, itemService *service.ContentItemService, logger *slog.Logger) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	grpcServer := grpc.NewServer()
	contentpb.RegisterContentServiceServer(grpcServer, NewServer(itemService))
	reflection.Register(grpcServer)

	go func() {
		<-ctx.Done()
		grpcServer.GracefulStop()
	}()

	logger.Info("grpc server listening", "addr", addr)
	return grpcServer.Serve(listener)
}
