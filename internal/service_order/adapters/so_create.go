package adapters

type CreateSORequest struct {
	CustomerID string          `json:"client"`
	VehicleID  string          `json:"vehicle"`
	WorkIDs    []string        `json:"works,omitempty"`
	Supplies   []AddSupplyItem `json:"supplies,omitempty"`
}

type AddWorksRequest struct {
	WorkIDs []string `json:"services"`
}

type AddSupplyItem struct {
	ID     string `json:"id"`
	Amount int    `json:"amount"`
}

type AddSuppliesRequest struct {
	Supplies []AddSupplyItem `json:"supplies"`
}

type SOResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}
