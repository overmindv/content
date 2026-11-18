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
	if err := validateModerationConfig(cfg); err != nil {
		return err
	}
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
	options := []postgres.StoreOption{postgres.WithOutbox(cfg.Kafka.Enabled, cfg.Kafka.Topic), postgres.WithManagedAttachments()}
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

	items, proposals := service.NewContentItemService(store, topics), service.NewChangeProposalService(store)
	if cfg.MediaURL != "" || cfg.MediaToken != "" {
		if cfg.MediaToken != "" && (cfg.MediaToken == cfg.GRPC.ServiceToken || cfg.MediaToken == cfg.ReviewCandidatesToken) {
			return errors.New("Media bindings require a separate service token")
		}
		media, err := external.NewMediaClient(cfg.MediaURL, cfg.MediaToken)
		if err != nil {
			return err
		}
		items.SetAttachmentChecker(media)
		proposals.SetAttachmentChecker(media)
		worker := service.NewMediaBindingWorker(store, media)
		app.AddRunnable("media-bindings", func(ctx context.Context) error { return worker.Run(ctx, app.Logger()) })
	}
	handler := NewServer(items, proposals, service.NewChangeReviewService(store))

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

func validateModerationConfig(cfg config.Config) error {
	token := cfg.GRPC.ModerationToken
	if token == "" {
		return nil
	}
	if len(token) < 32 || len(token) > 256 || token == cfg.GRPC.ServiceToken ||
		token == cfg.MediaToken || token == cfg.ReviewCandidatesToken ||
		strings.IndexFunc(token, func(r rune) bool { return r <= 32 || r >= 127 }) >= 0 {
		return errors.New("CONTENT_GRPC_MODERATION_TOKEN must be a separate printable service token")
	}
	if !cfg.Kafka.Enabled || !cfg.Kafka.ActivityEnabled {
		return errors.New("content moderation requires Kafka material activity")
	}
	return nil
}

func runGRPC(ctx context.Context, cfg config.GRPCConfig, handler *Server, logger *slog.Logger) error {
	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}

	grpcServer := newGRPCServer(handler, cfg.ServiceToken, cfg.ModerationToken)

	go func() {
		<-ctx.Done()
		grpcServer.GracefulStop()
	}()

	logger.Info("grpc server listening", "addr", cfg.Addr)
	return grpcServer.Serve(listener)
}

func newGRPCServer(handler *Server, serviceToken, moderationToken string) *grpc.Server {
	server := grpc.NewServer(
		grpc.MaxRecvMsgSize(512*1024),
		grpc.UnaryInterceptor(requireServiceActor(serviceToken, moderationToken)),
	)
	contentpb.RegisterContentServiceServer(server, handler)
	reflection.Register(server)
	return server
}
