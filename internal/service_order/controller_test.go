package service_order

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/auth"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/domain"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const internalToken = "internal-test-token"

func init() {
	gin.SetMode(gin.TestMode)
}

func newRouter(f *fixture) *gin.Engine {
	r := gin.New()
	RegisterRoutes(r, NewController(f.svc), internalToken)
	return r
}

// tokenFor signs a JWT for the given user and roles (the same key the middleware checks).
func tokenFor(userID string, roles ...auth.Role) string {
	tok, err := auth.GenerateToken(userID, roles, time.Now().Add(time.Hour))
	if err != nil {
		panic(err)
	}
	return "Bearer " + tok
}

// att, mec and cus are the tokens of the attendant, the mechanic and the customer who owns soID.
var (
	att = func() string { return tokenFor("staff-1", auth.ATTENDANT) }
	mec = func() string { return tokenFor("staff-2", auth.MECHANIC) }
	cus = func() string { return tokenFor(customerUser, auth.CUSTOMER) }
)

func call(r *gin.Engine, method, path, authz, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) int {
	t.Helper()
	var body struct {
		Code int `json:"code"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body.Code
}

const soPath = "/v1/service-order/" + soID

// ---- authentication and authorization matrix (routes.go) ----

type routeCase struct {
	name, method, path string
	allowed, denied    func() string
	body               string
}

func routeMatrix() []routeCase {
	return []routeCase{
		{"create", http.MethodPost, "/v1/service-order", att, mec, `{"client":"c1","vehicle":"v1","works":["w1"]}`},
		{"add work", http.MethodPost, soPath + "/works", att, mec, `{"services":["w1"]}`},
		{"remove work", http.MethodDelete, soPath + "/works/w1", att, mec, ""},
		{"received", http.MethodPut, soPath + "/received", att, mec, ""},
		{"start diagnosis", http.MethodPut, soPath + "/start-diagnosis", mec, att, ""},
		{"add supplies", http.MethodPost, soPath + "/supplies", mec, att, `{"supplies":[{"id":"s1","amount":1}]}`},
		{"remove supply", http.MethodDelete, soPath + "/supplies/s1", mec, att, ""},
		{"send", http.MethodPut, soPath + "/send", mec, att, ""},
		{"accept", http.MethodPut, soPath + "/accept", cus, att, ""},
		{"reject", http.MethodPut, soPath + "/reject", cus, att, ""},
		{"cancel", http.MethodPut, soPath + "/cancel", att, cus, ""},
		{"cancel by mechanic", http.MethodPut, soPath + "/cancel", mec, cus, ""},
		{"deliver", http.MethodPut, soPath + "/deliver", att, mec, ""},
		{"list", http.MethodGet, "/v1/service-order", att, cus, ""},
		{"list by mechanic", http.MethodGet, "/v1/service-order", mec, cus, ""},
		{"detail", http.MethodGet, soPath, att, cus, ""},
		{"works", http.MethodGet, soPath + "/works", mec, cus, ""},
		{"supplies", http.MethodGet, soPath + "/supplies", att, cus, ""},
	}
}

func TestRoutesRequireAuthenticationAndRole(t *testing.T) {
	for _, rt := range routeMatrix() {
		t.Run(rt.name, func(t *testing.T) {
			f := newFixture(t)
			f.seed(t, domain.SERVICE_ORDER_STATUS_NEW, nil, nil)
			r := newRouter(f)

			assert.Equal(t, http.StatusUnauthorized, call(r, rt.method, rt.path, "", rt.body).Code, "no token")
			assert.Equal(t, http.StatusUnauthorized, call(r, rt.method, rt.path, "Token abc", rt.body).Code, "wrong scheme")
			assert.Equal(t, http.StatusUnauthorized, call(r, rt.method, rt.path, "Bearer not-a-jwt", rt.body).Code, "bad jwt")
			assert.Equal(t, http.StatusForbidden, call(r, rt.method, rt.path, rt.denied(), rt.body).Code, "role not allowed")

			code := call(r, rt.method, rt.path, rt.allowed(), rt.body).Code
			assert.NotContains(t, []int{http.StatusUnauthorized, http.StatusForbidden}, code, "allowed role reaches the handler")
		})
	}
}

func TestExpiredTokenIsUnauthorized(t *testing.T) {
	f := newFixture(t)
	tok, err := auth.GenerateToken("staff-1", []auth.Role{auth.ATTENDANT}, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	rec := call(newRouter(f), http.MethodGet, "/v1/service-order", "Bearer "+tok, "")
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.JSONEq(t, `{"code":401,"error":"Token expired"}`, rec.Body.String())
}

func TestStatusEndpointIsPublic(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_RECEIVED, nil, nil)
	rec := call(newRouter(f), http.MethodGet, soPath+"/status", "", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"id":"`+soID+`","status":"RECEIVED"}`, rec.Body.String())
}

