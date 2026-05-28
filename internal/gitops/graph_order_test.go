package gitops

import (
	"reflect"
	"testing"
)

func TestParseGraphOrder(t *testing.T) {
	tests := []struct {
		value string
		want  GraphOrder
	}{
		{value: "", want: GraphOrderTopo},
		{value: "topo", want: GraphOrderTopo},
		{value: "date", want: GraphOrderDate},
		{value: "author-date", want: GraphOrderAuthorDate},
		{value: "first-parent", want: GraphOrderFirstParent},
	}

	for _, tt := range tests {
		got, err := ParseGraphOrder(tt.value)
		if err != nil {
			t.Fatalf("ParseGraphOrder(%q): %v", tt.value, err)
		}
		if got != tt.want {
			t.Fatalf("ParseGraphOrder(%q) = %q, want %q", tt.value, got, tt.want)
		}
	}
}

func TestParseGraphOrderRejectsInvalidValue(t *testing.T) {
	if _, err := ParseGraphOrder("message"); err == nil {
		t.Fatalf("ParseGraphOrder accepted invalid value")
	}
}

func TestGraphOrderGitLogArgs(t *testing.T) {
	tests := []struct {
		order GraphOrder
		want  []string
	}{
		{order: GraphOrderTopo, want: []string{"--topo-order"}},
		{order: GraphOrderDate, want: []string{"--date-order"}},
		{order: GraphOrderAuthorDate, want: []string{"--author-date-order"}},
		{order: GraphOrderFirstParent, want: []string{"--first-parent", "--topo-order"}},
	}

	for _, tt := range tests {
		got := tt.order.GitLogArgs()
		if !reflect.DeepEqual(got, tt.want) {
			t.Fatalf("%s.GitLogArgs() = %v, want %v", tt.order, got, tt.want)
		}
	}
}

func TestGraphOrderNextCyclesAllOrders(t *testing.T) {
	order := GraphOrderTopo
	for _, want := range []GraphOrder{GraphOrderDate, GraphOrderAuthorDate, GraphOrderFirstParent, GraphOrderTopo} {
		order = order.Next()
		if order != want {
			t.Fatalf("Next() = %q, want %q", order, want)
		}
	}
}
