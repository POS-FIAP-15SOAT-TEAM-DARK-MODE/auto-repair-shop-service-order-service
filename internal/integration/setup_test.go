//go:build integration

// Package integration runs the Service Order in process against a real PostgreSQL and MiniStack
// (SNS/SQS), with Customer and Catalog simulated by httptest. The test plays Billing and Execution:
// it reads the commands in the test queues and publishes the replies with the same saga_id.
//
// Run by the CI job integration-tests (go test -tags=integration ./internal/integration/...).
package integration

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/app"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/app/bootstrap"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/messaging"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

const (
	awsPrefix     = "auto-repair-shop-local" // physical names (contracts/resources.md)
	internalToken = "integration-internal-token"
	awaitTimeout  = 15 * time.Second
)

// rig is the environment shared by the scenarios. It is built once in TestMain.
type rig struct {
	baseURL   string
	httpc     *http.Client
	db        *sql.DB
	publisher messaging.Publisher
	customers *customerStub
	catalog   *catalogStub
	topics    map[string]string // logical name -> topic ARN (billing-events, execution-events)

	billingCmds   *mailbox // test-billing-commands
	executionCmds *mailbox // test-execution-commands
	catalogCmds   *mailbox // test-catalog-commands
	soEvents      *mailbox // test-so-events
}

var it *rig

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	if os.Getenv("AWS_ENDPOINT_URL") == "" || os.Getenv("POSTGRES_HOST") == "" {
		fmt.Fprintln(os.Stderr, "integration tests need POSTGRES_* and AWS_ENDPOINT_URL (MiniStack): they run in the CI job integration-tests")
		return 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop, err := setup(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "integration setup:", err)
		return 1
	}
	defer stop()
	return m.Run()
}

// setup starts the stubs and the application (bootstrap.Servers) in process.
func setup(ctx context.Context) (func(), error) {
	customers := newCustomerStub()
	catalog := newCatalogStub()
	customerSrv := httptest.NewServer(customers.handler())
	catalogSrv := httptest.NewServer(catalog.handler())

	snsClient, err := messaging.NewSNSClient(ctx)
	if err != nil {
		return nil, err
	}
	sqsClient, err := messaging.NewSQSClient(ctx)
	if err != nil {
		return nil, err
	}

	topicARN := func(name string) (string, error) {
		out, err := snsClient.CreateTopic(ctx, &sns.CreateTopicInput{Name: aws.String(awsPrefix + "-" + name)})
		if err != nil {
			return "", err
		}
		return aws.ToString(out.TopicArn), nil
	}
	queueURL := func(name string) (string, error) {
		out, err := sqsClient.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{QueueName: aws.String(awsPrefix + "-" + name)})
		if err != nil {
			return "", err
		}
		return aws.ToString(out.QueueUrl), nil
	}

	commandsARN, err := topicARN("saga-commands")
	if err != nil {
		return nil, err
	}
	soEventsARN, err := topicARN("so-events")
	if err != nil {
		return nil, err
	}
	billingEventsARN, err := topicARN("billing-events")
	if err != nil {
		return nil, err
	}
	executionEventsARN, err := topicARN("execution-events")
	if err != nil {
		return nil, err
	}
	repliesURL, err := queueURL("so-saga-replies")
	if err != nil {
		return nil, err
	}
	port, err := freePort()
	if err != nil {
		return nil, err
	}

	// Configuration read by bootstrap.Servers (the CI job does not set these).
	env := map[string]string{
		"CUSTOMER_URL":              customerSrv.URL,
		"CATALOG_URL":               catalogSrv.URL,
		"INTERNAL_TOKEN":            internalToken,
		"PORT":                      strconv.Itoa(port),
		"SO_SAGA_REPLIES_QUEUE_URL": repliesURL,
		"SAGA_COMMANDS_TOPIC_ARN":   commandsARN,
		"SO_EVENTS_TOPIC_ARN":       soEventsARN,
	}
	for k, v := range env {
		if err := os.Setenv(k, v); err != nil {
			return nil, err
		}
	}

	servers := bootstrap.Servers()
	for _, s := range servers {
		go func() {
			if err := s.Start(ctx); err != nil {
				fmt.Fprintln(os.Stderr, s.Name(), "stopped:", err)
			}
		}()
	}

	it = &rig{
		baseURL:   fmt.Sprintf("http://127.0.0.1:%d", port),
		httpc:     &http.Client{Timeout: 10 * time.Second},
		db:        app.DBConnect(),
		publisher: messaging.NewSNSPublisher(snsClient),
		customers: customers,
		catalog:   catalog,
		topics: map[string]string{
			"billing-events":   billingEventsARN,
			"execution-events": executionEventsARN,
		},
	}
	it.billingCmds = newMailbox(sqsClient, "test-billing-commands")
	it.executionCmds = newMailbox(sqsClient, "test-execution-commands")
	it.catalogCmds = newMailbox(sqsClient, "test-catalog-commands")
	it.soEvents = newMailbox(sqsClient, "test-so-events")
	for _, mb := range []*mailbox{it.billingCmds, it.executionCmds, it.catalogCmds, it.soEvents} {
		if err := mb.resolve(queueURL); err != nil {
			return nil, err
		}
	}

	if err := waitHTTP(it.baseURL+"/ping", awaitTimeout); err != nil {
		return nil, fmt.Errorf("application did not start: %w", err)
	}

	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, s := range servers {
			_ = s.Stop(shutdownCtx)
		}
		customerSrv.Close()
		catalogSrv.Close()
	}, nil
}

// freePort asks the kernel for an unused TCP port.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func waitHTTP(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		resp, err := http.Get(url) //nolint:noctx // test helper
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("GET %s: %v", url, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
