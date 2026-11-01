#!/bin/sh
# Creates the messaging resources of service-order-service in MiniStack (idempotent).
# Physical names: auto-repair-shop-local-<logical name> (contracts/resources.md).
# Owned here: saga-commands, so-events, billing-events, execution-events, so-saga-replies,
# and the test queues that let the integration tests play Billing and Execution.
set -eu

PREFIX="auto-repair-shop-local"
REGION="${AWS_REGION:-us-east-1}"
ENDPOINT="${AWS_ENDPOINT_URL:-http://ministack:4566}"

aws_local() {
  aws --endpoint-url "$ENDPOINT" --region "$REGION" "$@"
}

# json_attrs <name> <value>: {"name":"value"} with the value's quotes escaped.
json_attrs() {
  escaped=$(printf '%s' "$2" | sed 's/"/\\"/g')
  printf '{"%s":"%s"}' "$1" "$escaped"
}

queue_arn() {
  aws_local sqs get-queue-attributes --queue-url "$1" --attribute-names QueueArn \
    --query Attributes.QueueArn --output text
}

# create_queue <logical name>: DLQ + queue with RedrivePolicy (maxReceiveCount=5). Prints the queue URL.
create_queue() {
  name="${PREFIX}-$1"
  aws_local sqs create-queue --queue-name "${name}-dlq" >/dev/null
  dlq_url=$(aws_local sqs get-queue-url --queue-name "${name}-dlq" --query QueueUrl --output text)
  dlq_arn=$(queue_arn "$dlq_url")
  redrive=$(printf '{"deadLetterTargetArn":"%s","maxReceiveCount":"5"}' "$dlq_arn")
  aws_local sqs create-queue --queue-name "$name" --attributes "$(json_attrs RedrivePolicy "$redrive")" >/dev/null
  aws_local sqs get-queue-url --queue-name "$name" --query QueueUrl --output text
}

# allow_topics <queue_url> <queue_arn> <topic_arn>...: queue policy that lets the topics send to the queue.
allow_topics() {
  queue_url=$1
  queue_arn=$2
  shift 2
  sources=""
  for topic in "$@"; do
    sources="${sources}${sources:+,}\"${topic}\""
  done
  policy="{\"Version\":\"2012-10-17\",\"Statement\":[{\"Effect\":\"Allow\",\"Principal\":{\"Service\":\"sns.amazonaws.com\"},\"Action\":\"sqs:SendMessage\",\"Resource\":\"${queue_arn}\",\"Condition\":{\"ArnEquals\":{\"aws:SourceArn\":[${sources}]}}}]}"
  aws_local sqs set-queue-attributes --queue-url "$queue_url" --attributes "$(json_attrs Policy "$policy")" \
    || echo "warn: could not set queue policy on ${queue_url}" >&2
}

# subscribe <topic_arn> <queue_arn> [target]: raw delivery; FilterPolicy on target when given.
subscribe() {
  if [ -n "${3:-}" ]; then
    filter=$(printf '{"target":["%s"]}' "$3" | sed 's/"/\\"/g')
    attrs="{\"RawMessageDelivery\":\"true\",\"FilterPolicyScope\":\"MessageAttributes\",\"FilterPolicy\":\"${filter}\"}"
  else
    attrs='{"RawMessageDelivery":"true"}'
  fi
  aws_local sns subscribe --topic-arn "$1" --protocol sqs --notification-endpoint "$2" --attributes "$attrs" >/dev/null \
    || echo "warn: could not subscribe ${2} to ${1}" >&2
}

# Topics
COMMANDS_ARN=$(aws_local sns create-topic --name "${PREFIX}-saga-commands" --query TopicArn --output text)
SO_EVENTS_ARN=$(aws_local sns create-topic --name "${PREFIX}-so-events" --query TopicArn --output text)
BILLING_EVENTS_ARN=$(aws_local sns create-topic --name "${PREFIX}-billing-events" --query TopicArn --output text)
EXECUTION_EVENTS_ARN=$(aws_local sns create-topic --name "${PREFIX}-execution-events" --query TopicArn --output text)

# Replies consumed by the service (billing and execution events, no filter).
SO_REPLIES_URL=$(create_queue so-saga-replies)
SO_REPLIES_ARN=$(queue_arn "$SO_REPLIES_URL")
allow_topics "$SO_REPLIES_URL" "$SO_REPLIES_ARN" "$BILLING_EVENTS_ARN" "$EXECUTION_EVENTS_ARN"
subscribe "$BILLING_EVENTS_ARN" "$SO_REPLIES_ARN"
subscribe "$EXECUTION_EVENTS_ARN" "$SO_REPLIES_ARN"

# Test queues: the integration tests read the commands that the service publishes.
for target in billing execution catalog; do
  test_url=$(create_queue "test-${target}-commands")
  test_arn=$(queue_arn "$test_url")
  allow_topics "$test_url" "$test_arn" "$COMMANDS_ARN"
  subscribe "$COMMANDS_ARN" "$test_arn" "$target"
done

echo "service-order messaging ready: replies=${SO_REPLIES_URL} commands=${COMMANDS_ARN} events=${SO_EVENTS_ARN}"
