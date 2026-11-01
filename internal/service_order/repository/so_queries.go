package repository

// terminalStatuses are excluded from the default list view; pass an explicit status to see them.
var terminalStatuses = []string{"DELIVERED", "COMPLETED", "REJECTED", "CANCELLED"}

// statusSortCaseExpr orders the list by workflow priority.
const statusSortCaseExpr = `CASE so.status ` +
	`WHEN 'IN_PROGRESS' THEN 1 ` +
	`WHEN 'AWAITING_APPROVAL' THEN 2 ` +
	`WHEN 'IN_DIAGNOSIS' THEN 3 ` +
	`WHEN 'RECEIVED' THEN 4 ` +
	`WHEN 'NEW' THEN 5 ` +
	`ELSE 6 ` +
	`END ASC`

const (
	serviceOrderColumns = `
	so.id, so.status, so.total_amount,
	so.customer_id, so.customer_user_id, so.customer_name, so.customer_email, so.customer_type,
	so.customer_document, so.customer_company_name, so.customer_phone,
	so.vehicle_id, so.vehicle_license_plate, so.vehicle_brand, so.vehicle_model, so.vehicle_year,
	so.quote_sent_at, so.completed_at, so.delivered_at`

	insertServiceOrderQuery = `
	INSERT INTO service_order (id, status, total_amount,
		customer_id, customer_user_id, customer_name, customer_email, customer_type,
		customer_document, customer_company_name, customer_phone,
		vehicle_id, vehicle_license_plate, vehicle_brand, vehicle_model, vehicle_year)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`

	updateServiceOrderQuery = `
	UPDATE service_order
	SET status = $2, total_amount = $3, quote_sent_at = $4, completed_at = $5, delivered_at = $6,
	    updated_at = NOW()
	WHERE id = $1`

	serviceOrderFindByIDQuery = `SELECT ` + serviceOrderColumns + ` FROM service_order so WHERE so.id = $1 FOR UPDATE`

	serviceOrderStatusQuery = `SELECT status FROM service_order WHERE id = $1`

	countServiceOrderQuery  = `SELECT COUNT(so.id) FROM service_order so`
	searchServiceOrderQuery = `SELECT ` + serviceOrderColumns + ` FROM service_order so`

	listWorksQuery = `
	SELECT work_id, name, description, unit_price
	FROM service_order_work
	WHERE service_order_id = $1
	ORDER BY id ASC`

	insertWorkQuery = `
	INSERT INTO service_order_work (id, service_order_id, work_id, name, description, unit_price)
	VALUES ($1, $2, $3, $4, $5, $6)
	ON CONFLICT (service_order_id, work_id) DO NOTHING`

	deleteWorkQuery = `DELETE FROM service_order_work WHERE service_order_id = $1 AND work_id = $2`

	listSuppliesQuery = `
	SELECT supply_id, reservation_id, name, description, unit_price, version, quantity
	FROM service_order_supply
	WHERE service_order_id = $1
	ORDER BY id ASC`

	insertSupplyQuery = `
	INSERT INTO service_order_supply (id, service_order_id, supply_id, reservation_id, name, description, version, quantity, unit_price)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

	deleteSupplyQuery = `
	DELETE FROM service_order_supply
	WHERE service_order_id = $1 AND supply_id = $2
	RETURNING supply_id, reservation_id, name, description, unit_price, version, quantity`

	existsServiceOrderQuery = `SELECT EXISTS (SELECT 1 FROM service_order WHERE customer_id = $1 OR vehicle_id = $2)`

	updateTotalQuery = `UPDATE service_order SET total_amount = $2, updated_at = NOW() WHERE id = $1`
)
