package plugindata

import (
	"context"
	"testing"

	"github.com/gravitational/teleport/api/types"
	"github.com/gravitational/trace"
	"github.com/stretchr/testify/require"
)

const (
	resourceKind = "test"
)

type mockData struct {
	Foo string
	Bar string
}

func mockEncode(source mockData) map[string]string {
	result := make(map[string]string)

	result["foo"] = source.Foo
	result["bar"] = source.Bar

	return result
}

func mockDecode(source map[string]string) mockData {
	result := mockData{}

	result.Foo = source["foo"]
	result.Bar = source["bar"]

	return result
}

type mockClient struct {
	oldDataCursor      int
	oldData            []map[string]string
	updateResult       []error
	updateResultCursor int
}

func (c *mockClient) GetPluginData(_ context.Context, f types.PluginDataFilter) ([]types.PluginData, error) {
	i, err := types.NewPluginData(f.Resource, resourceKind)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	d, ok := i.(*types.PluginDataV3)
	if !ok {
		return nil, trace.Errorf("Failed to convert %T to types.PluginDataV3", i)
	}

	var data map[string]string
	if c.oldDataCursor < len(c.oldData) {
		data = c.oldData[c.oldDataCursor]
	}
	c.oldDataCursor++

	d.Spec.Entries = map[string]*types.PluginDataEntry{
		resourceKind: {Data: data},
	}

	return []types.PluginData{d}, nil
}

func (c *mockClient) UpdatePluginData(context.Context, types.PluginDataUpdateParams) error {
	if c.updateResultCursor+1 > len(c.updateResult) {
		return nil
	}
	err := c.updateResult[c.updateResultCursor]
	c.updateResultCursor++
	return err
}

func TestModifyFailed(t *testing.T) {
	c := &mockClient{
		oldData: []map[string]string{{"foo": "value"}},
	}
	cas := NewCAS(c, resourceKind, types.KindAccessRequest, mockEncode, mockDecode)

	r, err := cas.Update(context.Background(), "foo", func(data mockData) (mockData, error) {
		return mockData{}, trace.Errorf("fail")
	})

	require.Error(t, err, "fail")
	require.Equal(t, r, mockData{})
}

func TestModifySuccess(t *testing.T) {
	c := &mockClient{
		oldData: []map[string]string{{"foo": "value"}},
	}
	cas := NewCAS(c, resourceKind, types.KindAccessRequest, mockEncode, mockDecode)

	r, err := cas.Update(context.Background(), "foo", func(i mockData) (mockData, error) {
		i.Foo = "other value"
		return i, nil
	})

	require.NoError(t, err)
	require.NotNil(t, r)
	require.Equal(t, r.Foo, "other value")
}

func TestBackoff(t *testing.T) {
	c := &mockClient{
		oldData:      []map[string]string{{"foo": "value"}, {"foo": "value"}},
		updateResult: []error{trace.CompareFailed("fail"), nil},
	}
	cas := NewCAS(c, resourceKind, types.KindAccessRequest, mockEncode, mockDecode)

	r, err := cas.Update(context.Background(), "foo", func(_ mockData) (mockData, error) {
		return mockData{Foo: "yes"}, nil
	})

	require.NoError(t, err)
	require.NotNil(t, r)
	require.Equal(t, r.Foo, "yes")
}
