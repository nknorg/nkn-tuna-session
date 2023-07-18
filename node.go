package session

import (
	"math"
	"math/rand"
	"sort"
	"time"

	"github.com/nknorg/tuna/types"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

type sortByWeight types.Nodes

func (ns sortByWeight) Len() int {
	return len(ns)
}

func (ns sortByWeight) Swap(i, j int) {
	ns[i], ns[j] = ns[j], ns[i]
}

func (ns sortByWeight) Less(i, j int) bool {
	return nodeWeight(ns[i]) > nodeWeight(ns[j])
}

func nodeWeight(n *types.Node) float64 {
	return math.Pow(float64((n.Bandwidth+1)/(n.Delay+1)), 4)
}

func weightedRandomChoice(nodes types.Nodes) int {
	if len(nodes) == 0 {
		return -1
	}

	cdf := make([]float64, len(nodes))
	cdf[0] = nodeWeight(nodes[0])
	for i := 1; i < len(nodes); i++ {
		cdf[i] = cdf[i-1] + nodeWeight(nodes[i])
	}

	v := rand.Float64() * cdf[len(cdf)-1]
	return sort.Search(len(cdf), func(i int) bool { return cdf[i] > v }) % len(cdf)
}

func sortMeasuredNodes(nodes types.Nodes) {
	sort.Sort(sortByWeight(nodes))
	choice := weightedRandomChoice(nodes)
	for i := choice; i > 0; i-- {
		nodes[i-1], nodes[i] = nodes[i], nodes[i-1]
	}
}
