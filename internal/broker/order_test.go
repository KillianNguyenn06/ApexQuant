package broker

import (
	"testing"
	"time"

	"apexquant/internal/account"
	"apexquant/internal/algorithm"
	"apexquant/internal/marketdata"
)

func TestSubmitOrder(t *testing.T) {
	timestamp := time.Date(2026, time.January, 0, 0, 0, 0, 0, time.UTC)

	signal := algorithm.Signal{
		Symbol:    "AAPL",
		Action:    "Buy",
		Price:     100,
		CreatedAt: timestamp,
	}
	order := SubmitOrder(signal, 25)

	if order.ID == "" {
		t.Fatalf("expected an order ID")
	}

	if order.Symbol != "AAPL" {
		t.Fatalf("symbol: got %s, want AAPL", order.Symbol)
	}

	if order.Action != "Buy" {
		t.Fatalf("action: got %s, want Buy", order.Action)
	}

	if order.Quantity != 25 {
		t.Fatalf("quantity: got %.2f, want 25", order.Quantity)
	}

	if order.Status != "Submitted" {
		t.Fatalf("status: got %s, want Submitted", order.Status)
	}

	if !order.CreatedAt.Equal(timestamp) {
		t.Fatalf("created time: got %v, want %v", order.CreatedAt, timestamp)
	}
}

func TestFillOrderAtNextBar(t *testing.T) {
	order := account.Order{
		ID:       "order-1",
		Symbol:   "AAPL",
		Action:   "Buy",
		Quantity: 10,
		Status:   "Submitted",
	}

	nextBar := marketdata.BarTick{
		Symbol: "AAPL",
		Open:   105,
	}

	filledOrder := FillOrderAtNextBar(order, nextBar)

	if filledOrder.Status != "filled" {
		t.Fatalf("Status: got %s, want filled", filledOrder.Status)
	}

	if filledOrder.FilledPrice != 105 {
		t.Fatalf("filled price: got %.2f, want 105", filledOrder.FilledPrice)
	}
}

func TestFillOrderRejectsDifferentSymbol(t *testing.T) {
	order := account.Order{
		ID:       "order-1",
		Symbol:   "AAPL",
		Action:   "Buy",
		Quantity: 10,
		Status:   "Submitted",
	}

	wrongBar := marketdata.BarTick{
		Symbol: "MSFT",
		Open:   105,
	}
	result := FillOrderAtNextBar(order, wrongBar)
	if result.Status == "filled" {
		t.Fatal("order should not fill using another symbol's bar")
	}

	if result.FilledPrice != 0 {
		t.Fatalf("filled price: got %.2f, want 0", result.FilledPrice)
	}

}
