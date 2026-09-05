package s3store

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"nautilus/internal/errors"
	"nautilus/internal/objectstore"
	"nautilus/internal/optional"
	"nautilus/internal/testutil/require"
)

func TestByteRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name string
		Opts *objectstore.GetOptions
		Want string
	}{
		{
			Name: "offset and length",
			Opts: &objectstore.GetOptions{
				Offset: optional.Set[int64](10),
				Length: optional.Set[int64](5),
			},
			Want: "bytes=10-14",
		},
		{
			Name: "offset only",
			Opts: &objectstore.GetOptions{
				Offset: optional.Set[int64](10),
			},
			Want: "bytes=10-",
		},
		{
			Name: "length only",
			Opts: &objectstore.GetOptions{
				Length: optional.Set[int64](5),
			},
			Want: "bytes=0-4",
		},
		{
			Name: "zero offset and length",
			Opts: &objectstore.GetOptions{
				Offset: optional.Set[int64](0),
				Length: optional.Set[int64](0),
			},
		},
		{
			Name: "negative values",
			Opts: &objectstore.GetOptions{
				Offset: optional.Set[int64](-1),
				Length: optional.Set[int64](-1),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.Want, byteRange(tt.Opts))
		})
	}
}

func TestIsNotFound(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name string
		Err  error
		Want bool
	}{
		{
			Name: "no such key",
			Err:  &types.NoSuchKey{},
			Want: true,
		},
		{
			Name: "not found",
			Err:  &types.NotFound{},
			Want: true,
		},
		{
			Name: "wrapped no such key",
			Err:  errors.Wrap(&types.NoSuchKey{}, "get object"),
			Want: true,
		},
		{
			Name: "other error",
			Err:  errors.New("boom"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.Want, isNotFound(tt.Err))
		})
	}
}
