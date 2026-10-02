package main

import (
	"bufio"
	"strings"
	"testing"
)

func TestTerminalAllocations(t *testing.T) {
	allocations, err := readPortfolioAllocation(bufio.NewReader(strings.NewReader("2 aapl 50 MSFT 50")))
	if err != nil || len(allocations) != 2 || allocations[0].Symbol != "AAPL" || allocations[1].Weight != .5 {
		t.Fatalf("allocations=%+v err=%v", allocations, err)
	}
	for _, input := range []string{"", "2 AAPL", "1 AAPL NaN", "1 AAPL invalid"} {
		if _, err := readPortfolioAllocation(bufio.NewReader(strings.NewReader(input))); err == nil {
			t.Fatalf("accepted incomplete/invalid input %q", input)
		}
	}
}
