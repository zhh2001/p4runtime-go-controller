package fixtures_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/zhh2001/p4runtime-go-controller/pipeline"
	"github.com/zhh2001/p4runtime-go-controller/tableentry"
)

func loadL2(t *testing.T, infoPath string) *pipeline.Pipeline {
	t.Helper()
	info, err := os.ReadFile(infoPath)
	require.NoError(t, err)
	p, err := pipeline.LoadText(info, nil)
	require.NoError(t, err)
	return p
}

func TestL2Fixture(t *testing.T) {
	p := loadL2(t, "../../examples/testdata/l2.p4info.txt")
	table, ok := p.Table("MyIngress.t_l2")
	require.True(t, ok)
	assert.EqualValues(t, 0x02000001, table.ID)
	require.Len(t, table.MatchFields, 1)
	assert.Equal(t, "hdr.eth.dst", table.MatchFields[0].Name)
	assert.EqualValues(t, 48, table.MatchFields[0].Bitwidth)
	assert.Equal(t, p4configv1.MatchField_EXACT, table.MatchFields[0].MatchType)
	_, err := tableentry.NewBuilder(p, table.Name).Match("hdr.eth.dst", tableentry.Exact([]byte{1})).
		Action("MyIngress.forward", tableentry.Param("port", []byte{1})).Build()
	require.NoError(t, err)
	counter, ok := p.Counter("MyIngress.pkt_counter")
	require.True(t, ok)
	assert.EqualValues(t, 512, counter.Size)
	assert.Equal(t, p4configv1.CounterSpec_BOTH, counter.Unit)
	direct, ok := p.DirectCounter("MyIngress.pkt_counter_direct")
	require.True(t, ok)
	assert.Equal(t, table.Name, direct.DirectTableName)
	assert.Contains(t, table.Raw().GetDirectResourceIds(), direct.ID)
	for name, port := range map[string]string{"packet_in": "ingress_port", "packet_out": "egress_port"} {
		metadata, ok := p.PacketMetadata(name)
		require.True(t, ok)
		require.Len(t, metadata.Metadata, 2)
		field, ok := metadata.Field(port)
		require.True(t, ok)
		assert.EqualValues(t, 1, field.ID)
		assert.EqualValues(t, 9, field.Bitwidth)
		padding, ok := metadata.Field("_pad")
		require.True(t, ok)
		assert.EqualValues(t, 2, padding.ID)
		assert.EqualValues(t, 7, padding.Bitwidth)
	}
}

func TestL2Compilation(t *testing.T) {
	compiler, err := exec.LookPath("p4c-bm2-ss")
	if err != nil {
		t.Skip("p4c-bm2-ss unavailable; skipping source compilation")
	}
	dir := t.TempDir()
	infoPath, configPath := filepath.Join(dir, "l2.p4info.txtpb"), filepath.Join(dir, "l2.bmv2.json")
	command := exec.Command(compiler, "--arch", "v1model", "--p4runtime-files", infoPath, "-o", configPath, "../../examples/testdata/l2.p4")
	output, err := command.CombinedOutput()
	require.NoError(t, err, "compiler output: %s", output)
	generated := loadL2(t, infoPath)
	fixture := loadL2(t, "../../examples/testdata/l2.p4info.txt")
	assert.True(t, proto.Equal(fixture.Info(), generated.Info()), "regenerate the L2 fixture from its source with scripts/compile-l2.sh")
	config, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.NotEmpty(t, config)
}
