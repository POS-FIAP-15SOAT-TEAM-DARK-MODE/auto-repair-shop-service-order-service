package adapters

type SOFilterParams struct {
	Page       int64
	PageSize   int64
	Limit      int64
	Offset     int64
	Status     string
	CustomerID string
	VehicleID  string
	SortBy     string
	SortOrder  string
}

type WorkItemResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Price       string `json:"price"`
	Status      string `json:"status"`
}

type SupplyItemResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Price       string `json:"price"`
	Version     int    `json:"version"`
	Amount      int    `json:"amount"`
}

type CustomerResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	Type        string `json:"type"`
	Document    string `json:"document"`
	CompanyName string `json:"companyName,omitempty"`
	Phone       string `json:"phone"`
}

type VehicleResponse struct {
	ID           string `json:"id"`
	LicensePlate string `json:"licensePlate"`
	BrandModel   string `json:"brandModel"`
	Year         int    `json:"year"`
}

type SOListItemResponse struct {
	ID         string           `json:"id"`
	Status     string           `json:"status"`
	TotalValue float64          `json:"totalValue"`
	Customer   CustomerResponse `json:"customer"`
	Vehicle    VehicleResponse  `json:"vehicle"`
}

type PaginatedSOResponse struct {
	Items      []SOListItemResponse `json:"items"`
	TotalItems int64                `json:"totalItems"`
	TotalPages int64                `json:"totalPages"`
	PageSize   int64                `json:"pageSize"`
	Page       int64                `json:"page"`
}

type SODetailResponse struct {
	ID         string               `json:"id"`
	Status     string               `json:"status"`
	TotalValue float64              `json:"totalValue"`
	Customer   CustomerResponse     `json:"customer"`
	Vehicle    VehicleResponse      `json:"vehicle"`
	Works      []WorkItemResponse   `json:"works,omitempty"`
	Supplies   []SupplyItemResponse `json:"supplies,omitempty"`
}

type ListResponse[T any] struct {
	Items      []T   `json:"items"`
	TotalItems int64 `json:"totalItems"`
	TotalPages int64 `json:"totalPages"`
	PageSize   int64 `json:"pageSize"`
	Page       int64 `json:"page"`
}

type WorkExecutionTimeResponse struct {
	WorkID               string  `json:"workId"`
	WorkName             string  `json:"workName"`
	AverageExecutionTime float64 `json:"averageExecutionTimeHours"`
}
