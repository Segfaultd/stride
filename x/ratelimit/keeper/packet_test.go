package keeper_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"

	sdkmath "cosmossdk.io/math"
	transfertypes "github.com/cosmos/ibc-go/v5/modules/apps/transfer/types"
	channeltypes "github.com/cosmos/ibc-go/v5/modules/core/04-channel/types"
	"github.com/stretchr/testify/require"

	tmbytes "github.com/tendermint/tendermint/libs/bytes"

	"github.com/Stride-Labs/stride/v9/x/ratelimit/keeper"
	"github.com/Stride-Labs/stride/v9/x/ratelimit/types"
)

const (
	transferPort    = "transfer"
	uosmo           = "uosmo"
	ujuno           = "ujuno"
	ustrd           = "ustrd"
	stuatom         = "stuatom"
	channelOnStride = "channel-0"
	channelOnHost   = "channel-1"
)

func hashDenomTrace(denomTrace string) string {
	trace32byte := sha256.Sum256([]byte(denomTrace))
	var traceTmByte tmbytes.HexBytes = trace32byte[:]
	return fmt.Sprintf("ibc/%s", traceTmByte)
}

func TestParseDenomFromSendPacket(t *testing.T) {
	testCases := []struct {
		name             string
		packetDenomTrace string
		expectedDenom    string
	}{
		// Native assets stay as is
		{
			name:             "ustrd",
			packetDenomTrace: ustrd,
			expectedDenom:    ustrd,
		},
		{
			name:             "stuatom",
			packetDenomTrace: stuatom,
			expectedDenom:    stuatom,
		},
		// Non-native assets are hashed
		{
			name:             "uosmo_one_hop",
			packetDenomTrace: "transfer/channel-0/usomo",
			expectedDenom:    hashDenomTrace("transfer/channel-0/usomo"),
		},
		{
			name:             "uosmo_two_hops",
			packetDenomTrace: "transfer/channel-2/transfer/channel-1/usomo",
			expectedDenom:    hashDenomTrace("transfer/channel-2/transfer/channel-1/usomo"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			packet := transfertypes.FungibleTokenPacketData{
				Denom: tc.packetDenomTrace,
			}

			parsedDenom := keeper.ParseDenomFromSendPacket(packet)
			require.Equal(t, tc.expectedDenom, parsedDenom, tc.name)
		})
	}
}

func TestParseDenomFromRecvPacket(t *testing.T) {
	osmoChannelOnStride := "channel-0"
	strideChannelOnOsmo := "channel-100"
	junoChannelOnOsmo := "channel-200"
	junoChannelOnStride := "channel-300"

	testCases := []struct {
		name               string
		packetDenomTrace   string
		sourceChannel      string
		destinationChannel string
		expectedDenom      string
	}{
		// Sink asset one hop away:
		//   uosmo sent from Osmosis to Stride (uosmo)
		//   -> tack on prefix (transfer/channel-0/uosmo) and hash
		{
			name:               "sink_one_hop",
			packetDenomTrace:   uosmo,
			sourceChannel:      strideChannelOnOsmo,
			destinationChannel: osmoChannelOnStride,
			expectedDenom:      hashDenomTrace(fmt.Sprintf("%s/%s/%s", transferPort, osmoChannelOnStride, uosmo)),
		},
		// Sink asset two hops away:
		//   ujuno sent from Juno to Osmosis to Stride (transfer/channel-200/ujuno)
		//   -> tack on prefix (transfer/channel-0/transfer/channel-200/ujuno) and hash
		{
			name:               "sink_two_hops",
			packetDenomTrace:   fmt.Sprintf("%s/%s/%s", transferPort, junoChannelOnOsmo, ujuno),
			sourceChannel:      strideChannelOnOsmo,
			destinationChannel: osmoChannelOnStride,
			expectedDenom:      hashDenomTrace(fmt.Sprintf("%s/%s/%s/%s/%s", transferPort, osmoChannelOnStride, transferPort, junoChannelOnOsmo, ujuno)),
		},
		// Native source assets
		//    ustrd sent from Stride to Osmosis and then back to Stride (transfer/channel-0/ustrd)
		//    -> remove prefix and leave as is (ustrd)
		{
			name:               "native_source",
			packetDenomTrace:   fmt.Sprintf("%s/%s/%s", transferPort, strideChannelOnOsmo, ustrd),
			sourceChannel:      strideChannelOnOsmo,
			destinationChannel: osmoChannelOnStride,
			expectedDenom:      ustrd,
		},
		// Non-native source assets
		//    ujuno was sent from Juno to Stride, then to Osmosis, then back to Stride (transfer/channel-0/transfer/channel-300/ujuno)
		//    -> remove prefix (transfer/channel-300/ujuno) and hash
		{
			name:               "non_native_source",
			packetDenomTrace:   fmt.Sprintf("%s/%s/%s/%s/%s", transferPort, strideChannelOnOsmo, transferPort, junoChannelOnStride, ujuno),
			sourceChannel:      strideChannelOnOsmo,
			destinationChannel: osmoChannelOnStride,
			expectedDenom:      hashDenomTrace(fmt.Sprintf("%s/%s/%s", transferPort, junoChannelOnStride, ujuno)),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			packet := channeltypes.Packet{
				SourcePort:         transferPort,
				DestinationPort:    transferPort,
				SourceChannel:      tc.sourceChannel,
				DestinationChannel: tc.destinationChannel,
			}
			packetData := transfertypes.FungibleTokenPacketData{
				Denom: tc.packetDenomTrace,
			}

			parsedDenom := keeper.ParseDenomFromRecvPacket(packet, packetData)
			require.Equal(t, tc.expectedDenom, parsedDenom, tc.name)
		})
	}
}

