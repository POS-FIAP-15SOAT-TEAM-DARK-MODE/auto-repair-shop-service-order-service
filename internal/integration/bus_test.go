//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/messaging"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mailbox reads one test queue. Messages are deleted once read and kept in memory, filtered by
// service order id, so every scenario only looks at its own messages.
type mailbox struct {
	client *sqs.Client
	name   string // logical name, e.g. test-billing-commands
	url    string
	seen   []messaging.Envelope
}

func newMailbox(client *sqs.Client, name string) *mailbox {
	return &mailbox{client: client, name: name}
}

func (m *mailbox) resolve(queueURL func(string) (string, error)) error {
	url, err := queueURL(m.name)
	m.url = url
	return err
}

// pull long-polls the queue once (1 s) and stores what it receives.
func (m *mailbox) pull(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := m.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(m.url),
		MaxNumberOfMessages: 10,
		WaitTimeSeconds:     1,
	})
	require.NoError(t, err, m.name)
	for _, msg := range out.Messages {
		env, err := messaging.Decode([]byte(aws.ToString(msg.Body)))
		require.NoError(t, err, "%s: message body is not an envelope", m.name)
		m.seen = append(m.seen, env)
		_, err = m.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
			QueueUrl:      aws.String(m.url),
			ReceiptHandle: msg.ReceiptHandle,
		})
		require.NoError(t, err, m.name)
	}
}

// messagesOf returns the envelopes of the order, in the order they were read.
func (m *mailbox) messagesOf(soID string) []messaging.Envelope {
	var out []messaging.Envelope
	for _, e := range m.seen {
		if e.ServiceOrderID == soID {
			out = append(out, e)
		}
	}
	return out
}

// awaitTypes waits until the queue holds at least len(want) messages of the order, reads one more
// time to catch extras, and then compares the types exactly (as a multiset).
func (m *mailbox) awaitTypes(t *testing.T, soID string, want ...string) []messaging.Envelope {
	t.Helper()
	deadline := time.Now().Add(awaitTimeout)
	for len(m.messagesOf(soID)) < len(want) && time.Now().Before(deadline) {
		m.pull(t)
	}
	m.pull(t) // settle: a late duplicate or an unexpected command shows up here
	got := m.messagesOf(soID)
	types := make([]string, 0, len(got))
	for _, e := range got {
		types = append(types, e.Type)
	}
	assert.ElementsMatch(t, want, types, "commands read from %s for service order %s", m.name, soID)
	return got
}

// awaitMessage waits for the first message of type typ for the order and returns it.
func (m *mailbox) awaitMessage(t *testing.T, soID, typ string) messaging.Envelope {
	t.Helper()
	deadline := time.Now().Add(awaitTimeout)
	for {
		for _, e := range m.messagesOf(soID) {
			if e.Type == typ {
				return e
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no %s in %s for service order %s within %s", typ, m.name, soID, awaitTimeout)
		}
		m.pull(t)
	}
}

// publish sends an envelope to a topic with the type attribute, as the publishers do.
func publish(t *testing.T, topic string, env messaging.Envelope) {
	t.Helper()
	body, err := json.Marshal(env)
	require.NoError(t, err)
	require.NoError(t, it.publisher.Publish(context.Background(), it.topics[topic], string(body), map[string]string{"type": env.Type}))
}

// reply builds a reply of Billing or Execution to the saga of the order.
func reply(t *testing.T, sagaID, soID, typ string, payload any) messaging.Envelope {
	t.Helper()
	env, err := messaging.NewEnvelope(typ, sagaID, soID, "integration-request", payload)
	require.NoError(t, err)
	return env
}

// transition is the summary of a ServiceOrderStatusChanged: "previous->new[:reason]".
type transition struct {
	Previous *string `json:"previous"`
	New      string  `json:"new"`
	Reason   *string `json:"reason"`
	Total    string  `json:"total_amount"`
}

func (tr transition) String() string {
	prev := ""
	if tr.Previous != nil {
		prev = *tr.Previous
	}
	s := prev + "->" + tr.New
	if tr.Reason != nil {
		s += ":" + *tr.Reason
	}
	return s
}

// awaitTransitions waits for the status events of the order and compares them exactly.
func awaitTransitions(t *testing.T, soID string, want ...string) []transition {
	t.Helper()
	m := it.soEvents
	deadline := time.Now().Add(awaitTimeout)
	count := func() int { return len(m.messagesOf(soID)) }
	for count() < len(want) && time.Now().Before(deadline) {
		m.pull(t)
	}
	m.pull(t)

	var out []transition
	var got []string
	for _, e := range m.messagesOf(soID) {
		require.Equal(t, "ServiceOrderStatusChanged", e.Type)
		var tr transition
		require.NoError(t, json.Unmarshal(e.Payload, &tr))
		out = append(out, tr)
		got = append(got, tr.String())
	}
	assert.ElementsMatch(t, want, got, "status events of service order %s", soID)
	return out
}

// awaitSaga waits until the saga row has the step and status.
func awaitSaga(t *testing.T, sagaID, step, status string) {
	t.Helper()
	deadline := time.Now().Add(awaitTimeout)
	var gotStep, gotStatus string
	for {
		err := it.db.QueryRow("SELECT step, status FROM saga_instance WHERE id = $1", sagaID).Scan(&gotStep, &gotStatus)
		if err == nil && gotStep == step && gotStatus == status {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("saga %s: want step=%s status=%s, got step=%q status=%q (err=%v)", sagaID, step, status, gotStep, gotStatus, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// call performs an HTTP request on the application and returns the status and the body.
func call(t *testing.T, method, path, token string, body any) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, it.baseURL+path, reader)
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := it.httpc.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, out
}

// statusOf reads the public status endpoint.
func statusOf(t *testing.T, soID string) string {
	t.Helper()
	code, body := call(t, http.MethodGet, "/v1/service-order/"+soID+"/status", "", nil)
	require.Equal(t, http.StatusOK, code, string(body))
	var out struct {
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	return out.Status
}

// awaitStatus polls the status endpoint until the order reaches want.
func awaitStatus(t *testing.T, soID, want string) {
	t.Helper()
	deadline := time.Now().Add(awaitTimeout)
	got := ""
	for {
		got = statusOf(t, soID)
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("service order %s: want status %s, got %s", soID, want, got)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
