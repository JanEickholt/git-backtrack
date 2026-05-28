package gitops

import "fmt"

type GraphOrder string

const (
	GraphOrderTopo        GraphOrder = "topo"
	GraphOrderDate        GraphOrder = "date"
	GraphOrderAuthorDate  GraphOrder = "author-date"
	GraphOrderFirstParent GraphOrder = "first-parent"
)

func DefaultGraphOrder() GraphOrder {
	return GraphOrderTopo
}

func ParseGraphOrder(value string) (GraphOrder, error) {
	switch GraphOrder(value) {
	case "", GraphOrderTopo:
		return GraphOrderTopo, nil
	case GraphOrderDate:
		return GraphOrderDate, nil
	case GraphOrderAuthorDate:
		return GraphOrderAuthorDate, nil
	case GraphOrderFirstParent:
		return GraphOrderFirstParent, nil
	default:
		return "", fmt.Errorf("invalid graph order %q (want topo, date, author-date, or first-parent)", value)
	}
}

func (o GraphOrder) GitLogArgs() []string {
	switch o {
	case GraphOrderDate:
		return []string{"--date-order"}
	case GraphOrderAuthorDate:
		return []string{"--author-date-order"}
	case GraphOrderFirstParent:
		return []string{"--first-parent", "--topo-order"}
	default:
		return []string{"--topo-order"}
	}
}

func (o GraphOrder) Label() string {
	if o == "" {
		return string(DefaultGraphOrder())
	}
	return string(o)
}

func (o GraphOrder) Next() GraphOrder {
	switch o {
	case GraphOrderTopo, "":
		return GraphOrderDate
	case GraphOrderDate:
		return GraphOrderAuthorDate
	case GraphOrderAuthorDate:
		return GraphOrderFirstParent
	default:
		return GraphOrderTopo
	}
}
