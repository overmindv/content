package content

import (
	"context"
	"log/slog"
	"net"

	"github.com/overmindv/content/internal/config"
	"github.com/overmindv/content/internal/pkg/service"
	"github.com/overmindv/content/internal/pkg/service/external"
	"github.com/overmindv/content/internal/pkg/store/postgres"
	contentpb "github.com/overmindv/content/pkg/api/content"
	"github.com/overmindv/parker"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
)

// Build выполняет wiring бизнес-зависимостей content на каркас parker:
// открывает базу, настраивает Kafka-producer, transaction outbox и gRPC-сервер
// (как Runnable), регистрирует health-чеки. HTTP/middleware/метрики/миграции
// берёт на себя parker.
func Build(app *parker.App) error {
	cfg := config.Load()
	var topics service.TopicChecker
	if cfg.EntitiesURL != "" {
		client, err := external.NewTopicClient(cfg.EntitiesURL)
		if err != nil {
			return err
		}
		topics = client
	}

	pool, err := app.Postgres() // добавляет health-чек "postgres" в /ready
	if err != nil {
		return err
	}

	// События контента пишутся хранилищем в outbox атомарно с бизнес-изменением.
	store := postgres.NewContentItemStore(pool,
		postgres.WithOutbox(cfg.Kafka.Enabled, cfg.Kafka.Topic))

	// Включённый Kafka-producer регистрируется как зависимость /ready.
	var producer *parker.Producer
	if cfg.Kafka.Enabled {
		producer, err = app.NewProducer()
		if err != nil {
			return err
		}
		app.AddHealthCheck("kafka", parker.HealthCheckFunc(producer.Ping))
		// Outbox-диспетчер доставляет события контента в Kafka at-least-once.
		dispatcher := parker.NewOutboxDispatcher(parker.NewPgOutbox(pool), producer, cfg.Kafka.PollInterval, app.Logger())
		app.AddRunnable("kafka-outbox", dispatcher.Run)
	}

	handler := NewServer(service.NewContentItemService(store, topics), service.NewChangeProposalService(store), service.NewChangeReviewService(store))

	// gRPC — бизнес-транспорт content, вне скоупа parker, поэтому как Runnable.
	app.AddRunnable("grpc", func(ctx context.Context) error {
		return runGRPC(ctx, cfg.GRPC, handler, app.Logger())
	})

	return nil
}

func runGRPC(ctx context.Context, cfg config.GRPCConfig, handler *Server, logger *slog.Logger) error {
	tlsConfig, err := loadGRPCTLS(cfg)
	if err != nil {
		return err
	}
	var options []grpc.ServerOption
	if tlsConfig != nil {
		options = append(options, grpc.Creds(credentials.NewTLS(tlsConfig)))
	}
	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}

	grpcServer := newGRPCServer(handler, cfg.ServiceToken, options...)

	go func() {
		<-ctx.Done()
		grpcServer.GracefulStop()
	}()

	logger.Info("grpc server listening", "addr", cfg.Addr)

	return grpcServer.Serve(listener)
}

func newGRPCServer(handler *Server, serviceToken string, extra ...grpc.ServerOption) *grpc.Server {
	options := append(make([]grpc.ServerOption, 0, 2+len(extra)),
		grpc.MaxRecvMsgSize(512*1024),
		grpc.UnaryInterceptor(requireServiceActor(serviceToken)),
	)
	server := grpc.NewServer(append(options, extra...)...)
	contentpb.RegisterContentServiceServer(server, handler)
	reflection.Register(server)

	return server
}
