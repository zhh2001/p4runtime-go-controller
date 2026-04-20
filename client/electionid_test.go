package client_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zhh2001/p4runtime-go-controller/client"
)

func TestElectionID_Comparisons(t *testing.T) {
	cases := []struct {
		name  string
		a, b  client.ElectionID
		less  bool
		equal bool
	}{
		{
			name: "zero vs one",
			a:    client.ElectionID{}, b: client.ElectionID{Low: 1},
			less: true,
		},
		{
			name: "same high, different low",
			a:    client.ElectionID{High: 2, Low: 1},
			b:    client.ElectionID{High: 2, Low: 2},
			less: true,
		},
		{
			name: "different high",
			a:    client.ElectionID{High: 1, Low: math.MaxUint64},
			b:    client.ElectionID{High: 2, Low: 0},
			less: true,
		},
		{
			name:  "equal",
			a:     client.ElectionID{High: 7, Low: 42},
			b:     client.ElectionID{High: 7, Low: 42},
			equal: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.equal {
				assert.True(t, tc.a.Equal(tc.b))
				assert.False(t, tc.a.Less(tc.b))
				assert.Equal(t, 0, tc.a.Cmp(tc.b))
				return
			}
			assert.Equal(t, tc.less, tc.a.Less(tc.b))
			assert.Equal(t, !tc.less, tc.b.Less(tc.a))
			assert.False(t, tc.a.Equal(tc.b))
			if tc.less {
				assert.Equal(t, -1, tc.a.Cmp(tc.b))
				assert.Equal(t, 1, tc.b.Cmp(tc.a))
			}
		})
	}
}

func TestElectionID_IsZero(t *testing.T) {
	assert.True(t, client.ElectionID{}.IsZero())
	assert.False(t, client.ElectionID{Low: 1}.IsZero())
	assert.False(t, client.ElectionID{High: 1}.IsZero())
}

func TestElectionID_Increment(t *testing.T) {
	e := client.ElectionID{Low: 1}
	next, ok := e.Increment()
	require.True(t, ok)
	assert.Equal(t, client.ElectionID{Low: 2}, next)

	// low-to-high carry
	boundary := client.ElectionID{Low: math.MaxUint64}
	next, ok = boundary.Increment()
	require.True(t, ok)
	assert.Equal(t, client.ElectionID{High: 1, Low: 0}, next)

	// saturating at max
	max := client.ElectionID{High: math.MaxUint64, Low: math.MaxUint64}
	_, ok = max.Increment()
	assert.False(t, ok, "increment at max must return ok=false")
}

func TestElectionID_String(t *testing.T) {
	assert.Equal(t, "3:4", client.ElectionID{High: 3, Low: 4}.String())
}

func TestElectionID_BigInt(t *testing.T) {
	e := client.ElectionID{High: 1, Low: 2}
	bi := e.BigInt()
	require.NotNil(t, bi)
	// 1 << 64 | 2
	expected := "18446744073709551618"
	assert.Equal(t, expected, bi.String())
}
