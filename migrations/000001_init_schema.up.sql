CREATE TABLE service_order (
    id                    VARCHAR(26) PRIMARY KEY,
    status                VARCHAR(50) NOT NULL DEFAULT 'NEW',
    total_amount          NUMERIC(10, 2) NOT NULL DEFAULT 0,
    customer_id           VARCHAR(36) NOT NULL,
    customer_user_id      VARCHAR(36) NOT NULL,
    customer_name         VARCHAR(255) NOT NULL,
    customer_email        VARCHAR(255) NOT NULL,
    customer_type         VARCHAR(20) NOT NULL,
    customer_document     VARCHAR(18) NOT NULL,
    customer_company_name VARCHAR(255),
    customer_phone        VARCHAR(25) NOT NULL,
    vehicle_id            VARCHAR(36) NOT NULL,
    vehicle_license_plate VARCHAR(20) NOT NULL,
    vehicle_brand         VARCHAR(100) NOT NULL,
    vehicle_model         VARCHAR(100) NOT NULL,
    vehicle_year          INT NOT NULL,
    quote_sent_at         TIMESTAMP WITH TIME ZONE,
    completed_at          TIMESTAMP WITH TIME ZONE,
    delivered_at          TIMESTAMP WITH TIME ZONE,
    created_at            TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at            TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_service_order_status ON service_order (status);
CREATE INDEX idx_service_order_customer_id ON service_order (customer_id);
CREATE INDEX idx_service_order_vehicle_id ON service_order (vehicle_id);

CREATE TABLE service_order_work (
    id               VARCHAR(36) PRIMARY KEY,
    service_order_id VARCHAR(26) NOT NULL REFERENCES service_order (id) ON DELETE CASCADE,
    work_id          VARCHAR(36) NOT NULL,
    name             VARCHAR(255) NOT NULL,
    description      TEXT,
    unit_price       NUMERIC(10, 2) NOT NULL,
    UNIQUE (service_order_id, work_id)
);

CREATE TABLE service_order_supply (
    id               VARCHAR(36) PRIMARY KEY,
    service_order_id VARCHAR(26) NOT NULL REFERENCES service_order (id) ON DELETE CASCADE,
    supply_id        VARCHAR(36) NOT NULL,
    reservation_id   VARCHAR(36) NOT NULL,
    name             VARCHAR(255) NOT NULL,
    description      TEXT,
    version          INT NOT NULL,
    quantity         INT NOT NULL CHECK (quantity > 0),
    unit_price       NUMERIC(10, 2) NOT NULL,
    UNIQUE (service_order_id, supply_id)
);
CREATE INDEX idx_so_work_so_id ON service_order_work (service_order_id);
CREATE INDEX idx_so_supply_so_id ON service_order_supply (service_order_id);

CREATE TABLE saga_instance (
    id                 VARCHAR(36) PRIMARY KEY,
    service_order_id   VARCHAR(26) NOT NULL REFERENCES service_order (id) ON DELETE CASCADE,
    step               VARCHAR(20) NOT NULL
                       CHECK (step IN ('BUDGET', 'AWAITING_CUSTOMER', 'PAYMENT', 'EXECUTION_QUEUE', 'EXECUTION', 'DONE')),
    status             VARCHAR(20) NOT NULL DEFAULT 'RUNNING'
                       CHECK (status IN ('RUNNING', 'COMPLETED', 'COMPENSATED', 'FAILED')),
    budget_requested   BOOLEAN NOT NULL DEFAULT FALSE,
    payment_requested  BOOLEAN NOT NULL DEFAULT FALSE,
    payment_approved   BOOLEAN NOT NULL DEFAULT FALSE,
    execution_enqueued BOOLEAN NOT NULL DEFAULT FALSE,
    deadline           TIMESTAMP WITH TIME ZONE,
    created_at         TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at         TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX uq_saga_active_per_so ON saga_instance (service_order_id) WHERE status = 'RUNNING';
CREATE INDEX idx_saga_deadline ON saga_instance (deadline) WHERE status = 'RUNNING' AND deadline IS NOT NULL;

CREATE TABLE processed_message (
    message_id   UUID PRIMARY KEY,
    processed_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE outbox (
    id           BIGSERIAL PRIMARY KEY,
    message_id   UUID NOT NULL UNIQUE,
    topic        VARCHAR(50) NOT NULL,
    target       VARCHAR(20),
    type         VARCHAR(50) NOT NULL,
    body         JSONB NOT NULL,
    created_at   TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    published_at TIMESTAMP WITH TIME ZONE
);
CREATE INDEX idx_outbox_unpublished ON outbox (id) WHERE published_at IS NULL;
