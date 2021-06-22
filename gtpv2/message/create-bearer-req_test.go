// Copyright 2019-2021 go-gtp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package message_test

import (
	"net"
	"testing"

	"github.com/jbdamiano/go-gtp/gtpv2/ie"
	"github.com/jbdamiano/go-gtp/gtpv2/message"
	"github.com/jbdamiano/go-gtp/gtpv2/testutils"
)

func TestCreateBearerRequest(t *testing.T) {
	cases := []testutils.TestCase{
		{
			Description: "Normal",
			Structured: message.NewCreateBearerRequest(
				testutils.TestBearerInfo.TEID, testutils.TestBearerInfo.Seq,
				ie.NewEPSBearerID(5),
				ie.NewBearerContext(
					ie.NewEPSBearerID(0),
					ie.NewBearerQoS(1, 2, 1, 0xff, 0x1111111111, 0x2222222222, 0x1111111111, 0x2222222222),
					ie.NewBearerTFTCreateNewTFT(
						[]*ie.TFTPacketFilter{
							ie.NewTFTPacketFilter(
								ie.TFTPFPreRel7TFTFilter, 1, 0,
								ie.NewTFTPFComponentIPv4RemoteAddress(
									net.ParseIP("127.0.0.1"), net.IPv4Mask(255, 255, 255, 0),
								),
								ie.NewTFTPFComponentIPv4LocalAddress(
									net.ParseIP("127.0.0.1"), net.IPv4Mask(255, 255, 255, 0),
								),
								ie.NewTFTPFComponentIPv6RemoteAddress(
									net.ParseIP("2001::1"), net.CIDRMask(64, 128),
								),
								ie.NewTFTPFComponentIPv6RemoteAddressPrefixLength(
									net.ParseIP("2001::1"), 64,
								),
								ie.NewTFTPFComponentIPv6LocalAddressPrefixLength(
									net.ParseIP("2001::1"), 64,
								),
							),
							ie.NewTFTPacketFilter(
								ie.TFTPFDownlinkOnly, 2, 0,
								ie.NewTFTPFComponentProtocolIdentifierNextHeader(1),
								ie.NewTFTPFComponentSingleLocalPort(2152),
								ie.NewTFTPFComponentSingleRemotePort(2123),
								ie.NewTFTPFComponentLocalPortRange(2123, 2152),
								ie.NewTFTPFComponentRemotePortRange(2152, 2123),
							),
							ie.NewTFTPacketFilter(
								ie.TFTPFUplinkOnly, 3, 0,
								ie.NewTFTPFComponentSecurityParameterIndex(0xdeadbeef),
								ie.NewTFTPFComponentTypeOfServiceTrafficClass(1, 2),
								ie.NewTFTPFComponentFlowLabel(0x00011111),
								ie.NewTFTPFComponentDestinationMACAddress(mac1),
								ie.NewTFTPFComponentSourceMACAddress(mac2),
							),
							ie.NewTFTPacketFilter(
								ie.TFTPFBidirectional, 4, 0,
								ie.NewTFTPFComponentDot1QCTAGVID(0x0111),
								ie.NewTFTPFComponentDot1QSTAGVID(0x0222),
								ie.NewTFTPFComponentDot1QCTAGPCPDEI(3),
								ie.NewTFTPFComponentDot1QSTAGPCPDEI(5),
								ie.NewTFTPFComponentEthertype(0x0800),
							),
						},
						[]*ie.TFTParameter{
							ie.NewTFTParameter(ie.TFTParamIDAuthorizationToken, []byte{0xde, 0xad, 0xbe, 0xef}),
							ie.NewTFTParameter(ie.TFTParamIDFlowIdentifier, []byte{0x11, 0x11, 0x22, 0x22}),
							ie.NewTFTParameter(ie.TFTParamIDPacketFileterIdentifier, []byte{0x01, 0x02, 0x03, 0x04}),
						},
					),
				),
			),
			Serialized: []byte{
				// Header
				0x48, 0x5f, 0x00, 0xe3, 0x11, 0x22, 0x33, 0x44, 0x00, 0x00, 0x01, 0x00,
				// EBI
				0x49, 0x00, 0x01, 0x00, 0x05,
				// BearerContext
				0x5d, 0x00, 0xd2, 0x00,
				//   EBI
				0x49, 0x00, 0x01, 0x00, 0x00,
				//   BearerQoS
				0x50, 0x00, 0x16, 0x00, 0x49, 0xff,
				0x11, 0x11, 0x11, 0x11, 0x11, 0x22, 0x22, 0x22, 0x22, 0x22,
				0x11, 0x11, 0x11, 0x11, 0x11, 0x22, 0x22, 0x22, 0x22, 0x22,
				//   BearerTFT
				0x54, 0x00, 0xaf, 0x00, 0x34, 0x01, 0x00, 0x57, 0x10, 0x7f, 0x00, 0x00, 0x01, 0xff, 0xff, 0xff,
				0x00, 0x11, 0x7f, 0x00, 0x00, 0x01, 0xff, 0xff, 0xff, 0x00, 0x20, 0x20, 0x01, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0xff, 0xff, 0xff, 0xff, 0xff,
				0xff, 0xff, 0xff, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x21, 0x20, 0x01, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x40, 0x23, 0x20, 0x01,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x40, 0x12,
				0x00, 0x12, 0x30, 0x01, 0x40, 0x08, 0x68, 0x50, 0x08, 0x4b, 0x41, 0x08, 0x4b, 0x08, 0x68, 0x51,
				0x08, 0x68, 0x08, 0x4b, 0x23, 0x00, 0x1a, 0x60, 0xde, 0xad, 0xbe, 0xef, 0x70, 0x01, 0x02, 0x80,
				0x01, 0x11, 0x11, 0x81, 0x12, 0x34, 0x56, 0x78, 0x90, 0x01, 0x82, 0x12, 0x34, 0x56, 0x78, 0x90,
				0x02, 0x34, 0x00, 0x0d, 0x83, 0x01, 0x11, 0x84, 0x02, 0x22, 0x85, 0x03, 0x86, 0x05, 0x87, 0x08,
				0x00, 0x01, 0x04, 0xde, 0xad, 0xbe, 0xef, 0x02, 0x04, 0x11, 0x11, 0x22, 0x22, 0x03, 0x04, 0x01,
				0x02, 0x03, 0x04,
			},
		},
	}

	testutils.Run(t, cases, func(b []byte) (testutils.Serializable, error) {
		v, err := message.ParseCreateBearerRequest(b)
		if err != nil {
			return nil, err
		}
		v.Payload = nil
		return v, nil
	})
}
