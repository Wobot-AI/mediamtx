package conf

import (
	"testing"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/stretchr/testify/require"
)

func TestPathClone(t *testing.T) {
	original := &Path{
		Name:                "example",
		RTSPTransport:       RTSPTransport{new(gortsplib.ProtocolUDP)},
		SourceAnyPortEnable: new(true),
		RecordPath:          "/var/recordings",
	}

	clone := original.Clone()
	require.Equal(t, original, clone)
}

func TestIsValidPathName(t *testing.T) {
	for _, ca := range []struct {
		name   string
		path   string
		errMsg string
	}{
		{
			name: "valid nested path",
			path: "group/cam1",
		},
		{
			name: "valid dots inside segment",
			path: "cam.v1/main",
		},
		{
			name:   "parent directory",
			path:   "../cam1",
			errMsg: "can't contain dot path segments",
		},
		{
			name:   "embedded parent directory",
			path:   "group/../cam1",
			errMsg: "can't contain dot path segments",
		},
		{
			name:   "current directory",
			path:   "./cam1",
			errMsg: "can't contain dot path segments",
		},
	} {
		t.Run(ca.name, func(t *testing.T) {
			err := IsValidPathName(ca.path)
			if ca.errMsg != "" {
				require.EqualError(t, err, ca.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestPathHasOnDemandPublisherHTTP(t *testing.T) {
	require.True(t, Path{RunOnDemandHTTPAddress: "http://x/y"}.HasOnDemandPublisher())
	require.True(t, Path{RunOnDemand: "cmd"}.HasOnDemandPublisher())

	// the un-demand address alone does not summon a publisher, mirroring
	// the existing asymmetry with RunOnUnDemand.
	require.False(t, Path{RunOnUnDemandHTTPAddress: "http://x/y"}.HasOnDemandPublisher())
	require.False(t, Path{}.HasOnDemandPublisher())
}

func TestPathValidateOnDemandHTTP(t *testing.T) {
	for _, ca := range []struct {
		name   string
		mutate func(*Path)
		errMsg string
	}{
		{
			name:   "valid",
			mutate: func(p *Path) { p.RunOnDemandHTTPAddress = "https://example.com/initiate" },
		},
		{
			name: "valid with headers",
			mutate: func(p *Path) {
				p.RunOnDemandHTTPAddress = "https://example.com/initiate"
				p.RunOnDemandHTTPHeaders = []string{"Content-Type: application/json"}
			},
		},
		{
			name:   "address without scheme",
			mutate: func(p *Path) { p.RunOnDemandHTTPAddress = "example.com/initiate" },
			errMsg: "'runOnDemandHTTPAddress' must begin with http:// or https://",
		},
		{
			name: "malformed header",
			mutate: func(p *Path) {
				p.RunOnDemandHTTPAddress = "https://example.com/initiate"
				p.RunOnDemandHTTPHeaders = []string{"not-a-header"}
			},
			errMsg: "is not in 'Name: value' form",
		},
		{
			name: "incompatible with static source",
			mutate: func(p *Path) {
				p.Source = "rtsp://example.com/stream"
				p.SourceOnDemand = true
				p.RunOnDemandHTTPAddress = "https://example.com/initiate"
			},
			errMsg: "can be used only when source is 'publisher'",
		},
		{
			name:   "un-demand body without an address",
			mutate: func(p *Path) { p.RunOnUnDemandHTTPBody = `{"type":"stop"}` },
			errMsg: "'runOnUnDemandHTTPBody' requires 'runOnDemandHTTPAddress'",
		},
	} {
		t.Run(ca.name, func(t *testing.T) {
			pconf := &Path{}
			pconf.setDefaults()
			ca.mutate(pconf)

			err := pconf.validate(&Conf{}, "mypath", false, nil)

			if ca.errMsg == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), ca.errMsg)
			}
		})
	}
}

func TestPathValidateOnDemandHTTPAlwaysAvailable(t *testing.T) {
	pconf := &Path{}
	pconf.setDefaults()
	pconf.AlwaysAvailable = true
	pconf.AlwaysAvailableTracks = []AlwaysAvailableTrack{{Codec: CodecH264}}
	pconf.RunOnDemandHTTPAddress = "https://example.com/initiate"

	err := pconf.validate(&Conf{}, "mypath", false, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be used with 'alwaysAvailable'")
}

func TestPathValidateRunOnDemandExcludeQuery(t *testing.T) {
	pconf := &Path{}
	pconf.setDefaults()
	pconf.RunOnDemandHTTPAddress = "https://example.com/initiate"
	pconf.RunOnDemandExcludeQuery = "type=healthcheck"

	require.NoError(t, pconf.validate(&Conf{}, "mypath", false, nil))
	require.NotNil(t, pconf.RunOnDemandExcludeQueryRegexp)
	require.True(t, pconf.RunOnDemandExcludeQueryRegexp.MatchString("type=healthcheck"))
	require.True(t, pconf.RunOnDemandExcludeQueryRegexp.MatchString("a=b&type=healthcheck"))
	require.False(t, pconf.RunOnDemandExcludeQueryRegexp.MatchString("type=live"))

	bad := &Path{}
	bad.setDefaults()
	bad.RunOnDemandExcludeQuery = "([unclosed"
	err := bad.validate(&Conf{}, "mypath", false, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "'runOnDemandExcludeQuery' is not a valid regular expression")
}
