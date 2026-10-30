package builddata

import (
	"testing"

	"github.com/ddy2006/packfs/internal/arcset"
)

// TestFindNewSlotFillsStripe guards the m>0 path: five DATA shards with k=4
// must use stripe 0 positions 0..3 and only then create stripe 1.
func TestFindNewSlotFillsStripe(t *testing.T) {
	for _, m := range []int{0, 2} {
		s := &placementState{
			k:              4,
			m:              m,
			groupCount:     1,
			partitionWidth: 2,
			seqWidth:       3,
			capacity:       1 << 30,
			layout:         "raid3",
			used:           map[int]int64{},
			rows:           map[posKey]*arcset.ShardRow{},
			nextSeq:        map[int]int{},
		}

		want := [][2]int{{0, 0}, {0, 1}, {0, 2}, {0, 3}, {1, 0}}
		for i, w := range want {
			group, stripe, pos, partition, ok := s.findNewSlot(100)
			if !ok {
				t.Fatalf("m=%d i=%d: no slot", m, i)
			}
			if stripe != w[0] || pos != w[1] {
				t.Fatalf("m=%d i=%d: got stripe=%d pos=%d, want stripe=%d pos=%d",
					m, i, stripe, pos, w[0], w[1])
			}
			if partition != s.partition(group, pos, stripe) {
				t.Fatalf("m=%d i=%d: partition=%d, want %d",
					m, i, partition, s.partition(group, pos, stripe))
			}
			s.commit(&arcset.ShardRow{
				GroupNo:   intPtr(group),
				StripeSeq: intPtr(stripe),
				Pos:       intPtr(pos),
				Partition: intPtr(partition),
				FileSize:  100,
			})
		}
	}
}
