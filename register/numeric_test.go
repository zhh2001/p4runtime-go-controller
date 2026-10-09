package register

import (
	"bytes"
	"math/big"
	"testing"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func signedInteger(value []byte) *big.Int {
	v := new(big.Int).SetBytes(value)
	if len(value) > 0 && value[0]&0x80 != 0 {
		v.Sub(v, new(big.Int).Lsh(big.NewInt(1), uint(8*len(value))))
	}
	return v
}
func FuzzRegisterIntegerEncoding(f *testing.F) {
	for _, seed := range []struct {
		signed bool
		width  int16
		value  []byte
	}{
		{false, 9, []byte{1, 255}}, {false, 9, []byte{2, 0}}, {true, 9, []byte{255, 0}}, {true, 9, []byte{255, 255, 127}}, {true, 9, []byte{0, 0, 128}}, {true, 1, []byte{255}}, {false, 0, nil},
	} {
		f.Add(seed.signed, seed.width, seed.value)
	}
	f.Fuzz(func(t *testing.T, signed bool, widthInput int16, value []byte) {
		if len(value) > 128 {
			return
		}
		width := int32(widthInput % 260)
		before := bytes.Clone(value)
		v := new(big.Int).SetBytes(value)
		fits := width >= 0 && v.BitLen() <= int(width)
		var encoded []byte
		var err error
		if signed {
			v = signedInteger(value)
			fits = false
			if width > 0 {
				limit := new(big.Int).Lsh(big.NewInt(1), uint(width-1))
				fits = v.Cmp(new(big.Int).Neg(new(big.Int).Set(limit))) >= 0 && v.Cmp(limit) < 0
			}
			encoded, err = signedBytes(value, width)
		} else {
			encoded, err = unsignedBytes(value, width)
		}
		require.True(t, bytes.Equal(before, value), "input changed")
		if !fits {
			require.Error(t, err)
			return
		}
		require.NoError(t, err)
		require.NotEmpty(t, encoded)
		observed := new(big.Int).SetBytes(encoded)
		bitCount := v.BitLen()
		if signed {
			observed = signedInteger(encoded)
			if v.Sign() < 0 {
				bitCount = new(big.Int).Not(v).BitLen()
			}
			bitCount++
		}
		if bitCount == 0 {
			bitCount = 1
		}
		require.Zero(t, v.Cmp(observed), "value changed")
		require.Len(t, encoded, (bitCount+7)/8, "encoding is not shortest")
	})
}

func FuzzRegisterP4Data(f *testing.F) {
	bits := &p4configv1.P4DataTypeSpec{TypeSpec: &p4configv1.P4DataTypeSpec_Bitstring{Bitstring: &p4configv1.P4BitstringLikeTypeSpec{TypeSpec: &p4configv1.P4BitstringLikeTypeSpec_Bit{Bit: &p4configv1.P4BitTypeSpec{Bitwidth: 9}}}}}
	signed := proto.Clone(bits).(*p4configv1.P4DataTypeSpec)
	signed.GetBitstring().TypeSpec = &p4configv1.P4BitstringLikeTypeSpec_Int{Int: &p4configv1.P4IntTypeSpec{Bitwidth: 9}}
	variable := proto.Clone(bits).(*p4configv1.P4DataTypeSpec)
	variable.GetBitstring().TypeSpec = &p4configv1.P4BitstringLikeTypeSpec_Varbit{Varbit: &p4configv1.P4VarbitTypeSpec{MaxBitwidth: 32}}
	boolean := &p4configv1.P4DataTypeSpec{TypeSpec: &p4configv1.P4DataTypeSpec_Bool{Bool: &p4configv1.P4BoolType{}}}
	tuple := &p4configv1.P4DataTypeSpec{TypeSpec: &p4configv1.P4DataTypeSpec_Tuple{Tuple: &p4configv1.P4TupleTypeSpec{Members: []*p4configv1.P4DataTypeSpec{bits, boolean}}}}
	specs := []*p4configv1.P4DataTypeSpec{bits, signed, variable, boolean, tuple}
	values := []*p4v1.P4Data{
		{Data: &p4v1.P4Data_Bitstring{Bitstring: []byte{0, 1}}},
		{Data: &p4v1.P4Data_Bitstring{Bitstring: []byte{255, 255}}},
		{Data: &p4v1.P4Data_Varbit{Varbit: &p4v1.P4Varbit{Bitwidth: 16, Bitstring: []byte{0, 1}}}},
		{Data: &p4v1.P4Data_Bool{Bool: false}},
		{Data: &p4v1.P4Data_Tuple{Tuple: &p4v1.P4StructLike{Members: []*p4v1.P4Data{{Data: &p4v1.P4Data_Bitstring{Bitstring: []byte{0, 1}}}, {Data: &p4v1.P4Data_Bool{Bool: false}}}}}},
	}
	for i, value := range values {
		wire, err := proto.Marshal(value)
		require.NoError(f, err)
		f.Add(byte(i), wire)
	}
	f.Fuzz(func(t *testing.T, kind byte, wire []byte) {
		if len(wire) > 2048 {
			return
		}
		value := &p4v1.P4Data{}
		if err := proto.Unmarshal(wire, value); err != nil {
			return
		}
		before := proto.Clone(value)
		n := dataNormalizer{active: make(map[*p4configv1.P4DataTypeSpec]bool)}
		spec := specs[int(kind)%len(specs)]
		normalized, err := n.normalize(spec, value, false)
		require.True(t, proto.Equal(before, value), "input changed")
		if err != nil {
			return
		}
		again, err := n.normalize(spec, normalized, false)
		require.NoError(t, err)
		require.True(t, proto.Equal(normalized, again), "encoding is not idempotent")
		encoded, err := proto.Marshal(normalized)
		require.NoError(t, err)
		decoded := &p4v1.P4Data{}
		require.NoError(t, proto.Unmarshal(encoded, decoded))
		require.True(t, proto.Equal(normalized, decoded), "wire round trip changed value")
	})
}
