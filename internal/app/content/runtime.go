package content

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync"

	"github.com/overmindv/content/internal/config"
	contentpb "github.com/overmindv/content/internal/pkg/api/content"
	"github.com/overmindv/content/internal/pkg/kafka"
	"github.com/overmindv/content/internal/pkg/metrics"
	"github.com/overmindv/content/internal/pkg/service"
	"github.com/overmindv/content/internal/pkg/store/postgres"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

type Runtime struct {
	cfg         config.Config
	logger      *slog.Logger
	db          *sql.DB
	publisher   *kafka.Publisher
	itemService *service.ContentItemService
}

func NewRuntime(cfg config.Config, logger *slog.Logger) (*Runtime, error) {
	db, err := postgres.Open(postgres.Config{
		DSN:             cfg.Database.DSN,
		MaxOpenConns:    cfg.Database.MaxOpenConns,
		MaxIdleConns:    cfg.Database.MaxIdleConns,
		ConnMaxLifetime: cfg.Database.ConnMaxLifetime,
	})
	if err != nil {
		return nil, err
	}

	registryMetrics := metrics.New(cfg.Metrics.Namespace, nil)
	registryMetrics.SetDependency("postgres", true)
	registryMetrics.SetDependency("kafka", cfg.Kafka.Enabled)
	store := postgres.NewContentItemStore(db)
	publisher := kafka.NewPublisher(kafka.Config{
		Enabled:  cfg.Kafka.Enabled,
		Brokers:  cfg.Kafka.Brokers,
		Topic:    cfg.Kafka.Topic,
		ClientID: cfg.Kafka.ClientID,
	}, logger)

	return &Runtime{
		cfg:         cfg,
		logger:      logger,
		db:          db,
		publisher:   publisher,
		itemService: service.NewContentItemService(store, publisher, registryMetrics),
	}, nil
}

func (r *Runtime) Run(ctx context.Context) error {
	httpServer := &http.Server{
		Addr:         r.cfg.HTTP.Addr,
		Handler:      NewHTTPHandler(r.cfg, r.itemService.Store()),
		ReadTimeout:  r.cfg.HTTP.ReadTimeout,
		WriteTimeout: r.cfg.HTTP.WriteTimeout,
	}

	grpcServer := grpc.NewServer()
	contentpb.RegisterContentServiceServer(grpcServer, NewServer(r.itemService))
	reflection.Register(grpcServer)

	grpcListener, err := net.Listen("tcp", r.cfg.GRPC.Addr)
	if err != nil {
		return err
	}
	defer func() {
		_ = grpcListener.Close()
	}()

	errCh := make(chan error, 2)
	var once sync.Once

	go func() {
		r.logger.Info("starting http server", "addr", r.cfg.HTTP.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			once.Do(func() { errCh <- err })
		}
	}()

	go func() {
		r.logger.Info("starting grpc server", "addr", r.cfg.GRPC.Addr)
		if err := grpcServer.Serve(grpcListener); err != nil {
			once.Do(func() { errCh <- err })
		}
	}()

	select {
	case <-ctx.Done():
		r.logger.Info("shutdown requested")
	case err := <-errCh:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), r.cfg.HTTP.ShutdownTimeout)
	defer cancel()

	grpcServer.GracefulStop()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return err
	}

	if err := r.publisher.Close(); err != nil {
		r.logger.Warn("failed to close kafka publisher", "error", err)
	}

	return r.db.Close()
}
