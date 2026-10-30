package arcsetops_test

import (
	"context"
	"testing"

	"github.com/ddy2006/packfs/internal/arcset"
	"github.com/ddy2006/packfs/internal/arcsetops"
	"github.com/ddy2006/packfs/internal/pgtest"
)

func TestCreateAndExpand(t *testing.T) {
	pool := pgtest.Open(t)
	ctx := context.Background()

	res, err := arcsetops.Create(ctx, pool, arcsetops.CreateOptions{
		Name:              "ops-arc",
		TargetRoot:        "/tmp/ops-arc",
		EC:                "4+2",
		Groups:            1,
		Layout:            "raid3",
		PartitionWidth:    2,
		SeqWidth:          3,
		PartitionCapacity: 1 << 30,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if res.PartitionCount != 6 {
		t.Fatalf("partition_count=%d, want 6", res.PartitionCount)
	}
	if got := res.Arcset.Metadata["ec"]; got != "4+2" {
		t.Fatalf("ec=%v", got)
	}
	if got := metaInt(res.Arcset.Metadata, "data_capacity"); got != 4<<30 {
		t.Fatalf("data_capacity=%v", got)
	}

	exp, err := arcsetops.Expand(ctx, pool, int64(res.Arcset.ID), 2)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if exp.GroupCount != 3 || exp.PartitionCount != 18 {
		t.Fatalf("expand result: %+v", exp)
	}
	if exp.DataCapacity != 12<<30 || exp.RawCapacity != 18<<30 {
		t.Fatalf("expand capacity: %+v", exp)
	}

	a, err := arcset.NewPGStore(pool).FindByID(ctx, int64(res.Arcset.ID))
	if err != nil {
		t.Fatal(err)
	}
	if int(metaInt(a.Metadata, "group_count")) != 3 {
		t.Fatalf("persisted group_count=%v", a.Metadata["group_count"])
	}

	if _, err := arcsetops.Expand(ctx, pool, int64(res.Arcset.ID), 100); err == nil {
		t.Fatal("expected partition width overflow")
	}
}

func TestCreateRejectsInvalidOptions(t *testing.T) {
	pool := pgtest.Open(t)
	ctx := context.Background()
	if _, err := arcsetops.Create(ctx, pool, arcsetops.CreateOptions{
		Name: "bad", TargetRoot: "/tmp/bad", EC: "4+2", Groups: 0,
		PartitionCapacity: 1 << 30,
	}); err == nil {
		t.Fatal("expected groups error")
	}
	if _, err := arcsetops.Create(ctx, pool, arcsetops.CreateOptions{
		Name: "bad", TargetRoot: "/tmp/bad", EC: "8", Layout: "raid5", Groups: 1,
		PartitionCapacity: 1 << 30,
	}); err == nil {
		t.Fatal("expected layout error for m=0")
	}
}

func TestParseBytes(t *testing.T) {
	cases := map[string]int64{
		"1024": 1024,
		"1KiB": 1 << 10,
		"2MiB": 2 << 20,
		"3GB":  3 << 30,
		"1TiB": 1 << 40,
	}
	for in, want := range cases {
		got, err := arcsetops.ParseBytes(in)
		if err != nil || got != want {
			t.Fatalf("ParseBytes(%q)=%d,%v want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "0", "-1", "x"} {
		if _, err := arcsetops.ParseBytes(in); err == nil {
			t.Fatalf("ParseBytes(%q) expected error", in)
		}
	}
}

func metaInt(m map[string]any, key string) int64 {
	switch v := m[key].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	}
	return 0
}