func (s *KeeperTestSuite) createRateLimitCloseToQuota(denom string, channelId string, direction types.PacketDirection) {
	channelValue := sdkmath.NewInt(100)
	threshold := sdkmath.NewInt(10)

	// Set inflow/outflow close to threshold, depending on which direction we're going in
	inflow := sdkmath.ZeroInt()
	outflow := sdkmath.ZeroInt()
	if direction == types.PACKET_RECV {
		inflow = sdkmath.NewInt(9)
	} else {
		outflow = sdkmath.NewInt(9)
	}

	// Store rate limit
	s.App.RatelimitKeeper.SetRateLimit(s.Ctx, types.RateLimit{
		Path: &types.Path{
			Denom:     denom,
			ChannelId: channelId,
		},
		Quota: &types.Quota{
			MaxPercentSend: threshold,
			MaxPercentRecv: threshold,
		},
		Flow: &types.Flow{
			Inflow:       inflow,
			Outflow:      outflow,
			ChannelValue: channelValue,
		},
	})
}

func (s *KeeperTestSuite) TestSendRateLimitedPacket() {
	// For send packets, the source will be stride and the destination will be the host
	denom := ustrd
	sourceChannel := channelOnStride
	destinationChannel := channelOnHost
	amountToExceed := "5"

	// Create rate limit (for SEND, use SOURCE channel)
	s.createRateLimitCloseToQuota(denom, sourceChannel, types.PACKET_SEND)

	// This packet should cause an Outflow quota exceed error
	packetData, err := json.Marshal(transfertypes.FungibleTokenPacketData{Denom: denom, Amount: amountToExceed})
	s.Require().NoError(err)
	packet := channeltypes.Packet{
		SourcePort:         transferPort,
		SourceChannel:      sourceChannel,
		DestinationPort:    transferPort,
		DestinationChannel: destinationChannel,
		Data:               packetData,
	}

	// We check for a quota error because it doesn't appear until the end of the function
	// We're avoiding checking for a success here because we can get a false positive if the rate limit doesn't exist
	err = s.App.RatelimitKeeper.SendRateLimitedPacket(s.Ctx, packet)
	s.Require().ErrorIs(err, types.ErrQuotaExceeded, "error type")
	s.Require().ErrorContains(err, "Outflow exceeds quota", "error text")
}

func (s *KeeperTestSuite) TestReceiveRateLimitedPacket() {
	// For receive packets, the source will be the host and the destination will be stride
	packetDenom := uosmo
	sourceChannel := channelOnHost
	destinationChannel := channelOnStride
	amountToExceed := "5"

	// When the packet is recieved, the port and channel prefix will be added and the denom will be hashed
	//  before the rate limit is found from the store
	rateLimitDenom := hashDenomTrace(fmt.Sprintf("%s/%s/%s", transferPort, channelOnStride, packetDenom))

	// Create rate limit (for RECV, use DESTINATION channel)
	s.createRateLimitCloseToQuota(rateLimitDenom, destinationChannel, types.PACKET_RECV)

	// This packet should cause an Outflow quota exceed error
	packetData, err := json.Marshal(transfertypes.FungibleTokenPacketData{Denom: packetDenom, Amount: amountToExceed})
	s.Require().NoError(err)
	packet := channeltypes.Packet{
		SourcePort:         transferPort,
		SourceChannel:      sourceChannel,
		DestinationPort:    transferPort,
		DestinationChannel: destinationChannel,
		Data:               packetData,
	}

	// We check for a quota error because it doesn't appear until the end of the function
	// We're avoiding checking for a success here because we can get a false positive if the rate limit doesn't exist
	err = s.App.RatelimitKeeper.ReceiveRateLimitedPacket(s.Ctx, packet)
	s.Require().ErrorIs(err, types.ErrQuotaExceeded, "error type")
	s.Require().ErrorContains(err, "Inflow exceeds quota", "error text")
}