// ---- success codes ----

func TestCreateReturnsCreatedWithIdAndStatus(t *testing.T) {
	f := newFixture(t)
	rec := call(newRouter(f), http.MethodPost, "/v1/service-order", att(),
		`{"client":"c1","vehicle":"v1","works":["w1"],"supplies":[{"id":"s1","amount":2}]}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var body struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.NotEmpty(t, body.ID)
	assert.Equal(t, "NEW", body.Status)
}

func TestLocalFlowReturnsNoContent(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_NEW, nil, nil)
	r := newRouter(f)
	steps := []struct {
		method, path, authz, body string
	}{
		{http.MethodPost, soPath + "/works", att(), `{"services":["w1"]}`},
		{http.MethodDelete, soPath + "/works/w1", att(), ""},
		{http.MethodPost, soPath + "/works", att(), `{"services":["w1"]}`},
		{http.MethodPut, soPath + "/received", att(), ""},
		{http.MethodPut, soPath + "/start-diagnosis", mec(), ""},
		{http.MethodPost, soPath + "/supplies", mec(), `{"supplies":[{"id":"s1","amount":1}]}`},
		{http.MethodDelete, soPath + "/supplies/s1", mec(), ""},
		{http.MethodPost, soPath + "/supplies", mec(), `{"supplies":[{"id":"s1","amount":1}]}`},
		{http.MethodPut, soPath + "/send", mec(), ""},
	}
	for _, s := range steps {
		rec := call(r, s.method, s.path, s.authz, s.body)
		require.Equal(t, http.StatusNoContent, rec.Code, "%s %s: %s", s.method, s.path, rec.Body.String())
	}
	assert.Equal(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS, f.status(t))
}

func TestAcceptThenCancelReturnNoContent(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS, []domain.WorkItem{f.catalog.works["w1"]}, nil)
	f.awaitApproval(t)
	r := newRouter(f)

	rec := call(r, http.MethodPut, soPath+"/accept", cus(), "")
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Equal(t, domain.SERVICE_ORDER_STATUS_IN_PROGRESS, f.status(t))

	rec = call(r, http.MethodPut, soPath+"/cancel", att(), "")
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Equal(t, domain.SERVICE_ORDER_STATUS_CANCELLED, f.status(t))
}

func TestRejectReturnsNoContent(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS, []domain.WorkItem{f.catalog.works["w1"]}, nil)
	f.awaitApproval(t)
	rec := call(newRouter(f), http.MethodPut, soPath+"/reject", cus(), "")
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Equal(t, domain.SERVICE_ORDER_STATUS_REJECTED, f.status(t))
}

func TestDeliverReturnsNoContentWhenCompleted(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_COMPLETED, nil, nil)
	rec := call(newRouter(f), http.MethodPut, soPath+"/deliver", att(), "")
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Equal(t, domain.SERVICE_ORDER_STATUS_DELIVERED, f.status(t))
}

// ---- error mapping ----

type errCase struct {
	name   string
	setup  func(t *testing.T, f *fixture)
	method string
	path   string
	authz  string
	body   string
	status int
}

func TestErrorsMapToHTTPStatus(t *testing.T) {
	cases := []errCase{
		{"unknown customer", nil, http.MethodPost, "/v1/service-order", att(),
			`{"client":"nope","vehicle":"v1","works":["w1"]}`, http.StatusNotFound},
		{"unknown vehicle", nil, http.MethodPost, "/v1/service-order", att(),
			`{"client":"c1","vehicle":"nope","works":["w1"]}`, http.StatusNotFound},
		{"vehicle of another customer", func(t *testing.T, f *fixture) { f.customers.owners[vehicleID] = "x" },
			http.MethodPost, "/v1/service-order", att(), `{"client":"c1","vehicle":"v1","works":["w1"]}`, http.StatusForbidden},
		{"catalog unavailable", func(t *testing.T, f *fixture) { f.catalog.workErr = domain.ErrUpstreamUnavailable },
			http.MethodPost, "/v1/service-order", att(), `{"client":"c1","vehicle":"v1","works":["w1"]}`, http.StatusBadGateway},
		{"catalog rejects the request", func(t *testing.T, f *fixture) { f.catalog.workErr = domain.ErrUpstreamValidation },
			http.MethodPost, "/v1/service-order", att(), `{"client":"c1","vehicle":"v1","works":["w1"]}`, http.StatusUnprocessableEntity},
		{"unexpected failure", func(t *testing.T, f *fixture) { f.catalog.workErr = errBoom },
			http.MethodPost, "/v1/service-order", att(), `{"client":"c1","vehicle":"v1","works":["w1"]}`, http.StatusInternalServerError},
		{"malformed json", nil, http.MethodPost, "/v1/service-order", att(), `{"client":`, http.StatusBadRequest},
		{"out of stock supply", nil, http.MethodPost, "/v1/service-order", att(),
			`{"client":"c1","vehicle":"v1","works":["w1"],"supplies":[{"id":"s2","amount":9}]}`, http.StatusUnprocessableEntity},
		{"add work to order not NEW", seedWith(domain.SERVICE_ORDER_STATUS_RECEIVED), http.MethodPost, soPath + "/works", att(),
			`{"services":["w1"]}`, http.StatusConflict},
		{"add work with empty list", seedWith(domain.SERVICE_ORDER_STATUS_NEW), http.MethodPost, soPath + "/works", att(),
			`{"services":[]}`, http.StatusBadRequest},
		{"add work to missing order", nil, http.MethodPost, soPath + "/works", att(), `{"services":["w1"]}`, http.StatusNotFound},
		{"remove unknown work", seedWith(domain.SERVICE_ORDER_STATUS_NEW), http.MethodDelete, soPath + "/works/w9", att(), "", http.StatusNotFound},
		{"receive an order not NEW", seedWith(domain.SERVICE_ORDER_STATUS_RECEIVED), http.MethodPut, soPath + "/received", att(), "", http.StatusConflict},
		{"start diagnosis from NEW", seedWith(domain.SERVICE_ORDER_STATUS_NEW), http.MethodPut, soPath + "/start-diagnosis", mec(), "", http.StatusConflict},
		{"supplies outside diagnosis", seedWith(domain.SERVICE_ORDER_STATUS_NEW), http.MethodPost, soPath + "/supplies", mec(),
			`{"supplies":[{"id":"s1","amount":1}]}`, http.StatusConflict},
		{"remove supply not linked", seedWith(domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS), http.MethodDelete, soPath + "/supplies/s1", mec(), "", http.StatusNotFound},
		{"send without works", seedWith(domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS), http.MethodPut, soPath + "/send", mec(), "", http.StatusBadRequest},
		{"accept by another customer", awaitingApproval, http.MethodPut, soPath + "/accept", tokenFor("someone-else", auth.CUSTOMER), "", http.StatusForbidden},
		{"accept while not awaiting approval", seedWith(domain.SERVICE_ORDER_STATUS_NEW), http.MethodPut, soPath + "/accept", cus(), "", http.StatusConflict},
		{"reject while not awaiting approval", seedWith(domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS), http.MethodPut, soPath + "/reject", cus(), "", http.StatusConflict},
		{"cancel a completed order", seedWith(domain.SERVICE_ORDER_STATUS_COMPLETED), http.MethodPut, soPath + "/cancel", att(), "", http.StatusConflict},
		{"deliver before completion", seedWith(domain.SERVICE_ORDER_STATUS_IN_PROGRESS), http.MethodPut, soPath + "/deliver", att(), "", http.StatusConflict},
		{"detail of missing order", nil, http.MethodGet, soPath, att(), "", http.StatusNotFound},
		{"status of missing order", nil, http.MethodGet, soPath + "/status", "", "", http.StatusNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			if c.setup != nil {
				c.setup(t, f)
			}
			rec := call(newRouter(f), c.method, c.path, c.authz, c.body)
			assert.Equal(t, c.status, rec.Code, rec.Body.String())
			assert.Equal(t, c.status, errorCode(t, rec), "the error body carries the status code")
		})
	}
}

func seedWith(status domain.SERVICE_ORDER_STATUS) func(t *testing.T, f *fixture) {
	return func(t *testing.T, f *fixture) { f.seed(t, status, nil, nil) }
}

func awaitingApproval(t *testing.T, f *fixture) {
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS, []domain.WorkItem{f.catalog.works["w1"]}, nil)
	f.awaitApproval(t)
}

func TestExistsInternalEndpoint(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_NEW, nil, nil)
	r := newRouter(f)
	internal := func(query string, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/internal/service-orders/exists?"+query, nil)
		if token != "" {
			req.Header.Set("X-Internal-Token", token)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	rec := internal("customer_id=c1", internalToken)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"exists":true}`, rec.Body.String())

	rec = internal("vehicle_id=nope", internalToken)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"exists":false}`, rec.Body.String())

	assert.Equal(t, http.StatusUnauthorized, internal("customer_id=c1", "").Code, "token is required")
	assert.Equal(t, http.StatusUnauthorized, internal("customer_id=c1", "wrong").Code)
	assert.Equal(t, http.StatusUnprocessableEntity, internal("", internalToken).Code, "one parameter is required")
	assert.Equal(t, http.StatusUnprocessableEntity, internal("customer_id=c1&vehicle_id=v1", internalToken).Code, "not both")
}

func TestExistsReportsRepositoryFailure(t *testing.T) {
	f := newFixture(t)
	f.repo.existsErr = errBoom
	req := httptest.NewRequest(http.MethodGet, "/internal/service-orders/exists?customer_id=c1", nil)
	req.Header.Set("X-Internal-Token", internalToken)
	rec := httptest.NewRecorder()
	newRouter(f).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// ---- response formats identical to the monolith (adapters/so_read.go and mappers.go) ----

func seedDetailOrder(t *testing.T, f *fixture) {
	t.Helper()
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS,
		[]domain.WorkItem{{ID: "w1", Name: "Troca de óleo", Description: "Óleo e filtro", UnitPrice: decimal.RequireFromString("150.00")}},
		[]domain.SupplyItem{{ID: "s1", ReservationID: "res-1", Name: "Filtro", UnitPrice: decimal.RequireFromString("35.50"), Version: 3, Quantity: 2}})
}

func TestDetailResponseMatchesMonolithFormat(t *testing.T) {
	f := newFixture(t)
	seedDetailOrder(t, f)
	rec := call(newRouter(f), http.MethodGet, soPath, att(), "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{
		"id": "`+soID+`",
		"status": "IN_DIAGNOSIS",
		"totalValue": 221,
		"customer": {"id": "c1", "name": "João", "email": "joao@example.com", "type": "PF", "document": "00000000000", "phone": "11999990000"},
		"vehicle": {"id": "v1", "licensePlate": "ABC1D23", "brandModel": "Fiat - Uno", "year": 2018},
		"works": [{"id": "w1", "name": "Troca de óleo", "description": "Óleo e filtro", "price": "150", "status": "ACTIVE"}],
		"supplies": [{"id": "s1", "name": "Filtro", "description": "", "price": "35.5", "version": 3, "amount": 2}]
	}`, rec.Body.String())
}

