package bootstrap

import (
	"context"
	"database/sql"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/app"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/app/container"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/app/middleware"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/app/routing"
	catalogClient "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/clients/catalog"
	customerClient "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/clients/customer"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/env"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/logger"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/messaging"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/uow"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/saga"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/adapters"
	soRepository "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/repository"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// deadlineInterval is how often the saga deadlines are checked.
const deadlineInterval = 30 * time.Second

// Servers returns the HTTP API, the saga workers and, when configured, the outbox relay and the
// SQS consumer of the saga replies.
func Servers() []app.Server {
	database := app.DBConnect()
	internalToken := env.GetString("INTERNAL_TOKEN", "")
	unitOfWork := uow.NewTransactionalUoW(database)

	soRepo := soRepository.NewSOPostgres(database)
	sagaRepo := saga.NewPostgresRepository(database)
	customers := customerClient.New(env.GetString("CUSTOMER_URL", "http://customer-service:8081"), internalToken, 0)
	catalog := catalogClient.New(env.GetString("CATALOG_URL", "http://catalog-service:8082"), internalToken, 0)

	orch := saga.NewOrchestrator(unitOfWork, soRepo, sagaRepo, saga.PGOutbox{}, adapters.NewLogStatusNotifier(), saga.Config{
		StepTimeout:    env.GetTimeDuration("SAGA_STEP_TIMEOUT", 2*time.Minute),
		PaymentTimeout: env.GetTimeDuration("SAGA_PAYMENT_TIMEOUT", 24*time.Hour),
	})
	svc := service_order.NewService(unitOfWork, soRepo, customers, catalog, orch)
	ctrl := service_order.NewController(svc)

	router := routing.SetupRouter(middlewaresContainer(), func(r *gin.Engine) {
		service_order.RegisterRoutes(r, ctrl, internalToken)
	})

	servers := []app.Server{app.NewHTTPServer("http-api", ":"+env.GetString("PORT", "8086"), router)}
	servers = append(servers, newWorker("saga-deadlines", deadlineLoop(svc)))
	return append(servers, messagingWorkers(database, orch)...)
}

// deadlineLoop expires the saga steps whose deadline has passed.
func deadlineLoop(svc *service_order.Service) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(deadlineInterval)
		defer ticker.Stop()
		for {
			if n, err := svc.ExpireDeadlines(ctx); err != nil && ctx.Err() == nil {
				logger.Global().Error(err, zap.String("component", "saga-deadlines"))
			} else if n > 0 {
				logger.Global().Info("saga.deadlines_expired", zap.Int("count", n))
			}
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
			}
		}
	}
}

// messagingWorkers starts the relay and the consumer. They are disabled when the queue or topics
// are not configured.
func messagingWorkers(database *sql.DB, orch *saga.Orchestrator) []app.Server {
	queueURL := env.GetString("SO_SAGA_REPLIES_QUEUE_URL", "")
	commandsARN := env.GetString("SAGA_COMMANDS_TOPIC_ARN", "")
	eventsARN := env.GetString("SO_EVENTS_TOPIC_ARN", "")
	if queueURL == "" || commandsARN == "" || eventsARN == "" {
		logger.Global().Warn("messaging.disabled", zap.String("reason", "SO_SAGA_REPLIES_QUEUE_URL, SAGA_COMMANDS_TOPIC_ARN or SO_EVENTS_TOPIC_ARN is empty"))
		return nil
	}

	ctx := context.Background()
	snsClient, err := messaging.NewSNSClient(ctx)
	if err != nil {
		logger.Global().Fatal("failed to create SNS client", zap.Error(err))
	}
	sqsClient, err := messaging.NewSQSClient(ctx)
	if err != nil {
		logger.Global().Fatal("failed to create SQS client", zap.Error(err))
	}

	relay := messaging.NewRelay(database, messaging.NewSNSPublisher(snsClient), map[string]string{
		saga.TopicSagaCommands: commandsARN,
		saga.TopicSOEvents:     eventsARN,
	})
	consumer := messaging.NewConsumer(database, messaging.NewSQSQueue(sqsClient), queueURL)
	orch.RegisterReplies(consumer)

	return []app.Server{
		newWorker("outbox-relay", relay.Run),
		newWorker("sqs-consumer", consumer.Run),
	}
}

func middlewaresContainer() *container.Middlewares {
	return &container.Middlewares{
		"Logger":       middleware.Logger(),
		"Recovery":     middleware.Recovery(),
		"Metrics":      middleware.Metrics(),
		"ErrorHandler": middleware.ErrorHandler(),
	}
}
