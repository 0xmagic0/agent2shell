package transfer

import (
	"context"
	"errors"
	"testing"

	"github.com/0xmagic0/agent2shell/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockExec returns an ExecFunc that maps probe commands to canned responses.
func mockExec(responses map[string]*types.ExecResponse, errFor map[string]error) ExecFunc {
	return func(ctx context.Context, command string, timeout int) (*types.ExecResponse, error) {
		if err, ok := errFor[command]; ok {
			return nil, err
		}
		if resp, ok := responses[command]; ok {
			return resp, nil
		}
		// Default: command not found, exit 127
		return &types.ExecResponse{ExitCode: 127, Output: "command not found"}, nil
	}
}

func TestDetectDecoder(t *testing.T) {
	t.Parallel()

	base64Probe := decoderCandidates[0].probe
	opensslProbe := decoderCandidates[1].probe
	python3Probe := decoderCandidates[2].probe

	tests := []struct {
		name        string
		responses   map[string]*types.ExecResponse
		errFor      map[string]error
		wantName    string
		wantErr     error
		wantErrWrap bool // true = errors.Is check rather than equality
	}{
		{
			name: "base64 available",
			responses: map[string]*types.ExecResponse{
				base64Probe: {ExitCode: 0, Output: "test"},
			},
			wantName: "base64",
		},
		{
			name: "only python3 available",
			responses: map[string]*types.ExecResponse{
				base64Probe:  {ExitCode: 127, Output: ""},
				opensslProbe: {ExitCode: 127, Output: ""},
				python3Probe: {ExitCode: 0, Output: "test"},
			},
			wantName: "python3",
		},
		{
			name:    "none work",
			wantErr: ErrNoDecoder,
		},
		{
			name: "transport error on probe continues to next",
			errFor: map[string]error{
				base64Probe: errors.New("transport error"),
			},
			responses: map[string]*types.ExecResponse{
				opensslProbe: {ExitCode: 0, Output: "test"},
			},
			wantName: "openssl",
		},
		{
			name: "all return non-zero exit code",
			responses: map[string]*types.ExecResponse{
				base64Probe:  {ExitCode: 1, Output: ""},
				opensslProbe: {ExitCode: 1, Output: ""},
				python3Probe: {ExitCode: 1, Output: ""},
			},
			wantErr: ErrNoDecoder,
		},
		{
			name: "output has extra whitespace",
			responses: map[string]*types.ExecResponse{
				base64Probe: {ExitCode: 0, Output: "  test\n"},
			},
			wantName: "base64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			exec := mockExec(tt.responses, tt.errFor)
			got, err := DetectDecoder(context.Background(), exec, 5)
			if tt.wantErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, got)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, tt.wantName, got.Name)
		})
	}
}

func TestDetectDecoder_ContextCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	exec := mockExec(nil, nil)
	got, err := DetectDecoder(ctx, exec, 5)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, got)
}