func TestDetailOmitsEmptyCollectionsAndCompanyName(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_NEW, nil, nil)
	rec := call(newRouter(f), http.MethodGet, soPath, att(), "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), `"works"`)
	assert.NotContains(t, rec.Body.String(), `"supplies"`)
	assert.NotContains(t, rec.Body.String(), `"companyName"`)
}

func TestListResponseMatchesMonolithFormat(t *testing.T) {
	f := newFixture(t)
	seedDetailOrder(t, f)
	rec := call(newRouter(f), http.MethodGet, "/v1/service-order?page=1&pageSize=10&customerId=c1", att(), "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{
		"items": [{
			"id": "`+soID+`",
			"status": "IN_DIAGNOSIS",
			"totalValue": 221,
			"customer": {"id": "c1", "name": "João", "email": "joao@example.com", "type": "PF", "document": "00000000000", "phone": "11999990000"},
			"vehicle": {"id": "v1", "licensePlate": "ABC1D23", "brandModel": "Fiat - Uno", "year": 2018}
		}],
		"totalItems": 1, "totalPages": 1, "pageSize": 10, "page": 1
	}`, rec.Body.String())
}

func TestListParsesPaginationAndFilters(t *testing.T) {
	f := newFixture(t)
	seedDetailOrder(t, f)
	rec := call(newRouter(f), http.MethodGet, "/v1/service-order?page=abc&pageSize=-3&status=IN_DIAGNOSIS&vehicleId=v1&sort_by=status&sort_order=asc", att(), "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body struct {
		Page     int64 `json:"page"`
		PageSize int64 `json:"pageSize"`
		Total    int64 `json:"totalItems"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, int64(1), body.Page, "a bad page falls back to 1")
	assert.Equal(t, int64(10), body.PageSize, "a bad page size falls back to 10")
	assert.Equal(t, int64(1), body.Total)
}

func TestWorksAndSuppliesResponsesMatchMonolithFormat(t *testing.T) {
	f := newFixture(t)
	seedDetailOrder(t, f)
	r := newRouter(f)

	rec := call(r, http.MethodGet, soPath+"/works", mec(), "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{
		"items": [{"id": "w1", "name": "Troca de óleo", "description": "Óleo e filtro", "price": "150", "status": "ACTIVE"}],
		"totalItems": 1, "totalPages": 1, "pageSize": 1, "page": 1
	}`, rec.Body.String())

	rec = call(r, http.MethodGet, soPath+"/supplies", mec(), "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{
		"items": [{"id": "s1", "name": "Filtro", "description": "", "price": "35.5", "version": 3, "amount": 2}],
		"totalItems": 1, "totalPages": 1, "pageSize": 1, "page": 1
	}`, rec.Body.String())
}

func TestCreateRequestUsesMonolithFieldNames(t *testing.T) {
	f := newFixture(t)
	rec := call(newRouter(f), http.MethodPost, "/v1/service-order", att(),
		`{"customerId":"c1","vehicleId":"v1","works":["w1"]}`)
	assert.Equal(t, http.StatusNotFound, rec.Code, "the request uses 'client' and 'vehicle' like the monolith")
}
