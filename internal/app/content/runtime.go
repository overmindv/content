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
	"google.golang.org/grpc/credentials"
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
	if err := validateCatalogConfig(cfg); err != nil {
		return err
	}
	activityTopics, activityErr := cfg.Kafka.ActivityTopics()
	if activityErr != nil {
		return activityErr
	}
	reviewSource, sourceErr := reviewCandidateSource(cfg)
	if sourceErr != nil {
		return sourceErr
	}

	var topics service.TopicChecker
	if cfg.EntitiesURL != "" {
		client, err := external.NewTopicClient(cfg.EntitiesURL)
		if err != nil {
			return err
		}
		topics = client
	}

	var media *external.MediaClient
	if cfg.MediaURL != "" || cfg.MediaToken != "" {
		if cfg.MediaToken != "" && cfg.MediaToken == cfg.GRPC.ServiceToken {
			return errors.New("media bindings require a separate service token")
		}
		client, err := external.NewMediaClient(cfg.MediaURL, cfg.MediaToken)
		if err != nil {
			return err
		}
		media = client
	}

	pool, err := app.Postgres() // добавляет health-чек "postgres" в /ready
	if err != nil {
		return err
	}

	// События контента пишутся хранилищем в outbox атомарно с бизнес-изменением.
	options := []postgres.StoreOption{
		postgres.WithOutbox(cfg.Kafka.Enabled, cfg.Kafka.Topic), postgres.WithManagedAttachments(),
		postgres.WithMaterialActivity(activityTopics.Material, activityTopics.Learning),
		postgres.WithEditorialActivity(activityTopics.Contribution),
	}
	if reviewSource != nil {
		options = append(options, postgres.WithReviewCheckScheduling())
	}
	store := postgres.NewContentItemStore(pool, options...)
	if reviewSource != nil {
		worker := service.NewReviewCheckWorker(store, reviewSource)
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
		outbox := publishingOutbox{parker.NewPgOutbox(pool)}
		dispatcher := parker.NewOutboxDispatcher(outbox, producer, cfg.Kafka.PollInterval, app.Logger())
		app.AddRunnable("kafka-outbox", dispatcher.Run)
	}

	items, proposals := service.NewContentItemService(store, topics), service.NewChangeProposalService(store)
	if media != nil {
		items.SetAttachmentChecker(media)
		items.SetPublishedAttachmentSigner(media)
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

type claimedOutbox interface {
	parker.Outbox
	MarkClaimSent(context.Context, parker.OutboxRecord) error
	MarkClaimFailed(context.Context, parker.OutboxRecord) error
}

// Ограничиваем воркер публикацией, чтобы обновление Parker не включало очистку outbox.
type publishingOutbox struct{ claimedOutbox }

func reviewCandidateSource(cfg config.Config) (*external.ReviewCandidateClient, error) {
	source := cfg.ReviewCandidates
	if source.URL == "" && source.Token == "" {
		return nil, nil
	}
	if source.Token != "" && (source.Token == cfg.GRPC.ServiceToken ||
		source.Token == cfg.GRPC.ModerationToken || source.Token == cfg.MediaToken) {
		return nil, errors.New("review candidates require a distinct service token")
	}

	return external.NewReviewCandidateClient(source.URL, source.Token)
}

func validateModerationConfig(cfg config.Config) error {
	token := cfg.GRPC.ModerationToken
	if token == "" {
		return nil
	}
	if len(token) < 32 || token == cfg.GRPC.ServiceToken || token == cfg.MediaToken {
		return errors.New("CONTENT_GRPC_MODERATION_TOKEN requires a distinct token of at least 32 bytes")
	}
	if !cfg.Kafka.Enabled || !cfg.Kafka.Activity.Enabled {
		return errors.New("material moderation requires Kafka material activity")
	}

	return nil
}

func validateCatalogConfig(cfg config.Config) error {
	token := cfg.GRPC.CatalogToken
	if token == "" {
		return nil
	}
	if len(token) < 32 || len(token) > 256 || token == cfg.GRPC.ServiceToken ||
		token == cfg.GRPC.ModerationToken || token == cfg.MediaToken || token == cfg.ReviewCandidates.Token ||
		strings.ContainsFunc(token, func(r rune) bool { return r <= 32 || r >= 127 }) {
		return errors.New("CONTENT_GRPC_CATALOG_TOKEN must be a separate printable service token")
	}

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

	grpcServer := newGRPCServer(handler, cfg, options...)

	go func() {
		<-ctx.Done()
		grpcServer.GracefulStop()
	}()

	logger.Info("grpc server listening", "addr", cfg.Addr)

	return grpcServer.Serve(listener)
}

func newGRPCServer(handler *Server, cfg config.GRPCConfig, extra ...grpc.ServerOption) *grpc.Server {
	options := append(make([]grpc.ServerOption, 0, 2+len(extra)),
		grpc.MaxRecvMsgSize(512*1024),
		grpc.UnaryInterceptor(requireServiceActor(cfg)),
	)
	server := grpc.NewServer(append(options, extra...)...)
	contentpb.RegisterContentServiceServer(server, handler)
	contentpb.RegisterMaterialCatalogServiceServer(server, &materialCatalogServer{items: handler.itemService})
	reflection.Register(server)

	return server
}
