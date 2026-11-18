package content

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"

	"github.com/overmindv/content/internal/config"
	"github.com/overmindv/content/internal/pkg/service"
	"github.com/overmindv/content/internal/pkg/service/external"
	"github.com/overmindv/content/internal/pkg/store/postgres"
	contentpb "github.com/overmindv/content/pkg/api/content"
	"github.com/overmindv/parker"
	"google.golang.org/grpc"
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
	options := []postgres.StoreOption{postgres.WithOutbox(cfg.Kafka.Enabled, cfg.Kafka.Topic)}
	if cfg.Kafka.Enabled && cfg.Kafka.ActivityEnabled {
		seen := map[string]bool{cfg.Kafka.Topic: true}
		for _, topic := range []string{cfg.Kafka.MaterialTopic, cfg.Kafka.ContributionTopic, cfg.Kafka.LearningTopic} {
			if !validActivityTopic(topic) || seen[topic] {
				return errors.New("content activity requires distinct valid Kafka topics")
			}
			seen[topic] = true
		}
		options = append(options, postgres.WithActivityOutbox(cfg.Kafka.MaterialTopic, cfg.Kafka.ContributionTopic, cfg.Kafka.LearningTopic))
	}
	store := postgres.NewContentItemStore(pool, options...)

	if cfg.ReviewCandidatesURL != "" || cfg.ReviewCandidatesToken != "" {
		if cfg.ReviewCandidatesToken == cfg.GRPC.ServiceToken && cfg.ReviewCandidatesToken != "" {
			return errors.New("review candidates and content gRPC must use different service tokens")
		}
		source, err := external.NewReviewCandidateClient(cfg.ReviewCandidatesURL, cfg.ReviewCandidatesToken)
		if err != nil {
			return err
		}
		worker := service.NewReviewCheckWorker(store, source)
		app.AddRunnable("review-candidates", func(ctx context.Context) error {
			return worker.Run(ctx, app.Logger())
		})
	}

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

func validActivityTopic(topic string) bool {
	return len(topic) > 0 && len(topic) <= 249 && topic != "." && topic != ".." &&
		strings.IndexFunc(topic, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-')
		}) < 0
}

func runGRPC(ctx context.Context, cfg config.GRPCConfig, handler *Server, logger *slog.Logger) error {
	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}

	grpcServer := newGRPCServer(handler, cfg.ServiceToken)

	go func() {
		<-ctx.Done()
		grpcServer.GracefulStop()
	}()

	logger.Info("grpc server listening", "addr", cfg.Addr)
	return grpcServer.Serve(listener)
}

func newGRPCServer(handler *Server, serviceToken string) *grpc.Server {
	server := grpc.NewServer(
		grpc.MaxRecvMsgSize(512*1024),
		grpc.UnaryInterceptor(requireServiceActor(serviceToken)),
	)
	contentpb.RegisterContentServiceServer(server, handler)
	reflection.Register(server)
	return server
}
